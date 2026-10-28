package cache

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"unicode/utf8"
)

// Snapshot format (version 2): newline-delimited JSON, optionally gzipped.
// The first line is a header, every following line is one key:
//
//	{"format":"boltcache","version":2,"created":"2026-01-02T15:04:05Z"}
//	{"k":"user:1","t":"hash","h":{"name":"Ada"},"x":1767366245000}
//
// "x" is the expiration in unix milliseconds. When a key or any value is not
// valid UTF-8 the entry carries "b":true and every string in it is base64
// encoded, so binary data round-trips exactly.
//
// Version 1 snapshots (a single JSON object written by BoltCache 1.x) are
// still loaded; see loadLegacy.

const (
	snapshotFormat  = "boltcache"
	snapshotVersion = 2
)

type snapHeader struct {
	Format  string    `json:"format"`
	Version int       `json:"version"`
	Created time.Time `json:"created"`
}

type snapEntry struct {
	K string            `json:"k"`
	T string            `json:"t"`
	S string            `json:"s,omitempty"`
	L []string          `json:"l,omitempty"`
	M []string          `json:"m,omitempty"`
	H map[string]string `json:"h,omitempty"`
	Z []string          `json:"z,omitempty"` // sorted set: member, score, member, score...
	X int64             `json:"x,omitempty"`
	B bool              `json:"b,omitempty"`
}

// Snapshot writes every live key to w in snapshot format. Each shard is
// copied under its read lock and encoded afterwards, so writers are blocked
// only briefly.
func (c *BoltCache) Snapshot(w io.Writer) (int, error) {
	bw := bufio.NewWriterSize(w, 64<<10)
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(snapHeader{Format: snapshotFormat, Version: snapshotVersion, Created: time.Now().UTC()}); err != nil {
		return 0, err
	}
	written := 0
	var batch []snapEntry
	for i := range c.shards {
		s := &c.shards[i]
		ts := now()
		batch = batch[:0]
		s.mu.RLock()
		for k, e := range s.m {
			if e.expired(ts) {
				continue
			}
			batch = append(batch, toSnapEntry(k, e))
		}
		s.mu.RUnlock()
		for j := range batch {
			if err := enc.Encode(encodeBinary(batch[j])); err != nil {
				return written, err
			}
			written++
		}
	}
	return written, bw.Flush()
}

func toSnapEntry(k string, e *entry) snapEntry {
	se := snapEntry{K: k, T: e.kind.String()}
	if e.expireAt != 0 {
		se.X = e.expireAt / int64(time.Millisecond)
		if se.X == 0 {
			se.X = 1
		}
	}
	switch e.kind {
	case KindString:
		se.S = e.str
	case KindList:
		se.L = e.col.list.Values()
	case KindSet:
		se.M = make([]string, 0, len(e.col.set))
		for m := range e.col.set {
			se.M = append(se.M, m)
		}
	case KindHash:
		se.H = make(map[string]string, len(e.col.hash))
		for f, v := range e.col.hash {
			se.H[f] = v
		}
	case KindZSet:
		se.Z = make([]string, 0, 2*len(e.col.zset.dict))
		for m, s := range e.col.zset.dict {
			se.Z = append(se.Z, m, strconv.FormatFloat(s, 'g', -1, 64))
		}
	}
	return se
}

// encodeBinary base64-encodes all strings of se when any of them is not
// valid UTF-8 (JSON would otherwise corrupt them).
func encodeBinary(se snapEntry) snapEntry {
	if allUTF8(se) {
		return se
	}
	b64 := base64.StdEncoding.EncodeToString
	se.B = true
	se.K = b64([]byte(se.K))
	se.S = b64([]byte(se.S))
	for i, v := range se.L {
		se.L[i] = b64([]byte(v))
	}
	for i, v := range se.M {
		se.M[i] = b64([]byte(v))
	}
	for i := 0; i < len(se.Z); i += 2 {
		se.Z[i] = b64([]byte(se.Z[i]))
	}
	if se.H != nil {
		h := make(map[string]string, len(se.H))
		for f, v := range se.H {
			h[b64([]byte(f))] = b64([]byte(v))
		}
		se.H = h
	}
	return se
}

func allUTF8(se snapEntry) bool {
	if !utf8.ValidString(se.K) || !utf8.ValidString(se.S) {
		return false
	}
	for i := 0; i < len(se.Z); i += 2 {
		if !utf8.ValidString(se.Z[i]) {
			return false
		}
	}
	for _, v := range se.L {
		if !utf8.ValidString(v) {
			return false
		}
	}
	for _, v := range se.M {
		if !utf8.ValidString(v) {
			return false
		}
	}
	for f, v := range se.H {
		if !utf8.ValidString(f) || !utf8.ValidString(v) {
			return false
		}
	}
	return true
}

func decodeBinary(se *snapEntry) error {
	if !se.B {
		return nil
	}
	dec := func(s string) (string, error) {
		b, err := base64.StdEncoding.DecodeString(s)
		return string(b), err
	}
	var err error
	if se.K, err = dec(se.K); err != nil {
		return err
	}
	if se.S, err = dec(se.S); err != nil {
		return err
	}
	for i := range se.L {
		if se.L[i], err = dec(se.L[i]); err != nil {
			return err
		}
	}
	for i := range se.M {
		if se.M[i], err = dec(se.M[i]); err != nil {
			return err
		}
	}
	for i := 0; i < len(se.Z); i += 2 {
		if se.Z[i], err = dec(se.Z[i]); err != nil {
			return err
		}
	}
	if se.H != nil {
		h := make(map[string]string, len(se.H))
		for f, v := range se.H {
			df, err := dec(f)
			if err != nil {
				return err
			}
			if h[df], err = dec(v); err != nil {
				return err
			}
		}
		se.H = h
	}
	return nil
}

func fromSnapEntry(se snapEntry) (*entry, error) {
	var e *entry
	switch se.T {
	case "string":
		e = newString(se.K, se.S)
	case "list":
		e = newList(se.K)
		for _, v := range se.L {
			e.col.list.PushBack(v)
			e.col.size += listItemSize(v)
		}
	case "set":
		e = newSet(se.K)
		for _, m := range se.M {
			if _, ok := e.col.set[m]; !ok {
				e.col.set[m] = struct{}{}
				e.col.size += setMemberSize(m)
			}
		}
	case "hash":
		e = newHash(se.K)
		for f, v := range se.H {
			e.col.hash[f] = v
			e.col.size += hashFieldSize(f, v)
		}
	case "zset":
		if len(se.Z)%2 != 0 {
			return nil, fmt.Errorf("sorted set %q has an odd number of fields", se.K)
		}
		e = newZSet(se.K)
		for i := 0; i < len(se.Z); i += 2 {
			score, err := strconv.ParseFloat(se.Z[i+1], 64)
			if err != nil || math.IsNaN(score) {
				return nil, fmt.Errorf("sorted set %q: invalid score %q", se.K, se.Z[i+1])
			}
			e.zsetPut(se.Z[i], score)
		}
	default:
		return nil, fmt.Errorf("unknown type %q for key %q", se.T, se.K)
	}
	if se.X > 0 {
		e.expireAt = se.X * int64(time.Millisecond)
	}
	return e, nil
}

// restore inserts e at key, replacing any existing value. Empty collections
// and already expired entries are skipped.
func (c *BoltCache) restore(key string, e *entry) bool {
	if e.expired(now()) {
		return false
	}
	switch e.kind {
	case KindList:
		if e.col.list.Len() == 0 {
			return false
		}
	case KindSet:
		if len(e.col.set) == 0 {
			return false
		}
	case KindHash:
		if len(e.col.hash) == 0 {
			return false
		}
	case KindZSet:
		if len(e.col.zset.dict) == 0 {
			return false
		}
	}
	s := &c.shards[c.shardIndex(key)]
	s.mu.Lock()
	if old, ok := s.m[key]; ok {
		c.removeLocked(s, key, old)
	}
	s.m[key] = e
	c.account(s, 1, e.memSize(key))
	e.setAccess(now())
	s.mu.Unlock()
	return true
}

// Load reads a snapshot (version 2 or legacy, gzipped or not) from r and
// merges it into the cache. It returns the number of keys loaded.
func (c *BoltCache) Load(r io.Reader) (int, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	if magic, err := br.Peek(2); err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return 0, fmt.Errorf("open gzip snapshot: %w", err)
		}
		defer zr.Close()
		br = bufio.NewReaderSize(zr, 64<<10)
	}

	dec := json.NewDecoder(br)
	var first json.RawMessage
	if err := dec.Decode(&first); err != nil {
		if errors.Is(err, io.EOF) {
			return 0, nil
		}
		return 0, fmt.Errorf("decode snapshot: %w", err)
	}
	var hdr snapHeader
	if err := json.Unmarshal(first, &hdr); err != nil || hdr.Format != snapshotFormat {
		return c.loadLegacy(first)
	}
	if hdr.Version > snapshotVersion {
		return 0, fmt.Errorf("snapshot version %d is newer than supported version %d", hdr.Version, snapshotVersion)
	}

	loaded := 0
	for {
		var se snapEntry
		if err := dec.Decode(&se); err != nil {
			if errors.Is(err, io.EOF) {
				return loaded, nil
			}
			return loaded, fmt.Errorf("decode snapshot entry %d: %w", loaded+1, err)
		}
		if err := decodeBinary(&se); err != nil {
			return loaded, fmt.Errorf("decode snapshot entry %d: %w", loaded+1, err)
		}
		e, err := fromSnapEntry(se)
		if err != nil {
			return loaded, err
		}
		if c.restore(se.K, e) {
			loaded++
		}
	}
}

// loadLegacy converts a BoltCache 1.x snapshot: a JSON object mapping keys
// to {"Value": <any>, "ExpiresAt": <RFC3339>}. Values were stored untyped,
// so types are inferred: strings stay strings, arrays become lists, objects
// whose values are all {} become sets and other objects become hashes.
// Values written as raw bytes by 1.x were base64 encoded by encoding/json
// and cannot be told apart from strings; they load as their base64 text.
func (c *BoltCache) loadLegacy(raw json.RawMessage) (int, error) {
	var items map[string]struct {
		Value     json.RawMessage
		ExpiresAt time.Time
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return 0, fmt.Errorf("unrecognized snapshot format: %w", err)
	}
	loaded := 0
	for k, it := range items {
		e := legacyEntry(k, it.Value)
		if e == nil {
			continue
		}
		if !it.ExpiresAt.IsZero() {
			e.expireAt = unixNanoClamped(it.ExpiresAt)
		}
		if c.restore(k, e) {
			loaded++
		}
	}
	return loaded, nil
}

// UnixNano is t.UnixNano without overflow for dates past 2262.
func UnixNano(t time.Time) int64 { return unixNanoClamped(t) }

// unixNanoClamped is t.UnixNano without overflow for dates past 2262.
func unixNanoClamped(t time.Time) int64 {
	if t.After(time.Unix(0, math.MaxInt64)) {
		return math.MaxInt64
	}
	return t.UnixNano()
}

func legacyEntry(key string, v json.RawMessage) *entry {
	v = bytes.TrimSpace(v)
	if len(v) == 0 || string(v) == "null" {
		return nil
	}
	switch v[0] {
	case '"':
		var s string
		if json.Unmarshal(v, &s) != nil {
			return nil
		}
		return newString(key, s)
	case '[':
		var arr []json.RawMessage
		if json.Unmarshal(v, &arr) != nil {
			return nil
		}
		e := newList(key)
		for _, it := range arr {
			s := legacyScalar(it)
			e.col.list.PushBack(s)
			e.col.size += listItemSize(s)
		}
		return e
	case '{':
		var obj map[string]json.RawMessage
		if json.Unmarshal(v, &obj) != nil {
			return nil
		}
		isSet := len(obj) > 0
		for _, fv := range obj {
			if string(bytes.TrimSpace(fv)) != "{}" {
				isSet = false
				break
			}
		}
		if isSet {
			e := newSet(key)
			for m := range obj {
				e.col.set[m] = struct{}{}
				e.col.size += setMemberSize(m)
			}
			return e
		}
		e := newHash(key)
		for f, fv := range obj {
			s := legacyScalar(fv)
			e.col.hash[f] = s
			e.col.size += hashFieldSize(f, s)
		}
		return e
	default:
		return newString(key, legacyScalar(v))
	}
}

func legacyScalar(v json.RawMessage) string {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	var f float64
	if json.Unmarshal(v, &f) == nil {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return string(bytes.TrimSpace(v))
}

// SaveFile atomically writes a snapshot to path: data goes to a temporary
// file in the same directory which is fsynced and renamed over path, so a
// crash never leaves a truncated snapshot behind.
func (c *BoltCache) SaveFile(path string, compress bool) (int, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return 0, err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename

	var w io.Writer = tmp
	var zw *gzip.Writer
	if compress {
		zw = gzip.NewWriter(tmp)
		w = zw
	}
	n, err := c.Snapshot(w)
	if err == nil && zw != nil {
		err = zw.Close()
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, fmt.Errorf("write snapshot: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return 0, fmt.Errorf("replace snapshot: %w", err)
	}
	syncDir(dir)
	return n, nil
}

// LoadFile loads a snapshot file. A missing file is not an error.
func (c *BoltCache) LoadFile(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()
	return c.Load(f)
}

func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
}
