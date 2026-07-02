package session

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScanJSONLLines_LargeLine is the regression proof for GitHub issue #12.
// A single JSONL line larger than 1 MB must not cause the scan to abort.
func TestScanJSONLLines_LargeLine(t *testing.T) {
	dir := t.TempDir()
	jsonlFilepath := filepath.Join(dir, "session.jsonl")

	largePayload := strings.Repeat("a", 2*1024*1024) // 2 MB
	contents := "" +
		`{"type":"summary","summary":"first"}` + "\n" +
		`{"type":"user","payload":"` + largePayload + `"}` + "\n" +
		`{"type":"assistant","text":"last"}` + "\n"

	if err := os.WriteFile(jsonlFilepath, []byte(contents), 0644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	var lines [][]byte
	err := ScanJSONLLines(jsonlFilepath, func(line []byte) error {
		lines = append(lines, append([]byte(nil), line...))
		return nil
	})
	if err != nil {
		t.Fatalf("ScanJSONLLines returned error: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if !bytes.Contains(lines[1], []byte(largePayload)) {
		t.Fatalf("middle line does not contain the 2 MB payload")
	}
}

// TestScanJSONLLines_FinalLineWithoutNewline verifies the last line is yielded
// even if the file does not end with '\n'.
func TestScanJSONLLines_FinalLineWithoutNewline(t *testing.T) {
	dir := t.TempDir()
	jsonlFilepath := filepath.Join(dir, "session.jsonl")

	contents := `{"type":"user"}` + "\n" + `{"type":"assistant"}`
	if err := os.WriteFile(jsonlFilepath, []byte(contents), 0644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	var count int
	err := ScanJSONLLines(jsonlFilepath, func(line []byte) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("ScanJSONLLines returned error: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 lines, got %d", count)
	}
}

// TestScanJSONLLines_SkipsEmptyLines verifies that blank lines between records
// are silently skipped (fn is not invoked for them).
func TestScanJSONLLines_SkipsEmptyLines(t *testing.T) {
	dir := t.TempDir()
	jsonlFilepath := filepath.Join(dir, "session.jsonl")

	contents := `{"type":"user"}` + "\n\n" + `{"type":"assistant"}` + "\n"
	if err := os.WriteFile(jsonlFilepath, []byte(contents), 0644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	var count int
	err := ScanJSONLLines(jsonlFilepath, func(line []byte) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("ScanJSONLLines returned error: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 non-empty lines, got %d", count)
	}
}

// TestScanJSONLLines_CRLF verifies '\r' before '\n' is stripped.
func TestScanJSONLLines_CRLF(t *testing.T) {
	dir := t.TempDir()
	jsonlFilepath := filepath.Join(dir, "session.jsonl")

	contents := "{\"type\":\"user\"}\r\n{\"type\":\"assistant\"}\r\n"
	if err := os.WriteFile(jsonlFilepath, []byte(contents), 0644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	var seen [][]byte
	err := ScanJSONLLines(jsonlFilepath, func(line []byte) error {
		seen = append(seen, append([]byte(nil), line...))
		return nil
	})
	if err != nil {
		t.Fatalf("ScanJSONLLines returned error: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(seen))
	}
	for i, ln := range seen {
		if bytes.Contains(ln, []byte{'\r'}) {
			t.Fatalf("line %d still contains carriage return: %q", i, ln)
		}
	}
}

// TestScanJSONLLines_CallbackErrorStopsIteration verifies fn's error stops the
// scan and is returned unwrapped, so callers can use errors.Is on sentinel
// values.
func TestScanJSONLLines_CallbackErrorStopsIteration(t *testing.T) {
	dir := t.TempDir()
	jsonlFilepath := filepath.Join(dir, "session.jsonl")

	contents := `{"n":1}` + "\n" + `{"n":2}` + "\n" + `{"n":3}` + "\n"
	if err := os.WriteFile(jsonlFilepath, []byte(contents), 0644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	sentinel := errors.New("stop")
	var seen int
	err := ScanJSONLLines(jsonlFilepath, func(line []byte) error {
		seen++
		if seen == 2 {
			return sentinel
		}
		return nil
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if seen != 2 {
		t.Fatalf("expected iteration to stop after 2 lines, got %d", seen)
	}
}

// TestScanJSONLLines_MissingFile verifies a missing file returns a wrapped
// error (not a panic).
func TestScanJSONLLines_MissingFile(t *testing.T) {
	err := ScanJSONLLines(filepath.Join(t.TempDir(), "does-not-exist.jsonl"), func([]byte) error {
		return nil
	})
	if err == nil {
		t.Fatalf("expected error for missing file, got nil")
	}
}
