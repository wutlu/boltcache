package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"

	"github.com/wutlu/boltcache/v2/appinfo"
	"github.com/wutlu/boltcache/v2/config"
)

var forceGenerate bool

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage BoltCache configuration",
}

var generateConfigCmd = &cobra.Command{
	Use:   "generate",
	Short: "Write a documented default configuration file (to --config)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.GenerateDefaultConfig(configFile, forceGenerate); err != nil {
			return err
		}
		fmt.Printf("Default configuration written to %s\n", configFile)
		return nil
	},
}

var validateConfigCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the configuration file (--config)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := os.Stat(configFile); err != nil {
			return fmt.Errorf("cannot read %s: %w", configFile, err)
		}
		cfg, err := config.LoadConfig(configFile)
		if err != nil {
			return err
		}
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("%s is invalid:\n%w", configFile, err)
		}
		fmt.Printf("%s is valid ✅\n", configFile)
		return nil
	},
}

var showConfigCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the effective configuration (file merged over defaults)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(configFile)
		if err != nil {
			return err
		}
		if len(cfg.Security.Auth.Tokens) > 0 {
			masked := make([]string, len(cfg.Security.Auth.Tokens))
			for i := range masked {
				masked[i] = "********"
			}
			cfg.Security.Auth.Tokens = masked
		}
		out, err := yaml.Marshal(cfg)
		if err != nil {
			return err
		}
		fmt.Print(string(out))
		return nil
	},
}

func init() {
	generateConfigCmd.Flags().BoolVarP(&forceGenerate, "force", "f", false, "overwrite an existing file")
	configCmd.AddCommand(generateConfigCmd)
	configCmd.AddCommand(validateConfigCmd)
	configCmd.AddCommand(showConfigCmd)
}

func printVersion() {
	fmt.Printf("%s %s\n", appinfo.Title, appinfo.Version)
	if appinfo.Commit != "" {
		fmt.Printf("commit:  %s\n", appinfo.Commit)
	}
	if appinfo.Date != "" {
		fmt.Printf("built:   %s\n", appinfo.Date)
	}
	fmt.Printf("go:      %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
