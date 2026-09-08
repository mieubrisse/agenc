package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// addFakeWorkflowRun writes a workflow run next to a fake session: its agent
// transcripts under subagents/workflows/<runID>/, a journal in which the first
// `finished` agents reported, and its manifest under workflows/<runID>.json.
func addFakeWorkflowRun(t *testing.T, mainFilepath string, runID string, manifestJSON string, agents map[string]string, finished int) {
	t.Helper()
	sessionDir := strings.TrimSuffix(mainFilepath, ".jsonl")
	runDir := filepath.Join(sessionDir, "subagents", "workflows", runID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}
	var journal []string
	var ids []string
	for id := range agents {
		ids = append(ids, id)
	}
	for _, id := range ids {
		if err := os.WriteFile(filepath.Join(runDir, "agent-"+id+".jsonl"), []byte(agents[id]+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(runDir, "agent-"+id+".meta.json"), []byte(`{"agentType":"workflow-subagent","spawnDepth":1}`), 0644); err != nil {
			t.Fatal(err)
		}
		journal = append(journal, `{"type":"started","key":"k","agentId":"`+id+`"}`)
	}
	for _, id := range ids[:finished] {
		journal = append(journal, `{"type":"result","key":"k","agentId":"`+id+`","result":{"ok":true}}`)
	}
	if err := os.WriteFile(filepath.Join(runDir, "journal.jsonl"), []byte(strings.Join(journal, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if manifestJSON != "" {
		manifestDir := filepath.Join(sessionDir, "workflows")
		if err := os.MkdirAll(manifestDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(manifestDir, runID+".json"), []byte(manifestJSON), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// sessionWithAgentAndWorkflow is the one-agent fixture plus a two-agent
// workflow run whose spawn the main transcript records.
func sessionWithAgentAndWorkflow(t *testing.T) string {
	t.Helper()
	mainFilepath := sessionWithOneAgent(t)
	f, err := os.OpenFile(mainFilepath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	spawn := `{"type":"assistant","timestamp":"2026-01-01T00:00:02.000Z","uuid":"wfu","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_wf","name":"Workflow","input":{"scriptPath":"/x/analyze.js"}}]}}` + "\n" +
		`{"type":"user","timestamp":"2026-01-01T00:00:03.000Z","uuid":"wfr","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_wf","content":"Workflow launched in background. Task ID: w9gxz79on\nSummary: analyze threads"}]}}` + "\n"
	if _, err := f.WriteString(spawn); err != nil {
		t.Fatal(err)
	}
	f.Close()
	addFakeWorkflowRun(t, mainFilepath, "wf_3a23189e-9d5",
		`{"runId":"wf_3a23189e-9d5","timestamp":"2026-01-01T00:00:03.500Z","taskId":"w9gxz79on","workflowName":"analyze","summary":"one agent per thread","status":"completed","durationMs":"1487202","totalTokens":1741419,"totalToolCalls":"194","phases":[{"title":"Review"},{"title":"Verify"}],"result":{"dispatched":2}}`,
		map[string]string{
			"aw1": strings.Join([]string{
				fakeUserLine("2026-01-01T00:01:00.000Z", "WF AGENT ONE PROMPT"),
				`{"type":"assistant","uuid":"w1a","timestamp":"2026-01-01T00:01:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}`,
			}, "\n"),
			"aw2": fakeUserLine("2026-01-01T00:02:00.000Z", "WF AGENT TWO PROMPT"),
		}, 1)
	return mainFilepath
}

func runPrint(t *testing.T, mainFilepath string, opts transcriptPrintOptions) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if opts.format == "" {
		opts.format = textFormat
	}
	err := printTranscriptTo(mainFilepath, opts, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

func TestOverviewListsWorkflowRunsAsOneRowEach(t *testing.T) {
	out, _, err := runPrint(t, sessionWithAgentAndWorkflow(t), transcriptPrintOptions{listAgents: true, all: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"session 11111111-2222-3333-4444-555555555555: 4 messages, 2 tool calls, 0 errors, 0 compactions, ",
		"subagents: 1 spawned directly (", "; 2 in 1 workflow run(s) (",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("header missing %q in:\n%s", want, out)
		}
	}
	row := rowContaining(t, out, "wf_3a23189e-9d5")
	if row[1] != "workflow" || row[3] != "2" || row[4] != "agents" || row[5] != "194" {
		t.Errorf("run row = %v", row)
	}
	if !strings.Contains(out, "analyze: completed, 24m47s - one agent per thread") {
		t.Errorf("run label missing, got:\n%s", out)
	}
	if strings.Contains(out, "WF AGENT ONE PROMPT") || strings.Contains(out, "aw1") {
		t.Errorf("the overview must not list a run's agents individually:\n%s", out)
	}
	agentRow := rowContaining(t, out, "aa8d6202c85e084d7")
	if !strings.Contains(strings.Join(agentRow, " "), " KB ") && !strings.Contains(strings.Join(agentRow, " "), " B ") {
		t.Errorf("agent row must carry a SIZE cell: %v", agentRow)
	}
}

func TestOverviewJSONIsCompleteAndParseable(t *testing.T) {
	out, _, err := runPrint(t, sessionWithAgentAndWorkflow(t), transcriptPrintOptions{listAgents: true, jsonOutput: true, all: true})
	if err != nil {
		t.Fatal(err)
	}
	var doc overviewJSON
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if doc.Session.Messages != 4 || doc.Session.ToolCalls != 2 || doc.Session.Bytes <= 0 {
		t.Errorf("session = %+v", doc.Session)
	}
	if len(doc.Agents) != 1 || doc.Agents[0].ID != "aa8d6202c85e084d7" || doc.Agents[0].Messages != 7 || doc.Agents[0].ToolErrors != 2 || doc.Agents[0].Bytes <= 0 || doc.Agents[0].WorkflowRunID != "" {
		t.Errorf("agents = %+v", doc.Agents)
	}
	if len(doc.Workflows) != 1 {
		t.Fatalf("workflows = %+v", doc.Workflows)
	}
	wf := doc.Workflows[0]
	if wf.RunID != "wf_3a23189e-9d5" || wf.TaskID != "w9gxz79on" || wf.AgentCount != 2 || wf.ResumeCacheResults != 1 || wf.TotalTokens != 1741419 || wf.Bytes <= 0 || !wf.ManifestFound {
		t.Errorf("workflow = %+v", wf)
	}
}

func TestWorkflowViewShowsTheRunAndItsAgents(t *testing.T) {
	mainFilepath := sessionWithAgentAndWorkflow(t)
	for _, key := range []string{"wf_3a23189e-9d5", "3a23", "analyze", "w9gxz79on"} {
		out, _, err := runPrint(t, mainFilepath, transcriptPrintOptions{workflowID: key, all: true})
		if err != nil {
			t.Fatalf("--workflow %s: %v", key, err)
		}
		for _, want := range []string{
			"workflow wf_3a23189e-9d5 (analyze): completed, 24m47s, 2 agents, 194 tool calls, 1741419 tok",
			"task w9gxz79on, started ",
			"summary: one agent per thread",
			"phases: Review, Verify",
			`result: {"dispatched":2}`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("--workflow %s missing %q in:\n%s", key, want, out)
			}
		}
		if row := rowContaining(t, out, "aw1"); row[1] != "workflow-subagent" || row[3] != "2" || row[4] != "1" {
			t.Errorf("agent row = %v", row)
		}
		rowContaining(t, out, "aw2")
	}
}

func TestWorkflowViewJSONCarriesTheWholeManifest(t *testing.T) {
	out, _, err := runPrint(t, sessionWithAgentAndWorkflow(t), transcriptPrintOptions{workflowID: "analyze", jsonOutput: true, all: true})
	if err != nil {
		t.Fatal(err)
	}
	var doc workflowRunJSON
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if doc.Run.RunID != "wf_3a23189e-9d5" || len(doc.Agents) != 2 || doc.Manifest["result"] == nil || doc.Manifest["phases"] == nil {
		t.Errorf("doc = %+v", doc)
	}
	if doc.Agents[0].ID != "aw1" || doc.Agents[0].WorkflowRunID != "wf_3a23189e-9d5" || doc.Agents[0].ToolCalls != 1 {
		t.Errorf("agents = %+v", doc.Agents)
	}
}

func TestWorkflowViewRefusesUnknownRunAndNamesTheListing(t *testing.T) {
	_, _, err := runPrint(t, sessionWithAgentAndWorkflow(t), transcriptPrintOptions{workflowID: "nosuch", all: true})
	if err == nil || !strings.Contains(err.Error(), "no matching workflow run") || !strings.Contains(err.Error(), "--agents lists the runs") {
		t.Errorf("want a refusal that names --agents, got %v", err)
	}
	_, _, err = runPrint(t, sessionWithAgentAndWorkflow(t), transcriptPrintOptions{agentID: "nosuch", all: true})
	if err == nil || !strings.Contains(err.Error(), "--agents lists the agents") {
		t.Errorf("want a refusal that names --agents, got %v", err)
	}
}

func TestJSONFlagOutsideItsViewsTeachesTheAlternative(t *testing.T) {
	err := transcriptPrintOptions{format: textFormat, all: true, jsonOutput: true}.validate()
	if err == nil || !strings.Contains(err.Error(), "--json applies to --agents and --workflow") || !strings.Contains(err.Error(), "--format=jsonl") {
		t.Errorf("got %v", err)
	}
	for _, opts := range []transcriptPrintOptions{
		{format: textFormat, all: true, workflowID: "x", listAgents: true},
		{format: textFormat, all: true, workflowID: "x", agentID: "y"},
		{format: textFormat, all: true, workflowID: "x", expandAgents: true},
	} {
		if err := opts.validate(); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Errorf("%+v: got %v", opts, err)
		}
	}
}

func TestExpansionBudgetSkipsTheAgentWithTheNumberAndTheFlag(t *testing.T) {
	big := strings.Repeat("x", 1200*1024)
	mainFilepath := writeFakeSession(t,
		[]string{
			fakeUserLine("2026-01-01T00:00:00.000Z", "go"),
			`{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_big","name":"Agent","input":{"description":"big one"}}]}}`,
		},
		map[string][2]string{"abig": {
			`{"type":"assistant","uuid":"b","timestamp":"2026-01-01T00:01:00.000Z","message":{"role":"assistant","content":[{"type":"text","text":"` + big + `"}]}}`,
			`{"agentType":"Explore","toolUseId":"toolu_big"}`,
		}})

	out, _, err := runPrint(t, mainFilepath, transcriptPrintOptions{expandAgents: true, all: true, maxExpandMB: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[agent abig not expanded: 1.2 MB would exceed the 1.0 MB expansion budget; raise --max-expand-mb, or print it alone with --agent abig]"} {
		if !strings.Contains(out, want) {
			t.Errorf("note missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "xxxxxxxxxx") {
		t.Errorf("the over-budget agent must not be inlined")
	}
	out, _, err = runPrint(t, mainFilepath, transcriptPrintOptions{expandAgents: true, all: true, maxExpandMB: 0})
	if err != nil || !strings.Contains(out, "xxxxxxxxxx") {
		t.Errorf("0 must mean unlimited, got err=%v inlined=%v", err, strings.Contains(out, "xxxxxxxxxx"))
	}
	out, _, err = runPrint(t, mainFilepath, transcriptPrintOptions{expandAgents: true, all: true, maxExpandMB: 2})
	if err != nil || !strings.Contains(out, "xxxxxxxxxx") {
		t.Errorf("under budget must inline, got err=%v", err)
	}
}

func TestUnknownFlagSuggestsTheNearestOne(t *testing.T) {
	for typed, want := range map[string]string{
		"subagents": "agents", "agnets": "agents", "jsno": "json", "expand": "expand-agents", "workflows": "workflow",
		"sicne": "since", "tial": "tail", "verbos": "verbose", "ALL": "all", "agentsxyz": "agents",
		"zzzzzzzz": "", "a": "", "ag": "",
	} {
		if got := closestFlagName(sessionPrintCmd, typed); got != want {
			t.Errorf("closestFlagName(%q) = %q, want %q", typed, got, want)
		}
	}
	// A real flag of a sibling command is not a typo of this command's flags.
	if got := closestFlagName(missionLsCmd, "tail"); got != "" {
		t.Errorf("closestFlagName(mission ls, %q) = %q, want no suggestion", "tail", got)
	}
	rootCmd.SetArgs([]string{"session", "print", "abc", "--subagents"})
	defer rootCmd.SetArgs(nil)
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --subagents (did you mean --agents?)") {
		t.Errorf("got %v", err)
	}
}

func TestRawJSONLExpansionHonoursTheBudgetOnStderr(t *testing.T) {
	big := strings.Repeat("x", 1200*1024)
	mainFilepath := writeFakeSession(t,
		[]string{fakeUserLine("2026-01-01T00:00:00.000Z", "go")},
		map[string][2]string{"abig": {`{"type":"assistant","uuid":"b","timestamp":"2026-01-01T00:01:00.000Z","message":{"role":"assistant","content":[{"type":"text","text":"` + big + `"}]}}`, `{"agentType":"Explore"}`}})

	out, stderr, err := runPrint(t, mainFilepath, transcriptPrintOptions{format: jsonlFormat, expandAgents: true, all: true, maxExpandMB: 1})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "xxxxxxxxxx") {
		t.Errorf("the raw stream must not emit an agent over the budget")
	}
	if !strings.Contains(stderr, "warning: agent abig (1.2 MB) not emitted: over the --max-expand-mb=1 budget; raise it, or print it alone with --agent abig") {
		t.Errorf("stderr = %q", stderr)
	}
	out, _, err = runPrint(t, mainFilepath, transcriptPrintOptions{format: jsonlFormat, expandAgents: true, all: true, maxExpandMB: 0})
	if err != nil || !strings.Contains(out, "xxxxxxxxxx") {
		t.Errorf("0 must mean unlimited on the raw path too")
	}
}
