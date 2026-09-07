package session

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// renderLines writes JSONL records to a temp file and formats them, returning
// the rendered text. It covers the single-file case where no transcript tree is
// involved.
func renderLines(t *testing.T, opts FormatOptions, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
	var buf bytes.Buffer
	if err := FormatTranscriptFile(path, opts, &buf); err != nil {
		t.Fatalf("FormatTranscriptFile: %v", err)
	}
	return buf.String()
}

func mustContain(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n--- output ---\n%s", want, got)
		}
	}
}

func mustNotContain(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if strings.Contains(got, want) {
			t.Errorf("output should not contain %q\n--- output ---\n%s", want, got)
		}
	}
}

func TestFormatRendersCompactionBoundary(t *testing.T) {
	// Without this line the transcript silently jumps: everything above the
	// boundary was dropped from the model's context, and the summary below it
	// reads like a human message.
	got := renderLines(t, FormatOptions{},
		assistantLine("2026-01-01T00:00:00.000Z", "before"),
		`{"type":"system","subtype":"compact_boundary","timestamp":"2026-01-01T00:01:00.000Z","content":"Conversation compacted","compactMetadata":{"trigger":"auto","preTokens":354559,"postTokens":26757,"cumulativeDroppedTokens":327802}}`,
	)

	mustContain(t, got, "COMPACTED", "trigger=auto", "354559 tok before", "26757 tok after", "327802 tok dropped")
}

func TestFormatLabelsCompactSummaryDistinctlyFromAUserTurn(t *testing.T) {
	got := renderLines(t, FormatOptions{},
		`{"type":"user","isCompactSummary":true,"timestamp":"2026-01-01T00:01:00.000Z","message":{"role":"user","content":"This session is being continued from a previous conversation."}}`,
	)

	mustContain(t, got, "COMPACT SUMMARY", "This session is being continued")
	mustNotContain(t, got, "[2026-01-01T00:01:00.000Z USER]")
}

func TestFormatRendersForkProvenance(t *testing.T) {
	got := renderLines(t, FormatOptions{},
		`{"type":"user","timestamp":"2026-01-01T00:00:00.000Z","forkedFrom":{"sessionId":"3666d990-4ef0-4af4-9cab-0e0089c80759","messageUuid":"a83bf03d"},"message":{"role":"user","content":"hi"}}`,
	)

	mustContain(t, got, "[FORK]", "3666d990-4ef0-4af4-9cab-0e0089c80759")
}

func TestFormatTagsMessageOrigin(t *testing.T) {
	// A human order, a peer agent's message and a background-task notification
	// are all written as user records. Rendering them identically is how an
	// agent replaying a transcript mistakes a peer's message for the user's.
	tests := []struct {
		name   string
		origin string
		want   string
	}{
		{"human", `,"origin":{"kind":"human"}`, "USER human"},
		{"peer", `,"origin":{"kind":"peer","from":"build-lead-2"}`, "USER peer:build-lead-2"},
		{"task notification", `,"origin":{"kind":"task-notification"}`, "USER task-notification"},
		{"absent", ``, "[2026-01-01T00:00:00.000Z USER]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderLines(t, FormatOptions{},
				`{"type":"user","timestamp":"2026-01-01T00:00:00.000Z"`+tt.origin+`,"message":{"role":"user","content":"do the thing"}}`)
			mustContain(t, got, tt.want)
		})
	}
}

func TestFormatRendersAgentSpawnAndReturn(t *testing.T) {
	// The Agent tool used to render as a bare "Agent()" because the parameter
	// map only knew the older "Task" name, and its successful result was
	// dropped as an ordinary tool result — so a subagent left no trace at all.
	got := renderLines(t, FormatOptions{},
		`{"type":"assistant","timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Agent","input":{"description":"Extract KB reqs","subagent_type":"Explore"}}]}}`,
		`{"type":"user","timestamp":"2026-01-01T00:05:00.000Z","toolUseResult":{"status":"completed","agentId":"aa8d6202c85e084d7","agentType":"Explore","totalDurationMs":91000,"totalTokens":52248,"totalToolUseCount":7},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"done"}]}}`,
	)

	mustContain(t, got,
		`> Agent("Extract KB reqs", subagent_type="Explore")`,
		"< Agent aa8d6202c85e084d7 (Explore) completed",
		"7 tools", "1m31s", "52248 tok")
}

func TestFormatRendersOneAgentReturnPerRecord(t *testing.T) {
	// toolUseResult is a property of the record, not of a block. A record
	// carrying more than one tool_result block must not repeat the subagent
	// return line once per block.
	got := renderLines(t, FormatOptions{},
		`{"type":"user","timestamp":"2026-01-01T00:05:00.000Z","toolUseResult":{"status":"completed","agentId":"aa8d6202c85e084d7","agentType":"Explore"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"done"},{"type":"tool_result","tool_use_id":"toolu_2","content":"also done"}]}}`,
	)

	if n := strings.Count(got, "< Agent aa8d6202c85e084d7"); n != 1 {
		t.Errorf("agent return rendered %d times, want 1\n%s", n, got)
	}
}

func TestFormatAnnotatesSpawnWithAgentIDWhenTreeIsKnown(t *testing.T) {
	fs := newFakeSession(t,
		`{"type":"assistant","timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Agent","input":{"description":"look","subagent_type":"Explore"}}]}}`,
	)
	fs.addAgent("aabc123", &AgentMeta{AgentType: "Explore", ToolUseID: "toolu_1", Description: "look"},
		userLine("2026-01-01T00:00:10.000Z", "agent prompt"))
	root := fs.discover()

	var buf bytes.Buffer
	if err := FormatTranscript(root, FormatOptions{Root: root}, &buf); err != nil {
		t.Fatalf("FormatTranscript: %v", err)
	}

	// Without expansion the spawn still names the transcript, so the reader
	// knows what to pass to --agent.
	mustContain(t, buf.String(), "[agent aabc123]")
	mustNotContain(t, buf.String(), "begin agent")
}

func TestFormatExpandsAgentTranscriptInline(t *testing.T) {
	fs := newFakeSession(t,
		userLine("2026-01-01T00:00:00.000Z", "go"),
		`{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Agent","input":{"description":"look","subagent_type":"Explore"}}]}}`,
	)
	fs.addAgent("aabc123", &AgentMeta{AgentType: "Explore", ToolUseID: "toolu_1", Description: "look at the KB"},
		userLine("2026-01-01T00:00:10.000Z", "agent prompt here"),
		assistantLine("2026-01-01T00:00:11.000Z", "agent answer here"))
	root := fs.discover()

	var buf bytes.Buffer
	if err := FormatTranscript(root, FormatOptions{Root: root, ExpandAgents: true}, &buf); err != nil {
		t.Fatalf("FormatTranscript: %v", err)
	}
	got := buf.String()

	mustContain(t, got,
		"--- begin agent aabc123 (Explore): look at the KB ---",
		"agent prompt here",
		"agent answer here",
		"--- end agent aabc123 ---")
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "agent answer here") && !strings.HasPrefix(line, nestedIndent) {
			t.Errorf("nested transcript line should be indented, got %q", line)
		}
	}
}

func TestFormatExpandsNamedBackgroundAgentByName(t *testing.T) {
	// A named agent's sidecar carries a name and no toolUseId, and its spawn's
	// tool_result reports status "teammate_spawned" with no agentId. The name
	// is the only link between the call and the transcript.
	fs := newFakeSession(t,
		`{"type":"assistant","timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Agent","input":{"description":"probe","name":"v1d-probe"}}]}}`,
	)
	fs.addAgent("av1d-probe-0edd", &AgentMeta{AgentType: "v1d-probe", Name: "v1d-probe"},
		assistantLine("2026-01-01T00:00:30.000Z", "probe output"))
	root := fs.discover()

	var buf bytes.Buffer
	if err := FormatTranscript(root, FormatOptions{Root: root, ExpandAgents: true}, &buf); err != nil {
		t.Fatalf("FormatTranscript: %v", err)
	}

	mustContain(t, buf.String(), "[agent av1d-probe-0edd]", "probe output")
}

func TestFormatDisambiguatesReusedAgentNamesByStartTime(t *testing.T) {
	fs := newFakeSession(t,
		`{"type":"assistant","timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Agent","input":{"description":"first","name":"worker"}}]}}`,
		`{"type":"assistant","timestamp":"2026-01-01T02:00:00.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_2","name":"Agent","input":{"description":"second","name":"worker"}}]}}`,
	)
	fs.addAgent("aworker-early", &AgentMeta{Name: "worker"}, assistantLine("2026-01-01T00:00:05.000Z", "EARLY RUN"))
	fs.addAgent("aworker-late", &AgentMeta{Name: "worker"}, assistantLine("2026-01-01T02:00:05.000Z", "LATE RUN"))
	root := fs.discover()

	var buf bytes.Buffer
	if err := FormatTranscript(root, FormatOptions{Root: root, ExpandAgents: true}, &buf); err != nil {
		t.Fatalf("FormatTranscript: %v", err)
	}
	got := buf.String()

	earlyIdx := strings.Index(got, "EARLY RUN")
	lateIdx := strings.Index(got, "LATE RUN")
	if earlyIdx < 0 || lateIdx < 0 {
		t.Fatalf("both runs should be inlined\n%s", got)
	}
	if earlyIdx > lateIdx {
		t.Errorf("the earlier agent should be attributed to the earlier spawn")
	}
	if strings.Count(got, "EARLY RUN") != 1 || strings.Count(got, "LATE RUN") != 1 {
		t.Errorf("each transcript should be inlined exactly once")
	}
}

func TestFormatListsAgentsWithNoSpawnSite(t *testing.T) {
	// A subagent whose spawner was killed, or whose spawn falls outside a
	// tailed range, has nothing in the rendered text to hang off. Silently
	// omitting it would make an --expand-agents render look complete when it
	// is not.
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addAgent("aorphan1", &AgentMeta{AgentType: "Explore", Description: "never returned"},
		assistantLine("2026-01-01T00:00:10.000Z", "partial work"))
	root := fs.discover()

	var buf bytes.Buffer
	if err := FormatTranscript(root, FormatOptions{Root: root, ExpandAgents: true}, &buf); err != nil {
		t.Fatalf("FormatTranscript: %v", err)
	}

	mustContain(t, buf.String(), "[UNLINKED AGENTS] 1 subagent transcript(s)", "aorphan1", "never returned")
}

func TestFormatRendersQueuedInstructions(t *testing.T) {
	// A message the user types while a turn is running is written as a queue
	// operation, not as a user record. It used to be invisible, which made
	// "agenc mission print" unable to show that an order had been given.
	lines := []string{
		`{"type":"queue-operation","operation":"enqueue","timestamp":"2026-01-01T00:00:00.000Z","content":"stop and check the diff"}`,
		`{"type":"queue-operation","operation":"remove","timestamp":"2026-01-01T00:00:05.000Z","content":"stop and check the diff"}`,
		`{"type":"attachment","timestamp":"2026-01-01T00:00:06.000Z","attachment":{"type":"queued_command","prompt":"also push it"}}`,
	}

	got := renderLines(t, FormatOptions{}, lines...)
	mustContain(t, got, "QUEUED enqueue", "stop and check the diff", "QUEUED COMMAND", "also push it")
	mustNotContain(t, got, "QUEUE remove")

	verbose := renderLines(t, FormatOptions{Verbose: true}, lines...)
	mustContain(t, verbose, "QUEUE remove")
}

func TestFormatRendersSlashCommandsAndErrors(t *testing.T) {
	got := renderLines(t, FormatOptions{},
		`{"type":"system","subtype":"local_command","timestamp":"2026-01-01T00:00:00.000Z","content":"<command-name>/teleport</command-name>"}`,
		`{"type":"system","subtype":"api_error","timestamp":"2026-01-01T00:00:01.000Z","level":"error","retryAttempt":1,"maxRetries":10}`,
		`{"type":"system","subtype":"agents_killed","timestamp":"2026-01-01T00:00:02.000Z"}`,
		`{"type":"system","subtype":"model_refusal_fallback","timestamp":"2026-01-01T00:00:03.000Z","content":"Switched to Opus 4.8."}`,
	)

	mustContain(t, got, "COMMAND", "/teleport", "ERROR", "retry 1/10", "AGENTS KILLED", "MODEL REFUSAL FALLBACK")
}

func TestFormatShowsFailingHooksAndHidesCleanOnes(t *testing.T) {
	clean := `{"type":"system","subtype":"stop_hook_summary","timestamp":"2026-01-01T00:00:00.000Z","hookErrors":[],"preventedContinuation":false}`
	failing := `{"type":"system","subtype":"stop_hook_summary","timestamp":"2026-01-01T00:00:01.000Z","hookErrors":["boom"],"preventedContinuation":true,"stopReason":"blocked by guard"}`

	got := renderLines(t, FormatOptions{}, clean, failing)
	mustContain(t, got, "STOP HOOK", "1 hook error(s)", "blocked continuation", "blocked by guard")
	if strings.Count(got, "STOP HOOK") != 1 {
		t.Errorf("a clean stop hook should not render by default\n%s", got)
	}

	verbose := renderLines(t, FormatOptions{Verbose: true}, clean, failing)
	if strings.Count(verbose, "STOP HOOK") != 2 {
		t.Errorf("verbose should render both stop hooks\n%s", verbose)
	}
}

func TestFormatVerboseAddsThinkingAttachmentsAndSuccessfulResults(t *testing.T) {
	lines := []string{
		`{"type":"assistant","timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"weighing the options"},{"type":"text","text":"answer"}]}}`,
		`{"type":"user","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"file1.txt"}]}}`,
		`{"type":"attachment","timestamp":"2026-01-01T00:00:02.000Z","attachment":{"type":"file","filename":"/tmp/x.md","displayPath":"x.md"}}`,
		`{"type":"system","subtype":"turn_duration","timestamp":"2026-01-01T00:00:03.000Z","durationMs":95150,"messageCount":50}`,
	}

	got := renderLines(t, FormatOptions{}, lines...)
	mustContain(t, got, "answer")
	mustNotContain(t, got, "weighing the options", "file1.txt", "ATTACHMENT", "TURN")

	verbose := renderLines(t, FormatOptions{Verbose: true}, lines...)
	mustContain(t, verbose, "THINKING", "weighing the options", "file1.txt", "ATTACHMENT file", "x.md", "TURN", "1m35s", "50 messages")
}

func TestFormatVerboseCarriesMinorRecordPayloads(t *testing.T) {
	// These record types carry their whole meaning in one field. Rendering the
	// bare type name reports that something happened and discards what it was.
	lines := []string{
		`{"type":"mode","timestamp":"2026-01-01T00:00:00.000Z","mode":"plan"}`,
		`{"type":"permission-mode","timestamp":"2026-01-01T00:00:01.000Z","permissionMode":"acceptEdits"}`,
		`{"type":"custom-title","timestamp":"2026-01-01T00:00:02.000Z","customTitle":"renamed by the user"}`,
		`{"type":"agent-name","timestamp":"2026-01-01T00:00:03.000Z","agentName":"build-lead-2"}`,
		`{"type":"last-prompt","timestamp":"2026-01-01T00:00:04.000Z","lastPrompt":"land the plane"}`,
	}

	mustNotContain(t, renderLines(t, FormatOptions{}, lines...), "MODE", "CUSTOM-TITLE")

	verbose := renderLines(t, FormatOptions{Verbose: true}, lines...)
	mustContain(t, verbose,
		"[2026-01-01T00:00:00.000Z MODE] plan",
		"[2026-01-01T00:00:01.000Z PERMISSION-MODE] acceptEdits",
		"[2026-01-01T00:00:02.000Z CUSTOM-TITLE] renamed by the user",
		"[2026-01-01T00:00:03.000Z AGENT-NAME] build-lead-2",
		"[2026-01-01T00:00:04.000Z LAST-PROMPT] land the plane")
}

func TestFormatTailAppliesToTheMainFileButNotToInlinedAgents(t *testing.T) {
	fs := newFakeSession(t,
		userLine("2026-01-01T00:00:00.000Z", "OLD TURN THAT THE TAIL DROPS"),
		assistantLine("2026-01-01T00:00:01.000Z", "old reply"),
		`{"type":"assistant","timestamp":"2026-01-01T00:00:02.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Agent","input":{"description":"look","subagent_type":"Explore"}}]}}`,
	)
	fs.addAgent("aabc123", &AgentMeta{AgentType: "Explore", ToolUseID: "toolu_1"},
		userLine("2026-01-01T00:00:10.000Z", "AGENT FIRST LINE"),
		assistantLine("2026-01-01T00:00:11.000Z", "AGENT LAST LINE"))
	root := fs.discover()

	var buf bytes.Buffer
	if err := FormatTranscript(root, FormatOptions{Root: root, ExpandAgents: true, TailLines: 1}, &buf); err != nil {
		t.Fatalf("FormatTranscript: %v", err)
	}
	got := buf.String()

	mustNotContain(t, got, "OLD TURN THAT THE TAIL DROPS")
	// A tail of a subagent would cut off the prompt that gives its output
	// meaning, so inlined subagents are always rendered whole.
	mustContain(t, got, "AGENT FIRST LINE", "AGENT LAST LINE")
}

func TestFormatDoesNotInlineAnAgentTwiceForDuplicateRecords(t *testing.T) {
	// Real transcripts contain records written twice with the same uuid. Both
	// copies render, so a spawn can be seen twice; the subagent's transcript
	// must still appear once.
	spawn := `{"type":"assistant","timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Agent","input":{"description":"look","subagent_type":"Explore"}}]}}`
	fs := newFakeSession(t, spawn, spawn)
	fs.addAgent("aabc123", &AgentMeta{AgentType: "Explore", ToolUseID: "toolu_1"},
		assistantLine("2026-01-01T00:00:10.000Z", "AGENT BODY"))
	root := fs.discover()

	var buf bytes.Buffer
	if err := FormatTranscript(root, FormatOptions{Root: root, ExpandAgents: true}, &buf); err != nil {
		t.Fatalf("FormatTranscript: %v", err)
	}

	if got := strings.Count(buf.String(), "AGENT BODY"); got != 1 {
		t.Errorf("subagent transcript inlined %d times, want 1\n%s", got, buf.String())
	}
}

func TestFormatDefaultShapeIsUnchangedForPlainConversation(t *testing.T) {
	// The default render must stay byte-identical to what the formatter
	// produced before the tree work: header, body, blank line between blocks,
	// no trailing blank line.
	got := renderLines(t, FormatOptions{},
		userLine("2026-01-01T00:00:00.000Z", "Hello"),
		assistantLine("2026-01-01T00:00:01.000Z", "Hi"),
	)

	want := "[2026-01-01T00:00:00.000Z USER]\nHello\n\n[2026-01-01T00:00:01.000Z ASSISTANT]\nHi\n"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// --- Long-tail record classes -----------------------------------------------
//
// Everything below pins a rendering branch that the tests above leave
// unexercised. Each one was chosen because mutating the branch it covers left
// the rest of the suite green.

// spawnLine builds an assistant record whose single tool_use is a subagent
// spawn with the given tool-use ID, which is what a sidecar's toolUseId links
// against.
func spawnLine(timestamp string, toolUseID string, description string) string {
	return `{"type":"assistant","timestamp":"` + timestamp + `","message":{"role":"assistant","content":[{"type":"tool_use","id":` + quote(toolUseID) + `,"name":"Agent","input":{"description":` + quote(description) + `}}]}}`
}

func TestFormatRendersSummaryRecords(t *testing.T) {
	// A summary record is the auto-generated title of a resumed conversation;
	// dropping it removes the only line saying what the session was about.
	got := renderLines(t, FormatOptions{},
		`{"type":"summary","timestamp":"2026-01-01T00:00:00.000Z","summary":"Refactored the pool linker"}`,
		`{"type":"summary","timestamp":"2026-01-01T00:00:01.000Z","summary":""}`,
	)

	mustContain(t, got, "[2026-01-01T00:00:00.000Z SUMMARY]\nRefactored the pool linker")
	// An empty summary carries nothing, so it renders no header at all.
	mustNotContain(t, got, "[2026-01-01T00:00:01.000Z SUMMARY]")
}

func TestFormatPrefixesSystemNoticesWithTheirLevel(t *testing.T) {
	// api_error has its own earlier case and never reaches the level prefix; an
	// "informational" record does. A warning-level notice rendered as an
	// ordinary one is how a degraded run reads as a healthy one in a replay.
	got := renderLines(t, FormatOptions{},
		`{"type":"system","subtype":"informational","timestamp":"2026-01-01T00:00:00.000Z","level":"warning","content":"context is running low"}`,
		`{"type":"system","subtype":"informational","timestamp":"2026-01-01T00:00:01.000Z","level":"error","content":"tool call rejected"}`,
		`{"type":"system","subtype":"informational","timestamp":"2026-01-01T00:00:02.000Z","content":"nothing special"}`,
	)

	mustContain(t, got,
		"[2026-01-01T00:00:00.000Z WARNING INFORMATIONAL] context is running low",
		"[2026-01-01T00:00:01.000Z ERROR INFORMATIONAL] tool call rejected",
		"[2026-01-01T00:00:02.000Z INFORMATIONAL] nothing special")
}

func TestFormatRendersAPIErrorsWithoutRetryCounts(t *testing.T) {
	// Not every api_error record carries maxRetries. Without the bare-detail
	// branch the line would render as a header with nothing after it.
	got := renderLines(t, FormatOptions{},
		`{"type":"system","subtype":"api_error","timestamp":"2026-01-01T00:00:00.000Z","level":"error"}`,
	)

	mustContain(t, got, "[2026-01-01T00:00:00.000Z ERROR] API error")
	mustNotContain(t, got, "retry")
}

func TestFormatRendersPopAllQueueOperations(t *testing.T) {
	// popAll drains a whole queue of typed-ahead instructions into a turn.
	// Treating it as bookkeeping loses every message in it.
	got := renderLines(t, FormatOptions{},
		`{"type":"queue-operation","operation":"popAll","timestamp":"2026-01-01T00:00:00.000Z","content":"actually stop and rebase first"}`,
	)

	mustContain(t, got, "[2026-01-01T00:00:00.000Z QUEUED popAll] actually stop and rebase first")
}

func TestFormatVerboseStopHookDetail(t *testing.T) {
	// The default-mode test only counts STOP HOOK occurrences, so neither the
	// verbose hook count nor the clean-run wording is pinned by it.
	clean := `{"type":"system","subtype":"stop_hook_summary","timestamp":"2026-01-01T00:00:00.000Z","hookErrors":[]}`
	ranHooks := `{"type":"system","subtype":"stop_hook_summary","timestamp":"2026-01-01T00:00:01.000Z","hookErrors":[],"hookInfos":[{"name":"a"},{"name":"b"},{"name":"c"}]}`

	got := renderLines(t, FormatOptions{Verbose: true}, clean, ranHooks)

	mustContain(t, got,
		"[2026-01-01T00:00:00.000Z STOP HOOK] ran clean",
		"[2026-01-01T00:00:01.000Z STOP HOOK] 3 hook(s) ran")
}

func TestFormatDecodesEveryEventContentShape(t *testing.T) {
	// A record's content is written as a bare string, an array of strings, an
	// array of {text}/{content} blocks, or a single object. A shape the decoder
	// cannot read renders the event with its payload silently missing.
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"bare string", `"plain content"`, "plain content"},
		{"array of strings", `["alpha","bravo"]`, "alpha bravo"},
		{"array of blocks", `[{"text":"gamma"},{"content":"delta"}]`, "gamma delta"},
		{"single object", `{"text":"epsilon"}`, "epsilon"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderLines(t, FormatOptions{},
				`{"type":"system","subtype":"local_command","timestamp":"2026-01-01T00:00:00.000Z","content":`+tt.content+`}`)
			mustContain(t, got, "[2026-01-01T00:00:00.000Z COMMAND] "+tt.want)
		})
	}
}

func TestFormatAttachmentFallsBackThroughEveryLabelField(t *testing.T) {
	// displayPath short-circuits the fallback chain, so a displayPath fixture
	// exercises none of the fields below it — and an attachment whose only
	// identifying field is one of those renders as a bare header.
	tests := []struct {
		name       string
		attachment string
		want       string
	}{
		{"displayPath", `{"type":"file","displayPath":"docs/x.md"}`, "ATTACHMENT file] docs/x.md"},
		{"filename", `{"type":"file","filename":"/tmp/only-filename.md"}`, "ATTACHMENT file] /tmp/only-filename.md"},
		{"path", `{"type":"file","path":"/tmp/only-path.md"}`, "ATTACHMENT file] /tmp/only-path.md"},
		{"banner", `{"type":"notice","banner":"BANNER ONLY"}`, "ATTACHMENT notice] BANNER ONLY"},
		{"hookName", `{"type":"hook","hookName":"PreToolUse-guard"}`, "ATTACHMENT hook] PreToolUse-guard"},
		{"description", `{"type":"diagnostic","description":"lint reported 3 problems"}`, "ATTACHMENT diagnostic] lint reported 3 problems"},
		{"style", `{"type":"output_style","style":"explanatory"}`, "ATTACHMENT output_style] explanatory"},
		{"url", `{"type":"link","url":"https://example.com/spec"}`, "ATTACHMENT link] https://example.com/spec"},
		{"content", `{"type":"hook_result","content":"hook stdout body"}`, "ATTACHMENT hook_result] hook stdout body"},
		{"untyped with no identifying field", `{}`, "ATTACHMENT unknown]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderLines(t, FormatOptions{Verbose: true},
				`{"type":"attachment","timestamp":"2026-01-01T00:00:00.000Z","attachment":`+tt.attachment+`}`)
			mustContain(t, got, "[2026-01-01T00:00:00.000Z "+tt.want)
		})
	}
}

func TestFormatSkipsMalformedAttachments(t *testing.T) {
	// An attachment payload that is not an object must render nothing, rather
	// than an "ATTACHMENT unknown" header that looks like a real record.
	got := renderLines(t, FormatOptions{Verbose: true},
		`{"type":"attachment","timestamp":"2026-01-01T00:00:00.000Z","attachment":"not an object"}`,
		`{"type":"attachment","timestamp":"2026-01-01T00:00:01.000Z","attachment":{"type":"file","displayPath":"real.md"}}`,
	)

	// The well-formed neighbour proves the attachment path ran at all, so the
	// absence below is a real skip rather than a render that never happened.
	mustContain(t, got, "[2026-01-01T00:00:01.000Z ATTACHMENT file] real.md")
	mustNotContain(t, got, "2026-01-01T00:00:00.000Z ATTACHMENT")
}

func TestFormatEventDetailOccupiesExactlyOneLine(t *testing.T) {
	// The documented contract for an event is one line. Both halves of it are
	// load-bearing: an untruncated hook payload floods the render, and an
	// uncollapsed one puts transcript text where a header is expected.
	longTail := strings.Repeat("A", maxEventContentLen+20) + "TAIL_MARKER"

	got := renderLines(t, FormatOptions{},
		`{"type":"system","subtype":"local_command","timestamp":"2026-01-01T00:00:00.000Z","content":`+quote(longTail)+`}`,
		`{"type":"system","subtype":"local_command","timestamp":"2026-01-01T00:00:01.000Z","content":"first fragment\nsecond fragment\n\n  third fragment"}`,
	)

	mustContain(t, got, "...")
	mustNotContain(t, got, "TAIL_MARKER")
	mustContain(t, got, "[2026-01-01T00:00:01.000Z COMMAND] first fragment second fragment third fragment")
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "second fragment") && !strings.Contains(line, "first fragment") {
			t.Errorf("event detail wrapped onto a second line: %q", line)
		}
	}
}

func TestFormatHidesMetaUserRecordsByDefault(t *testing.T) {
	// isMeta records are Claude Code's own injected scaffolding, not something
	// the user said. Rendering them by default puts words in the user's mouth.
	meta := `{"type":"user","isMeta":true,"timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"user","content":"Caveat: the messages below were generated by a hook."}}`

	got := renderLines(t, FormatOptions{}, meta, userLine("2026-01-01T00:00:01.000Z", "a real turn"))
	mustContain(t, got, "a real turn")
	mustNotContain(t, got, "Caveat: the messages below")

	mustContain(t, renderLines(t, FormatOptions{Verbose: true}, meta), "Caveat: the messages below")
}

func TestFormatExpandsDeepAgentChainsUpToTheNestingLimit(t *testing.T) {
	// A chain one longer than maxExpansionDepth: expansion must recurse the
	// whole chain and then say so when it stops, because an agent silently
	// dropped at the bound is indistinguishable from one that never ran.
	ids := []string{"a01", "a02", "a03", "a04", "a05", "a06", "a07", "a08", "a09", "a10", "a11", "a12", "a13"}

	fs := newFakeSession(t, spawnLine("2026-01-01T00:00:00.000Z", "toolu_a01", "start the chain"))
	// Carry the neighbours in variables rather than indexing off i: the guarded
	// ids[i-1] is safe, but gosec cannot prove it (G602), and a lint suppression
	// would be a worse trade than simply not indexing.
	previousID := ""
	for i, id := range ids {
		// An empty ParentAgentID attaches the first agent to the session root.
		meta := &AgentMeta{AgentType: "general-purpose", ToolUseID: "toolu_" + id, ParentAgentID: previousID}
		lines := []string{assistantLine("2026-01-01T00:01:00.000Z", "BODY-"+id)}
		for _, nextID := range ids[i+1:] {
			lines = append(lines, spawnLine("2026-01-01T00:01:01.000Z", "toolu_"+nextID, "go deeper"))
			break
		}
		fs.addAgent(id, meta, lines...)
		previousID = id
	}
	root := fs.discover()

	var buf bytes.Buffer
	if err := FormatTranscript(root, FormatOptions{Root: root, ExpandAgents: true}, &buf); err != nil {
		t.Fatalf("FormatTranscript: %v", err)
	}
	got := buf.String()

	// Nesting works well past one level, all the way to the bound.
	mustContain(t, got, "--- begin agent a03 (general-purpose) ---", "--- begin agent a12 (general-purpose) ---")
	for _, id := range ids[:len(ids)-1] {
		mustContain(t, got, "BODY-"+id)
	}
	// The 13th is refused, and says why.
	mustContain(t, got, "[agent a13 not expanded: nesting limit 12 reached]")
	mustNotContain(t, got, "BODY-a13", "--- begin agent a13")
	// The refusal is the anchor, so a13 is not also reported as unlinked.
	mustNotContain(t, got, "[UNLINKED AGENTS]")
}

func TestFormatReportsAnUnreadableAgentTranscript(t *testing.T) {
	fs := newFakeSession(t, spawnLine("2026-01-01T00:00:00.000Z", "toolu_1", "look"))
	fs.addAgent("aunreadable", &AgentMeta{AgentType: "Explore", ToolUseID: "toolu_1"},
		assistantLine("2026-01-01T00:00:10.000Z", "unreachable body"))

	path := filepath.Join(fs.projectDir, fs.sessionID, "subagents", "agent-aunreadable.jsonl")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	// Running as root defeats the permission bit, which would leave this test
	// asserting on a transcript that read back perfectly fine.
	if f, err := os.Open(path); err == nil {
		f.Close()
		t.Skip("cannot make a file unreadable in this environment")
	}

	root := fs.discover()
	var buf bytes.Buffer
	if err := FormatTranscript(root, FormatOptions{Root: root, ExpandAgents: true}, &buf); err != nil {
		t.Fatalf("FormatTranscript: %v", err)
	}

	// A subagent that cannot be read must say so where its transcript belongs,
	// not vanish from a render that claims to be complete.
	mustContain(t, buf.String(), "[agent aunreadable transcript unreadable:")
	mustNotContain(t, buf.String(), "unreachable body")
}

func TestFormatReportsAnAgentWithNoConversationMessages(t *testing.T) {
	// A subagent that wrote only bookkeeping records renders an empty body.
	// Without the placeholder the spawn's begin/end markers wrap nothing, which
	// reads as a formatting bug rather than as the fact it is.
	fs := newFakeSession(t, spawnLine("2026-01-01T00:00:00.000Z", "toolu_1", "look"))
	fs.addAgent("aempty", &AgentMeta{AgentType: "Explore", ToolUseID: "toolu_1"},
		`{"type":"file-history-snapshot","timestamp":"2026-01-01T00:00:10.000Z"}`,
		`{"type":"file-history-snapshot","timestamp":"2026-01-01T00:00:11.000Z"}`)
	root := fs.discover()

	var buf bytes.Buffer
	if err := FormatTranscript(root, FormatOptions{Root: root, ExpandAgents: true}, &buf); err != nil {
		t.Fatalf("FormatTranscript: %v", err)
	}

	mustContain(t, buf.String(),
		"--- begin agent aempty (Explore) ---",
		"(no conversation messages)",
		"--- end agent aempty ---")
}

func TestFormatIndentsContinuationLinesOfMultiLineValues(t *testing.T) {
	// A wrapped body whose later lines start at column zero is indistinguishable
	// from a new transcript entry.
	got := renderLines(t, FormatOptions{Verbose: true},
		`{"type":"user","timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"result line one\nresult line two"}]}}`,
		`{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"thought line one\nthought line two"}]}}`,
	)

	mustContain(t, got,
		"  < result line one\n    result line two",
		"  ~ THINKING: thought line one\n    thought line two")
}

func TestFormatRendersSubSecondAndSubMinuteAgentDurations(t *testing.T) {
	// Only the minutes branch is exercised elsewhere, and most subagent runs
	// are shorter than that.
	tests := []struct {
		name       string
		durationMs string
		want       string
	}{
		{"milliseconds", "820", "820ms"},
		{"seconds", "12400", "12.4s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderLines(t, FormatOptions{},
				`{"type":"user","timestamp":"2026-01-01T00:05:00.000Z","toolUseResult":{"status":"completed","agentId":"adur1","agentType":"Explore","totalDurationMs":`+tt.durationMs+`},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"done"}]}}`)
			mustContain(t, got, "< Agent adur1 (Explore) completed - "+tt.want)
		})
	}
}

func TestFormatOrdersUnlinkedAgentsByStartTime(t *testing.T) {
	// The trailer's own sort is what puts these in start order: the tree walk
	// reaches them parent-before-child, which here is not chronological.
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addAgent("aparent", &AgentMeta{AgentType: "Explore"},
		assistantLine("2026-01-01T00:05:00.000Z", "parent work"))
	fs.addAgent("asibling", &AgentMeta{AgentType: "Explore"},
		assistantLine("2026-01-01T00:03:00.000Z", "sibling work"))
	fs.addAgent("achild", &AgentMeta{AgentType: "Explore", ParentAgentID: "aparent"},
		assistantLine("2026-01-01T00:01:00.000Z", "child work"))
	root := fs.discover()

	var buf bytes.Buffer
	if err := FormatTranscript(root, FormatOptions{Root: root, ExpandAgents: true}, &buf); err != nil {
		t.Fatalf("FormatTranscript: %v", err)
	}
	got := buf.String()

	trailerIdx := strings.Index(got, "[UNLINKED AGENTS] 3 subagent transcript(s)")
	if trailerIdx < 0 {
		t.Fatalf("expected all three agents in an unlinked-agents trailer\n%s", got)
	}
	trailer := got[trailerIdx:]
	child := strings.Index(trailer, "achild")
	sibling := strings.Index(trailer, "asibling")
	parent := strings.Index(trailer, "aparent")
	if child < 0 || sibling < 0 || parent < 0 {
		t.Fatalf("trailer should name all three agents, got\n%s", trailer)
	}
	if child > sibling || sibling > parent {
		t.Errorf("trailer order = achild@%d asibling@%d aparent@%d, want chronological 00:01 < 00:03 < 00:05\n%s",
			child, sibling, parent, trailer)
	}
}
