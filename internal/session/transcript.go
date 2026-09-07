package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A Claude session is not a single file. The main conversation lives at
// <project>/<session-id>.jsonl, but every subagent the session spawns gets its
// own transcript under <project>/<session-id>/subagents/, written as a pair:
//
//	agent-<agent-id>.jsonl       the subagent's conversation
//	agent-<agent-id>.meta.json   who spawned it, what type, how deep
//
// Subagents nest — a subagent can spawn subagents — but the directory stays
// flat: every descendant of the session, at any depth, is a sibling file in
// that one directory. The tree is reconstructed from each meta file's
// parentAgentId, not from the filesystem layout.

const (
	// subagentsDirname is the directory under <project>/<session-id>/ holding
	// subagent transcripts.
	subagentsDirname = "subagents"

	// agentFilePrefix and agentFileSuffix bracket the agent ID in a subagent
	// transcript filename: agent-<agent-id>.jsonl
	agentFilePrefix = "agent-"
	agentFileSuffix = ".jsonl"

	// agentMetaSuffix replaces agentFileSuffix to locate a subagent's sidecar
	// metadata file: agent-<agent-id>.meta.json
	agentMetaSuffix = ".meta.json"

	// startTimestampScanLimit caps how many leading JSONL records are examined
	// when looking for a transcript's first timestamp. Discovery orders
	// subagents chronologically, and the first record almost always carries a
	// timestamp; the limit keeps discovery from reading whole files.
	startTimestampScanLimit = 8

	// maxExpansionDepth bounds recursive subagent expansion. Real spawn depths
	// observed on disk top out around 5; the bound exists so that a corrupted
	// or cyclic parentAgentId chain cannot produce unbounded output.
	maxExpansionDepth = 12
)

// errStopScanning halts ScanJSONLLines once a callback has what it needs.
// Callers identify it with errors.Is and treat it as success.
var errStopScanning = errors.New("stop scanning")

// AgentMeta mirrors the fields of a subagent's agent-<id>.meta.json sidecar.
// Every field is optional, and on a long-lived machine most of them are absent:
// of 7053 subagent transcripts, 3.6% have no sidecar at all, only 31% of the
// sidecars name a parentAgentId, and only 8.4% of named agents carry a
// toolUseId. Discovery therefore never requires a sidecar — a subagent with no
// metadata still appears in the tree, attached to the session root.
type AgentMeta struct {
	AgentType       string `json:"agentType"`
	CustomAgentType string `json:"customAgentType"`
	Description     string `json:"description"`
	Name            string `json:"name"`
	ToolUseID       string `json:"toolUseId"`
	ParentAgentID   string `json:"parentAgentId"`
	SpawnDepth      *int   `json:"spawnDepth"`
	Model           string `json:"model"`
	TaskKind        string `json:"taskKind"`
	TeamName        string `json:"teamName"`
	PermissionMode  string `json:"permissionMode"`
	IsFork          bool   `json:"isFork"`
	StoppedByUser   bool   `json:"stoppedByUser"`
}

// DisplayType returns the most specific agent-type label available, preferring
// a custom agent type over the built-in one and falling back to "unknown" when
// no sidecar was found.
//
// A forked agent is marked, because it did not start from a fresh context: it
// inherited its spawner's entire conversation, so its transcript begins mid
// thought and reads as though records are missing when nothing is.
func (m AgentMeta) DisplayType() string {
	base := "unknown"
	switch {
	case m.CustomAgentType != "":
		base = m.CustomAgentType
	case m.AgentType != "":
		base = m.AgentType
	}
	if m.IsFork {
		return base + " (fork)"
	}
	return base
}

// DisplayLabel returns the human-facing one-line label for an agent: its
// spawn-time name if it was given one, otherwise the description the spawner
// wrote, otherwise an empty string.
func (m AgentMeta) DisplayLabel() string {
	if m.Name != "" {
		return m.Name
	}
	return m.Description
}

// Transcript is a single JSONL conversation log belonging to a session: either
// the session's own main transcript (AgentID == "") or one subagent's.
type Transcript struct {
	// AgentID is the subagent identifier parsed from the filename, or "" for
	// the session's main transcript.
	AgentID string

	// Filepath is the absolute path to the JSONL file.
	Filepath string

	// Meta is the sidecar metadata; the zero value when MetaFound is false.
	Meta AgentMeta

	// MetaFound reports whether an agent-<id>.meta.json sidecar was read.
	MetaFound bool

	// Depth is the distance from the session's main transcript in the
	// reconstructed tree — 0 for the main transcript, 1 for a subagent it
	// spawned directly, and so on. It is derived from the tree, not copied
	// from the sidecar's spawnDepth, so it stays correct when a sidecar is
	// missing or its parent's transcript was never written.
	Depth int

	// StartedAt is the timestamp of the transcript's first timestamped record,
	// or "" if none of the leading records carried one. Used to order siblings.
	StartedAt string

	// Orphaned reports that the sidecar named a parentAgentId with no matching
	// transcript on disk, so this node was attached to the session root
	// instead of to its real parent.
	Orphaned bool

	// Children are the subagents this transcript spawned, ordered by StartedAt.
	Children []*Transcript
}

// IsMain reports whether this is the session's own transcript rather than a
// subagent's.
func (t *Transcript) IsMain() bool { return t.AgentID == "" }

// Walk invokes fn on this transcript and every descendant, depth-first in
// child order. It is cycle-safe: a node is visited at most once.
func (t *Transcript) Walk(fn func(*Transcript)) {
	visited := map[*Transcript]bool{}
	var walk func(*Transcript)
	walk = func(n *Transcript) {
		if n == nil || visited[n] {
			return
		}
		visited[n] = true
		fn(n)
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(t)
}

// Flatten returns this transcript and every descendant in depth-first order.
func (t *Transcript) Flatten() []*Transcript {
	var out []*Transcript
	t.Walk(func(n *Transcript) { out = append(out, n) })
	return out
}

// SessionSubagentsDirpath returns the directory holding a session's subagent
// transcripts. The directory does not necessarily exist — a session that never
// spawned a subagent has none.
func SessionSubagentsDirpath(projectDirpath string, sessionID string) string {
	return filepath.Join(projectDirpath, sessionID, subagentsDirname)
}

// DiscoverSessionTranscripts builds the full transcript tree for a session: the
// main JSONL plus every subagent transcript, linked into a tree by each
// subagent's parentAgentId.
//
// The main transcript must exist; a missing one is an error. Everything below
// it is best-effort by design — a missing subagents directory, an unreadable
// sidecar, a parentAgentId naming an agent whose transcript was never written,
// or a cycle in the parent chain each degrade to "attach to the session root"
// rather than dropping the transcript. A transcript that exists on disk is
// always reachable in the returned tree.
func DiscoverSessionTranscripts(projectDirpath string, sessionID string) (*Transcript, error) {
	mainFilepath := filepath.Join(projectDirpath, sessionID+agentFileSuffix)
	if _, err := os.Stat(mainFilepath); err != nil {
		return nil, fmt.Errorf("no transcript for session '%s' at '%s': %w", sessionID, mainFilepath, err)
	}

	root := &Transcript{
		Filepath:  mainFilepath,
		StartedAt: readStartTimestamp(mainFilepath),
	}

	nodes := discoverSubagentTranscripts(SessionSubagentsDirpath(projectDirpath, sessionID))
	linkTranscriptTree(root, nodes)
	return root, nil
}

// DiscoverTranscriptsForFile builds the transcript tree for an arbitrary main
// JSONL path, deriving the session's sidecar directory from the filename. It is
// the entry point for callers that already hold a path (mission print resolves
// the active JSONL by modification time rather than by session ID).
func DiscoverTranscriptsForFile(jsonlFilepath string) (*Transcript, error) {
	dir := filepath.Dir(jsonlFilepath)
	sessionID := strings.TrimSuffix(filepath.Base(jsonlFilepath), agentFileSuffix)
	return DiscoverSessionTranscripts(dir, sessionID)
}

// discoverSubagentTranscripts reads every agent-<id>.jsonl in a subagents
// directory, pairing each with its sidecar metadata when present. Returns nil
// when the directory does not exist or cannot be read.
func discoverSubagentTranscripts(subagentsDirpath string) []*Transcript {
	entries, err := os.ReadDir(subagentsDirpath)
	if err != nil {
		return nil
	}

	var nodes []*Transcript
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, agentFilePrefix) || !strings.HasSuffix(name, agentFileSuffix) {
			continue
		}
		agentID := strings.TrimSuffix(strings.TrimPrefix(name, agentFilePrefix), agentFileSuffix)
		if agentID == "" {
			continue
		}

		transcriptFilepath := filepath.Join(subagentsDirpath, name)
		meta, metaFound := readAgentMeta(filepath.Join(subagentsDirpath, agentFilePrefix+agentID+agentMetaSuffix))

		nodes = append(nodes, &Transcript{
			AgentID:   agentID,
			Filepath:  transcriptFilepath,
			Meta:      meta,
			MetaFound: metaFound,
			StartedAt: readStartTimestamp(transcriptFilepath),
		})
	}
	return nodes
}

// readAgentMeta parses a subagent's sidecar metadata file. A missing or
// malformed sidecar yields the zero AgentMeta and false — never an error,
// because a subagent with no readable metadata must still appear in the tree.
func readAgentMeta(metaFilepath string) (AgentMeta, bool) {
	data, err := os.ReadFile(metaFilepath)
	if err != nil {
		return AgentMeta{}, false
	}
	var meta AgentMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return AgentMeta{}, false
	}
	return meta, true
}

// linkTranscriptTree attaches each discovered subagent to its parent, filling
// in Depth and Orphaned. Any node whose parent is unknown, self-referential, or
// part of a cycle is attached to the root so that it is still reachable.
func linkTranscriptTree(root *Transcript, nodes []*Transcript) {
	byID := make(map[string]*Transcript, len(nodes))
	for _, n := range nodes {
		byID[n.AgentID] = n
	}

	for _, n := range nodes {
		parentID := n.Meta.ParentAgentID
		if parentID == "" || parentID == n.AgentID {
			root.Children = append(root.Children, n)
			continue
		}
		parent, ok := byID[parentID]
		if !ok {
			// The sidecar names a spawner whose transcript is not on disk
			// (deleted, or spawned from a different session). Keep the
			// transcript reachable and record why it moved.
			n.Orphaned = true
			root.Children = append(root.Children, n)
			continue
		}
		if createsParentCycle(byID, n.AgentID, parentID) {
			n.Orphaned = true
			root.Children = append(root.Children, n)
			continue
		}
		parent.Children = append(parent.Children, n)
	}

	assignDepthsAndSort(root, 0)
}

// createsParentCycle reports whether making parentID the parent of agentID
// would close a cycle, by walking the existing parent chain upward from
// parentID looking for agentID. The walk is bounded by the node count so a
// pre-existing cycle among other nodes cannot hang it.
func createsParentCycle(byID map[string]*Transcript, agentID string, parentID string) bool {
	for i := 0; i <= len(byID); i++ {
		if parentID == "" || parentID == agentID {
			return parentID == agentID
		}
		next, ok := byID[parentID]
		if !ok {
			return false
		}
		parentID = next.Meta.ParentAgentID
	}
	return true
}

// assignDepthsAndSort walks the linked tree setting Depth from the tree shape
// and ordering each node's children chronologically. Sibling order falls back
// to agent ID so the output is deterministic when timestamps are missing or
// identical.
func assignDepthsAndSort(node *Transcript, depth int) {
	node.Depth = depth
	sort.SliceStable(node.Children, func(i, j int) bool {
		a, b := node.Children[i], node.Children[j]
		if a.StartedAt != b.StartedAt {
			return a.StartedAt < b.StartedAt
		}
		return a.AgentID < b.AgentID
	})
	for _, child := range node.Children {
		assignDepthsAndSort(child, depth+1)
	}
}

// readStartTimestamp returns the timestamp of the first timestamped record in a
// JSONL file, or "" if none of the leading records carry one.
func readStartTimestamp(jsonlFilepath string) string {
	found := ""
	seen := 0
	err := ScanJSONLLines(jsonlFilepath, func(line []byte) error {
		seen++
		var record struct {
			Timestamp string `json:"timestamp"`
		}
		if err := json.Unmarshal(line, &record); err == nil && record.Timestamp != "" {
			found = record.Timestamp
			return errStopScanning
		}
		if seen >= startTimestampScanLimit {
			return errStopScanning
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopScanning) {
		return ""
	}
	return found
}

// TranscriptStats summarizes one transcript's contents. Producing it requires a
// full pass over the file, so it is computed on demand rather than during
// discovery.
type TranscriptStats struct {
	UserMessages      int
	AssistantMessages int
	ToolCalls         int
	ToolErrors        int
	CompactBoundaries int
	FirstTimestamp    string
	LastTimestamp     string
}

// SummarizeTranscript scans a JSONL transcript and counts its conversation
// records. A file that cannot be read yields zeroed stats and an error; callers
// rendering a table should show the row anyway.
//
// Records repeated under the same uuid are counted once. Real transcripts hold
// a great many of them — on one 68 MB session on disk, counting every line
// reports 10011 messages against a true 4663 — so counting lines rather than
// records overstates a session's size by more than 100%.
func SummarizeTranscript(jsonlFilepath string) (TranscriptStats, error) {
	var stats TranscriptStats
	seenUUIDs := map[string]bool{}
	err := ScanJSONLLines(jsonlFilepath, func(line []byte) error {
		var record struct {
			Type      string          `json:"type"`
			Subtype   string          `json:"subtype"`
			Timestamp string          `json:"timestamp"`
			UUID      string          `json:"uuid"`
			Message   json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(line, &record); err != nil {
			return nil
		}
		// A record with no uuid (queue operations, titles, mode switches) has
		// nothing to identify it, so it is always counted.
		if record.UUID != "" {
			if seenUUIDs[record.UUID] {
				return nil
			}
			seenUUIDs[record.UUID] = true
		}
		if record.Timestamp != "" {
			if stats.FirstTimestamp == "" {
				stats.FirstTimestamp = record.Timestamp
			}
			stats.LastTimestamp = record.Timestamp
		}

		switch record.Type {
		case "user":
			stats.UserMessages++
			for _, b := range decodeContentBlocks(record.Message) {
				if b.Type == "tool_result" && b.IsError {
					stats.ToolErrors++
				}
			}
		case "assistant":
			stats.AssistantMessages++
			for _, b := range decodeContentBlocks(record.Message) {
				if b.Type == "tool_use" {
					stats.ToolCalls++
				}
			}
		case "system":
			if record.Subtype == "compact_boundary" {
				stats.CompactBoundaries++
			}
		}
		return nil
	})
	if err != nil {
		return TranscriptStats{}, err
	}
	return stats, nil
}

// decodeContentBlocks extracts the content blocks from a raw message, returning
// nil for string-valued or malformed content.
func decodeContentBlocks(rawMessage json.RawMessage) []contentBlock {
	if len(rawMessage) == 0 {
		return nil
	}
	var msg apiMessage
	if err := json.Unmarshal(rawMessage, &msg); err != nil {
		return nil
	}
	var blocks []contentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return nil
	}
	return blocks
}

// ErrAgentNotFound is returned by ResolveAgent when no subagent in the tree
// matches the requested ID prefix.
var ErrAgentNotFound = errors.New("no matching subagent transcript")

// ResolveAgent finds the single subagent in a transcript tree whose agent ID
// starts with the given prefix. An exact match always wins over prefix matches.
// An ambiguous prefix is an error naming every candidate, so the caller can
// print something actionable rather than picking arbitrarily.
func ResolveAgent(root *Transcript, prefix string) (*Transcript, error) {
	if prefix == "" {
		return nil, fmt.Errorf("%w: empty agent ID", ErrAgentNotFound)
	}

	var exact *Transcript
	var matches []*Transcript
	root.Walk(func(n *Transcript) {
		if n.IsMain() || !strings.HasPrefix(n.AgentID, prefix) {
			return
		}
		if n.AgentID == prefix && exact == nil {
			exact = n
		}
		matches = append(matches, n)
	})

	if exact != nil {
		return exact, nil
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("%w for ID prefix '%s'", ErrAgentNotFound, prefix)
	case 1:
		return matches[0], nil
	default:
		ids := make([]string, 0, len(matches))
		for _, m := range matches {
			ids = append(ids, m.AgentID)
		}
		sort.Strings(ids)
		return nil, fmt.Errorf("agent ID prefix '%s' is ambiguous, matches: %s", prefix, strings.Join(ids, ", "))
	}
}
