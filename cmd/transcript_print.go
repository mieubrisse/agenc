package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/session"
	"github.com/odyssey/agenc/internal/tableprinter"
)

// A Claude session's transcript is a tree, not a file. The main conversation
// lives in one JSONL; every subagent it spawns writes its own, and those
// subagents spawn subagents. `session print` and `mission print` share the
// logic below so that both reach the whole tree and render it identically.

const (
	// emptySessionMessage is shown when a transcript exists but contains no
	// renderable content (e.g., only metadata entries from a freshly-spawned
	// mission that has not yet produced user/assistant messages).
	emptySessionMessage = "(session has no conversation messages yet)\n"

	// textFormat and jsonlFormat are the accepted --format values.
	textFormat  = "text"
	jsonlFormat = "jsonl"

	// mainTranscriptLabel names the session's own transcript in the agent
	// tree listing, where every other row is a subagent ID.
	mainTranscriptLabel = "(main)"

	// agentTreeIndent prefixes an agent's ID once per level of nesting so the
	// listing reads as a tree. The ID itself stays a whole whitespace-delimited
	// word, so the column is still greppable.
	agentTreeIndent = "  "

	// maxAgentLabelLen caps the description column in the agent tree listing.
	maxAgentLabelLen = 60
)

// countingWriter wraps an io.Writer and tracks the number of bytes written. It
// lets the print path detect that a renderer produced no output without
// buffering a potentially huge transcript in memory.
type countingWriter struct {
	w     io.Writer
	count int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.count += int64(n)
	return n, err
}

// transcriptPrintOptions holds the flags shared by `session print` and
// `mission print`.
type transcriptPrintOptions struct {
	// tailLines, when > 0, limits output to the last N JSONL records.
	tailLines int

	// all prints the entire transcript, overriding tailLines.
	all bool

	// format is textFormat or jsonlFormat.
	format string

	// listAgents prints the session's subagent tree instead of a transcript.
	listAgents bool

	// agentID selects a subagent's transcript by ID or unique ID prefix.
	agentID string

	// expandAgents inlines subagent transcripts at their spawn sites.
	expandAgents bool

	// verbose renders every record class, including hooks, turn timings,
	// attachments, thinking blocks and successful tool results.
	verbose bool
}

// validate rejects flag combinations that have no coherent meaning, before any
// file is read, so the user gets the error rather than partial output.
func (o transcriptPrintOptions) validate() error {
	if o.format != textFormat && o.format != jsonlFormat {
		return stacktrace.NewError("invalid --%s %q: must be %q or %q", formatFlagName, o.format, textFormat, jsonlFormat)
	}
	if !o.all && o.tailLines < 0 {
		return stacktrace.NewError("--%s value must be positive", tailFlagName)
	}
	if o.listAgents && o.agentID != "" {
		return stacktrace.NewError("--%s and --%s are mutually exclusive", agentsFlagName, agentFlagName)
	}
	if o.listAgents && o.expandAgents {
		return stacktrace.NewError("--%s and --%s are mutually exclusive", agentsFlagName, expandAgentsFlagName)
	}
	return nil
}

// formatOptions converts the CLI flags into renderer options.
func (o transcriptPrintOptions) formatOptions(root *session.Transcript) session.FormatOptions {
	tail := o.tailLines
	if o.all {
		tail = 0
	}
	return session.FormatOptions{
		TailLines:    tail,
		Verbose:      o.verbose,
		ExpandAgents: o.expandAgents,
		Root:         root,
	}
}

// printTranscript renders a session's transcript to stdout under the given
// options.
func printTranscript(jsonlFilepath string, opts transcriptPrintOptions) error {
	return printTranscriptTo(jsonlFilepath, opts, os.Stdout, os.Stderr)
}

// printTranscriptTo is the testable core of printTranscript with explicit
// writers.
//
// A transcript may exist but contain only metadata entries (file-history
// snapshots, mode records) when a freshly-spawned mission has not yet produced
// any conversation. The renderer writes nothing in that case; rather than
// return silently, this reports the reason on stderr so a caller piping stdout
// still sees why it got nothing.
func printTranscriptTo(jsonlFilepath string, opts transcriptPrintOptions, stdout io.Writer, stderr io.Writer) error {
	if err := opts.validate(); err != nil {
		return err
	}

	root, err := session.DiscoverTranscriptsForFile(jsonlFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to discover transcripts for '%s'", jsonlFilepath)
	}

	if opts.listAgents {
		return printAgentTree(root, stdout, stderr)
	}

	target := root
	if opts.agentID != "" {
		target, err = session.ResolveAgent(root, opts.agentID)
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve --%s '%s'", agentFlagName, opts.agentID)
		}
	}

	cw := &countingWriter{w: stdout}
	switch opts.format {
	case textFormat:
		if err := session.FormatTranscript(target, opts.formatOptions(root), cw); err != nil {
			return stacktrace.Propagate(err, "")
		}
	case jsonlFormat:
		if err := writeRawJSONL(target, opts, cw); err != nil {
			return stacktrace.Propagate(err, "")
		}
	}

	if cw.count == 0 {
		fmt.Fprint(stderr, emptySessionMessage)
	}
	writeAgentHint(target, opts, stderr)
	return nil
}

// writeRawJSONL emits the raw JSONL for the selected transcript. Under
// --expand-agents it concatenates the selected transcript and every descendant;
// the records are self-identifying (each carries sessionId and, for subagents,
// agentId), so a concatenated stream stays attributable.
func writeRawJSONL(target *session.Transcript, opts transcriptPrintOptions, w io.Writer) error {
	tail := opts.tailLines
	if opts.all {
		tail = 0
	}

	if _, err := session.TailJSONLFile(target.Filepath, tail, w); err != nil {
		return err
	}
	if !opts.expandAgents {
		return nil
	}

	for _, node := range target.Flatten() {
		if node == target {
			continue
		}
		// Subagent transcripts are emitted whole: a tail of a subagent omits
		// the prompt that gives its output meaning.
		if _, err := session.TailJSONLFile(node.Filepath, 0, w); err != nil {
			return err
		}
	}
	return nil
}

// writeAgentHint tells the caller that subagent transcripts exist but were not
// printed. Without it, a `--all` render looks complete while omitting every
// subagent — which is exactly the blindness these flags exist to remove.
func writeAgentHint(target *session.Transcript, opts transcriptPrintOptions, stderr io.Writer) {
	if opts.expandAgents || opts.agentID != "" {
		return
	}
	hidden := len(target.Flatten()) - 1
	if hidden <= 0 {
		return
	}
	fmt.Fprintf(stderr,
		"(%d subagent transcript(s) not shown - --%s to list them, --%s <id> to print one, --%s to inline them)\n",
		hidden, agentsFlagName, agentFlagName, expandAgentsFlagName)
}

// printAgentTree lists the session's transcript tree: the main conversation and
// every subagent below it, indented by nesting depth.
func printAgentTree(root *session.Transcript, stdout io.Writer, stderr io.Writer) error {
	nodes := root.Flatten()
	if len(nodes) <= 1 {
		fmt.Fprintln(stdout, "No subagent transcripts for this session.")
		return nil
	}

	tbl := tableprinter.NewTable("AGENT", "TYPE", "MODEL", "MSGS", "TOOLS", "ERR", "STARTED", "LABEL").WithWriter(stdout)
	for _, node := range nodes {
		stats, err := session.SummarizeTranscript(node.Filepath)
		if err != nil {
			// A transcript that cannot be read still belongs in the listing —
			// its absence from the table would read as "this agent does not
			// exist", which is a different and wrong claim.
			fmt.Fprintf(stderr, "warning: could not read transcript '%s': %v\n", node.Filepath, err)
		}

		tbl.AddRow(
			agentTreeCell(node),
			agentTypeCell(node),
			orDash(node.Meta.Model),
			fmt.Sprintf("%d", stats.UserMessages+stats.AssistantMessages),
			fmt.Sprintf("%d", stats.ToolCalls),
			fmt.Sprintf("%d", stats.ToolErrors),
			formatTranscriptTimestamp(node.StartedAt),
			truncatePrompt(agentLabelCell(node), maxAgentLabelLen),
		)
	}
	tbl.Print()
	return nil
}

// agentTreeCell renders the AGENT column: the main transcript's placeholder, or
// a subagent ID indented once per level of nesting.
func agentTreeCell(node *session.Transcript) string {
	if node.IsMain() {
		return mainTranscriptLabel
	}
	return strings.Repeat(agentTreeIndent, node.Depth-1) + node.AgentID
}

// agentTypeCell renders the TYPE column, flagging the two states that explain
// an otherwise confusing tree: a subagent whose parent transcript is missing,
// and one whose sidecar metadata was never written.
func agentTypeCell(node *session.Transcript) string {
	if node.IsMain() {
		return "session"
	}
	suffix := ""
	if node.Orphaned {
		suffix = " (orphaned)"
	} else if !node.MetaFound {
		suffix = " (no metadata)"
	}
	return node.Meta.DisplayType() + suffix
}

// agentLabelCell renders the LABEL column: the agent's name or the description
// its spawner wrote.
func agentLabelCell(node *session.Transcript) string {
	if node.IsMain() {
		return ""
	}
	return node.Meta.DisplayLabel()
}

// formatTranscriptTimestamp renders a JSONL ISO-8601 timestamp in local time,
// falling back to the raw value when it does not parse.
func formatTranscriptTimestamp(timestamp string) string {
	if timestamp == "" {
		return "-"
	}
	parsed, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return timestamp
	}
	return parsed.Local().Format("2006-01-02 15:04")
}

// orDash substitutes a placeholder for an empty table cell.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// registerTranscriptPrintFlags adds the transcript-tree flags shared by
// `session print` and `mission print` to a command.
func registerTranscriptPrintFlags(cmd *cobra.Command, opts *transcriptPrintOptions) {
	flags := cmd.Flags()
	flags.BoolVar(&opts.listAgents, agentsFlagName, false, "list the session's subagent transcripts instead of printing a conversation")
	flags.StringVar(&opts.agentID, agentFlagName, "", "print a subagent's transcript, by agent ID or unique ID prefix")
	flags.BoolVar(&opts.expandAgents, expandAgentsFlagName, false, "inline each subagent's transcript at the point it was spawned")
	flags.BoolVar(&opts.verbose, verboseFlagName, false, "render every record class: hooks, turn timings, attachments, thinking, successful tool results")
}
