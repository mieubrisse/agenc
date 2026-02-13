package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   loginCmdStr,
	Short: "Deprecated: use 'agenc config set claudeCodeOAuthToken <token>' instead",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("agenc login is no longer needed.")
		fmt.Println()
		fmt.Println("Set your Claude Code OAuth token with:")
		fmt.Println("  agenc config set claudeCodeOAuthToken <token>")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
