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
func ParseCommand(dst [][]byte, buf []byte) (args [][]byte, n int, inline bool, err error) {
	if len(buf) == 0 {
		return dst, 0, false, nil
	}
	if buf[0] != '*' {
		return parseInline(dst, buf)
	}
	count, pos, err := parseLength(buf, 1)
	if err != nil || pos == 0 {
		return dst, 0, false, err
	}
	if count <= 0 {
		return dst, pos, false, nil
	}
	if count > MaxArgs {
		return dst, 0, false, protoErr("invalid multibulk length")
	}
	args = dst
	for i := 0; i < count; i++ {
		if pos >= len(buf) {
			return dst, 0, false, nil
		}
		if buf[pos] != '$' {
			return dst, 0, false, protoErr("expected '$', got '" + string(buf[pos]) + "'")
		}
		size, next, err := parseLength(buf, pos+1)
		if err != nil {
			return dst, 0, false, err
		}
		if next == 0 {
			return dst, 0, false, nil
		}
		if size < 0 || size > MaxBulkLen {
			return dst, 0, false, protoErr("invalid bulk length")
		}
		end := next + size
		if end+2 > len(buf) {
			return dst, 0, false, nil
		}
		if buf[end] != '\r' || buf[end+1] != '\n' {
			return dst, 0, false, protoErr("bulk string not terminated by CRLF")
		}
		args = append(args, buf[next:end:end])
		pos = end + 2
	}
	return args, pos, false, nil
}

// parseLength parses "<int>\r\n" starting at buf[start]. pos is the index
// after the CRLF, or 0 when the line is incomplete.
func parseLength(buf []byte, start int) (v, pos int, err error) {
	i := bytes.IndexByte(buf[start:], '\n')
	if i < 0 {
		if len(buf)-start > 32 {
			return 0, 0, protoErr("length line too long")
		}
		return 0, 0, nil
	}
	end := start + i
	if end == start || buf[end-1] != '\r' {
		return 0, 0, protoErr("invalid length line")
	}
	n, perr := strconv.Atoi(string(buf[start : end-1]))
	if perr != nil {
		return 0, 0, protoErr("invalid length")
	}
	return n, end + 1, nil
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
