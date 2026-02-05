package cmd

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var loginCmd = &cobra.Command{
	Use:   loginCmdStr,
	Short: "Log in to Claude (credentials stored in $AGENC_DIRPATH/claude/)",
	RunE: func(cmd *cobra.Command, args []string) error {
		claudeBinary, err := exec.LookPath("claude")
		if err != nil {
			return stacktrace.Propagate(err, "'claude' binary not found in PATH")
		}

		claudeConfigDirpath := config.GetGlobalClaudeDirpath(agencDirpath)

		env := os.Environ()
		env = append(env, "CLAUDE_CONFIG_DIR="+claudeConfigDirpath)

		return syscall.Exec(claudeBinary, []string{"claude", "login"}, env)
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
