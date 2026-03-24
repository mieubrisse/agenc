package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/odyssey/agenc/internal/database"
	"github.com/spf13/cobra"
)

var missionDraftCmd = &cobra.Command{
	Use:    draftCmdStr + " <mission-id>",
	Short:  "Open an editor to draft text and paste it into the mission's pane",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE:   runMissionDraft,
}

func init() {
	missionCmd.AddCommand(missionDraftCmd)
}

func runMissionDraft(cmd *cobra.Command, args []string) error {
	missionIDInput := args[0]

	client, err := serverClient()
	if err != nil {
		return err
	}

	mission, err := client.GetMission(missionIDInput)
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve mission %s", missionIDInput)
	}

	if mission.TmuxPane == nil {
		return stacktrace.NewError("mission %s has no tmux pane", database.ShortID(mission.ID))
	}
	targetPane := "%" + *mission.TmuxPane

	tmpFile, err := os.CreateTemp("", "agenc-draft-*.md")
	if err != nil {
		return stacktrace.Propagate(err, "failed to create temp file")
	}
	tmpFilepath := tmpFile.Name()
	tmpFile.Close()
	defer func() { _ = os.Remove(tmpFilepath) }()

	editorEnv := os.Getenv("EDITOR")
	if editorEnv == "" {
		editorEnv = "vim"
	}

	editorParts := strings.Fields(editorEnv)
	editorBinary := editorParts[0]
	editorArgs := append(editorParts[1:], tmpFilepath)

	// Start vim/nvim in insert mode since the user opened Side Draft to type
	baseName := filepath.Base(editorBinary)
	if baseName == "vim" || baseName == "nvim" {
		editorArgs = append([]string{"-c", "startinsert"}, editorArgs...)
	}

	editorCmd := exec.Command(editorBinary, editorArgs...)
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

	pasteCmd := exec.Command("tmux", "paste-buffer", "-t", targetPane)
	if output, err := pasteCmd.CombinedOutput(); err != nil {
		return stacktrace.Propagate(err, "failed to paste buffer into pane: %s", string(output))
	}

	return nil
}
