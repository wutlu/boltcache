package resp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// Kind identifies the type of a reply Value.
type Kind uint8

const (
	KindSimple Kind = iota
	KindError
	KindInt
	KindBulk
	KindNull
	KindArray
	KindNullArray
)

// Value is a decoded reply. It's used where a reply must be inspected rather
// than streamed: Lua scripts, the REST /command endpoint, the legacy text
// protocol and the client.
type Value struct {
	Kind  Kind
	Str   string // Simple, Error, Bulk
	Int   int64
	Array []Value
}

// IsNil reports whether v is a null bulk string or null array.
func (v Value) IsNil() bool { return v.Kind == KindNull || v.Kind == KindNullArray }

// Interface converts v into plain Go values for JSON encoding: strings,
// int64, nil and []any. Errors become map{"error": msg}.
func (v Value) Interface() any {
	switch v.Kind {
	case KindSimple, KindBulk:
		return v.Str
	case KindError:
		return map[string]string{"error": v.Str}
	case KindInt:
		return v.Int
	case KindArray:
		out := make([]any, len(v.Array))
		for i, e := range v.Array {
			out[i] = e.Interface()
		}
		return out
	}
	return nil
}

// Write replays v into w.
func (v Value) Write(w Writer) {
	switch v.Kind {
	case KindSimple:
		w.Simple(v.Str)
	case KindError:
		w.Error(v.Str)
	case KindInt:
		w.Int(v.Int)
	case KindBulk:
		w.Bulk(v.Str)
	case KindNull:
		w.Null()
	case KindNullArray:
		w.NullArray()
	case KindArray:
		w.Array(len(v.Array))
		for _, e := range v.Array {
			e.Write(w)
		}
	}
}

// Recorder is a Writer that builds Values. After a complete reply has been
// written, Result returns it.
type Recorder struct {
	root  []Value
	stack []frame
}

type frame struct {
	vals []Value
	want int
}

var _ Writer = (*Recorder)(nil)

func (r *Recorder) push(v Value) {
	for {
		if len(r.stack) == 0 {
			r.root = append(r.root, v)
			return
		}
		top := &r.stack[len(r.stack)-1]
		top.vals = append(top.vals, v)
		if len(top.vals) < top.want {
			return
		}
		v = Value{Kind: KindArray, Array: top.vals}
		r.stack = r.stack[:len(r.stack)-1]
	}
}

func (r *Recorder) Simple(s string)    { r.push(Value{Kind: KindSimple, Str: s}) }
func (r *Recorder) Error(msg string)   { r.push(Value{Kind: KindError, Str: msg}) }
func (r *Recorder) Int(n int64)        { r.push(Value{Kind: KindInt, Int: n}) }
func (r *Recorder) Bulk(s string)      { r.push(Value{Kind: KindBulk, Str: s}) }
func (r *Recorder) BulkBytes(b []byte) { r.push(Value{Kind: KindBulk, Str: string(b)}) }
func (r *Recorder) Null()              { r.push(Value{Kind: KindNull}) }
func (r *Recorder) NullArray()         { r.push(Value{Kind: KindNullArray}) }

func (r *Recorder) Array(n int) {
	if n <= 0 {
		r.push(Value{Kind: KindArray, Array: []Value{}})
		return
	}
	r.stack = append(r.stack, frame{vals: make([]Value, 0, n), want: n})
}

// Result returns the first complete reply written.
func (r *Recorder) Result() Value {
	if len(r.root) == 0 {
		return Value{Kind: KindNull}
	}
	return r.root[0]
}

// Results returns all complete replies written so far.
func (r *Recorder) Results() []Value { return r.root }

// Reset clears recorded replies.
func (r *Recorder) Reset() {
	r.root = r.root[:0]
	r.stack = r.stack[:0]
}

// ReadValue reads one RESP2 reply from r (used by the client and tests).
func ReadValue(r *bufio.Reader) (Value, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return Value{}, err
	}
	if len(line) < 3 || line[len(line)-2] != '\r' {
		return Value{}, errors.New("resp: malformed reply line")
	}
	body := line[1 : len(line)-2]
	switch line[0] {
	case '+':
		return Value{Kind: KindSimple, Str: body}, nil
	case '-':
		return Value{Kind: KindError, Str: body}, nil
	case ':':
		n, err := strconv.ParseInt(body, 10, 64)
		return Value{Kind: KindInt, Int: n}, err
	case '$':
		n, err := strconv.Atoi(body)
		if err != nil {
			return Value{}, err
		}
		if n < 0 {
			return Value{Kind: KindNull}, nil
		}
		buf := make([]byte, n+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return Value{}, err
		}
		return Value{Kind: KindBulk, Str: string(buf[:n])}, nil
	case '*':
		n, err := strconv.Atoi(body)
		if err != nil {
			return Value{}, err
		}
		if n < 0 {
			return Value{Kind: KindNullArray}, nil
		}
		arr := make([]Value, n)
		for i := range arr {
			if arr[i], err = ReadValue(r); err != nil {
				return Value{}, err
			}
		}
		return Value{Kind: KindArray, Array: arr}, nil
	}
	return Value{}, fmt.Errorf("resp: unknown reply type %q", line[0])
}

// String renders v the way redis-cli does.
func (v Value) String() string { return v.format("") }

func (v Value) format(indent string) string {
	switch v.Kind {
	case KindSimple:
		return v.Str
	case KindError:
		return "(error) " + v.Str
	case KindInt:
		return "(integer) " + strconv.FormatInt(v.Int, 10)
	case KindBulk:
		return strconv.Quote(v.Str)
	case KindNull, KindNullArray:
		return "(nil)"
	case KindArray:
		if len(v.Array) == 0 {
			return "(empty array)"
		}
		s := ""
		width := len(strconv.Itoa(len(v.Array)))
		for i, e := range v.Array {
			if i > 0 {
				s += "\n" + indent
			}
			prefix := fmt.Sprintf("%*d) ", width, i+1)
			s += prefix + e.format(indent+fmt.Sprintf("%*s", len(prefix), ""))
		}
		return s
	}
	return ""
}
