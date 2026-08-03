package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   loginCmdStr,
	Short: "Deprecated: use 'claude auth login' or 'agenc token set <token>'",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("'agenc login' is no longer needed.")
		fmt.Println()
		fmt.Println("AgenC missions default to native Claude authentication.")
		fmt.Println("To authenticate with Claude:")
		fmt.Println("  claude auth login")
		fmt.Println()
		fmt.Println("For headless or multi-session workflows, you can store a long-lived token:")
		fmt.Println("  agenc token set <token>      (store a token you already have)")
		fmt.Println("  agenc token setup            (interactive wizard to obtain a token)")
		fmt.Println("  agenc token clear            (remove token; revert to native auth)")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
