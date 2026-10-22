package command

import (
	"math"
	"strconv"
	"strings"

	"github.com/wutlu/boltcache/v2/internal/resp"
)

const (
	errSyntax      = "ERR syntax error"
	errNotInt      = "ERR value is not an integer or out of range"
	errNotFloat    = "ERR value is not a valid float"
	errInvalidTime = "ERR invalid expire time in '%s' command"
)

func parseInt(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}

func parseFloat(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(f) {
		return 0, false
	}
	return f, true
}

func writeStrings(w resp.Writer, vals []string) {
	w.Array(len(vals))
	for _, v := range vals {
		w.Bulk(v)
	}
}

func writeOptional(w resp.Writer, v string, ok bool) {
	if ok {
		w.Bulk(v)
	} else {
		w.Null()
	}
}

func writeBool(w resp.Writer, b bool) {
	if b {
		w.Int(1)
	} else {
		w.Int(0)
	}
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func eqFold(a, b string) bool { return strings.EqualFold(a, b) }
