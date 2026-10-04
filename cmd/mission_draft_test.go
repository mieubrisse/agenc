package cmd

import (
	"slices"
	"testing"
)

// The e2e suite cannot cover this. Its target is a mission's own pane, so asserting on what the
// paste delivered means asserting on the receiving program's rendering -- and Claude does not
// reliably launch in the test environment, which the suite already records as a documented skip.
// Pinning the invariant here instead follows the same approach the repo takes for the `mission
// peers` and `mission search` formatters.
//
// What this guards: -p is the difference between a long draft arriving whole and arriving
// truncated, and nothing about the flag's name suggests that. See buildPasteBufferArgs for the
// mechanism and the measured byte counts.
func TestBuildPasteBufferArgs_PastesAsBracketedPaste(t *testing.T) {
	args := buildPasteBufferArgs("%42")

	if !slices.Contains(args, "-p") {
		t.Fatalf("paste must be a bracketed paste, but -p is missing from %q; a draft over ~1KB "+
			"spans more than one pty read and silently loses everything but its last chunk", args)
	}
}

func TestBuildPasteBufferArgs_TargetsTheRequestedPane(t *testing.T) {
	args := buildPasteBufferArgs("%42")

	targetIdx := slices.Index(args, "-t")
	if targetIdx == -1 {
		t.Fatalf("expected a -t target flag in %q", args)
	}
	if targetIdx == len(args)-1 {
		t.Fatalf("-t is the final argument in %q, so it carries no pane", args)
	}
	if got := args[targetIdx+1]; got != "%42" {
		t.Fatalf("expected -t to target pane %q, got %q", "%42", got)
	}
}

// -r would stop tmux rewriting the draft's newlines into carriage returns. Claude Code normalises
// those inside a bracketed paste, so passing it changes nothing -- and a future maintainer adding
// it should have to state why rather than acquiring it by accident.
func TestBuildPasteBufferArgs_DoesNotSuppressNewlineRewriting(t *testing.T) {
	args := buildPasteBufferArgs("%42")

	if slices.Contains(args, "-r") {
		t.Fatalf("unexpected -r in %q; see buildPasteBufferArgs for why it is deliberately absent", args)
	}
}
