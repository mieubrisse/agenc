package cmd

import (
	"io"
	"strings"
	"testing"
	"time"
)

// TestPayloadIsNeededForEvent pins which events pay the stdin read. Adding an
// event here without the wrapper needing its payload costs every one of those
// hooks up to stdinReadTimeout; leaving StopFailure out silently disables the
// subagent guard, because the payload it is read from would never arrive.
func TestPayloadIsNeededForEvent(t *testing.T) {
	needsPayload := map[string]bool{
		"Notification":       true,
		"StopFailure":        true,
		"Stop":               false,
		"UserPromptSubmit":   false,
		"PostToolUse":        false,
		"PostToolUseFailure": false,
	}
	for event, expected := range needsPayload {
		if actual := payloadIsNeededForEvent(event); actual != expected {
			t.Errorf("payloadIsNeededForEvent(%q) = %v, want %v", event, actual, expected)
		}
	}
}

func TestExtractClaudeHookPayload(t *testing.T) {
	testCases := []struct {
		name                     string
		stdin                    string
		expectedNotificationType string
		expectedAgentID          string
		expectedError            string
		expectedParsed           bool
	}{
		{
			// The main agent's turn died. agent_id is absent, which is the
			// signal the wrapper acts on — so Parsed must be true to tell it
			// apart from a read that simply failed.
			name:           "StopFailure from the main agent has no agent_id",
			stdin:          `{"hook_event_name":"StopFailure","error":"rate_limit","last_assistant_message":"API Error: 429"}`,
			expectedError:  "rate_limit",
			expectedParsed: true,
		},
		{
			name:            "StopFailure from a subagent carries agent_id",
			stdin:           `{"hook_event_name":"StopFailure","error":"unknown","agent_id":"a9721025944e80c23","agent_type":"general-purpose"}`,
			expectedAgentID: "a9721025944e80c23",
			expectedError:   "unknown",
			expectedParsed:  true,
		},
		{
			name:                     "Notification still yields notification_type",
			stdin:                    `{"hook_event_name":"Notification","notification_type":"permission_prompt"}`,
			expectedNotificationType: "permission_prompt",
			expectedParsed:           true,
		},
		{
			// Every failure path below must report Parsed false. The wrapper
			// treats that as "nothing is known", which is what stops a failed
			// read from being mistaken for a main-agent failure.
			name:           "empty stdin is not parsed",
			stdin:          "",
			expectedParsed: false,
		},
		{
			name:           "malformed JSON is not parsed",
			stdin:          `{"hook_event_name":"StopFailure"`,
			expectedParsed: false,
		},
		{
			name:           "JSON that is not an object is not parsed",
			stdin:          `["StopFailure"]`,
			expectedParsed: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload := extractClaudeHookPayload(strings.NewReader(testCase.stdin))

			if payload.NotificationType != testCase.expectedNotificationType {
				t.Errorf("notification type = %q, want %q", payload.NotificationType, testCase.expectedNotificationType)
			}
			if payload.AgentID != testCase.expectedAgentID {
				t.Errorf("agent ID = %q, want %q", payload.AgentID, testCase.expectedAgentID)
			}
			if payload.Error != testCase.expectedError {
				t.Errorf("error kind = %q, want %q", payload.Error, testCase.expectedError)
			}
			if payload.Parsed != testCase.expectedParsed {
				t.Errorf("parsed = %v, want %v", payload.Parsed, testCase.expectedParsed)
			}
		})
	}
}

// neverEOFReader blocks forever instead of returning io.EOF, standing in for
// the case the stdin timeout exists to survive: Claude Code handing a hook an
// open stdin it never closes.
type neverEOFReader struct {
	released chan struct{}
}

func (r *neverEOFReader) Read(p []byte) (int, error) {
	<-r.released
	return 0, io.EOF
}

// TestExtractClaudeHookPayload_TimeoutIsNotParsed is the single most important
// test in this file. A timed-out read leaves AgentID empty for a reason that
// has nothing to do with which agent failed; if it reported Parsed true, the
// wrapper would read that empty AgentID as "the main agent's turn died" and
// mark a mission idle while it is still working.
func TestExtractClaudeHookPayload_TimeoutIsNotParsed(t *testing.T) {
	reader := &neverEOFReader{released: make(chan struct{})}
	// Release the blocked goroutine when the test ends so it does not leak.
	t.Cleanup(func() { close(reader.released) })

	start := time.Now()
	payload := extractClaudeHookPayload(reader)
	elapsed := time.Since(start)

	if payload.Parsed {
		t.Error("expected parsed=false after a stdin read that never reached EOF")
	}
	if payload.AgentID != "" {
		t.Errorf("expected empty agent ID on timeout, got %q", payload.AgentID)
	}
	if elapsed < stdinReadTimeout {
		t.Errorf("returned after %s, before the %s timeout could have elapsed", elapsed, stdinReadTimeout)
	}
	// The hook must not hold Claude up appreciably longer than the timeout.
	if elapsed > stdinReadTimeout*4 {
		t.Errorf("took %s, far longer than the %s timeout", elapsed, stdinReadTimeout)
	}
}
