package session

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// workflowSpawn writes the two records a Workflow call leaves in a transcript:
// the assistant's tool_use and the user-side tool_result naming the task ID.
func workflowSpawn(timestamp string, toolUseID string, taskID string) []string {
	return []string{
		`{"type":"assistant","timestamp":"` + timestamp + `","uuid":"wu-` + toolUseID + `","message":{"role":"assistant","content":[{"type":"tool_use","id":"` + toolUseID + `","name":"Workflow","input":{"scriptPath":"/x/analyze.js"}}]}}`,
		`{"type":"user","timestamp":"` + timestamp + `","uuid":"wr-` + toolUseID + `","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"` + toolUseID + `","content":"Workflow launched in background. Task ID: ` + taskID + `\nSummary: one agent per thread"}]}}`,
	}
}

func sessionWithOneWorkflowRun(t *testing.T, extraMain ...string) *fakeSession {
	t.Helper()
	lines := append([]string{userLine("2026-01-01T00:00:00.000Z", "go")}, workflowSpawn("2026-01-01T00:01:00.000Z", "toolu_wf1", "w9gxz79on")...)
	lines = append(lines, extraMain...)
	fs := newFakeSession(t, lines...)
	fs.addWorkflowAgent("wf_3a23189e-9d5", "aw1", assistantLine("2026-01-01T00:02:00.000Z", "WORKFLOW AGENT ONE BODY"))
	fs.addWorkflowAgent("wf_3a23189e-9d5", "aw2", assistantLine("2026-01-01T00:03:00.000Z", "WORKFLOW AGENT TWO BODY"))
	fs.addWorkflowJournal("wf_3a23189e-9d5", []string{"aw1", "aw2"}, 1)
	fs.addWorkflowManifest("wf_3a23189e-9d5", `{"runId":"wf_3a23189e-9d5","timestamp":"2026-01-01T00:01:00.500Z","taskId":"w9gxz79on","workflowName":"analyze","summary":"one agent per thread","status":"completed","durationMs":"1487202"}`)
	return fs
}

func renderRoot(t *testing.T, root *Transcript, opts FormatOptions) string {
	t.Helper()
	opts.Root = root
	var buf bytes.Buffer
	if err := FormatTranscript(root, opts, &buf); err != nil {
		t.Fatalf("FormatTranscript: %v", err)
	}
	return buf.String()
}

func TestFormatAnchorsAWorkflowSpawnToItsRun(t *testing.T) {
	// A Workflow call used to render as a bare "> Workflow()" — 62 times in one
	// real session — with no run ID, no count and no way to open it.
	root := sessionWithOneWorkflowRun(t).discover()
	got := renderRoot(t, root, FormatOptions{})

	mustContain(t, got, `> Workflow()  [workflow wf_3a23189e-9d5 (analyze): 2 agents, completed, `)
	mustNotContain(t, got, "WORKFLOW AGENT ONE BODY")
}

func TestFormatFooterCountsEverythingNotShown(t *testing.T) {
	fs := sessionWithOneWorkflowRun(t)
	fs.addAgent("aloose", &AgentMeta{AgentType: "Explore"}, assistantLine("2026-01-01T00:05:00.000Z", "loose body"))
	root := fs.discover()

	got := renderRoot(t, root, FormatOptions{ListCommand: "agenc session print 11111111 --agents"})

	footer := "[SUBAGENTS] 3 transcript(s) not shown: 1 spawned directly, 2 in 1 workflow run(s) - --agents to list them, --agent <id> or --workflow <id> to open one - agenc session print 11111111 --agents"
	mustContain(t, got, footer)
	if !strings.HasSuffix(strings.TrimRight(got, "\n"), footer) {
		t.Errorf("the footer must be the last line of the render")
	}
}

func TestFormatFooterIsSilentWhenNothingIsHidden(t *testing.T) {
	root := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go")).discover()
	mustNotContain(t, renderRoot(t, root, FormatOptions{}), "[SUBAGENTS]", "[WORKFLOWS]")
}

func TestFormatFooterIsScopedToTheMainTranscript(t *testing.T) {
	fs := sessionWithOneWorkflowRun(t)
	fs.addAgent("aloose", &AgentMeta{AgentType: "Explore"}, assistantLine("2026-01-01T00:05:00.000Z", "loose body"))
	root := fs.discover()
	agent, err := ResolveAgent(root, "aloose")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := FormatTranscript(agent, FormatOptions{Root: root}, &buf); err != nil {
		t.Fatal(err)
	}
	mustNotContain(t, buf.String(), "[SUBAGENTS]")
}

func TestFormatExpansionNeverInlinesWorkflowRuns(t *testing.T) {
	root := sessionWithOneWorkflowRun(t).discover()
	got := renderRoot(t, root, FormatOptions{ExpandAgents: true})

	mustNotContain(t, got, "WORKFLOW AGENT ONE BODY", "begin agent aw1", "[UNLINKED WORKFLOWS]", "[UNLINKED AGENTS]")
	mustContain(t, got, "[WORKFLOWS] 2 agent transcript(s) in 1 workflow run(s) are not inlined - --workflow <id> to list a run")
}

func TestFormatListsWorkflowRunsWhoseSpawnIsOutsideTheTail(t *testing.T) {
	// Twenty records after the spawn push it out of a --tail 5 window. Under
	// expansion the run must still be accounted for, by name, once — not once
	// per agent.
	var filler []string
	for i := 0; i < 20; i++ {
		filler = append(filler, userLine("2026-01-01T00:10:00.000Z", "later"))
	}
	root := sessionWithOneWorkflowRun(t, filler...).discover()
	got := renderRoot(t, root, FormatOptions{ExpandAgents: true, TailLines: 5})

	mustNotContain(t, got, "[workflow wf_3a23189e-9d5")
	mustContain(t, got, "[UNLINKED WORKFLOWS] 1 workflow run(s) with no spawn site in the printed range", "  wf_3a23189e-9d5 (analyze): 2 agent(s), completed")
	if n := strings.Count(got, "wf_3a23189e-9d5"); n != 1 {
		t.Errorf("run named %d times, want exactly once (the trailer line), not once per agent; got:\n%s", n, got)
	}
}

func TestScanFindsTaskIDsOutsideTheTailWindow(t *testing.T) {
	// The link between a spawn and its run lives in the tool_result record, so
	// it must be collected over the whole file like fork provenance is.
	var filler []string
	for i := 0; i < 20; i++ {
		filler = append(filler, userLine("2026-01-01T00:10:00.000Z", "later"))
	}
	root := sessionWithOneWorkflowRun(t, filler...).discover()
	_, _, taskIDs, err := scanTranscriptFile(root.Filepath, 3)
	if err != nil {
		t.Fatal(err)
	}
	if taskIDs["toolu_wf1"] != "w9gxz79on" {
		t.Errorf("taskIDs = %v", taskIDs)
	}
}

func TestFormatWorkflowSpawnWithNoRunOnDiskStaysBare(t *testing.T) {
	// A spawn whose run directory and manifest were both deleted has nothing to
	// anchor to; rendering must not invent one.
	fs := newFakeSession(t, append([]string{userLine("2026-01-01T00:00:00.000Z", "go")}, workflowSpawn("2026-01-01T00:01:00.000Z", "toolu_gone", "wgone")...)...)
	got := renderRoot(t, fs.discover(), FormatOptions{})
	mustContain(t, got, "> Workflow()")
	mustNotContain(t, got, "[workflow", "[SUBAGENTS]")
}

func TestFormatExpansionBudgetIsChargedPerAgentAsItIsInlined(t *testing.T) {
	// Two agents of ~2 KB each under a 3 KB budget: the first is inlined, the
	// second is replaced by a note naming its size, the budget and the flags.
	// Charging at the inline site, not up front, is what lets a tailed render
	// expand the agents in its window without being refused for the rest.
	body := strings.Repeat("y", 2000)
	fs := newFakeSession(t,
		userLine("2026-01-01T00:00:00.000Z", "go"),
		spawnLine("2026-01-01T00:01:00.000Z", "toolu_1", "first"),
		spawnLine("2026-01-01T00:02:00.000Z", "toolu_2", "second"),
	)
	fs.addAgent("afirst", &AgentMeta{AgentType: "Explore", ToolUseID: "toolu_1"}, assistantLine("2026-01-01T00:01:01.000Z", "FIRST "+body))
	fs.addAgent("asecond", &AgentMeta{AgentType: "Explore", ToolUseID: "toolu_2"}, assistantLine("2026-01-01T00:02:01.000Z", "SECOND "+body))
	root := fs.discover()

	got := renderRoot(t, root, FormatOptions{ExpandAgents: true, MaxExpandBytes: 3 * 1024})

	mustContain(t, got, "--- begin agent afirst", "[agent asecond not expanded: ", " would exceed the 3.0 KB expansion budget; raise --max-expand-mb, or print it alone with --agent asecond]")
	mustNotContain(t, got, "SECOND yyy", "[UNLINKED AGENTS]")

	unlimited := renderRoot(t, root, FormatOptions{ExpandAgents: true})
	mustContain(t, unlimited, "--- begin agent afirst", "--- begin agent asecond")
}

func TestFormatTimeWindowKeepsOnlyRecordsInsideIt(t *testing.T) {
	fs := newFakeSession(t,
		`{"type":"user","uuid":"f1","timestamp":"2026-01-01T00:00:00.000Z","forkedFrom":{"sessionId":"src-session","messageUuid":"x"},"message":{"role":"user","content":"BEFORE"}}`,
		userLine("2026-01-01T10:00:00.000Z", "INSIDE ONE"),
		`{"type":"queue-operation","operation":"enqueue","content":"NO TIMESTAMP"}`,
		userLine("2026-01-01T11:00:00.000Z", "INSIDE TWO"),
		userLine("2026-01-01T12:00:00.000Z", "AFTER"),
	)
	fs.addAgent("aloose", &AgentMeta{AgentType: "Explore"}, assistantLine("2026-01-01T10:30:00.000Z", "agent body"))
	root := fs.discover()

	got := renderRoot(t, root, FormatOptions{
		Since: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		Until: time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC),
	})

	mustContain(t, got, "INSIDE ONE", "INSIDE TWO", "[FORK] this session was forked from session src-session", "[SUBAGENTS] 1 transcript(s) not shown")
	mustNotContain(t, got, "BEFORE", "AFTER", "NO TIMESTAMP")

	// Positive control: with no window, everything renders.
	all := renderRoot(t, root, FormatOptions{})
	mustContain(t, all, "BEFORE", "AFTER", "NO TIMESTAMP")
}

func TestFormatBudgetSkipAccountsForWhatTheSkippedAgentSpawned(t *testing.T) {
	// A loose agent whose transcript spawns a workflow run and a nested agent.
	// Skipped for budget, the run and the nested agent must be attributed to
	// the skip — not reported as "no spawn site in the printed range", which
	// would be false: the site exists, it was not rendered.
	fs := newFakeSession(t,
		userLine("2026-01-01T00:00:00.000Z", "go"),
		spawnLine("2026-01-01T00:01:00.000Z", "toolu_outer", "outer"),
	)
	outerLines := append([]string{userLine("2026-01-01T00:01:01.000Z", strings.Repeat("o", 3000))},
		workflowSpawn("2026-01-01T00:01:02.000Z", "toolu_inner_wf", "wnested1")...)
	outerLines = append(outerLines, spawnLine("2026-01-01T00:01:03.000Z", "toolu_inner_agent", "inner"))
	fs.addAgent("aouter", &AgentMeta{AgentType: "Explore", ToolUseID: "toolu_outer"}, outerLines...)
	fs.addAgent("ainner", &AgentMeta{AgentType: "Explore", ToolUseID: "toolu_inner_agent", ParentAgentID: "aouter"}, assistantLine("2026-01-01T00:01:04.000Z", "inner body"))
	fs.addWorkflowAgent("wf_nest0001-aaa", "awn", assistantLine("2026-01-01T00:01:05.000Z", "wf body"))
	fs.addWorkflowManifest("wf_nest0001-aaa", `{"taskId":"wnested1","workflowName":"nested","status":"completed","startTime":1767225665000}`)
	root := fs.discover()

	got := renderRoot(t, root, FormatOptions{ExpandAgents: true, MaxExpandBytes: 1024})

	mustContain(t, got, "[agent aouter not expanded: ", "; 1 nested agent(s) and 1 workflow run(s) spawned inside it are not shown either]")
	mustNotContain(t, got, "[UNLINKED WORKFLOWS]", "[UNLINKED AGENTS]", "inner body")

	// Positive control: with the budget off, the run is anchored inside the
	// inlined block and nothing is reported unlinked either.
	all := renderRoot(t, root, FormatOptions{ExpandAgents: true})
	mustContain(t, all, "[workflow wf_nest0001-aaa (nested): 1 agents, completed]", "--- begin agent ainner")
	mustNotContain(t, all, "[UNLINKED WORKFLOWS]", "[UNLINKED AGENTS]")
}

func TestFormatBudgetNoteRendersOncePerAgent(t *testing.T) {
	// Real files carry the same tool_use block in several records with
	// distinct uuids; the note must not repeat with them.
	fs := newFakeSession(t,
		userLine("2026-01-01T00:00:00.000Z", "go"),
		spawnLine("2026-01-01T00:01:00.000Z", "toolu_big", "big"),
		strings.Replace(spawnLine("2026-01-01T00:01:00.000Z", "toolu_big", "big"), `"uuid":"`, `"uuid":"dup-`, 1),
	)
	fs.addAgent("abig", &AgentMeta{AgentType: "Explore", ToolUseID: "toolu_big"}, assistantLine("2026-01-01T00:01:01.000Z", strings.Repeat("b", 3000)))
	root := fs.discover()

	got := renderRoot(t, root, FormatOptions{ExpandAgents: true, MaxExpandBytes: 1024})
	if n := strings.Count(got, "[agent abig not expanded"); n != 1 {
		t.Errorf("budget note rendered %d times for one agent, want 1\n%s", n, got)
	}
	if n := strings.Count(renderRoot(t, root, FormatOptions{ExpandAgents: true}), "--- begin agent abig"); n != 1 {
		t.Errorf("control: agent inlined %d times, want 1", n)
	}
}
