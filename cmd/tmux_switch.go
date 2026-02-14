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

	// Auto-wrap in a larger popup when running the interactive picker. This handles
	// three invocation contexts:
	//
	// 1. From the palette: The palette runs in a 60%×50% popup. We detect we're
	//    showing an interactive picker (no mission ID provided) and re-exec in
	//    an 80%×70% popup, giving the mission list more room to display.
	//
	// 2. From a keybinding: Tmux keybindings use `run-shell`, which doesn't
	//    provide a TTY. Without this check, fzf would fail with "Inappropriate
	//    ioctl for device". We detect the missing TTY and re-exec in a popup.
	//
	// 3. From a shell: When a mission ID is provided, we skip the popup and
	//    switch directly. When no ID is provided (interactive mode), we wrap
	//    in a popup for consistency.
	//
	// To prevent infinite recursion, we set AGENC_IN_POPUP=1 before wrapping.
	// The larger 80%×70% size gives more room for the mission list compared to
	// the standard 60%×50% palette popup.
	alreadyInPopup := os.Getenv("AGENC_IN_POPUP") == "1"
	needsInteractivePicker := len(args) == 0
	hasTTY := term.IsTerminal(int(os.Stdin.Fd()))

	if !alreadyInPopup && (!hasTTY || needsInteractivePicker) {
		// Build the command to re-exec in a popup
		cmdArgs := []string{"agenc", "tmux", "switch"}
		cmdArgs = append(cmdArgs, args...)
		cmdStr := strings.Join(cmdArgs, " ")

		popupCmd := exec.Command("tmux", "display-popup", "-E", "-w", "80%", "-h", "70%", cmdStr)
		popupCmd.Env = append(os.Environ(), "AGENC_IN_POPUP=1")
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

	entries, err := buildMissionPickerEntries(db, switchable)
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
			return []string{e.LastActive, e.ShortID, e.TmuxTitle, e.Session, e.Repo}
		},
		FzfPrompt:         "Switch to mission: ",
		FzfHeaders:        []string{"LAST ACTIVE", "ID", "TITLE", "SESSION", "REPO"},
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
