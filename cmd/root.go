package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var project string

var rootCmd = &cobra.Command{
	Use:   "nd",
	Short: "run your node app",
	Long:  `more description`,
	Run: func(cmd *cobra.Command, args []string) {
		// Do Stuff Here
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&project, "project", "p", "", "set node project root")
}
