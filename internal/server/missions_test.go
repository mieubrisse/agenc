package server

import (
	"strings"
	"testing"

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

func TestBuildCronTriggeredNotification_TruncatesLongPrompt(t *testing.T) {
	mission := &database.Mission{ID: "m1", ShortID: "m1"}
	prompt := strings.Repeat("p", cronPromptPreviewMaxBytes+50)
	req := CreateMissionRequest{Source: "cron", SourceID: "cid", Prompt: prompt}
	n := buildCronTriggeredNotification(mission, req, "name", "")

	if !strings.Contains(n.BodyMarkdown, "…") {
		t.Errorf("expected truncation marker, got: %v", n.BodyMarkdown)
	}
}
