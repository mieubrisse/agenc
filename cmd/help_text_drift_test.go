package cmd

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

// flagReferenceRegex matches a long-flag mention in help prose, stopping before
// the "=" of a "--flag=value" form so the value isn't mistaken for a flag.
var flagReferenceRegex = regexp.MustCompile(`--[a-z][a-z0-9-]*`)

// quotedSpanRegex matches single- and double-quoted spans. Help text quotes
// arguments meant for other programs -- claudeArgs entries like "--chrome" are
// Claude Code's flags, not AgenC's -- so quoted spans are stripped before
// looking for AgenC flag references.
var quotedSpanRegex = regexp.MustCompile(`"[^"]*"|'[^']*'`)

// externalToolFlagNames are long flags that appear unquoted in help text or docs
// but belong to programs AgenC invokes rather than to AgenC itself, so the
// command tree is not the place to look for them.
var externalToolFlagNames = map[string]bool{
	// Claude Code's own flag, named in docs/system-architecture.md to explain
	// why it is deliberately NOT in the forwarded-flag allowlist.
	"bare": true,

	// Homebrew's, from the "$(brew --prefix)" install path in the completion
	// help text Cobra generates for us.
	"prefix": true,
}

// TestHelpTextFlagReferencesExist checks that every AgenC flag named in help
// text is a flag some command actually registers. Help text promising a flag
// that was never implemented is not a cosmetic problem: agents read this text
// as the contract and call the flag, which is how two Adjutant missions came to
// invoke a cron "--overlap" flag that never existed.
func TestHelpTextFlagReferencesExist(t *testing.T) {
	requireFlagLookupWorks(t)

	forEachCommand(GetRootCmd(), func(cmd *cobra.Command) {
		t.Run(cmd.CommandPath(), func(t *testing.T) {
			for _, referencedFlagName := range findFlagReferences(helpTextOf(cmd)) {
				if externalToolFlagNames[referencedFlagName] {
					continue
				}
				if isFlagRegisteredAnywhere(GetRootCmd(), referencedFlagName) {
					continue
				}
				t.Errorf(
					"Help text for '%v' references flag '--%v', which no command in the tree registers. Either fix the help text, implement the flag, or -- if it belongs to another program AgenC invokes -- add it to externalToolFlagNames.",
					cmd.CommandPath(),
					referencedFlagName,
				)
			}
		})
	})
}

// handWrittenDocRelFilepaths are the prose docs that name AgenC flags without
// being generated from the command tree. Files under docs/cli/ are regenerated
// from Cobra help by `make docs`, so checking the help text covers them; files
// under docs/plans/ and docs/research/ are dated records of past thinking and are
// deliberately left alone.
var handWrittenDocRelFilepaths = []string{
	"../docs/configuration.md",
	"../docs/system-architecture.md",
	"../internal/claudeconfig/adjutant_claude.md",
	"../README.md",
}

// TestHandWrittenDocFlagReferencesExist applies the same check as
// TestHelpTextFlagReferencesExist to the hand-written docs. This is where the
// original drift lived: adjutant_claude.md documented a cron "--overlap" flag
// that was never implemented, and two Adjutant missions called it.
func TestHandWrittenDocFlagReferencesExist(t *testing.T) {
	requireFlagLookupWorks(t)

	for _, docRelFilepath := range handWrittenDocRelFilepaths {
		t.Run(docRelFilepath, func(t *testing.T) {
			docContent, err := os.ReadFile(docRelFilepath)
			if err != nil {
				t.Fatalf("Couldn't read hand-written doc '%v': %v", docRelFilepath, err)
			}

			for _, line := range strings.Split(string(docContent), "\n") {
				// Only lines that invoke the CLI are making a claim about
				// AgenC's own flags; prose elsewhere may name other tools'.
				if !strings.Contains(line, agencCmdStr+" ") {
					continue
				}
				for _, referencedFlagName := range findFlagReferences(line) {
					if externalToolFlagNames[referencedFlagName] {
						continue
					}
					if isFlagRegisteredAnywhere(GetRootCmd(), referencedFlagName) {
						continue
					}
					t.Errorf(
						"'%v' documents flag '--%v' on an %v command line, but no command in the tree registers it. Either fix the doc, implement the flag, or -- if it belongs to another program -- add it to externalToolFlagNames.\n    line: %v",
						docRelFilepath,
						referencedFlagName,
						agencCmdStr,
						strings.TrimSpace(line),
					)
				}
			}
		})
	}
}

// TestHelpTextYAMLExamplesMatchSchema parses every config.yml example embedded
// in help text against the real config schema with unknown fields rejected, so
// an example naming a key the schema does not have fails the build.
func TestHelpTextYAMLExamplesMatchSchema(t *testing.T) {
	foundAnyExample := false

	forEachCommand(GetRootCmd(), func(cmd *cobra.Command) {
		examples := extractHelpYAMLExamples(cmd.Long)
		if len(examples) == 0 {
			return
		}
		foundAnyExample = true

		t.Run(cmd.CommandPath(), func(t *testing.T) {
			for _, example := range examples {
				var cfg config.AgencConfig
				if err := yaml.UnmarshalWithOptions([]byte(example), &cfg, yaml.Strict()); err != nil {
					t.Errorf(
						"The config.yml example in '%v' help text doesn't match the schema in agenc_config.go: %v\n--- example ---\n%v\n--- end example ---",
						cmd.CommandPath(),
						err,
						example,
					)
				}
			}
		})
	})

	if !foundAnyExample {
		t.Fatalf(
			"Found no config.yml examples in any command's help text. Examples are located by the %q marker, so either the examples are gone or a help text stopped using the marker, and this test is now inspecting nothing.",
			configYAMLExampleMarker,
		)
	}
}

// TestHelpTextYAMLExamplesUseTheMarker catches the near-miss that would silently
// shrink this file's coverage: a help text that introduces a config.yml example
// with its own wording instead of the shared marker. Such an example is invisible
// to TestHelpTextYAMLExamplesMatchSchema, which is worse than having no example,
// because the suite still reports green.
func TestHelpTextYAMLExamplesUseTheMarker(t *testing.T) {
	forEachCommand(GetRootCmd(), func(cmd *cobra.Command) {
		for _, line := range strings.Split(cmd.Long, "\n") {
			trimmedLine := strings.TrimSpace(line)
			if !strings.HasSuffix(trimmedLine, "config.yml:") {
				continue
			}
			if trimmedLine == configYAMLExampleMarker {
				continue
			}
			t.Errorf(
				"Help text for '%v' introduces a config.yml example with %q. Use the configYAMLExampleMarker constant instead, or the example is never checked against the schema.",
				cmd.CommandPath(),
				trimmedLine,
			)
		}
	})
}

// isFlagRegisteredAnywhere reports whether any command in the tree registers a
// long flag by this name. Help text routinely points at a sibling command's
// flags -- "agenc cron enable" documents "agenc config cron update --enabled" --
// so membership is checked tree-wide rather than per-command.
func isFlagRegisteredAnywhere(rootCmd *cobra.Command, flagName string) bool {
	isRegistered := false
	forEachCommand(rootCmd, func(cmd *cobra.Command) {
		if isRegistered {
			return
		}
		// Cobra registers --help lazily at execute time, so without this the
		// tree would look like it has no help flag and every documented
		// "--help" would read as a phantom.
		cmd.InitDefaultHelpFlag()

		if cmd.Flags().Lookup(flagName) != nil || cmd.PersistentFlags().Lookup(flagName) != nil {
			isRegistered = true
		}
	})
	return isRegistered
}

// requireFlagLookupWorks guards against the whole flag-reference check passing
// because lookup silently returns nothing. Every Cobra command has --help once
// initialized, so failing to find it means the mechanism is broken and every
// other assertion in this file is vacuous.
func requireFlagLookupWorks(t *testing.T) {
	t.Helper()
	if !isFlagRegisteredAnywhere(GetRootCmd(), "help") {
		t.Fatal("Couldn't find the --help flag that every Cobra command has; flag lookup is broken and these checks are inspecting nothing")
	}
}

func findFlagReferences(helpText string) []string {
	unquotedHelpText := quotedSpanRegex.ReplaceAllString(helpText, " ")

	seenFlagNames := map[string]bool{}
	var flagNames []string
	for _, match := range flagReferenceRegex.FindAllString(unquotedHelpText, -1) {
		flagName := strings.TrimPrefix(match, "--")
		if seenFlagNames[flagName] {
			continue
		}
		seenFlagNames[flagName] = true
		flagNames = append(flagNames, flagName)
	}
	return flagNames
}

// extractHelpYAMLExamples returns the config.yml examples in a help string,
// dedented so they parse as standalone documents. An example runs from the line
// after the marker to the first non-blank line that is back at column zero.
func extractHelpYAMLExamples(helpText string) []string {
	var examples []string

	lines := strings.Split(helpText, "\n")
	for lineIdx, line := range lines {
		if strings.TrimSpace(line) != configYAMLExampleMarker {
			continue
		}

		var exampleLines []string
		for _, exampleLine := range lines[lineIdx+1:] {
			isBlank := strings.TrimSpace(exampleLine) == ""
			isIndented := strings.HasPrefix(exampleLine, " ") || strings.HasPrefix(exampleLine, "\t")
			if !isBlank && !isIndented {
				break
			}
			exampleLines = append(exampleLines, exampleLine)
		}

		example := dedent(exampleLines)
		if strings.TrimSpace(example) != "" {
			examples = append(examples, example)
		}
	}

	return examples
}

// dedent removes the common leading indentation shared by every non-blank line,
// which is what makes an indented help-text example parse as YAML.
func dedent(lines []string) string {
	smallestIndentWidth := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indentWidth := len(line) - len(strings.TrimLeft(line, " \t"))
		if smallestIndentWidth == -1 || indentWidth < smallestIndentWidth {
			smallestIndentWidth = indentWidth
		}
	}
	if smallestIndentWidth <= 0 {
		return strings.Join(lines, "\n")
	}

	dedentedLines := make([]string, 0, len(lines))
	for _, line := range lines {
		if len(line) < smallestIndentWidth {
			dedentedLines = append(dedentedLines, strings.TrimSpace(line))
			continue
		}
		dedentedLines = append(dedentedLines, line[smallestIndentWidth:])
	}
	return strings.Join(dedentedLines, "\n")
}

func helpTextOf(cmd *cobra.Command) string {
	return strings.Join([]string{cmd.Short, cmd.Long, cmd.Example}, "\n")
}

func forEachCommand(cmd *cobra.Command, visit func(*cobra.Command)) {
	visit(cmd)
	for _, subCmd := range cmd.Commands() {
		forEachCommand(subCmd, visit)
	}
}
