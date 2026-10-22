package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/wutlu/boltcache/appinfo"
)

var (
	configFile string
	rootCmd    = &cobra.Command{
		Use:           "boltcache",
		Short:         "BoltCache - high performance, Redis-compatible in-memory cache",
		Long:          "BoltCache is a Redis-compatible in-memory cache server with a REST API, Lua scripting, pub/sub and persistence.",
		Version:       appinfo.Version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
)

func Execute() error {
	return rootCmd.Execute()
}

func defaultConfigPath() string {
	if p := os.Getenv("BOLTCACHE_CONFIG"); p != "" {
		return p
	}
	return "config.yaml"
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", defaultConfigPath(), "path to the configuration file (env BOLTCACHE_CONFIG)")

	rootCmd.AddCommand(serverCmd)
	rootCmd.AddCommand(clusterCmd)
	rootCmd.AddCommand(clientCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(versionCmd)
}
