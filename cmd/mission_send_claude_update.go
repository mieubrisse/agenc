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

This command is called by Claude Code hooks (Stop, StopFailure, UserPromptSubmit,
Notification, PostToolUse, PostToolUseFailure) to report state changes. For
Notification and StopFailure events, hook JSON is read from stdin (with a timeout)
to extract the fields the wrapper needs — notification_type for the former,
agent_id and error for the latter. All other events skip stdin entirely to avoid
blocking when Claude Code doesn't close it.

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

	// Only read stdin for the events whose payload the wrapper actually needs:
	// Notification (for notification_type) and StopFailure (for agent_id, which
	// says whether the MAIN agent's turn died or merely a subagent's). The rest
	// (Stop, UserPromptSubmit, PostToolUse, PostToolUseFailure) pass nothing
	// useful via stdin, and Claude Code may not close stdin for them — causing
	// io.ReadAll to block indefinitely.
	var payload claudeHookPayload
	if payloadIsNeededForEvent(event) {
		payload = extractClaudeHookPayload(os.Stdin)
	}

	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		// Silently fail — never block Claude
		return nil
	}

	socketFilepath := config.GetMissionSocketFilepath(agencDirpath, missionID)

	// Use a short timeout to avoid blocking Claude if the wrapper is unresponsive
	client := wrapper.NewWrapperClient(socketFilepath, claudeUpdateClientTimeout)
	update := wrapper.ClaudeUpdateRequest{
		Event:            event,
		NotificationType: payload.NotificationType,
		AgentID:          payload.AgentID,
		HookError:        payload.Error,
		PayloadParsed:    payload.Parsed,
	}
	if err := client.SendClaudeUpdate(update); err != nil {
		// Silently fail — never block Claude
		return nil
	}

	return nil
}

// claudeHookPayload holds the fields the wrapper needs out of a Claude Code
// hook's JSON payload. Parsed reports whether the payload was read and decoded
// successfully; it is NOT implied by the other fields being set, because every
// one of them is legitimately absent on some events.
//
// Parsed is the field that keeps the StopFailure handler safe. "No agent_id"
// and "could not read the payload" both leave AgentID empty, but they mean
// opposite things: the first says the main agent's turn died, the second says
// we know nothing at all. Collapsing them would let a failed read mark a
// working mission idle.
type claudeHookPayload struct {
	NotificationType string
	AgentID          string
	Error            string
	Parsed           bool
}

// payloadIsNeededForEvent reports whether the wrapper needs anything out of
// this event's stdin payload. Reading stdin costs up to stdinReadTimeout, so
// events that carry nothing useful skip it entirely.
func payloadIsNeededForEvent(event string) bool {
	return event == "Notification" || event == "StopFailure"
}

// extractClaudeHookPayload reads stdin with a short timeout and decodes the
// hook JSON payload. A zero-value payload (Parsed false) is returned if stdin
// is empty, the JSON is malformed, or the read times out — callers must treat
// that as "nothing is known about this event" rather than as absent fields.
//
// The timeout prevents blocking if Claude Code doesn't close stdin. This is a
// CLI process that exits immediately after, so a leaked goroutine on timeout
// is acceptable.
func extractClaudeHookPayload(reader io.Reader) claudeHookPayload {
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
			return claudeHookPayload{}
		}
		data = res.data
	case <-time.After(stdinReadTimeout):
		return claudeHookPayload{}
	}

	if len(data) == 0 {
		return claudeHookPayload{}
	}

	// agent_id, per Claude Code's own hook-payload schema: "Subagent
	// identifier. Present only when the hook fires from within a subagent
	// (e.g., a tool called by an AgentTool worker). Absent for the main
	// thread, even in --agent sessions. Use this field (not agent_type) to
	// distinguish subagent calls from main-thread calls."
	var decoded struct {
		NotificationType string `json:"notification_type"`
		AgentID          string `json:"agent_id"`
		Error            string `json:"error"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return claudeHookPayload{}
	}
	return claudeHookPayload{
		NotificationType: decoded.NotificationType,
		AgentID:          decoded.AgentID,
		Error:            decoded.Error,
		Parsed:           true,
	}
}
