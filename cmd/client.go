package cmd

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var clientAddr string

// Root client command
var clientCmd = &cobra.Command{
	Use:   "client",
	Short: "Interact with BoltCache server",
}

// Interactive mode
var interactiveCmd = &cobra.Command{
	Use:   "interactive",
	Short: "Start interactive client session",
	Run: func(cmd *cobra.Command, args []string) {
		runInteractive(clientAddr)
	},
}

// Benchmark mode
var benchmarkCmd = &cobra.Command{
	Use:   "benchmark",
	Short: "Run performance benchmark",
	Run: func(cmd *cobra.Command, args []string) {
		runBenchmark(clientAddr)
	},
}

// Test features
var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Test BoltCache features",
	Run: func(cmd *cobra.Command, args []string) {
		runTest(clientAddr)
	},
}

func init() {
	clientCmd.PersistentFlags().StringVar(&clientAddr, "addr", "localhost:6380", "BoltCache server address")

	clientCmd.AddCommand(interactiveCmd)
	clientCmd.AddCommand(benchmarkCmd)
	clientCmd.AddCommand(testCmd)
}

// --- Implementations ---

func runInteractive(addr string) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	fmt.Println("Welcome to BoltCache Interactive Client!")
	fmt.Println("Type commands to interact with the cache server.")
	fmt.Println("Type 'quit' to exit.")
	fmt.Println("\nAvailable command types:")
	fmt.Println("  Strings: SET key value [ttl], GET key, DEL key")
	fmt.Println("  Lists: LPUSH key value, LPOP key")
	fmt.Println("  Sets: SADD key member, SMEMBERS key")
	fmt.Println("  Hashes: HSET key field value, HGET key field")
	fmt.Println("  Pub/Sub: SUBSCRIBE channel, PUBLISH channel message")
	fmt.Println("  Scripts: EVAL script numkeys key arg")
	fmt.Println("  Info: INFO, PING")

	scanner := bufio.NewScanner(os.Stdin)
	reader := bufio.NewReader(conn)

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "quit" {
			break
		}

		fmt.Fprintln(conn, input)
		resp, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error reading response:", err)
			break
		}

		fmt.Print(strings.TrimSpace(resp) + "\n")
	}
}

func runBenchmark(addr string) {
	fmt.Println("Running BoltCache benchmark...")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// SET benchmark
	for i := 0; i < 10000; i++ {
		fmt.Fprintf(conn, "SET key%d value%d\n", i, i)
		reader.ReadString('\n')
	}

	// GET benchmark
	for i := 0; i < 10000; i++ {
		fmt.Fprintf(conn, "GET key%d\n", i)
		reader.ReadString('\n')
	}

	// List benchmark
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(conn, "LPUSH list%d item%d\n", i%10, i)
		reader.ReadString('\n')
	}

	// Hash benchmark
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(conn, "HSET hash%d field%d value%d\n", i%10, i, i)
		reader.ReadString('\n')
	}

	fmt.Println("Benchmark completed: 22,000 operations")
}

func runTest(addr string) {
	fmt.Println("Testing BoltCache features...")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)

	tests := []string{
		"SET user:1 John",
		"GET user:1",
		"LPUSH mylist item1 item2",
		"LPOP mylist",
		"SADD myset member1 member2",
		"SMEMBERS myset",
		"HSET myhash field1 value1",
		"HGET myhash field1",
		"EVAL 'redis.call(\"SET\", KEYS[1], ARGV[1])' 1 testkey testvalue",
		"GET testkey",
		"INFO",
	}

	for _, t := range tests {
		fmt.Printf("> %s\n", t)
		fmt.Fprintln(conn, t)
		resp, _ := reader.ReadString('\n')
		fmt.Printf("< %s", resp)
		fmt.Println()
	}

	fmt.Println("Feature tests completed!")
}
