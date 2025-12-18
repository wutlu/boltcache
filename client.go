package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run client.go <command>")
		fmt.Println("Commands:")
		fmt.Println("  interactive - Start interactive mode")
		fmt.Println("  benchmark - Run performance benchmark")
		fmt.Println("  test - Run feature tests")
		return
	}

	switch os.Args[1] {
	case "interactive":
		interactive()
	case "benchmark":
		benchmark()
	case "test":
		testFeatures()
	default:
		fmt.Println("Unknown command:", os.Args[1])
	}
}

func testFeatures() {
	fmt.Println("Testing BoltCache features...")
	
	conn, err := net.Dial("tcp", "localhost:6380")
	if err != nil {
		fmt.Println("Failed to connect:", err)
		return
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
	
	for _, test := range tests {
		fmt.Printf("> %s\n", test)
		fmt.Fprintln(conn, test)
		response, _ := reader.ReadString('\n')
		fmt.Printf("< %s", response)
		fmt.Println()
	}
	
	fmt.Println("Feature tests completed!")
}

func interactive() {
	conn, err := net.Dial("tcp", "localhost:6380")
	if err != nil {
		fmt.Println("Failed to connect:", err)
		return
	}
	defer conn.Close()

	fmt.Println("Connected to BoltCache. Type 'quit' to exit.")
	fmt.Println("Commands:")
	fmt.Println("  String: SET key value [ttl], GET key, DEL key")
	fmt.Println("  List: LPUSH key value, LPOP key")
	fmt.Println("  Set: SADD key member, SMEMBERS key")
	fmt.Println("  Hash: HSET key field value, HGET key field")
	fmt.Println("  Pub/Sub: SUBSCRIBE channel, PUBLISH channel message")
	fmt.Println("  Script: EVAL script numkeys key arg")
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
		
		response, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error reading response:", err)
			break
		}
		
		fmt.Print(strings.TrimSpace(response), "\n")
	}
}

func benchmark() {
	fmt.Println("Running BoltCache benchmark...")
	
	conn, err := net.Dial("tcp", "localhost:6380")
	if err != nil {
		fmt.Println("Failed to connect:", err)
		return
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	
	// SET benchmark
	fmt.Println("Testing SET operations...")
	for i := 0; i < 10000; i++ {
		fmt.Fprintf(conn, "SET key%d value%d\n", i, i)
		reader.ReadString('\n')
	}
	
	// GET benchmark  
	fmt.Println("Testing GET operations...")
	for i := 0; i < 10000; i++ {
		fmt.Fprintf(conn, "GET key%d\n", i)
		reader.ReadString('\n')
	}
	
	// List benchmark
	fmt.Println("Testing LIST operations...")
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(conn, "LPUSH list%d item%d\n", i%10, i)
		reader.ReadString('\n')
	}
	
	// Hash benchmark
	fmt.Println("Testing HASH operations...")
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(conn, "HSET hash%d field%d value%d\n", i%10, i, i)
		reader.ReadString('\n')
	}
	
	fmt.Println("Benchmark completed: 22,000 operations")
}