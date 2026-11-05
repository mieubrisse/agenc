package cmd

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"
)

// goreleaserConfigRelFilepath is the GoReleaser config whose "brews" section is
// published verbatim into the Homebrew tap's formula.
const goreleaserConfigRelFilepath = "../.goreleaser.yaml"

// homebrewBinaryRefStr is how the formula's Ruby test block names the installed
// binary.
const homebrewBinaryRefStr = "#{bin}/" + agencCmdStr

// longFlagArgPrefix is the prefix distinguishing a flag argument from a
// subcommand argument in the formula's test block.
const longFlagArgPrefix = "--"

// doubleQuotedStringRegex matches a double-quoted Ruby string literal. The test
// block passes the binary and each of its arguments as separate quoted strings,
// which is what makes the arguments extractable without a Ruby parser.
var doubleQuotedStringRegex = regexp.MustCompile(`"([^"]*)"`)

// goreleaserConfig is the slice of GoReleaser's schema this check reads. Parsed
// non-strictly on purpose: the real file carries builds, archives, nfpms and
// publishers this test has no business knowing about.
type goreleaserConfig struct {
	Brews []struct {
		Name string `yaml:"name"`
		Test string `yaml:"test"`
	} `yaml:"brews"`
}

// TestHomebrewFormulaTestBlockInvokesRealCommands checks that every command and
// flag the published Homebrew formula hands to the agenc binary is one the
// binary actually accepts.
//
// This is the same class of defect as the help-text drift this package already
// guards, with a worse feedback loop. `brew test` is opt-in, GoReleaser never
// runs it, and nothing in CI installs from the tap, so a test block naming a
// flag that does not exist ships green forever. It did exactly that: the block
// was GoReleaser's template default, `system "#{bin}/agenc", "--version"`, and
// AgenC has no --version flag -- it has a `version` subcommand -- so
// `brew test agenc` failed on every release through v0.15.0 with nobody
// watching.
//
// Folding this into handWrittenDocRelFilepaths was tried first and does not
// work: that scan only considers lines containing "agenc " with a trailing
// space, and the formula writes `agenc", "--version"`, so the file would have
// been added to the list and inspected nothing.
func TestHomebrewFormulaTestBlockInvokesRealCommands(t *testing.T) {
	requireFlagLookupWorks(t)

	configContent, err := os.ReadFile(goreleaserConfigRelFilepath)
	if err != nil {
		t.Fatalf("Couldn't read the GoReleaser config at '%v': %v", goreleaserConfigRelFilepath, err)
	}

	var parsedConfig goreleaserConfig
	if err := yaml.Unmarshal(configContent, &parsedConfig); err != nil {
		t.Fatalf("Couldn't parse the GoReleaser config at '%v': %v", goreleaserConfigRelFilepath, err)
	}

	if len(parsedConfig.Brews) == 0 {
		t.Fatalf(
			"Found no 'brews' entries in '%v'; either the Homebrew tap stopped being published from here or this parse stopped working, and this check is now inspecting nothing",
			goreleaserConfigRelFilepath,
		)
	}

	assertedArgCount := 0
	for _, brew := range parsedConfig.Brews {
		for _, invocationArg := range findFormulaBinaryInvocationArgs(brew.Test) {
			assertedArgCount++

			if strings.HasPrefix(invocationArg, longFlagArgPrefix) {
				flagName := strings.TrimPrefix(invocationArg, longFlagArgPrefix)
				if isFlagRegisteredAnywhere(GetRootCmd(), flagName) {
					continue
				}
				t.Errorf(
					"The '%v' formula's test block runs '%v %v', but no command in the tree registers the '%v' flag, so 'brew test %v' fails. Either fix the test block in '%v' or implement the flag.",
					brew.Name,
					agencCmdStr,
					invocationArg,
					flagName,
					brew.Name,
					goreleaserConfigRelFilepath,
				)
				continue
			}

			if isSubcommandRegistered(GetRootCmd(), invocationArg) {
				continue
			}
			t.Errorf(
				"The '%v' formula's test block runs '%v %v', but the command tree has no '%v' subcommand, so 'brew test %v' fails. Either fix the test block in '%v' or implement the subcommand.",
				brew.Name,
				agencCmdStr,
				invocationArg,
				invocationArg,
				brew.Name,
				goreleaserConfigRelFilepath,
			)
		}
	}

	if assertedArgCount == 0 {
		t.Fatalf(
			"Found no arguments passed to '%v' in any formula test block in '%v'; either the test blocks stopped invoking the binary or the extraction stopped working, and this check is now inspecting nothing",
			homebrewBinaryRefStr,
			goreleaserConfigRelFilepath,
		)
	}
}

// findFormulaBinaryInvocationArgs returns the arguments a formula test block
// passes to the agenc binary, one entry per quoted argument.
func findFormulaBinaryInvocationArgs(formulaTestBlock string) []string {
	invocationArgs := []string{}
	for _, line := range strings.Split(formulaTestBlock, "\n") {
		if !strings.Contains(line, homebrewBinaryRefStr) {
			continue
		}
		for _, quotedStringMatch := range doubleQuotedStringRegex.FindAllStringSubmatch(line, -1) {
			quotedString := quotedStringMatch[1]
			// The binary reference is the thing being invoked, not an argument
			// to itself.
			if strings.Contains(quotedString, homebrewBinaryRefStr) {
				continue
			}
			invocationArgs = append(invocationArgs, quotedString)
		}
	}
	return invocationArgs
}

// isSubcommandRegistered reports whether the command tree holds a command of
// the given name at any depth.
func isSubcommandRegistered(rootCmd *cobra.Command, subcommandName string) bool {
	isRegistered := false
	forEachCommand(rootCmd, func(cmd *cobra.Command) {
		if cmd.Name() == subcommandName {
			isRegistered = true
		}
	})
	return isRegistered
}
