package cmd

import (
	"testing"
	"time"

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

func TestSortSearchRowsByRecency_NewestFirst(t *testing.T) {
	mk := func(id string, ts string) searchFzfRow {
		t.Helper()
		parsed, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			t.Fatalf("bad test timestamp %q: %v", ts, err)
		}
		return searchFzfRow{shortID: id, sortTime: parsed}
	}
	rows := []searchFzfRow{
		mk("old", "2026-06-17T13:04:00Z"),
		mk("new", "2026-09-05T23:13:00Z"),
		mk("mid", "2026-08-04T21:20:00Z"),
	}

	sortSearchRowsByRecency(rows)

	got := []string{rows[0].shortID, rows[1].shortID, rows[2].shortID}
	want := []string{"new", "mid", "old"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestSortSearchRowsByRecency_StableOnTies(t *testing.T) {
	ts := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	rows := []searchFzfRow{
		{shortID: "first", sortTime: ts},
		{shortID: "second", sortTime: ts},
	}

	sortSearchRowsByRecency(rows)

	if rows[0].shortID != "first" || rows[1].shortID != "second" {
		t.Fatalf("expected merge order preserved on ties, got %s,%s", rows[0].shortID, rows[1].shortID)
	}
}

func TestMissionRecency_PrefersLastUserPrompt(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	prompted := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)

	if got := missionRecency(&prompted, created); !got.Equal(prompted) {
		t.Fatalf("expected last-prompt time, got %v", got)
	}
	if got := missionRecency(nil, created); !got.Equal(created) {
		t.Fatalf("expected created-at fallback, got %v", got)
	}
}

func TestRecencyFromStrings(t *testing.T) {
	prompted := "2026-05-05T00:00:00Z"
	created := "2026-01-01T00:00:00Z"

	got := recencyFromStrings(&prompted, created)
	if got.Format(time.RFC3339) != prompted {
		t.Fatalf("expected %s, got %s", prompted, got.Format(time.RFC3339))
	}

	got = recencyFromStrings(nil, created)
	if got.Format(time.RFC3339) != created {
		t.Fatalf("expected %s, got %s", created, got.Format(time.RFC3339))
	}

	bad := "not-a-timestamp"
	if got := recencyFromStrings(&bad, "also-bad"); !got.IsZero() {
		t.Fatalf("expected zero time for unparseable input, got %v", got)
	}
}
