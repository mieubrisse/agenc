package wrapper

import (
	"context"
	"testing"
	"time"
)

// TestStopFailureStateTransitions covers the StopFailure arm of
// handleClaudeUpdate. A turn that dies on an API error fires StopFailure
// instead of Stop, so without this arm the wrapper reports "busy" forever —
// but the same event also fires when a subagent dies while the main agent
// keeps working, and acting on that would mark a working mission idle.
//
// Every row starts from the busy state a turn in flight would be in, so a
// row that expects no change is genuinely asserting the mission was spared.
func TestStopFailureStateTransitions(t *testing.T) {
	testCases := []struct {
		name               string
		request            ClaudeUpdateRequest
		expectedIdle       bool
		expectedClaudeStat string
	}{
		{
			// The case this whole change exists for.
			name: "main agent failure goes idle",
			request: ClaudeUpdateRequest{
				Event:         "StopFailure",
				HookError:     "server_error",
				PayloadParsed: true,
			},
			expectedIdle:       true,
			expectedClaudeStat: "idle",
		},
		{
			// A usage quota is the one error kind that does not mean the turn
			// is over: Claude Code can wait out the reset and resume. Reporting
			// idle here would let the reaper kill the mission at 30 minutes,
			// and an observed real pause ran 70.
			name: "quota failure is NOT idle",
			request: ClaudeUpdateRequest{
				Event:         "StopFailure",
				HookError:     "rate_limit",
				PayloadParsed: true,
			},
			expectedIdle:       false,
			expectedClaudeStat: "quota_blocked",
		},
		{
			// KEV-70 in reverse: the main agent is still working.
			name: "subagent failure leaves the mission busy",
			request: ClaudeUpdateRequest{
				Event:         "StopFailure",
				HookError:     "unknown",
				AgentID:       "a9721025944e80c23",
				PayloadParsed: true,
			},
			expectedIdle:       false,
			expectedClaudeStat: "busy",
		},
		{
			// An unread payload says nothing about who failed. Treating its
			// empty AgentID as "the main agent" is the fail-open bug.
			name: "unparsed payload leaves the mission busy",
			request: ClaudeUpdateRequest{
				Event:         "StopFailure",
				PayloadParsed: false,
			},
			expectedIdle:       false,
			expectedClaudeStat: "busy",
		},
		{
			// Belt and braces: an unread payload that somehow still carried an
			// agent ID must not be acted on either.
			name: "unparsed payload with an agent ID leaves the mission busy",
			request: ClaudeUpdateRequest{
				Event:         "StopFailure",
				AgentID:       "a9721025944e80c23",
				PayloadParsed: false,
			},
			expectedIdle:       false,
			expectedClaudeStat: "busy",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client, w := startStopFailureTestWrapper(t)

			// Put the wrapper in the state a turn in flight leaves it in.
			if err := client.SendClaudeUpdate(ClaudeUpdateRequest{Event: "UserPromptSubmit"}); err != nil {
				t.Fatalf("UserPromptSubmit failed: %v", err)
			}
			if w.getClaudeStateStringUnderLock() != "busy" {
				t.Fatalf("precondition failed: expected busy before StopFailure, got %q", w.getClaudeStateStringUnderLock())
			}

			if err := client.SendClaudeUpdate(testCase.request); err != nil {
				t.Fatalf("StopFailure failed: %v", err)
			}

			w.stateMu.RLock()
			actualIdle := w.claudeIdle
			actualNeedsAttention := w.needsAttention
			w.stateMu.RUnlock()

			if actualIdle != testCase.expectedIdle {
				t.Errorf("claudeIdle = %v, want %v", actualIdle, testCase.expectedIdle)
			}
			if actual := w.getClaudeStateStringUnderLock(); actual != testCase.expectedClaudeStat {
				t.Errorf("claude state = %q, want %q", actual, testCase.expectedClaudeStat)
			}
			// needs_attention vetoes reaping exactly as busy does, so a
			// StopFailure must never leave it set — that would swap one
			// never-reclaimed mission for another.
			if actualNeedsAttention {
				t.Error("needsAttention must be false after any StopFailure")
			}
		})
	}
}

// TestStopFailureClearsNeedsAttention covers the path where the turn died while
// a permission prompt was still on screen. The prompt is moot once the turn is
// over, and leaving needsAttention set would keep the reaper off the mission.
func TestStopFailureClearsNeedsAttention(t *testing.T) {
	client, w := startStopFailureTestWrapper(t)

	if err := client.SendClaudeUpdate(ClaudeUpdateRequest{Event: "UserPromptSubmit"}); err != nil {
		t.Fatalf("UserPromptSubmit failed: %v", err)
	}
	notification := ClaudeUpdateRequest{
		Event:            "Notification",
		NotificationType: "permission_prompt",
		PayloadParsed:    true,
	}
	if err := client.SendClaudeUpdate(notification); err != nil {
		t.Fatalf("Notification failed: %v", err)
	}
	if actual := w.getClaudeStateStringUnderLock(); actual != "needs_attention" {
		t.Fatalf("precondition failed: expected needs_attention, got %q", actual)
	}

	mainAgentFailure := ClaudeUpdateRequest{
		Event:         "StopFailure",
		HookError:     "overloaded",
		PayloadParsed: true,
	}
	if err := client.SendClaudeUpdate(mainAgentFailure); err != nil {
		t.Fatalf("StopFailure failed: %v", err)
	}

	if actual := w.getClaudeStateStringUnderLock(); actual != "idle" {
		t.Errorf("claude state = %q, want \"idle\"", actual)
	}
}

// startStopFailureTestWrapper brings up a wrapper with its HTTP server and
// event loop running, and returns a client pointed at it. Mirrors the setup in
// TestClaudeUpdateEventsStateTracking.
func startStopFailureTestWrapper(t *testing.T) (*WrapperClient, *Wrapper) {
	t.Helper()

	setup := setupTest(t)
	t.Cleanup(setup.cleanup)

	w := createTestWrapper(setup.agencDirpath, setup.missionID, "github.com/test/repo")
	w.claudeIdle = true
	w.hasConversation = false
	w.commandCh = make(chan commandWithResponse, 1)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go startHTTPServer(ctx, setup.socketFilepath, w, w.logger)
	startTestEventLoop(ctx, w, 0)

	if err := waitForSocket(setup.socketFilepath, 2*time.Second); err != nil {
		t.Fatalf("HTTP server not ready: %v", err)
	}

	return NewWrapperClient(setup.socketFilepath, 1*time.Second), w
}

// getClaudeStateStringUnderLock is the test-side read of the state the server's
// reaper sees. getClaudeStateString itself requires the caller to hold stateMu.
func (w *Wrapper) getClaudeStateStringUnderLock() string {
	w.stateMu.RLock()
	defer w.stateMu.RUnlock()
	return w.getClaudeStateString()
}

// TestQuotaBlockedClearsWhenWorkResumes covers the exits from the quota state.
// Without them a mission that resumed would keep reporting quota_blocked and
// would be spared forever — trading a premature kill for a permanent leak.
func TestQuotaBlockedClearsWhenWorkResumes(t *testing.T) {
	quotaFailure := ClaudeUpdateRequest{
		Event:         "StopFailure",
		HookError:     "rate_limit",
		PayloadParsed: true,
	}

	testCases := []struct {
		name          string
		resumeEvent   ClaudeUpdateRequest
		expectedState string
	}{
		{
			// Claude Code's own signal that an armed quota wait fired.
			name: "the quota auto-resume notification",
			resumeEvent: ClaudeUpdateRequest{
				Event:            "Notification",
				NotificationType: "quota_auto_resume_fired",
				PayloadParsed:    true,
			},
			expectedState: "busy",
		},
		{
			// Backstop: a tool running proves the wait is over even if the
			// notification never arrived.
			name:          "a tool call completing",
			resumeEvent:   ClaudeUpdateRequest{Event: "PostToolUse"},
			expectedState: "busy",
		},
		{
			name:          "the user submitting a new prompt",
			resumeEvent:   ClaudeUpdateRequest{Event: "UserPromptSubmit"},
			expectedState: "busy",
		},
		{
			// A turn that actually finishes supersedes the wait.
			name:          "the turn finishing normally",
			resumeEvent:   ClaudeUpdateRequest{Event: "Stop"},
			expectedState: "idle",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client, w := startStopFailureTestWrapper(t)

			if err := client.SendClaudeUpdate(ClaudeUpdateRequest{Event: "UserPromptSubmit"}); err != nil {
				t.Fatalf("UserPromptSubmit failed: %v", err)
			}
			if err := client.SendClaudeUpdate(quotaFailure); err != nil {
				t.Fatalf("quota StopFailure failed: %v", err)
			}
			if actual := w.getClaudeStateStringUnderLock(); actual != "quota_blocked" {
				t.Fatalf("precondition failed: expected quota_blocked, got %q", actual)
			}

			if err := client.SendClaudeUpdate(testCase.resumeEvent); err != nil {
				t.Fatalf("resume event failed: %v", err)
			}
			if actual := w.getClaudeStateStringUnderLock(); actual != testCase.expectedState {
				t.Errorf("claude state = %q, want %q", actual, testCase.expectedState)
			}
		})
	}
}
