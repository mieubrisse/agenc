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

func TestMergeClaudeArgsEmitsResolvedModel(t *testing.T) {
	merged := MergeClaudeArgs(ClaudeArgSources{ResolvedModel: "sonnet"})
	expected := []string{"--model", "sonnet"}
	if !reflect.DeepEqual(merged, expected) {
		t.Fatalf("expected %v, got %v", expected, merged)
	}
}

func TestMergeClaudeArgsMissionModelSuppressesResolvedModel(t *testing.T) {
	merged := MergeClaudeArgs(ClaudeArgSources{ResolvedModel: "sonnet", MissionClaudeArgs: map[string]string{"model": "opus"}})
	expected := []string{"--model", "opus"}
	if !reflect.DeepEqual(merged, expected) {
		t.Fatalf("expected the resolved model to be replaced, not duplicated; expected %v, got %v", expected, merged)
	}
}

func TestMergeClaudeArgsKeepsNonConflictingConfigArgs(t *testing.T) {
	merged := MergeClaudeArgs(ClaudeArgSources{ResolvedModel: "sonnet", ConfigClaudeArgs: []string{"--chrome", "--verbose"}})
	expected := []string{"--model", "sonnet", "--chrome", "--verbose"}
	if !reflect.DeepEqual(merged, expected) {
		t.Fatalf("expected %v, got %v", expected, merged)
	}
}

func TestMergeClaudeArgsStripsInlineValueConfigArgConflict(t *testing.T) {
	merged := MergeClaudeArgs(ClaudeArgSources{ConfigClaudeArgs: []string{"--chrome", "--model=sonnet"}, MissionClaudeArgs: map[string]string{"model": "opus"}})
	expected := []string{"--chrome", "--model", "opus"}
	if !reflect.DeepEqual(merged, expected) {
		t.Fatalf("expected the config --model=sonnet to be stripped; expected %v, got %v", expected, merged)
	}
}

func TestMergeClaudeArgsStripsSeparateValueConfigArgConflict(t *testing.T) {
	merged := MergeClaudeArgs(ClaudeArgSources{ConfigClaudeArgs: []string{"--model", "sonnet", "--chrome"}, MissionClaudeArgs: map[string]string{"model": "opus"}})
	expected := []string{"--chrome", "--model", "opus"}
	if !reflect.DeepEqual(merged, expected) {
		t.Fatalf("expected both the config --model and its value token to be stripped; expected %v, got %v", expected, merged)
	}
}

func TestMergeClaudeArgsStripsTrailingBareConfigArgConflict(t *testing.T) {
	merged := MergeClaudeArgs(ClaudeArgSources{ConfigClaudeArgs: []string{"--chrome", "--model"}, MissionClaudeArgs: map[string]string{"model": "opus"}})
	expected := []string{"--chrome", "--model", "opus"}
	if !reflect.DeepEqual(merged, expected) {
		t.Fatalf("expected a valueless trailing --model to be stripped without panicking; expected %v, got %v", expected, merged)
	}
}

func TestMergeClaudeArgsEmitsMissionArgsInTableOrder(t *testing.T) {
	merged := MergeClaudeArgs(ClaudeArgSources{MissionClaudeArgs: map[string]string{"effort": "high", "model": "opus"}})
	expected := []string{"--model", "opus", "--effort", "high"}
	if !reflect.DeepEqual(merged, expected) {
		t.Fatalf("expected forwarded flags in table order regardless of map iteration order; expected %v, got %v", expected, merged)
	}
}

func TestMergeClaudeArgsIgnoresUnknownMissionKeys(t *testing.T) {
	merged := MergeClaudeArgs(ClaudeArgSources{MissionClaudeArgs: map[string]string{"model": "opus", "bogus": "value"}})
	expected := []string{"--model", "opus"}
	if !reflect.DeepEqual(merged, expected) {
		t.Fatalf("expected unknown keys to be dropped rather than emitted as flags; expected %v, got %v", expected, merged)
	}
}

func TestValidateMissionClaudeArgsAcceptsKnownKeys(t *testing.T) {
	if err := ValidateMissionClaudeArgs(map[string]string{"model": "opus", "effort": "high"}); err != nil {
		t.Fatalf("expected known keys to validate, got %v", err)
	}
}

func TestValidateMissionClaudeArgsRejectsUnknownKey(t *testing.T) {
	err := ValidateMissionClaudeArgs(map[string]string{"bogus": "value"})
	if err == nil {
		t.Fatal("expected an unknown Claude arg key to be rejected")
	}
}

func TestValidateMissionClaudeArgsRejectsEmptyValue(t *testing.T) {
	err := ValidateMissionClaudeArgs(map[string]string{"model": ""})
	if err == nil {
		t.Fatal("expected an empty Claude arg value to be rejected")
	}
}

func TestUnknownClaudeArgKeysReportsOnlyUnknownKeys(t *testing.T) {
	unknown := UnknownClaudeArgKeys(map[string]string{"model": "opus", "zeta": "1", "alpha": "2"})
	expected := []string{"alpha", "zeta"}
	if !reflect.DeepEqual(unknown, expected) {
		t.Fatalf("expected sorted unknown keys %v, got %v", expected, unknown)
	}
}

func TestFormatMissionClaudeArgsRendersHumanReadableString(t *testing.T) {
	formatted := FormatMissionClaudeArgs(map[string]string{"model": "opus", "effort": "high"})
	expected := "--model opus --effort high"
	if formatted != expected {
		t.Fatalf("expected %q, got %q", expected, formatted)
	}
}

func TestFormatMissionClaudeArgsRendersEmptyStringForNoArgs(t *testing.T) {
	if formatted := FormatMissionClaudeArgs(nil); formatted != "" {
		t.Fatalf("expected an empty string for no args, got %q", formatted)
	}
}
