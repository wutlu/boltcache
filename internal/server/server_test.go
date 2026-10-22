package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"

	"github.com/wutlu/boltcache/config"
	"github.com/wutlu/boltcache/internal/resp"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

type testServer struct {
	*Server
	cfg  *config.Config
	tcp  string
	resp string
	rest string
}

func testConfig(t *testing.T) *config.Config {
	cfg := config.Default()
	cfg.Server.TCP.Host = "127.0.0.1"
	cfg.Server.TCP.Port = freePort(t)
	cfg.Server.RESP.Host = "127.0.0.1"
	cfg.Server.RESP.Port = freePort(t)
	cfg.Server.REST.Host = "127.0.0.1"
	cfg.Server.REST.Port = freePort(t)
	cfg.Server.SwaggerUI = true
	cfg.Persistence.File = filepath.Join(t.TempDir(), "data.json")
	cfg.Persistence.BackupCount = 0
	return cfg
}

func startServer(t *testing.T, cfg *config.Config) *testServer {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := New(cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	ts := &testServer{
		Server: srv,
		cfg:    cfg,
		tcp:    cfg.GetTCPAddress(),
		resp:   cfg.GetRESPAddress(),
		rest:   "http://" + cfg.GetRESTAddress(),
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			ts.stop(t)
		}
	})
	return ts
}

func (ts *testServer) stop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ts.Shutdown(ctx); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

func goRedis(t *testing.T, addr, password string) *redis.Client {
	t.Helper()
	c := redis.NewClient(&redis.Options{Addr: addr, Password: password, MaxRetries: -1, PoolSize: 8})
	t.Cleanup(func() { c.Close() })
	return c
}

// TestGoRedisCompatibility drives the server with the most popular Go Redis
// client, covering connection setup (HELLO/CLIENT SETINFO negotiation),
// pipelines, transactions, scripts and pub/sub.
func TestGoRedisCompatibility(t *testing.T) {
	ts := startServer(t, testConfig(t))
	ctx := context.Background()
	rdb := goRedis(t, ts.resp, "")

	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := rdb.Set(ctx, "k", "v", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if v, err := rdb.Get(ctx, "k").Result(); err != nil || v != "v" {
		t.Fatalf("get = %q %v", v, err)
	}
	if _, err := rdb.Get(ctx, "missing").Result(); err != redis.Nil {
		t.Fatalf("missing key: %v", err)
	}
	if ttl := rdb.TTL(ctx, "k").Val(); ttl < 59*time.Second {
		t.Fatalf("ttl = %v", ttl)
	}
	if ok := rdb.SetNX(ctx, "k", "x", 0).Val(); ok {
		t.Fatal("SETNX overwrote")
	}
	rdb.HSet(ctx, "user:1", map[string]any{"name": "Ada", "year": 1815})
	if m := rdb.HGetAll(ctx, "user:1").Val(); m["name"] != "Ada" || m["year"] != "1815" {
		t.Fatalf("hgetall = %v", m)
	}
	rdb.RPush(ctx, "q", "a", "b", "c")
	if v := rdb.LRange(ctx, "q", 0, -1).Val(); strings.Join(v, ",") != "a,b,c" {
		t.Fatalf("lrange = %v", v)
	}
	rdb.SAdd(ctx, "s", "x", "y")
	if n := rdb.SCard(ctx, "s").Val(); n != 2 {
		t.Fatalf("scard = %d", n)
	}
	if v, err := rdb.IncrByFloat(ctx, "f", 1.5).Result(); err != nil || v != 1.5 {
		t.Fatalf("incrbyfloat = %v %v", v, err)
	}

	// Pipeline of 1000 commands in one round trip.
	pipe := rdb.Pipeline()
	for i := 0; i < 1000; i++ {
		pipe.Incr(ctx, "pipe-counter")
	}
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if v := rdb.Get(ctx, "pipe-counter").Val(); v != "1000" {
		t.Fatalf("pipe-counter = %s", v)
	}

	// MULTI/EXEC with WATCH (optimistic locking).
	err := rdb.Watch(ctx, func(tx *redis.Tx) error {
		n, err := tx.Get(ctx, "pipe-counter").Int()
		if err != nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(p redis.Pipeliner) error {
			p.Set(ctx, "pipe-counter", n*2, 0)
			return nil
		})
		return err
	}, "pipe-counter")
	if err != nil {
		t.Fatalf("watch tx: %v", err)
	}
	if v := rdb.Get(ctx, "pipe-counter").Val(); v != "2000" {
		t.Fatalf("after tx = %s", v)
	}

	// Lua scripts through EVALSHA with automatic fallback to EVAL.
	script := redis.NewScript(`return redis.call('INCRBY', KEYS[1], ARGV[1])`)
	if v, err := script.Run(ctx, rdb, []string{"lua-counter"}, 5).Int(); err != nil || v != 5 {
		t.Fatalf("script = %v %v", v, err)
	}
	if v, err := script.Run(ctx, rdb, []string{"lua-counter"}, 5).Int(); err != nil || v != 10 {
		t.Fatalf("script (cached) = %v %v", v, err)
	}

	// SCAN iterator.
	for i := 0; i < 300; i++ {
		rdb.Set(ctx, fmt.Sprintf("scan:%d", i), i, 0)
	}
	seen := map[string]bool{}
	iter := rdb.Scan(ctx, 0, "scan:*", 50).Iterator()
	for iter.Next(ctx) {
		seen[iter.Val()] = true
	}
	if iter.Err() != nil || len(seen) != 300 {
		t.Fatalf("scan saw %d keys: %v", len(seen), iter.Err())
	}

	// Pub/Sub.
	sub := rdb.Subscribe(ctx, "news")
	defer sub.Close()
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	psub := rdb.PSubscribe(ctx, "events.*")
	defer psub.Close()
	if _, err := psub.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	if n := rdb.Publish(ctx, "news", "hello").Val(); n != 1 {
		t.Fatalf("publish reached %d", n)
	}
	rdb.Publish(ctx, "events.login", "ada")
	select {
	case m := <-sub.Channel():
		if m.Channel != "news" || m.Payload != "hello" {
			t.Fatalf("message = %+v", m)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no message received")
	}
	select {
	case m := <-psub.Channel():
		if m.Pattern != "events.*" || m.Channel != "events.login" || m.Payload != "ada" {
			t.Fatalf("pmessage = %+v", m)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no pattern message received")
	}

	// Errors are surfaced as Redis errors.
	if err := rdb.LPush(ctx, "k", "x").Err(); err == nil || !strings.HasPrefix(err.Error(), "WRONGTYPE") {
		t.Fatalf("wrongtype: %v", err)
	}
	if info := rdb.Info(ctx, "server").Val(); !strings.Contains(info, "boltcache_version:") {
		t.Fatalf("info = %q", info)
	}
}

func TestAuthAcrossProtocols(t *testing.T) {
	cfg := testConfig(t)
	cfg.Security.Auth.Enabled = true
	cfg.Security.Auth.Tokens = []string{"s3cret-token"}
	ts := startServer(t, cfg)
	ctx := context.Background()

	if err := goRedis(t, ts.resp, "").Set(ctx, "k", "v", 0).Err(); err == nil || !strings.Contains(err.Error(), "NOAUTH") {
		t.Fatalf("unauthenticated RESP write: %v", err)
	}
	if err := goRedis(t, ts.resp, "wrong").Ping(ctx).Err(); err == nil {
		t.Fatal("wrong password accepted")
	}
	rdb := goRedis(t, ts.resp, "s3cret-token")
	if err := rdb.Set(ctx, "k", "v", 0).Err(); err != nil {
		t.Fatalf("authenticated write: %v", err)
	}

	// REST: health stays public, data requires a token.
	get := func(path, token string) int {
		req, _ := http.NewRequest("GET", ts.rest+path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := get("/health", ""); code != 200 {
		t.Fatalf("/health = %d", code)
	}
	if code := get("/cache/k", ""); code != 401 {
		t.Fatalf("unauthenticated GET = %d", code)
	}
	if code := get("/cache/k", "nope"); code != 401 {
		t.Fatalf("bad token GET = %d", code)
	}
	if code := get("/cache/k", "s3cret-token"); code != 200 {
		t.Fatalf("authenticated GET = %d", code)
	}
	if code := get("/metrics", ""); code != 401 {
		t.Fatalf("unauthenticated /metrics = %d", code)
	}

	// Runtime tokens work for RESP too, and can be revoked.
	var created struct {
		Value struct{ Token string } `json:"value"`
	}
	req, _ := http.NewRequest("POST", ts.rest+"/auth/tokens", nil)
	req.Header.Set("X-API-Token", "s3cret-token")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	if created.Value.Token == "" {
		t.Fatal("no token created")
	}
	if err := goRedis(t, ts.resp, created.Value.Token).Ping(ctx).Err(); err != nil {
		t.Fatalf("runtime token rejected: %v", err)
	}

	// The legacy text protocol authenticates with AUTH too.
	conn, err := net.Dial("tcp", ts.tcp)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	fmt.Fprint(conn, "GET k\nAUTH s3cret-token\nGET k\n")
	for _, want := range []string{"ERROR: NOAUTH Authentication required.", "OK", "VALUE v"} {
		line, _ := r.ReadString('\n')
		if strings.TrimSpace(line) != want {
			t.Fatalf("text protocol: got %q want %q", line, want)
		}
	}
}

func TestLegacyTextProtocol(t *testing.T) {
	ts := startServer(t, testConfig(t))
	conn, err := net.Dial("tcp", ts.tcp)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	cases := []struct{ cmd, want string }{
		{"PING", "PONG"},
		{"SET user:1 John", "OK"},
		{"GET user:1", "VALUE John"},
		{"SET session abc 5m", "OK"},
		{"GET missing", "NIL"},
		{"LPUSH mylist item1 item2", "INTEGER 2"},
		{"LPOP mylist", "VALUE item2"},
		{"SADD myset member1", "INTEGER 1"},
		{"SMEMBERS myset", "ARRAY member1"},
		{"HSET myhash field1 value1", "INTEGER 1"},
		{"HGET myhash field1", "VALUE value1"},
		{`SET quoted "hello world"`, "OK"},
		{"GET quoted", "VALUE hello world"},
		{"EVAL \"return redis.call('GET', KEYS[1])\" 1 user:1", "RESULT John"},
		{"GET myhash", "ERROR: WRONGTYPE Operation against a key holding the wrong kind of value"},
		{"PUBLISH ch hi", "PUBLISHED 0"},
		{"DEL user:1", "INTEGER 1"},
	}
	for _, c := range cases {
		fmt.Fprintf(conn, "%s\r\n", c.cmd)
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimRight(line, "\r\n"); got != c.want {
			t.Errorf("%s: got %q want %q", c.cmd, got, c.want)
		}
	}
	// TTL given as a Go duration (1.x syntax) is honoured.
	fmt.Fprint(conn, "TTL session\n")
	if line, _ := r.ReadString('\n'); strings.TrimSpace(line) != "INTEGER 300" {
		t.Errorf("TTL session = %q", line)
	}

	// The same port speaks RESP to RESP clients.
	rdb := goRedis(t, ts.tcp, "")
	if v := rdb.Get(context.Background(), "quoted").Val(); v != "hello world" {
		t.Fatalf("RESP on tcp port = %q", v)
	}

	// Pub/sub in text mode delivers "MESSAGE <channel> <message>".
	sub, err := net.Dial("tcp", ts.tcp)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	sr := bufio.NewReader(sub)
	fmt.Fprint(sub, "SUBSCRIBE alerts\n")
	if line, _ := sr.ReadString('\n'); strings.TrimSpace(line) != "SUBSCRIBED alerts" {
		t.Fatalf("subscribe reply = %q", line)
	}
	fmt.Fprint(conn, "PUBLISH alerts disk full\n")
	_, _ = r.ReadString('\n')
	_ = sub.SetReadDeadline(time.Now().Add(3 * time.Second))
	if line, _ := sr.ReadString('\n'); strings.TrimSpace(line) != "MESSAGE alerts disk full" {
		t.Fatalf("message = %q", line)
	}
}

// TestFragmentedAndPipelinedInput sends commands split at every byte and
// many commands in one write; 1.x silently dropped partially received
// commands.
func TestFragmentedAndPipelinedInput(t *testing.T) {
	ts := startServer(t, testConfig(t))
	conn, err := net.Dial("tcp", ts.resp)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r := bufio.NewReader(conn)

	payload := strings.Repeat("v", 100000)
	cmd := resp.AppendCommand(nil, "SET", "big", payload)
	for i := 0; i < len(cmd); i += 7919 {
		end := min(i+7919, len(cmd))
		if _, err := conn.Write(cmd[i:end]); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	if v, err := resp.ReadValue(r); err != nil || v.Str != "OK" {
		t.Fatalf("fragmented SET = %+v %v", v, err)
	}

	var batch []byte
	for i := 0; i < 5000; i++ {
		batch = resp.AppendCommand(batch, "INCR", "n")
	}
	batch = resp.AppendCommand(batch, "STRLEN", "big")
	if _, err := conn.Write(batch); err != nil {
		t.Fatal(err)
	}
	var last resp.Value
	for i := 0; i <= 5000; i++ {
		if last, err = resp.ReadValue(r); err != nil {
			t.Fatal(err)
		}
	}
	if last.Int != 100000 {
		t.Fatalf("STRLEN = %+v", last)
	}

	// Protocol errors close the connection with an error reply.
	if _, err := conn.Write([]byte("*1\r\n$-5\r\n")); err != nil {
		t.Fatal(err)
	}
	v, _ := resp.ReadValue(r)
	if v.Kind != resp.KindError || !strings.Contains(v.Str, "Protocol error") {
		t.Fatalf("protocol error reply = %+v", v)
	}
}

func TestPersistenceAcrossRestart(t *testing.T) {
	cfg := testConfig(t)
	ts := startServer(t, cfg)
	ctx := context.Background()
	rdb := goRedis(t, ts.resp, "")
	rdb.Set(ctx, "bin", "\x00\x01\xff", 0)
	rdb.HSet(ctx, "user", "name", "Ada")
	rdb.SAdd(ctx, "tags", "go", "cache")
	rdb.RPush(ctx, "jobs", "a", "b")
	rdb.Set(ctx, "session", "x", time.Hour)
	rdb.Close()
	ts.stop(t)

	// Restart on new ports with the same data file.
	cfg2 := testConfig(t)
	cfg2.Persistence.File = cfg.Persistence.File
	ts2 := startServer(t, cfg2)
	rdb2 := goRedis(t, ts2.resp, "")
	if v := rdb2.Get(ctx, "bin").Val(); v != "\x00\x01\xff" {
		t.Errorf("binary value = %q", v)
	}
	// 1.x lost hash fields when writing to a hash loaded from disk.
	rdb2.HSet(ctx, "user", "lang", "Go")
	if m := rdb2.HGetAll(ctx, "user").Val(); m["name"] != "Ada" || m["lang"] != "Go" {
		t.Errorf("hash after restart = %v", m)
	}
	if n := rdb2.SCard(ctx, "tags").Val(); n != 2 {
		t.Errorf("set after restart has %d members", n)
	}
	if v := rdb2.LRange(ctx, "jobs", 0, -1).Val(); strings.Join(v, ",") != "a,b" {
		t.Errorf("list after restart = %v", v)
	}
	if ttl := rdb2.TTL(ctx, "session").Val(); ttl < 59*time.Minute {
		t.Errorf("ttl after restart = %v", ttl)
	}
}

func TestRESTAPI(t *testing.T) {
	ts := startServer(t, testConfig(t))
	do := func(method, path string, body any) (int, map[string]any) {
		t.Helper()
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, ts.rest+path, rd)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}

	if code, out := do("GET", "/ping", nil); code != 200 || out["value"] != "PONG" {
		t.Fatalf("/ping = %d %v", code, out)
	}
	if code, _ := do("PUT", "/cache/user:1", map[string]any{"value": "John Doe"}); code != 200 {
		t.Fatalf("PUT = %d", code)
	}
	if code, out := do("GET", "/cache/user:1", nil); code != 200 || out["value"] != "John Doe" {
		t.Fatalf("GET = %d %v", code, out)
	}
	if code, _ := do("PUT", "/cache/session:abc", map[string]any{"value": "active", "ttl": "5m"}); code != 200 {
		t.Fatalf("PUT ttl = %d", code)
	}
	if code, out := do("GET", "/cache/session:abc/ttl", nil); code != 200 || out["value"].(map[string]any)["ttl_ms"].(float64) < 299000 {
		t.Fatalf("TTL = %d %v", code, out)
	}
	if code, _ := do("PUT", "/cache/x", map[string]any{"value": "v", "ttl": "soon"}); code != 400 {
		t.Fatalf("bad ttl = %d", code)
	}
	if code, _ := do("PUT", "/cache/x", "not an object"); code != 400 {
		t.Fatalf("bad json = %d", code)
	}
	if code, _ := do("GET", "/cache/missing", nil); code != 404 {
		t.Fatalf("missing = %d", code)
	}
	if code, out := do("POST", "/list/mylist", []string{"a", "b"}); code != 200 || out["count"].(float64) != 2 {
		t.Fatalf("list push = %d %v", code, out)
	}
	if code, out := do("GET", "/list/mylist", nil); code != 200 || fmt.Sprint(out["value"]) != "[b a]" {
		t.Fatalf("list range = %d %v", code, out)
	}
	if code, out := do("DELETE", "/list/mylist", nil); code != 200 || out["value"] != "b" {
		t.Fatalf("list pop = %d %v", code, out)
	}
	do("POST", "/set/myset", []string{"m1", "m2"})
	if code, out := do("GET", "/set/myset", nil); code != 200 || fmt.Sprint(out["value"]) != "[m1 m2]" {
		t.Fatalf("set members = %d %v", code, out)
	}
	do("PUT", "/hash/user:2/name", map[string]any{"value": "Ada"})
	do("PUT", "/hash/user:2/year", map[string]any{"value": 1815})
	if code, out := do("GET", "/hash/user:2", nil); code != 200 || fmt.Sprint(out["value"]) != "map[name:Ada year:1815]" {
		t.Fatalf("hash getall = %d %v", code, out)
	}
	if code, out := do("GET", "/cache/user:2", nil); code != 200 || fmt.Sprint(out["value"]) != "map[name:Ada year:1815]" {
		t.Fatalf("GET on hash = %d %v", code, out)
	}
	if code, _ := do("GET", "/hash/user:1/name", nil); code != 409 {
		t.Fatalf("WRONGTYPE status = %d", code)
	}
	if code, out := do("POST", "/eval", map[string]any{
		"script": "return redis.call('SET', KEYS[1], ARGV[1])", "keys": []string{"scriptkey"}, "args": []string{"v"},
	}); code != 200 || out["success"] != true {
		t.Fatalf("eval = %d %v", code, out)
	}
	if code, out := do("POST", "/command", map[string]any{"args": []string{"MGET", "scriptkey", "nope"}}); code != 200 || fmt.Sprint(out["value"]) != "[v <nil>]" {
		t.Fatalf("command = %d %v", code, out)
	}
	if code, out := do("GET", "/keys?pattern=user:*", nil); code != 200 || out["count"].(float64) != 2 {
		t.Fatalf("keys = %d %v", code, out)
	}
	if code, out := do("GET", "/info", nil); code != 200 || out["value"].(map[string]any)["keys"].(float64) < 5 {
		t.Fatalf("info = %d %v", code, out)
	}
	if code, _ := do("DELETE", "/cache/user:1", nil); code != 200 {
		t.Fatalf("delete = %d", code)
	}
	if code, _ := do("GET", "/auth/tokens", nil); code != 404 {
		t.Fatalf("tokens with auth disabled = %d", code)
	}

	res, err := http.Get(ts.rest + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	for _, want := range []string{"boltcache_keys ", "boltcache_commands_processed_total", "boltcache_http_responses_total{code=\"2xx\"}"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("/metrics missing %q", want)
		}
	}
	for _, path := range []string{"/", "/docs", "/openapi.json"} {
		res, err := http.Get(ts.rest + path)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("%s = %v %v", path, res.StatusCode, err)
		}
		res.Body.Close()
	}

	// Oversized bodies are rejected.
	big := map[string]any{"value": strings.Repeat("x", 11<<20)}
	if code, _ := do("PUT", "/cache/big", big); code != 400 {
		t.Fatalf("oversized body = %d", code)
	}
}

func TestWebSocketPubSub(t *testing.T) {
	ts := startServer(t, testConfig(t))
	wsURL := strings.Replace(ts.rest, "http://", "ws://", 1) + "/subscribe/news"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Publish from RESP and REST; both reach the WebSocket subscriber.
	deadline := time.Now().Add(3 * time.Second)
	rdb := goRedis(t, ts.resp, "")
	for rdb.PubSubNumSub(context.Background(), "news").Val()["news"] == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	rdb.Publish(context.Background(), "news", "from-resp")
	b, _ := json.Marshal(map[string]string{"message": "from-rest"})
	res, err := http.Post(ts.rest+"/publish/news", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for _, want := range []string{"from-resp", "from-rest"} {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if string(msg) != want {
			t.Fatalf("got %q want %q", msg, want)
		}
	}

	// Cross-origin WebSocket from a disallowed origin is rejected.
	cfg := testConfig(t)
	cfg.Server.REST.CORSOrigins = []string{"https://app.example.com"}
	ts2 := startServer(t, cfg)
	h := http.Header{"Origin": {"https://evil.example.com"}}
	if _, _, err := websocket.DefaultDialer.Dial(strings.Replace(ts2.rest, "http://", "ws://", 1)+"/subscribe/x", h); err == nil {
		t.Fatal("disallowed origin accepted")
	}
}

func TestMaxConnections(t *testing.T) {
	cfg := testConfig(t)
	cfg.Server.TCP.MaxConnections = 2
	ts := startServer(t, cfg)
	var conns []net.Conn
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	for i := 0; i < 2; i++ {
		c, err := net.Dial("tcp", ts.resp)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
		fmt.Fprint(c, "PING\r\n")
		_, _ = bufio.NewReader(c).ReadString('\n')
	}
	c, err := net.Dial("tcp", ts.resp)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, _ := bufio.NewReader(c).ReadString('\n')
	if !strings.Contains(line, "max number of clients") {
		t.Fatalf("third connection got %q", line)
	}
}

func TestConcurrentClients(t *testing.T) {
	ts := startServer(t, testConfig(t))
	ctx := context.Background()
	rdb := goRedis(t, ts.resp, "")
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				key := fmt.Sprintf("c:%d", i%20)
				rdb.HIncrBy(ctx, key, "n", 1)
				rdb.HGetAll(ctx, key)
			}
		}(g)
	}
	wg.Wait()
	total := int64(0)
	for i := 0; i < 20; i++ {
		n, _ := rdb.HGet(ctx, fmt.Sprintf("c:%d", i), "n").Int64()
		total += n
	}
	if total != 16*200 {
		t.Fatalf("lost updates: %d", total)
	}
}

func TestCSRFAndUnauthenticatedLimits(t *testing.T) {
	cfg := testConfig(t)
	cfg.Server.REST.CORSOrigins = []string{"https://app.example.com"}
	ts := startServer(t, cfg)
	post := func(origin string) int {
		req, _ := http.NewRequest("POST", ts.rest+"/command", strings.NewReader(`["SET","x","1"]`))
		req.Header.Set("Content-Type", "text/plain")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := post("https://evil.example.com"); code != 403 {
		t.Fatalf("cross-site POST = %d", code)
	}
	if code := post("https://app.example.com"); code != 200 {
		t.Fatalf("allowed origin POST = %d", code)
	}
	if code := post(""); code != 200 {
		t.Fatalf("POST without Origin = %d", code)
	}
	if code := post(ts.rest); code != 200 {
		t.Fatalf("same-origin POST = %d", code)
	}

	// With auth on, unauthenticated clients can't send large requests.
	cfg2 := testConfig(t)
	cfg2.Security.Auth.Enabled = true
	cfg2.Security.Auth.Tokens = []string{"tok"}
	ts2 := startServer(t, cfg2)
	conn, err := net.Dial("tcp", ts2.resp)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = conn.Write(resp.AppendCommand(nil, "SET", "k", strings.Repeat("x", 1<<20)))
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	v, err := resp.ReadValue(bufio.NewReader(conn))
	if err != nil || !strings.Contains(v.Str, "Protocol error") {
		t.Fatalf("unauthenticated big request: %+v %v", v, err)
	}
	if err := goRedis(t, ts2.resp, "tok").Set(context.Background(), "k", strings.Repeat("x", 1<<20), 0).Err(); err != nil {
		t.Fatalf("authenticated big request: %v", err)
	}
}

func TestRESPListenerFollowsTCPHost(t *testing.T) {
	cfg := config.Default()
	cfg.Server.TCP.Host = "127.0.0.1"
	if got := cfg.GetRESPAddress(); got != "127.0.0.1:6382" {
		t.Fatalf("RESP address = %s", got)
	}
}
