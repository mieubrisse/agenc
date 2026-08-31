package mission

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

// withFakeClaudeOnPath prepends a directory containing an executable named
// "claude" to PATH for the duration of the test, so exec.LookPath("claude")
// resolves deterministically regardless of whether the real binary is
// installed in the environment running the tests.
func withFakeClaudeOnPath(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	fakeClaude := filepath.Join(binDir, "claude")
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("failed to write fake claude binary: %v", err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)
}

// TestBuildClaudeCmdStateY is the faithful unit assertion of the State Y flip's
// env change: the production BuildClaudeCmd must set NO CLAUDE_CONFIG_DIR entry
// (Claude reads the real ~/.claude, or whatever the surrounding environment
// already sets). The e2e itself runs WITH CLAUDE_CONFIG_DIR set by the harness,
// so it cannot assert absence — this test can, by inspecting cmd.Env directly.
// It also asserts --settings <op-settings file> is passed and that an absent
// token file yields no CLAUDE_CODE_OAUTH_TOKEN injection.
func TestBuildClaudeCmdStateY(t *testing.T) {
	withFakeClaudeOnPath(t)

	// Isolate CLAUDE_CONFIG_DIR: even if the surrounding env sets it, os.Environ()
	// would carry it through. The production flip drops AgenC's own append, so
	// verify AgenC does not (re)inject it. Unset it here so any CLAUDE_CONFIG_DIR
	// entry in cmd.Env would have to come from AgenC's code, not the ambient env.
	// t.Setenv snapshots the original value and restores it on cleanup.
	t.Setenv("CLAUDE_CONFIG_DIR", "placeholder")
	if err := os.Unsetenv("CLAUDE_CONFIG_DIR"); err != nil {
		t.Fatalf("failed to unset CLAUDE_CONFIG_DIR: %v", err)
	}

	agencDirpath := t.TempDir()
	agentDirpath := t.TempDir()
	missionID := "test-mission-uuid"

	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, nil, nil)
	if err != nil {
		t.Fatalf("BuildClaudeCmd returned error: %v", err)
	}

	// (1) NO CLAUDE_CONFIG_DIR entry in the environment — the core of the flip.
	for _, env := range cmd.Env {
		if strings.HasPrefix(env, "CLAUDE_CONFIG_DIR=") {
			t.Errorf("cmd.Env must not contain CLAUDE_CONFIG_DIR under State Y, but found %q", env)
		}
	}

	// (2) --settings <op-settings file> is passed for the operational overlay.
	wantSettings := config.GetMissionOpSettingsFilepath(agencDirpath, missionID)
	foundSettings := false
	for i, arg := range cmd.Args {
		if arg == "--settings" {
			if i+1 >= len(cmd.Args) {
				t.Fatalf("--settings present but has no value in args %v", cmd.Args)
			}
			if cmd.Args[i+1] != wantSettings {
				t.Errorf("--settings value = %q, want %q", cmd.Args[i+1], wantSettings)
			}
			foundSettings = true
		}
	}
	if !foundSettings {
		t.Errorf("cmd.Args missing --settings; got %v", cmd.Args)
	}

	// (3) No token file present → no CLAUDE_CODE_OAUTH_TOKEN injection.
	for _, env := range cmd.Env {
		if strings.HasPrefix(env, "CLAUDE_CODE_OAUTH_TOKEN=") {
			t.Errorf("cmd.Env must not inject CLAUDE_CODE_OAUTH_TOKEN when no token file exists, but found %q", env)
		}
	}

	// AGENC_MISSION_UUID is still set.
	wantMissionEnv := config.MissionUUIDEnvVar + "=" + missionID
	foundMissionEnv := false
	for _, env := range cmd.Env {
		if env == wantMissionEnv {
			foundMissionEnv = true
		}
	}
	if !foundMissionEnv {
		t.Errorf("cmd.Env missing %q; got %v", wantMissionEnv, cmd.Env)
	}
}

// TestBuildClaudeCmdInjectsTokenWhenPresent verifies the machine-token fallback:
// when a token file is present, CLAUDE_CODE_OAUTH_TOKEN is injected.
func TestBuildClaudeCmdInjectsTokenWhenPresent(t *testing.T) {
	withFakeClaudeOnPath(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "placeholder")
	if err := os.Unsetenv("CLAUDE_CONFIG_DIR"); err != nil {
		t.Fatalf("failed to unset CLAUDE_CONFIG_DIR: %v", err)
	}

	agencDirpath := t.TempDir()
	agentDirpath := t.TempDir()
	missionID := "test-mission-uuid"

	const token = "sk-ant-test-token"
	if err := config.WriteOAuthToken(agencDirpath, token); err != nil {
		t.Fatalf("failed to write token: %v", err)
	}

	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, nil, nil)
	if err != nil {
		t.Fatalf("BuildClaudeCmd returned error: %v", err)
	}

	found := false
	for _, env := range cmd.Env {
		if env == "CLAUDE_CODE_OAUTH_TOKEN="+token {
			found = true
		}
	}
	if !found {
		t.Errorf("cmd.Env must inject CLAUDE_CODE_OAUTH_TOKEN=%q when token file present; got %v", token, cmd.Env)
	}
}

func TestBuildResumeArgs(t *testing.T) {
	tests := []struct {
		name          string
		sessionID     string
		initialPrompt string
		want          []string
	}{
		{
			name:          "session id, no prompt",
			sessionID:     "sess-abc",
			initialPrompt: "",
			want:          []string{"-r", "sess-abc"},
		},
		{
			name:          "no session id, no prompt",
			sessionID:     "",
			initialPrompt: "",
			want:          []string{"-c"},
		},
		{
			name:          "session id with prompt",
			sessionID:     "sess-abc",
			initialPrompt: "follow up please",
			want:          []string{"-r", "sess-abc", "follow up please"},
		},
		{
			name:          "no session id with prompt",
			sessionID:     "",
			initialPrompt: "follow up please",
			want:          []string{"-c", "follow up please"},
		},
		{
			name:          "prompt with shell metachars preserved literally",
			sessionID:     "sess-abc",
			initialPrompt: "$(rm -rf /); echo 'hi'",
			want:          []string{"-r", "sess-abc", "$(rm -rf /); echo 'hi'"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildResumeArgs(tt.sessionID, tt.initialPrompt)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildResumeArgs(%q, %q) = %v, want %v", tt.sessionID, tt.initialPrompt, got, tt.want)
			}
		})
	}
}

// TestBuildClaudeCmdArgOrder pins the argv layout BuildClaudeCmd produces:
// AgenC's operational overlay first, then the merged model/config/per-mission
// args, then the call-site args that choose the conversation shape. The order
// is behaviour, not incidental — a per-mission override that landed before the
// resolved model would be silently outranked by Claude's last-wins parsing.
func TestBuildClaudeCmdArgOrder(t *testing.T) {
	withFakeClaudeOnPath(t)

	agencDirpath := t.TempDir()
	agentDirpath := t.TempDir()
	missionID := "test-mission-uuid"

	mergedClaudeArgs := MergeClaudeArgs("sonnet", []string{"--chrome"}, map[string]string{"model": "opus"})
	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, mergedClaudeArgs, []string{"-c"})
	if err != nil {
		t.Fatalf("BuildClaudeCmd returned error: %v", err)
	}

	// cmd.Args[0] is the claude binary path itself.
	gotArgs := cmd.Args[1:]
	opSettingsFilepath := config.GetMissionOpSettingsFilepath(agencDirpath, missionID)
	expectedArgs := []string{
		"--settings", opSettingsFilepath,
		"--chrome",
		"--model", "opus",
		"-c",
	}
	if !reflect.DeepEqual(gotArgs, expectedArgs) {
		t.Fatalf("expected args %v, got %v", expectedArgs, gotArgs)
	}
}
