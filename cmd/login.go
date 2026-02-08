package cmd

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   loginCmdStr,
	Short: "Log in to Claude (credentials stored in macOS Keychain)",
	RunE: func(cmd *cobra.Command, args []string) error {
		claudeBinary, err := exec.LookPath("claude")
		if err != nil {
			return stacktrace.Propagate(err, "'claude' binary not found in PATH")
		}

		// Run without CLAUDE_CONFIG_DIR so credentials are written to the
		// default Keychain entry ("Claude Code-credentials"), which per-mission
		// Keychain cloning reads from.
		return syscall.Exec(claudeBinary, []string{"claude", "login"}, os.Environ())
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
