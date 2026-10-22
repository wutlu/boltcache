package server

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/http/pprof"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"

	"github.com/wutlu/boltcache/v2/config"
	"github.com/wutlu/boltcache/v2/internal/auth"
	"github.com/wutlu/boltcache/v2/internal/command"
	"github.com/wutlu/boltcache/v2/internal/resp"
	"github.com/wutlu/boltcache/v2/swaggerui"
)

//go:embed web
var webFS embed.FS

// CacheResponse is the JSON envelope of every REST response.
type CacheResponse struct {
	Success bool   `json:"success"`
	Value   any    `json:"value,omitempty"`
	Error   string `json:"error,omitempty"`
	Count   int64  `json:"count,omitempty"`
}

type restServer struct {
	cfg     *config.Config
	engine  *command.Engine
	auth    *auth.Manager
	log     *slog.Logger
	metrics *metrics
	version string

	maxBody    int64
	defaultTTL time.Duration
	maxTTL     time.Duration
	limiter    *ipLimiter
	upgrader   websocket.Upgrader
	wsClients  atomic.Int64
	clients    *atomic.Int64

	srv *http.Server
}

func newRESTServer(cfg *config.Config, e *command.Engine, am *auth.Manager, m *metrics, log *slog.Logger, version string, clients *atomic.Int64) *restServer {
	maxBody, _ := config.ParseMemorySize(cfg.Server.REST.MaxRequestSize)
	if maxBody <= 0 {
		maxBody = 10 << 20
	}
	rs := &restServer{
		cfg:        cfg,
		engine:     e,
		auth:       am,
		log:        log.With("listener", "rest"),
		metrics:    m,
		version:    version,
		maxBody:    maxBody,
		defaultTTL: cfg.Cache.DefaultTTL,
		maxTTL:     cfg.Cache.MaxTTL,
		clients:    clients,
	}
	if rl := cfg.Security.RateLimit; rl.Enabled {
		rs.limiter = newIPLimiter(rate.Limit(rl.RequestsPerSecond), max(rl.Burst, 1))
	}
	rs.upgrader = websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     rs.originAllowed,
	}
	return rs
}

func (rs *restServer) handler() http.Handler {
	mux := http.NewServeMux()
	timeout := rs.cfg.Server.REST.RequestTimeout
	h := func(pattern string, fn http.HandlerFunc) {
		var handler http.Handler = fn
		if timeout > 0 {
			handler = http.TimeoutHandler(fn, timeout, `{"success":false,"error":"request timed out"}`)
		}
		mux.Handle(pattern, handler)
	}

	h("GET /ping", rs.ping)
	if hc := rs.cfg.Monitoring.Health; hc.Enabled {
		h("GET "+hc.Endpoint, rs.health)
		h("GET /ready", rs.health)
	}
	if mc := rs.cfg.Monitoring.Metrics; mc.Enabled {
		h("GET "+mc.Endpoint, rs.metrics.handler)
	}
	h("GET /info", rs.info)

	h("PUT /cache/{key}", rs.setValue)
	h("POST /cache/{key}", rs.setValue)
	h("GET /cache/{key}", rs.getValue)
	h("DELETE /cache/{key}", rs.deleteValue)
	h("GET /cache/{key}/ttl", rs.getTTL)
	h("GET /keys", rs.keys)

	h("POST /list/{key}", rs.listPush)
	h("DELETE /list/{key}", rs.listPop)
	h("GET /list/{key}", rs.listRange)

	h("POST /set/{key}", rs.setAdd)
	h("GET /set/{key}", rs.setMembers)
	h("DELETE /set/{key}/{member}", rs.setRemove)

	h("GET /hash/{key}", rs.hashGetAll)
	h("PUT /hash/{key}/{field}", rs.hashSet)
	h("GET /hash/{key}/{field}", rs.hashGet)
	h("DELETE /hash/{key}/{field}", rs.hashDel)

	h("POST /publish/{channel}", rs.publish)
	mux.HandleFunc("GET /subscribe/{channel}", rs.subscribe) // no timeout: long-lived

	h("POST /eval", rs.eval)
	h("POST /command", rs.command)

	h("GET /auth/tokens", rs.listTokens)
	h("POST /auth/tokens", rs.createToken)
	h("DELETE /auth/tokens/{token}", rs.deleteToken)

	if rs.cfg.Server.SwaggerUI {
		h("GET /openapi.json", swaggerui.OpenAPIHandler)
		h("GET /docs", swaggerui.SwaggerUIHandler)
	}
	if pc := rs.cfg.Monitoring.Profiling; pc.Enabled {
		prefix := strings.TrimRight(pc.Endpoint, "/")
		mux.HandleFunc("GET "+prefix+"/", func(w http.ResponseWriter, r *http.Request) {
			r.URL.Path = "/debug/pprof/" + strings.TrimPrefix(r.URL.Path, prefix+"/")
			pprof.Index(w, r)
		})
		mux.HandleFunc("GET "+prefix+"/cmdline", pprof.Cmdline)
		mux.HandleFunc("GET "+prefix+"/profile", pprof.Profile)
		mux.HandleFunc("GET "+prefix+"/symbol", pprof.Symbol)
		mux.HandleFunc("GET "+prefix+"/trace", pprof.Trace)
	}

	static, _ := fs.Sub(webFS, "web")
	page := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		b, _ := fs.ReadFile(static, "rest-client.html")
		_, _ = w.Write(b)
	}
	mux.HandleFunc("GET /{$}", page)
	mux.HandleFunc("GET /rest-client.html", page)

	return rs.middleware(mux)
}

// publicPaths never require a token: health checks for orchestrators and
// static pages (the web client asks for the token itself).
func (rs *restServer) isPublic(path string) bool {
	switch path {
	case "/ping", "/ready", "/", "/rest-client.html", "/docs", "/openapi.json":
		return true
	}
	return rs.cfg.Monitoring.Health.Enabled && path == rs.cfg.Monitoring.Health.Endpoint
}

func (rs *restServer) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				rs.log.Error("panic in HTTP handler", "panic", rec, "path", r.URL.Path)
				if !sw.wrote {
					rs.sendError(sw, "internal server error", http.StatusInternalServerError)
				}
			}
			rs.metrics.observeHTTP(sw.status)
			if rs.log.Enabled(r.Context(), slog.LevelDebug) {
				rs.log.Debug("http request", "method", r.Method, "path", r.URL.Path,
					"status", sw.status, "duration", time.Since(start), "remote", r.RemoteAddr)
			}
		}()

		rs.applyCORS(sw, r)
		if r.Method == http.MethodOptions {
			sw.WriteHeader(http.StatusNoContent)
			return
		}
		// CSRF protection: browsers send Origin with cross-site requests, and
		// a "simple" POST (text/plain, forms) runs without a CORS preflight.
		// Reject state-changing requests from origins that aren't allowed.
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !rs.originAllowed(r) {
			rs.sendError(sw, "cross-origin request from "+r.Header.Get("Origin")+" is not allowed (server.rest.cors_origins)", http.StatusForbidden)
			return
		}
		if rs.limiter != nil && !rs.limiter.allow(clientIP(r)) {
			sw.Header().Set("Retry-After", "1")
			rs.sendError(sw, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		if !rs.isPublic(r.URL.Path) && !rs.auth.Check(auth.FromRequest(r)) {
			sw.Header().Set("WWW-Authenticate", `Bearer realm="boltcache"`)
			rs.sendError(sw, "authentication required", http.StatusUnauthorized)
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(sw, r.Body, rs.maxBody)
		}
		next.ServeHTTP(sw, r)
	})
}

func (rs *restServer) applyCORS(w http.ResponseWriter, r *http.Request) {
	rc := rs.cfg.Server.REST
	if !rc.CORSEnabled {
		return
	}
	origin := r.Header.Get("Origin")
	h := w.Header()
	switch {
	case slices.Contains(rc.CORSOrigins, "*"):
		h.Set("Access-Control-Allow-Origin", "*")
	case origin != "" && slices.Contains(rc.CORSOrigins, origin):
		h.Set("Access-Control-Allow-Origin", origin)
		h.Add("Vary", "Origin")
	default:
		return
	}
	h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Token")
	h.Set("Access-Control-Max-Age", "86400")
}

// originAllowed reports whether a request's Origin (if any) is the server
// itself or listed in cors_origins. Used for WebSocket upgrades and to block
// cross-site request forgery.
func (rs *restServer) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // non-browser client
	}
	if u := strings.TrimPrefix(strings.TrimPrefix(origin, "http://"), "https://"); u == r.Host {
		return true // same origin
	}
	rc := rs.cfg.Server.REST
	return rc.CORSEnabled && (slices.Contains(rc.CORSOrigins, "*") || slices.Contains(rc.CORSOrigins, origin))
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status, w.wrote = code, true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Hijack is required by the WebSocket upgrader.
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijacking not supported")
	}
	w.status, w.wrote = http.StatusSwitchingProtocols, true
	return h.Hijack()
}

func (rs *restServer) sendResponse(w http.ResponseWriter, resp CacheResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (rs *restServer) sendError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(CacheResponse{Success: false, Error: message})
}

// exec runs a command as an authenticated internal session.
func (rs *restServer) exec(args ...string) resp.Value {
	s := rs.engine.NewSession("rest", nil)
	s.Authenticated = true
	var rec resp.Recorder
	rs.engine.ExecStrings(s, args, &rec)
	return rec.Result()
}

// reply converts a command reply to an HTTP response. Missing values become
// 404 when notFound is non-empty.
func (rs *restServer) reply(w http.ResponseWriter, v resp.Value, notFound string) {
	switch {
	case v.Kind == resp.KindError:
		code := http.StatusBadRequest
		switch {
		case strings.HasPrefix(v.Str, "WRONGTYPE"):
			code = http.StatusConflict
		case strings.HasPrefix(v.Str, "OOM"):
			code = http.StatusInsufficientStorage
		}
		rs.sendError(w, v.Str, code)
	case v.IsNil() && notFound != "":
		rs.sendError(w, notFound, http.StatusNotFound)
	case v.Kind == resp.KindInt:
		rs.sendResponse(w, CacheResponse{Success: true, Value: v.Int, Count: v.Int})
	case v.Kind == resp.KindSimple:
		rs.sendResponse(w, CacheResponse{Success: true})
	default:
		rs.sendResponse(w, CacheResponse{Success: true, Value: v.Interface()})
	}
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return fmt.Errorf("request body exceeds %d bytes", mbe.Limit)
		}
		if errors.Is(err, io.EOF) {
			return errors.New("request body is empty")
		}
		return fmt.Errorf("invalid JSON: %v", err)
	}
	return nil
}

// --- handlers ---

func (rs *restServer) ping(w http.ResponseWriter, _ *http.Request) {
	rs.sendResponse(w, CacheResponse{Success: true, Value: "PONG"})
}

func (rs *restServer) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": rs.version})
}

func (rs *restServer) info(w http.ResponseWriter, _ *http.Request) {
	c := rs.engine.Cache()
	st := c.Stats()
	commands, _ := rs.engine.Stats()
	o := c.Options()
	rs.sendResponse(w, CacheResponse{Success: true, Value: map[string]any{
		"version":          rs.version,
		"uptime_seconds":   int64(rs.metrics.uptime().Seconds()),
		"keys":             st.Keys,
		"keys_with_ttl":    st.KeysWithTTL,
		"used_memory":      st.UsedMemory,
		"max_memory":       o.MaxMemory,
		"max_keys":         o.MaxKeys,
		"eviction_policy":  o.EvictionPolicy,
		"hits":             st.Hits,
		"misses":           st.Misses,
		"expired_keys":     st.Expired,
		"evicted_keys":     st.Evicted,
		"commands":         commands,
		"clients":          rs.clients.Load(),
		"replicas":         0,
		"lua_scripting":    rs.cfg.Features.LuaScripting,
		"pub_sub":          rs.cfg.Features.PubSub,
		"auth_enabled":     rs.auth.Enabled(),
		"pubsub_channels":  len(rs.engine.Hub().Channels("")),
		"websocket_client": rs.wsClients.Load(),
	}})
}

type setRequest struct {
	Value json.RawMessage `json:"value"`
	TTL   string          `json:"ttl,omitempty"`
}

// valueString accepts any JSON value: strings are stored as-is, other
// values (numbers, objects...) as their JSON text.
func valueString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	return string(raw), true
}

func (rs *restServer) ttlFor(s string) (time.Duration, error) {
	ttl := rs.defaultTTL
	if s != "" {
		d, err := time.ParseDuration(s)
		if err != nil || d < 0 {
			return 0, fmt.Errorf("invalid ttl %q (examples: 30s, 5m, 2h)", s)
		}
		ttl = d
	}
	if rs.maxTTL > 0 && ttl > rs.maxTTL {
		return 0, fmt.Errorf("ttl %s exceeds cache.max_ttl (%s)", ttl, rs.maxTTL)
	}
	return ttl, nil
}

func (rs *restServer) setValue(w http.ResponseWriter, r *http.Request) {
	var req setRequest
	if err := decodeJSON(r, &req); err != nil {
		rs.sendError(w, err.Error(), http.StatusBadRequest)
		return
	}
	val, ok := valueString(req.Value)
	if !ok {
		rs.sendError(w, `missing "value"`, http.StatusBadRequest)
		return
	}
	ttl, err := rs.ttlFor(req.TTL)
	if err != nil {
		rs.sendError(w, err.Error(), http.StatusBadRequest)
		return
	}
	args := []string{"SET", r.PathValue("key"), val}
	if ttl > 0 {
		args = append(args, "PX", strconv.FormatInt(max(ttl.Milliseconds(), 1), 10))
	}
	rs.reply(w, rs.exec(args...), "")
}

// getValue returns the value of any type: strings as strings, lists and
// sets as arrays, hashes as objects and sorted sets as [{member, score}].
func (rs *restServer) getValue(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	switch t := rs.exec("TYPE", key); t.Str {
	case "none":
		rs.sendError(w, "Key not found", http.StatusNotFound)
	case "list":
		rs.reply(w, rs.exec("LRANGE", key, "0", "-1"), "Key not found")
	case "set":
		rs.reply(w, rs.exec("SMEMBERS", key), "Key not found")
	case "hash":
		rs.hashObject(w, key)
	case "zset":
		v := rs.exec("ZRANGE", key, "0", "-1", "WITHSCORES")
		if v.Kind == resp.KindError {
			rs.reply(w, v, "")
			return
		}
		out := make([]map[string]any, 0, len(v.Array)/2)
		for i := 0; i+1 < len(v.Array); i += 2 {
			score, _ := strconv.ParseFloat(v.Array[i+1].Str, 64)
			out = append(out, map[string]any{"member": v.Array[i].Str, "score": jsonScore(score)})
		}
		rs.sendResponse(w, CacheResponse{Success: true, Value: out, Count: int64(len(out))})
	default:
		rs.reply(w, rs.exec("GET", key), "Key not found")
	}
}

// jsonScore keeps infinite scores representable in JSON.
func jsonScore(f float64) any {
	if math.IsInf(f, 0) {
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	return f
}

func (rs *restServer) deleteValue(w http.ResponseWriter, r *http.Request) {
	v := rs.exec("DEL", r.PathValue("key"))
	rs.sendResponse(w, CacheResponse{Success: true, Count: v.Int})
}

func (rs *restServer) getTTL(w http.ResponseWriter, r *http.Request) {
	v := rs.exec("PTTL", r.PathValue("key"))
	if v.Int == -2 {
		rs.sendError(w, "Key not found", http.StatusNotFound)
		return
	}
	out := map[string]any{"ttl_ms": v.Int, "persistent": v.Int == -1}
	if v.Int >= 0 {
		out["ttl"] = (time.Duration(v.Int) * time.Millisecond).String()
	}
	rs.sendResponse(w, CacheResponse{Success: true, Value: out})
}

func (rs *restServer) keys(w http.ResponseWriter, r *http.Request) {
	pattern := r.URL.Query().Get("pattern")
	if pattern == "" {
		pattern = "*"
	}
	limit := 1000
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = min(l, 100000)
	}
	cursor := r.URL.Query().Get("cursor")
	if cursor == "" {
		cursor = "0"
	}
	v := rs.exec("SCAN", cursor, "MATCH", pattern, "COUNT", strconv.Itoa(limit))
	if v.Kind == resp.KindError {
		rs.reply(w, v, "")
		return
	}
	keys := v.Array[1].Interface().([]any)
	rs.sendResponse(w, CacheResponse{Success: true, Count: int64(len(keys)), Value: map[string]any{
		"keys":   keys,
		"cursor": v.Array[0].Str, // "0" when the iteration is complete
	}})
}

func (rs *restServer) listPush(w http.ResponseWriter, r *http.Request) {
	var values []string
	if err := decodeJSON(r, &values); err != nil {
		rs.sendError(w, "expected a JSON array of strings: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(values) == 0 {
		rs.sendError(w, "no values given", http.StatusBadRequest)
		return
	}
	cmd := "LPUSH"
	if strings.EqualFold(r.URL.Query().Get("side"), "right") {
		cmd = "RPUSH"
	}
	rs.reply(w, rs.exec(append([]string{cmd, r.PathValue("key")}, values...)...), "")
}

func (rs *restServer) listPop(w http.ResponseWriter, r *http.Request) {
	cmd := "LPOP"
	if strings.EqualFold(r.URL.Query().Get("side"), "right") {
		cmd = "RPOP"
	}
	rs.reply(w, rs.exec(cmd, r.PathValue("key")), "List empty or not found")
}

func (rs *restServer) listRange(w http.ResponseWriter, r *http.Request) {
	start, stop := r.URL.Query().Get("start"), r.URL.Query().Get("stop")
	if start == "" {
		start = "0"
	}
	if stop == "" {
		stop = "-1"
	}
	rs.reply(w, rs.exec("LRANGE", r.PathValue("key"), start, stop), "")
}

func (rs *restServer) setAdd(w http.ResponseWriter, r *http.Request) {
	var members []string
	if err := decodeJSON(r, &members); err != nil {
		rs.sendError(w, "expected a JSON array of strings: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(members) == 0 {
		rs.sendError(w, "no members given", http.StatusBadRequest)
		return
	}
	rs.reply(w, rs.exec(append([]string{"SADD", r.PathValue("key")}, members...)...), "")
}

func (rs *restServer) setMembers(w http.ResponseWriter, r *http.Request) {
	rs.reply(w, rs.exec("SMEMBERS", r.PathValue("key")), "")
}

func (rs *restServer) setRemove(w http.ResponseWriter, r *http.Request) {
	rs.reply(w, rs.exec("SREM", r.PathValue("key"), r.PathValue("member")), "")
}

func (rs *restServer) hashSet(w http.ResponseWriter, r *http.Request) {
	var req setRequest
	if err := decodeJSON(r, &req); err != nil {
		rs.sendError(w, err.Error(), http.StatusBadRequest)
		return
	}
	val, ok := valueString(req.Value)
	if !ok {
		rs.sendError(w, `missing "value"`, http.StatusBadRequest)
		return
	}
	v := rs.exec("HSET", r.PathValue("key"), r.PathValue("field"), val)
	if v.Kind == resp.KindError {
		rs.reply(w, v, "")
		return
	}
	rs.sendResponse(w, CacheResponse{Success: true, Count: v.Int})
}

func (rs *restServer) hashGet(w http.ResponseWriter, r *http.Request) {
	rs.reply(w, rs.exec("HGET", r.PathValue("key"), r.PathValue("field")), "Field not found")
}

func (rs *restServer) hashDel(w http.ResponseWriter, r *http.Request) {
	rs.reply(w, rs.exec("HDEL", r.PathValue("key"), r.PathValue("field")), "")
}

func (rs *restServer) hashGetAll(w http.ResponseWriter, r *http.Request) {
	rs.hashObject(w, r.PathValue("key"))
}

func (rs *restServer) hashObject(w http.ResponseWriter, key string) {
	v := rs.exec("HGETALL", key)
	if v.Kind == resp.KindError {
		rs.reply(w, v, "")
		return
	}
	obj := make(map[string]string, len(v.Array)/2)
	for i := 0; i+1 < len(v.Array); i += 2 {
		obj[v.Array[i].Str] = v.Array[i+1].Str
	}
	rs.sendResponse(w, CacheResponse{Success: true, Value: obj, Count: int64(len(obj))})
}

func (rs *restServer) publish(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message json.RawMessage `json:"message"`
	}
	if err := decodeJSON(r, &req); err != nil {
		rs.sendError(w, err.Error(), http.StatusBadRequest)
		return
	}
	msg, ok := valueString(req.Message)
	if !ok {
		rs.sendError(w, `missing "message"`, http.StatusBadRequest)
		return
	}
	v := rs.exec("PUBLISH", r.PathValue("channel"), msg)
	if v.Kind == resp.KindError {
		rs.reply(w, v, "")
		return
	}
	rs.sendResponse(w, CacheResponse{Success: true, Count: v.Int, Value: v.Int})
}

func (rs *restServer) eval(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Script string   `json:"script"`
		Keys   []string `json:"keys"`
		Args   []string `json:"args"`
	}
	if err := decodeJSON(r, &req); err != nil {
		rs.sendError(w, err.Error(), http.StatusBadRequest)
		return
	}
	args := append([]string{"EVAL", req.Script, strconv.Itoa(len(req.Keys))}, req.Keys...)
	rs.reply(w, rs.exec(append(args, req.Args...)...), "")
}

// command runs any command: {"args": ["HSET", "user:1", "name", "Ada"]} or
// a bare JSON array. The reply is converted to JSON (errors -> 400).
func (rs *restServer) command(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		rs.sendError(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	var args []string
	if err := json.Unmarshal(body, &args); err != nil {
		var req struct {
			Args []string `json:"args"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			rs.sendError(w, `expected {"args": ["COMMAND", "arg", ...]}`, http.StatusBadRequest)
			return
		}
		args = req.Args
	}
	if len(args) == 0 {
		rs.sendError(w, "empty command", http.StatusBadRequest)
		return
	}
	v := rs.exec(args...)
	if v.Kind == resp.KindError {
		rs.reply(w, v, "")
		return
	}
	rs.sendResponse(w, CacheResponse{Success: true, Value: v.Interface()})
}

func (rs *restServer) listTokens(w http.ResponseWriter, _ *http.Request) {
	if !rs.auth.Enabled() {
		rs.sendError(w, "authentication is disabled (security.auth.enabled)", http.StatusNotFound)
		return
	}
	rs.sendResponse(w, CacheResponse{Success: true, Value: rs.auth.List()})
}

func (rs *restServer) createToken(w http.ResponseWriter, _ *http.Request) {
	if !rs.auth.Enabled() {
		rs.sendError(w, "authentication is disabled (security.auth.enabled)", http.StatusNotFound)
		return
	}
	rs.sendResponse(w, CacheResponse{Success: true, Value: map[string]string{
		"token": rs.auth.Generate(),
		"note":  "runtime token: kept in memory until the server restarts",
	}})
}

func (rs *restServer) deleteToken(w http.ResponseWriter, r *http.Request) {
	if !rs.auth.Enabled() {
		rs.sendError(w, "authentication is disabled (security.auth.enabled)", http.StatusNotFound)
		return
	}
	removed, fromConfig := rs.auth.Revoke(r.PathValue("token"))
	switch {
	case fromConfig:
		rs.sendError(w, "tokens from the config file can't be revoked at runtime", http.StatusForbidden)
	case !removed:
		rs.sendError(w, "token not found", http.StatusNotFound)
	default:
		rs.sendResponse(w, CacheResponse{Success: true})
	}
}

// --- WebSocket pub/sub ---

const (
	wsWriteWait  = 10 * time.Second
	wsPongWait   = 60 * time.Second
	wsPingPeriod = 50 * time.Second
	wsQueueSize  = 1024
)

// wsSubscriber delivers messages through a buffered queue drained by a
// single writer goroutine (gorilla/websocket allows one concurrent writer).
// A subscriber whose queue overflows is disconnected.
type wsSubscriber struct {
	queue chan []byte
	once  sync.Once
	done  chan struct{}
}

func (s *wsSubscriber) Deliver(channel, pattern, message string) {
	select {
	case <-s.done:
		return
	default:
	}
	select {
	case s.queue <- []byte(message):
	default:
		s.close() // too slow
	}
}

func (s *wsSubscriber) close() { s.once.Do(func() { close(s.done) }) }

func (rs *restServer) subscribe(w http.ResponseWriter, r *http.Request) {
	if !rs.cfg.Features.PubSub {
		rs.sendError(w, "Pub/Sub is disabled on this server (features.pub_sub)", http.StatusNotFound)
		return
	}
	channel := r.PathValue("channel")
	conn, err := rs.upgrader.Upgrade(w, r, nil)
	if err != nil {
		rs.log.Debug("websocket upgrade failed", "err", err)
		return
	}
	rs.wsClients.Add(1)
	rs.clients.Add(1)
	defer func() {
		rs.wsClients.Add(-1)
		rs.clients.Add(-1)
	}()

	sub := &wsSubscriber{queue: make(chan []byte, wsQueueSize), done: make(chan struct{})}
	hub := rs.engine.Hub()
	hub.Subscribe(sub, channel)
	defer hub.Unsubscribe(sub, channel)

	// Reader: handles pongs and detects disconnects.
	conn.SetReadLimit(4096)
	_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(wsPongWait)) })
	go func() {
		defer sub.close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ping := time.NewTicker(wsPingPeriod)
	defer ping.Stop()
	defer conn.Close()
	for {
		select {
		case msg := <-sub.queue:
			_ = conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ping.C:
			_ = conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-sub.done:
			_ = conn.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
			return
		}
	}
}

// --- rate limiting ---

type ipLimiter struct {
	mu      sync.Mutex
	limit   rate.Limit
	burst   int
	clients map[string]*ipEntry
	lastGC  time.Time
}

type ipEntry struct {
	lim  *rate.Limiter
	seen time.Time
}

func newIPLimiter(limit rate.Limit, burst int) *ipLimiter {
	return &ipLimiter{limit: limit, burst: burst, clients: map[string]*ipEntry{}, lastGC: time.Now()}
}

func (l *ipLimiter) allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastGC) > time.Minute {
		for k, e := range l.clients {
			if now.Sub(e.seen) > 3*time.Minute {
				delete(l.clients, k)
			}
		}
		l.lastGC = now
	}
	e, ok := l.clients[ip]
	if !ok {
		e = &ipEntry{lim: rate.NewLimiter(l.limit, l.burst)}
		l.clients[ip] = e
	}
	e.seen = now
	return e.lim.AllowN(now, 1)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (rs *restServer) start(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("rest listener on %s: %w", addr, err)
	}
	rs.srv = &http.Server{
		Handler:           rs.handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(rs.log.Handler(), slog.LevelWarn),
	}
	tls := rs.cfg.Security.TLS
	go func() {
		var err error
		if tls.Enabled {
			err = rs.srv.ServeTLS(ln, tls.CertFile, tls.KeyFile)
		} else {
			err = rs.srv.Serve(ln)
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			rs.log.Error("rest server stopped", "err", err)
		}
	}()
	scheme := "http"
	if tls.Enabled {
		scheme = "https"
	}
	rs.log.Info("listening", "addr", addr, "url", scheme+"://"+displayAddr(addr))
	return nil
}

func (rs *restServer) stop(ctx context.Context) error {
	if rs.srv == nil {
		return nil
	}
	return rs.srv.Shutdown(ctx)
}

func displayAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return net.JoinHostPort(host, port)
}
