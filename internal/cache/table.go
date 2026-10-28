package cache

import (
	"encoding/binary"
	"hash/maphash"
	"math/bits"
	"sync/atomic"
	"unsafe"
)

// table is one shard's keyspace. Keys and string values live in records
// packed into byte chunks (an arena), and an open-addressing index of
// 8-byte slots points at them. Compared with a map[string]*entry this
// stores a short key with a 32-byte value in about 70 bytes instead of
// about 120, and since neither the index nor the chunks contain pointers,
// the garbage collector doesn't scan them: its work no longer grows with
// the number of keys. Collections stay ordinary Go values, referenced from
// their record through colls.
//
// A record's key never changes once written, so keys may be handed out as
// strings aliasing the chunk (SCAN results), which Go keeps alive for them.
// Values are overwritten in place when the new one has the same length, so
// values must not be read after the shard lock is released: callers copy
// them or encode them while holding it. Replaced and deleted records become
// dead space that maybeCompact reclaims by copying the live ones to new
// chunks.
//
// A table is not safe for concurrent use; the shard lock guards it. Access
// metadata is the exception: readers update it atomically.
type table struct {
	slots []uint64 // 0 = empty, slotTomb = deleted, else tag | ref
	count int      // live keys
	tombs int      // tombstone slots

	chunks [][]byte
	live   int // bytes of live records
	dead   int // bytes of replaced or deleted records

	colls     []*entry // collection values, indexed by their records
	freeColls []uint32

	seed maphash.Seed // the cache's, to rehash keys

	// withMeta reserves 8 bytes of access metadata (LRU clock, LFU
	// counter) at the start of every record, which is then 8-byte aligned
	// so it can be updated atomically. Only caches with a memory or key
	// limit need it.
	withMeta bool
}

// A slot holds a 12-bit tag from the key's hash (never 0, so a used slot is
// never 0 or slotTomb) and a reference: the record's chunk index and offset.
const (
	slotTomb   = 1
	tagShift   = 52
	refMask    = 1<<tagShift - 1
	chunkShift = 32
	chunkMask  = 1<<20 - 1
	offMask    = 1<<32 - 1

	minChunk = 1 << 10
	maxChunk = 16 << 10

	// The low bits of a key's hash pick its shard, so in-shard positions
	// use the bits above them.
	posShift = 10
)

// Record layout:
//
//	meta     uint64, only in tables withMeta
//	kind     byte
//	flags    byte (recTTL)
//	keyLen   uvarint
//	key
//	strings:     [expireAt int64 little endian, with recTTL] valLen uvarint, value
//	collections: index into colls, uvarint
const recTTL = 1

func slotTag(h uint64) uint64 { return (h>>tagShift | 1) << tagShift }

// rec is a decoded record. key and str alias the chunk.
type rec struct {
	kind     Kind
	key      string
	str      string
	expireAt int64
	colIdx   int
	meta     *atomic.Uint64 // nil unless withMeta
	size     int            // bytes the record occupies
}

func (t *table) bytesAt(ref uint64) []byte {
	return t.chunks[ref>>chunkShift&chunkMask][ref&offMask:]
}

func (t *table) decode(ref uint64) rec {
	b := t.bytesAt(ref)
	start := len(b)
	var r rec
	if t.withMeta {
		r.meta = (*atomic.Uint64)(unsafe.Pointer(&b[0]))
		b = b[8:]
	}
	r.kind = Kind(b[0])
	flags := b[1]
	kl, n := binary.Uvarint(b[2:])
	b = b[2+n:]
	r.key = unsafe.String(unsafe.SliceData(b), int(kl))
	b = b[kl:]
	if r.kind != KindString {
		ci, n := binary.Uvarint(b)
		r.colIdx = int(ci)
		r.size = start - len(b) + n
		return r
	}
	if flags&recTTL != 0 {
		r.expireAt = int64(binary.LittleEndian.Uint64(b))
		b = b[8:]
	}
	vl, n := binary.Uvarint(b)
	b = b[n:]
	r.str = unsafe.String(unsafe.SliceData(b), int(vl))
	r.size = start - len(b) + int(vl)
	return r
}

// keyAt returns the key of the record at ref without decoding the rest.
func (t *table) keyAt(ref uint64) string {
	b := t.bytesAt(ref)
	if t.withMeta {
		b = b[8:]
	}
	kl, n := binary.Uvarint(b[2:])
	return unsafe.String(unsafe.SliceData(b[2+n:]), int(kl))
}

// find returns the slot index holding key (h is its hash), or -1.
func (t *table) find(key string, h uint64) int {
	if t.count == 0 {
		return -1
	}
	mask := uint64(len(t.slots) - 1)
	tag := slotTag(h)
	for i := h >> posShift & mask; ; i = (i + 1) & mask {
		s := t.slots[i]
		if s == 0 {
			return -1
		}
		if s&^refMask == tag && t.keyAt(s) == key {
			return int(i)
		}
	}
}

// alloc reserves n bytes (8-byte aligned withMeta) and returns their
// reference and the space itself.
func (t *table) alloc(n int) (uint64, []byte) {
	ci := len(t.chunks) - 1
	if ci >= 0 {
		c := t.chunks[ci]
		off := len(c)
		if t.withMeta {
			off = (off + 7) &^ 7
		}
		if off+n <= cap(c) {
			t.chunks[ci] = c[:off+n]
			return uint64(ci)<<chunkShift | uint64(off), c[off : off+n]
		}
	}
	size := minChunk
	if ci >= 0 {
		size = min(cap(t.chunks[ci])*2, maxChunk)
	}
	c := make([]byte, n, max(size, n))
	t.chunks = append(t.chunks, c)
	return uint64(len(t.chunks)-1) << chunkShift, c
}

func uvarintLen(x uint64) int { return (bits.Len64(x|1) + 6) / 7 }

// header writes the common record prefix and returns the rest of b.
func (t *table) header(b []byte, kind Kind, flags byte, key string) []byte {
	if t.withMeta {
		clear(b[:8])
		b = b[8:]
	}
	b[0], b[1] = byte(kind), flags
	p := 2 + binary.PutUvarint(b[2:], uint64(len(key)))
	p += copy(b[p:], key)
	return b[p:]
}

func (t *table) headerLen(key string) int {
	n := 2 + uvarintLen(uint64(len(key))) + len(key)
	if t.withMeta {
		n += 8
	}
	return n
}

// putString stores a string record for key (hash h), replacing any
// existing value, and returns its slot index. A string of the same length
// and with the same kind of expiration is overwritten in place.
func (t *table) putString(key, val string, expireAt int64, h uint64) int {
	if i := t.find(key, h); i >= 0 && t.overwrite(t.slots[i], val, expireAt) {
		return i
	}
	n := t.headerLen(key) + uvarintLen(uint64(len(val))) + len(val)
	var flags byte
	if expireAt != 0 {
		flags = recTTL
		n += 8
	}
	ref, b := t.alloc(n)
	b = t.header(b, KindString, flags, key)
	if expireAt != 0 {
		binary.LittleEndian.PutUint64(b, uint64(expireAt))
		b = b[8:]
	}
	p := binary.PutUvarint(b, uint64(len(val)))
	copy(b[p:], val)
	return t.place(key, h, ref, n)
}

// overwrite replaces the value of the string record at ref in place if it
// has the same length and the same kind of expiration, and reports whether
// it did.
func (t *table) overwrite(ref uint64, val string, expireAt int64) bool {
	b := t.bytesAt(ref)
	if t.withMeta {
		b = b[8:]
	}
	if Kind(b[0]) != KindString || (b[1]&recTTL != 0) != (expireAt != 0) {
		return false
	}
	kl, n := binary.Uvarint(b[2:])
	b = b[2+n+int(kl):]
	exp := b
	if expireAt != 0 {
		b = b[8:]
	}
	vl, n := binary.Uvarint(b)
	if int(vl) != len(val) {
		return false
	}
	if expireAt != 0 {
		binary.LittleEndian.PutUint64(exp, uint64(expireAt))
	}
	copy(b[n:], val)
	return true
}

// putColl stores collection e for key (hash h), replacing any existing
// value, and returns its slot index.
func (t *table) putColl(key string, e *entry, h uint64) int {
	var ci uint32
	if n := len(t.freeColls); n > 0 {
		ci = t.freeColls[n-1]
		t.freeColls = t.freeColls[:n-1]
		t.colls[ci] = e
	} else {
		ci = uint32(len(t.colls))
		t.colls = append(t.colls, e)
	}
	n := t.headerLen(key) + uvarintLen(uint64(ci))
	ref, b := t.alloc(n)
	b = t.header(b, e.kind, 0, key)
	binary.PutUvarint(b, uint64(ci))
	return t.place(key, h, ref, n)
}

// place points key's slot at the record just written, retiring the record
// it replaces, and returns the slot index.
func (t *table) place(key string, h uint64, ref uint64, size int) int {
	t.live += size
	slot := slotTag(h) | ref
	if i := t.find(key, h); i >= 0 {
		t.retire(t.slots[i])
		t.slots[i] = slot
		t.maybeCompact()
		return i
	}
	if (t.count+t.tombs+1)*8 > len(t.slots)*7 {
		t.rehash(t.count + 1)
	}
	mask := uint64(len(t.slots) - 1)
	i := h >> posShift & mask
	for t.slots[i] > slotTomb {
		i = (i + 1) & mask
	}
	if t.slots[i] == slotTomb {
		t.tombs--
	}
	t.slots[i] = slot
	t.count++
	return int(i)
}

// retire accounts a record that's no longer referenced and releases its
// collection.
func (t *table) retire(slot uint64) {
	r := t.decode(slot)
	t.live -= r.size
	t.dead += r.size
	if r.kind != KindString {
		t.colls[r.colIdx] = nil
		t.freeColls = append(t.freeColls, uint32(r.colIdx))
	}
}

// remove deletes the key in slot i.
func (t *table) remove(i int) {
	t.retire(t.slots[i])
	t.slots[i] = slotTomb
	t.tombs++
	t.count--
	if t.count == 0 {
		t.reset()
		return
	}
	t.maybeCompact()
}

// reset empties the table, keeping its configuration.
func (t *table) reset() { *t = table{seed: t.seed, withMeta: t.withMeta} }

// setExpire changes the expiration of the string record in slot i (key,
// hash h), in place when the record already has a TTL field.
func (t *table) setExpire(i int, key string, h uint64, expireAt int64) {
	r := t.decode(t.slots[i])
	if r.expireAt != 0 && expireAt != 0 {
		b := t.bytesAt(t.slots[i])
		if t.withMeta {
			b = b[8:]
		}
		kl, n := binary.Uvarint(b[2:])
		binary.LittleEndian.PutUint64(b[2+n+int(kl):], uint64(expireAt))
		return
	}
	var meta uint64
	if r.meta != nil {
		meta = r.meta.Load()
	}
	j := t.putString(key, r.str, expireAt, h)
	if r.meta != nil {
		t.decode(t.slots[j]).meta.Store(meta)
	}
}

// rehash rebuilds the index with room for n keys, dropping tombstones.
func (t *table) rehash(n int) {
	size := 8
	for size*7 < n*16 { // at most 7/16 full afterwards
		size *= 2
	}
	old := t.slots
	t.slots = make([]uint64, size)
	t.tombs = 0
	mask := uint64(size - 1)
	for _, s := range old {
		if s <= slotTomb {
			continue
		}
		i := t.hashOf(s) >> posShift & mask
		for t.slots[i] != 0 {
			i = (i + 1) & mask
		}
		t.slots[i] = s
	}
}

// hashOf recomputes the hash of the key in slot s (see BoltCache.hash).
func (t *table) hashOf(s uint64) uint64 { return maphash.String(t.seed, t.keyAt(s)) }

// maybeCompact copies live records to fresh chunks once dead space
// dominates, so deleted and overwritten values don't hold memory forever.
// Shards are small, so this takes microseconds.
func (t *table) maybeCompact() {
	if t.dead < minChunk || t.dead < t.live {
		return
	}
	old := t.chunks
	t.chunks = nil
	t.live, t.dead = 0, 0
	for i, s := range t.slots {
		if s <= slotTomb {
			continue
		}
		src := old[s>>chunkShift&chunkMask][s&offMask:]
		size := t.recordSize(src)
		ref, dst := t.alloc(size)
		copy(dst, src[:size])
		t.live += size
		t.slots[i] = s&^refMask | ref
	}
}

// recordSize returns the length of the record at the start of b.
func (t *table) recordSize(b []byte) int {
	start := len(b)
	if t.withMeta {
		b = b[8:]
	}
	kind, flags := Kind(b[0]), b[1]
	kl, n := binary.Uvarint(b[2:])
	b = b[2+n+int(kl):]
	if kind != KindString {
		_, n := binary.Uvarint(b)
		return start - len(b) + n
	}
	if flags&recTTL != 0 {
		b = b[8:]
	}
	vl, n := binary.Uvarint(b)
	return start - len(b) + n + int(vl)
}

// coll returns the collection entry of record r.
func (t *table) coll(r rec) *entry { return t.colls[r.colIdx] }

// each calls fn for every key's slot index until it returns false. fn may
// remove the key it's given.
func (t *table) each(fn func(i int) bool) {
	for i := 0; i < len(t.slots); i++ {
		if t.slots[i] > slotTomb && !fn(i) {
			return
		}
	}
}
