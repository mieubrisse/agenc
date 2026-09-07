package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

const (
	// maxToolParamLen is the maximum length for tool parameter values before truncation.
	maxToolParamLen = 100

	// maxErrorLen is the maximum length for tool result error messages before truncation.
	maxErrorLen = 200

	// maxEventContentLen caps the one-line rendering of an event record's
	// content (hook output, queued prompt, system notice) in default mode.
	maxEventContentLen = 300

	// maxToolResultLen caps a successful tool result's body. Successful results
	// only render under Verbose, where they are context rather than the point.
	maxToolResultLen = 400

	// nestedIndent prefixes every line of an inlined subagent transcript.
	nestedIndent = "    "
)

// jsonlRecord is the union of every top-level field the formatter reads from a
// JSONL record. Claude Code writes more than twenty record types into a single
// transcript and a given record populates only a handful of these; every field
// is therefore optional and absence is never an error.
type jsonlRecord struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	Message   json.RawMessage `json:"message"`
	Timestamp string          `json:"timestamp"`

	// Conversation-shaping flags.
	IsMeta           bool `json:"isMeta"`
	IsCompactSummary bool `json:"isCompactSummary"`

	// Provenance. Origin distinguishes a real human turn from a peer agent's
	// message or a background-task notification; ForkedFrom marks a record
	// copied in when the session was forked off another one.
	Origin     *recordOrigin `json:"origin"`
	ForkedFrom *forkRef      `json:"forkedFrom"`

	// Event payloads.
	Content         json.RawMessage  `json:"content"`
	Level           string           `json:"level"`
	Operation       string           `json:"operation"`
	CompactMetadata *compactMetadata `json:"compactMetadata"`
	Attachment      json.RawMessage  `json:"attachment"`
	ToolUseResult   json.RawMessage  `json:"toolUseResult"`
	Summary         string           `json:"summary"`

	// Payloads of the small single-purpose record types. Each of these carries
	// its whole meaning in one field; without them a verbose render prints the
	// record's existence and drops what it said.
	CustomTitle      string `json:"customTitle"`
	AgencCustomTitle string `json:"agencCustomTitle"`
	AITitle          string `json:"aiTitle"`
	AgentName        string `json:"agentName"`
	Mode             string `json:"mode"`
	PermissionMode   string `json:"permissionMode"`
	LastPrompt       string `json:"lastPrompt"`

	// Hook and error bookkeeping.
	HookErrors            json.RawMessage `json:"hookErrors"`
	HookInfos             json.RawMessage `json:"hookInfos"`
	PreventedContinuation bool            `json:"preventedContinuation"`
	StopReason            string          `json:"stopReason"`
	DurationMs            float64         `json:"durationMs"`
	MessageCount          int             `json:"messageCount"`
	RetryAttempt          int             `json:"retryAttempt"`
	MaxRetries            int             `json:"maxRetries"`
}

// recordOrigin identifies who or what produced a user record.
type recordOrigin struct {
	Kind string `json:"kind"`
	From string `json:"from"`
}

// forkRef points at the session and message a forked session branched from.
type forkRef struct {
	SessionID   string `json:"sessionId"`
	MessageUUID string `json:"messageUuid"`
}

// compactMetadata describes a compaction boundary: what triggered it and how
// much context it discarded.
type compactMetadata struct {
	Trigger                 string  `json:"trigger"`
	PreTokens               float64 `json:"preTokens"`
	PostTokens              float64 `json:"postTokens"`
	CumulativeDroppedTokens float64 `json:"cumulativeDroppedTokens"`
	DurationMs              float64 `json:"durationMs"`
}

// apiMessage represents a Claude API message with role and content blocks.
type apiMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// contentBlock represents a single block within a message's content array.
type contentBlock struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text"`
	Thinking  string                 `json:"thinking"`
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Input     map[string]interface{} `json:"input"`
	ToolUseID string                 `json:"tool_use_id"`
	Content   json.RawMessage        `json:"content"`
	IsError   bool                   `json:"is_error"`
}

// agentToolUseResult is the toolUseResult a completed subagent spawn writes
// back onto the tool_result record in the spawner's transcript. It is the only
// place the spawner's transcript records which agent ID ran.
type agentToolUseResult struct {
	Status            string  `json:"status"`
	AgentID           string  `json:"agentId"`
	AgentType         string  `json:"agentType"`
	ResolvedModel     string  `json:"resolvedModel"`
	TotalDurationMs   float64 `json:"totalDurationMs"`
	TotalTokens       float64 `json:"totalTokens"`
	TotalToolUseCount float64 `json:"totalToolUseCount"`
}

// toolParamSpec defines which input fields to extract for a tool's one-line summary.
type toolParamSpec struct {
	primary   string
	secondary string
}

// toolParamMap maps tool names to the input fields that should appear in their one-line summary.
var toolParamMap = map[string]toolParamSpec{
	"Bash":           {primary: "command"},
	"Read":           {primary: "file_path"},
	"Edit":           {primary: "file_path"},
	"Write":          {primary: "file_path"},
	"Glob":           {primary: "pattern"},
	"Grep":           {primary: "pattern", secondary: "path"},
	"WebSearch":      {primary: "query"},
	"WebFetch":       {primary: "url"},
	"Task":           {primary: "description"},
	"Agent":          {primary: "description", secondary: "subagent_type"},
	"SendMessage":    {primary: "to", secondary: "summary"},
	"NotebookEdit":   {primary: "notebook_path"},
	"Skill":          {primary: "skill"},
	"SlashCommand":   {primary: "command"},
	"TaskCreate":     {primary: "subject"},
	"TaskUpdate":     {primary: "taskId", secondary: "status"},
	"TaskOutput":     {primary: "taskId"},
	"TaskStop":       {primary: "taskId"},
	"BashOutput":     {primary: "bash_id"},
	"Workflow":       {primary: "name"},
	"ScheduleWakeup": {primary: "reason"},
	"ToolSearch":     {primary: "query"},
}

// FormatOptions controls which record classes a transcript render includes and
// whether subagent transcripts are inlined.
//
// The default (zero value) renders the conversation plus the structural events
// that change how the conversation should be read: compaction boundaries, fork
// lineage, subagent spawns and returns, queued human input, slash commands and
// errors. Verbose adds the high-volume bookkeeping records — hooks, turn
// timings, attachments, thinking blocks, successful tool results — which are
// noise for reading but necessary for auditing.
type FormatOptions struct {
	// TailLines, when > 0, limits the render to the last N JSONL records of
	// the transcript named by the caller. Inlined subagent transcripts are
	// always rendered whole, because a tail of a subagent is meaningless
	// without its prompt.
	TailLines int

	// Verbose renders every record class rather than the conversational ones.
	Verbose bool

	// ExpandAgents inlines each subagent's transcript, indented, at the point
	// its spawner called it. Requires Root.
	ExpandAgents bool

	// Root is the session's transcript tree. When set, subagent spawns are
	// annotated with the agent ID needed to print them, and ExpandAgents
	// becomes available. When nil the formatter renders a single flat file.
	Root *Transcript

	// MaxExpandBytes bounds the subagent transcript bytes ExpandAgents may
	// inline across the whole render; 0 is unlimited. Past the budget an agent
	// is not inlined and a note in its place says so and names the flag. The
	// budget is enforced where inlining happens, so a tailed render is only
	// charged for the agents actually spawned inside the printed window.
	MaxExpandBytes int64

	// ListCommand, when set, is the copy-pasteable command that lists the
	// session's subagents. It is appended to the footer that reports how many
	// transcripts the render left out, so a reader who cannot see stderr and
	// has never read --help still learns the exact next command.
	ListCommand string
}

// FormatTranscriptFile renders one JSONL transcript file under the given
// options. Subagent awareness requires opts.Root; without it the file is
// rendered on its own.
func FormatTranscriptFile(jsonlFilepath string, opts FormatOptions, w io.Writer) error {
	f := newConversationFormatter(opts)
	f.target = opts.Root
	return f.render(jsonlFilepath, opts.TailLines, w)
}

// FormatTranscript renders a node of a transcript tree — the session's main
// transcript or one subagent's — under the given options.
func FormatTranscript(t *Transcript, opts FormatOptions, w io.Writer) error {
	if t == nil {
		return fmt.Errorf("no transcript to format")
	}
	f := newConversationFormatter(opts)
	f.target = t
	return f.render(t.Filepath, opts.TailLines, w)
}

// conversationFormatter holds the per-render lookup tables and the set of
// subagents already inlined, so that a subagent reachable both by its spawn
// tool_use ID and by its return record is rendered exactly once.
type conversationFormatter struct {
	opts FormatOptions
	// target is the transcript node being rendered, which is the session's
	// main transcript unless the caller selected one subagent. It scopes the
	// unlinked-agents trailer.
	target      *Transcript
	byToolUseID map[string]*Transcript
	byAgentID   map[string]*Transcript
	byAgentName map[string][]*Transcript
	// claimed marks agents a spawn site or return line has already accounted
	// for, so a reused agent name is not handed to two call sites and the
	// unlinked-agents trailer only lists what genuinely has no anchor.
	claimed map[string]bool
	// byTaskID indexes workflow runs by the task ID their spawn's tool_result
	// reported; runByToolUseID is filled per render from the file scan, which
	// is the only place the tool_use ID and the task ID meet.
	byTaskID       map[string]*WorkflowRun
	runByToolUseID map[string]*WorkflowRun
	// claimedRuns marks workflow runs whose spawn site was rendered, so the
	// trailer lists only runs with no anchor in the printed range.
	claimedRuns map[string]bool
	// expandedBytes is the running total of subagent bytes inlined so far,
	// shared with nested formatters so the budget is per render, not per level.
	expandedBytes *int64
	// inlined marks agents whose transcript has already been written out.
	// Transcripts contain duplicate records (the same uuid written twice is
	// present in real files), so a spawn can be rendered more than once;
	// without this guard the duplicate would inline the whole subagent twice.
	inlined map[string]bool
	depth   int
}

func newConversationFormatter(opts FormatOptions) *conversationFormatter {
	f := &conversationFormatter{
		opts:          opts,
		byToolUseID:   map[string]*Transcript{},
		byAgentID:     map[string]*Transcript{},
		byAgentName:   map[string][]*Transcript{},
		claimed:       map[string]bool{},
		inlined:       map[string]bool{},
		byTaskID:      map[string]*WorkflowRun{},
		claimedRuns:   map[string]bool{},
		expandedBytes: new(int64),
	}
	if opts.Root != nil {
		for _, run := range opts.Root.Workflows {
			if run.TaskID != "" {
				f.byTaskID[run.TaskID] = run
			}
		}
		opts.Root.Walk(func(n *Transcript) {
			if n.IsMain() {
				return
			}
			f.byAgentID[n.AgentID] = n
			if n.Meta.ToolUseID != "" {
				f.byToolUseID[n.Meta.ToolUseID] = n
			}
			if n.Meta.Name != "" {
				f.byAgentName[n.Meta.Name] = append(f.byAgentName[n.Meta.Name], n)
			}
		})
		for name := range f.byAgentName {
			candidates := f.byAgentName[name]
			sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].StartedAt < candidates[j].StartedAt })
		}
	}
	return f
}

// render reads a transcript file and writes its formatted blocks, separated by
// blank lines. A leading fork banner is emitted when the file carries fork
// provenance.
func (f *conversationFormatter) render(jsonlFilepath string, tailLines int, w io.Writer) error {
	lines, fork, taskIDs, err := scanTranscriptFile(jsonlFilepath, tailLines)
	if err != nil {
		return err
	}
	f.runByToolUseID = map[string]*WorkflowRun{}
	for toolUseID, taskID := range taskIDs {
		if run := f.byTaskID[taskID]; run != nil {
			f.runByToolUseID[toolUseID] = run
		}
	}

	var blocks []string
	if fork != nil {
		blocks = append(blocks, fmt.Sprintf("[FORK] this session was forked from session %s\n", fork.SessionID))
	}
	seenUUIDs := map[string]bool{}
	for _, line := range lines {
		if isDuplicateRecord(line, seenUUIDs) {
			continue
		}
		if formatted := f.formatLine(line); formatted != "" {
			blocks = append(blocks, formatted)
		}
	}
	if trailer := f.unexpandedAgentsTrailer(); trailer != "" {
		blocks = append(blocks, trailer)
	}
	if trailer := f.unlinkedWorkflowsTrailer(); trailer != "" {
		blocks = append(blocks, trailer)
	}
	if footer := f.hiddenTranscriptsFooter(); footer != "" {
		blocks = append(blocks, footer)
	}

	for i, block := range blocks {
		fmt.Fprint(w, block)
		if i < len(blocks)-1 {
			fmt.Fprintln(w)
		}
	}
	return nil
}

// isDuplicateRecord reports whether a line repeats a record already rendered,
// recording its identity when it does not.
//
// Real transcripts contain the same record written twice — on one 68 MB session
// on disk, 44% of the records are repeats, and a forked file can hold its entire
// pre-fork conversation verbatim a second time. Rendering both copies shows the
// conversation happening twice, which is not what happened. Records with no uuid
// (queue operations, titles, mode switches) are never deduplicated, because
// nothing identifies them.
func isDuplicateRecord(line string, seen map[string]bool) bool {
	var record struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal([]byte(line), &record); err != nil || record.UUID == "" {
		return false
	}
	if seen[record.UUID] {
		return true
	}
	seen[record.UUID] = true
	return false
}

// unexpandedAgentsTrailer lists subagent transcripts that expansion never
// reached. An agent whose spawn tool_use is outside the printed tail, or whose
// spawner was killed before writing a result, has no anchor in the rendered
// text; without this trailer it would silently vanish from an --expand-agents
// render that claims to be complete.
//
// The walk is over the transcript being rendered, not the whole session: when
// the caller asked for one subagent, the other 297 in the session are not
// "missing from the printed range", they were never in scope, and listing them
// buries the transcript that was asked for.
func (f *conversationFormatter) unexpandedAgentsTrailer() string {
	if !f.opts.ExpandAgents || f.depth > 0 {
		return ""
	}
	scope := f.target
	if scope == nil {
		scope = f.opts.Root
	}
	if scope == nil {
		return ""
	}

	var missing []*Transcript
	scope.Walk(func(n *Transcript) {
		if n == scope || n.IsMain() {
			return
		}
		if !f.claimed[n.AgentID] {
			missing = append(missing, n)
		}
	})
	if len(missing) == 0 {
		return ""
	}
	sort.SliceStable(missing, func(i, j int) bool { return missing[i].StartedAt < missing[j].StartedAt })

	var b strings.Builder
	fmt.Fprintf(&b, "[UNLINKED AGENTS] %d subagent transcript(s) with no spawn site in the printed range\n", len(missing))
	for _, n := range missing {
		fmt.Fprintf(&b, "  %s (%s) %s\n", n.AgentID, n.Meta.DisplayType(), truncate(n.Meta.DisplayLabel(), maxToolParamLen))
	}
	return b.String()
}

// unlinkedWorkflowsTrailer is the workflow-run counterpart of the agents
// trailer: under expansion it lists runs whose Workflow spawn is outside the
// printed range. It is bounded by the number of runs, never by their agents.
func (f *conversationFormatter) unlinkedWorkflowsTrailer() string {
	if !f.opts.ExpandAgents || f.depth > 0 || f.target == nil || !f.target.IsMain() || f.opts.Root == nil {
		return ""
	}
	var missing []*WorkflowRun
	for _, run := range f.opts.Root.Workflows {
		if !f.claimedRuns[run.RunID] {
			missing = append(missing, run)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[UNLINKED WORKFLOWS] %d workflow run(s) with no spawn site in the printed range\n", len(missing))
	for _, run := range missing {
		fmt.Fprintf(&b, "  %s (%s): %d agent(s), %s\n", run.RunID, run.DisplayName(), len(run.Agents), run.Status)
	}
	return b.String()
}

// hiddenTranscriptsFooter closes a render of the session's main transcript
// with what it left out and the command that shows it. It is the one line a
// reader is guaranteed to reach, which is why it is on stdout and carries the
// exact flags: a hint on stderr is invisible to every caller that captured
// only stdout, and "see --help" is not a command.
//
// Workflow runs are never inlined, even under expansion — one real session
// holds 7214 agents in 60 runs, and inlining them would turn a 1 MB render
// into hundreds of megabytes — so under expansion the footer reports the runs.
func (f *conversationFormatter) hiddenTranscriptsFooter() string {
	if f.depth > 0 || f.target == nil || !f.target.IsMain() || f.opts.Root == nil {
		return ""
	}
	root := f.opts.Root
	loose := len(root.Flatten()) - 1
	workflowAgents := 0
	for _, run := range root.Workflows {
		workflowAgents += len(run.Agents)
	}
	runs := len(root.Workflows)

	if f.opts.ExpandAgents {
		if workflowAgents == 0 {
			return ""
		}
		return fmt.Sprintf("[WORKFLOWS] %d agent transcript(s) in %d workflow run(s) are not inlined - --workflow <id> to list a run\n", workflowAgents, runs)
	}

	if loose+workflowAgents == 0 {
		return ""
	}
	var parts []string
	if loose > 0 {
		parts = append(parts, fmt.Sprintf("%d spawned directly", loose))
	}
	if runs > 0 {
		parts = append(parts, fmt.Sprintf("%d in %d workflow run(s)", workflowAgents, runs))
	}
	line := fmt.Sprintf("[SUBAGENTS] %d transcript(s) not shown: %s - --agents to list them, --agent <id> or --workflow <id> to open one", loose+workflowAgents, strings.Join(parts, ", "))
	if f.opts.ListCommand != "" {
		line += " - " + f.opts.ListCommand
	}
	return line + "\n"
}

// formatLine parses a single JSONL record and returns its formatted
// representation, or "" if the record renders nothing under these options.
func (f *conversationFormatter) formatLine(line string) string {
	var record jsonlRecord
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		return ""
	}

	switch record.Type {
	case "user":
		return f.formatUserEntry(record)
	case "assistant":
		return f.formatAssistantEntry(record)
	default:
		return f.formatEventEntry(record)
	}
}

// formatUserEntry formats a user record. Plain text content is shown under a
// [timestamp USER] header tagged with the message's origin. A record carrying a
// subagent's toolUseResult renders as an agent-return line, and optionally the
// whole subagent transcript. Tool result blocks render on error, and under
// Verbose on success.
func (f *conversationFormatter) formatUserEntry(record jsonlRecord) string {
	if record.IsCompactSummary {
		return f.formatCompactSummary(record)
	}
	if record.IsMeta && !f.opts.Verbose {
		return ""
	}

	var msg apiMessage
	if err := json.Unmarshal(record.Message, &msg); err != nil {
		return ""
	}

	header := formatRoleHeader(userRoleLabel(record.Origin), record.Timestamp)

	// String content is a plain user turn.
	var textContent string
	if err := json.Unmarshal(msg.Content, &textContent); err == nil {
		if textContent == "" {
			return ""
		}
		return header + "\n" + textContent + "\n"
	}

	var blocks []contentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return ""
	}

	parts, hasUserText := f.formatUserBlocks(blocks, f.decodeAgentResult(record.ToolUseResult))
	if len(parts) == 0 {
		return ""
	}
	// Blocks that are purely tool results are not something the user said, so
	// they render without a USER header. A record that mixes real user text
	// with tool results keeps the header.
	if hasUserText {
		return header + "\n" + strings.Join(parts, "\n") + "\n"
	}
	return strings.Join(parts, "\n") + "\n"
}

// formatUserBlocks renders the content blocks of a user record, reporting
// whether any of them was text the user actually wrote.
func (f *conversationFormatter) formatUserBlocks(blocks []contentBlock, agentResult *agentToolUseResult) ([]string, bool) {
	var parts []string
	hasUserText := false
	for _, b := range blocks {
		if b.Type == "text" {
			if b.Text != "" {
				hasUserText = true
				parts = append(parts, b.Text)
			}
			continue
		}
		if b.Type != "tool_result" {
			continue
		}
		rendered := f.formatToolResultBlock(b, agentResult)
		if rendered == "" {
			continue
		}
		if agentResult != nil && !b.IsError {
			// toolUseResult is a property of the record, not of any one block,
			// so a record carrying several tool_result blocks would otherwise
			// repeat the same subagent return line once per block.
			agentResult = nil
		}
		parts = append(parts, rendered)
	}
	return parts, hasUserText
}

// formatToolResultBlock renders one tool_result block, or "" when it is not
// visible under these options. A failure always renders; a subagent's return
// renders as its own summary line; an ordinary success is noise by default and
// context under Verbose.
func (f *conversationFormatter) formatToolResultBlock(b contentBlock, agentResult *agentToolUseResult) string {
	if b.IsError {
		errMsg := extractToolResultText(b)
		if errMsg == "" {
			return ""
		}
		return "  > ERROR: " + truncate(errMsg, maxErrorLen)
	}
	if agentResult != nil {
		return f.formatAgentReturn(agentResult)
	}
	if !f.opts.Verbose {
		return ""
	}
	body := extractToolResultText(b)
	if body == "" {
		return ""
	}
	return "  < " + indentContinuation(truncate(body, maxToolResultLen))
}

// formatCompactSummary renders the synthetic user message Claude Code injects
// after compacting a conversation. It is labelled distinctly because it is not
// something the user said — mistaking it for a human turn misreads the whole
// transcript — and it is never truncated, because after a compaction it is the
// only surviving record of everything before the boundary.
func (f *conversationFormatter) formatCompactSummary(record jsonlRecord) string {
	var msg apiMessage
	if err := json.Unmarshal(record.Message, &msg); err != nil {
		return ""
	}
	body := ""
	var textContent string
	if err := json.Unmarshal(msg.Content, &textContent); err == nil {
		body = textContent
	} else {
		var blocks []contentBlock
		if err := json.Unmarshal(msg.Content, &blocks); err == nil {
			var texts []string
			for _, b := range blocks {
				if b.Type == "text" && b.Text != "" {
					texts = append(texts, b.Text)
				}
			}
			body = strings.Join(texts, "\n")
		}
	}
	if body == "" {
		return ""
	}
	return formatRoleHeader("COMPACT SUMMARY", record.Timestamp) + "\n" + body + "\n"
}

// userRoleLabel tags the USER header with the message's origin when the record
// carries one. Distinguishing a human order from a peer agent's message or a
// background-task notification is not cosmetic: all three arrive as user
// records and are otherwise indistinguishable in the rendered transcript.
func userRoleLabel(origin *recordOrigin) string {
	if origin == nil || origin.Kind == "" {
		return "USER"
	}
	if origin.From != "" {
		return fmt.Sprintf("USER %s:%s", origin.Kind, origin.From)
	}
	return "USER " + origin.Kind
}

// decodeAgentResult extracts a subagent completion payload from a record's
// toolUseResult, returning nil when the field is absent or belongs to an
// ordinary tool.
func (f *conversationFormatter) decodeAgentResult(raw json.RawMessage) *agentToolUseResult {
	if len(raw) == 0 {
		return nil
	}
	var result agentToolUseResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil
	}
	if result.AgentID == "" {
		return nil
	}
	return &result
}

// formatAgentReturn renders a subagent's completion as a one-line summary, and
// inlines the subagent's transcript beneath it when expansion is on and the
// spawn site did not already inline it.
func (f *conversationFormatter) formatAgentReturn(result *agentToolUseResult) string {
	line := fmt.Sprintf("  < Agent %s (%s) %s", result.AgentID, orUnknown(result.AgentType), orUnknown(result.Status))
	var extras []string
	if result.TotalToolUseCount > 0 {
		extras = append(extras, fmt.Sprintf("%d tools", int(result.TotalToolUseCount)))
	}
	if result.TotalDurationMs > 0 {
		extras = append(extras, formatDurationMs(result.TotalDurationMs))
	}
	if result.TotalTokens > 0 {
		extras = append(extras, fmt.Sprintf("%d tok", int(result.TotalTokens)))
	}
	if len(extras) > 0 {
		line += " - " + strings.Join(extras, ", ")
	}

	if agent := f.byAgentID[result.AgentID]; agent != nil {
		f.claimed[agent.AgentID] = true
		if nested := f.expandAgent(agent); nested != "" {
			return line + "\n" + nested
		}
	}
	return line
}

// formatAssistantEntry formats an assistant record. Text blocks render as-is,
// tool_use blocks as one-line summaries, and thinking blocks only under
// Verbose.
func (f *conversationFormatter) formatAssistantEntry(record jsonlRecord) string {
	var msg apiMessage
	if err := json.Unmarshal(record.Message, &msg); err != nil {
		return ""
	}

	var blocks []contentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return ""
	}

	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		case "tool_use":
			parts = append(parts, f.formatToolUseBlock(b, record.Timestamp))
		case "thinking":
			if !f.opts.Verbose {
				continue
			}
			if thought := firstNonEmpty(b.Thinking, b.Text); thought != "" {
				parts = append(parts, "  ~ THINKING: "+indentContinuation(thought))
			}
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return formatRoleHeader("ASSISTANT", record.Timestamp) + "\n" + strings.Join(parts, "\n") + "\n"
}

// formatToolUseBlock renders a tool call, annotating a subagent spawn with the
// agent ID that names its transcript and, under expansion, inlining that
// transcript directly beneath the call.
func (f *conversationFormatter) formatToolUseBlock(b contentBlock, timestamp string) string {
	line := formatToolCall(b.Name, b.Input)

	if run := f.runByToolUseID[b.ID]; run != nil {
		f.claimedRuns[run.RunID] = true
		return line + "  " + workflowAnchor(run)
	}

	agent := f.resolveSpawnedAgent(b, timestamp)
	if agent == nil {
		return line
	}
	f.claimed[agent.AgentID] = true
	line += fmt.Sprintf("  [agent %s]", agent.AgentID)
	if nested := f.expandAgent(agent); nested != "" {
		return line + "\n" + nested
	}
	return line
}

// workflowAnchor renders the bracketed summary that follows a Workflow spawn:
// the run ID a reader needs to open it, its name, how many agents it spawned
// and how many of those never reported, its status and duration. Everything
// here comes from the manifest and the directory listing, not from the agent
// transcripts, so the anchor costs nothing per agent.
func workflowAnchor(run *WorkflowRun) string {
	parts := []string{fmt.Sprintf("%d agents", len(run.Agents)), run.Status}
	if run.DurationMs > 0 {
		parts = append(parts, formatDurationMs(float64(run.DurationMs)))
	}
	return fmt.Sprintf("[workflow %s (%s): %s]", run.RunID, run.DisplayName(), strings.Join(parts, ", "))
}

// resolveSpawnedAgent finds the transcript a spawn tool_use produced.
//
// The direct link is the sidecar's toolUseId, but only about half the subagent
// transcripts on disk carry one: a *named* agent (spawned with a name, the
// pattern behind background teammates) gets a sidecar with a name and no
// toolUseId, and its spawn's tool_result reports status "teammate_spawned"
// with no agentId either. For those the name is the only link, and a name can
// be reused across a session, so the candidate is disambiguated by start time:
// a subagent's transcript begins at or after the call that spawned it, and an
// already-claimed transcript is never handed to a second spawn site.
//
// A candidate that started BEFORE the call is never accepted: it belongs to an
// earlier spawn of the same name. Measured over every named spawn on this
// machine, a subagent's transcript begins between 0.0s and 948s after its call
// and never before it, so this rejects no legitimate match. When nothing is
// left, this returns nil rather than guessing, and the unlinked-agents trailer
// reports the transcript — a smaller error than attributing one agent's work to
// another agent's call.
//
// What it does NOT guarantee: if a spawn produced no transcript at all (the
// agent died before writing one), the next same-named transcript is later than
// that dead call and will be attributed to it. Distinguishing that case needs
// information the spawn site does not carry.
func (f *conversationFormatter) resolveSpawnedAgent(b contentBlock, timestamp string) *Transcript {
	if agent := f.byToolUseID[b.ID]; agent != nil {
		return agent
	}
	if b.Name != "Agent" && b.Name != "Task" {
		return nil
	}
	name := extractStringField(b.Input, "name")
	if name == "" {
		return nil
	}

	// Candidates are ordered by start time, so the first unclaimed one that did
	// not start before this call is the earliest match.
	for _, c := range f.byAgentName[name] {
		if f.claimed[c.AgentID] {
			continue
		}
		if timestamp != "" && c.StartedAt != "" && c.StartedAt < timestamp {
			continue
		}
		return c
	}
	return nil
}

// expandAgent renders a subagent's transcript indented for inlining, or ""
// when expansion is off, the agent is unknown, it was already inlined, or the
// nesting bound has been reached. Marking the agent expanded before recursing
// makes a cyclic parent chain terminate.
func (f *conversationFormatter) expandAgent(agent *Transcript) string {
	if agent == nil || !f.opts.ExpandAgents || f.inlined[agent.AgentID] {
		return ""
	}
	if f.depth >= maxExpansionDepth {
		return nestedIndent + fmt.Sprintf("[agent %s not expanded: nesting limit %d reached]\n", agent.AgentID, maxExpansionDepth)
	}
	if budget := f.opts.MaxExpandBytes; budget > 0 && *f.expandedBytes+agent.Bytes > budget {
		f.claimed[agent.AgentID] = true
		return nestedIndent + fmt.Sprintf("[agent %s not expanded: %s would exceed the %s expansion budget; raise --max-expand-mb, or print it alone with --agent %s]\n",
			agent.AgentID, formatByteCount(agent.Bytes), formatByteCount(budget), agent.AgentID)
	}
	*f.expandedBytes += agent.Bytes
	f.claimed[agent.AgentID] = true
	f.inlined[agent.AgentID] = true

	nested := &conversationFormatter{
		opts:          FormatOptions{Verbose: f.opts.Verbose, ExpandAgents: true, Root: f.opts.Root, MaxExpandBytes: f.opts.MaxExpandBytes},
		target:        agent,
		byToolUseID:   f.byToolUseID,
		byAgentID:     f.byAgentID,
		byAgentName:   f.byAgentName,
		byTaskID:      f.byTaskID,
		claimed:       f.claimed,
		claimedRuns:   f.claimedRuns,
		inlined:       f.inlined,
		expandedBytes: f.expandedBytes,
		depth:         f.depth + 1,
	}

	var buf bytes.Buffer
	// A subagent transcript is always rendered whole: a tail of a subagent
	// omits the prompt that gives its output meaning.
	if err := nested.render(agent.Filepath, 0, &buf); err != nil {
		return indentBlock(fmt.Sprintf("[agent %s transcript unreadable: %v]\n", agent.AgentID, err), nestedIndent)
	}

	label := agent.Meta.DisplayLabel()
	head := fmt.Sprintf("--- begin agent %s (%s)", agent.AgentID, agent.Meta.DisplayType())
	if label != "" {
		head += fmt.Sprintf(": %s", truncate(label, maxToolParamLen))
	}
	head += " ---\n"
	tail := fmt.Sprintf("--- end agent %s ---\n", agent.AgentID)

	body := buf.String()
	if body == "" {
		body = "(no conversation messages)\n"
	}
	return indentBlock(head+body+tail, nestedIndent)
}

// scanTranscriptFile makes one pass over a JSONL transcript, returning the last
// n lines (or all of them when n <= 0) together with the file's fork reference.
//
// The fork reference is collected over the WHOLE file rather than over the
// returned window. Fork provenance occupies a contiguous prefix of a forked
// transcript — on one real file, records 1 through 2775 of 6049 — so scanning
// only a tail loses it. `session print` tails by default, which is exactly the
// invocation where a silent omission is least likely to be noticed.
//
// The same pass collects, for every Workflow spawn, the task ID its tool_result
// reported ("Task ID: w9gxz79on"), keyed by tool_use ID. That result record is
// the only place the spawn's tool_use ID and the run's task ID appear together,
// and the run's manifest carries the same task ID, which is how a Workflow call
// is linked to the run directory it produced.
func scanTranscriptFile(jsonlFilepath string, n int) ([]string, *forkRef, map[string]string, error) {
	var fork *forkRef
	taskIDs := map[string]string{}
	noteTaskID := func(line []byte) {
		if !bytes.Contains(line, []byte(workflowTaskIDMarker)) {
			return
		}
		var record struct {
			Type    string          `json:"type"`
			Message json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(line, &record); err != nil || record.Type != "user" {
			return
		}
		for _, b := range decodeContentBlocks(record.Message) {
			if b.Type != "tool_result" || b.ToolUseID == "" {
				continue
			}
			if id := workflowTaskIDPattern.FindStringSubmatch(extractToolResultText(b)); id != nil {
				taskIDs[b.ToolUseID] = id[1]
			}
		}
	}
	noteFork := func(line []byte) {
		noteTaskID(line)
		if fork != nil {
			return
		}
		var record struct {
			ForkedFrom *forkRef `json:"forkedFrom"`
		}
		if err := json.Unmarshal(line, &record); err != nil {
			return
		}
		if record.ForkedFrom != nil && record.ForkedFrom.SessionID != "" {
			fork = record.ForkedFrom
		}
	}

	if n <= 0 {
		var lines []string
		err := ScanJSONLLines(jsonlFilepath, func(line []byte) error {
			noteFork(line)
			lines = append(lines, string(line))
			return nil
		})
		if err != nil {
			return nil, nil, nil, err
		}
		return lines, fork, taskIDs, nil
	}

	// The ring grows to the requested size rather than being allocated up
	// front, so a mistyped --tail cannot allocate gigabytes for a small file.
	ring := make([]string, 0, ringSeedCapacity(n))
	total := 0
	err := ScanJSONLLines(jsonlFilepath, func(line []byte) error {
		noteFork(line)
		if len(ring) < n {
			ring = append(ring, string(line))
		} else {
			ring[total%n] = string(line)
		}
		total++
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}

	count := total
	if count > n {
		count = n
	}
	startIdx := total - count
	result := make([]string, count)
	for i := 0; i < count; i++ {
		result[i] = ring[(startIdx+i)%n]
	}
	return result, fork, taskIDs, nil
}

// workflowTaskIDMarker is the substring a Workflow spawn's tool_result carries
// before the task ID; workflowTaskIDPattern extracts the ID itself.
const workflowTaskIDMarker = "Task ID: "

var workflowTaskIDPattern = regexp.MustCompile(`Task ID: ([A-Za-z0-9_-]+)`)

// maxRingSeedCapacity bounds the up-front allocation of a tail buffer. The ring
// still grows to whatever --tail asked for, but only as lines actually arrive,
// so the memory a request can reserve is bounded by the file, not by the flag.
const maxRingSeedCapacity = 1024

// ringSeedCapacity returns the initial capacity for a tail ring of size n.
func ringSeedCapacity(n int) int {
	if n < maxRingSeedCapacity {
		return n
	}
	return maxRingSeedCapacity
}

// formatRoleHeader builds the header line for a conversation entry, e.g.
// "[2026-03-16T16:06:30.476Z USER]". If the timestamp is empty, falls back
// to just "[USER]".
func formatRoleHeader(role string, timestamp string) string {
	if timestamp == "" {
		return "[" + role + "]"
	}
	return "[" + timestamp + " " + role + "]"
}

// extractToolResultText extracts the text body of a tool_result content block.
// The content field is either a plain string or an array of content blocks.
func extractToolResultText(b contentBlock) string {
	var textContent string
	if err := json.Unmarshal(b.Content, &textContent); err == nil {
		return textContent
	}

	var innerBlocks []contentBlock
	if err := json.Unmarshal(b.Content, &innerBlocks); err != nil {
		return ""
	}

	var texts []string
	for _, inner := range innerBlocks {
		if inner.Type == "text" && inner.Text != "" {
			texts = append(texts, inner.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// formatToolCall produces a one-line summary of a tool invocation, e.g.:
//
//	> Bash("ls -la")
//	> Grep("pattern", path="/some/dir")
//
// For MCP tools (names containing "__"), it attempts to use the first
// string-valued field. For unknown tools, it returns just the tool name.
func formatToolCall(toolName string, input map[string]interface{}) string {
	spec, known := toolParamMap[toolName]
	if !known {
		// MCP tools have double underscores in their name.
		if strings.Contains(toolName, "__") {
			return formatMCPToolCall(toolName, input)
		}
		return "  > " + toolName + "()"
	}

	primaryVal := extractStringField(input, spec.primary)
	if primaryVal == "" {
		return "  > " + toolName + "()"
	}

	primaryVal = truncate(primaryVal, maxToolParamLen)
	if spec.secondary == "" {
		return fmt.Sprintf("  > %s(%q)", toolName, primaryVal)
	}

	secondaryVal := extractStringField(input, spec.secondary)
	if secondaryVal == "" {
		return fmt.Sprintf("  > %s(%q)", toolName, primaryVal)
	}

	secondaryVal = truncate(secondaryVal, maxToolParamLen)
	return fmt.Sprintf("  > %s(%q, %s=%q)", toolName, primaryVal, spec.secondary, secondaryVal)
}

// formatMCPToolCall formats a tool call for an MCP tool by using the first
// string-valued field (in alphabetical key order) from the input map.
func formatMCPToolCall(toolName string, input map[string]interface{}) string {
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if strVal, ok := input[k].(string); ok && strVal != "" {
			return fmt.Sprintf("  > %s(%q)", toolName, truncate(strVal, maxToolParamLen))
		}
	}
	return "  > " + toolName + "()"
}

// extractStringField extracts a string value from a map, handling string,
// json.Number, and float64 types.
func extractStringField(m map[string]interface{}, key string) string {
	val, ok := m[key]
	if !ok {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return fmt.Sprintf("%g", v)
	default:
		return ""
	}
}

// truncate shortens a string to maxLen characters, appending "..." if it
// was truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// indentBlock prefixes every non-empty line of a block with the given indent.
func indentBlock(block string, indent string) string {
	if block == "" {
		return ""
	}
	lines := strings.Split(strings.TrimSuffix(block, "\n"), "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// indentContinuation aligns the second and later lines of a multi-line value
// under the marker that introduces it, so a wrapped body cannot be mistaken for
// a new transcript entry.
func indentContinuation(s string) string {
	return strings.ReplaceAll(s, "\n", "\n    ")
}

// formatByteCount renders a byte count for a notice: whole bytes below 1 KB,
// one decimal above.
func formatByteCount(n int64) string {
	const kb, mb, gb = 1024.0, 1024.0 * 1024.0, 1024.0 * 1024.0 * 1024.0
	f := float64(n)
	switch {
	case f >= gb:
		return fmt.Sprintf("%.1f GB", f/gb)
	case f >= mb:
		return fmt.Sprintf("%.1f MB", f/mb)
	case f >= kb:
		return fmt.Sprintf("%.1f KB", f/kb)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// formatDurationMs renders a millisecond duration compactly: "820ms", "12.4s",
// "3m12s".
func formatDurationMs(ms float64) string {
	switch {
	case ms < 1000:
		return fmt.Sprintf("%dms", int(ms))
	case ms < 60000:
		return fmt.Sprintf("%.1fs", ms/1000)
	default:
		total := int(ms / 1000)
		return fmt.Sprintf("%dm%02ds", total/60, total%60)
	}
}

// orUnknown substitutes a placeholder for an empty field so a rendered line
// never has a hole in it.
func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// firstNonEmpty returns the first non-empty argument, or "".
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
