package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/odyssey/agenc/internal/database"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var tmuxSwitchCmd = &cobra.Command{
	Use:   switchCmdStr + " [mission-id]",
	Short: "Switch to a running mission's tmux window",
	Long: `Presents an fzf picker of running missions and switches focus to the
selected mission's tmux window. Only missions with an active tmux pane
are shown.

Without arguments, opens an interactive fzf picker. With a mission ID
argument (short 8-char hex or full UUID), switches directly.

This command is designed to be invoked from a tmux display-popup or
keybinding.`,
	Args: cobra.ArbitraryArgs,
	RunE: runTmuxSwitch,
}

func init() {
	tmuxCmd.AddCommand(tmuxSwitchCmd)
}

func runTmuxSwitch(cmd *cobra.Command, args []string) error {
	if !isInsideAgencTmux() {
		return stacktrace.NewError("must be run inside the AgenC tmux session (AGENC_TMUX != 1)")
	}

	// Auto-wrap in a popup if we're running without a TTY. This handles three
	// invocation contexts:
	//
	// 1. From the palette: The palette itself runs in a tmux display-popup, so
	//    stdin is already a TTY. We detect this and run normally (no nested popup).
	//
	// 2. From a keybinding: Tmux keybindings use `run-shell`, which doesn't
	//    provide a TTY. Without this check, fzf would fail with "Inappropriate
	//    ioctl for device". We detect the missing TTY and re-exec in a popup.
	//
	// 3. From a shell: The shell has a TTY, so we run normally.
	//
	// This ensures the fzf picker works in all three contexts without requiring
	// different command strings in the config or keybinding generation logic.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		// Build the command to re-exec in a popup
		cmdArgs := []string{"agenc", "tmux", "switch"}
		cmdArgs = append(cmdArgs, args...)
		cmdStr := strings.Join(cmdArgs, " ")

		popupCmd := exec.Command("tmux", "display-popup", "-E", "-w", "60%", "-h", "50%", cmdStr)
		popupCmd.Stdout = os.Stdout
		popupCmd.Stderr = os.Stderr
		return popupCmd.Run()
	}

	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	missions, err := db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	// Only include missions that are running AND have a tmux pane registered.
	var switchable []*database.Mission
	for _, m := range missions {
		if m.TmuxPane != nil && getMissionStatus(m.ID, m.Status) == "RUNNING" {
			switchable = append(switchable, m)
		}
	}

	if len(switchable) == 0 {
		fmt.Println("No running missions with tmux windows.")
		return nil
	}

	entries, err := buildMissionPickerEntries(db, switchable, 50)
	if err != nil {
		return err
	}

	result, err := Resolve(strings.Join(args, " "), Resolver[missionPickerEntry]{
		TryCanonical: func(input string) (missionPickerEntry, bool, error) {
			if !looksLikeMissionID(input) {
				return missionPickerEntry{}, false, nil
			}
			missionID, err := db.ResolveMissionID(input)
			if err != nil {
				return missionPickerEntry{}, false, stacktrace.Propagate(err, "failed to resolve mission ID")
			}
			for _, e := range entries {
				if e.MissionID == missionID {
					return e, true, nil
				}
			}
			return missionPickerEntry{}, false, stacktrace.NewError("mission %s is not running or has no tmux window", input)
		},
		GetItems: func() ([]missionPickerEntry, error) { return entries, nil },
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.ShortID, e.TmuxTitle, e.Session, e.Repo}
		},
		FzfPrompt:         "Switch to mission: ",
		FzfHeaders:        []string{"ID", "TITLE", "SESSION", "REPO"},
		MultiSelect:       false,
		NotCanonicalError: "not a valid mission ID",
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	selected := result.Items[0]

	// Look up the mission's tmux pane and switch to its window.
	mission, err := db.GetMission(selected.MissionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get mission")
	}
	if mission == nil {
		return stacktrace.NewError("mission '%s' not found", selected.MissionID)
	}
	if mission.TmuxPane == nil {
		return stacktrace.NewError("mission %s no longer has a tmux pane", selected.ShortID)
	}

	return switchToTmuxPane(*mission.TmuxPane)
}

// switchToTmuxPane switches focus to the tmux window containing the given pane ID.
// The pane ID is the numeric form without the "%" prefix.
func switchToTmuxPane(paneID string) error {
	targetPane := "%" + paneID
	if err := exec.Command("tmux", "select-window", "-t", targetPane).Run(); err != nil {
		return stacktrace.Propagate(err, "failed to switch to tmux window for pane %s", targetPane)
	}
	if err := exec.Command("tmux", "select-pane", "-t", targetPane).Run(); err != nil {
		return stacktrace.Propagate(err, "failed to select tmux pane %s", targetPane)
	}
	return nil
}
