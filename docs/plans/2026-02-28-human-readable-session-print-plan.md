Human-Readable Session Print Implementation Plan
==================================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `agenc mission print` and `agenc session print` default to a human-readable conversation view, with raw JSONL available via `--format=jsonl`.

**Architecture:** Add a `FormatConversation()` function in `internal/session/format.go` that parses JSONL entries and writes a human-readable conversation view. The CLI commands gain a `--format` flag that dispatches between the new formatter (default) and the existing raw JSONL output.

**Tech Stack:** Go stdlib (`encoding/json`, `bufio`, `io`, `fmt`, `strings`). No new dependencies.

**Design doc:** `docs/plans/2026-02-28-human-readable-session-print-design.md`

---

### Task 1: Add format flag constant

**Files:**
- Modify: `cmd/command_str_consts.go:100-101`

**Step 1: Add the constant**

Add `formatFlagName` to the session/mission print flags section:

```go
	// session/mission print flags
	tailFlagName   = "tail"
	formatFlagName = "format"
```

**Step 2: Commit**

```bash
git add cmd/command_str_consts.go
git commit -m "Add format flag name constant for print commands"
```

---

### Task 2: Create format.go with JSONL parsing types and tool parameter map

**Files:**
- Create: `internal/session/format.go`

**Step 1: Write the failing test**

Create `internal/session/format_test.go` with a test for `formatToolCall`:

```go
package session

import (
	"testing"
)

func TestFormatToolCall(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    map[string]interface{}
		want     string
	}{
		{
			name:     "Bash with command",
			toolName: "Bash",
			input:    map[string]interface{}{"command": "git status", "description": "Show status"},
			want:     `  > Bash("git status")`,
		},
		{
			name:     "Read with file_path",
			toolName: "Read",
			input:    map[string]interface{}{"file_path": "/src/main.go"},
			want:     `  > Read("/src/main.go")`,
		},
		{
			name:     "Grep with pattern and path",
			toolName: "Grep",
			input:    map[string]interface{}{"pattern": "TODO", "path": "src/"},
			want:     `  > Grep("TODO", path="src/")`,
		},
		{
			name:     "unknown tool",
			toolName: "SomeNewTool",
			input:    map[string]interface{}{"foo": "bar"},
			want:     `  > SomeNewTool()`,
		},
		{
			name:     "long parameter truncated",
			toolName: "Bash",
			input:    map[string]interface{}{"command": "echo " + string(make([]byte, 200))},
			want:     `  > Bash("echo ` + string(make([]byte, 94)) + `...")`,
		},
		{
			name:     "MCP tool uses first string field",
			toolName: "mcp__todoist__add-tasks",
			input:    map[string]interface{}{"tasks": []interface{}{}, "query": "search term"},
			want:     `  > mcp__todoist__add-tasks("search term")`,
		},
		{
			name:     "Edit with file_path",
			toolName: "Edit",
			input:    map[string]interface{}{"file_path": "/src/main.go", "old_string": "foo", "new_string": "bar"},
			want:     `  > Edit("/src/main.go")`,
		},
		{
			name:     "WebSearch with query",
			toolName: "WebSearch",
			input:    map[string]interface{}{"query": "best pizza RS"},
			want:     `  > WebSearch("best pizza RS")`,
		},
		{
			name:     "Task with description",
			toolName: "Task",
			input:    map[string]interface{}{"description": "Explore codebase", "prompt": "long prompt text..."},
			want:     `  > Task("Explore codebase")`,
		},
		{
			name:     "TaskUpdate with taskId and status",
			toolName: "TaskUpdate",
			input:    map[string]interface{}{"taskId": "3", "status": "completed"},
			want:     `  > TaskUpdate("3", status="completed")`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatToolCall(tt.toolName, tt.input)
			if got != tt.want {
				t.Errorf("formatToolCall() = %q, want %q", got, tt.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: FAIL — `formatToolCall` undefined

**Step 3: Write format.go with types, tool map, and formatToolCall**

Create `internal/session/format.go`:

```go
package session

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// maxToolParamLen is the maximum length for a tool parameter value before truncation.
	maxToolParamLen = 100

	// maxErrorLen is the maximum length for a tool result error message.
	maxErrorLen = 200
)

// jsonlEntry is the minimal structure for dispatching JSONL lines by type.
type jsonlEntry struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
}

// apiMessage represents a Claude API message with role and content blocks.
type apiMessage struct {
	Role    string            `json:"role"`
	Content json.RawMessage   `json:"content"`
}

// contentBlock represents a single block within a message's content array.
type contentBlock struct {
	Type       string                 `json:"type"`
	Text       string                 `json:"text"`
	Name       string                 `json:"name"`
	Input      map[string]interface{} `json:"input"`
	ToolUseID  string                 `json:"tool_use_id"`
	Content    json.RawMessage        `json:"content"`
	IsError    bool                   `json:"is_error"`
}

// toolParamSpec defines which input fields to extract for a tool's one-line summary.
type toolParamSpec struct {
	// primary is the main parameter field name (shown as the first argument).
	primary string
	// secondary is an optional second parameter field name (shown as key=value).
	secondary string
}

// toolParamMap maps tool names to their key parameter specifications.
var toolParamMap = map[string]toolParamSpec{
	"Bash":         {primary: "command"},
	"Read":         {primary: "file_path"},
	"Edit":         {primary: "file_path"},
	"Write":        {primary: "file_path"},
	"Glob":         {primary: "pattern"},
	"Grep":         {primary: "pattern", secondary: "path"},
	"WebSearch":    {primary: "query"},
	"WebFetch":     {primary: "url"},
	"Task":         {primary: "description"},
	"NotebookEdit": {primary: "notebook_path"},
	"Skill":        {primary: "skill"},
	"TaskCreate":   {primary: "subject"},
	"TaskUpdate":   {primary: "taskId", secondary: "status"},
}

// formatToolCall produces a one-line summary of a tool invocation.
// Example: `  > Read("/src/main.go")`
func formatToolCall(toolName string, input map[string]interface{}) string {
	spec, found := toolParamMap[toolName]
	if !found {
		// For MCP tools (contain "__"), try to find the first string-valued field.
		if strings.Contains(toolName, "__") {
			for _, v := range input {
				if s, ok := v.(string); ok && s != "" {
					return fmt.Sprintf("  > %s(%q)", toolName, truncate(s, maxToolParamLen))
				}
			}
		}
		return fmt.Sprintf("  > %s()", toolName)
	}

	primaryVal := extractStringField(input, spec.primary)
	if primaryVal == "" {
		return fmt.Sprintf("  > %s()", toolName)
	}

	primaryVal = truncate(primaryVal, maxToolParamLen)
	if spec.secondary != "" {
		secondaryVal := extractStringField(input, spec.secondary)
		if secondaryVal != "" {
			return fmt.Sprintf("  > %s(%q, %s=%q)", toolName, primaryVal, spec.secondary, truncate(secondaryVal, maxToolParamLen))
		}
	}

	return fmt.Sprintf("  > %s(%q)", toolName, primaryVal)
}

// extractStringField retrieves a string value from a map, handling both
// string and json.Number types. Returns "" if the field is missing or
// not a string-like type.
func extractStringField(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case json.Number:
		return val.String()
	case float64:
		return fmt.Sprintf("%g", val)
	default:
		return ""
	}
}

// truncate shortens a string to maxLen characters, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
```

**Step 4: Run test to verify it passes**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 5: Commit**

```bash
git add internal/session/format.go internal/session/format_test.go
git commit -m "Add tool parameter map and formatToolCall for human-readable print"
```

---

### Task 3: Implement FormatConversation

**Files:**
- Modify: `internal/session/format.go`
- Modify: `internal/session/format_test.go`

**Step 1: Write the failing test**

Add to `internal/session/format_test.go`:

```go
func TestFormatConversation(t *testing.T) {
	claudeTmpDir := "/tmp/claude"
	if err := os.MkdirAll(claudeTmpDir, 0755); err != nil {
		t.Fatalf("failed to create /tmp/claude: %v", err)
	}

	tests := []struct {
		name  string
		jsonl string
		n     int
		want  string
	}{
		{
			name: "user text message",
			jsonl: `{"type":"user","message":{"role":"user","content":"hello world"}}
`,
			n: 0,
			want: `[USER]
hello world
`,
		},
		{
			name: "assistant text message",
			jsonl: `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Here is my response."}]}}
`,
			n: 0,
			want: `[ASSISTANT]
Here is my response.
`,
		},
		{
			name: "assistant with tool_use",
			jsonl: `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Let me check."},{"type":"tool_use","name":"Read","input":{"file_path":"/src/main.go"}}]}}
`,
			n: 0,
			want: `[ASSISTANT]
Let me check.
  > Read("/src/main.go")
`,
		},
		{
			name: "assistant with only tool calls",
			jsonl: `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"git status"}}]}}
`,
			n: 0,
			want: `[ASSISTANT]
  > Bash("git status")
`,
		},
		{
			name: "thinking blocks are skipped",
			jsonl: `{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hmm let me think"},{"type":"text","text":"Done."}]}}
`,
			n: 0,
			want: `[ASSISTANT]
Done.
`,
		},
		{
			name: "progress and system lines are skipped",
			jsonl: `{"type":"progress","data":{"type":"hook_progress"}}
{"type":"system","subtype":"stop_hook_summary"}
{"type":"file-history-snapshot","snapshot":{}}
{"type":"queue-operation","operation":"enqueue"}
{"type":"user","message":{"role":"user","content":"hello"}}
`,
			n: 0,
			want: `[USER]
hello
`,
		},
		{
			name: "user with tool_result error",
			jsonl: `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"123","is_error":true,"content":"Error: file not found"}]}}
`,
			n: 0,
			want: `  > ERROR: Error: file not found
`,
		},
		{
			name: "user with tool_result success is skipped",
			jsonl: `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"123","content":"file contents here..."}]}}
`,
			n:    0,
			want: "",
		},
		{
			name: "tail limits JSONL entries",
			jsonl: `{"type":"user","message":{"role":"user","content":"first message"}}
{"type":"user","message":{"role":"user","content":"second message"}}
{"type":"user","message":{"role":"user","content":"third message"}}
`,
			n: 1,
			want: `[USER]
third message
`,
		},
		{
			name: "full conversation flow",
			jsonl: `{"type":"progress","data":{"type":"hook_progress"}}
{"type":"user","message":{"role":"user","content":"find me a burger shop"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Let me search for that."},{"type":"tool_use","name":"WebSearch","input":{"query":"best burgers"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"123","content":"search results..."}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Here is what I found."}]}}
`,
			n: 0,
			want: `[USER]
find me a burger shop

[ASSISTANT]
Let me search for that.
  > WebSearch("best burgers")

[ASSISTANT]
Here is what I found.
`,
		},
		{
			name: "blank line between blocks",
			jsonl: `{"type":"user","message":{"role":"user","content":"question 1"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"answer 1"}]}}
{"type":"user","message":{"role":"user","content":"question 2"}}
`,
			n: 0,
			want: `[USER]
question 1

[ASSISTANT]
answer 1

[USER]
question 2
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp(claudeTmpDir, "format-test-")
			if err != nil {
				t.Fatalf("failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			jsonlFilepath := filepath.Join(tmpDir, "session.jsonl")
			if err := os.WriteFile(jsonlFilepath, []byte(tt.jsonl), 0644); err != nil {
				t.Fatalf("failed to write JSONL: %v", err)
			}

			var buf strings.Builder
			err = FormatConversation(jsonlFilepath, tt.n, &buf)
			if err != nil {
				t.Fatalf("FormatConversation() error: %v", err)
			}
			if buf.String() != tt.want {
				t.Errorf("FormatConversation() =\n%s\nwant:\n%s", buf.String(), tt.want)
			}
		})
	}
}
```

Add these imports to the test file: `"os"`, `"path/filepath"`, `"strings"`.

**Step 2: Run test to verify it fails**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: FAIL — `FormatConversation` undefined

**Step 3: Implement FormatConversation in format.go**

Add to `internal/session/format.go`:

```go
import (
	"bufio"
	"io"
	"os"
)
```

```go
// FormatConversation reads a JSONL session file and writes a human-readable
// conversation view to the given writer. If n > 0, only the last n JSONL
// entries are processed. If n <= 0, the entire file is processed.
func FormatConversation(jsonlFilepath string, n int, w io.Writer) error {
	lines, err := collectJSONLLines(jsonlFilepath, n)
	if err != nil {
		return err
	}

	wroteBlock := false
	for _, line := range lines {
		output := formatJSONLLine(line)
		if output == "" {
			continue
		}
		if wroteBlock {
			fmt.Fprintln(w)
		}
		fmt.Fprint(w, output)
		wroteBlock = true
	}

	return nil
}

// collectJSONLLines reads JSONL lines from a file. If n > 0, returns the last
// n lines. If n <= 0, returns all lines.
func collectJSONLLines(jsonlFilepath string, n int) ([]string, error) {
	file, err := os.Open(jsonlFilepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open session file '%s': %w", jsonlFilepath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	if n <= 0 {
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("error reading session file: %w", err)
		}
		return lines, nil
	}

	ring := make([]string, n)
	total := 0
	for scanner.Scan() {
		ring[total%n] = scanner.Text()
		total++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading session file: %w", err)
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
	return result, nil
}

// formatJSONLLine parses a single JSONL line and returns the human-readable
// output string for it. Returns "" for lines that should be skipped.
func formatJSONLLine(line string) string {
	var entry jsonlEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return ""
	}

	switch entry.Type {
	case "user":
		return formatUserEntry(entry.Message)
	case "assistant":
		return formatAssistantEntry(entry.Message)
	default:
		return ""
	}
}

// formatUserEntry formats a user message. User messages have content that is
// either a plain string or an array of content blocks.
func formatUserEntry(rawMessage json.RawMessage) string {
	var msg apiMessage
	if err := json.Unmarshal(rawMessage, &msg); err != nil {
		return ""
	}

	// Try parsing content as a plain string first
	var contentStr string
	if err := json.Unmarshal(msg.Content, &contentStr); err == nil {
		if contentStr == "" {
			return ""
		}
		return fmt.Sprintf("[USER]\n%s\n", contentStr)
	}

	// Parse as array of content blocks
	var blocks []contentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return ""
	}

	var parts []string
	hasUserText := false
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				hasUserText = true
				parts = append(parts, b.Text)
			}
		case "tool_result":
			if b.IsError {
				errMsg := extractToolResultError(b)
				if errMsg != "" {
					parts = append(parts, fmt.Sprintf("  > ERROR: %s", truncate(errMsg, maxErrorLen)))
				}
			}
			// Non-error tool results are skipped
		}
	}

	if len(parts) == 0 {
		return ""
	}

	var sb strings.Builder
	if hasUserText {
		sb.WriteString("[USER]\n")
	}
	for _, p := range parts {
		sb.WriteString(p)
		sb.WriteString("\n")
	}
	return sb.String()
}

// formatAssistantEntry formats an assistant message containing text, tool_use,
// and/or thinking blocks.
func formatAssistantEntry(rawMessage json.RawMessage) string {
	var msg apiMessage
	if err := json.Unmarshal(rawMessage, &msg); err != nil {
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
			parts = append(parts, formatToolCall(b.Name, b.Input))
		// thinking blocks are skipped
		}
	}

	if len(parts) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("[ASSISTANT]\n")
	for _, p := range parts {
		sb.WriteString(p)
		sb.WriteString("\n")
	}
	return sb.String()
}

// extractToolResultError extracts the error message from a tool_result block.
// The content field can be a string or an array of content blocks.
func extractToolResultError(b contentBlock) string {
	// Try as string first
	var s string
	if err := json.Unmarshal(b.Content, &s); err == nil {
		return s
	}

	// Try as array of text blocks
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(b.Content, &blocks); err == nil {
		for _, block := range blocks {
			if block.Text != "" {
				return block.Text
			}
		}
	}

	return ""
}
```

**Step 4: Run test to verify it passes**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 5: Commit**

```bash
git add internal/session/format.go internal/session/format_test.go
git commit -m "Implement FormatConversation for human-readable session output"
```

---

### Task 4: Add --format flag to session print and mission print

**Files:**
- Modify: `cmd/session_print.go`
- Modify: `cmd/mission_print.go`

**Step 1: Update session_print.go**

Replace the entire file with:

```go
package cmd

import (
	"os"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/session"
)

const defaultTailLines = 20

var sessionPrintTailFlag int
var sessionPrintAllFlag bool
var sessionPrintFormatFlag string

var sessionPrintCmd = &cobra.Command{
	Use:   printCmdStr + " <session-uuid>",
	Short: "Print the transcript for a Claude session",
	Long: `Print the transcript for a Claude session.

Outputs a human-readable conversation view by default.
Use --format=jsonl for raw JSONL output.

Outputs the last 20 JSONL entries by default. Use --tail to change the count,
or --all to print the entire session.

Example:
  agenc session print 18749fb5-02ba-4b19-b989-4e18fbf8ea92
  agenc session print 18749fb5-02ba-4b19-b989-4e18fbf8ea92 --tail 50
  agenc session print 18749fb5-02ba-4b19-b989-4e18fbf8ea92 --all
  agenc session print 18749fb5-02ba-4b19-b989-4e18fbf8ea92 --format=jsonl`,
	Args: cobra.ExactArgs(1),
	RunE: runSessionPrint,
}

func init() {
	sessionPrintCmd.Flags().IntVar(&sessionPrintTailFlag, tailFlagName, defaultTailLines, "number of JSONL entries to process from end of session")
	sessionPrintCmd.Flags().BoolVar(&sessionPrintAllFlag, allFlagName, false, "print entire session")
	sessionPrintCmd.Flags().StringVar(&sessionPrintFormatFlag, formatFlagName, "text", "output format: text or jsonl")
	sessionCmd.AddCommand(sessionPrintCmd)
}

func runSessionPrint(cmd *cobra.Command, args []string) error {
	sessionID := args[0]

	if !sessionPrintAllFlag && sessionPrintTailFlag <= 0 {
		return stacktrace.NewError("--tail value must be positive")
	}

	jsonlFilepath, err := session.FindSessionJSONLPath(sessionID)
	if err != nil {
		return stacktrace.Propagate(err, "")
	}

	return printSession(jsonlFilepath, sessionPrintTailFlag, sessionPrintAllFlag, sessionPrintFormatFlag)
}

// printSession is the shared printing logic used by both session print and
// mission print commands. It dispatches between human-readable text output
// and raw JSONL based on the format parameter.
func printSession(jsonlFilepath string, tailLines int, all bool, format string) error {
	n := tailLines
	if all {
		n = 0
	}

	switch format {
	case "jsonl":
		_, err := session.TailJSONLFile(jsonlFilepath, n, os.Stdout)
		if err != nil {
			return stacktrace.Propagate(err, "")
		}
		return nil
	case "text":
		return session.FormatConversation(jsonlFilepath, n, os.Stdout)
	default:
		return stacktrace.NewError("invalid format %q: must be \"text\" or \"jsonl\"", format)
	}
}
```

**Step 2: Update mission_print.go**

Add the format flag variable and wire it through. Changes:

1. Add `var missionPrintFormatFlag string` after line 14
2. Add format flag registration in `init()` after line 38
3. Update the call on line 111 to pass the format flag

The updated file:

```go
package cmd

import (
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/session"
)

var missionPrintTailFlag int
var missionPrintAllFlag bool
var missionPrintFormatFlag string

var missionPrintCmd = &cobra.Command{
	Use:   printCmdStr + " [mission-id]",
	Short: "Print the transcript for a mission's current session",
	Long: `Print the transcript for a mission's current session.

Without arguments, opens an interactive fzf picker to select a mission.
With arguments, accepts a mission ID (short 8-char hex or full UUID).

Outputs a human-readable conversation view by default.
Use --format=jsonl for raw JSONL output.

Outputs the last 20 JSONL entries by default. Use --tail to change the count,
or --all to print the entire session.

Example:
  agenc mission print
  agenc mission print 2571d5d8
  agenc mission print 2571d5d8 --tail 50
  agenc mission print 2571d5d8 --all
  agenc mission print 2571d5d8 --format=jsonl`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionPrint,
}

func init() {
	missionPrintCmd.Flags().IntVar(&missionPrintTailFlag, tailFlagName, defaultTailLines, "number of JSONL entries to process from end of session")
	missionPrintCmd.Flags().BoolVar(&missionPrintAllFlag, allFlagName, false, "print entire session")
	missionPrintCmd.Flags().StringVar(&missionPrintFormatFlag, formatFlagName, "text", "output format: text or jsonl")
	missionCmd.AddCommand(missionPrintCmd)
}

func runMissionPrint(cmd *cobra.Command, args []string) error {
	if !missionPrintAllFlag && missionPrintTailFlag <= 0 {
		return stacktrace.NewError("--tail value must be positive")
	}

	client, err := serverClient()
	if err != nil {
		return err
	}

	missions, err := client.ListMissions(false, "")
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		return stacktrace.NewError("no missions found")
	}

	entries := buildMissionPickerEntries(missions, defaultPromptMaxLen)

	input := strings.Join(args, " ")
	result, err := Resolve(input, Resolver[missionPickerEntry]{
		TryCanonical: func(input string) (missionPickerEntry, bool, error) {
			if !looksLikeMissionID(input) {
				return missionPickerEntry{}, false, nil
			}
			missionID, err := client.ResolveMissionID(input)
			if err != nil {
				return missionPickerEntry{}, false, stacktrace.Propagate(err, "failed to resolve mission ID")
			}
			for _, e := range entries {
				if e.MissionID == missionID {
					return e, true, nil
				}
			}
			return missionPickerEntry{}, false, stacktrace.NewError("mission %s not found", input)
		},
		GetItems: func() ([]missionPickerEntry, error) { return entries, nil },
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Status, e.Session, e.Repo}
		},
		FzfPrompt:         "Select mission to print session: ",
		FzfHeaders:        []string{"LAST ACTIVE", "ID", "STATUS", "SESSION", "REPO"},
		MultiSelect:       false,
		NotCanonicalError: "not a valid mission ID",
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	missionID := result.Items[0].MissionID

	// Resolve mission's current session ID
	sessionID := claudeconfig.GetLastSessionID(agencDirpath, missionID)
	if sessionID == "" {
		return stacktrace.NewError("no current session found for mission %s", missionID)
	}

	// Find and print the session JSONL
	jsonlFilepath, err := session.FindSessionJSONLPath(sessionID)
	if err != nil {
		return stacktrace.Propagate(err, "")
	}

	return printSession(jsonlFilepath, missionPrintTailFlag, missionPrintAllFlag, missionPrintFormatFlag)
}
```

**Step 3: Run checks**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS (builds, formats, vets, tests all pass)

**Step 4: Commit**

```bash
git add cmd/session_print.go cmd/mission_print.go cmd/command_str_consts.go
git commit -m "Add --format flag to session print and mission print commands"
```

---

### Task 5: Manual smoke test

**Step 1: Build**

Run: `make build` (with `dangerouslyDisableSandbox: true`)

**Step 2: Test human-readable output on a real session**

Run: `./agenc mission print --all` (pick a mission with the fzf picker)

Verify the output shows `[USER]`, `[ASSISTANT]`, tool call summaries, and skips progress/system entries.

**Step 3: Test JSONL backward compatibility**

Run: `./agenc mission print --all --format=jsonl` (same mission)

Verify it produces raw JSONL, identical to the old default behavior.

**Step 4: Test tail behavior**

Run: `./agenc mission print --tail 5`

Verify it processes only the last 5 JSONL entries.

**Step 5: Test invalid format**

Run: `./agenc mission print --format=xml`

Verify it produces an error message about invalid format.

**Step 6: Commit any fixes, push**

```bash
git push
```
