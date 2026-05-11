package cmd

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/server"
	"github.com/odyssey/agenc/internal/tableprinter"
)

var attachNoFocusFlag bool

var missionAttachCmd = &cobra.Command{
	Use:   attachCmdStr + " [mission-id]",
	Short: "Attach a mission to the current tmux session",
	Long: `Attach a mission to the current tmux session.

Links the mission's tmux window into your session and focuses it.
If the mission is already linked, just focuses the window.
Stopped missions are automatically resumed; archived missions are unarchived first.

Without arguments, opens an interactive search picker showing all missions.
Type to search by conversation content; results update live.
With arguments, accepts a mission ID (short 8-char hex or full UUID).`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionAttach,
}

func init() {
	missionCmd.AddCommand(missionAttachCmd)
	missionAttachCmd.Flags().BoolVar(&attachNoFocusFlag, noFocusFlagName, false, "don't focus the mission's tmux window after attaching")
}

func runMissionAttach(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	tmuxSession := getCallingSessionName()
	if tmuxSession == "" {
		return stacktrace.NewError("mission attach requires tmux; run inside a tmux session")
	}

	input := strings.Join(args, " ")

	var missionID string

	if input != "" && looksLikeMissionID(input) {
		// Direct ID resolution
		resolved, err := client.ResolveMissionID(input)
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve mission ID")
		}
		missionID = resolved
	} else {
		// Search-based picker
		selectedID, err := runMissionSearchPicker(client)
		if err != nil {
			return err
		}
		if selectedID == "" {
			return nil // User cancelled
		}
		resolved, err := client.ResolveMissionID(selectedID)
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve selected mission")
		}
		missionID = resolved
	}

	// Migrate old .assistant marker if present
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get agenc directory path")
	}
	if err := config.MigrateAssistantMarkerIfNeeded(agencDirpath, missionID); err != nil {
		return stacktrace.Propagate(err, "failed to migrate assistant marker")
	}

	fmt.Printf("Attaching mission: %s\n", database.ShortID(missionID))

	if err := client.AttachMission(missionID, tmuxSession, attachNoFocusFlag); err != nil {
		return stacktrace.Propagate(err, "failed to attach mission")
	}

	return nil
}

// runMissionSearchPicker opens the search-mode fzf picker for missions.
// Returns the selected mission's short ID, or empty string if cancelled.
func runMissionSearchPicker(client *server.Client) (string, error) {
	// Find our binary path for the reload command
	agencBinary, err := os.Executable()
	if err != nil {
		agencBinary = "agenc"
	}
	reloadCmd := fmt.Sprintf("%s mission search-fzf {q}", agencBinary)

	// Build initial rows (recent missions for empty query)
	missions, err := client.ListMissions(server.ListMissionsRequest{IncludeArchived: true})
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		return "", stacktrace.NewError("no missions to attach")
	}

	sortMissionsForPicker(missions)
	entries := buildMissionPickerEntries(missions, 30)

	// Format initial input matching the search-fzf output format
	var buf bytes.Buffer
	tbl := tableprinter.NewTable("ID", "LAST PROMPT", "SESSION", "REPO", "MATCH").WithWriter(&buf)
	for _, e := range entries {
		tbl.AddRow(e.ShortID, e.LastPrompt, e.Session, e.Repo, "")
	}
	tbl.Print()

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	var initialInput strings.Builder
	for i, line := range lines {
		if i == 0 {
			continue // skip header
		}
		idx := i - 1
		if idx < len(entries) {
			initialInput.WriteString(entries[idx].ShortID)
			initialInput.WriteString("\t")
			initialInput.WriteString(line)
			initialInput.WriteString("\n")
		}
	}

	return runFzfSearchPicker(FzfSearchPickerConfig{
		Prompt:        "Search missions: ",
		Headers:       []string{"ID", "LAST PROMPT", "SESSION", "REPO", "MATCH"},
		ReloadCommand: reloadCmd,
		InitialInput:  initialInput.String(),
	})
}
