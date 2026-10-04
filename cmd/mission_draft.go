package cmd

import (
	"fmt"
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

	// The mission's short ID makes a recovered draft attributable, since drafts outlive the
	// command and several missions' drafts accumulate side by side. The '*' is what keeps
	// repeated drafts from the SAME mission from overwriting each other — CreateTemp replaces
	// it with a random component — so it must stay.
	draftFilenamePattern := fmt.Sprintf("agenc-draft-%s-*.md", database.ShortID(mission.ID))
	tmpFile, err := os.CreateTemp("", draftFilenamePattern)
	if err != nil {
		return stacktrace.Propagate(err, "failed to create draft file matching pattern '%v'", draftFilenamePattern)
	}
	tmpFilepath := tmpFile.Name()
	tmpFile.Close()

	// The draft file is deliberately never deleted. A paste into the target pane occasionally
	// lands garbled, and this file is the only copy of what the user typed — deleting it on exit
	// destroys their work with no way back. Leaving it in the OS temp directory keeps it
	// recoverable (`ls -t $TMPDIR/agenc-draft-*.md` is newest-first) and lets the OS reap it.

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

	// Drop the target pane out of copy mode if it's in it, so the paste lands in the prompt.
	// Gated on pane_in_mode — send-keys -X against a pane not in any mode can deliver a stray
	// key to the running program (e.g. accepting Claude's default-suggestion prompt).
	modeOut, modeErr := exec.Command("tmux", "display-message", "-p", "-t", targetPane, "#{pane_in_mode}").Output()
	if modeErr == nil && strings.TrimSpace(string(modeOut)) == "1" {
		_ = exec.Command("tmux", "send-keys", "-t", targetPane, "-X", "cancel").Run()
	}

	// -p is load-bearing, not cosmetic: it wraps the payload in bracketed-paste control codes.
	// The pty hands its reader at most ~1KB per read, so a draft over roughly a kilobyte arrives
	// in two chunks. Without the brackets nothing tells the receiving program that the chunks
	// belong to one paste, and Claude Code keeps only the last one — a 1437-byte draft silently
	// lost its leading 1022 bytes, landing as a message truncated mid-word with no error anywhere.
	// (-r, which would also stop tmux rewriting newlines to carriage returns, is not needed:
	// Claude Code normalises them inside a bracketed paste. Verified against a live pane.)
	pasteCmd := exec.Command("tmux", "paste-buffer", "-p", "-t", targetPane)
	if output, err := pasteCmd.CombinedOutput(); err != nil {
		return stacktrace.Propagate(err, "failed to paste buffer into pane: %s", string(output))
	}

	return nil
}
