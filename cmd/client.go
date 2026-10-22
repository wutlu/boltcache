package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/wutlu/boltcache/v2/internal/resp"
)

var (
	clientAddr     string
	clientPassword string

	benchClients  int
	benchRequests int
	benchPipeline int
	benchSize     int
	benchKeyspace int
	benchTests    string
)

var clientCmd = &cobra.Command{
	Use:   "client",
	Short: "Interact with a BoltCache (or Redis) server over RESP",
}

var interactiveCmd = &cobra.Command{
	Use:   "interactive",
	Short: "Start an interactive session (like redis-cli)",
	RunE:  func(cmd *cobra.Command, args []string) error { return runInteractive() },
}

var benchmarkCmd = &cobra.Command{
	Use:   "benchmark",
	Short: "Measure throughput and latency with parallel, pipelined clients",
	RunE:  func(cmd *cobra.Command, args []string) error { return runBenchmark() },
}

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Run a quick feature check against a server",
	RunE:  func(cmd *cobra.Command, args []string) error { return runFeatureTest() },
}

func init() {
	clientCmd.PersistentFlags().StringVarP(&clientAddr, "addr", "a", "localhost:6382", "server address (RESP port)")
	clientCmd.PersistentFlags().StringVarP(&clientPassword, "password", "p", os.Getenv("BOLTCACHE_TOKEN"), "auth token (env BOLTCACHE_TOKEN)")

	f := benchmarkCmd.Flags()
	f.IntVarP(&benchClients, "clients", "n", 50, "parallel connections")
	f.IntVarP(&benchRequests, "requests", "r", 200000, "total requests per test")
	f.IntVarP(&benchPipeline, "pipeline", "P", 1, "commands per pipeline batch")
	f.IntVarP(&benchSize, "size", "d", 32, "value size in bytes")
	f.IntVar(&benchKeyspace, "keyspace", 100000, "number of distinct keys")
	f.StringVarP(&benchTests, "tests", "t", "set,get", "comma-separated tests: set,get,incr,lpush,hset,ping")

	clientCmd.AddCommand(interactiveCmd, benchmarkCmd, testCmd)
}

type client struct {
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
}

func dial(addr, password string) (*client, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	c := &client{conn: conn, r: bufio.NewReaderSize(conn, 64<<10), w: bufio.NewWriterSize(conn, 64<<10)}
	if password != "" {
		v, err := c.do("AUTH", password)
		if err != nil {
			conn.Close()
			return nil, err
		}
		if v.Kind == resp.KindError {
			conn.Close()
			return nil, errors.New(v.Str)
		}
	}
	return c, nil
}

func (c *client) send(args ...string) error {
	_, err := c.w.Write(resp.AppendCommand(nil, args...))
	return err
}

func (c *client) do(args ...string) (resp.Value, error) {
	if err := c.send(args...); err != nil {
		return resp.Value{}, err
	}
	if err := c.w.Flush(); err != nil {
		return resp.Value{}, err
	}
	return resp.ReadValue(c.r)
}

func (c *client) Close() error { return c.conn.Close() }

func runInteractive() error {
	c, err := dial(clientAddr, clientPassword)
	if err != nil {
		return err
	}
	defer c.Close()
	fmt.Printf("Connected to %s. Type commands (e.g. SET greeting \"hello world\"), 'help' or 'quit'.\n", clientAddr)
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 64<<20)
	prompt := clientAddr + "> "
	for {
		fmt.Print(prompt)
		if !in.Scan() {
			fmt.Println()
			return in.Err()
		}
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		if strings.EqualFold(line, "help") {
			fmt.Println("Any Redis command works, e.g.:\n  SET k v EX 60 | GET k | HSET user name Ada | LPUSH q job | SUBSCRIBE news\n  EVAL \"return redis.call('INCR', KEYS[1])\" 1 counter\n  INFO | DBSIZE | KEYS * | quit")
			continue
		}
		argv, _, _, perr := resp.ParseCommand(nil, []byte(line+"\n"))
		if perr != nil {
			fmt.Println("(error)", perr)
			continue
		}
		args := make([]string, len(argv))
		for i, a := range argv {
			args[i] = string(a)
		}
		v, err := c.do(args...)
		if err != nil {
			return err
		}
		fmt.Println(v.String())
		name := strings.ToUpper(args[0])
		if name == "QUIT" {
			return nil
		}
		if name == "SUBSCRIBE" || name == "PSUBSCRIBE" {
			// Print messages until the connection closes (Ctrl+C to exit).
			for i := 1; i < len(args)-1; i++ {
				if v, err = resp.ReadValue(c.r); err != nil {
					return err
				}
				fmt.Println(v.String())
			}
			fmt.Println("Reading messages... (press Ctrl+C to quit)")
			for {
				v, err := resp.ReadValue(c.r)
				if err != nil {
					return err
				}
				fmt.Println(v.String())
			}
		}
	}
}

func runBenchmark() error {
	if benchClients < 1 || benchRequests < 1 || benchPipeline < 1 {
		return errors.New("clients, requests and pipeline must be >= 1")
	}
	value := strings.Repeat("x", benchSize)
	fmt.Printf("BoltCache benchmark: %s, %d clients, %d requests, pipeline %d, %d byte values\n\n",
		clientAddr, benchClients, benchRequests, benchPipeline, benchSize)
	for _, t := range strings.Split(benchTests, ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		var gen func(i int) []string
		switch t {
		case "set":
			gen = func(i int) []string { return []string{"SET", "key:" + strconv.Itoa(i%benchKeyspace), value} }
		case "get":
			gen = func(i int) []string { return []string{"GET", "key:" + strconv.Itoa(i%benchKeyspace)} }
		case "incr":
			gen = func(i int) []string { return []string{"INCR", "counter:" + strconv.Itoa(i%benchKeyspace)} }
		case "lpush":
			gen = func(i int) []string { return []string{"LPUSH", "list:" + strconv.Itoa(i%100), value} }
		case "hset":
			gen = func(i int) []string {
				return []string{"HSET", "hash:" + strconv.Itoa(i%1000), "f" + strconv.Itoa(i%benchKeyspace), value}
			}
		case "ping":
			gen = func(int) []string { return []string{"PING"} }
		default:
			return fmt.Errorf("unknown test %q", t)
		}
		if err := benchOne(strings.ToUpper(t), gen); err != nil {
			return err
		}
	}
	return nil
}

func benchOne(name string, gen func(i int) []string) error {
	var (
		next      atomic.Int64
		wg        sync.WaitGroup
		mu        sync.Mutex
		latencies []time.Duration
		firstErr  error
	)
	total := int64(benchRequests)
	start := time.Now()
	for c := 0; c < benchClients; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cl, err := dial(clientAddr, clientPassword)
			if err != nil {
				mu.Lock()
				firstErr = err
				mu.Unlock()
				return
			}
			defer cl.Close()
			var local []time.Duration
			for {
				begin := next.Add(int64(benchPipeline)) - int64(benchPipeline)
				if begin >= total {
					break
				}
				n := min(int64(benchPipeline), total-begin)
				t0 := time.Now()
				for i := int64(0); i < n; i++ {
					_ = cl.send(gen(int(begin + i))...)
				}
				if err := cl.w.Flush(); err != nil {
					mu.Lock()
					firstErr = err
					mu.Unlock()
					return
				}
				for i := int64(0); i < n; i++ {
					v, err := resp.ReadValue(cl.r)
					if err != nil || v.Kind == resp.KindError {
						mu.Lock()
						if err == nil {
							err = errors.New(v.Str)
						}
						firstErr = err
						mu.Unlock()
						return
					}
				}
				local = append(local, time.Since(t0))
			}
			mu.Lock()
			latencies = append(latencies, local...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	if firstErr != nil {
		return fmt.Errorf("%s: %w", name, firstErr)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	pct := func(p float64) time.Duration {
		if len(latencies) == 0 {
			return 0
		}
		return latencies[min(int(p*float64(len(latencies))), len(latencies)-1)]
	}
	fmt.Printf("%-6s %12.0f ops/sec   p50 %-10v p99 %-10v max %v\n", name,
		float64(total)/elapsed.Seconds(), pct(0.50).Round(time.Microsecond),
		pct(0.99).Round(time.Microsecond), pct(1).Round(time.Microsecond))
	return nil
}

func runFeatureTest() error {
	c, err := dial(clientAddr, clientPassword)
	if err != nil {
		return err
	}
	defer c.Close()
	tests := [][]string{
		{"PING"},
		{"SET", "bc:test:user", "Ada", "EX", "60"},
		{"GET", "bc:test:user"},
		{"TTL", "bc:test:user"},
		{"INCR", "bc:test:counter"},
		{"RPUSH", "bc:test:list", "a", "b", "c"},
		{"LRANGE", "bc:test:list", "0", "-1"},
		{"SADD", "bc:test:set", "x", "y"},
		{"SMEMBERS", "bc:test:set"},
		{"HSET", "bc:test:hash", "name", "Ada", "lang", "Go"},
		{"HGETALL", "bc:test:hash"},
		{"EVAL", "return redis.call('INCRBY', KEYS[1], ARGV[1])", "1", "bc:test:counter", "10"},
		{"MULTI"}, {"INCR", "bc:test:counter"}, {"GET", "bc:test:counter"}, {"EXEC"},
		{"DEL", "bc:test:user", "bc:test:counter", "bc:test:list", "bc:test:set", "bc:test:hash"},
		{"DBSIZE"},
	}
	failed := 0
	for _, t := range tests {
		v, err := c.do(t...)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return errors.New("server closed the connection")
			}
			return err
		}
		mark := "✓"
		if v.Kind == resp.KindError {
			mark, failed = "✗", failed+1
		}
		fmt.Printf("%s %s\n  %s\n", mark, strings.Join(t, " "), strings.ReplaceAll(v.String(), "\n", "\n  "))
	}
	if failed > 0 {
		return fmt.Errorf("%d command(s) failed", failed)
	}
	fmt.Println("\nAll feature checks passed.")
	return nil
}
