// Package resp implements the Redis Serialization Protocol (RESP2): an
// incremental, zero-copy command parser that also accepts inline commands
// (what you type in telnet), and reply writers.
package resp

import (
	"bytes"
	"errors"
	"strconv"
)

const (
	// MaxBulkLen is the largest accepted bulk string (same as Redis).
	MaxBulkLen = 512 << 20
	// MaxArgs bounds the number of arguments in one command.
	MaxArgs = 1 << 20
	// MaxInlineLen bounds inline command lines.
	MaxInlineLen = 64 << 10
)

// ProtocolError is returned for malformed input. The connection should be
// closed after reporting it.
type ProtocolError struct{ msg string }

func (e *ProtocolError) Error() string { return "ERR Protocol error: " + e.msg }

func protoErr(msg string) error { return &ProtocolError{msg} }

// IsProtocolError reports whether err is a *ProtocolError.
func IsProtocolError(err error) bool {
	var pe *ProtocolError
	return errors.As(err, &pe)
}

// ParseCommand parses one command from the start of buf. The returned
// arguments alias buf (they're valid until buf is modified) and are appended
// to dst. n is the number of bytes consumed; n == 0 with a nil error means
// more data is needed. inline reports whether the command used the inline
// format. Empty lines and empty arrays are consumed and return no args.
//
// ParseCommand is stateless: it re-parses a partial command from the start
// on every call. Network servers should use a Parser instead.
func ParseCommand(dst [][]byte, buf []byte) (args [][]byte, n int, inline bool, err error) {
	var p Parser
	return p.Parse(dst, buf)
}

// Parser parses commands incrementally. While a multibulk command is only
// partially received it remembers the arguments already parsed and how many
// bytes it needs next, so a large command arriving in many small reads is
// parsed in linear time.
//
// Between calls the caller must keep the unconsumed bytes in place: buf has
// to start at the same command and only grow by appending.
type Parser struct {
	// MaxArgs and MaxBulk limit the next command (0 = package defaults).
	// Servers lower them for unauthenticated clients, like Redis does.
	MaxArgs int
	MaxBulk int

	active bool // inside a multibulk command
	count  int  // number of arguments of the current command
	pos    int  // offset of the next unparsed argument
	spans  []span
	need   int // total bytes required before parsing can progress (0 = unknown)
}

type span struct{ start, end int }

// Need returns how many bytes (counted from the start of the pending
// command) must be buffered before Parse can make progress, or 0 if unknown.
func (p *Parser) Need() int { return p.need }

// Reset discards any partial command.
func (p *Parser) Reset() {
	p.active, p.count, p.pos, p.need = false, 0, 0, 0
	p.spans = p.spans[:0]
}

func (p *Parser) fail(dst [][]byte, err error) ([][]byte, int, bool, error) {
	p.Reset()
	return dst, 0, false, err
}

// Parse works like ParseCommand but resumes partially parsed commands.
func (p *Parser) Parse(dst [][]byte, buf []byte) (args [][]byte, n int, inline bool, err error) {
	maxArgs, maxBulk := p.MaxArgs, p.MaxBulk
	if maxArgs <= 0 {
		maxArgs = MaxArgs
	}
	if maxBulk <= 0 {
		maxBulk = MaxBulkLen
	}
	if !p.active {
		p.need = 0
		if len(buf) == 0 {
			return dst, 0, false, nil
		}
		if buf[0] != '*' {
			return parseInline(dst, buf)
		}
		count, pos, err := parseLength(buf, 1)
		if err != nil {
			return p.fail(dst, err)
		}
		if pos == 0 {
			return dst, 0, false, nil
		}
		if count <= 0 {
			return dst, pos, false, nil
		}
		if count > maxArgs {
			return p.fail(dst, protoErr("invalid multibulk length"))
		}
		p.active, p.count, p.pos = true, count, pos
		p.spans = p.spans[:0]
	}
	for len(p.spans) < p.count {
		pos := p.pos
		if pos >= len(buf) {
			p.need = pos + 1
			return dst, 0, false, nil
		}
		if buf[pos] != '$' {
			return p.fail(dst, protoErr("expected '$', got '"+string(buf[pos])+"'"))
		}
		size, next, err := parseLength(buf, pos+1)
		if err != nil {
			return p.fail(dst, err)
		}
		if next == 0 {
			p.need = 0
			return dst, 0, false, nil
		}
		if size < 0 || size > maxBulk {
			return p.fail(dst, protoErr("invalid bulk length"))
		}
		end := next + size
		if end+2 > len(buf) {
			p.need = end + 2
			return dst, 0, false, nil
		}
		if buf[end] != '\r' || buf[end+1] != '\n' {
			return p.fail(dst, protoErr("bulk string not terminated by CRLF"))
		}
		p.spans = append(p.spans, span{next, end})
		p.pos = end + 2
	}
	args = dst
	for _, sp := range p.spans {
		args = append(args, buf[sp.start:sp.end:sp.end])
	}
	n = p.pos
	p.Reset()
	return args, n, false, nil
}

// parseLength parses "<int>\r\n" starting at buf[start]. pos is the index
// after the CRLF, or 0 when the line is incomplete.
func parseLength(buf []byte, start int) (v, pos int, err error) {
	// Hand-rolled: lengths are short decimal numbers, and this runs for
	// every argument of every command.
	i := start
	neg := false
	if i < len(buf) && buf[i] == '-' {
		neg = true
		i++
	}
	digits := 0
	for ; i < len(buf); i++ {
		c := buf[i]
		if c < '0' || c > '9' {
			break
		}
		if digits == 18 {
			return 0, 0, protoErr("invalid length")
		}
		v = v*10 + int(c-'0')
		digits++
	}
	if i+1 >= len(buf) { // need "\r\n"
		if len(buf)-start > 32 {
			return 0, 0, protoErr("length line too long")
		}
		return 0, 0, nil
	}
	if digits == 0 || buf[i] != '\r' || buf[i+1] != '\n' {
		return 0, 0, protoErr("invalid length")
	}
	if neg {
		v = -v
	}
	return v, i + 2, nil
}

func parseInline(dst [][]byte, buf []byte) ([][]byte, int, bool, error) {
	i := bytes.IndexByte(buf, '\n')
	if i < 0 {
		if len(buf) > MaxInlineLen {
			return dst, 0, true, protoErr("too big inline request")
		}
		return dst, 0, true, nil
	}
	line := buf[:i]
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	args, err := splitInline(dst, line)
	if err != nil {
		return dst, 0, true, err
	}
	return args, i + 1, true, nil
}

// splitInline splits an inline command into arguments, honouring "double"
// (with \n, \t, \xHH... escapes) and 'single' quotes like redis-cli.
func splitInline(dst [][]byte, line []byte) ([][]byte, error) {
	args := dst
	i := 0
	for {
		for i < len(line) && isSpace(line[i]) {
			i++
		}
		if i >= len(line) {
			return args, nil
		}
		switch line[i] {
		case '"', '\'':
			quote := line[i]
			i++
			var arg []byte
			closed := false
			for i < len(line) {
				c := line[i]
				if quote == '"' && c == '\\' && i+1 < len(line) {
					i++
					switch e := line[i]; e {
					case 'n':
						arg = append(arg, '\n')
					case 'r':
						arg = append(arg, '\r')
					case 't':
						arg = append(arg, '\t')
					case 'b':
						arg = append(arg, '\b')
					case 'a':
						arg = append(arg, '\a')
					case 'x':
						if i+2 < len(line) {
							if v, err := strconv.ParseUint(string(line[i+1:i+3]), 16, 8); err == nil {
								arg = append(arg, byte(v))
								i += 2
								break
							}
						}
						arg = append(arg, 'x')
					default:
						arg = append(arg, e)
					}
					i++
					continue
				}
				if quote == '\'' && c == '\\' && i+1 < len(line) && line[i+1] == '\'' {
					arg = append(arg, '\'')
					i += 2
					continue
				}
				if c == quote {
					closed = true
					i++
					break
				}
				arg = append(arg, c)
				i++
			}
			if !closed || (i < len(line) && !isSpace(line[i])) {
				return dst, protoErr("unbalanced quotes in request")
			}
			if arg == nil {
				arg = []byte{}
			}
			args = append(args, arg)
		default:
			start := i
			for i < len(line) && !isSpace(line[i]) {
				i++
			}
			args = append(args, line[start:i:i])
		}
	}
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

// Writer receives a command reply. Arrays are written as Array(n) followed
// by exactly n elements (which may themselves be arrays).
type Writer interface {
	Simple(s string)
	Error(msg string)
	Int(n int64)
	Bulk(s string)
	BulkBytes(b []byte)
	Null()
	Array(n int)
	NullArray()
}

// Encoder appends RESP2 encoded replies to Buf.
type Encoder struct{ Buf []byte }

var _ Writer = (*Encoder)(nil)

func (e *Encoder) Simple(s string) {
	e.Buf = append(e.Buf, '+')
	e.Buf = append(e.Buf, s...)
	e.Buf = append(e.Buf, '\r', '\n')
}

// Error writes an error reply. Messages without an upper-case error code
// prefix get "ERR " prepended, and line breaks are replaced so the reply
// stays a single line.
func (e *Encoder) Error(msg string) {
	e.Buf = append(e.Buf, '-')
	if !hasErrorCode(msg) {
		e.Buf = append(e.Buf, "ERR "...)
	}
	for i := 0; i < len(msg); i++ {
		c := msg[i]
		if c == '\r' || c == '\n' {
			c = ' '
		}
		e.Buf = append(e.Buf, c)
	}
	e.Buf = append(e.Buf, '\r', '\n')
}

// hasErrorCode reports whether msg starts with an all-caps word such as
// ERR, WRONGTYPE or NOAUTH.
func hasErrorCode(msg string) bool {
	i := 0
	for i < len(msg) && msg[i] >= 'A' && msg[i] <= 'Z' {
		i++
	}
	return i >= 2 && (i == len(msg) || msg[i] == ' ')
}

func (e *Encoder) Int(n int64) {
	e.Buf = append(e.Buf, ':')
	e.Buf = strconv.AppendInt(e.Buf, n, 10)
	e.Buf = append(e.Buf, '\r', '\n')
}

func (e *Encoder) Bulk(s string) {
	e.Buf = append(e.Buf, '$')
	e.Buf = strconv.AppendInt(e.Buf, int64(len(s)), 10)
	e.Buf = append(e.Buf, '\r', '\n')
	e.Buf = append(e.Buf, s...)
	e.Buf = append(e.Buf, '\r', '\n')
}

func (e *Encoder) BulkBytes(b []byte) {
	e.Buf = append(e.Buf, '$')
	e.Buf = strconv.AppendInt(e.Buf, int64(len(b)), 10)
	e.Buf = append(e.Buf, '\r', '\n')
	e.Buf = append(e.Buf, b...)
	e.Buf = append(e.Buf, '\r', '\n')
}

func (e *Encoder) Null()      { e.Buf = append(e.Buf, "$-1\r\n"...) }
func (e *Encoder) NullArray() { e.Buf = append(e.Buf, "*-1\r\n"...) }

func (e *Encoder) Array(n int) {
	e.Buf = append(e.Buf, '*')
	e.Buf = strconv.AppendInt(e.Buf, int64(n), 10)
	e.Buf = append(e.Buf, '\r', '\n')
}

// AppendCommand encodes args as a RESP array of bulk strings.
func AppendCommand(b []byte, args ...string) []byte {
	e := Encoder{Buf: b}
	e.Array(len(args))
	for _, a := range args {
		e.Bulk(a)
	}
	return e.Buf
}
