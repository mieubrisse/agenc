package tableprinter

import (
	"bytes"
	"strings"
	"testing"
)

func TestVisibleWidth_PlainText(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"hello", 5},
		{"", 0},
		{"hello world", 11},
	}
	for _, tt := range tests {
		got := VisibleWidth(tt.input)
		if got != tt.want {
			t.Errorf("VisibleWidth(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestVisibleWidth_ANSICodes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{
			name:  "green text",
			input: "\033[32mRUNNING\033[0m",
			want:  7,
		},
		{
			name:  "bold red text",
			input: "\033[1;31mERROR\033[0m",
			want:  5,
		},
		{
			name:  "only ANSI codes",
			input: "\033[32m\033[0m",
			want:  0,
		},
		{
			name:  "mixed plain and colored",
			input: "status: \033[33mARCHIVED\033[0m ok",
			want:  19, // "status: ARCHIVED ok"
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VisibleWidth(tt.input)
			if got != tt.want {
				t.Errorf("VisibleWidth(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestVisibleWidth_Emoji(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{
			name:  "simple emoji",
			input: "🔍 Researcher",
			want:  13, // 🔍 = 2 columns, space + "Researcher" = 11
		},
		{
			name:  "ZWJ sequence emoji",
			input: "👨‍💻 Software Engineer",
			want:  20, // 👨‍💻 = 2 columns, space + "Software Engineer" = 18
		},
		{
			name:  "checkmark emoji",
			input: "✅ Todoist Manager",
			want:  18, // ✅ = 2 columns, space + "Todoist Manager" = 16
		},
		{
			name:  "emoji with ANSI codes",
			input: "\033[32m🤖 Bot\033[0m",
			want:  6, // 🤖 = 2 columns, space + "Bot" = 4
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VisibleWidth(tt.input)
			if got != tt.want {
				t.Errorf("VisibleWidth(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewTable_BasicAlignment(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable("NAME", "VALUE").WithWriter(&buf)
	tbl.AddRow("short", "1")
	tbl.AddRow("a longer name", "2")
	tbl.Print()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines (header + 2 rows), got %d", len(lines))
	}

	// The VALUE column should start at the same position in each line.
	// Find the index of the value column content in each row.
	headerValueIdx := strings.Index(lines[0], "VALUE")
	row1ValueIdx := strings.Index(lines[1], "1")
	row2ValueIdx := strings.Index(lines[2], "2")

	if headerValueIdx != row1ValueIdx || headerValueIdx != row2ValueIdx {
		t.Errorf("VALUE column misaligned: header=%d, row1=%d, row2=%d",
			headerValueIdx, row1ValueIdx, row2ValueIdx)
	}
}

func TestNewTable_EmojiCellsAlignCorrectly(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable("AGENT", "STATUS").WithWriter(&buf)
	tbl.AddRow("👨‍💻 Software Engineer", "RUNNING")
	tbl.AddRow("--", "STOPPED")
	tbl.Print()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(lines))
	}

	// The STATUS column values should start at the same visible position
	// regardless of whether the AGENT cell contains emoji.
	emojiLine := lines[1]
	plainLine := lines[2]

	emojiStatusIdx := VisibleWidth(emojiLine[:strings.Index(emojiLine, "RUNNING")])
	plainStatusIdx := VisibleWidth(plainLine[:strings.Index(plainLine, "STOPPED")])

	if emojiStatusIdx != plainStatusIdx {
		t.Errorf("STATUS column misaligned: emoji row starts at visible pos %d, plain row at %d",
			emojiStatusIdx, plainStatusIdx)
	}
}

func TestNewTable_ANSICellsAlignCorrectly(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable("STATUS", "NAME").WithWriter(&buf)
	tbl.AddRow("\033[32mRUNNING\033[0m", "mission-1")
	tbl.AddRow("STOPPED", "mission-2")
	tbl.Print()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(lines))
	}

	// The "NAME" column values should start at the same position in visible width.
	// The ANSI-colored row has extra invisible bytes, so raw string length will differ,
	// but the visible column start should be the same.
	runningLine := lines[1]
	stoppedLine := lines[2]

	// Find where "mission-1" and "mission-2" start by visible width
	runningNameIdx := VisibleWidth(runningLine[:strings.Index(runningLine, "mission-1")])
	stoppedNameIdx := VisibleWidth(stoppedLine[:strings.Index(stoppedLine, "mission-2")])

	if runningNameIdx != stoppedNameIdx {
		t.Errorf("NAME column misaligned: RUNNING row starts at visible pos %d, STOPPED row at %d",
			runningNameIdx, stoppedNameIdx)
	}
}
