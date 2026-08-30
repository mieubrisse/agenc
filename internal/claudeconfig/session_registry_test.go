package claudeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// writeRegistryRecord writes a Claude Code session-registry record shaped like
// the real thing — extra fields included, so the test proves the reader
// tolerates fields AgenC does not model.
func writeRegistryRecord(t *testing.T, registryDirpath string, pid int, cwd string, peerName string, tmuxTarget string) {
	t.Helper()
	record := map[string]any{
		"pid":           pid,
		"sessionId":     "8e0d0a2e-0000-4000-8000-000000000000",
		"cwd":           cwd,
		"version":       "2.1.224",
		"peerProtocol":  1,
		"kind":          "interactive",
		"tmux":          tmuxTarget,
		"name":          peerName,
		"nameSource":    "derived",
		"status":        "idle",
		"bridgeSession": "session_abc",
	}
	recordBytes, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("failed to marshal registry record: %v", err)
	}
	recordFilepath := filepath.Join(registryDirpath, strconv.Itoa(pid)+".json")
	if err := os.WriteFile(recordFilepath, recordBytes, 0644); err != nil {
		t.Fatalf("failed to write registry record %q: %v", recordFilepath, err)
	}
}

func TestListMissionSessionsInDir_MatchesMissionsAndSkipsEverythingElse(t *testing.T) {
	registryDirpath := t.TempDir()
	agencDirpath := t.TempDir()
	missionsDirpath := filepath.Join(agencDirpath, "missions")

	missionID := "dd09fd51-5d07-4125-814c-3c9f24f3f247"
	writeRegistryRecord(t, registryDirpath, 1001,
		filepath.Join(missionsDirpath, missionID, "agent"), "agent-da", "hyperspace:@126.%134")

	// A Claude session running outside the agenc directory entirely.
	writeRegistryRecord(t, registryDirpath, 1002,
		filepath.Join(agencDirpath, "elsewhere"), "agent-99", "cockpit:@1.%2")

	// A nested agenc installation living inside a mission's agent directory:
	// shares the missions/ prefix but carries extra path segments.
	writeRegistryRecord(t, registryDirpath, 1003,
		filepath.Join(missionsDirpath, missionID, "agent", "_test-env", "missions", "other-mission", "agent"),
		"agent-77", "cockpit:@3.%4")

	// The mission directory itself rather than its agent/ subdirectory.
	writeRegistryRecord(t, registryDirpath, 1004,
		filepath.Join(missionsDirpath, missionID), "agent-66", "cockpit:@5.%6")

	// Entries the reader must skip rather than choke on.
	if err := os.WriteFile(filepath.Join(registryDirpath, "9999.json"), []byte("{not json"), 0644); err != nil {
		t.Fatalf("failed to write malformed record: %v", err)
	}
	if err := os.WriteFile(filepath.Join(registryDirpath, "notes.txt"), []byte("ignore me"), 0644); err != nil {
		t.Fatalf("failed to write non-record file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(registryDirpath, "subdir.json"), 0755); err != nil {
		t.Fatalf("failed to create directory entry: %v", err)
	}

	missionSessions, err := listMissionSessionsInDir(registryDirpath, agencDirpath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(missionSessions) != 1 {
		t.Fatalf("expected exactly 1 mission session, got %d: %+v", len(missionSessions), missionSessions)
	}

	got := missionSessions[0]
	if got.MissionID != missionID {
		t.Errorf("got mission ID %q, want %q", got.MissionID, missionID)
	}
	if got.PID != 1001 {
		t.Errorf("got PID %d, want 1001", got.PID)
	}
	if got.PeerName != "agent-da" {
		t.Errorf("got peer name %q, want %q", got.PeerName, "agent-da")
	}
	if got.TmuxTarget != "hyperspace:@126.%134" {
		t.Errorf("got tmux target %q, want %q", got.TmuxTarget, "hyperspace:@126.%134")
	}
}

func TestListMissionSessionsInDir_RelativeAgencDirpathStillMatches(t *testing.T) {
	registryDirpath := t.TempDir()
	agencDirpath := t.TempDir()

	// Mirror the test environment, where AGENC_DIRPATH is relative to the
	// process's working directory while the registry records absolute paths.
	t.Chdir(agencDirpath)

	missionID := "f2f47c6a-638b-4892-988c-a82041d27da9"
	absAgentDirpath, err := filepath.Abs(filepath.Join("_test-env", "missions", missionID, "agent"))
	if err != nil {
		t.Fatalf("failed to absolutize agent dirpath: %v", err)
	}
	writeRegistryRecord(t, registryDirpath, 2001, absAgentDirpath, "agent-5e", "hyperspace:@156.%165")

	missionSessions, err := listMissionSessionsInDir(registryDirpath, "_test-env")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(missionSessions) != 1 {
		t.Fatalf("expected exactly 1 mission session, got %d: %+v", len(missionSessions), missionSessions)
	}
	if missionSessions[0].MissionID != missionID {
		t.Errorf("got mission ID %q, want %q", missionSessions[0].MissionID, missionID)
	}
}

func TestListMissionSessionsInDir_AbsentRegistryIsNotAnError(t *testing.T) {
	missionSessions, err := listMissionSessionsInDir(filepath.Join(t.TempDir(), "never-created"), t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(missionSessions) != 0 {
		t.Errorf("expected no mission sessions, got %+v", missionSessions)
	}
}
