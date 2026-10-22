package glob

import (
	"strings"
	"testing"
)

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"*", "", true},
		{"*", "anything", true},
		{"user:*", "user:1", true},
		{"user:*", "users:1", false},
		{"h?llo", "hello", true},
		{"h?llo", "hllo", false},
		{"h*llo", "hllo", true},
		{"h*llo", "heeeello", true},
		{"h[ae]llo", "hallo", true},
		{"h[ae]llo", "hillo", false},
		{"h[^e]llo", "hallo", true},
		{"h[^e]llo", "hello", false},
		{"h[a-c]llo", "hbllo", true},
		{"h[a-c]llo", "hdllo", false},
		{`h\*llo`, "h*llo", true},
		{`h\*llo`, "hello", false},
		{"a*b*c", "aXbYc", true},
		{"a*b*c", "aXbY", false},
		{"news.*", "news.tech", true},
		{"", "", true},
		{"", "a", false},
	}
	for _, c := range cases {
		if got := Match(c.pattern, c.s); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.s, got, c.want)
		}
	}
}

func TestPathologicalPatternTerminates(t *testing.T) {
	p := strings.Repeat("*a", 50) + "b"
	s := strings.Repeat("a", 60)
	if Match(p, s) {
		t.Fatal("unexpected match")
	}
}

func FuzzMatch(f *testing.F) {
	f.Add("h*llo", "hello")
	f.Add("[a-", "b")
	f.Add(`\`, `\`)
	f.Fuzz(func(t *testing.T, p, s string) {
		got := Match(p, s) // must not panic
		if IsLiteral(p) && got != (p == s) {
			t.Fatalf("literal %q vs %q", p, s)
		}
	})
}
