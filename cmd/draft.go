package cmd

import (
	"os"
	"os/exec"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var draftCmd = &cobra.Command{
	Use:   draftCmdStr + " <target-pane-id>",
	Short: "Open an editor to draft text and paste it into a tmux pane",
	Long: `Opens $EDITOR (defaults to vim) with a temporary Markdown file. After the
editor exits, if the file has content, it is pasted into the specified tmux
pane using tmux load-buffer and paste-buffer. The temporary file is cleaned
up afterward.

This command is designed to be called by the Side Draft palette command,
which opens it in a horizontal tmux split alongside the target pane.`,
	Args: cobra.ExactArgs(1),
	RunE: runDraft,
}

func init() {
	rootCmd.AddCommand(draftCmd)
}

func runDraft(cmd *cobra.Command, args []string) error {
	targetPaneID := args[0]

	tmpFile, err := os.CreateTemp("", "agenc-draft-*.md")
	if err != nil {
		return stacktrace.Propagate(err, "failed to create temp file")
	}
	tmpFilepath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpFilepath)

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}

	editorCmd := exec.Command(editor, tmpFilepath)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr
	if err := editorCmd.Run(); err != nil {
		return stacktrace.Propagate(err, "editor exited with error")
	}

	info, err := os.Stat(tmpFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to stat temp file")
	}
	if info.Size() == 0 {
		return nil
	}

	loadCmd := exec.Command("tmux", "load-buffer", tmpFilepath)
	if output, err := loadCmd.CombinedOutput(); err != nil {
		return stacktrace.Propagate(err, "failed to load buffer into tmux: %s", string(output))
	}

	pasteCmd := exec.Command("tmux", "paste-buffer", "-t", targetPaneID)
	if output, err := pasteCmd.CombinedOutput(); err != nil {
		return stacktrace.Propagate(err, "failed to paste buffer into pane %s: %s", targetPaneID, string(output))
	}

	return nil
}
