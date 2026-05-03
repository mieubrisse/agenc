package cmd

import (
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/wrapper"
)

const (
	claudeUpdateClientTimeout = 1 * time.Second
	stdinReadTimeout          = 500 * time.Millisecond
)

var missionSendClaudeUpdateCmd = &cobra.Command{
	Use:   claudeUpdateCmdStr + " <mission-uuid> <event>",
	Short: "Send a Claude hook event to the mission wrapper",
	Long: `Send a Claude hook event to the mission wrapper via its unix socket.

This command is called by Claude Code hooks (Stop, UserPromptSubmit, Notification,
PostToolUse, PostToolUseFailure) to report state changes. For Notification events,
hook JSON is read from stdin (with a timeout) to extract notification_type. All
other events skip stdin entirely to avoid blocking when Claude Code doesn't close it.

Always exits 0, even on failure, to avoid blocking Claude.`,
	Args:               cobra.ExactArgs(2),
	SilenceErrors:      true,
	SilenceUsage:       true,
	DisableFlagParsing: true,
	RunE:               runMissionSendClaudeUpdate,
}

func init() {
	missionSendCmd.AddCommand(missionSendClaudeUpdateCmd)
}

func runMissionSendClaudeUpdate(cmd *cobra.Command, args []string) error {
	missionID := args[0]
	event := args[1]

	// Only read stdin for Notification events (to extract notification_type).
	// Other events (Stop, UserPromptSubmit, PostToolUse, PostToolUseFailure)
	// don't pass useful data via stdin, and Claude Code may not close stdin
	// for them — causing io.ReadAll to block indefinitely.
	var notificationType string
	if event == "Notification" {
		notificationType = extractNotificationType(os.Stdin)
	}

	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		// Silently fail — never block Claude
		return nil
	}

	socketFilepath := config.GetMissionSocketFilepath(agencDirpath, missionID)

	// Use a short timeout to avoid blocking Claude if the wrapper is unresponsive
	client := wrapper.NewWrapperClient(socketFilepath, claudeUpdateClientTimeout)
	if err := client.SendClaudeUpdate(event, notificationType); err != nil {
		// Silently fail — never block Claude
		return nil
	}

	return nil
}

// extractNotificationType reads stdin with a short timeout and extracts the
// notification_type field from the hook JSON payload. Returns empty string if
// stdin is empty, the field is not present, or the read times out.
//
// The timeout prevents blocking if Claude Code doesn't close stdin. This is a
// CLI process that exits immediately after, so a leaked goroutine on timeout
// is acceptable.
func extractNotificationType(reader io.Reader) string {
	type readResult struct {
		data []byte
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		data, err := io.ReadAll(reader)
		ch <- readResult{data, err}
	}()

	var data []byte
	select {
	case res := <-ch:
		if res.err != nil {
			return ""
		}
		data = res.data
	case <-time.After(stdinReadTimeout):
		return ""
	}

	if len(data) == 0 {
		return ""
	}

	var payload struct {
		NotificationType string `json:"notification_type"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}
	return payload.NotificationType
}
