package cmd

import (
	"log"

	"github.com/spf13/cobra"

	server "boltcache/internal/server"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the BoltCache server",
	Run: func(cmd *cobra.Command, args []string) {
		srv, err := server.NewServer(configFile)
		if err != nil {
			log.Fatalf("Failed to create server: %v", err)
		}

		if err := srv.Start(); err != nil {
			log.Fatalf("Server exited with error: %v", err)
		}
	},
}
