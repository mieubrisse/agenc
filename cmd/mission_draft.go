package cmd

import (
	"os"
	"os/exec"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

// draftTargetPaneEnvVar is the environment variable that carries the tmux pane ID
// to paste drafted text into. Set by the palette command before opening the split.
const draftTargetPaneEnvVar = "AGENC_DRAFT_TARGET_PANE"

var missionDraftCmd = &cobra.Command{
	Use:    draftCmdStr,
	Short:  "Open an editor to draft text and paste it into the calling pane",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE:   runMissionDraft,
}

func init() {
	missionCmd.AddCommand(missionDraftCmd)
}

func runMissionDraft(cmd *cobra.Command, args []string) error {
	targetPaneID := os.Getenv(draftTargetPaneEnvVar)
	if targetPaneID == "" {
		targetPaneID = "{last}"
	}

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
		return stacktrace.Propagate(err, "failed to paste buffer into pane: %s", string(output))
	}

	return nil
}
