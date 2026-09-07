package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// A session's subagents live in two places, and the second is the bigger one.
//
// Agents spawned with the Agent tool write to <session-id>/subagents/ directly.
// Agents spawned by the Workflow tool write to a per-run directory,
// <session-id>/subagents/workflows/<run-id>/, next to a journal.jsonl that
// records each agent's start and structured result. The run's own manifest —
// name, summary, status, duration, token totals and the task ID its spawn
// reported — is written separately to <session-id>/workflows/<run-id>.json.
//
// Measured over every session on one machine: 8918 of 16076 subagent
// transcripts (55%) were in workflow run directories, so a reader of the flat
// directory alone sees less than half of what a session did. The nested
// sidecars carry only an agentType and spawnDepth — no parent, no name, no
// toolUseId — so a workflow agent is linked to its spawn site through its run:
// the manifest's taskId matches the "Task ID" the Workflow tool's result
// reported, 106 of 106 runs on that machine.
//
// The manifest's agentCount disagrees with the number of transcripts on disk
// in 40 of those 106 runs, so agent counts are always taken from the directory.

const (
	// workflowsDirname names both the per-run agent directories under
	// subagents/ and the manifest directory under <session-id>/.
	workflowsDirname = "workflows"

	// workflowRunPrefix starts every run ID and run directory name.
	workflowRunPrefix = "wf_"

	// workflowJournalFilename is the per-run journal of agent starts and results.
	workflowJournalFilename = "journal.jsonl"

	// workflowManifestSuffix completes <run-id> into its manifest filename.
	workflowManifestSuffix = ".json"

	// unknownWorkflowStatus is reported when a run has agents on disk but no
	// readable manifest.
	unknownWorkflowStatus = "unknown"
)

// WorkflowRun is one Workflow tool invocation: its manifest and the agents it
// spawned. It is not a Transcript — a run has no conversation of its own — so
// it is not a node in the agent tree; it hangs off the session root.
type WorkflowRun struct {
	// RunID is the wf_-prefixed identifier naming the run directory and manifest.
	RunID string

	// Dirpath is the run's agent directory; "" when only a manifest exists.
	Dirpath string

	// ManifestFilepath is the run's manifest; ManifestFound reports whether it
	// was read successfully. A run without a manifest still lists its agents.
	ManifestFilepath string
	ManifestFound    bool

	// TaskID is the background task ID the Workflow tool's result reported,
	// which is how the run is linked back to its spawn site.
	TaskID string

	Name    string
	Summary string
	Status  string

	// StartedAt is the manifest's RFC3339 timestamp, or the earliest agent's
	// start when there is no manifest.
	StartedAt string

	DurationMs     int64
	TotalTokens    int64
	TotalToolCalls int64

	// JournaledResults counts the journal's result records, or -1 when the
	// journal could not be read. The journal is the Workflow tool's resume
	// cache, not a completion log: on one machine every agent file had a
	// "started" record, while runs marked completed had hundreds of agents
	// with no "result" (699 started, 253 results). It therefore says how
	// many results were cached for resume, and nothing about which agents
	// finished; it is kept for the JSON view and never rendered as a claim.
	JournaledResults int

	// Bytes is the total size of the run's agent transcripts on disk.
	Bytes int64

	// Agents are the run's transcripts. Their StartedAt is filled lazily by
	// LoadAgentDetails, because reading the first record of every agent in a
	// 7000-agent session costs seconds that a listing of runs does not need.
	Agents []*Transcript

	detailsLoaded bool
}

// LoadAgentDetails reads each agent's start timestamp and orders the run's
// agents chronologically. It is idempotent and is called by every code path
// that shows a run's agents individually.
func (r *WorkflowRun) LoadAgentDetails() {
	if r.detailsLoaded {
		return
	}
	r.detailsLoaded = true
	for _, a := range r.Agents {
		if a.StartedAt == "" {
			a.StartedAt = readStartTimestamp(a.Filepath)
		}
	}
	sort.SliceStable(r.Agents, func(i, j int) bool { return lessByStart(r.Agents[i], r.Agents[j]) })
}

// DisplayName returns the workflow's name from its manifest, or a placeholder.
func (r *WorkflowRun) DisplayName() string {
	if r.Name != "" {
		return r.Name
	}
	return "workflow"
}

// flexInt64 decodes a JSON number that the manifest writer sometimes quotes
// ("agentCount": "20") and sometimes does not. Anything unparseable reads as 0
// rather than failing the whole manifest, because every field here is
// descriptive and a partial manifest beats none.
type flexInt64 int64

func (v *flexInt64) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*v = 0
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		*v = 0
		return nil
	}
	*v = flexInt64(f)
	return nil
}

// workflowManifest mirrors the fields of <session-id>/workflows/<run-id>.json
// that the listing uses. The file also carries the script, args, phases,
// progress log and the run's structured result; those are read on demand by
// ReadWorkflowManifestJSON.
type workflowManifest struct {
	RunID          string    `json:"runId"`
	Timestamp      string    `json:"timestamp"`
	TaskID         string    `json:"taskId"`
	WorkflowName   string    `json:"workflowName"`
	Summary        string    `json:"summary"`
	Status         string    `json:"status"`
	DurationMs     flexInt64 `json:"durationMs"`
	TotalTokens    flexInt64 `json:"totalTokens"`
	TotalToolCalls flexInt64 `json:"totalToolCalls"`
}

// SessionWorkflowsDirpath returns the directory holding a session's workflow
// run manifests. It need not exist.
func SessionWorkflowsDirpath(projectDirpath string, sessionID string) string {
	return filepath.Join(projectDirpath, sessionID, workflowsDirname)
}

// discoverWorkflowRuns finds every workflow run of a session: the union of run
// directories under subagents/workflows/ and manifests under workflows/, keyed
// by run ID. A run with agents but no manifest is listed with status unknown; a
// run with a manifest but no agents is listed with none.
func discoverWorkflowRuns(subagentsDirpath string, manifestsDirpath string) []*WorkflowRun {
	byID := map[string]*WorkflowRun{}
	get := func(runID string) *WorkflowRun {
		if r := byID[runID]; r != nil {
			return r
		}
		r := &WorkflowRun{RunID: runID, Status: unknownWorkflowStatus, JournaledResults: -1}
		byID[runID] = r
		return r
	}

	runsDirpath := filepath.Join(subagentsDirpath, workflowsDirname)
	if entries, err := os.ReadDir(runsDirpath); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), workflowRunPrefix) {
				continue
			}
			run := get(entry.Name())
			run.Dirpath = filepath.Join(runsDirpath, entry.Name())
			run.Agents = discoverSubagentTranscripts(run.Dirpath, false)
			for _, a := range run.Agents {
				a.Workflow = run
				a.Depth = 1
				run.Bytes += a.Bytes
			}
			sort.SliceStable(run.Agents, func(i, j int) bool { return run.Agents[i].AgentID < run.Agents[j].AgentID })
			run.JournaledResults = countJournalResults(filepath.Join(run.Dirpath, workflowJournalFilename))
		}
	}

	if entries, err := os.ReadDir(manifestsDirpath); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasPrefix(name, workflowRunPrefix) || !strings.HasSuffix(name, workflowManifestSuffix) {
				continue
			}
			run := get(strings.TrimSuffix(name, workflowManifestSuffix))
			run.ManifestFilepath = filepath.Join(manifestsDirpath, name)
			applyWorkflowManifest(run)
		}
	}

	runs := make([]*WorkflowRun, 0, len(byID))
	for _, run := range byID {
		if run.StartedAt == "" && len(run.Agents) > 0 {
			// No manifest: the run's start is its earliest agent's, which costs
			// one read per agent — paid only for runs that lack a manifest.
			run.LoadAgentDetails()
			run.StartedAt = run.Agents[0].StartedAt
		}
		runs = append(runs, run)
	}
	sort.SliceStable(runs, func(i, j int) bool {
		if runs[i].StartedAt != runs[j].StartedAt {
			return runs[i].StartedAt < runs[j].StartedAt
		}
		return runs[i].RunID < runs[j].RunID
	})
	return runs
}

// applyWorkflowManifest reads a run's manifest into it. A missing or malformed
// manifest leaves the run listed with whatever the directory established.
func applyWorkflowManifest(run *WorkflowRun) {
	data, err := os.ReadFile(run.ManifestFilepath)
	if err != nil {
		return
	}
	var m workflowManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return
	}
	run.ManifestFound = true
	run.TaskID = m.TaskID
	run.Name = m.WorkflowName
	run.Summary = m.Summary
	if m.Status != "" {
		run.Status = m.Status
	}
	run.StartedAt = m.Timestamp
	run.DurationMs = int64(m.DurationMs)
	run.TotalTokens = int64(m.TotalTokens)
	run.TotalToolCalls = int64(m.TotalToolCalls)
}

// countJournalResults counts the result records in a run's journal, or -1 when
// the journal cannot be read. See WorkflowRun.JournaledResults for what that
// number does and does not mean.
func countJournalResults(journalFilepath string) int {
	n := 0
	err := ScanJSONLLines(journalFilepath, func(line []byte) error {
		var record struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(line, &record) == nil && record.Type == "result" {
			n++
		}
		return nil
	})
	if err != nil {
		return -1
	}
	return n
}

// ReadWorkflowManifestJSON returns a run's whole manifest decoded as generic
// JSON, for callers that want the fields the listing does not keep — the
// phases, the progress log, the script and the run's structured result.
func ReadWorkflowManifestJSON(run *WorkflowRun) (map[string]interface{}, error) {
	if run == nil || run.ManifestFilepath == "" {
		return nil, fmt.Errorf("workflow run has no manifest on disk")
	}
	data, err := os.ReadFile(run.ManifestFilepath)
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("manifest '%s' is not valid JSON: %w", run.ManifestFilepath, err)
	}
	return out, nil
}

// ErrWorkflowNotFound is returned by ResolveWorkflow when nothing matches.
var ErrWorkflowNotFound = errors.New("no matching workflow run")

// ResolveWorkflow finds one workflow run by run ID, run ID prefix (with or
// without the wf_ prefix), task ID, or workflow name. An exact ID match always
// wins; an ambiguous key is an error naming every candidate.
func ResolveWorkflow(root *Transcript, key string) (*WorkflowRun, error) {
	if key == "" {
		return nil, fmt.Errorf("%w: empty workflow ID", ErrWorkflowNotFound)
	}
	var matches []*WorkflowRun
	for _, run := range root.Workflows {
		if run.RunID == key || run.TaskID == key {
			return run, nil
		}
	}
	for _, run := range root.Workflows {
		if strings.HasPrefix(run.RunID, key) || strings.HasPrefix(strings.TrimPrefix(run.RunID, workflowRunPrefix), key) || run.Name == key {
			matches = append(matches, run)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("%w for '%s'", ErrWorkflowNotFound, key)
	case 1:
		return matches[0], nil
	default:
		ids := make([]string, 0, len(matches))
		for _, m := range matches {
			ids = append(ids, fmt.Sprintf("%s (%s, %d agents)", m.RunID, m.DisplayName(), len(m.Agents)))
		}
		return nil, fmt.Errorf("workflow '%s' is ambiguous, matches: %s", key, strings.Join(ids, "; "))
	}
}
