package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
)

var missionSendKeysCmd = &cobra.Command{
	Use:   sendKeysCmdStr + " <mission-id> [keys...]",
	Short: "Send keystrokes to a running mission's tmux pane",
	Long: `Send keystrokes to a running mission's tmux pane via tmux send-keys.

Keys are passed through to tmux verbatim — use tmux key names for special keys:
  Enter, Escape, C-c, C-d, Space, Tab, Up, Down, Left, Right, etc.

Examples:
  agenc mission send-keys abc123 "hello world" Enter
  agenc mission send-keys abc123 C-c
  echo "fix the bug" | agenc mission send-keys abc123
  echo "fix the bug" | agenc mission send-keys abc123 Enter`,
	Args: cobra.MinimumNArgs(1),
	RunE: runMissionSendKeys,
}

func init() {
	missionCmd.AddCommand(missionSendKeysCmd)
}

func runMissionSendKeys(cmd *cobra.Command, args []string) error {
	missionIDInput := args[0]
	positionalKeys := args[1:]

	// Build the keys list: stdin content first (if piped), then positional args
	var keys []string

	if !isatty.IsTerminal(os.Stdin.Fd()) && !isatty.IsCygwinTerminal(os.Stdin.Fd()) {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read stdin")
		}
		stdinContent := strings.TrimRight(string(data), "\n")
		if stdinContent != "" {
			keys = append(keys, stdinContent)
		}
	}

	keys = append(keys, positionalKeys...)

	if len(keys) == 0 {
		return stacktrace.NewError(
			"no keys provided — pass keys as arguments or pipe via stdin\n\n"+
				"Examples:\n"+
				"  %s %s %s abc123 \"hello world\" Enter\n"+
				"  echo \"hello\" | %s %s %s abc123",
			agencCmdStr, missionCmdStr, sendKeysCmdStr,
			agencCmdStr, missionCmdStr, sendKeysCmdStr,
		)
	}

	client, err := serverClient()
	if err != nil {
		return err
	}

	// Resolve mission ID (supports short IDs)
	missionID, err := client.ResolveMissionID(missionIDInput)
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve mission ID")
	}

	if err := client.SendKeys(missionID, keys); err != nil {
		return stacktrace.Propagate(err, "failed to send keys to mission %s", database.ShortID(missionID))
	}

	fmt.Printf("Sent %d key(s) to mission %s\n", len(keys), database.ShortID(missionID))
	return nil
}
