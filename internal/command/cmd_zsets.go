package command

import (
	"math"
	"strconv"
	"strings"

	"github.com/wutlu/boltcache/v2/internal/cache"
	"github.com/wutlu/boltcache/v2/internal/glob"
	"github.com/wutlu/boltcache/v2/internal/resp"
)

func registerZSetCommands() {
	register("zadd", cmdZAdd, -4, flagWrite|flagFast, 1, 1, 1)
	register("zincrby", cmdZIncrBy, 4, flagWrite|flagFast, 1, 1, 1)
	register("zscore", cmdZScore, 3, flagReadOnly|flagFast, 1, 1, 1)
	register("zmscore", cmdZMScore, -3, flagReadOnly|flagFast, 1, 1, 1)
	register("zcard", cmdZCard, 2, flagReadOnly|flagFast, 1, 1, 1)
	register("zcount", cmdZCount, 4, flagReadOnly|flagFast, 1, 1, 1)
	register("zrank", cmdZRank(false), -3, flagReadOnly|flagFast, 1, 1, 1)
	register("zrevrank", cmdZRank(true), -3, flagReadOnly|flagFast, 1, 1, 1)
	register("zrem", cmdZRem, -3, flagWrite|flagFast, 1, 1, 1)
	register("zrange", cmdZRange, -4, flagReadOnly, 1, 1, 1)
	register("zrevrange", cmdZRangeCompat(false, true), -4, flagReadOnly, 1, 1, 1)
	register("zrangebyscore", cmdZRangeCompat(true, false), -4, flagReadOnly, 1, 1, 1)
	register("zrevrangebyscore", cmdZRangeCompat(true, true), -4, flagReadOnly, 1, 1, 1)
	register("zpopmin", cmdZPop(false), -2, flagWrite|flagFast, 1, 1, 1)
	register("zpopmax", cmdZPop(true), -2, flagWrite|flagFast, 1, 1, 1)
	register("zremrangebyrank", cmdZRemRangeByRank, 4, flagWrite, 1, 1, 1)
	register("zremrangebyscore", cmdZRemRangeByScore, 4, flagWrite, 1, 1, 1)
	register("zscan", cmdZScan, -3, flagReadOnly, 1, 1, 1)
}

// parseScore accepts floats including inf, +inf and -inf, but not NaN.
func parseScore(s string) (float64, bool) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) {
		return 0, false
	}
	return f, true
}

// parseBound parses a ZRANGEBYSCORE bound: "1.5", "(1.5" (exclusive),
// "-inf", "+inf".
func parseBound(s string) (cache.ScoreBound, bool) {
	b := cache.ScoreBound{}
	if strings.HasPrefix(s, "(") {
		b.Exclusive = true
		s = s[1:]
	}
	f, ok := parseScore(s)
	b.Value = f
	return b, ok
}

// formatScore renders scores the way Redis does: integers without a
// decimal point, "inf"/"-inf" for infinities.
func formatScore(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case f == math.Trunc(f) && math.Abs(f) < 1e17:
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', 17, 64)
}

const errMinMaxFloat = "ERR min or max is not a float"

func writeZMembers(w resp.Writer, ms []cache.ZMember, withScores bool) {
	if withScores {
		w.Array(2 * len(ms))
	} else {
		w.Array(len(ms))
	}
	for _, m := range ms {
		w.Bulk(m.Member)
		if withScores {
			w.Bulk(formatScore(m.Score))
		}
	}
}

func cmdZAdd(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	var opts cache.ZAddOptions
	ch := false
	i := 2
	for ; i < len(args); i++ {
		switch upper(args[i]) {
		case "NX":
			opts.NX = true
		case "XX":
			opts.XX = true
		case "GT":
			opts.GT = true
		case "LT":
			opts.LT = true
		case "CH":
			ch = true
		case "INCR":
			opts.Incr = true
		default:
			goto pairs
		}
	}
pairs:
	rest := args[i:]
	if len(rest) == 0 || len(rest)%2 != 0 {
		w.Error(errSyntax)
		return
	}
	if opts.NX && opts.XX {
		w.Error("ERR XX and NX options at the same time are not compatible")
		return
	}
	if (opts.GT && opts.LT) || (opts.NX && (opts.GT || opts.LT)) {
		w.Error("ERR GT, LT, and/or NX options at the same time are not compatible")
		return
	}
	members := make([]cache.ZMember, 0, len(rest)/2)
	for j := 0; j < len(rest); j += 2 {
		score, ok := parseScore(rest[j])
		if !ok {
			w.Error(errNotFloat)
			return
		}
		members = append(members, cache.ZMember{Member: rest[j+1], Score: score})
	}
	res, err := db.ZAdd(args[1], opts, members)
	if err != nil {
		writeErr(w, err)
		return
	}
	switch {
	case opts.Incr && !res.Applied:
		w.Null()
	case opts.Incr:
		w.Bulk(formatScore(res.Score))
	case ch:
		w.Int(int64(res.Changed))
	default:
		w.Int(int64(res.Added))
	}
}

func cmdZIncrBy(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	d, ok := parseScore(args[2])
	if !ok {
		w.Error(errNotFloat)
		return
	}
	res, err := db.ZAdd(args[1], cache.ZAddOptions{Incr: true}, []cache.ZMember{{Member: args[3], Score: d}})
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Bulk(formatScore(res.Score))
}

func cmdZScore(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	s, ok, err := db.ZScore(args[1], args[2])
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOptional(w, formatScore(s), ok)
}

func cmdZMScore(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	scores, oks, err := db.ZMScore(args[1], args[2:]...)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Array(len(scores))
	for i, s := range scores {
		writeOptional(w, formatScore(s), oks[i])
	}
}

func cmdZCard(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	n, err := db.ZCard(args[1])
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Int(int64(n))
}

func cmdZCount(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	min, ok1 := parseBound(args[2])
	max, ok2 := parseBound(args[3])
	if !ok1 || !ok2 {
		w.Error(errMinMaxFloat)
		return
	}
	n, err := db.ZCount(args[1], min, max)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Int(int64(n))
}

func cmdZRank(rev bool) handler {
	return func(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
		withScore := false
		switch {
		case len(args) == 4 && eqFold(args[3], "WITHSCORE"):
			withScore = true
		case len(args) != 3:
			w.Error(errSyntax)
			return
		}
		rank, score, ok, err := db.ZRank(args[1], args[2], rev)
		if err != nil {
			writeErr(w, err)
			return
		}
		switch {
		case !ok && withScore:
			w.NullArray()
		case !ok:
			w.Null()
		case withScore:
			w.Array(2)
			w.Int(int64(rank))
			w.Bulk(formatScore(score))
		default:
			w.Int(int64(rank))
		}
	}
}

func cmdZRem(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	n, err := db.ZRem(args[1], args[2:]...)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Int(int64(n))
}

// zrangeQuery is a parsed ZRANGE request.
type zrangeQuery struct {
	key           string
	start, stop   string
	byScore, rev  bool
	withScores    bool
	limit         bool
	offset, count int
}

func (q zrangeQuery) run(db *cache.Tx, w resp.Writer) {
	var (
		ms  []cache.ZMember
		err error
	)
	if q.byScore {
		// With REV the arguments are given as max, min.
		lo, hi := q.start, q.stop
		if q.rev {
			lo, hi = hi, lo
		}
		min, ok1 := parseBound(lo)
		max, ok2 := parseBound(hi)
		if !ok1 || !ok2 {
			w.Error(errMinMaxFloat)
			return
		}
		count := -1
		if q.limit {
			count = q.count
		}
		ms, err = db.ZRangeByScore(q.key, min, max, q.rev, max0(q.offset), count)
		if q.limit && q.offset < 0 {
			ms = []cache.ZMember{}
		}
	} else {
		start, ok1 := parseInt(q.start)
		stop, ok2 := parseInt(q.stop)
		if !ok1 || !ok2 {
			w.Error(errNotInt)
			return
		}
		ms, err = db.ZRangeByRank(q.key, clampInt(start), clampInt(stop), q.rev)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	writeZMembers(w, ms, q.withScores)
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// parseZRangeOptions parses WITHSCORES and LIMIT (plus BYSCORE/BYLEX/REV
// when full is set, for ZRANGE).
func parseZRangeOptions(q *zrangeQuery, opts []string, full bool) string {
	for i := 0; i < len(opts); i++ {
		switch o := upper(opts[i]); {
		case o == "WITHSCORES":
			q.withScores = true
		case o == "LIMIT" && i+2 < len(opts):
			off, ok1 := parseInt(opts[i+1])
			cnt, ok2 := parseInt(opts[i+2])
			if !ok1 || !ok2 {
				return errNotInt
			}
			q.limit, q.offset, q.count = true, clampInt(off), clampInt(cnt)
			i += 2
		case full && o == "BYSCORE":
			q.byScore = true
		case full && o == "REV":
			q.rev = true
		case full && o == "BYLEX":
			return "ERR BYLEX is not supported yet"
		default:
			return errSyntax
		}
	}
	if q.limit && !q.byScore {
		return "ERR syntax error, LIMIT is only supported in combination with either BYSCORE or BYLEX"
	}
	return ""
}

func cmdZRange(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	q := zrangeQuery{key: args[1], start: args[2], stop: args[3]}
	if msg := parseZRangeOptions(&q, args[4:], true); msg != "" {
		w.Error(msg)
		return
	}
	q.run(db, w)
}

// cmdZRangeCompat implements ZREVRANGE, ZRANGEBYSCORE and ZREVRANGEBYSCORE.
func cmdZRangeCompat(byScore, rev bool) handler {
	return func(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
		q := zrangeQuery{key: args[1], start: args[2], stop: args[3], byScore: byScore, rev: rev}
		if msg := parseZRangeOptions(&q, args[4:], false); msg != "" {
			w.Error(msg)
			return
		}
		q.run(db, w)
	}
}

func cmdZPop(max bool) handler {
	return func(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
		if len(args) > 3 {
			w.Error(errSyntax)
			return
		}
		count := 1
		if len(args) == 3 {
			n, ok := parseInt(args[2])
			if !ok || n < 0 {
				w.Error("ERR value is out of range, must be positive")
				return
			}
			count = int(min(n, 1<<30))
		}
		ms, err := db.ZPop(args[1], count, max)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeZMembers(w, ms, true)
	}
}

func cmdZRemRangeByRank(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	start, ok1 := parseInt(args[2])
	stop, ok2 := parseInt(args[3])
	if !ok1 || !ok2 {
		w.Error(errNotInt)
		return
	}
	n, err := db.ZRemRangeByRank(args[1], clampInt(start), clampInt(stop))
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Int(int64(n))
}

func cmdZRemRangeByScore(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	min, ok1 := parseBound(args[2])
	max, ok2 := parseBound(args[3])
	if !ok1 || !ok2 {
		w.Error(errMinMaxFloat)
		return
	}
	n, err := db.ZRemRangeByScore(args[1], min, max)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Int(int64(n))
}

func cmdZScan(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	if _, err := strconv.ParseUint(args[2], 10, 64); err != nil {
		w.Error("ERR invalid cursor")
		return
	}
	pattern, _, _, perr := scanOptions(args[3:], false)
	if perr != "" {
		w.Error(perr)
		return
	}
	ms, err := db.ZMembers(args[1])
	if err != nil {
		writeErr(w, err)
		return
	}
	out := ms[:0]
	for _, m := range ms {
		if pattern == "" || glob.Match(pattern, m.Member) {
			out = append(out, m)
		}
	}
	w.Array(2)
	w.Bulk("0")
	writeZMembers(w, out, true)
}
