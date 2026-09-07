package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/rodaine/table"

	"github.com/odyssey/agenc/internal/session"
	"github.com/odyssey/agenc/internal/tableprinter"
)

// The --agents view is the session's overview: what the session is, what it
// spawned, and what each part costs to read. It is bounded by the number of
// workflow RUNS, never by their agents — a run row is built from its manifest
// and directory listing without opening a single agent transcript, so a
// session with 7214 agents in 60 runs lists in 99 rows, not 7253.

const (
	// maxResultPreviewBytes caps the run result shown by --workflow in text
	// mode; --json carries the whole manifest.
	maxResultPreviewBytes = 4000

	// workflowRowType labels a workflow run's row in the TYPE column.
	workflowRowType = "workflow"

	// maxWorkflowLabelLen caps a run row's LABEL cell. It is wider than an
	// agent's because the run's summary is the one field that says what the
	// run was for, and the overview is where a reader chooses what to open.
	maxWorkflowLabelLen = 120
)

// sessionJSON is the session block of the --agents --json document.
type sessionJSON struct {
	ID                  string `json:"id"`
	Path                string `json:"path"`
	Messages            int    `json:"messages"`
	ToolCalls           int    `json:"tool_calls"`
	ToolErrors          int    `json:"tool_errors"`
	Compactions         int    `json:"compactions"`
	FirstTimestamp      string `json:"first_timestamp,omitempty"`
	LastTimestamp       string `json:"last_timestamp,omitempty"`
	Bytes               int64  `json:"bytes"`
	ForkedFromSessionID string `json:"forked_from_session,omitempty"`
}

// agentJSON is one subagent in the JSON views.
type agentJSON struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	Model         string `json:"model,omitempty"`
	Label         string `json:"label,omitempty"`
	ParentID      string `json:"parent_id,omitempty"`
	WorkflowRunID string `json:"workflow_run_id,omitempty"`
	Depth         int    `json:"depth"`
	StartedAt     string `json:"started_at,omitempty"`
	Bytes         int64  `json:"bytes"`
	Messages      int    `json:"messages"`
	ToolCalls     int    `json:"tool_calls"`
	ToolErrors    int    `json:"tool_errors"`
	Orphaned      bool   `json:"orphaned"`
	MetadataFound bool   `json:"metadata_found"`
	Path          string `json:"path"`
}

// workflowJSON is one workflow run in the JSON views.
type workflowJSON struct {
	RunID            string `json:"run_id"`
	TaskID           string `json:"task_id,omitempty"`
	Name             string `json:"name,omitempty"`
	Summary          string `json:"summary,omitempty"`
	Status           string `json:"status"`
	StartedAt        string `json:"started_at,omitempty"`
	DurationMs       int64  `json:"duration_ms"`
	TotalTokens      int64  `json:"total_tokens"`
	TotalToolCalls   int64  `json:"total_tool_calls"`
	AgentCount       int    `json:"agent_count"`
	JournaledResults int    `json:"journaled_results"`
	Bytes            int64  `json:"bytes"`
	ManifestFound    bool   `json:"manifest_found"`
	Path             string `json:"path,omitempty"`
}

// overviewJSON is the --agents --json document.
type overviewJSON struct {
	Session   sessionJSON    `json:"session"`
	Agents    []agentJSON    `json:"agents"`
	Workflows []workflowJSON `json:"workflows"`
}

// workflowRunJSON is the --workflow --json document: the run, its whole
// manifest, and its agents with their transcript statistics.
type workflowRunJSON struct {
	Run      workflowJSON           `json:"run"`
	Manifest map[string]interface{} `json:"manifest,omitempty"`
	Agents   []agentJSON            `json:"agents"`
}

func sessionIDFromPath(jsonlFilepath string) string {
	return strings.TrimSuffix(filepath.Base(jsonlFilepath), ".jsonl")
}

// summarizeOrWarn computes a transcript's statistics, warning on stderr when
// the file cannot be read. The row is still emitted: an absent row would claim
// the agent does not exist, which is a different and wrong statement.
func summarizeOrWarn(path string, stderr io.Writer) session.TranscriptStats {
	stats, err := session.SummarizeTranscript(path)
	if err != nil {
		fmt.Fprintf(stderr, "warning: could not read transcript '%s': %v\n", path, err)
	}
	return stats
}

func agentToJSON(node *session.Transcript, stats session.TranscriptStats) agentJSON {
	out := agentJSON{
		ID:            node.AgentID,
		Type:          node.Meta.DisplayType(),
		Model:         node.Meta.Model,
		Label:         node.Meta.DisplayLabel(),
		ParentID:      node.Meta.ParentAgentID,
		Depth:         node.Depth,
		StartedAt:     node.StartedAt,
		Bytes:         node.Bytes,
		Messages:      stats.UserMessages + stats.AssistantMessages,
		ToolCalls:     stats.ToolCalls,
		ToolErrors:    stats.ToolErrors,
		Orphaned:      node.Orphaned,
		MetadataFound: node.MetaFound,
		Path:          node.Filepath,
	}
	if node.Workflow != nil {
		out.WorkflowRunID = node.Workflow.RunID
	}
	return out
}

func workflowToJSON(run *session.WorkflowRun) workflowJSON {
	return workflowJSON{
		RunID:            run.RunID,
		TaskID:           run.TaskID,
		Name:             run.Name,
		Summary:          run.Summary,
		Status:           run.Status,
		StartedAt:        run.StartedAt,
		DurationMs:       run.DurationMs,
		TotalTokens:      run.TotalTokens,
		TotalToolCalls:   run.TotalToolCalls,
		AgentCount:       len(run.Agents),
		JournaledResults: run.JournaledResults,
		Bytes:            run.Bytes,
		ManifestFound:    run.ManifestFound,
		Path:             run.Dirpath,
	}
}

// printAgentOverview renders the --agents view: a header describing the
// session, then one row per directly spawned agent (indented by nesting) and
// one row per workflow run.
func printAgentOverview(root *session.Transcript, opts transcriptPrintOptions, stdout io.Writer, stderr io.Writer) error {
	mainStats := summarizeOrWarn(root.Filepath, stderr)
	loose := root.Flatten()[1:]

	if opts.jsonOutput {
		doc := overviewJSON{
			Session:   sessionToJSON(root, mainStats),
			Agents:    []agentJSON{},
			Workflows: []workflowJSON{},
		}
		for _, node := range loose {
			doc.Agents = append(doc.Agents, agentToJSON(node, summarizeOrWarn(node.Filepath, stderr)))
		}
		for _, run := range root.Workflows {
			doc.Workflows = append(doc.Workflows, workflowToJSON(run))
		}
		return writeJSON(stdout, doc)
	}

	writeOverviewHeader(root, mainStats, stdout)
	if len(loose) == 0 && len(root.Workflows) == 0 {
		fmt.Fprintln(stdout, "No subagent transcripts for this session.")
		return nil
	}

	tbl := newAgentTable(stdout)
	tbl.AddRow(mainTranscriptLabel, "session", "-",
		fmt.Sprintf("%d", mainStats.UserMessages+mainStats.AssistantMessages),
		fmt.Sprintf("%d", mainStats.ToolCalls), fmt.Sprintf("%d", mainStats.ToolErrors),
		formatTranscriptTimestamp(root.StartedAt), formatBytes(root.Bytes), "")
	for _, node := range loose {
		addAgentRow(tbl, node, summarizeOrWarn(node.Filepath, stderr))
	}
	for _, run := range root.Workflows {
		tbl.AddRow(run.RunID, workflowRowType, "-",
			fmt.Sprintf("%d agents", len(run.Agents)),
			orDash(nonZero(run.TotalToolCalls)), "-",
			formatTranscriptTimestamp(run.StartedAt), formatBytes(run.Bytes),
			truncatePrompt(workflowLabel(run), maxWorkflowLabelLen))
	}
	tbl.Print()
	return nil
}

func sessionToJSON(root *session.Transcript, stats session.TranscriptStats) sessionJSON {
	return sessionJSON{
		ID:                  sessionIDFromPath(root.Filepath),
		Path:                root.Filepath,
		Messages:            stats.UserMessages + stats.AssistantMessages,
		ToolCalls:           stats.ToolCalls,
		ToolErrors:          stats.ToolErrors,
		Compactions:         stats.CompactBoundaries,
		FirstTimestamp:      stats.FirstTimestamp,
		LastTimestamp:       stats.LastTimestamp,
		Bytes:               root.Bytes,
		ForkedFromSessionID: stats.ForkedFromSessionID,
	}
}

// writeOverviewHeader prints the three lines that tell a reader what they are
// looking at and what it would cost to read: the session, its lineage, and its
// subagents with their sizes on disk.
func writeOverviewHeader(root *session.Transcript, stats session.TranscriptStats, w io.Writer) {
	fmt.Fprintf(w, "session %s: %d messages, %d tool calls, %d errors, %d compactions, %s, %s - %s\n",
		sessionIDFromPath(root.Filepath), stats.UserMessages+stats.AssistantMessages, stats.ToolCalls, stats.ToolErrors,
		stats.CompactBoundaries, formatBytes(root.Bytes), formatTranscriptTimestamp(stats.FirstTimestamp), formatTranscriptTimestamp(stats.LastTimestamp))
	if stats.ForkedFromSessionID != "" {
		fmt.Fprintf(w, "forked from session %s\n", stats.ForkedFromSessionID)
	}
	loose := root.Flatten()[1:]
	var looseBytes, workflowBytes int64
	workflowAgents := 0
	for _, n := range loose {
		looseBytes += n.Bytes
	}
	for _, run := range root.Workflows {
		workflowBytes += run.Bytes
		workflowAgents += len(run.Agents)
	}
	var parts []string
	if len(loose) > 0 {
		parts = append(parts, fmt.Sprintf("%d spawned directly (%s)", len(loose), formatBytes(looseBytes)))
	}
	if len(root.Workflows) > 0 {
		parts = append(parts, fmt.Sprintf("%d in %d workflow run(s) (%s)", workflowAgents, len(root.Workflows), formatBytes(workflowBytes)))
	}
	if len(parts) > 0 {
		fmt.Fprintf(w, "subagents: %s\n", strings.Join(parts, "; "))
	}
}

// workflowLabel is the LABEL cell of a run row: name, status, how many agents
// never reported, duration, then the run's summary.
func workflowLabel(run *session.WorkflowRun) string {
	parts := []string{run.Status}
	if run.DurationMs > 0 {
		parts = append(parts, formatDurationMs(run.DurationMs))
	}
	label := fmt.Sprintf("%s: %s", run.DisplayName(), strings.Join(parts, ", "))
	if run.Summary != "" {
		label += " - " + run.Summary
	}
	return label
}

// printWorkflowRun renders the --workflow view: the run's manifest facts, its
// phases and result, then its agents with transcript statistics. Only here are
// a run's agent transcripts opened, and only this run's.
func printWorkflowRun(run *session.WorkflowRun, opts transcriptPrintOptions, stdout io.Writer, stderr io.Writer) error {
	run.LoadAgentDetails()
	manifest, manifestErr := session.ReadWorkflowManifestJSON(run)
	if manifestErr != nil && run.ManifestFilepath != "" {
		fmt.Fprintf(stderr, "warning: %v\n", manifestErr)
	}

	agents := make([]agentJSON, 0, len(run.Agents))
	stats := make([]session.TranscriptStats, 0, len(run.Agents))
	for _, node := range run.Agents {
		s := summarizeOrWarn(node.Filepath, stderr)
		stats = append(stats, s)
		agents = append(agents, agentToJSON(node, s))
	}

	if opts.jsonOutput {
		return writeJSON(stdout, workflowRunJSON{Run: workflowToJSON(run), Manifest: manifest, Agents: agents})
	}

	fmt.Fprintf(stdout, "workflow %s (%s): %s\n", run.RunID, run.DisplayName(), strings.Join(runFacts(run), ", "))
	if run.TaskID != "" || run.StartedAt != "" {
		fmt.Fprintf(stdout, "task %s, started %s\n", orDash(run.TaskID), formatTranscriptTimestamp(run.StartedAt))
	}
	if run.Summary != "" {
		fmt.Fprintf(stdout, "summary: %s\n", run.Summary)
	}
	if !run.ManifestFound {
		fmt.Fprintln(stdout, "manifest: not found on disk")
	}
	if titles := phaseTitles(manifest); len(titles) > 0 {
		fmt.Fprintf(stdout, "phases: %s\n", strings.Join(titles, ", "))
	}
	if result, ok := manifest["result"]; ok && result != nil {
		encoded, err := json.Marshal(result)
		if err == nil {
			preview := string(encoded)
			if len(preview) > maxResultPreviewBytes {
				preview = preview[:maxResultPreviewBytes] + fmt.Sprintf("... (%d bytes; --%s for the whole manifest)", len(encoded), transcriptJSONFlagName)
			}
			fmt.Fprintf(stdout, "result: %s\n", preview)
		}
	}
	if len(run.Agents) == 0 {
		fmt.Fprintln(stdout, "No agent transcripts for this run.")
		return nil
	}
	fmt.Fprintln(stdout)
	tbl := newAgentTable(stdout)
	for i, node := range run.Agents {
		addAgentRow(tbl, node, stats[i])
	}
	tbl.Print()
	return nil
}

func runFacts(run *session.WorkflowRun) []string {
	facts := []string{run.Status}
	if run.DurationMs > 0 {
		facts = append(facts, formatDurationMs(run.DurationMs))
	}
	facts = append(facts, fmt.Sprintf("%d agents", len(run.Agents)))
	if run.TotalToolCalls > 0 {
		facts = append(facts, fmt.Sprintf("%d tool calls", run.TotalToolCalls))
	}
	if run.TotalTokens > 0 {
		facts = append(facts, fmt.Sprintf("%d tok", run.TotalTokens))
	}
	return facts
}

// phaseTitles extracts phase titles from a manifest's phases array, tolerating
// any shape it does not recognise.
func phaseTitles(manifest map[string]interface{}) []string {
	phases, _ := manifest["phases"].([]interface{})
	var titles []string
	for _, p := range phases {
		if m, ok := p.(map[string]interface{}); ok {
			if title, ok := m["title"].(string); ok && title != "" {
				titles = append(titles, title)
			}
		}
	}
	return titles
}

func newAgentTable(w io.Writer) table.Table {
	return tableprinter.NewTable("AGENT", "TYPE", "MODEL", "MSGS", "TOOLS", "ERR", "STARTED", "SIZE", "LABEL").WithWriter(w)
}

func addAgentRow(tbl table.Table, node *session.Transcript, stats session.TranscriptStats) {
	tbl.AddRow(
		agentTreeCell(node),
		agentTypeCell(node),
		orDash(node.Meta.Model),
		fmt.Sprintf("%d", stats.UserMessages+stats.AssistantMessages),
		fmt.Sprintf("%d", stats.ToolCalls),
		fmt.Sprintf("%d", stats.ToolErrors),
		formatTranscriptTimestamp(node.StartedAt),
		formatBytes(node.Bytes),
		truncatePrompt(agentLabelCell(node), maxAgentLabelLen),
	)
}

func nonZero(n int64) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%d", n)
}

// formatDurationMs renders a millisecond duration the way the transcript
// renderer does: 850ms, 12.3s, 24m48s.
func formatDurationMs(ms int64) string {
	switch {
	case ms < 1000:
		return fmt.Sprintf("%dms", ms)
	case ms < 60000:
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	default:
		total := ms / 1000
		return fmt.Sprintf("%dm%02ds", total/60, total%60)
	}
}

func writeJSON(w io.Writer, doc interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return stacktrace.Propagate(err, "failed to encode JSON")
	}
	return nil
}
