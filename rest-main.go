package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	var (
		restMode    = flag.Bool("rest", false, "Start REST API server")
		port        = flag.Int("port", 8080, "REST API port")
		persistFile = flag.String("persist", "./data/boltcache.json", "Persistence file")
	)
	flag.Parse()

	if *restMode {
		// REST API mode
		cache := NewBoltCache(*persistFile)
		server := NewRestServer(cache)
		server.Start(*port)
	} else {
		// TCP mode (existing)
		cache := NewBoltCache(*persistFile)

		// Add replicas if specified
		for _, replica := range flag.Args() {
			cache.AddReplica(replica)
		}

		startTCPServer(cache)
	}
}

func startTCPServer(cache *BoltCache) {
	// Existing TCP server code would go here
	// For now, just show usage
	fmt.Println("TCP mode not implemented in this file")
	fmt.Println("Use: go run main.go lua.go")
	os.Exit(1)
}