package database

import (
	"reflect"
	"testing"
)

// hostileMissionEnv is what a real per-mission environment can look like: a
// bare marker alongside a value carrying spaces, both quote styles and a
// newline. Storing it on the mission row is what makes it survive reload,
// restart and the wrapper's own respawns, so the round-trip has to be exact.
var hostileMissionEnv = map[string]string{
	"REFLECTION_SESSION": "1",
	"REFLECTION_NOTE":    "Tonight is for reflection.\nDon't open \"work\" topics.",
}

func TestCreateMissionPersistsClaudeEnv(t *testing.T) {
	db := openTestDB(t)

	created, err := db.CreateMission("github.com/owner/repo", &CreateMissionParams{
		ClaudeEnv: hostileMissionEnv,
	})
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	if !reflect.DeepEqual(created.ClaudeEnv, hostileMissionEnv) {
		t.Fatalf("expected the created mission to carry %v, got %v", hostileMissionEnv, created.ClaudeEnv)
	}

	got, err := db.GetMission(created.ID)
	if err != nil {
		t.Fatalf("failed to get mission: %v", err)
	}
	if got == nil {
		t.Fatal("expected the mission to be found")
	}
	if !reflect.DeepEqual(got.ClaudeEnv, hostileMissionEnv) {
		t.Fatalf("expected the stored mission to round-trip %v, got %v", hostileMissionEnv, got.ClaudeEnv)
	}
}

func TestCreateMissionWithoutClaudeEnvLeavesItEmpty(t *testing.T) {
	db := openTestDB(t)

	created, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	got, err := db.GetMission(created.ID)
	if err != nil {
		t.Fatalf("failed to get mission: %v", err)
	}
	if len(got.ClaudeEnv) != 0 {
		t.Fatalf("expected no mission environment, got %v", got.ClaudeEnv)
	}
}

// ListMissions and GetMission scan through separate code paths, so the list
// path needs its own assertion — a column added to one scanner and not the
// other reads back as nil without erroring.
func TestListMissionsCarriesClaudeEnv(t *testing.T) {
	db := openTestDB(t)

	created, err := db.CreateMission("github.com/owner/repo", &CreateMissionParams{
		ClaudeEnv: hostileMissionEnv,
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
		if !reflect.DeepEqual(m.ClaudeEnv, hostileMissionEnv) {
			t.Fatalf("expected the listed mission to carry %v, got %v", hostileMissionEnv, m.ClaudeEnv)
		}
	}
	if !found {
		t.Fatalf("expected mission '%s' in the list", created.ID)
	}
}

// The environment is a third independent channel: a mission may carry it, the
// two arg channels, any combination, or none, and none may leak into another's
// column. This is what would notice a column added to the INSERT in the wrong
// position, which stores silently and reads back as the neighbour's value.
func TestCreateMissionKeepsClaudeEnvIndependentOfTheArgChannels(t *testing.T) {
	db := openTestDB(t)

	envOnly, err := db.CreateMission("github.com/owner/repo", &CreateMissionParams{
		ClaudeEnv: map[string]string{"REFLECTION_SESSION": "1"},
	})
	if err != nil {
		t.Fatalf("failed to create the env-only mission: %v", err)
	}
	argsOnly, err := db.CreateMission("github.com/owner/repo", &CreateMissionParams{
		ClaudeArgs:            map[string]string{"model": "opus"},
		ClaudePassthroughArgs: []string{"--add-dir", "/one"},
	})
	if err != nil {
		t.Fatalf("failed to create the args-only mission: %v", err)
	}

	gotEnvOnly, err := db.GetMission(envOnly.ID)
	if err != nil {
		t.Fatalf("failed to get the env-only mission: %v", err)
	}
	if len(gotEnvOnly.ClaudeArgs) != 0 {
		t.Fatalf("expected the env-only mission to carry no keyed args, got %v", gotEnvOnly.ClaudeArgs)
	}
	if len(gotEnvOnly.ClaudePassthroughArgs) != 0 {
		t.Fatalf("expected the env-only mission to carry no passthrough args, got %q", gotEnvOnly.ClaudePassthroughArgs)
	}

	gotArgsOnly, err := db.GetMission(argsOnly.ID)
	if err != nil {
		t.Fatalf("failed to get the args-only mission: %v", err)
	}
	if len(gotArgsOnly.ClaudeEnv) != 0 {
		t.Fatalf("expected the args-only mission to carry no environment, got %v", gotArgsOnly.ClaudeEnv)
	}
}

// A mission that carries all three channels reads all three back, which is the
// positional-column assertion the single-channel tests above cannot make: a
// scanner reading the columns in the wrong order would still populate every
// field, just with each other's values.
func TestCreateMissionCarriesAllThreeChannelsTogether(t *testing.T) {
	db := openTestDB(t)

	claudeArgs := map[string]string{"model": "opus"}
	claudePassthroughArgs := []string{"--add-dir", "/one"}

	created, err := db.CreateMission("github.com/owner/repo", &CreateMissionParams{
		ClaudeArgs:            claudeArgs,
		ClaudePassthroughArgs: claudePassthroughArgs,
		ClaudeEnv:             hostileMissionEnv,
	})
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	got, err := db.GetMission(created.ID)
	if err != nil {
		t.Fatalf("failed to get mission: %v", err)
	}
	if !reflect.DeepEqual(got.ClaudeArgs, claudeArgs) {
		t.Errorf("expected keyed args %v, got %v", claudeArgs, got.ClaudeArgs)
	}
	if !reflect.DeepEqual(got.ClaudePassthroughArgs, claudePassthroughArgs) {
		t.Errorf("expected passthrough args %q, got %q", claudePassthroughArgs, got.ClaudePassthroughArgs)
	}
	if !reflect.DeepEqual(got.ClaudeEnv, hostileMissionEnv) {
		t.Errorf("expected environment %v, got %v", hostileMissionEnv, got.ClaudeEnv)
	}
}
