package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const (
	// maxToolParamLen is the maximum length for tool parameter values before truncation.
	maxToolParamLen = 100

	// maxErrorLen is the maximum length for tool result error messages before truncation.
	maxErrorLen = 200
)

// jsonlEntry is the minimal structure for dispatching JSONL lines by type.
type jsonlEntry struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
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
	Name      string                 `json:"name"`
	Input     map[string]interface{} `json:"input"`
	ToolUseID string                 `json:"tool_use_id"`
	Content   json.RawMessage        `json:"content"`
	IsError   bool                   `json:"is_error"`
}

// toolParamSpec defines which input fields to extract for a tool's one-line summary.
type toolParamSpec struct {
	primary   string
	secondary string
}

// toolParamMap maps tool names to the input fields that should appear in their one-line summary.
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

// FormatConversation reads a JSONL session file and writes a human-readable
// formatted conversation to the given writer. If n > 0, only the last n JSONL
// entries are included; if n <= 0, all entries are included.
func FormatConversation(jsonlFilepath string, n int, w io.Writer) error {
	lines, err := collectJSONLLines(jsonlFilepath, n)
	if err != nil {
		return err
	}

	var blocks []string
	for _, line := range lines {
		formatted := formatJSONLLine(line)
		if formatted != "" {
			blocks = append(blocks, formatted)
		}
	}

	for i, block := range blocks {
		fmt.Fprint(w, block)
		if i < len(blocks)-1 {
			fmt.Fprintln(w)
		}
	}
	return nil
}

// collectJSONLLines reads all lines from a JSONL file, returning the last n
// lines if n > 0, or all lines if n <= 0. Uses a 1MB scanner buffer and a
// ring buffer for efficient tail collection.
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

// formatJSONLLine parses a single JSONL line and returns its formatted
// representation, or "" if the line should be skipped.
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
		// Skip system, progress, file-history-snapshot, queue-operation,
		// summary, custom-title, and any other non-conversation types.
		return ""
	}
}

// formatUserEntry formats a user message entry. Plain text content is shown
// under a [USER] header. Tool result blocks with errors are shown as error
// lines; successful tool results are skipped entirely.
func formatUserEntry(rawMessage json.RawMessage) string {
	var msg apiMessage
	if err := json.Unmarshal(rawMessage, &msg); err != nil {
		return ""
	}

	// Try string content first (plain user text).
	var textContent string
	if err := json.Unmarshal(msg.Content, &textContent); err == nil {
		if textContent == "" {
			return ""
		}
		return "[USER]\n" + textContent + "\n"
	}

	// Array content — look for user text and tool_result blocks with errors.
	var blocks []contentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return ""
	}

	var parts []string
	hasUserText := false
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			hasUserText = true
			parts = append(parts, b.Text)
		}
		if b.Type == "tool_result" && b.IsError {
			errMsg := extractToolResultError(b)
			if errMsg != "" {
				parts = append(parts, "  > ERROR: "+truncate(errMsg, maxErrorLen))
			}
		}
		// Successful tool results are skipped.
	}

	if len(parts) == 0 {
		return ""
	}
	if hasUserText {
		return "[USER]\n" + strings.Join(parts, "\n") + "\n"
	}
	return strings.Join(parts, "\n") + "\n"
}

// formatAssistantEntry formats an assistant message entry. Text blocks are
// rendered as-is, tool_use blocks as one-line summaries, and thinking blocks
// are skipped.
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
		case "thinking":
			// Skip thinking blocks.
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return "[ASSISTANT]\n" + strings.Join(parts, "\n") + "\n"
}

// extractToolResultError extracts the error message from a tool_result content
// block. The content field can be either a plain string or an array of text
// blocks.
func extractToolResultError(b contentBlock) string {
	// Try string content first.
	var textContent string
	if err := json.Unmarshal(b.Content, &textContent); err == nil {
		return textContent
	}

	// Try array of content blocks.
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
