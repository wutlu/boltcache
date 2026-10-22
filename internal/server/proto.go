package server

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"github.com/panjf2000/gnet/v2"
	"github.com/panjf2000/gnet/v2/pkg/logging"

	"github.com/wutlu/boltcache/v2/internal/command"
	"github.com/wutlu/boltcache/v2/internal/resp"
)

// maxPendingPush is the most pub/sub output that may queue for one
// connection before it's considered too slow and disconnected (similar to
// Redis' client-output-buffer-limit for pubsub clients).
const maxPendingPush = 32 << 20

const (
	// Request limits for clients that haven't authenticated (when auth is
	// enabled), so they can't make the server buffer large payloads.
	unauthMaxArgs = 10
	unauthMaxBulk = 16 << 10
	// maxQueryBuffer bounds a single pending command (Redis: 1GB).
	maxQueryBuffer = 1 << 30
)

// protoServer serves the RESP protocol and, when legacyText is set, the
// BoltCache 1.x text protocol, on an event-driven gnet listener. Every
// connection speaks RESP when the client sends RESP arrays; inline commands
// (typed in telnet/nc) are answered in the legacy text format on the TCP
// listener and in RESP on the RESP listener, exactly like Redis.
type protoServer struct {
	gnet.BuiltinEventEngine

	name       string
	addr       string
	legacyText bool
	maxConns   int64
	engine     *command.Engine
	log        *slog.Logger
	clients    *atomic.Int64 // shared across listeners for INFO

	conns   atomic.Int64
	eng     gnet.Engine
	booted  chan struct{}
	stopped chan error
}

type protoConn struct {
	gc      gnet.Conn
	srv     *protoServer
	session *command.Session
	enc     resp.Encoder
	args    [][]byte
	parser  resp.Parser
	in      []byte // unconsumed input (a partially received command)

	textPush atomic.Bool  // deliver pub/sub messages in the legacy format
	pending  atomic.Int64 // queued async push bytes
	closing  atomic.Bool
}

func newProtoServer(name, addr string, legacyText bool, maxConns int, e *command.Engine, log *slog.Logger, clients *atomic.Int64) *protoServer {
	return &protoServer{
		name:       name,
		addr:       addr,
		legacyText: legacyText,
		maxConns:   int64(maxConns),
		engine:     e,
		log:        log.With("listener", name),
		clients:    clients,
		booted:     make(chan struct{}),
		stopped:    make(chan error, 1),
	}
}

// start runs the listener in the background and waits until it's accepting
// connections or failed to bind.
func (ps *protoServer) start() error {
	go func() {
		ps.stopped <- gnet.Run(ps, "tcp://"+ps.addr,
			gnet.WithMulticore(true),
			gnet.WithTCPNoDelay(gnet.TCPNoDelay),
			gnet.WithTCPKeepAlive(5*time.Minute),
			gnet.WithReuseAddr(true),
			gnet.WithLogger(gnetLogger{ps.log}),
			gnet.WithLogLevel(logging.ErrorLevel),
		)
	}()
	select {
	case <-ps.booted:
		ps.log.Info("listening", "addr", ps.addr)
		return nil
	case err := <-ps.stopped:
		if err == nil {
			err = fmt.Errorf("listener stopped")
		}
		return fmt.Errorf("%s listener on %s: %w", ps.name, ps.addr, err)
	case <-time.After(10 * time.Second):
		return fmt.Errorf("%s listener on %s: timed out starting", ps.name, ps.addr)
	}
}

func (ps *protoServer) stop(ctx context.Context) error {
	return ps.eng.Stop(ctx)
}

func (ps *protoServer) OnBoot(eng gnet.Engine) gnet.Action {
	ps.eng = eng
	close(ps.booted)
	return gnet.None
}

func (ps *protoServer) OnOpen(gc gnet.Conn) ([]byte, gnet.Action) {
	n := ps.conns.Add(1)
	ps.clients.Add(1)
	if ps.maxConns > 0 && n > ps.maxConns {
		gc.SetContext(nil)
		return []byte("-ERR max number of clients reached\r\n"), gnet.Close
	}
	pc := &protoConn{gc: gc, srv: ps}
	pc.session = ps.engine.NewSession(gc.RemoteAddr().String(), pc)
	gc.SetContext(pc)
	return nil, gnet.None
}

func (ps *protoServer) OnClose(gc gnet.Conn, _ error) gnet.Action {
	ps.conns.Add(-1)
	ps.clients.Add(-1)
	if pc, ok := gc.Context().(*protoConn); ok && pc != nil {
		pc.closing.Store(true)
		ps.engine.CloseSession(pc.session)
	}
	return gnet.None
}

func (ps *protoServer) OnTraffic(gc gnet.Conn) (action gnet.Action) {
	pc, ok := gc.Context().(*protoConn)
	if !ok || pc == nil {
		return gnet.Close
	}
	defer func() {
		if r := recover(); r != nil {
			ps.log.Error("panic while handling command; closing connection",
				"panic", r, "remote", pc.session.Addr, "stack", string(debug.Stack()))
			action = gnet.Close
		}
	}()

	// Take everything gnet has buffered. When a command is incomplete the
	// remainder is kept in pc.in; gnet's Peek would instead copy all pending
	// data on every read, which is quadratic for large commands.
	data, _ := gc.Next(-1)
	buf, owned := data, false
	if len(pc.in) > 0 {
		pc.in = append(pc.in, data...)
		buf, owned = pc.in, true
	}

	consumed := 0
	pc.enc.Buf = pc.enc.Buf[:0]
	for consumed < len(buf) {
		if need := pc.parser.Need(); need > len(buf)-consumed {
			break // the pending command can't be complete yet
		}
		if pc.session.Authenticated {
			pc.parser.MaxArgs, pc.parser.MaxBulk = 0, 0
		} else {
			// Like Redis: unauthenticated clients can't send big requests.
			pc.parser.MaxArgs, pc.parser.MaxBulk = unauthMaxArgs, unauthMaxBulk
		}
		args, n, inline, err := pc.parser.Parse(pc.args[:0], buf[consumed:])
		pc.args = args[:0]
		if err != nil {
			pc.enc.Error(err.Error())
			_, _ = gc.Write(pc.enc.Buf)
			return gnet.Close
		}
		if n == 0 {
			break // incomplete command: wait for more data
		}
		consumed += n
		if len(args) == 0 {
			continue
		}
		if inline && ps.legacyText {
			pc.execText(args)
		} else {
			ps.engine.Exec(pc.session, args, &pc.enc)
		}
		if pc.session.Close {
			_, _ = gc.Write(pc.enc.Buf)
			return gnet.Close
		}
		if len(pc.enc.Buf) > 256<<10 {
			_, _ = gc.Write(pc.enc.Buf)
			pc.enc.Buf = pc.enc.Buf[:0]
		}
	}

	// Keep the unconsumed tail (the start of an incomplete command).
	rest := buf[consumed:]
	switch {
	case len(rest) == 0:
		pc.in = pc.in[:0]
		if cap(pc.in) > 1<<20 {
			pc.in = nil
		}
	case owned:
		if consumed > 0 {
			pc.in = pc.in[:copy(pc.in, rest)]
		}
	default:
		pc.in = append(pc.in[:0], rest...)
	}
	if len(pc.in) > maxQueryBuffer {
		pc.enc.Error("ERR Protocol error: query buffer limit exceeded")
		_, _ = gc.Write(pc.enc.Buf)
		return gnet.Close
	}

	if len(pc.enc.Buf) > 0 {
		_, _ = gc.Write(pc.enc.Buf)
	}
	if cap(pc.enc.Buf) > 4<<20 { // don't keep huge buffers around
		pc.enc.Buf = nil
	}
	return gnet.None
}

// Deliver implements pubsub.Subscriber. It's called from publishers'
// goroutines and must not block.
func (pc *protoConn) Deliver(channel, pattern, message string) {
	if pc.closing.Load() {
		return
	}
	var buf []byte
	if pc.textPush.Load() {
		buf = []byte("MESSAGE " + channel + " " + escapeText(message) + "\n")
	} else {
		var e resp.Encoder
		command.WriteMessage(&e, channel, pattern, message)
		buf = e.Buf
	}
	size := int64(len(buf))
	if pc.pending.Add(size) > maxPendingPush {
		if pc.closing.CompareAndSwap(false, true) {
			pc.srv.log.Warn("closing slow pub/sub subscriber", "remote", pc.session.Addr)
			_ = pc.gc.Close()
		}
		return
	}
	err := pc.gc.AsyncWrite(buf, func(gnet.Conn, error) error {
		pc.pending.Add(-size)
		return nil
	})
	if err != nil {
		pc.pending.Add(-size)
	}
}

// gnetLogger routes gnet's internal logs to slog.
type gnetLogger struct{ l *slog.Logger }

func (g gnetLogger) Debugf(f string, a ...any) { g.l.Debug(fmt.Sprintf(f, a...)) }
func (g gnetLogger) Infof(f string, a ...any)  { g.l.Debug(fmt.Sprintf(f, a...)) }
func (g gnetLogger) Warnf(f string, a ...any)  { g.l.Warn(fmt.Sprintf(f, a...)) }
func (g gnetLogger) Errorf(f string, a ...any) { g.l.Error(fmt.Sprintf(f, a...)) }
func (g gnetLogger) Fatalf(f string, a ...any) { g.l.Error(fmt.Sprintf(f, a...)) }

// --- legacy text protocol ---

// execText runs an inline command and renders the reply in the BoltCache
// 1.x text format:
//
//	OK / PONG            simple strings
//	VALUE <v>            bulk strings
//	NIL                  null
//	INTEGER <n>          integers
//	ARRAY <a> <b> ...    arrays (flattened)
//	ERROR: <msg>         errors
func (pc *protoConn) execText(argv [][]byte) {
	args := make([]string, len(argv))
	for i, a := range argv {
		args[i] = string(a)
	}
	name := strings.ToUpper(args[0])
	switch {
	// 1.x accepted a Go duration as SET's 4th argument: SET key value 5m.
	case name == "SET" && len(args) == 4:
		if d, err := time.ParseDuration(args[3]); err == nil && d > 0 {
			args = []string{args[0], args[1], args[2], "PX", fmt.Sprint(d.Milliseconds())}
		}
	// 1.x published everything after the channel: PUBLISH ch hello world.
	case name == "PUBLISH" && len(args) > 3:
		args = []string{args[0], args[1], strings.Join(args[2:], " ")}
	}
	var rec resp.Recorder
	pc.srv.engine.ExecStrings(pc.session, args, &rec)
	for _, v := range rec.Results() {
		pc.enc.Buf = append(pc.enc.Buf, renderText(name, v)...)
		pc.enc.Buf = append(pc.enc.Buf, '\n')
	}
	if name == "SUBSCRIBE" || name == "PSUBSCRIBE" {
		pc.textPush.Store(true)
	}
}

func renderText(cmd string, v resp.Value) string {
	switch v.Kind {
	case resp.KindSimple:
		return v.Str
	case resp.KindError:
		return "ERROR: " + v.Str
	case resp.KindInt:
		if cmd == "PUBLISH" {
			return fmt.Sprintf("PUBLISHED %d", v.Int)
		}
		return fmt.Sprintf("INTEGER %d", v.Int)
	case resp.KindBulk:
		if cmd == "EVAL" || cmd == "EVALSHA" {
			return "RESULT " + escapeText(v.Str)
		}
		return "VALUE " + escapeText(v.Str)
	case resp.KindNull, resp.KindNullArray:
		return "NIL"
	case resp.KindArray:
		if (cmd == "SUBSCRIBE" || cmd == "PSUBSCRIBE") && len(v.Array) == 3 {
			return "SUBSCRIBED " + v.Array[1].Str
		}
		var parts []string
		flatten(v, &parts)
		return strings.TrimRight("ARRAY "+strings.Join(parts, " "), " ")
	}
	return ""
}

func flatten(v resp.Value, out *[]string) {
	switch v.Kind {
	case resp.KindArray:
		for _, e := range v.Array {
			flatten(e, out)
		}
	case resp.KindInt:
		*out = append(*out, fmt.Sprint(v.Int))
	case resp.KindNull, resp.KindNullArray:
		*out = append(*out, "(nil)")
	default:
		*out = append(*out, escapeText(v.Str))
	}
}

var textEscaper = strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\r", "\\r")

// escapeText keeps a value on one line so line-based clients don't break.
func escapeText(s string) string {
	if !strings.ContainsAny(s, "\\\n\r") {
		return s
	}
	return textEscaper.Replace(s)
}
