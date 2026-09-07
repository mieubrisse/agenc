package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/odyssey/agenc/internal/session"
)

// ansiEscapeByte is the byte every ANSI escape sequence starts with. Command
// output is consumed by agents, so a colour code in a table cell breaks a
// comparison silently — see the no-ANSI convention in CLAUDE.md.
const ansiEscapeByte = "\033"

// writeFakeSession lays out a Claude project directory the way Claude Code
// does: a main transcript plus a subagents directory beside it. It returns the
// main transcript's path, which is what the print path is handed.
func writeFakeSession(t *testing.T, mainLines []string, agents map[string][2]string) string {
	t.Helper()
	projectDir := t.TempDir()
	sessionID := "11111111-2222-3333-4444-555555555555"

	mainFilepath := filepath.Join(projectDir, sessionID+".jsonl")
	if err := os.WriteFile(mainFilepath, []byte(strings.Join(mainLines, "\n")+"\n"), 0644); err != nil {
		t.Fatalf("write main transcript: %v", err)
	}

	if len(agents) > 0 {
		subagentsDir := filepath.Join(projectDir, sessionID, "subagents")
		if err := os.MkdirAll(subagentsDir, 0755); err != nil {
			t.Fatalf("mkdir subagents: %v", err)
		}
		for agentID, pair := range agents {
			transcript, meta := pair[0], pair[1]
			if err := os.WriteFile(filepath.Join(subagentsDir, "agent-"+agentID+".jsonl"), []byte(transcript+"\n"), 0644); err != nil {
				t.Fatalf("write agent transcript: %v", err)
			}
			if meta == "" {
				continue
			}
			if err := os.WriteFile(filepath.Join(subagentsDir, "agent-"+agentID+".meta.json"), []byte(meta), 0644); err != nil {
				t.Fatalf("write agent meta: %v", err)
			}
		}
	}
	return mainFilepath
}

func fakeUserLine(timestamp string, text string) string {
	encoded, _ := json.Marshal(text)
	return `{"type":"user","timestamp":"` + timestamp + `","message":{"role":"user","content":` + string(encoded) + `}}`
}

// sessionWithOneAgent is the fixture the print tests share: a main transcript
// that spawns one Explore subagent, plus that subagent's transcript.
//
// The subagent's transcript deliberately carries a DIFFERENT number of
// messages, tool calls and tool errors, and a start time an hour after the
// session's. A fixture where those counts coincide makes a right answer and a
// wrong answer render identically, so swapping two columns would pass.
func sessionWithOneAgent(t *testing.T) string {
	t.Helper()
	return writeFakeSession(t,
		[]string{
			fakeUserLine("2026-01-01T00:00:00.000Z", "do the thing"),
			`{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Agent","input":{"description":"scout the repo","subagent_type":"Explore"}}]}}`,
		},
		map[string][2]string{
			"aa8d6202c85e084d7": {
				strings.Join([]string{
					fakeUserLine("2026-01-01T01:00:10.000Z", "SUBAGENT PROMPT TEXT"),
					// 3 tool calls, 2 tool errors, 4 assistant + 3 user messages.
					`{"type":"assistant","uuid":"s1","timestamp":"2026-01-01T01:00:11.000Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}},{"type":"tool_use","name":"Read","input":{"file_path":"/x"}}]}}`,
					`{"type":"user","uuid":"s2","timestamp":"2026-01-01T01:00:12.000Z","message":{"role":"user","content":[{"type":"tool_result","is_error":true,"content":"boom one"}]}}`,
					`{"type":"assistant","uuid":"s3","timestamp":"2026-01-01T01:00:13.000Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Glob","input":{"pattern":"*.go"}}]}}`,
					`{"type":"user","uuid":"s4","timestamp":"2026-01-01T01:00:14.000Z","message":{"role":"user","content":[{"type":"tool_result","is_error":true,"content":"boom two"}]}}`,
					`{"type":"assistant","uuid":"s5","timestamp":"2026-01-01T01:00:15.000Z","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`,
					`{"type":"assistant","uuid":"s6","timestamp":"2026-01-01T01:00:16.000Z","message":{"role":"assistant","content":[{"type":"text","text":"really done"}]}}`,
				}, "\n"),
				`{"agentType":"Explore","description":"scout the repo","toolUseId":"toolu_1","spawnDepth":1,"model":"haiku"}`,
			},
		})
}

func TestTranscriptPrintOptionsValidate(t *testing.T) {
	tests := []struct {
		name    string
		opts    transcriptPrintOptions
		wantErr string
	}{
		{
			name: "text format is valid",
			opts: transcriptPrintOptions{format: textFormat, all: true},
		},
		{
			name: "jsonl format is valid",
			opts: transcriptPrintOptions{format: jsonlFormat, all: true},
		},
		{
			name:    "unknown format",
			opts:    transcriptPrintOptions{format: "yaml", all: true},
			wantErr: "invalid --format",
		},
		{
			name:    "negative tail",
			opts:    transcriptPrintOptions{format: textFormat, tailLines: -1},
			wantErr: "must be positive",
		},
		{
			name:    "agents and agent together",
			opts:    transcriptPrintOptions{format: textFormat, all: true, listAgents: true, agentID: "abc"},
			wantErr: "mutually exclusive",
		},
		{
			name:    "agents and expand-agents together",
			opts:    transcriptPrintOptions{format: textFormat, all: true, listAgents: true, expandAgents: true},
			wantErr: "mutually exclusive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestPrintTranscriptListsTheAgentTree(t *testing.T) {
	mainFilepath := sessionWithOneAgent(t)

	var stdout, stderr bytes.Buffer
	opts := transcriptPrintOptions{format: textFormat, all: true, listAgents: true}
	if err := printTranscriptTo(mainFilepath, opts, &stdout, &stderr); err != nil {
		t.Fatalf("printTranscriptTo: %v", err)
	}
	got := stdout.String()

	for _, want := range []string{"AGENT", "TYPE", mainTranscriptLabel, "aa8d6202c85e084d7", "Explore", "haiku", "scout the repo"} {
		if !strings.Contains(got, want) {
			t.Errorf("agent tree missing %q\n%s", want, got)
		}
	}

	// Assert the numbers by position on the subagent's row. Checking that the
	// digits appear somewhere in the table would pass with two columns swapped.
	row := rowContaining(t, got, "aa8d6202c85e084d7")
	// AGENT TYPE MODEL MSGS TOOLS ERR STARTED(date time) LABEL...
	if len(row) < 8 {
		t.Fatalf("subagent row has %d fields, want at least 8: %q", len(row), row)
	}
	if row[3] != "7" {
		t.Errorf("MSGS = %q, want 7 (3 user + 4 assistant): %q", row[3], row)
	}
	if row[4] != "3" {
		t.Errorf("TOOLS = %q, want 3: %q", row[4], row)
	}
	if row[5] != "2" {
		t.Errorf("ERR = %q, want 2: %q", row[5], row)
	}
	if !strings.HasPrefix(row[6], "2026-01-01") {
		t.Errorf("STARTED = %q, want the subagent's own start date: %q", row[6], row)
	}
}

// rowContaining returns the whitespace-split fields of the single table row
// holding the given token, failing the test when it is absent or ambiguous.
func rowContaining(t *testing.T, table string, token string) []string {
	t.Helper()
	var found []string
	matches := 0
	for _, line := range strings.Split(table, "\n") {
		if strings.Contains(line, token) {
			matches++
			found = strings.Fields(line)
		}
	}
	if matches != 1 {
		t.Fatalf("expected exactly one row containing %q, found %d\n%s", token, matches, table)
	}
	return found
}

func TestPrintAgentTreeIndentsByNestingDepth(t *testing.T) {
	// The AGENT column encodes the tree by indenting once per level. With only
	// depth-1 agents in a fixture, removing the indentation entirely changes
	// nothing, so the tree shape needs a nested agent to be observable.
	mainFilepath := writeFakeSession(t,
		[]string{fakeUserLine("2026-01-01T00:00:00.000Z", "go")},
		map[string][2]string{
			"aparent": {
				fakeUserLine("2026-01-01T00:01:00.000Z", "parent"),
				`{"agentType":"Explore"}`,
			},
			"achild": {
				fakeUserLine("2026-01-01T00:02:00.000Z", "child"),
				`{"agentType":"general-purpose","parentAgentId":"aparent"}`,
			},
		})

	var stdout, stderr bytes.Buffer
	opts := transcriptPrintOptions{format: textFormat, all: true, listAgents: true}
	if err := printTranscriptTo(mainFilepath, opts, &stdout, &stderr); err != nil {
		t.Fatalf("printTranscriptTo: %v", err)
	}

	var parentIndent, childIndent int
	for _, line := range strings.Split(stdout.String(), "\n") {
		switch {
		case strings.Contains(line, "aparent"):
			parentIndent = len(line) - len(strings.TrimLeft(line, " "))
		case strings.Contains(line, "achild"):
			childIndent = len(line) - len(strings.TrimLeft(line, " "))
		}
	}
	if childIndent <= parentIndent {
		t.Errorf("a nested agent must be indented deeper than its parent: parent=%d child=%d\n%s",
			parentIndent, childIndent, stdout.String())
	}
}

func TestPrintAgentTreeFlagsAgentsThatNeedExplaining(t *testing.T) {
	// An agent whose parent transcript is missing, and one whose sidecar was
	// never written, both sit flush with real siblings. Without a marker the
	// tree silently misrepresents where they came from.
	mainFilepath := writeFakeSession(t,
		[]string{fakeUserLine("2026-01-01T00:00:00.000Z", "go")},
		map[string][2]string{
			"aorphan":   {fakeUserLine("2026-01-01T00:01:00.000Z", "x"), `{"agentType":"Explore","parentAgentId":"agone"}`},
			"abaremeta": {fakeUserLine("2026-01-01T00:02:00.000Z", "y"), ""},
			"aforked":   {fakeUserLine("2026-01-01T00:03:00.000Z", "z"), `{"agentType":"general-purpose","isFork":true}`},
		})

	var stdout, stderr bytes.Buffer
	opts := transcriptPrintOptions{format: textFormat, all: true, listAgents: true}
	if err := printTranscriptTo(mainFilepath, opts, &stdout, &stderr); err != nil {
		t.Fatalf("printTranscriptTo: %v", err)
	}
	got := stdout.String()

	for _, want := range []string{"(orphaned)", "(no metadata)", "(fork)"} {
		if !strings.Contains(got, want) {
			t.Errorf("agent tree missing the %s marker\n%s", want, got)
		}
	}
}

func TestPrintTranscriptPassesVerboseThroughToTheRenderer(t *testing.T) {
	// --verbose is registered, documented and tested at the library level, but
	// nothing crossed the CLI boundary — the whole flag could have been dead in
	// the shipped binary with every test still green.
	mainFilepath := writeFakeSession(t,
		[]string{
			fakeUserLine("2026-01-01T00:00:00.000Z", "go"),
			`{"type":"assistant","uuid":"a1","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"WEIGHING THE OPTIONS"},{"type":"text","text":"answer"}]}}`,
		}, nil)

	var quietOut, quietErr bytes.Buffer
	if err := printTranscriptTo(mainFilepath, transcriptPrintOptions{format: textFormat, all: true}, &quietOut, &quietErr); err != nil {
		t.Fatalf("printTranscriptTo: %v", err)
	}
	var verboseOut, verboseErr bytes.Buffer
	if err := printTranscriptTo(mainFilepath, transcriptPrintOptions{format: textFormat, all: true, verbose: true}, &verboseOut, &verboseErr); err != nil {
		t.Fatalf("printTranscriptTo: %v", err)
	}

	if strings.Contains(quietOut.String(), "WEIGHING THE OPTIONS") {
		t.Errorf("thinking must be hidden without --verbose\n%s", quietOut.String())
	}
	if !strings.Contains(verboseOut.String(), "WEIGHING THE OPTIONS") {
		t.Errorf("--verbose must reach the renderer\n%s", verboseOut.String())
	}
}

func TestPrintTranscriptAllOverridesTail(t *testing.T) {
	// Every other test passes all:true with tailLines:0, where the override is
	// a no-op — so deleting it changed nothing.
	mainFilepath := writeFakeSession(t,
		[]string{
			fakeUserLine("2026-01-01T00:00:00.000Z", "THE FIRST TURN"),
			fakeUserLine("2026-01-01T00:00:01.000Z", "the second turn"),
			fakeUserLine("2026-01-01T00:00:02.000Z", "the third turn"),
		}, nil)

	for _, format := range []string{textFormat, jsonlFormat} {
		t.Run(format, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			opts := transcriptPrintOptions{format: format, all: true, tailLines: 1}
			if err := printTranscriptTo(mainFilepath, opts, &stdout, &stderr); err != nil {
				t.Fatalf("printTranscriptTo: %v", err)
			}
			if !strings.Contains(stdout.String(), "THE FIRST TURN") {
				t.Errorf("--all must override --tail, got %q", stdout.String())
			}
		})
	}
}

// The agent tree is command output, not TUI chrome, so it must carry no ANSI
// escapes. It cannot be covered by the e2e ANSI sweep, which has no session
// transcripts to render, so this is the check that holds it.
func TestPrintAgentTreeEmitsNoAnsi(t *testing.T) {
	mainFilepath := sessionWithOneAgent(t)

	var stdout, stderr bytes.Buffer
	opts := transcriptPrintOptions{format: textFormat, all: true, listAgents: true}
	if err := printTranscriptTo(mainFilepath, opts, &stdout, &stderr); err != nil {
		t.Fatalf("printTranscriptTo: %v", err)
	}

	// Positive control: a table that rendered nothing would also contain no
	// escape byte, so assert the rows are actually there first.
	if !strings.Contains(stdout.String(), "aa8d6202c85e084d7") {
		t.Fatalf("agent tree did not render its rows, so the ANSI check would inspect nothing:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), ansiEscapeByte) {
		t.Errorf("agent tree must not emit ANSI escapes, got %q", stdout.String())
	}
}

func TestPrintAgentTreeSaysSoWhenThereAreNoSubagents(t *testing.T) {
	mainFilepath := writeFakeSession(t, []string{fakeUserLine("2026-01-01T00:00:00.000Z", "hi")}, nil)

	var stdout, stderr bytes.Buffer
	opts := transcriptPrintOptions{format: textFormat, all: true, listAgents: true}
	if err := printTranscriptTo(mainFilepath, opts, &stdout, &stderr); err != nil {
		t.Fatalf("printTranscriptTo: %v", err)
	}

	if !strings.Contains(stdout.String(), "No subagent transcripts") {
		t.Errorf("expected an explicit empty-state line, got %q", stdout.String())
	}
}

func TestPrintTranscriptSelectsASubagentByIDPrefix(t *testing.T) {
	mainFilepath := sessionWithOneAgent(t)

	var stdout, stderr bytes.Buffer
	opts := transcriptPrintOptions{format: textFormat, all: true, agentID: "aa8d6202"}
	if err := printTranscriptTo(mainFilepath, opts, &stdout, &stderr); err != nil {
		t.Fatalf("printTranscriptTo: %v", err)
	}

	if !strings.Contains(stdout.String(), "SUBAGENT PROMPT TEXT") {
		t.Errorf("expected the subagent's transcript, got %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "do the thing") {
		t.Errorf("--agent should print the subagent only, not the main transcript")
	}
}

func TestPrintTranscriptReportsAnUnknownAgent(t *testing.T) {
	mainFilepath := sessionWithOneAgent(t)

	var stdout, stderr bytes.Buffer
	opts := transcriptPrintOptions{format: textFormat, all: true, agentID: "nosuchagent"}
	err := printTranscriptTo(mainFilepath, opts, &stdout, &stderr)

	if err == nil {
		t.Fatal("expected an error for an unknown agent ID")
	}
	if !strings.Contains(err.Error(), "nosuchagent") {
		t.Errorf("error should name the requested ID, got %v", err)
	}
}

func TestPrintTranscriptTellsTheCallerAboutUnprintedSubagents(t *testing.T) {
	// The default render omits subagents. Saying nothing would let a --all
	// print look complete while hiding everything the subagents did, which is
	// the blindness these flags exist to remove.
	mainFilepath := sessionWithOneAgent(t)

	var stdout, stderr bytes.Buffer
	opts := transcriptPrintOptions{format: textFormat, all: true}
	if err := printTranscriptTo(mainFilepath, opts, &stdout, &stderr); err != nil {
		t.Fatalf("printTranscriptTo: %v", err)
	}

	// The footer is on STDOUT and carries the exact command: a caller that
	// captured only stdout, and never read --help, still learns the next step.
	opts.listCommand = "agenc session print 11111111 --agents"
	stdout.Reset()
	if err := printTranscriptTo(mainFilepath, opts, &stdout, &stderr); err != nil {
		t.Fatalf("printTranscriptTo: %v", err)
	}
	if !strings.Contains(stdout.String(), "[SUBAGENTS] 1 transcript(s) not shown: 1 spawned directly - --agents to list them, --agent <id> or --workflow <id> to open one - agenc session print 11111111 --agents") {
		t.Errorf("expected the footer on stdout, got %q", stdout.String())
	}
	if strings.Contains(stderr.String(), "not shown") {
		t.Errorf("the hint moved to stdout; stderr must not repeat it, got %q", stderr.String())
	}
	if strings.Contains(stdout.String(), "SUBAGENT PROMPT TEXT") {
		t.Errorf("the default render should not inline subagents")
	}
}

func TestPrintTranscriptDoesNotHintWhenSubagentsAreShown(t *testing.T) {
	mainFilepath := sessionWithOneAgent(t)

	var stdout, stderr bytes.Buffer
	opts := transcriptPrintOptions{format: textFormat, all: true, expandAgents: true}
	if err := printTranscriptTo(mainFilepath, opts, &stdout, &stderr); err != nil {
		t.Fatalf("printTranscriptTo: %v", err)
	}

	if !strings.Contains(stdout.String(), "SUBAGENT PROMPT TEXT") {
		t.Fatalf("--expand-agents should inline the subagent, got %q", stdout.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), "not shown") {
		t.Errorf("no footer should be printed once subagents are inlined, got %q", stdout.String())
	}
}

func TestPrintTranscriptJSONLExpansionConcatenatesEveryTranscript(t *testing.T) {
	mainFilepath := sessionWithOneAgent(t)

	var stdout, stderr bytes.Buffer
	opts := transcriptPrintOptions{format: jsonlFormat, all: true, expandAgents: true}
	if err := printTranscriptTo(mainFilepath, opts, &stdout, &stderr); err != nil {
		t.Fatalf("printTranscriptTo: %v", err)
	}
	got := stdout.String()

	if !strings.Contains(got, "do the thing") || !strings.Contains(got, "SUBAGENT PROMPT TEXT") {
		t.Errorf("expected main and subagent records in the stream, got %q", got)
	}
	for i, line := range strings.Split(strings.TrimSpace(got), "\n") {
		var probe map[string]interface{}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
	}
}

// stubSessionResolver stands in for the server's session-ID resolution.
type stubSessionResolver struct {
	resolved string
	err      error
}

func (s stubSessionResolver) ResolveSessionID(id string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.resolved, nil
}

func TestResolveMissionSessionJSONLWithoutTheFlagTakesTheMostRecent(t *testing.T) {
	projectDir := t.TempDir()
	older := filepath.Join(projectDir, "aaaaaaaa-0000-0000-0000-000000000000.jsonl")
	newer := filepath.Join(projectDir, "bbbbbbbb-0000-0000-0000-000000000000.jsonl")
	for _, path := range []string{older, newer} {
		if err := os.WriteFile(path, []byte(fakeUserLine("2026-01-01T00:00:00.000Z", "hi")+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// FindActiveJSONLPath picks by modification time, so make the ordering explicit.
	touchOlder(t, older)

	got, err := resolveMissionSessionJSONL(stubSessionResolver{}, projectDir, "mission-1", "")
	if err != nil {
		t.Fatalf("resolveMissionSessionJSONL: %v", err)
	}

	if got != newer {
		t.Errorf("got %q, want the most recently modified transcript %q", got, newer)
	}
}

func TestResolveMissionSessionJSONLWithTheFlagPicksThatSession(t *testing.T) {
	projectDir := t.TempDir()
	target := "cccccccc-0000-0000-0000-000000000000"
	if err := os.WriteFile(filepath.Join(projectDir, target+".jsonl"), []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveMissionSessionJSONL(stubSessionResolver{resolved: target}, projectDir, "mission-1", "cccccccc")
	if err != nil {
		t.Fatalf("resolveMissionSessionJSONL: %v", err)
	}

	if filepath.Base(got) != target+".jsonl" {
		t.Errorf("got %q, want the transcript for session %s", got, target)
	}
}

func TestResolveMissionSessionJSONLRejectsASessionFromAnotherMission(t *testing.T) {
	// The server resolves session IDs globally. Printing another mission's
	// transcript under this mission's ID would be a wrong answer that looks
	// like a right one.
	projectDir := t.TempDir()

	_, err := resolveMissionSessionJSONL(stubSessionResolver{resolved: "dddddddd-0000-0000-0000-000000000000"}, projectDir, "mission-1", "dddddddd")

	if err == nil {
		t.Fatal("expected an error when the session has no transcript in this mission")
	}
	if !strings.Contains(err.Error(), "mission-1") {
		t.Errorf("error should name the mission, got %v", err)
	}
}

func TestWarnOnUnprintedSessions(t *testing.T) {
	projectDir := t.TempDir()
	var paths []string
	for _, id := range []string{"aaaaaaaa-0000-0000-0000-000000000000", "bbbbbbbb-0000-0000-0000-000000000000"} {
		path := filepath.Join(projectDir, id+".jsonl")
		if err := os.WriteFile(path, []byte(fakeUserLine("2026-01-01T00:00:00.000Z", "hi")+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	// ListSessionIDs skips transcripts with no conversation records, so the
	// fixture above deliberately writes a real user message into both.
	if got := len(session.ListSessionIDs(projectDir)); got != 2 {
		t.Fatalf("fixture should expose 2 sessions, got %d", got)
	}

	t.Run("warns when other sessions exist", func(t *testing.T) {
		var stderr bytes.Buffer
		warnOnUnprintedSessions(projectDir, paths[1], "", &stderr)
		if !strings.Contains(stderr.String(), "mission has 2 sessions") {
			t.Errorf("expected a multi-session warning, got %q", stderr.String())
		}
	})

	t.Run("stays quiet when the caller chose the session", func(t *testing.T) {
		var stderr bytes.Buffer
		warnOnUnprintedSessions(projectDir, paths[1], "bbbbbbbb", &stderr)
		if stderr.Len() != 0 {
			t.Errorf("expected no warning when --session was given, got %q", stderr.String())
		}
	})

	t.Run("stays quiet for a single-session mission", func(t *testing.T) {
		single := t.TempDir()
		if err := os.WriteFile(filepath.Join(single, "eeeeeeee-0000-0000-0000-000000000000.jsonl"),
			[]byte(fakeUserLine("2026-01-01T00:00:00.000Z", "hi")+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		var stderr bytes.Buffer
		warnOnUnprintedSessions(single, filepath.Join(single, "eeeeeeee-0000-0000-0000-000000000000.jsonl"), "", &stderr)
		if stderr.Len() != 0 {
			t.Errorf("expected no warning for a single-session mission, got %q", stderr.String())
		}
	})
}

func TestFormatTranscriptTimestamp(t *testing.T) {
	if got := formatTranscriptTimestamp(""); got != "-" {
		t.Errorf("empty timestamp = %q, want %q", got, "-")
	}
	if got := formatTranscriptTimestamp("not a timestamp"); got != "not a timestamp" {
		t.Errorf("unparseable timestamp should pass through, got %q", got)
	}
	if got := formatTranscriptTimestamp("2026-01-01T00:00:00.000Z"); !strings.HasPrefix(got, "202") {
		t.Errorf("parsed timestamp = %q, want a formatted date", got)
	}
}

// touchOlder rewinds a file's modification time so "most recent" is unambiguous.
func touchOlder(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	older := info.ModTime().Add(-time.Hour)
	if err := os.Chtimes(path, older, older); err != nil {
		t.Fatal(fmt.Errorf("chtimes: %w", err))
	}
}
