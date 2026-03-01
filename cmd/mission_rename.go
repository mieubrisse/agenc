package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/server"
)

var missionRenameCmd = &cobra.Command{
	Use:   renameCmdStr + " [mission-id] [title]",
	Short: "Rename the active session's window title for a mission",
	Long: `Rename the active session's window title for a mission.

This is a convenience command that resolves the mission's active session and
renames it. If no mission-id is provided, uses $AGENC_CALLING_MISSION_UUID.
If no title is provided, prompts for input interactively.

Example:
  agenc mission rename                          # uses env var, prompts for title
  agenc mission rename abc12345 "My Feature"    # explicit mission and title`,
	Args: cobra.RangeArgs(0, 2),
	RunE: runMissionRename,
}

func init() {
	missionCmd.AddCommand(missionRenameCmd)
}

func runMissionRename(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	// Resolve mission ID from args or env var
	var missionID string
	if len(args) >= 1 {
		missionID = args[0]
	} else {
		missionID = os.Getenv("AGENC_CALLING_MISSION_UUID")
	}
	if missionID == "" {
		return stacktrace.NewError("no mission ID provided; pass a mission ID or set $AGENC_CALLING_MISSION_UUID")
	}

	// Resolve the active session
	sessions, err := client.ListMissionSessions(missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to list sessions for mission %s", missionID)
	}
	if len(sessions) == 0 {
		return stacktrace.NewError("no sessions found for mission %s", missionID)
	}
	activeSession := sessions[0] // already sorted by updated_at DESC

	// Get title from args or prompt
	var title string
	if len(args) >= 2 {
		title = args[1]
	} else {
		title, err = promptForTitle()
		if err != nil {
			if errors.Is(err, errPromptCancelled) {
				return nil
			}
			return err
		}
	}

	req := server.UpdateSessionRequest{
		AgencCustomTitle: &title,
	}
	if err := client.UpdateSession(activeSession.ID, req); err != nil {
		return stacktrace.Propagate(err, "failed to rename session")
	}

	if title == "" {
		fmt.Println("Session title cleared.")
	} else {
		fmt.Printf("Session renamed to %q.\n", title)
	}
	return nil
}
