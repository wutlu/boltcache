package cmd

import (
	"github.com/spf13/cobra"
)

import (
	logger "boltcache/logger"
)

var (
	configFile string
	rootCmd    = &cobra.Command{
		Use:   "boltcache",
		Short: "BoltCache - high performance in-memory cache",
		Long:  "BoltCache is a Redis-like high-performance cache server with Lua scripting, pub/sub, and cluster support.",
	}
)

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	logger.StartupMessage()

	rootCmd.PersistentFlags().StringVar(&configFile, "config", "./data/boltcache.json", "Path to configuration file")

	// Burada tüm subcommand’lar root'a eklenecek
	rootCmd.AddCommand(serverCmd)
	rootCmd.AddCommand(clusterCmd)
	rootCmd.AddCommand(clientCmd)
	rootCmd.AddCommand(configCmd)
}
