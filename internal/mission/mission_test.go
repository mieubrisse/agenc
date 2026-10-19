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

	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, nil, nil, nil)
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

	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, nil, nil, nil)
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
			want:          []string{"-r", "sess-abc", "--", "follow up please"},
		},
		{
			name:          "no session id with prompt",
			sessionID:     "",
			initialPrompt: "follow up please",
			want:          []string{"-c", "--", "follow up please"},
		},
		{
			name:          "prompt with shell metachars preserved literally",
			sessionID:     "sess-abc",
			initialPrompt: "$(rm -rf /); echo 'hi'",
			want:          []string{"-r", "sess-abc", "--", "$(rm -rf /); echo 'hi'"},
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

	mergedClaudeArgs := MergeClaudeArgs(ClaudeArgSources{
		ResolvedModel:     "sonnet",
		ConfigClaudeArgs:  []string{"--chrome"},
		MissionClaudeArgs: map[string]string{"model": "opus"},
	})
	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, mergedClaudeArgs, []string{"-c"}, nil)
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

// TestBuildClaudeCmdCarriesHostilePassthroughValueIntact is the core assertion
// behind the passthrough design: a Claude flag value containing spaces, both
// quote styles and newlines reaches the Claude process as exactly one argv
// element, byte-for-byte as written.
//
// It holds by construction rather than by escaping. BuildClaudeCmd hands a
// []string to exec.Command, which execs the binary directly and never invokes a
// shell, and the args reach it from the mission record rather than from a
// command string — so there is no layer that could mangle a quote and no
// escaping that could be got wrong. This test is what would notice if a future
// change routed them through a shell after all.
func TestBuildClaudeCmdCarriesHostilePassthroughValueIntact(t *testing.T) {
	withFakeClaudeOnPath(t)

	agencDirpath := t.TempDir()
	agentDirpath := t.TempDir()
	missionID := "test-mission-uuid"

	mergedClaudeArgs := MergeClaudeArgs(ClaudeArgSources{
		ClaudePassthroughArgs: []string{"--append-system-prompt", hostileClaudeArgValue},
	})
	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, mergedClaudeArgs, nil, nil)
	if err != nil {
		t.Fatalf("BuildClaudeCmd returned error: %v", err)
	}

	// cmd.Args[0] is the claude binary path itself.
	gotArgs := cmd.Args[1:]
	opSettingsFilepath := config.GetMissionOpSettingsFilepath(agencDirpath, missionID)
	expectedArgs := []string{
		"--settings", opSettingsFilepath,
		"--append-system-prompt", hostileClaudeArgValue,
	}
	if !reflect.DeepEqual(gotArgs, expectedArgs) {
		t.Fatalf("expected args %v, got %v", expectedArgs, gotArgs)
	}
}

// TestBuildClaudeCmdInjectsMissionEnv is the headline assertion of the
// per-mission environment channel: a variable attached to the mission reaches
// the Claude process's environment.
func TestBuildClaudeCmdInjectsMissionEnv(t *testing.T) {
	withFakeClaudeOnPath(t)

	agencDirpath := t.TempDir()
	agentDirpath := t.TempDir()
	missionID := "test-mission-uuid"

	missionEnv := map[string]string{
		"REFLECTION_SESSION": "1",
		// Hostile on purpose: a value with spaces, newlines and both quote
		// styles goes into the environment as one entry, because nothing here
		// crosses a shell.
		"MARKER_NOTE": hostileClaudeArgValue,
	}
	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, nil, nil, missionEnv)
	if err != nil {
		t.Fatalf("BuildClaudeCmd returned error: %v", err)
	}

	for name, value := range missionEnv {
		want := name + "=" + value
		found := false
		for _, entry := range cmd.Env {
			if entry == want {
				found = true
			}
		}
		if !found {
			t.Errorf("cmd.Env missing %q", want)
		}
	}
}

// TestBuildClaudeCmdMissionEnvCannotOverrideAgencOwnVars pins the ordering that
// IS the protection: AgenC re-applies every variable it owns on top of the
// mission's own, so a caller naming one is overridden rather than overriding.
// The last entry for a name is what a process resolves to.
//
// It runs over config.ProtectedEnvVars rather than a hand-written list, so
// protecting a new variable is covered here the moment it is added there. The
// validation in claude_env.go refuses these names before they can ever be
// stored — this test deliberately bypasses it, because the ordering has to hold
// for a value that reached the record by some other route (a client written
// against an older build, a hand-edited row) rather than only for values that
// passed today's validation.
//
// Two of these matter beyond tidiness. If AGENC_MISSION_UUID inverted, mission
// state tracking would break for that mission, silently. If
// AGENC_MISSION_SOURCE inverted, a mission could set it to "cron" and win the
// exemption Kevin's night-time gate hook grants cron-spawned missions.
func TestBuildClaudeCmdMissionEnvCannotOverrideAgencOwnVars(t *testing.T) {
	withFakeClaudeOnPath(t)

	agencDirpath := t.TempDir()
	agentDirpath := t.TempDir()
	missionID := "test-mission-uuid"

	const hijackValue = "hijacked"

	// Every protected variable has to be present in the ambient environment for
	// this to be a real test: the spawn path re-applies them from there, and one
	// that is absent is skipped rather than emitted empty. Give each a known
	// AgenC-side value to recover.
	agencSideValues := map[string]string{}
	hijackAttempt := map[string]string{}
	for _, name := range config.ProtectedEnvVars {
		agencSideValue := "agenc-value-for-" + name
		agencSideValues[name] = agencSideValue
		t.Setenv(name, agencSideValue)
		hijackAttempt[name] = hijackValue
	}

	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, nil, nil, hijackAttempt)
	if err != nil {
		t.Fatalf("BuildClaudeCmd returned error: %v", err)
	}

	for _, name := range config.ProtectedEnvVars {
		// The mission UUID is produced from missionID rather than read from the
		// ambient environment, so it is the one with a different expectation.
		want := agencSideValues[name]
		if name == config.MissionUUIDEnvVar {
			want = missionID
		}
		// The OAuth token is appended only when a token file exists, which it
		// does not here, so the ambient value is what survives.
		resolved, found := resolveEnvEntry(cmd.Env, name)
		if !found {
			t.Errorf("%s is missing from cmd.Env entirely", name)
			continue
		}
		if resolved == hijackValue {
			t.Errorf("%s was HIJACKED: a mission's --env value overrode AgenC's own", name)
			continue
		}
		if resolved != want {
			t.Errorf("%s resolved to %q, want %q", name, resolved, want)
		}
	}
}

// TestBuildClaudeCmdInjectsOAuthTokenAfterMissionEnv covers the one protected
// variable the test above cannot: the token is read from a file rather than the
// environment, so it only appears when a token file exists.
func TestBuildClaudeCmdInjectsOAuthTokenAfterMissionEnv(t *testing.T) {
	withFakeClaudeOnPath(t)

	agencDirpath := t.TempDir()
	agentDirpath := t.TempDir()
	missionID := "test-mission-uuid"

	const token = "sk-ant-real-token"
	if err := config.WriteOAuthToken(agencDirpath, token); err != nil {
		t.Fatalf("failed to write token: %v", err)
	}

	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, nil, nil, map[string]string{
		config.OAuthTokenEnvVar: "stolen",
	})
	if err != nil {
		t.Fatalf("BuildClaudeCmd returned error: %v", err)
	}

	resolved, found := resolveEnvEntry(cmd.Env, config.OAuthTokenEnvVar)
	if !found {
		t.Fatalf("%s is missing from cmd.Env", config.OAuthTokenEnvVar)
	}
	if resolved != token {
		t.Errorf("%s resolved to %q, want the token from disk %q", config.OAuthTokenEnvVar, resolved, token)
	}
}

// resolveEnvEntry returns the value a process would see for name, which is the
// LAST entry carrying it — the resolution rule the ordering above relies on.
func resolveEnvEntry(env []string, name string) (string, bool) {
	prefix := name + "="
	value := ""
	found := false
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			value = strings.TrimPrefix(entry, prefix)
			found = true
		}
	}
	return value, found
}

// TestBuildClaudeCmdMissionEnvLeavesArgvAlone is the acceptance criterion the
// whole issue exists for: using the environment channel must not disturb
// AgenC's own operational configuration. That configuration travels on the
// command line as `--settings <op-settings file>`, so the proof is that the
// argv a mission with environment variables produces is identical to the argv
// the same mission produces without them.
func TestBuildClaudeCmdMissionEnvLeavesArgvAlone(t *testing.T) {
	withFakeClaudeOnPath(t)

	agencDirpath := t.TempDir()
	agentDirpath := t.TempDir()
	missionID := "test-mission-uuid"

	withoutEnv, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, nil, []string{"-c"}, nil)
	if err != nil {
		t.Fatalf("BuildClaudeCmd without env returned error: %v", err)
	}
	withEnv, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, nil, []string{"-c"}, map[string]string{
		"REFLECTION_SESSION": "1",
	})
	if err != nil {
		t.Fatalf("BuildClaudeCmd with env returned error: %v", err)
	}

	if !reflect.DeepEqual(withoutEnv.Args, withEnv.Args) {
		t.Fatalf("setting a mission environment changed the Claude argv: without %v, with %v", withoutEnv.Args, withEnv.Args)
	}

	// And the marker's name does not leak onto the command line, where a
	// `ps` listing would expose a value meant for the process environment.
	for _, arg := range withEnv.Args {
		if strings.Contains(arg, "REFLECTION_SESSION") {
			t.Errorf("mission environment name reached the Claude argv in %q", arg)
		}
	}
}

// TestBuildClaudeCmdMissionEnvCannotSupplyAnOAuthToken is the sharpest case the
// protected-name drop exists for, and the one an append-after-the-fact ordering
// could not have covered.
//
// Under State Y — the default, and what Kevin's own installation runs — there is
// no token file, so AgenC injects no CLAUDE_CODE_OAUTH_TOKEN at all and Claude
// authenticates with its native ~/.claude credentials. "Inject nothing" has no
// value that can be appended on top of a caller's, so without dropping the name
// from the mission's own variables a mission could hand Claude a token of its
// choosing and have it authenticate with that instead.
func TestBuildClaudeCmdMissionEnvCannotSupplyAnOAuthToken(t *testing.T) {
	withFakeClaudeOnPath(t)

	agencDirpath := t.TempDir()
	agentDirpath := t.TempDir()
	missionID := "test-mission-uuid"

	// No token file written, and no ambient token: this is State Y.
	t.Setenv(config.OAuthTokenEnvVar, "placeholder")
	if err := os.Unsetenv(config.OAuthTokenEnvVar); err != nil {
		t.Fatalf("failed to unset %s: %v", config.OAuthTokenEnvVar, err)
	}

	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, nil, nil, map[string]string{
		config.OAuthTokenEnvVar: "sk-ant-attacker-supplied",
	})
	if err != nil {
		t.Fatalf("BuildClaudeCmd returned error: %v", err)
	}

	if resolved, found := resolveEnvEntry(cmd.Env, config.OAuthTokenEnvVar); found {
		t.Errorf(
			"a mission supplied its own %s and it reached the Claude process as %q; "+
				"under State Y the variable must be absent so Claude uses its native auth",
			config.OAuthTokenEnvVar,
			resolved,
		)
	}
}

// TestBuildClaudeCmdProtectsIntroduceCaseWithoutInventingValues covers the
// production shape, which is NOT the override case most of this file assumes.
//
// Measured on the real install: a mission's Claude has only
// AGENC_MISSION_SOURCE and AGENC_MISSION_UUID. AGENC_DIRPATH, AGENC_TEST_ENV
// and CLAUDE_CONFIG_DIR are absent, because Server.tmuxEnvPrefix returns ""
// unless the install is namespaced, and the OAuth token is absent because no
// token file exists under State Y. So for those names `--env` does not override
// an AgenC value — it INTRODUCES a variable AgenC deliberately leaves unset.
//
// Two things therefore have to hold at once, and they pull in opposite
// directions:
//
//  1. The caller's value must not survive. The drop is what does this; nothing
//     could be appended on top, because there is no AgenC value to append.
//  2. AgenC must not invent one either. Appending AGENC_DIRPATH=<resolved> to
//     "protect" it would newly define that variable in every production
//     mission, where today it is unset and everything falls back to ~/.agenc.
//     That would be a behaviour change smuggled in as a side effect of a
//     reorder, so this test pins its absence.
func TestBuildClaudeCmdProtectsIntroduceCaseWithoutInventingValues(t *testing.T) {
	withFakeClaudeOnPath(t)

	agencDirpath := t.TempDir()
	agentDirpath := t.TempDir()
	missionID := "test-mission-uuid"

	// The production shape: these are not in the environment at all.
	introduceOnlyNames := []string{
		config.AgencDirpathEnvVarName(),
		config.TestEnvVar,
		config.OAuthTokenEnvVar,
	}
	missionEnv := map[string]string{}
	for _, name := range introduceOnlyNames {
		t.Setenv(name, "placeholder")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("failed to unset %s: %v", name, err)
		}
		missionEnv[name] = "introduced-by-the-mission"
	}

	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, nil, nil, missionEnv)
	if err != nil {
		t.Fatalf("BuildClaudeCmd returned error: %v", err)
	}

	for _, name := range introduceOnlyNames {
		resolved, found := resolveEnvEntry(cmd.Env, name)
		if !found {
			continue // The correct outcome: absent, exactly as AgenC left it.
		}
		if resolved == "introduced-by-the-mission" {
			t.Errorf("a mission INTRODUCED %s, which AgenC deliberately leaves unset", name)
			continue
		}
		t.Errorf("AgenC invented a value for %s (%q), newly defining a variable production missions do not have", name, resolved)
	}
}
