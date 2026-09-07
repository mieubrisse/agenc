package session

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// addWorkflowAgent writes an agent transcript inside a workflow run directory,
// with the minimal sidecar the Workflow tool actually writes.
func (fs *fakeSession) addWorkflowAgent(runID string, agentID string, lines ...string) {
	fs.t.Helper()
	dir := filepath.Join(fs.projectDir, fs.sessionID, "subagents", "workflows", runID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		fs.t.Fatalf("mkdir run dir: %v", err)
	}
	fs.writeFile(filepath.Join(dir, "agent-"+agentID+".jsonl"), strings.Join(lines, "\n"))
	fs.writeFile(filepath.Join(dir, "agent-"+agentID+".meta.json"), `{"agentType":"workflow-subagent","spawnDepth":1}`)
}

// addWorkflowManifest writes <session>/workflows/<runID>.json verbatim.
func (fs *fakeSession) addWorkflowManifest(runID string, manifestJSON string) {
	fs.t.Helper()
	dir := filepath.Join(fs.projectDir, fs.sessionID, "workflows")
	if err := os.MkdirAll(dir, 0755); err != nil {
		fs.t.Fatalf("mkdir workflows: %v", err)
	}
	fs.writeFile(filepath.Join(dir, runID+".json"), manifestJSON)
}

// addWorkflowJournal writes a run's journal with the given agent IDs started and
// the first `finished` of them reporting a result.
func (fs *fakeSession) addWorkflowJournal(runID string, started []string, finished int) {
	fs.t.Helper()
	var lines []string
	for _, id := range started {
		lines = append(lines, `{"type":"started","key":"v2:k","agentId":"`+id+`"}`)
	}
	for _, id := range started[:finished] {
		lines = append(lines, `{"type":"result","key":"v2:k","agentId":"`+id+`","result":{"ok":true}}`)
	}
	fs.writeFile(filepath.Join(fs.projectDir, fs.sessionID, "subagents", "workflows", runID, "journal.jsonl"), strings.Join(lines, "\n"))
}

// countAgentFilesIndependently counts agent-*.jsonl under a session directory
// with filepath.WalkDir, which shares no code with discovery. Discovery was once
// verified by a sweep that read directories the same non-recursive way the code
// did, so the sweep confirmed a bug that hid 55% of the transcripts on disk.
func countAgentFilesIndependently(t *testing.T, sessionDir string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(sessionDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if !d.IsDir() && strings.HasPrefix(name, "agent-") && strings.HasSuffix(name, ".jsonl") {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return n
}

func TestDiscoveryReadsWorkflowRunDirectories(t *testing.T) {
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addAgent("aloose", &AgentMeta{AgentType: "Explore"}, assistantLine("2026-01-01T00:01:00.000Z", "loose body"))
	fs.addWorkflowAgent("wf_aaaa1111-111", "aw1", assistantLine("2026-01-01T00:02:00.000Z", "wf body one"))
	fs.addWorkflowAgent("wf_aaaa1111-111", "aw2", assistantLine("2026-01-01T00:03:00.000Z", "wf body two"))
	fs.addWorkflowAgent("wf_bbbb2222-222", "aw3", assistantLine("2026-01-01T00:04:00.000Z", "wf body three"))

	root := fs.discover()

	if got, want := len(root.AllAgents()), countAgentFilesIndependently(t, filepath.Join(fs.projectDir, fs.sessionID)); got != want {
		t.Fatalf("AllAgents() = %d, independent count on disk = %d", got, want)
	}
	// Positive control on the control: the independent count must include the
	// nested files, or agreement proves nothing.
	if countAgentFilesIndependently(t, filepath.Join(fs.projectDir, fs.sessionID)) != 4 {
		t.Fatal("the independent counter did not see the nested layout")
	}
	if len(root.Workflows) != 2 {
		t.Fatalf("workflow runs = %d, want 2", len(root.Workflows))
	}
	// Runs stay out of the tree walk, so Flatten is bounded by loose agents.
	if got := len(root.Flatten()); got != 2 {
		t.Errorf("Flatten() = %d nodes, want 2 (main + loose agent); runs must not be tree nodes", got)
	}
	run := root.Workflows[0]
	run.LoadAgentDetails()
	if run.RunID != "wf_aaaa1111-111" || len(run.Agents) != 2 || run.Agents[0].AgentID != "aw1" {
		t.Errorf("first run = %s with %d agents, want wf_aaaa1111-111 with 2", run.RunID, len(run.Agents))
	}
	if run.Agents[0].Workflow != run || run.Agents[0].Depth != 1 {
		t.Errorf("workflow agent must point at its run and sit at depth 1")
	}
	if run.Status != "unknown" || run.ManifestFound {
		t.Errorf("run with no manifest: status=%q manifestFound=%v, want unknown/false", run.Status, run.ManifestFound)
	}
	if run.StartedAt != "2026-01-01T00:02:00.000Z" {
		t.Errorf("run without manifest should start at its earliest agent, got %q", run.StartedAt)
	}
	if second := root.Workflows[1]; second.Agents[0].StartedAt != "" && !second.detailsLoaded {
		t.Errorf("a run with agents must not read every agent's start eagerly")
	}
	if run.Bytes <= 0 || run.Agents[0].Bytes <= 0 {
		t.Errorf("sizes must be recorded: run=%d agent=%d", run.Bytes, run.Agents[0].Bytes)
	}
}

func TestDiscoveryReadsWorkflowManifestAndJournal(t *testing.T) {
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addWorkflowAgent("wf_cccc3333-333", "ac1", assistantLine("2026-01-01T00:02:00.000Z", "one"))
	fs.addWorkflowAgent("wf_cccc3333-333", "ac2", assistantLine("2026-01-01T00:03:00.000Z", "two"))
	fs.addWorkflowAgent("wf_cccc3333-333", "ac3", assistantLine("2026-01-01T00:04:00.000Z", "three"))
	fs.addWorkflowJournal("wf_cccc3333-333", []string{"ac1", "ac2", "ac3"}, 2)
	// Numbers quoted, as the real writer quotes them; agentCount deliberately
	// wrong, as it is in 40 of 106 real runs.
	fs.addWorkflowManifest("wf_cccc3333-333", `{"runId":"wf_cccc3333-333","timestamp":"2026-01-01T00:01:30.000Z","taskId":"wtask1234","workflowName":"analyze","summary":"one agent per thread","status":"completed","agentCount":"20","durationMs":"1487202","totalTokens":1741419,"totalToolCalls":"194"}`)
	// A manifest with no agent directory is still a run.
	fs.addWorkflowManifest("wf_dddd4444-444", `{"runId":"wf_dddd4444-444","timestamp":"2026-01-01T00:00:30.000Z","taskId":"wtask9999","workflowName":"empty","status":"failed"}`)

	root := fs.discover()

	if len(root.Workflows) != 2 {
		t.Fatalf("runs = %d, want 2", len(root.Workflows))
	}
	if root.Workflows[0].RunID != "wf_dddd4444-444" {
		t.Errorf("runs must order by manifest timestamp; first = %s", root.Workflows[0].RunID)
	}
	run := root.Workflows[1]
	if !run.ManifestFound || run.TaskID != "wtask1234" || run.Name != "analyze" || run.Status != "completed" || run.Summary != "one agent per thread" {
		t.Errorf("manifest not applied: %+v", *run)
	}
	if run.StartedAt != "2026-01-01T00:01:30.000Z" {
		t.Errorf("StartedAt must come from the manifest, got %q", run.StartedAt)
	}
	if run.DurationMs != 1487202 || run.TotalTokens != 1741419 || run.TotalToolCalls != 194 {
		t.Errorf("quoted and unquoted numbers must both decode: %d %d %d", run.DurationMs, run.TotalTokens, run.TotalToolCalls)
	}
	if len(run.Agents) != 3 {
		t.Errorf("agent count must come from disk (3), not the manifest (20): got %d", len(run.Agents))
	}
	if run.JournaledResults != 2 {
		t.Errorf("journal: results=%d, want 2", run.JournaledResults)
	}
	if empty := root.Workflows[0]; len(empty.Agents) != 0 || empty.Status != "failed" || empty.JournaledResults != -1 {
		t.Errorf("manifest-only run: %+v", *empty)
	}
}

func TestDiscoveryToleratesMalformedManifestAndJournal(t *testing.T) {
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addWorkflowAgent("wf_eeee5555-555", "ae1", assistantLine("2026-01-01T00:02:00.000Z", "one"))
	fs.addWorkflowManifest("wf_eeee5555-555", `{not json`)

	root := fs.discover()

	if len(root.Workflows) != 1 || len(root.Workflows[0].Agents) != 1 {
		t.Fatalf("a run with a broken manifest must still list its agents: %+v", root.Workflows)
	}
	if run := root.Workflows[0]; run.ManifestFound || run.Status != "unknown" || run.JournaledResults != -1 {
		t.Errorf("broken manifest/absent journal: %+v", *run)
	}
}

func TestResolveAgentReachesWorkflowAgentsAndNames(t *testing.T) {
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addAgent("areviewer-tests-97ef", &AgentMeta{Name: "reviewer-tests", Description: "Adversarial test-quality review"}, assistantLine("2026-01-01T00:01:00.000Z", "r"))
	fs.addAgent("areviewer-code-1234", &AgentMeta{Name: "reviewer-code", Description: "Adversarial correctness review"}, assistantLine("2026-01-01T00:01:00.000Z", "r"))
	fs.addWorkflowAgent("wf_ffff6666-666", "af0123456789", assistantLine("2026-01-01T00:02:00.000Z", "w"))

	for key, want := range map[string]string{
		"af0123456789":   "af0123456789",         // workflow agent by exact ID
		"af01":           "af0123456789",         // and by prefix
		"reviewer-tests": "areviewer-tests-97ef", // exact name beats the shared ID prefix "areviewer"
		"test-quality":   "areviewer-tests-97ef", // description substring
	} {
		got, err := ResolveAgent(fs.discover(), key)
		if err != nil {
			t.Errorf("ResolveAgent(%q): %v", key, err)
			continue
		}
		if got.AgentID != want {
			t.Errorf("ResolveAgent(%q) = %s, want %s", key, got.AgentID, want)
		}
	}

	_, err := ResolveAgent(fs.discover(), "areviewer")
	if err == nil || !strings.Contains(err.Error(), "areviewer-code-1234 (reviewer-code)") || !strings.Contains(err.Error(), "areviewer-tests-97ef (reviewer-tests)") {
		t.Errorf("ambiguous prefix must name every candidate with its label, got %v", err)
	}
	_, err = ResolveAgent(fs.discover(), "Adversarial")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("a description substring shared by two agents is ambiguous, got %v", err)
	}
	if _, err := ResolveAgent(fs.discover(), "nosuch"); !errors.Is(err, ErrAgentNotFound) {
		t.Errorf("want ErrAgentNotFound, got %v", err)
	}
}

func TestResolveWorkflowByEveryHandle(t *testing.T) {
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addWorkflowAgent("wf_3a23189e-9d5", "a1", assistantLine("2026-01-01T00:02:00.000Z", "w"))
	fs.addWorkflowAgent("wf_3a9999ee-000", "a2", assistantLine("2026-01-01T00:03:00.000Z", "w"))
	fs.addWorkflowManifest("wf_3a23189e-9d5", `{"taskId":"w9gxz79on","workflowName":"analyze","timestamp":"2026-01-01T00:01:00.000Z"}`)
	fs.addWorkflowManifest("wf_3a9999ee-000", `{"taskId":"wother","workflowName":"classify","timestamp":"2026-01-01T00:02:30.000Z"}`)
	root := fs.discover()

	for _, key := range []string{"wf_3a23189e-9d5", "wf_3a23", "3a23", "w9gxz79on", "analyze"} {
		run, err := ResolveWorkflow(root, key)
		if err != nil || run.RunID != "wf_3a23189e-9d5" {
			t.Errorf("ResolveWorkflow(%q) = %v, %v", key, run, err)
		}
	}
	if _, err := ResolveWorkflow(root, "wf_3a"); err == nil || !strings.Contains(err.Error(), "wf_3a23189e-9d5 (analyze, 1 agents)") {
		t.Errorf("ambiguous prefix must list candidates, got %v", err)
	}
	if _, err := ResolveWorkflow(root, "zzz"); !errors.Is(err, ErrWorkflowNotFound) {
		t.Errorf("want ErrWorkflowNotFound, got %v", err)
	}
}

func TestReadWorkflowManifestJSONReturnsTheWholeManifest(t *testing.T) {
	fs := newFakeSession(t, userLine("2026-01-01T00:00:00.000Z", "go"))
	fs.addWorkflowManifest("wf_1111aaaa-aaa", `{"runId":"wf_1111aaaa-aaa","result":{"dispatched":3},"phases":[{"title":"Review"}]}`)
	root := fs.discover()

	m, err := ReadWorkflowManifestJSON(root.Workflows[0])
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(m["result"])
	if string(encoded) != `{"dispatched":3}` {
		t.Errorf("result = %s", encoded)
	}
	if _, err := ReadWorkflowManifestJSON(&WorkflowRun{}); err == nil {
		t.Error("a run without a manifest path must error, not return nil")
	}
}

// TestDiscoveryAgreesWithWalkDirOnARealSession is the positive control against
// the disk: point it at a real session and discovery must find exactly the
// files an independent walk finds. It skips unless both variables are set.
//
//	TXOB_REAL_PROJECT_DIR=~/.claude/projects/<encoded-cwd> TXOB_REAL_SESSION_ID=<uuid> go test ./internal/session -run RealSession -v
func TestDiscoveryAgreesWithWalkDirOnARealSession(t *testing.T) {
	projectDir, sessionID := os.Getenv("TXOB_REAL_PROJECT_DIR"), os.Getenv("TXOB_REAL_SESSION_ID")
	if projectDir == "" || sessionID == "" {
		t.Skip("set TXOB_REAL_PROJECT_DIR and TXOB_REAL_SESSION_ID to run against a real session")
	}
	root, err := DiscoverSessionTranscripts(projectDir, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	got, want := len(root.AllAgents()), countAgentFilesIndependently(t, filepath.Join(projectDir, sessionID))
	t.Logf("discovered %d agents in %d runs + %d loose; walkdir counts %d", got, len(root.Workflows), len(root.Flatten())-1, want)
	if got != want {
		t.Fatalf("discovery = %d, independent walk = %d", got, want)
	}
}

// TestDiscoveryAgreesWithWalkDirAcrossAProjectsRoot is the corpus-wide form of
// the control above: every session under every project directory. It prints
// the totals it saw, so a run that finds nothing cannot pass as a clean one.
//
//	TXOB_REAL_PROJECTS_ROOT=~/.claude/projects go test ./internal/session -run ProjectsRoot -v
func TestDiscoveryAgreesWithWalkDirAcrossAProjectsRoot(t *testing.T) {
	root := os.Getenv("TXOB_REAL_PROJECTS_ROOT")
	if root == "" {
		t.Skip("set TXOB_REAL_PROJECTS_ROOT to run against a real ~/.claude/projects")
	}
	projects, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	sessions, agents, runs, walked, mismatches := 0, 0, 0, 0, 0
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		projectDir := filepath.Join(root, project.Name())
		entries, err := os.ReadDir(projectDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			sessionID := strings.TrimSuffix(e.Name(), ".jsonl")
			tree, err := DiscoverSessionTranscripts(projectDir, sessionID)
			if err != nil {
				t.Errorf("%s/%s: %v", project.Name(), sessionID, err)
				continue
			}
			sessions++
			got := len(tree.AllAgents())
			agents += got
			runs += len(tree.Workflows)
			want := 0
			if _, err := os.Stat(filepath.Join(projectDir, sessionID)); err == nil {
				want = countAgentFilesIndependently(t, filepath.Join(projectDir, sessionID))
			}
			walked += want
			if got != want {
				mismatches++
				t.Errorf("%s/%s: discovery=%d walkdir=%d", project.Name(), sessionID, got, want)
			}
		}
	}
	t.Logf("sessions=%d agents=%d (walkdir %d) workflow runs=%d mismatches=%d", sessions, agents, walked, runs, mismatches)
	if sessions == 0 || walked == 0 {
		t.Fatalf("the control inspected nothing: sessions=%d walked=%d", sessions, walked)
	}
}
