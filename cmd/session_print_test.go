package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPrintSessionMetadataOnly verifies that printSessionTo emits the
// empty-session message to stderr (and nothing to stdout) when the JSONL
// contains only metadata entries — the bug previously masqueraded as
// "agenc mission print | tail produces empty output" during the brief
// window between mission spawn and the first user/assistant message.
func TestPrintSessionMetadataOnly(t *testing.T) {
	jsonlContent := `{"type":"file-history-snapshot","messageId":"a","snapshot":{},"timestamp":"2026-05-03T18:04:38.746Z"}
{"type":"file-history-snapshot","messageId":"b","snapshot":{},"timestamp":"2026-05-03T18:04:38.802Z"}
{"type":"progress","data":{"hookEvent":"SessionStart"},"timestamp":"2026-05-03T18:04:39.000Z"}
{"type":"system","data":{"foo":"bar"}}
`
	jsonlFilepath := filepath.Join(t.TempDir(), "metadata-only.jsonl")
	if err := os.WriteFile(jsonlFilepath, []byte(jsonlContent), 0644); err != nil {
		t.Fatalf("failed to write JSONL: %v", err)
	}

	for _, format := range []string{"text", "jsonl"} {
		t.Run(format, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := printSessionTo(jsonlFilepath, 0, true, format, &stdout, &stderr); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if format == "text" && stdout.Len() != 0 {
				t.Errorf("expected stdout to be empty for text format, got %q", stdout.String())
			}
			// jsonl format echoes raw lines, so stdout will contain the metadata
			// entries verbatim. We only assert the empty-message rule for the
			// text-format case where no formatted output is produced.

			if format == "text" {
				if !strings.Contains(stderr.String(), "no conversation messages yet") {
					t.Errorf("expected empty-session message in stderr, got %q", stderr.String())
				}
			}
		})
	}
}

// TestPrintSessionWithConversation verifies the normal happy path: when
// user/assistant entries are present, output goes to stdout and stderr stays
// empty.
func TestPrintSessionWithConversation(t *testing.T) {
	jsonlContent := `{"type":"file-history-snapshot","messageId":"a","snapshot":{},"timestamp":"2026-05-03T18:04:38.746Z"}
{"type":"user","message":{"role":"user","content":"Hello"},"timestamp":"2026-05-03T18:04:39.000Z"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hi there!"}]},"timestamp":"2026-05-03T18:04:40.000Z"}
`
	jsonlFilepath := filepath.Join(t.TempDir(), "with-content.jsonl")
	if err := os.WriteFile(jsonlFilepath, []byte(jsonlContent), 0644); err != nil {
		t.Fatalf("failed to write JSONL: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := printSessionTo(jsonlFilepath, 0, true, "text", &stdout, &stderr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(stdout.String(), "Hello") || !strings.Contains(stdout.String(), "Hi there!") {
		t.Errorf("expected conversation in stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("expected stderr to be empty for non-empty session, got %q", stderr.String())
	}
}
