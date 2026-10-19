package mission

import (
	"reflect"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

func TestBuildNewConversationArgs(t *testing.T) {
	tests := []struct {
		name          string
		initialPrompt string
		want          []string
	}{
		{
			name:          "no prompt yields no args",
			initialPrompt: "",
			want:          nil,
		},
		{
			name:          "a prompt is separated from the flags before it",
			initialPrompt: "do the thing",
			want:          []string{"--", "do the thing"},
		},
		{
			name:          "a prompt that itself looks like a flag is still a prompt",
			initialPrompt: "--not-a-flag",
			want:          []string{"--", "--not-a-flag"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildNewConversationArgs(tt.initialPrompt)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildNewConversationArgs(%q) = %v, want %v", tt.initialPrompt, got, tt.want)
			}
		})
	}
}

// TestBuildClaudeCmdSeparatesPromptFromTrailingVariadicPassthrough is the
// regression test for a silent loss of the mission's prompt. A passthrough flag
// that takes a variable number of values (--add-dir <directories...>) or an
// optional one (--debug [filter]) lands immediately before the positional
// prompt and swallows it.
//
// Measured against claude 2.1.274: `claude -p --add-dir /tmp 'Reply with
// exactly: PROMPTOK' </dev/null` fails with "Input must be provided either
// through stdin or as a prompt argument", while the same command with `--`
// before the prompt answers PROMPTOK. Inside a mission nothing would have
// errored at all — the prompt would have become an extra allowed directory and
// Claude would have started with no prompt.
//
// Every cron mission passes a prompt, so this sat on the path of all scheduled
// work from the moment passthrough flags existed.
func TestBuildClaudeCmdSeparatesPromptFromTrailingVariadicPassthrough(t *testing.T) {
	withFakeClaudeOnPath(t)

	agencDirpath := t.TempDir()
	agentDirpath := t.TempDir()
	missionID := "test-mission-uuid"
	initialPrompt := "PROMPTMARKER please reply ok"

	mergedClaudeArgs := MergeClaudeArgs(ClaudeArgSources{
		ClaudePassthroughArgs: []string{"--add-dir", "/tmp"},
	})
	cmd, err := BuildClaudeCmd(
		agencDirpath,
		missionID,
		agentDirpath,
		mergedClaudeArgs,
		buildNewConversationArgs(initialPrompt),
		nil,
	)
	if err != nil {
		t.Fatalf("BuildClaudeCmd returned error: %v", err)
	}

	// cmd.Args[0] is the claude binary path itself.
	gotArgs := cmd.Args[1:]
	opSettingsFilepath := config.GetMissionOpSettingsFilepath(agencDirpath, missionID)
	expectedArgs := []string{
		"--settings", opSettingsFilepath,
		"--add-dir", "/tmp",
		"--", initialPrompt,
	}
	if !reflect.DeepEqual(gotArgs, expectedArgs) {
		t.Fatalf("expected args %v, got %v", expectedArgs, gotArgs)
	}

	// Asserted separately from the slice comparison above, because this is the
	// property that actually has to hold and the one a future reordering would
	// break: the prompt is the final argv element, and the element before it
	// ends Claude's option parsing.
	if gotArgs[len(gotArgs)-1] != initialPrompt {
		t.Fatalf("expected the prompt to be the final argv element, got %q", gotArgs[len(gotArgs)-1])
	}
	if gotArgs[len(gotArgs)-2] != endOfClaudeFlagsSeparator {
		t.Fatalf("expected the element before the prompt to end option parsing, got %q", gotArgs[len(gotArgs)-2])
	}
}

// The resume shape carries the same guard. It is safe without it today, since
// the "-c" or "-r <session>" it emits first terminates any variadic flag behind
// it — but nothing pins that ordering, so the prompt should not depend on it.
func TestBuildClaudeCmdSeparatesResumePromptFromTrailingVariadicPassthrough(t *testing.T) {
	withFakeClaudeOnPath(t)

	agencDirpath := t.TempDir()
	agentDirpath := t.TempDir()
	missionID := "test-mission-uuid"
	initialPrompt := "PROMPTMARKER please reply ok"

	mergedClaudeArgs := MergeClaudeArgs(ClaudeArgSources{
		ClaudePassthroughArgs: []string{"--add-dir", "/tmp"},
	})
	cmd, err := BuildClaudeCmd(
		agencDirpath,
		missionID,
		agentDirpath,
		mergedClaudeArgs,
		buildResumeArgs("sess-abc", initialPrompt),
		nil,
	)
	if err != nil {
		t.Fatalf("BuildClaudeCmd returned error: %v", err)
	}

	gotArgs := cmd.Args[1:]
	opSettingsFilepath := config.GetMissionOpSettingsFilepath(agencDirpath, missionID)
	expectedArgs := []string{
		"--settings", opSettingsFilepath,
		"--add-dir", "/tmp",
		"-r", "sess-abc",
		"--", initialPrompt,
	}
	if !reflect.DeepEqual(gotArgs, expectedArgs) {
		t.Fatalf("expected args %v, got %v", expectedArgs, gotArgs)
	}
}
