package server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/session"
)

const (
	// summarizerInterval is how often the daemon checks for missions needing
	// AI-generated descriptions.
	summarizerInterval = 2 * time.Minute

	// summarizerPromptThreshold is the number of new user prompts that must
	// accumulate before a mission becomes eligible for re-summarization.
	summarizerPromptThreshold = 10

	// summarizerMaxUserMessages is the maximum number of recent user messages
	// to include in the summarization prompt.
	summarizerMaxUserMessages = 15

	// summarizerTimeout is the maximum time to wait for a single Claude CLI
	// summarization call.
	summarizerTimeout = 30 * time.Second

	// summarizerModel is the Claude model used for generating mission descriptions.
	// Haiku is used for speed and cost efficiency.
	summarizerModel = "claude-haiku-4-5-20251001"
)

// runMissionSummarizerLoop periodically scans for active missions that have
// accumulated enough new user prompts and generates short AI descriptions for
// their tmux window titles.
//
// Architecture note: This uses the Claude CLI subprocess (`claude --print`)
// rather than a direct Anthropic API call. This is intentionally heavier
// (process spawn overhead per call) to avoid requiring users to configure an
// API key — the CLI reuses the existing OAuth token. If performance becomes an
// issue, consider switching to a direct API call with the Anthropic SDK, which
// would eliminate subprocess overhead but require separate API key configuration.
func (s *Server) runMissionSummarizerLoop(ctx context.Context) {
	// Initial delay to avoid racing with startup I/O
	select {
	case <-ctx.Done():
		return
	case <-time.After(summarizerInterval):
		s.runMissionSummarizerCycle(ctx)
	}

	ticker := time.NewTicker(summarizerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runMissionSummarizerCycle(ctx)
		}
	}
}

// runMissionSummarizerCycle performs a single pass over all active missions,
// generating AI descriptions for those that are eligible.
func (s *Server) runMissionSummarizerCycle(ctx context.Context) {
	missions, err := s.db.ListMissionsNeedingSummary(summarizerPromptThreshold)
	if err != nil {
		s.logger.Printf("Mission summarizer: failed to list eligible missions: %v", err)
		return
	}

	for _, m := range missions {
		if ctx.Err() != nil {
			return
		}

		// Skip missions that already have a custom title from /rename — the
		// AI summary would never be displayed since /rename takes priority.
		claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(s.agencDirpath, m.ID)
		if customTitle := session.FindCustomTitle(claudeConfigDirpath, m.ID); customTitle != "" {
			// Still reset the counter so we don't re-check every cycle
			if err := s.db.UpdateAISummary(m.ID, m.AISummary); err != nil {
				s.logger.Printf("Mission summarizer: failed to reset summary counter for %s: %v", m.ShortID, err)
			}
			continue
		}

		// Extract recent user messages for context
		userMessages := session.ExtractRecentUserMessages(claudeConfigDirpath, m.ID, summarizerMaxUserMessages)
		if len(userMessages) == 0 {
			continue
		}

		summary, err := s.generateMissionSummary(ctx, userMessages)
		if err != nil {
			s.logger.Printf("Mission summarizer: failed to generate summary for %s: %v", m.ShortID, err)
			continue
		}

		if err := s.db.UpdateAISummary(m.ID, summary); err != nil {
			s.logger.Printf("Mission summarizer: failed to save summary for %s: %v", m.ShortID, err)
			continue
		}

		s.logger.Printf("Mission summarizer: updated summary for %s: %q", m.ShortID, summary)
	}
}

// generateMissionSummary calls Claude Haiku via the CLI to produce a short
// description of what the user is working on, based on their recent messages.
func (s *Server) generateMissionSummary(ctx context.Context, userMessages []string) (string, error) {
	// Build the prompt with recent user messages
	var messageBullets strings.Builder
	for _, msg := range userMessages {
		// Truncate individual messages to keep the prompt reasonable
		truncated := msg
		if len(truncated) > 200 {
			truncated = truncated[:197] + "..."
		}
		messageBullets.WriteString("- ")
		messageBullets.WriteString(truncated)
		messageBullets.WriteString("\n")
	}

	prompt := fmt.Sprintf(
		"You are generating a short description for a terminal window title. "+
			"Based on the user's recent messages from a coding session, write a concise "+
			"3-8 word description of what they are currently working on. "+
			"Output ONLY the description — no quotes, no punctuation at the end, no explanation.\n\n"+
			"Recent user messages:\n%s", messageBullets.String())

	claudeBinary, err := exec.LookPath("claude")
	if err != nil {
		return "", fmt.Errorf("'claude' binary not found in PATH: %w", err)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, summarizerTimeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, claudeBinary, "--print", "--model", summarizerModel, "-p", prompt)

	// Pass OAuth token for authentication
	oauthToken, err := config.ReadOAuthToken(s.agencDirpath)
	if err != nil {
		return "", fmt.Errorf("failed to read OAuth token: %w", err)
	}
	if oauthToken != "" {
		cmd.Env = append(os.Environ(), "CLAUDE_CODE_OAUTH_TOKEN="+oauthToken)
	}

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude CLI failed: %w", err)
	}

	summary := strings.TrimSpace(string(output))
	if summary == "" {
		return "", fmt.Errorf("claude returned empty summary")
	}

	return summary, nil
}
