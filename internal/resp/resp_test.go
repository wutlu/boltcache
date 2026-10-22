package resp

import (
	"bufio"
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func strs(args [][]byte) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = string(a)
	}
	return out
}

func TestParseMultibulk(t *testing.T) {
	buf := AppendCommand(nil, "SET", "key", "va\r\nlue", "")
	args, n, inline, err := ParseCommand(nil, buf)
	if err != nil || n != len(buf) || inline {
		t.Fatalf("n=%d inline=%v err=%v", n, inline, err)
	}
	if got := strs(args); !reflect.DeepEqual(got, []string{"SET", "key", "va\r\nlue", ""}) {
		t.Fatalf("args = %q", got)
	}
}

func TestParseIncompleteAtEverySplit(t *testing.T) {
	buf := AppendCommand(nil, "HSET", "user:1", "name", "Ada Lovelace")
	buf = append(buf, AppendCommand(nil, "GET", "x")...)
	first := len(AppendCommand(nil, "HSET", "user:1", "name", "Ada Lovelace"))
	for i := 0; i < first; i++ {
		_, n, _, err := ParseCommand(nil, buf[:i])
		if err != nil || n != 0 {
			t.Fatalf("prefix %d: n=%d err=%v", i, n, err)
		}
	}
	args, n, _, err := ParseCommand(nil, buf)
	if err != nil || n != first || len(args) != 4 {
		t.Fatalf("full: n=%d args=%q err=%v", n, strs(args), err)
	}
	args, n2, _, err := ParseCommand(nil, buf[n:])
	if err != nil || n2 != len(buf)-n || !reflect.DeepEqual(strs(args), []string{"GET", "x"}) {
		t.Fatalf("second: %q %v", strs(args), err)
	}
}

func TestParseInline(t *testing.T) {
	cases := map[string][]string{
		"PING\r\n":                      {"PING"},
		"set  k   v\n":                  {"set", "k", "v"},
		`SET k "hello world"` + "\r\n":  {"SET", "k", "hello world"},
		`SET k "a\nb\x41"` + "\n":       {"SET", "k", "a\nbA"},
		`SET k 'it\'s'` + "\n":          {"SET", "k", "it's"},
		`SET k ""` + "\n":               {"SET", "k", ""},
		"\r\n":                          {},
		`EVAL "return 1" 0` + "\n":      {"EVAL", "return 1", "0"},
		"GET\tkey\n":                    {"GET", "key"},
		`ECHO "quote \" inside"` + "\n": {"ECHO", `quote " inside`},
	}
	for in, want := range cases {
		args, n, inline, err := ParseCommand(nil, []byte(in))
		if err != nil || n != len(in) || !inline {
			t.Errorf("%q: n=%d inline=%v err=%v", in, n, inline, err)
			continue
		}
		if got := strs(args); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
	if _, n, _, err := ParseCommand(nil, []byte("PING")); n != 0 || err != nil {
		t.Errorf("incomplete inline: n=%d err=%v", n, err)
	}
	if _, _, _, err := ParseCommand(nil, []byte(`SET k "open`+"\n")); !IsProtocolError(err) {
		t.Errorf("unbalanced quotes: %v", err)
	}
	if _, _, _, err := ParseCommand(nil, bytes.Repeat([]byte("a"), MaxInlineLen+1)); !IsProtocolError(err) {
		t.Errorf("huge inline: %v", err)
	}
}

func TestParseErrors(t *testing.T) {
	for _, in := range []string{
		"*1\r\n:5\r\n",
		"*1\r\n$-5\r\n",
		"*x\r\n",
		"*1\r\n$3\r\nabcXY",
		"*2000000\r\n",
		"*1\r\n$" + strings.Repeat("9", 40),
	} {
		if _, _, _, err := ParseCommand(nil, []byte(in)); !IsProtocolError(err) {
			t.Errorf("%q: want protocol error, got %v", in, err)
		}
	}
	// Empty multibulk is consumed silently.
	if args, n, _, err := ParseCommand(nil, []byte("*0\r\n")); err != nil || n != 4 || len(args) != 0 {
		t.Errorf("*0: %v %d %v", args, n, err)
	}
}

func TestEncoderAndReadValue(t *testing.T) {
	var e Encoder
	e.Array(6)
	e.Simple("OK")
	e.Error("WRONGTYPE bad")
	e.Error("something failed\nbadly")
	e.Int(-42)
	e.Bulk("bin\x00ary")
	e.Array(2)
	e.Null()
	e.NullArray()

	r := bufio.NewReader(bytes.NewReader(e.Buf))
	v, err := ReadValue(r)
	if err != nil {
		t.Fatal(err)
	}
	want := Value{Kind: KindArray, Array: []Value{
		{Kind: KindSimple, Str: "OK"},
		{Kind: KindError, Str: "WRONGTYPE bad"},
		{Kind: KindError, Str: "ERR something failed badly"},
		{Kind: KindInt, Int: -42},
		{Kind: KindBulk, Str: "bin\x00ary"},
		{Kind: KindArray, Array: []Value{{Kind: KindNull}, {Kind: KindNullArray}}},
	}}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("got %+v", v)
	}

	// Recorder builds the same tree.
	var rec Recorder
	want.Write(&rec)
	if !reflect.DeepEqual(rec.Result(), want) {
		t.Fatalf("recorder = %+v", rec.Result())
	}
}

func FuzzParseCommand(f *testing.F) {
	f.Add([]byte("*2\r\n$3\r\nGET\r\n$1\r\nk\r\n"))
	f.Add([]byte("SET k \"v\\x00\"\r\n"))
	f.Add([]byte("*-1\r\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		args, n, _, err := ParseCommand(nil, data)
		if err != nil {
			return
		}
		if n < 0 || n > len(data) {
			t.Fatalf("consumed %d of %d", n, len(data))
		}
		if n == 0 || len(args) == 0 {
			return
		}
		// Round trip: re-encoding the parsed args must parse to the same args.
		enc := AppendCommand(nil, strs(args)...)
		again, m, _, err := ParseCommand(nil, enc)
		if err != nil || m != len(enc) || !reflect.DeepEqual(strs(again), strs(args)) {
			t.Fatalf("round trip mismatch: %q vs %q (%v)", strs(args), strs(again), err)
		}
	})
}

func TestParserIncremental(t *testing.T) {
	big := strings.Repeat("x", 1<<20)
	cmd := AppendCommand(nil, "SET", "k", big)
	cmd = AppendCommand(cmd, "GET", "k")
	var p Parser
	var buf []byte
	var got [][]string
	for i := 0; i < len(cmd); i += 1000 {
		buf = append(buf, cmd[i:min(i+1000, len(cmd))]...)
		for {
			if need := p.Need(); need > len(buf) {
				break // nothing to do until enough bytes arrive
			}
			args, n, _, err := p.Parse(nil, buf)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				break
			}
			got = append(got, strs(args))
			buf = buf[n:]
		}
	}
	if len(got) != 2 || len(got[0][2]) != 1<<20 || got[1][0] != "GET" {
		t.Fatalf("parsed %d commands", len(got))
	}

	// Limits for unauthenticated clients.
	p = Parser{MaxArgs: 10, MaxBulk: 16}
	if _, _, _, err := p.Parse(nil, []byte("*11\r\n")); !IsProtocolError(err) {
		t.Fatalf("too many args: %v", err)
	}
	if _, _, _, err := p.Parse(nil, []byte("*1\r\n$17\r\n")); !IsProtocolError(err) {
		t.Fatalf("bulk too big: %v", err)
	}
	if args, _, _, err := p.Parse(nil, AppendCommand(nil, "AUTH", "token")); err != nil || len(args) != 2 {
		t.Fatalf("small command rejected: %v", err)
	}
}
