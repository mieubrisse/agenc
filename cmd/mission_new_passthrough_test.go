package cmd

import (
	"reflect"
	"testing"
)

// splitAtClaudePassthroughSeparator is what decides whether "owner/repo" is a
// repo reference or a Claude arg, so each shape a caller can type gets pinned
// here. The real Cobra parse is exercised end-to-end in scripts/e2e-test.sh;
// this covers the arithmetic.
func TestSplitAtClaudePassthroughSeparator(t *testing.T) {
	tests := []struct {
		name                string
		args                []string
		dashIndex           int
		expectedPositional  []string
		expectedPassthrough []string
	}{
		{
			name:               "no separator leaves every arg positional",
			args:               []string{"owner/repo"},
			dashIndex:          -1,
			expectedPositional: []string{"owner/repo"},
		},
		{
			name:                "repo then separator splits at the repo",
			args:                []string{"owner/repo", "--append-system-prompt", "be brief"},
			dashIndex:           1,
			expectedPositional:  []string{"owner/repo"},
			expectedPassthrough: []string{"--append-system-prompt", "be brief"},
		},
		{
			name:                "separator first leaves no repo reference, so the picker still opens",
			args:                []string{"--append-system-prompt", "be brief"},
			dashIndex:           0,
			expectedPositional:  []string{},
			expectedPassthrough: []string{"--append-system-prompt", "be brief"},
		},
		{
			name:               "trailing separator with nothing after it yields no passthrough args",
			args:               []string{"owner/repo"},
			dashIndex:          1,
			expectedPositional: []string{"owner/repo"},
		},
		{
			name:                "an arg that looks like a repo reference stays a Claude arg after the separator",
			args:                []string{"--add-dir", "owner/repo"},
			dashIndex:           0,
			expectedPositional:  []string{},
			expectedPassthrough: []string{"--add-dir", "owner/repo"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			positional, passthrough := splitAtClaudePassthroughSeparator(test.args, test.dashIndex)
			if !reflect.DeepEqual(positional, test.expectedPositional) {
				t.Fatalf("expected positional args %q, got %q", test.expectedPositional, positional)
			}
			if !reflect.DeepEqual(passthrough, test.expectedPassthrough) {
				t.Fatalf("expected passthrough args %q, got %q", test.expectedPassthrough, passthrough)
			}
		})
	}
}
