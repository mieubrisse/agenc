package database

import (
	"reflect"
	"testing"
)

// hostilePassthroughArgs is what a real per-mission passthrough list looks
// like: a multi-line, quote-bearing system prompt, a repeated flag, and a JSON
// value. Storing it on the mission row is what makes it survive reload,
// restart and the wrapper's own respawns, so the round-trip has to be exact.
var hostilePassthroughArgs = []string{
	"--append-system-prompt", "Tonight is for reflection.\nDon't open \"work\" topics.",
	"--add-dir", "/one",
	"--add-dir", "/two",
}

func TestCreateMissionPersistsClaudePassthroughArgs(t *testing.T) {
	db := openTestDB(t)

	created, err := db.CreateMission("github.com/owner/repo", &CreateMissionParams{
		ClaudePassthroughArgs: hostilePassthroughArgs,
	})
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	if !reflect.DeepEqual(created.ClaudePassthroughArgs, hostilePassthroughArgs) {
		t.Fatalf("expected the created mission to carry %q, got %q", hostilePassthroughArgs, created.ClaudePassthroughArgs)
	}

	got, err := db.GetMission(created.ID)
	if err != nil {
		t.Fatalf("failed to get mission: %v", err)
	}
	if got == nil {
		t.Fatal("expected the mission to be found")
	}
	if !reflect.DeepEqual(got.ClaudePassthroughArgs, hostilePassthroughArgs) {
		t.Fatalf("expected the stored mission to round-trip %q, got %q", hostilePassthroughArgs, got.ClaudePassthroughArgs)
	}
}

func TestCreateMissionWithoutClaudePassthroughArgsLeavesThemEmpty(t *testing.T) {
	db := openTestDB(t)

	created, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	got, err := db.GetMission(created.ID)
	if err != nil {
		t.Fatalf("failed to get mission: %v", err)
	}
	if len(got.ClaudePassthroughArgs) != 0 {
		t.Fatalf("expected no passthrough args, got %q", got.ClaudePassthroughArgs)
	}
}

// ListMissions and GetMission scan through separate code paths, so the list
// path needs its own assertion — a column added to one scanner and not the
// other reads back as nil without erroring.
func TestListMissionsCarriesClaudePassthroughArgs(t *testing.T) {
	db := openTestDB(t)

	created, err := db.CreateMission("github.com/owner/repo", &CreateMissionParams{
		ClaudePassthroughArgs: hostilePassthroughArgs,
	})
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	missions, err := db.ListMissions(ListMissionsParams{})
	if err != nil {
		t.Fatalf("failed to list missions: %v", err)
	}

	found := false
	for _, m := range missions {
		if m.ID != created.ID {
			continue
		}
		found = true
		if !reflect.DeepEqual(m.ClaudePassthroughArgs, hostilePassthroughArgs) {
			t.Fatalf("expected the listed mission to carry %q, got %q", hostilePassthroughArgs, m.ClaudePassthroughArgs)
		}
	}
	if !found {
		t.Fatalf("expected mission '%s' in the list", created.ID)
	}
}

// Passthrough args and the keyed Claude args are independent channels: one
// mission may carry either, both, or neither, and neither may leak into the
// other's column.
func TestCreateMissionKeepsClaudeArgChannelsIndependent(t *testing.T) {
	db := openTestDB(t)

	passthroughOnly, err := db.CreateMission("github.com/owner/repo", &CreateMissionParams{
		ClaudePassthroughArgs: []string{"--add-dir", "/one"},
	})
	if err != nil {
		t.Fatalf("failed to create the passthrough-only mission: %v", err)
	}
	keyedOnly, err := db.CreateMission("github.com/owner/repo", &CreateMissionParams{
		ClaudeArgs: map[string]string{"model": "opus"},
	})
	if err != nil {
		t.Fatalf("failed to create the keyed-args-only mission: %v", err)
	}

	gotPassthroughOnly, err := db.GetMission(passthroughOnly.ID)
	if err != nil {
		t.Fatalf("failed to get the passthrough-only mission: %v", err)
	}
	if len(gotPassthroughOnly.ClaudeArgs) != 0 {
		t.Fatalf("expected the passthrough-only mission to carry no keyed args, got %v", gotPassthroughOnly.ClaudeArgs)
	}

	gotKeyedOnly, err := db.GetMission(keyedOnly.ID)
	if err != nil {
		t.Fatalf("failed to get the keyed-args-only mission: %v", err)
	}
	if len(gotKeyedOnly.ClaudePassthroughArgs) != 0 {
		t.Fatalf("expected the keyed-args-only mission to carry no passthrough args, got %q", gotKeyedOnly.ClaudePassthroughArgs)
	}
}
