package cmd

import (
	"testing"

	"github.com/odyssey/agenc/internal/database"
)

func TestMatchMissionSubstring_RepoHit(t *testing.T) {
	m := &database.Mission{GitRepo: "github.com/Foo/Bar"}
	if !matchMissionSubstring(m, "foo/bar") {
		t.Fatal("expected case-insensitive match against GitRepo")
	}
}

func TestMatchMissionSubstring_PromptHit(t *testing.T) {
	m := &database.Mission{Prompt: "Authentication feature", GitRepo: "x"}
	if !matchMissionSubstring(m, "auth") {
		t.Fatal("expected case-insensitive substring match against Prompt")
	}
}

func TestMatchMissionSubstring_TitleHitViaPromptFallback(t *testing.T) {
	// resolveSessionName falls back to Prompt when no custom/auto title is
	// available. So a hit against the prompt-derived title is functionally
	// the same as a Prompt hit, but we exercise the title path explicitly
	// here to lock in the contract.
	m := &database.Mission{Prompt: "implement caching layer"}
	if !matchMissionSubstring(m, "caching") {
		t.Fatal("expected match against title (resolved via Prompt fallback)")
	}
}

func TestMatchMissionSubstring_NoHit(t *testing.T) {
	m := &database.Mission{Prompt: "x", GitRepo: "y"}
	if matchMissionSubstring(m, "nothing") {
		t.Fatal("expected no match")
	}
}

func TestMatchMissionSubstring_EmptyFieldsDoNotFalseMatch(t *testing.T) {
	m := &database.Mission{Prompt: "", GitRepo: ""}
	if matchMissionSubstring(m, "anything") {
		t.Fatal("empty fields should not match a non-empty query")
	}
}
