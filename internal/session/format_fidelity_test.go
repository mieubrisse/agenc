package session

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The tests here pin behaviours that an adversarial review proved wrong against
// the real transcripts on disk. Each one is the hermetic form of a defect that
// was measured on a live file; the measured numbers are in the comments so a
// future reader can tell what the fixture stands for.

func TestFormatFindsForkProvenanceOutsideTheTailWindow(t *testing.T) {
	// Fork provenance occupies a contiguous PREFIX of a forked transcript — on
	// one real file, records 1 through 2775 of 6049. Scanning only the tailed
	// window therefore lost the banner at `session print`'s default --tail 20,
	// which is the invocation where a silent omission is least likely to be
	// noticed. The scan now covers the whole file.
	var lines []string
	for i := 0; i < 5; i++ {
		lines = append(lines, fmt.Sprintf(
			`{"type":"user","timestamp":"2026-01-01T00:00:%02d.000Z","forkedFrom":{"sessionId":"src-session-id","messageUuid":"m%d"},"uuid":"f%d","message":{"role":"user","content":"forked prefix %d"}}`, i, i, i, i))
	}
	for i := 0; i < 20; i++ {
		lines = append(lines, fmt.Sprintf(
			`{"type":"user","timestamp":"2026-01-01T01:00:%02d.000Z","uuid":"n%d","message":{"role":"user","content":"after the fork %d"}}`, i, i, i))
	}

	tailed := renderLines(t, FormatOptions{TailLines: 3}, lines...)

	// Positive control: the tail really did cut the prefix off.
	mustNotContain(t, tailed, "forked prefix 0")
	mustContain(t, tailed, "[FORK] this session was forked from session src-session-id")
}

func TestFormatSkipsRecordsRepeatedUnderTheSameUUID(t *testing.T) {
	// Real transcripts write the same record twice — a forked file can carry
	// its whole pre-fork conversation verbatim a second time. Rendering both
	// copies shows a conversation that never happened.
	duplicated := `{"type":"user","timestamp":"2026-01-01T00:00:00.000Z","uuid":"same-uuid","message":{"role":"user","content":"SAID ONCE"}}`

	got := renderLines(t, FormatOptions{}, duplicated, duplicated)

	if n := strings.Count(got, "SAID ONCE"); n != 1 {
		t.Errorf("record rendered %d times, want 1\n%s", n, got)
	}
}

func TestFormatKeepsDistinctRecordsThatHaveNoUUID(t *testing.T) {
	// Queue operations, titles and mode switches carry no uuid. Deduplicating
	// on an empty key would collapse all of them into one.
	got := renderLines(t, FormatOptions{},
		`{"type":"queue-operation","operation":"enqueue","timestamp":"2026-01-01T00:00:00.000Z","content":"first instruction"}`,
		`{"type":"queue-operation","operation":"enqueue","timestamp":"2026-01-01T00:00:01.000Z","content":"second instruction"}`,
	)

	mustContain(t, got, "first instruction", "second instruction")
}

func TestSummarizeTranscriptCountsRepeatedRecordsOnce(t *testing.T) {
	// On a 68 MB session on disk, counting lines reported 10011 messages and
	// 3168 tool calls against a deduplicated truth of 4663 and 1482 — an
	// overstatement of more than 100% in a column the new --agents table prints.
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	user := `{"type":"user","uuid":"u1","timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"user","content":"hi"}}`
	assistant := `{"type":"assistant","uuid":"a1","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}`
	content := strings.Join([]string{user, assistant, user, assistant}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	stats, err := SummarizeTranscript(path)
	if err != nil {
		t.Fatalf("SummarizeTranscript: %v", err)
	}

	if stats.UserMessages != 1 || stats.AssistantMessages != 1 || stats.ToolCalls != 1 {
		t.Errorf("user=%d assistant=%d tools=%d, want 1/1/1 after deduplication",
			stats.UserMessages, stats.AssistantMessages, stats.ToolCalls)
	}
}

func TestSummarizeTranscriptCountsUUIDlessRecordsEveryTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	noUUID := `{"type":"user","timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"user","content":"hi"}}`
	if err := os.WriteFile(path, []byte(noUUID+"\n"+noUUID+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	stats, err := SummarizeTranscript(path)
	if err != nil {
		t.Fatalf("SummarizeTranscript: %v", err)
	}

	if stats.UserMessages != 2 {
		t.Errorf("user messages = %d, want 2: records with no uuid cannot be deduplicated", stats.UserMessages)
	}
}

func TestFormatTrailerCoversOnlyTheRenderedTranscript(t *testing.T) {
	// Printing one subagent of a 298-agent session used to append a trailer
	// naming 297 others as "no spawn site in the printed range" — 80% of the
	// output, and it listed the very transcript being printed.
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addAgent("atarget", &AgentMeta{AgentType: "Explore", Description: "the one asked for"},
		assistantLine("2026-01-01T00:01:00.000Z", "TARGET BODY"))
	fs.addAgent("aother1", &AgentMeta{AgentType: "Explore", Description: "unrelated one"},
		assistantLine("2026-01-01T00:02:00.000Z", "other body"))
	fs.addAgent("aother2", &AgentMeta{AgentType: "Explore", Description: "unrelated two"},
		assistantLine("2026-01-01T00:03:00.000Z", "other body two"))
	root := fs.discover()

	target, err := ResolveAgent(root, "atarget")
	if err != nil {
		t.Fatalf("ResolveAgent: %v", err)
	}
	var buf bytes.Buffer
	if err := FormatTranscript(target, FormatOptions{Root: root, ExpandAgents: true}, &buf); err != nil {
		t.Fatalf("FormatTranscript: %v", err)
	}
	got := buf.String()

	mustContain(t, got, "TARGET BODY")
	mustNotContain(t, got, "UNLINKED AGENTS", "aother1", "aother2", "atarget (")
}

func TestFormatTrailerStillCoversTheSessionWhenTheSessionIsRendered(t *testing.T) {
	// The scoping fix must not silence the trailer on the case it exists for.
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addAgent("astray", &AgentMeta{AgentType: "Explore", Description: "never returned"},
		assistantLine("2026-01-01T00:01:00.000Z", "stray body"))
	root := fs.discover()

	var buf bytes.Buffer
	if err := FormatTranscript(root, FormatOptions{Root: root, ExpandAgents: true}, &buf); err != nil {
		t.Fatalf("FormatTranscript: %v", err)
	}

	mustContain(t, buf.String(), "[UNLINKED AGENTS] 1 subagent transcript(s)", "astray")
}

func TestFormatDeclinesASubagentThatStartedBeforeItsSupposedSpawn(t *testing.T) {
	// A transcript that began before the call cannot be that call's output. It
	// belongs to an earlier spawn of the same name, and attributing it here
	// would put one agent's work under another agent's call.
	fs := newFakeSession(t,
		`{"type":"assistant","timestamp":"2026-01-01T12:00:00.000Z","uuid":"s1","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_late","name":"Agent","input":{"description":"the later call","name":"worker"}}]}}`,
	)
	fs.addAgent("aworker-early", &AgentMeta{Name: "worker"},
		assistantLine("2026-01-01T10:00:00.000Z", "WORK FROM AN EARLIER SPAWN"))
	root := fs.discover()

	var buf bytes.Buffer
	if err := FormatTranscript(root, FormatOptions{Root: root, ExpandAgents: true}, &buf); err != nil {
		t.Fatalf("FormatTranscript: %v", err)
	}
	got := buf.String()

	if strings.Contains(got, "[agent aworker-early]") {
		t.Errorf("a transcript that started 2h before the call must not be attributed to it\n%s", got)
	}
	// It must not vanish either: the trailer is what keeps it visible.
	mustContain(t, got, "[UNLINKED AGENTS]", "aworker-early")
}

func TestDecodeAgentResultIgnoresOrdinaryToolResults(t *testing.T) {
	// Ordinary tools also write a toolUseResult. Without the agent-ID guard,
	// every Bash and Read result would render as "< Agent unknown (unknown)".
	got := renderLines(t, FormatOptions{},
		`{"type":"user","timestamp":"2026-01-01T00:00:00.000Z","uuid":"u1","toolUseResult":{"stdout":"file1.txt","stderr":"","interrupted":false},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"file1.txt"}]}}`,
	)

	if strings.Contains(got, "Agent") {
		t.Errorf("an ordinary tool result must not render as a subagent return\n%s", got)
	}
}

func TestTailDoesNotAllocateForTheRequestedSizeUpFront(t *testing.T) {
	// `--tail 20000000` on a one-line file grew the heap by 305 MB, because the
	// ring was allocated for the requested count before anything was read. A
	// mistyped digit count could exhaust memory on a tiny file.
	path := filepath.Join(t.TempDir(), "one.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user","uuid":"u1","message":{"role":"user","content":"only line"}}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	var buf bytes.Buffer
	if err := FormatTranscriptFile(path, FormatOptions{TailLines: 20_000_000}, &buf); err != nil {
		t.Fatalf("FormatTranscriptFile: %v", err)
	}
	runtime.ReadMemStats(&after)

	// Positive control: a render that produced nothing would also allocate
	// nothing, and would pass the size assertion for the wrong reason.
	mustContain(t, buf.String(), "only line")
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 32*1024*1024 {
		t.Errorf("allocated %d bytes for a one-line file, want far under 32 MB", grew)
	}
}
