package mission

import (
	"reflect"
	"strings"
	"testing"
)

// hostileClaudeArgValue is the value this package's passthrough tests are built
// around: spaces, both quote styles, and newlines, which is what an
// --append-system-prompt value actually looks like. Everything about the
// passthrough design exists so that this value survives untouched.
const hostileClaudeArgValue = "Tonight is for reflection.\nDon't open \"work\" topics.\nAsk 'what happened today?'"

func TestMergeClaudeArgsAppendsPassthroughLast(t *testing.T) {
	merged := MergeClaudeArgs(ClaudeArgSources{
		ResolvedModel:         "sonnet",
		ConfigClaudeArgs:      []string{"--chrome"},
		MissionClaudeArgs:     map[string]string{"effort": "high"},
		ClaudePassthroughArgs: []string{"--append-system-prompt", "be brief"},
	})
	expected := []string{"--model", "sonnet", "--chrome", "--effort", "high", "--append-system-prompt", "be brief"}
	if !reflect.DeepEqual(merged, expected) {
		t.Fatalf("expected passthrough args appended after every other source; expected %v, got %v", expected, merged)
	}
}

// A passthrough flag the config chain also sets must end up after the config
// one, because that ordering is the whole mechanism by which a per-mission
// passthrough flag outranks config: Claude takes the last occurrence.
func TestMergeClaudeArgsPassthroughOutranksConfigByOrdering(t *testing.T) {
	merged := MergeClaudeArgs(ClaudeArgSources{
		ConfigClaudeArgs:      []string{"--model", "sonnet"},
		ClaudePassthroughArgs: []string{"--model", "opus"},
	})
	expected := []string{"--model", "sonnet", "--model", "opus"}
	if !reflect.DeepEqual(merged, expected) {
		t.Fatalf("expected the passthrough --model to come last so Claude resolves it; expected %v, got %v", expected, merged)
	}
}

// Passthrough args are opaque argv, so nothing about them may be interpreted —
// no deduplication, no reordering, no quoting, no splitting. A repeated flag is
// legitimate (--add-dir), and so is a value that happens to look like a flag.
func TestMergeClaudeArgsPassesPassthroughThroughVerbatim(t *testing.T) {
	passthroughArgs := []string{
		"--add-dir", "/one",
		"--add-dir", "/two",
		"--append-system-prompt", hostileClaudeArgValue,
		"--agents", `{"reviewer": {"prompt": "You review"}}`,
	}
	merged := MergeClaudeArgs(ClaudeArgSources{ClaudePassthroughArgs: passthroughArgs})
	if !reflect.DeepEqual(merged, passthroughArgs) {
		t.Fatalf("expected passthrough args untouched; expected %v, got %v", passthroughArgs, merged)
	}
}

func TestClaudePassthroughArgsWarningsStaysQuietForOrdinaryFlags(t *testing.T) {
	warnings := ClaudePassthroughArgsWarnings([]string{"--append-system-prompt", "be brief", "--add-dir", "/tmp"})
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for flags that break no AgenC invariant, got %v", warnings)
	}
}

func TestClaudePassthroughArgsWarningsFlagsSettings(t *testing.T) {
	warnings := ClaudePassthroughArgsWarnings([]string{"--settings", `{"env":{"A":"1"}}`})
	if len(warnings) != 1 {
		t.Fatalf("expected exactly one warning for a passthrough --settings, got %v", warnings)
	}
	if !strings.Contains(warnings[0], settingsClaudeFlag) {
		t.Fatalf("expected the warning to name the flag it is about, got %q", warnings[0])
	}
}

// "--settings=x" is the same flag as "--settings x" and must warn identically;
// matching on the whole token would miss it.
func TestClaudePassthroughArgsWarningsFlagsInlineValueSettings(t *testing.T) {
	warnings := ClaudePassthroughArgsWarnings([]string{`--settings={"env":{"A":"1"}}`})
	if len(warnings) != 1 {
		t.Fatalf("expected the inline-value form to warn like the separate-value form, got %v", warnings)
	}
}

func TestClaudePassthroughArgsWarningsFlagsBare(t *testing.T) {
	warnings := ClaudePassthroughArgsWarnings([]string{"--bare"})
	if len(warnings) != 1 {
		t.Fatalf("expected exactly one warning for a passthrough --bare, got %v", warnings)
	}
}

func TestFormatClaudePassthroughArgsLeavesPlainTokensAlone(t *testing.T) {
	formatted := FormatClaudePassthroughArgs([]string{"--add-dir", "/tmp/notes"})
	expected := "--add-dir /tmp/notes"
	if formatted != expected {
		t.Fatalf("expected %q, got %q", expected, formatted)
	}
}

// The display rendering must stay on one line whatever the value contains:
// `mission inspect` prints it inside an aligned block, and callers parse that
// output line by line.
func TestFormatClaudePassthroughArgsKeepsHostileValuesOnOneLine(t *testing.T) {
	formatted := FormatClaudePassthroughArgs([]string{"--append-system-prompt", hostileClaudeArgValue})
	if strings.ContainsAny(formatted, "\n\r") {
		t.Fatalf("expected a single-line rendering, got %q", formatted)
	}
	if !strings.HasPrefix(formatted, "--append-system-prompt ") {
		t.Fatalf("expected the flag name rendered plainly, got %q", formatted)
	}
}

func TestFormatClaudePassthroughArgsReturnsEmptyForNoArgs(t *testing.T) {
	if formatted := FormatClaudePassthroughArgs(nil); formatted != "" {
		t.Fatalf("expected an empty string for no passthrough args, got %q", formatted)
	}
}
