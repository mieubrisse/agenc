package database

import (
	"reflect"
	"testing"
)

func TestCreateMissionPersistsClaudeArgs(t *testing.T) {
	db := openTestDB(t)

	claudeArgs := map[string]string{"model": "opus", "effort": "high"}
	created, err := db.CreateMission("github.com/owner/repo", &CreateMissionParams{ClaudeArgs: claudeArgs})
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	if !reflect.DeepEqual(created.ClaudeArgs, claudeArgs) {
		t.Fatalf("expected the created mission to carry %v, got %v", claudeArgs, created.ClaudeArgs)
	}

	got, err := db.GetMission(created.ID)
	if err != nil {
		t.Fatalf("failed to get mission: %v", err)
	}
	if got == nil {
		t.Fatal("expected the mission to be found")
	}
	if !reflect.DeepEqual(got.ClaudeArgs, claudeArgs) {
		t.Fatalf("expected the stored mission to round-trip %v, got %v", claudeArgs, got.ClaudeArgs)
	}
}

func TestCreateMissionWithoutClaudeArgsLeavesThemEmpty(t *testing.T) {
	db := openTestDB(t)

	created, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	got, err := db.GetMission(created.ID)
	if err != nil {
		t.Fatalf("failed to get mission: %v", err)
	}
	if len(got.ClaudeArgs) != 0 {
		t.Fatalf("expected no Claude args, got %v", got.ClaudeArgs)
	}
}

func TestListMissionsCarriesClaudeArgs(t *testing.T) {
	db := openTestDB(t)

	claudeArgs := map[string]string{"model": "opus"}
	created, err := db.CreateMission("github.com/owner/repo", &CreateMissionParams{ClaudeArgs: claudeArgs})
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	missions, err := db.ListMissions(ListMissionsParams{})
	if err != nil {
		t.Fatalf("failed to list missions: %v", err)
	}

	for _, m := range missions {
		if m.ID != created.ID {
			continue
		}
		if !reflect.DeepEqual(m.ClaudeArgs, claudeArgs) {
			t.Fatalf("expected the listed mission to carry %v, got %v", claudeArgs, m.ClaudeArgs)
		}
		return
	}
	t.Fatal("expected the created mission to appear in the list")
}
