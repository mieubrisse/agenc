package server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/odyssey/agenc/internal/config"
)

const (
	// summarizerModel is the Claude model used for generating session descriptions.
	summarizerModel = "claude-haiku-4-5-20251001"

	// summarizerTimeout is the maximum time to wait for a single Claude CLI
	// summarization call.
	summarizerTimeout = 30 * time.Second

	// summarizerMaxPromptLen is the maximum length of the user prompt to include
	// in the summarization request. Longer prompts are truncated.
	summarizerMaxPromptLen = 500

	// summarizerMaxOutputLen is the maximum length (in bytes) of a valid summary.
	// Responses longer than this are rejected — they indicate the model ignored
	// the system prompt and produced a conversational response instead of a title.
	summarizerMaxOutputLen = 120
)

// generateSessionSummary calls Claude Haiku via the CLI to produce a short
// description of what the user is working on, based on their first message.
// maxWords controls the upper word-count bound rendered into the system prompt.
func generateSessionSummary(ctx context.Context, agencDirpath string, firstUserMessage string, maxWords int) (string, error) {
	// Truncate long prompts
	truncated := firstUserMessage
	if len(truncated) > summarizerMaxPromptLen {
		truncated = truncated[:summarizerMaxPromptLen-3] + "..."
	}

	systemPrompt := buildSummarizerSystemPrompt(maxWords)

	claudeBinary, err := exec.LookPath("claude")
	if err != nil {
		return "", fmt.Errorf("'claude' binary not found in PATH: %w", err)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, summarizerTimeout)
	defer cancel()

	// Wrap the user message so the model sees it as input to summarize,
	// not as a conversation to respond to.
	wrappedPrompt := "Generate a window title for this user request:\n\n" + truncated

	cmd := exec.CommandContext(cmdCtx, claudeBinary,
		"--print",
		"--model", summarizerModel,
		"--system-prompt", systemPrompt,
		"--no-session-persistence",
		"--tools", "",
		"--disable-slash-commands",
		"-p", wrappedPrompt,
	)

	// Run from a temp directory as defense-in-depth: if --no-session-persistence
	// is ever removed or broken, JSONL files won't land in a mission's project
	// directory and trigger recursive re-summarization.
	cmd.Dir = os.TempDir()

	// Pass OAuth token for authentication
	oauthToken, err := config.ReadOAuthToken(agencDirpath)
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

	// Reject responses that are too long — the model likely ignored the
	// system prompt and produced a conversational response.
	if len(summary) > summarizerMaxOutputLen {
		return "", fmt.Errorf("claude returned response too long (%d bytes), likely not a title", len(summary))
	}

	return summary, nil
}

// buildSummarizerSystemPrompt renders the system prompt used by the auto-
// summarizer. The "3-N word" range is the only knob; all other instructions
// are fixed. The lower bound of 3 is intentionally hardcoded — it matches the
// floor enforced by ValidateSessionTitleMaxWords.
func buildSummarizerSystemPrompt(maxWords int) string {
	return fmt.Sprintf(
		"You are a title generator. You will receive the text of a user's request to an AI coding assistant. "+
			"Your job: output a 3-%d word terminal window title summarizing what the user is working on. "+
			"Rules: output ONLY the title. No quotes. No punctuation at the end. No markdown. No explanation. "+
			"Do NOT answer the user's request. Do NOT ask questions. Do NOT offer help. Just the title.",
		maxWords,
	)
}
