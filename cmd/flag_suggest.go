package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// An unknown flag is the most common wrong invocation an agent makes, and
// "unknown flag: --subagents" teaches nothing. When the misspelling is close to
// a flag the command actually has, the error names it.

// maxFlagSuggestionDistance bounds how far a typo may be from a real flag
// before no suggestion is offered; beyond it, guessing misleads.
const maxFlagSuggestionDistance = 3

func init() {
	rootCmd.SetFlagErrorFunc(suggestFlagOnError)
}

// suggestFlagOnError appends "(did you mean --x?)" to an unknown-flag error
// when one of the command's flags is within editing distance, or shares the
// typed prefix. Other flag errors pass through unchanged.
func suggestFlagOnError(cmd *cobra.Command, err error) error {
	const prefix = "unknown flag: --"
	msg := err.Error()
	if !strings.HasPrefix(msg, prefix) {
		return err
	}
	typed := strings.SplitN(strings.TrimPrefix(msg, prefix), " ", 2)[0]
	typed = strings.SplitN(typed, "=", 2)[0]
	if best := closestFlagName(cmd, typed); best != "" {
		return fmt.Errorf("%s (did you mean --%s?)", msg, best)
	}
	return err
}

// closestFlagName returns the command's flag nearest to typed, or "" when
// nothing is close enough.
func closestFlagName(cmd *cobra.Command, typed string) string {
	best, bestDistance := "", maxFlagSuggestionDistance+1
	consider := func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		d := editDistance(strings.ToLower(typed), f.Name)
		if strings.HasPrefix(f.Name, strings.ToLower(typed)) || strings.HasPrefix(strings.ToLower(typed), f.Name) {
			d = 1
		}
		if d < bestDistance || (d == bestDistance && f.Name < best) {
			best, bestDistance = f.Name, d
		}
	}
	cmd.Flags().VisitAll(consider)
	cmd.InheritedFlags().VisitAll(consider)
	if bestDistance > maxFlagSuggestionDistance {
		return ""
	}
	return best
}

// editDistance is the Levenshtein distance between two strings.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
