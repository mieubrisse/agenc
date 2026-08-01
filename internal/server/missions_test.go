package server

import (
	"os"
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

func TestBuildWrapperResumeCmd_NoPromptOmitsFlag(t *testing.T) {
	s := &Server{agencDirpath: "/tmp/agenc-test"}
	cmd, err := s.buildWrapperResumeCmd("mission-id-123", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(cmd, "--prompt") {
		t.Errorf("empty prompt should not produce --prompt flag, got: %q", cmd)
	}
	if !strings.Contains(cmd, "mission resume --run-wrapper mission-id-123") {
		t.Errorf("missing resume invocation, got: %q", cmd)
	}
}

func TestBuildWrapperResumeCmd_PromptThreadsThrough(t *testing.T) {
	s := &Server{agencDirpath: "/tmp/agenc-test"}
	cmd, err := s.buildWrapperResumeCmd("mission-id-123", "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(cmd, "--prompt 'hello world'") {
		t.Errorf("expected --prompt 'hello world' in command, got: %q", cmd)
	}
}

func TestBuildWrapperResumeCmd_EscapesSingleQuotes(t *testing.T) {
	s := &Server{agencDirpath: "/tmp/agenc-test"}
	cmd, err := s.buildWrapperResumeCmd("mission-id-123", "don't 'do' it")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `--prompt 'don'\''t '\''do'\'' it'`
	if !strings.Contains(cmd, want) {
		t.Errorf("expected escaped form %q in command, got: %q", want, cmd)
	}
}

func TestBuildWrapperResumeCmd_PreservesShellMetachars(t *testing.T) {
	s := &Server{agencDirpath: "/tmp/agenc-test"}
	payload := "$(rm -rf /); echo hi && `whoami`"
	cmd, err := s.buildWrapperResumeCmd("mission-id-123", payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "--prompt '" + payload + "'"
	if !strings.Contains(cmd, want) {
		t.Errorf("expected literal preservation %q in command, got: %q", want, cmd)
	}
}

func TestParseCronSourceMetadata_Empty(t *testing.T) {
	name, trigger := parseCronSourceMetadata("")
	if name != "" || trigger != "" {
		t.Fatalf("expected empty values, got name='%v' trigger='%v'", name, trigger)
	}
}

func TestParseCronSourceMetadata_Malformed(t *testing.T) {
	name, trigger := parseCronSourceMetadata("{not valid json")
	if name != "" || trigger != "" {
		t.Fatalf("expected empty values for malformed JSON, got name='%v' trigger='%v'", name, trigger)
	}
}

func TestParseCronSourceMetadata_ExtractsFields(t *testing.T) {
	name, trigger := parseCronSourceMetadata(`{"cron_name":"daily-review","trigger":"manual"}`)
	if name != "daily-review" {
		t.Errorf("expected name 'daily-review', got '%v'", name)
	}
	if trigger != "manual" {
		t.Errorf("expected trigger 'manual', got '%v'", trigger)
	}
}

func TestParseCronSourceMetadata_MissingCronName(t *testing.T) {
	name, trigger := parseCronSourceMetadata(`{"trigger":"manual"}`)
	if name != "" {
		t.Errorf("expected empty name, got '%v'", name)
	}
	if trigger != "manual" {
		t.Errorf("expected trigger 'manual', got '%v'", trigger)
	}
}

func TestBuildCronTriggeredNotification_FullMetadata(t *testing.T) {
	mission := &database.Mission{ID: "mid-full-uuid", ShortID: "mid-shrt", GitRepo: "owner/repo"}
	req := CreateMissionRequest{Source: "cron", SourceID: "cron-id-1", Prompt: "do the thing"}
	n := buildCronTriggeredNotification(mission, req, "daily-review", "")

	if n.Kind != "cron.triggered" {
		t.Errorf("kind: want 'cron.triggered', got '%v'", n.Kind)
	}
	if n.Title != "Cron triggered: daily-review" {
		t.Errorf("title: want 'Cron triggered: daily-review', got '%v'", n.Title)
	}
	if n.MissionID == nil || *n.MissionID != "mid-full-uuid" {
		t.Errorf("MissionID mismatch: %v", n.MissionID)
	}
	if !strings.Contains(n.BodyMarkdown, "**Cron:** daily-review") {
		t.Errorf("body missing cron name: %v", n.BodyMarkdown)
	}
	if !strings.Contains(n.BodyMarkdown, "**Mission:** mid-shrt") {
		t.Errorf("body missing mission short ID: %v", n.BodyMarkdown)
	}
	if !strings.Contains(n.BodyMarkdown, "**Trigger:** scheduled") {
		t.Errorf("body should default trigger to 'scheduled': %v", n.BodyMarkdown)
	}
	if !strings.Contains(n.BodyMarkdown, "**Repo:** owner/repo") {
		t.Errorf("body missing repo: %v", n.BodyMarkdown)
	}
	if !strings.Contains(n.BodyMarkdown, "do the thing") {
		t.Errorf("body missing prompt: %v", n.BodyMarkdown)
	}
}

func TestBuildCronTriggeredNotification_FallsBackToSourceIDWhenNameMissing(t *testing.T) {
	mission := &database.Mission{ID: "mid-fallback-uuid", ShortID: "mid-fall"}
	req := CreateMissionRequest{Source: "cron", SourceID: "cron-id-fallback"}
	n := buildCronTriggeredNotification(mission, req, "", "")

	if n.Title != "Cron triggered: cron-id-fallback" {
		t.Errorf("expected fallback to source ID, got '%v'", n.Title)
	}
}

func TestBuildCronTriggeredNotification_ManualTrigger(t *testing.T) {
	mission := &database.Mission{ID: "m1", ShortID: "m1"}
	req := CreateMissionRequest{Source: "cron", SourceID: "cid"}
	n := buildCronTriggeredNotification(mission, req, "name", "manual")

	if !strings.Contains(n.BodyMarkdown, "**Trigger:** manual") {
		t.Errorf("expected manual trigger label: %v", n.BodyMarkdown)
	}
}

func TestCronWantsNotifications(t *testing.T) {
	disabled := false
	enabled := true
	cfg := &config.AgencConfig{
		Crons: map[string]config.CronConfig{
			"silent": {ID: "cron-silent", NotificationsEnabled: &disabled},
			"loud":   {ID: "cron-loud", NotificationsEnabled: &enabled},
			"legacy": {ID: "cron-legacy"},
		},
	}
	s := &Server{}
	s.cachedConfig.Store(cfg)

	tests := []struct {
		name   string
		cronID string
		want   bool
	}{
		{"disabled cron suppresses notification", "cron-silent", false},
		{"explicitly enabled cron keeps notification", "cron-loud", true},
		{"unset field defaults to enabled", "cron-legacy", true},
		{"unknown cron ID defaults to enabled", "cron-unknown", true},
		{"empty cron ID defaults to enabled", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.cronWantsNotifications(tt.cronID); got != tt.want {
				t.Errorf("cronWantsNotifications(%q) = %v, want %v", tt.cronID, got, tt.want)
			}
		})
	}
}

func TestBuildCronTriggeredNotification_TruncatesLongPrompt(t *testing.T) {
	mission := &database.Mission{ID: "m1", ShortID: "m1"}
	prompt := strings.Repeat("p", cronPromptPreviewMaxBytes+50)
	req := CreateMissionRequest{Source: "cron", SourceID: "cid", Prompt: prompt}
	n := buildCronTriggeredNotification(mission, req, "name", "")

	if !strings.Contains(n.BodyMarkdown, "…") {
		t.Errorf("expected truncation marker, got: %v", n.BodyMarkdown)
	}
}

// --- tmuxEnvPrefix tests ---

// TestTmuxEnvPrefix_DefaultPath verifies that the default agenc installation
// (the real ~/.agenc path) produces an empty prefix (no export needed).
func TestTmuxEnvPrefix_DefaultPath(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot determine home dir: %v", err)
	}
	s := &Server{agencDirpath: homeDir + "/.agenc"}
	prefix := s.tmuxEnvPrefix()
	if prefix != "" {
		t.Errorf("expected empty prefix for default path, got %q", prefix)
	}
}

// TestTmuxEnvPrefix_NonDefaultPath verifies that a non-default agencDirpath
// produces a prefix that exports AGENC_DIRPATH.
func TestTmuxEnvPrefix_NonDefaultPath(t *testing.T) {
	s := &Server{agencDirpath: "/tmp/agenc-test-nondefault"}
	prefix := s.tmuxEnvPrefix()
	if prefix == "" {
		t.Fatal("expected non-empty prefix for non-default path")
	}
	if !strings.Contains(prefix, "AGENC_DIRPATH='/tmp/agenc-test-nondefault'") {
		t.Errorf("expected AGENC_DIRPATH in prefix, got %q", prefix)
	}
}

// TestTmuxEnvPrefix_ClaudeConfigDirForwarded verifies that when CLAUDE_CONFIG_DIR
// is set, it is included in the tmux env prefix so that the spawned wrapper
// process inherits the same config directory.
func TestTmuxEnvPrefix_ClaudeConfigDirForwarded(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/test-claude-config")
	s := &Server{agencDirpath: "/tmp/agenc-test-nondefault"}
	prefix := s.tmuxEnvPrefix()
	if !strings.Contains(prefix, "CLAUDE_CONFIG_DIR='/tmp/test-claude-config'") {
		t.Errorf("expected CLAUDE_CONFIG_DIR in prefix, got %q", prefix)
	}
}

// TestTmuxEnvPrefix_ClaudeConfigDirAbsentWhenUnset verifies that when
// CLAUDE_CONFIG_DIR is not set, it is NOT included in the prefix (production
// default: unset env produces no extra export).
func TestTmuxEnvPrefix_ClaudeConfigDirAbsentWhenUnset(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	s := &Server{agencDirpath: "/tmp/agenc-test-nondefault"}
	prefix := s.tmuxEnvPrefix()
	if strings.Contains(prefix, "CLAUDE_CONFIG_DIR") {
		t.Errorf("expected no CLAUDE_CONFIG_DIR in prefix when env is unset, got %q", prefix)
	}
}
