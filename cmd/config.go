package cmd

import (
	"log"

	"github.com/spf13/cobra"
	"boltcache/config"
)


var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage BoltCache configuration",
}

var generateConfigCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate default configuration file",
	Run: func(cmd *cobra.Command, args []string) {
		if err := config.GenerateDefaultConfig(configFile); err != nil {
			log.Fatalf("Failed to generate config: %v", err)
		}
		log.Println("Default config generated: config.yaml")
	},
}

var validateConfigCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration file",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.LoadConfig("config.yaml")
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
		if err := cfg.Validate(); err != nil {
			log.Fatalf("Config is invalid: %v", err)
		}
		log.Println("Configuration is valid ✅")
	},
}

func init() {
	configCmd.AddCommand(generateConfigCmd)
	configCmd.AddCommand(validateConfigCmd)
}
