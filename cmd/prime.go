package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/claudeconfig"
)

var primeCmd = &cobra.Command{
	Use:   primeCmdStr,
	Short: "Print AgenC CLI quick reference for AI agent context",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print(claudeconfig.GetPrimeContent())
	},
}

func init() {
	rootCmd.AddCommand(primeCmd)
}
