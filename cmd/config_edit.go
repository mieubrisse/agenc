package cmd

import (
	"os"
	"os/exec"
	"syscall"

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
	editor := os.Getenv("EDITOR")
	if editor == "" {
		return stacktrace.NewError("$EDITOR is not set; set it to your preferred editor (e.g. export EDITOR=vim)")
	}

	editorBinary, err := exec.LookPath(editor)
	if err != nil {
		return stacktrace.Propagate(err, "editor '%s' not found in PATH", editor)
	}

	configFilepath := config.GetConfigFilepath(agencDirpath)

	return syscall.Exec(editorBinary, []string{editor, configFilepath}, os.Environ())
}
