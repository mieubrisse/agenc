package cmd

import (
	"errors"
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/server"
)

var sessionRenameCmd = &cobra.Command{
	Use:   renameCmdStr + " <session-id> [title]",
	Short: "Rename a session's window title",
	Long: `Rename a session's window title.

Sets the agenc_custom_title on the session, which controls the tmux window name.
If no title is provided, prompts for input interactively.
An empty title clears the custom title, falling back to the auto-resolved title.

Example:
  agenc session rename 18749fb5-02ba-4b19-b989-4e18fbf8ea92 "My Feature Work"
  agenc session rename 18749fb5-02ba-4b19-b989-4e18fbf8ea92    # prompts for title`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runSessionRename,
}

func init() {
	sessionCmd.AddCommand(sessionRenameCmd)
}

func runSessionRename(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	sessionID := args[0]

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
	if err := client.UpdateSession(sessionID, req); err != nil {
		return stacktrace.Propagate(err, "failed to rename session")
	}

	if title == "" {
		fmt.Println("Session title cleared.")
	} else {
		fmt.Printf("Session renamed to %q.\n", title)
	}
	return nil
}

// promptForTitle reads a title interactively from the terminal. The user can
// press Enter to submit, or ESC/Ctrl+C to cancel. Returns errPromptCancelled
// when the user cancels. Falls back to simple line reading for non-terminal stdin.
func promptForTitle() (string, error) {
	return readRawLine("New title (empty to clear, ESC to cancel): ")
}
