package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSession builds a Claude project directory on disk: a main transcript at
// <dir>/<sessionID>.jsonl plus subagent transcripts under
// <dir>/<sessionID>/subagents/. It mirrors the real layout so the discovery
// tests exercise the same path resolution the CLI uses.
type fakeSession struct {
	t          *testing.T
	projectDir string
	sessionID  string
}

func newFakeSession(t *testing.T, mainLines ...string) *fakeSession {
	t.Helper()
	fs := &fakeSession{t: t, projectDir: t.TempDir(), sessionID: "11111111-2222-3333-4444-555555555555"}
	fs.writeFile(filepath.Join(fs.projectDir, fs.sessionID+".jsonl"), strings.Join(mainLines, "\n"))
	return fs
}

// addAgent writes a subagent transcript and, when meta is non-nil, its sidecar.
func (fs *fakeSession) addAgent(agentID string, meta *AgentMeta, lines ...string) {
	fs.t.Helper()
	dir := filepath.Join(fs.projectDir, fs.sessionID, "subagents")
	if err := os.MkdirAll(dir, 0755); err != nil {
		fs.t.Fatalf("mkdir subagents: %v", err)
	}
	fs.writeFile(filepath.Join(dir, "agent-"+agentID+".jsonl"), strings.Join(lines, "\n"))
	if meta == nil {
		return
	}
	encoded, err := json.Marshal(meta)
	if err != nil {
		fs.t.Fatalf("marshal meta: %v", err)
	}
	fs.writeFile(filepath.Join(dir, "agent-"+agentID+".meta.json"), string(encoded))
}

func (fs *fakeSession) writeFile(path string, content string) {
	fs.t.Helper()
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		fs.t.Fatalf("write %s: %v", path, err)
	}
}

func (fs *fakeSession) discover() *Transcript {
	fs.t.Helper()
	root, err := DiscoverSessionTranscripts(fs.projectDir, fs.sessionID)
	if err != nil {
		fs.t.Fatalf("DiscoverSessionTranscripts: %v", err)
	}
	return root
}

// userLine and assistantLine build minimal conversation records.
func userLine(timestamp string, text string) string {
	return `{"type":"user","timestamp":"` + timestamp + `","message":{"role":"user","content":` + quote(text) + `}}`
}

func assistantLine(timestamp string, text string) string {
	return `{"type":"assistant","timestamp":"` + timestamp + `","message":{"role":"assistant","content":[{"type":"text","text":` + quote(text) + `}]}}`
}

func quote(s string) string {
	encoded, _ := json.Marshal(s)
	return string(encoded)
}

func agentIDsOf(nodes []*Transcript) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.AgentID)
	}
	return out
}

func TestDiscoverSessionTranscriptsMainOnly(t *testing.T) {
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "hi"))

	root := fs.discover()

	if !root.IsMain() {
		t.Errorf("root should be the main transcript, got agent ID %q", root.AgentID)
	}
	if got := len(root.Flatten()); got != 1 {
		t.Errorf("expected 1 transcript, got %d", got)
	}
	if root.StartedAt != "2026-01-01T00:00:00.000Z" {
		t.Errorf("StartedAt = %q, want the first record's timestamp", root.StartedAt)
	}
}

func TestDiscoverSessionTranscriptsMissingMainIsAnError(t *testing.T) {
	dir := t.TempDir()

	_, err := DiscoverSessionTranscripts(dir, "nope")

	if err == nil {
		t.Fatal("expected an error for a session with no main transcript")
	}
}

func TestDiscoverSessionTranscriptsBuildsNestedTree(t *testing.T) {
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addAgent("aparent", &AgentMeta{AgentType: "Explore", ToolUseID: "toolu_1"},
		userLine("2026-01-01T00:01:00.000Z", "parent agent prompt"))
	fs.addAgent("achild", &AgentMeta{AgentType: "general-purpose", ParentAgentID: "aparent"},
		userLine("2026-01-01T00:02:00.000Z", "child agent prompt"))
	fs.addAgent("agrandchild", &AgentMeta{AgentType: "general-purpose", ParentAgentID: "achild"},
		userLine("2026-01-01T00:03:00.000Z", "grandchild prompt"))

	root := fs.discover()

	if got := agentIDsOf(root.Flatten()); len(got) != 4 {
		t.Fatalf("expected 4 transcripts, got %v", got)
	}
	if got := len(root.Children); got != 1 {
		t.Fatalf("expected 1 direct child of the session, got %d", got)
	}
	parent := root.Children[0]
	if parent.AgentID != "aparent" || parent.Depth != 1 {
		t.Errorf("parent = %s at depth %d, want aparent at depth 1", parent.AgentID, parent.Depth)
	}
	if len(parent.Children) != 1 || parent.Children[0].AgentID != "achild" {
		t.Fatalf("expected achild under aparent, got %v", agentIDsOf(parent.Children))
	}
	grandchild := parent.Children[0].Children
	if len(grandchild) != 1 || grandchild[0].Depth != 3 {
		t.Fatalf("expected agrandchild at depth 3, got %v", agentIDsOf(grandchild))
	}
}

func TestDiscoverSessionTranscriptsKeepsAgentsWithNoMetadata(t *testing.T) {
	// 3.6% of the subagent transcripts on a long-lived machine have no sidecar.
	// Dropping them would make the tool silently under-report what a session did.
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addAgent("abare", nil, userLine("2026-01-01T00:01:00.000Z", "no sidecar"))

	root := fs.discover()

	if len(root.Children) != 1 {
		t.Fatalf("expected the sidecar-less agent to be attached to the root, got %v", agentIDsOf(root.Children))
	}
	node := root.Children[0]
	if node.MetaFound {
		t.Error("MetaFound should be false when no sidecar exists")
	}
	if got := node.Meta.DisplayType(); got != "unknown" {
		t.Errorf("DisplayType() = %q, want %q", got, "unknown")
	}
}

func TestDiscoverSessionTranscriptsKeepsAgentsWithUnreadableMetadata(t *testing.T) {
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addAgent("abroken", nil, userLine("2026-01-01T00:01:00.000Z", "x"))
	fs.writeFile(filepath.Join(fs.projectDir, fs.sessionID, "subagents", "agent-abroken.meta.json"), "{not json")

	root := fs.discover()

	if len(root.Children) != 1 || root.Children[0].MetaFound {
		t.Fatalf("a malformed sidecar should degrade to no-metadata, got %v", agentIDsOf(root.Children))
	}
}

func TestDiscoverSessionTranscriptsMarksOrphans(t *testing.T) {
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addAgent("alonely", &AgentMeta{AgentType: "Explore", ParentAgentID: "agone"},
		userLine("2026-01-01T00:01:00.000Z", "x"))

	root := fs.discover()

	if len(root.Children) != 1 {
		t.Fatalf("an orphan must stay reachable from the root, got %v", agentIDsOf(root.Children))
	}
	if !root.Children[0].Orphaned {
		t.Error("expected Orphaned to be set when the named parent has no transcript")
	}
}

func TestDiscoverSessionTranscriptsBreaksParentCycles(t *testing.T) {
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addAgent("aone", &AgentMeta{ParentAgentID: "atwo"}, userLine("2026-01-01T00:01:00.000Z", "one"))
	fs.addAgent("atwo", &AgentMeta{ParentAgentID: "aone"}, userLine("2026-01-01T00:02:00.000Z", "two"))
	fs.addAgent("aself", &AgentMeta{ParentAgentID: "aself"}, userLine("2026-01-01T00:03:00.000Z", "self"))

	root := fs.discover()

	// The assertion that matters is termination plus reachability: every
	// transcript on disk appears exactly once, and Flatten() returns.
	got := agentIDsOf(root.Flatten())
	if len(got) != 4 {
		t.Fatalf("expected the session plus 3 agents exactly once each, got %v", got)
	}
}

func TestDiscoverSessionTranscriptsOrdersSiblingsByStartTime(t *testing.T) {
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addAgent("azzz", &AgentMeta{}, userLine("2026-01-01T00:01:00.000Z", "first"))
	fs.addAgent("aaaa", &AgentMeta{}, userLine("2026-01-01T00:09:00.000Z", "last"))

	root := fs.discover()

	if got := agentIDsOf(root.Children); got[0] != "azzz" || got[1] != "aaaa" {
		t.Errorf("children = %v, want chronological order [azzz aaaa]", got)
	}
}

func TestResolveAgent(t *testing.T) {
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addAgent("aa11bb", &AgentMeta{}, userLine("2026-01-01T00:01:00.000Z", "x"))
	fs.addAgent("aa22cc", &AgentMeta{}, userLine("2026-01-01T00:02:00.000Z", "y"))
	fs.addAgent("aa", &AgentMeta{}, userLine("2026-01-01T00:03:00.000Z", "z"))
	root := fs.discover()

	t.Run("exact match wins over prefix matches", func(t *testing.T) {
		// "aa" is also a prefix of aa11bb and aa22cc; the exact ID must win
		// rather than being reported as ambiguous.
		got, err := ResolveAgent(root, "aa")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.AgentID != "aa" {
			t.Errorf("got %q, want the exact match %q", got.AgentID, "aa")
		}
	})

	t.Run("unique prefix", func(t *testing.T) {
		got, err := ResolveAgent(root, "aa11")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.AgentID != "aa11bb" {
			t.Errorf("got %q, want aa11bb", got.AgentID)
		}
	})

	t.Run("no match", func(t *testing.T) {
		_, err := ResolveAgent(root, "zzzz")
		if !errors.Is(err, ErrAgentNotFound) {
			t.Errorf("error = %v, want ErrAgentNotFound", err)
		}
	})

	t.Run("empty prefix", func(t *testing.T) {
		if _, err := ResolveAgent(root, ""); !errors.Is(err, ErrAgentNotFound) {
			t.Errorf("error = %v, want ErrAgentNotFound", err)
		}
	})
}

func TestResolveAgentAmbiguousPrefixListsCandidates(t *testing.T) {
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addAgent("abc111", &AgentMeta{}, userLine("2026-01-01T00:01:00.000Z", "x"))
	fs.addAgent("abc222", &AgentMeta{}, userLine("2026-01-01T00:02:00.000Z", "y"))
	root := fs.discover()

	_, err := ResolveAgent(root, "abc")

	if err == nil {
		t.Fatal("expected an ambiguity error")
	}
	for _, want := range []string{"abc111", "abc222"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should name candidate %s", err.Error(), want)
		}
	}
}

func TestSummarizeTranscript(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	content := strings.Join([]string{
		userLine("2026-01-01T00:00:00.000Z", "hello"),
		`{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"user","timestamp":"2026-01-01T00:00:02.000Z","message":{"role":"user","content":[{"type":"tool_result","is_error":true,"content":"boom"}]}}`,
		`{"type":"system","subtype":"compact_boundary","timestamp":"2026-01-01T00:00:03.000Z"}`,
		`{"type":"file-history-snapshot","timestamp":"2026-01-01T00:00:04.000Z"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	stats, err := SummarizeTranscript(path)
	if err != nil {
		t.Fatalf("SummarizeTranscript: %v", err)
	}

	if stats.UserMessages != 2 || stats.AssistantMessages != 1 {
		t.Errorf("messages: user=%d assistant=%d, want 2 and 1", stats.UserMessages, stats.AssistantMessages)
	}
	if stats.ToolCalls != 1 || stats.ToolErrors != 1 || stats.CompactBoundaries != 1 {
		t.Errorf("tools=%d errors=%d compactions=%d, want 1/1/1", stats.ToolCalls, stats.ToolErrors, stats.CompactBoundaries)
	}
	if stats.FirstTimestamp != "2026-01-01T00:00:00.000Z" || stats.LastTimestamp != "2026-01-01T00:00:04.000Z" {
		t.Errorf("timestamps = %s..%s, want the file's first and last", stats.FirstTimestamp, stats.LastTimestamp)
	}
}

func TestDiscoverTranscriptsForFile(t *testing.T) {
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addAgent("aone", &AgentMeta{}, userLine("2026-01-01T00:01:00.000Z", "x"))

	root, err := DiscoverTranscriptsForFile(filepath.Join(fs.projectDir, fs.sessionID+".jsonl"))
	if err != nil {
		t.Fatalf("DiscoverTranscriptsForFile: %v", err)
	}

	if len(root.Flatten()) != 2 {
		t.Errorf("expected the sidecar directory to be found from the file path, got %v", agentIDsOf(root.Flatten()))
	}
}

// --- Long-tail discovery invariants -----------------------------------------

func TestReadStartTimestampScansPastLeadingUntimestampedRecords(t *testing.T) {
	// The leading records of a real transcript are frequently untimestamped
	// bookkeeping. A scan that gives up on the first one leaves the transcript
	// with no start time, and every ordering built on StartedAt collapses.
	fs := newFakeSession(t,
		`{"type":"file-history-snapshot"}`,
		`{"type":"mode","mode":"plan"}`,
		`{"type":"permission-mode","permissionMode":"acceptEdits"}`,
		`{"type":"summary","summary":"resumed"}`,
		`{"type":"file-history-snapshot"}`,
		`{"type":"agent-name","agentName":"lead"}`,
		`{"type":"last-prompt","lastPrompt":"go"}`,
		userLine("2026-01-01T00:00:07.000Z", "the first timestamped record"),
	)

	root := fs.discover()

	if root.StartedAt != "2026-01-01T00:00:07.000Z" {
		t.Errorf("StartedAt = %q, want the 8th record's timestamp — the scan must run to the limit, not stop at the first untimestamped record", root.StartedAt)
	}
}

func TestAgentMetaDisplayLabelPrefersNameOverDescription(t *testing.T) {
	// A named agent and the description its spawner wrote are different facts.
	// Showing the description for a named agent breaks the only handle a reader
	// has for matching a transcript to the SendMessage target it answers to.
	tests := []struct {
		name string
		meta AgentMeta
		want string
	}{
		{"name wins over description", AgentMeta{Name: "reviewer-tests", Description: "close long-tail test gaps"}, "reviewer-tests"},
		{"description when unnamed", AgentMeta{Description: "close long-tail test gaps"}, "close long-tail test gaps"},
		{"empty when neither is set", AgentMeta{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.meta.DisplayLabel(); got != tt.want {
				t.Errorf("DisplayLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDiscoverSessionTranscriptsBreaksStartTimeTiesByAgentID(t *testing.T) {
	// Both agents start at the same instant, and the directory hands them over
	// in the opposite order — '-' sorts before '.', so agent-aa-1.jsonl is read
	// before agent-aa.jsonl. Only the agent-ID tiebreak makes the tree
	// deterministic rather than an artefact of the filesystem.
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addAgent("aa", &AgentMeta{}, userLine("2026-01-01T00:01:00.000Z", "first"))
	fs.addAgent("aa-1", &AgentMeta{}, userLine("2026-01-01T00:01:00.000Z", "second"))

	root := fs.discover()

	got := agentIDsOf(root.Children)
	if len(got) != 2 {
		t.Fatalf("expected both agents attached to the root, got %v", got)
	}
	if got[0] != "aa" || got[1] != "aa-1" {
		t.Errorf("children = %v, want [aa aa-1] — identical start times must fall back to agent ID order", got)
	}
}

func TestWalkVisitsEachNodeOnceOnACyclicGraph(t *testing.T) {
	// linkTranscriptTree guarantees an acyclic tree, so discovery never reaches
	// Walk's visited guard. The guard is a documented property of Walk itself —
	// every caller that hand-builds or mutates a tree relies on it — so it is
	// pinned here on a graph built directly rather than discovered.
	root := &Transcript{}
	a := &Transcript{AgentID: "acycle-a"}
	b := &Transcript{AgentID: "acycle-b"}
	root.Children = []*Transcript{a}
	a.Children = []*Transcript{b}
	b.Children = []*Transcript{a, root}

	visits := map[string]int{}
	root.Walk(func(n *Transcript) { visits[n.AgentID]++ })

	if len(visits) != 3 {
		t.Fatalf("expected the root plus both agents visited, got %v", visits)
	}
	for id, n := range visits {
		if n != 1 {
			t.Errorf("node %q visited %d times, want exactly 1", id, n)
		}
	}
}
