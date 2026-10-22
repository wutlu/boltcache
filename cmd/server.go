package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/wutlu/boltcache/config"
	"github.com/wutlu/boltcache/internal/server"
	"github.com/wutlu/boltcache/logger"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the BoltCache server",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(configFile)
		if err != nil {
			return err
		}
		if _, err := os.Stat(configFile); err != nil {
			fmt.Fprintf(os.Stderr, "config file %s not found, using defaults (run `boltcache config generate` to create one)\n", configFile)
		}
		log, closer, err := logger.Setup(cfg.Logging)
		if err != nil {
			return err
		}
		defer closer.Close()

		if isatty.IsTerminal(os.Stdout.Fd()) && !strings.EqualFold(cfg.Logging.Format, "json") {
			logger.StartupMessage()
		}
		srv, err := server.New(cfg, log)
		if err != nil {
			return err
		}
		return srv.Run()
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		printVersion()
	},
}
