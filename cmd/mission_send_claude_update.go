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
)

var missionSendClaudeUpdateCmd = &cobra.Command{
	Use:   claudeUpdateCmdStr + " <mission-uuid> <event>",
	Short: "Send a Claude hook event to the mission wrapper",
	Long: `Send a Claude hook event to the mission wrapper via its unix socket.

This command is called by Claude Code hooks (Stop, UserPromptSubmit, Notification)
to report state changes. Hook JSON is read from stdin when available.

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

	// Read stdin for hook JSON (Claude provides event-specific fields).
	// Non-blocking: if stdin is empty or a pipe with no data, we proceed
	// without it.
	notificationType := extractNotificationType(os.Stdin)

	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		// Silently fail — never block Claude
		return nil
	}

	socketFilepath := config.GetMissionSocketFilepath(agencDirpath, missionID)

	socketCmd := wrapper.Command{
		Command:          "claude_update",
		Event:            event,
		NotificationType: notificationType,
	}

	// Use a short timeout to avoid blocking Claude if the wrapper is unresponsive
	resp, err := wrapper.SendCommandWithTimeout(socketFilepath, socketCmd, claudeUpdateClientTimeout)
	if err != nil {
		// Silently fail — never block Claude
		_ = resp
		return nil
	}

	return nil
}

// extractNotificationType reads stdin and extracts the notification_type field
// from the hook JSON payload. Returns empty string if stdin is empty or the
// field is not present.
func extractNotificationType(reader io.Reader) string {
	data, err := io.ReadAll(reader)
	if err != nil || len(data) == 0 {
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
