package cmd

import (
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/mattn/go-isatty"
	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var configEditCmd = &cobra.Command{
	Use:   editCmdStr,
	Short: "Open config.yml in your editor ($EDITOR)",
	RunE:  runConfigEdit,
}

func init() {
	configCmd.AddCommand(configEditCmd)
}

func runConfigEdit(cmd *cobra.Command, args []string) error {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return stacktrace.NewError("'%s %s %s' requires a terminal; use '%s %s %s'/'%s %s %s' instead",
			agencCmdStr, configCmdStr, editCmdStr,
			agencCmdStr, configCmdStr, getCmdStr,
			agencCmdStr, configCmdStr, setCmdStr,
		)
	}
	if _, err := getAgencContext(); err != nil {
		return err
	}
	editorEnv := os.Getenv("EDITOR")
	if editorEnv == "" {
		return stacktrace.NewError("$EDITOR is not set; set it to your preferred editor (e.g. export EDITOR=vim)")
	}

	// Split $EDITOR on whitespace so values like "code --wait" work correctly.
	editorParts := strings.Fields(editorEnv)

	editorBinary, err := exec.LookPath(editorParts[0])
	if err != nil {
		return stacktrace.Propagate(err, "editor '%s' not found in PATH", editorParts[0])
	}

	configFilepath := config.GetConfigFilepath(agencDirpath)

	// Build argv: editor name, any extra flags from $EDITOR, then the config file path.
	argv := append(editorParts, configFilepath)

	return syscall.Exec(editorBinary, argv, os.Environ())
}
