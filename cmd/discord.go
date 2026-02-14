package cmd

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"
)

var discordCmd = &cobra.Command{
	Use:   discordCmdStr,
	Short: "Open the AgenC Discord community in your browser",
	Long: `Open the AgenC Discord community in your browser.

The AgenC Discord server is the central hub for community discussion,
support, feature requests, and development updates.

Discord URL: https://discord.gg/x9Y8Se4XF3`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := exec.Command("open", "-a", "Google Chrome", "https://discord.gg/x9Y8Se4XF3").Run(); err != nil {
			return fmt.Errorf("failed to open Discord URL in browser: %w", err)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(discordCmd)
}
