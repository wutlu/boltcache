// Package glob implements Redis-style glob matching used by KEYS, SCAN MATCH
// and PSUBSCRIBE.
//
// Supported syntax:
//
//   - matches any sequence of characters (including none)
//     ?      matches exactly one character
//     [abc]  matches one character from the set
//     [^ab]  matches one character not in the set
//     [a-z]  matches one character in the range
//     \x     matches x literally
package glob

// Match reports whether s matches the glob pattern. It runs in O(len(pattern)
// * len(s)) time: on a mismatch it only backtracks to the most recent '*',
// so hostile patterns like "*a*a*a*...b" cannot cause exponential work.
func Match(pattern, s string) bool {
	p, i := 0, 0
	starP, starI := -1, 0
	for i < len(s) {
		if p < len(pattern) {
			switch pattern[p] {
			case '*':
				starP, starI = p, i
				p++
				continue
			case '?':
				p++
				i++
				continue
			default:
				if next, ok := matchOne(pattern, p, s[i]); ok {
					p = next
					i++
					continue
				}
			}
		}
		if starP < 0 {
			return false
		}
		// Let the last '*' absorb one more character and retry.
		starI++
		p, i = starP+1, starI
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}

// matchOne matches the single-character token at pattern[p] (a literal, an
// escaped character or a class) against c, returning the index after it.
func matchOne(pattern string, p int, c byte) (int, bool) {
	switch pattern[p] {
	case '[':
		rest, ok := matchClass(pattern[p+1:], c)
		return len(pattern) - len(rest), ok
	case '\\':
		if p+1 < len(pattern) {
			p++
		}
	}
	return p + 1, pattern[p] == c
}

// matchClass matches c against the character class at the start of p (just
// after the opening '['). It returns the pattern remaining after the closing
// ']' and whether c matched.
func matchClass(p string, c byte) (string, bool) {
	negate := false
	if len(p) > 0 && p[0] == '^' {
		negate = true
		p = p[1:]
	}
	matched := false
	for len(p) > 0 && p[0] != ']' {
		switch {
		case p[0] == '\\' && len(p) >= 2:
			if p[1] == c {
				matched = true
			}
			p = p[2:]
		case len(p) >= 3 && p[1] == '-' && p[2] != ']':
			lo, hi := p[0], p[2]
			if lo > hi {
				lo, hi = hi, lo
			}
			if c >= lo && c <= hi {
				matched = true
			}
			p = p[3:]
		default:
			if p[0] == c {
				matched = true
			}
			p = p[1:]
		}
	}
	if len(p) > 0 { // skip ']'
		p = p[1:]
	}
	return p, matched != negate
}

// IsLiteral reports whether pattern contains no glob metacharacters, in which
// case callers can do an exact lookup instead of a scan.
func IsLiteral(pattern string) bool {
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*', '?', '[', '\\':
			return false
		}
	}
	return true
}
