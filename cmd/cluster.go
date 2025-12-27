package cmd

import (
	"log"
"fmt"
	"github.com/spf13/cobra"

	"boltcache/config"
	"boltcache/internal/server"
)

var (
	nodeID   string
	nodePort int
	replicas []string
)

var clusterCmd = &cobra.Command{
	Use:   "cluster",
	Short: "Start a BoltCache cluster node",
	Run: func(cmd *cobra.Command, args []string) {
		if nodeID == "" || nodePort == 0 {
			log.Fatalf("Both --node and --port must be specified")
		}

		cfg, err := config.LoadConfig(configFile)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
fmt.Print(replicas)

		server.RunClusterCMD(cfg, nodeID, nodePort, replicas)
	},
}

func init() {
	clusterCmd.Flags().StringVar(&nodeID, "node", "", "Cluster node ID")
	clusterCmd.Flags().IntVar(&nodePort, "port", 0, "Cluster node port")
	clusterCmd.Flags().StringSliceVar(&replicas, "replica", nil, "Add replicas (comma separated)")
}
