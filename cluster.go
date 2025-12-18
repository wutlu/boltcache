package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type ClusterNode struct {
	ID      string
	Address string
	Port    int
	Role    string // master, slave
	cache   *BoltCache
}

func NewClusterNode(id, address string, port int, role string) *ClusterNode {
	return &ClusterNode{
		ID:      id,
		Address: address,
		Port:    port,
		Role:    role,
		cache:   NewBoltCache(fmt.Sprintf("./data/node_%s.json", id)),
	}
}

func (n *ClusterNode) Start() {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", n.Port))
	if err != nil {
		log.Fatal("Failed to start cluster node:", err)
	}
	defer listener.Close()

	fmt.Printf("Cluster node %s (%s) started on :%d\n", n.ID, n.Role, n.Port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Failed to accept connection:", err)
			continue
		}

		go n.cache.handleConnection(conn)
	}
}

func (n *ClusterNode) JoinCluster(masterAddr string) {
	conn, err := net.Dial("tcp", masterAddr)
	if err != nil {
		log.Printf("Failed to join cluster: %v", err)
		return
	}
	defer conn.Close()

	fmt.Fprintf(conn, "CLUSTER JOIN %s %s:%d\n", n.ID, n.Address, n.Port)
	fmt.Printf("Joined cluster with master %s\n", masterAddr)
}

// Cluster management commands
func startCluster() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: go run . cluster <node-id> <port> [master-address]")
		return
	}

	nodeID := os.Args[2]
	port, _ := strconv.Atoi(os.Args[3])
	
	var role string
	var masterAddr string
	
	if len(os.Args) > 4 {
		role = "slave"
		masterAddr = os.Args[4]
	} else {
		role = "master"
	}

	node := NewClusterNode(nodeID, "localhost", port, role)
	
	if role == "slave" {
		go func() {
			time.Sleep(2 * time.Second)
			node.JoinCluster(masterAddr)
		}()
	}
	
	node.Start()
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "cluster" {
		startCluster()
		return
	}

	// Normal single node mode
	persistFile := "./data/boltcache.json"
	if len(os.Args) > 1 {
		persistFile = os.Args[1]
	}

	cache := NewBoltCache(persistFile)

	// Add replicas if specified
	if len(os.Args) > 2 {
		for i := 2; i < len(os.Args); i++ {
			cache.AddReplica(os.Args[i])
		}
	}

	listener, err := net.Listen("tcp", ":6380")
	if err != nil {
		log.Fatal("Failed to start server:", err)
	}
	defer listener.Close()

	fmt.Println("BoltCache server started on :6380")
	fmt.Printf("Persistence: %s\n", persistFile)
	fmt.Printf("Replicas: %v\n", cache.replicas)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Failed to accept connection:", err)
			continue
		}

		go cache.handleConnection(conn)
	}
}