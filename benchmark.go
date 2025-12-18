package main

import (
	"bufio"
	"fmt"
	"net"
	"sync"
	"time"
)

func benchmarkBoltCacheTCP(operations int, concurrency int) {
	fmt.Printf("BoltCache TCP Benchmark: %d operations, %d concurrent connections\n", operations, concurrency)
	
	var wg sync.WaitGroup
	start := time.Now()
	
	opsPerWorker := operations / concurrency
	
	// SET benchmark
	fmt.Println("Running SET operations...")
	setStart := time.Now()
	
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			
			conn, err := net.Dial("tcp", "localhost:6380")
			if err != nil {
				fmt.Printf("Failed to connect: %v\n", err)
				return
			}
			defer conn.Close()
			
			writer := bufio.NewWriter(conn)
			reader := bufio.NewReader(conn)
			
			for j := 0; j < opsPerWorker; j++ {
				key := fmt.Sprintf("key%d_%d", workerID, j)
				value := fmt.Sprintf("value%d_%d", workerID, j)
				
				fmt.Fprintf(writer, "SET %s %s\n", key, value)
				writer.Flush()
				
				// Read response
				reader.ReadLine()
			}
		}(i)
	}
	
	wg.Wait()
	setDuration := time.Since(setStart)
	setOpsPerSec := float64(operations) / setDuration.Seconds()
	
	fmt.Printf("SET: %.2f ops/sec, %.2f ms avg\n", setOpsPerSec, setDuration.Seconds()*1000/float64(operations))
	
	// GET benchmark
	fmt.Println("Running GET operations...")
	getStart := time.Now()
	
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			
			conn, err := net.Dial("tcp", "localhost:6380")
			if err != nil {
				fmt.Printf("Failed to connect: %v\n", err)
				return
			}
			defer conn.Close()
			
			writer := bufio.NewWriter(conn)
			reader := bufio.NewReader(conn)
			
			for j := 0; j < opsPerWorker; j++ {
				key := fmt.Sprintf("key%d_%d", workerID, j)
				
				fmt.Fprintf(writer, "GET %s\n", key)
				writer.Flush()
				
				// Read response
				reader.ReadLine()
			}
		}(i)
	}
	
	wg.Wait()
	getDuration := time.Since(getStart)
	getOpsPerSec := float64(operations) / getDuration.Seconds()
	
	fmt.Printf("GET: %.2f ops/sec, %.2f ms avg\n", getOpsPerSec, getDuration.Seconds()*1000/float64(operations))
	
	totalDuration := time.Since(start)
	fmt.Printf("Total time: %.2f seconds\n", totalDuration.Seconds())
}

func main() {
	// Test connection first
	conn, err := net.Dial("tcp", "localhost:6380")
	if err != nil {
		fmt.Printf("Cannot connect to BoltCache TCP server: %v\n", err)
		fmt.Println("Make sure BoltCache is running with TCP support on port 6380")
		return
	}
	conn.Close()
	
	fmt.Println("=== BoltCache TCP Benchmark ===")
	benchmarkBoltCacheTCP(10000, 50)
}