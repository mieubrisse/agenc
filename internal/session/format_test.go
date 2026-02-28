package session

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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
			input:    map[string]interface{}{"command": "ls -la"},
			want:     `  > Bash("ls -la")`,
		},
		{
			name:     "Read with file_path",
			toolName: "Read",
			input:    map[string]interface{}{"file_path": "/tmp/foo.txt"},
			want:     `  > Read("/tmp/foo.txt")`,
		},
		{
			name:     "Grep with primary and secondary",
			toolName: "Grep",
			input:    map[string]interface{}{"pattern": "TODO", "path": "/src"},
			want:     `  > Grep("TODO", path="/src")`,
		},
		{
			name:     "Grep with primary only",
			toolName: "Grep",
			input:    map[string]interface{}{"pattern": "TODO"},
			want:     `  > Grep("TODO")`,
		},
		{
			name:     "unknown tool",
			toolName: "SomeUnknownTool",
			input:    map[string]interface{}{"foo": "bar"},
			want:     "  > SomeUnknownTool()",
		},
		{
			name:     "long param truncation",
			toolName: "Bash",
			input:    map[string]interface{}{"command": strings.Repeat("x", 150)},
			want:     `  > Bash("` + strings.Repeat("x", 100) + `...")`,
		},
		{
			name:     "MCP tool fallback uses first string field",
			toolName: "mcp__todoist__add-tasks",
			input:    map[string]interface{}{"content": "Buy groceries"},
			want:     `  > mcp__todoist__add-tasks("Buy groceries")`,
		},
		{
			name:     "MCP tool with no string fields",
			toolName: "mcp__todoist__list-tasks",
			input:    map[string]interface{}{},
			want:     "  > mcp__todoist__list-tasks()",
		},
		{
			name:     "Edit with file_path",
			toolName: "Edit",
			input:    map[string]interface{}{"file_path": "/tmp/edit.go"},
			want:     `  > Edit("/tmp/edit.go")`,
		},
		{
			name:     "WebSearch with query",
			toolName: "WebSearch",
			input:    map[string]interface{}{"query": "golang testing"},
			want:     `  > WebSearch("golang testing")`,
		},
		{
			name:     "Task with description",
			toolName: "Task",
			input:    map[string]interface{}{"description": "analyze the codebase"},
			want:     `  > Task("analyze the codebase")`,
		},
		{
			name:     "TaskUpdate with primary and secondary",
			toolName: "TaskUpdate",
			input:    map[string]interface{}{"taskId": "42", "status": "completed"},
			want:     `  > TaskUpdate("42", status="completed")`,
		},
		{
			name:     "empty primary param",
			toolName: "Bash",
			input:    map[string]interface{}{"command": ""},
			want:     "  > Bash()",
		},
		{
			name:     "nil input map",
			toolName: "Bash",
			input:    nil,
			want:     "  > Bash()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatToolCall(tt.toolName, tt.input)
			if got != tt.want {
				t.Errorf("formatToolCall(%q, ...) =\n  %q\nwant:\n  %q", tt.toolName, got, tt.want)
			}
		})
	}
}

func TestFormatConversation(t *testing.T) {
	claudeTmpDir := "/tmp/claude"
	if err := os.MkdirAll(claudeTmpDir, 0755); err != nil {
		t.Fatalf("failed to create /tmp/claude: %v", err)
	}

	tests := []struct {
		name         string
		jsonlContent string
		n            int
		want         string
	}{
		{
			name: "user text message",
			jsonlContent: `{"type":"user","message":{"role":"user","content":"Hello, Claude!"}}
`,
			n:    0,
			want: "[USER]\nHello, Claude!\n",
		},
		{
			name: "assistant text message",
			jsonlContent: `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hi there!"}]}}
`,
			n:    0,
			want: "[ASSISTANT]\nHi there!\n",
		},
		{
			name: "assistant with tool_use",
			jsonlContent: `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Let me check."},{"type":"tool_use","name":"Bash","input":{"command":"ls -la"}}]}}
`,
			n:    0,
			want: "[ASSISTANT]\nLet me check.\n  > Bash(\"ls -la\")\n",
		},
		{
			name: "assistant with only tool calls no text",
			jsonlContent: `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/tmp/foo.txt"}},{"type":"tool_use","name":"Glob","input":{"pattern":"*.go"}}]}}
`,
			n:    0,
			want: "[ASSISTANT]\n  > Read(\"/tmp/foo.txt\")\n  > Glob(\"*.go\")\n",
		},
		{
			name: "thinking blocks skipped",
			jsonlContent: `{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","text":"I need to think..."},{"type":"text","text":"Here is my answer."}]}}
`,
			n:    0,
			want: "[ASSISTANT]\nHere is my answer.\n",
		},
		{
			name: "progress lines skipped",
			jsonlContent: `{"type":"progress","message":"doing stuff"}
`,
			n:    0,
			want: "",
		},
		{
			name: "system lines skipped",
			jsonlContent: `{"type":"system","message":"system init"}
`,
			n:    0,
			want: "",
		},
		{
			name: "file-history-snapshot lines skipped",
			jsonlContent: `{"type":"file-history-snapshot","message":"snapshot data"}
`,
			n:    0,
			want: "",
		},
		{
			name: "queue-operation lines skipped",
			jsonlContent: `{"type":"queue-operation","message":"queue data"}
`,
			n:    0,
			want: "",
		},
		{
			name: "user with tool_result error",
			jsonlContent: `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"abc","is_error":true,"content":"Command failed: exit code 1"}]}}
`,
			n:    0,
			want: "[USER]\n  > ERROR: Command failed: exit code 1\n",
		},
		{
			name: "user with tool_result error as array content",
			jsonlContent: `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"abc","is_error":true,"content":[{"type":"text","text":"error detail here"}]}]}}
`,
			n:    0,
			want: "[USER]\n  > ERROR: error detail here\n",
		},
		{
			name: "user with successful tool_result skipped",
			jsonlContent: `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"abc","is_error":false,"content":"success output"}]}}
`,
			n:    0,
			want: "",
		},
		{
			name: "tail limits JSONL entries",
			jsonlContent: `{"type":"user","message":{"role":"user","content":"First message"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"First reply"}]}}
{"type":"user","message":{"role":"user","content":"Second message"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Second reply"}]}}
`,
			n: 2,
			want: "[USER]\nSecond message\n" +
				"\n" +
				"[ASSISTANT]\nSecond reply\n",
		},
		{
			name: "full conversation flow with blank lines between blocks",
			jsonlContent: `{"type":"user","message":{"role":"user","content":"What files are here?"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Let me check."},{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"abc","is_error":false,"content":"file1.txt\nfile2.txt"}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"I found two files: file1.txt and file2.txt."}]}}
`,
			n: 0,
			want: "[USER]\nWhat files are here?\n" +
				"\n" +
				"[ASSISTANT]\nLet me check.\n  > Bash(\"ls\")\n" +
				"\n" +
				"[ASSISTANT]\nI found two files: file1.txt and file2.txt.\n",
		},
		{
			name: "blank lines between blocks but not after last",
			jsonlContent: `{"type":"user","message":{"role":"user","content":"Hello"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hi"}]}}
`,
			n: 0,
			want: "[USER]\nHello\n" +
				"\n" +
				"[ASSISTANT]\nHi\n",
		},
		{
			name: "skipped lines do not produce extra blank lines",
			jsonlContent: `{"type":"user","message":{"role":"user","content":"Hello"}}
{"type":"progress","message":"thinking..."}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hi"}]}}
`,
			n: 0,
			want: "[USER]\nHello\n" +
				"\n" +
				"[ASSISTANT]\nHi\n",
		},
		{
			name:         "empty file",
			jsonlContent: "",
			n:            0,
			want:         "",
		},
		{
			name: "assistant with only thinking block produces no output",
			jsonlContent: `{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","text":"just thinking"}]}}
`,
			n:    0,
			want: "",
		},
		{
			name: "long error message is truncated",
			jsonlContent: `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"abc","is_error":true,"content":"` + strings.Repeat("E", 250) + `"}]}}
`,
			n:    0,
			want: "[USER]\n  > ERROR: " + strings.Repeat("E", 200) + "...\n",
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
			if err := os.WriteFile(jsonlFilepath, []byte(tt.jsonlContent), 0644); err != nil {
				t.Fatalf("failed to write JSONL file: %v", err)
			}

			var buf bytes.Buffer
			if err := FormatConversation(jsonlFilepath, tt.n, &buf); err != nil {
				t.Fatalf("FormatConversation() error: %v", err)
			}

			got := buf.String()
			if got != tt.want {
				t.Errorf("FormatConversation() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestFormatConversationFileError(t *testing.T) {
	var buf bytes.Buffer
	err := FormatConversation("/nonexistent/path/session.jsonl", 0, &buf)
	if err == nil {
		t.Error("FormatConversation() should return error for nonexistent file")
	}
}
