package mission

import (
	"reflect"
	"testing"
)

func TestMergeClaudeArgsWithNoSourcesReturnsNothing(t *testing.T) {
	merged := MergeClaudeArgs(ClaudeArgSources{})
	if len(merged) != 0 {
		t.Fatalf("expected no args, got %v", merged)
	}
}

// A mission that specifies nothing runs with exactly the config chain's args,
// which is how a configured default model reaches it.
func TestMergeClaudeArgsAppliesConfigDefaultsToAMissionWithNoArgs(t *testing.T) {
	merged := MergeClaudeArgs(ClaudeArgSources{ConfigClaudeArgs: []string{"--model", "opus", "--chrome"}})
	expected := []string{"--model", "opus", "--chrome"}
	if !reflect.DeepEqual(merged, expected) {
		t.Fatalf("expected the config defaults untouched; expected %v, got %v", expected, merged)
	}
}

// AgenC knows no flag names, so it must not drop a config flag the mission
// repeats: both reach Claude, and the mission's wins by coming last.
func TestMergeClaudeArgsDedupesNothing(t *testing.T) {
	merged := MergeClaudeArgs(ClaudeArgSources{
		ConfigClaudeArgs:      []string{"--model=opus", "--effort", "low"},
		ClaudePassthroughArgs: []string{"--model", "fable", "--effort", "high"},
	})
	expected := []string{"--model=opus", "--effort", "low", "--model", "fable", "--effort", "high"}
	if !reflect.DeepEqual(merged, expected) {
		t.Fatalf("expected both sources kept in order; expected %v, got %v", expected, merged)
	}
}
