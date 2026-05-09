package cmd

import (
	"testing"
	"time"
)

func TestFormatLastPrompt_NilReturnsDoubleDash(t *testing.T) {
	got := formatLastPrompt(nil, time.Now())
	if got != "--" {
		t.Fatalf("formatLastPrompt(nil, ...) = %q, want %q", got, "--")
	}
}

func TestFormatLastPrompt_NonNilReturnsLocalFormatted(t *testing.T) {
	ts := time.Date(2026, 5, 9, 14, 30, 0, 0, time.UTC)
	got := formatLastPrompt(&ts, time.Now())
	expected := ts.Local().Format("2006-01-02 15:04")
	if got != expected {
		t.Fatalf("formatLastPrompt(&t, ...) = %q, want %q", got, expected)
	}
}
