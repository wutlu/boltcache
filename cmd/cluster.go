package cmd

import (
	"errors"

	"github.com/spf13/cobra"
)

// The 1.x "cluster" command started a standalone node and printed its
// replica list, but never replicated any data. Rather than keep a command
// that suggests high availability it doesn't provide, it now explains that.
var clusterCmd = &cobra.Command{
	Use:    "cluster",
	Short:  "(not available) clustering and replication are on the roadmap",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return errors.New("clustering/replication is not implemented yet: the 1.x `cluster` command ran an " +
			"isolated node and never replicated data. Run `boltcache server` instead; follow " +
			"https://github.com/wutlu/boltcache/issues for replication progress")
	},
	DisableFlagParsing: true,
}
