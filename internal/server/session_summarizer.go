package server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/odyssey/agenc/internal/config"
)

const (
	// summarizerModel is the Claude model used for generating session descriptions.
	summarizerModel = "claude-haiku-4-5-20251001"

	// summarizerTimeout is the maximum time to wait for a single Claude CLI
	// summarization call.
	summarizerTimeout = 30 * time.Second

	// summarizerChannelSize is the buffer size for the summary request channel.
	summarizerChannelSize = 64

	// summarizerMaxPromptLen is the maximum length of the user prompt to include
	// in the summarization request. Longer prompts are truncated.
	summarizerMaxPromptLen = 500

	// summarizerMaxOutputLen is the maximum length (in bytes) of a valid summary.
	// Responses longer than this are rejected — they indicate the model ignored
	// the system prompt and produced a conversational response instead of a title.
	summarizerMaxOutputLen = 80
)

// summaryRequest represents a request to generate an auto_summary for a session.
type summaryRequest struct {
	sessionID        string
	missionID        string
	firstUserMessage string
}

// requestSessionSummary sends a summary request to the summarizer worker.
// Non-blocking: if the channel is full, the request is dropped (it will be
// retried on the next scan cycle if auto_summary is still empty).
func (s *Server) requestSessionSummary(sessionID string, missionID string, firstUserMessage string) {
	select {
	case s.sessionSummaryCh <- summaryRequest{
		sessionID:        sessionID,
		missionID:        missionID,
		firstUserMessage: firstUserMessage,
	}:
	default:
		s.logger.Printf("Session summarizer: channel full, dropping request for session '%s'", sessionID)
	}
}

// runSessionSummarizerWorker consumes summary requests from the channel and
// generates auto_summary values via a Haiku CLI call. Uses a sync.Map to
// permanently cache which sessions have been processed — each session only
// triggers one Haiku call for the lifetime of the process.
func (s *Server) runSessionSummarizerWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-s.sessionSummaryCh:
			s.handleSummaryRequest(ctx, req)
		}
	}
}

// handleSummaryRequest processes a single summary request. Checks the
// deduplication cache and DB before making the Haiku call.
func (s *Server) handleSummaryRequest(ctx context.Context, req summaryRequest) {
	// Check deduplication cache — if we've already seen this session, skip
	if _, exists := s.summarizedSessions.Load(req.sessionID); exists {
		return
	}

	// Check DB — if auto_summary is already populated (e.g. from a previous
	// process lifetime), cache the result and skip
	sess, err := s.db.GetSession(req.sessionID)
	if err != nil {
		s.logger.Printf("Session summarizer: failed to get session '%s': %v", req.sessionID, err)
		return
	}
	if sess != nil && sess.AutoSummary != "" {
		s.summarizedSessions.Store(req.sessionID, true)
		return
	}

	// Generate summary via Haiku
	summary, err := generateSessionSummary(ctx, s.agencDirpath, req.firstUserMessage)
	if err != nil {
		s.logger.Printf("Session summarizer: failed to generate summary for session '%s': %v", req.sessionID, err)
		return
	}

	// Write to DB
	if err := s.db.UpdateSessionAutoSummary(req.sessionID, summary); err != nil {
		s.logger.Printf("Session summarizer: failed to save summary for session '%s': %v", req.sessionID, err)
		return
	}

	// Cache and reconcile
	s.summarizedSessions.Store(req.sessionID, true)
	s.reconcileTmuxWindowTitle(req.missionID)
	s.logger.Printf("Session summarizer: set auto_summary for session '%s': %q", req.sessionID, summary)
}

// generateSessionSummary calls Claude Haiku via the CLI to produce a short
// description of what the user is working on, based on their first message.
func generateSessionSummary(ctx context.Context, agencDirpath string, firstUserMessage string) (string, error) {
	// Truncate long prompts
	truncated := firstUserMessage
	if len(truncated) > summarizerMaxPromptLen {
		truncated = truncated[:summarizerMaxPromptLen-3] + "..."
	}

	systemPrompt := "You are a title generator. You will receive the text of a user's request to an AI coding assistant. " +
		"Your job: output a 3-8 word terminal window title summarizing what the user is working on. " +
		"Rules: output ONLY the title. No quotes. No punctuation at the end. No markdown. No explanation. " +
		"Do NOT answer the user's request. Do NOT ask questions. Do NOT offer help. Just the title."

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

// initSessionSummarizer initializes the session summarizer channel and
// deduplication map on the server. Must be called before starting the worker.
func (s *Server) initSessionSummarizer() {
	s.sessionSummaryCh = make(chan summaryRequest, summarizerChannelSize)
	s.summarizedSessions = &sync.Map{}
}
