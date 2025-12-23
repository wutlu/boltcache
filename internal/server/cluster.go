package server

import (
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"time"
)

import (
	config "boltcache/config"
	cache "boltcache/internal/cache"
	logger "boltcache/logger"
)

type ClusterNode struct {
	ID      string
	Address string
	Port    int
	Role    string // master, slave
	Cache *cache.BoltCache
}

func NewClusterNode(id, address string, port int, role string) *ClusterNode {
	return &ClusterNode{
		ID:      id,
		Address: address,
		Port:    port,
		Role:    role,
		Cache:   cache.NewBoltCache(fmt.Sprintf("./data/node_%s.json", id)),
	}
}

func (n *ClusterNode) Start(cfg *config.Config) {


	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", n.Port))
	if err != nil {
		log.Fatal("Failed to start cluster node:", err)
	}
	defer listener.Close()

	fmt.Printf("Cluster node %s (%s) started on :%d\n", n.ID, n.Role, n.Port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			logger.Log("Failed to accept connection: %v", err)
			continue
		}

		go handleConnection(conn, n.Cache)
	}
}

func (n *ClusterNode) JoinCluster(masterAddr string) {
	conn, err := net.Dial("tcp", masterAddr)
	if err != nil {
		logger.Log("Failed to join cluster: %v", err)
		return
	}
	defer conn.Close()

	fmt.Fprintf(conn, "CLUSTER JOIN %s %s:%d\n", n.ID, n.Address, n.Port)
	fmt.Printf("Joined cluster with master %s\n", masterAddr)
}

// Cluster management commands
func startCluster(cfg *config.Config) {
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
	node.Cache.StartDataCleaner()

	if role == "slave" {
		go func() {
			time.Sleep(2 * time.Second)
			node.JoinCluster(masterAddr)
		}()
	}

	node.Start(cfg)
}

func RunClusterCMD(cfg *config.Config, nodeID string, port int, replicas []string) {
	persistFile := fmt.Sprintf("./data/boltcache_%s.json", nodeID)

	cache := cache.NewBoltCache(persistFile)
	cache.StartDataCleaner()

	for _, r := range replicas {
		cache.AddReplica(r)
	}

	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to start server on %s: %v", addr, err)
	}
	defer listener.Close()

	fmt.Printf("BoltCache node %s started on %s\n", nodeID, addr)
	fmt.Printf("Persistence: %s\n", persistFile)
	fmt.Printf("Replicas: %v\n", cache.Replicas)

	for {
		conn, err := listener.Accept()
		if err != nil {
			logger.Log("Failed to accept connection: %v", err)
			continue
		}
		go handleConnection(conn, cache)
	}
}
