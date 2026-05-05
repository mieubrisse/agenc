package server

import (
	"strings"
	"testing"
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
