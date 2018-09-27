package cmd

import (
	nd "github.com/jdxcode/nd/lib"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(refreshCmd)
}

var refreshCmd = &cobra.Command{
	Use:   ":refresh",
	Short: "ensures all node modules are installed",
	Long:  "equivalent to `node SCRIPT`",
	Run: func(cmd *cobra.Command, args []string) {
		nd.Load("")
	},
}
