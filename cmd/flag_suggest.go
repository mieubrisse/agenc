package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// An unknown flag is the most common wrong invocation an agent makes, and
// "unknown flag: --subagents" teaches nothing. When the misspelling is close to
// a flag the command actually has, the error names it.

// minAffixLength is the shortest typed or real flag name the containment
// rules consider: "--a" is not evidence for anything, and "--subagents"
// contains "agents" only meaningfully because both are words.
const minAffixLength = 4

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
// nothing is close enough. Three rules, in priority order, each with a floor
// that keeps it from firing on noise:
//
//   - a typo: edit distance (adjacent transposition counts one) within a
//     quarter of the typed length, at least one — so "--sicne" reaches
//     "--since", while "--tail" on a command that has "--all" (distance 2 on
//     four letters) reaches nothing, because that flag belongs elsewhere;
//   - a real name inside what was typed ("--subagents" holds "agents"), the
//     longest such name winning;
//   - what was typed inside a real name ("--expand" is in "--expand-agents"),
//     the shortest such name winning.
func closestFlagName(cmd *cobra.Command, typed string) string {
	typed = strings.ToLower(typed)
	var names []string
	collect := func(f *pflag.Flag) {
		if !f.Hidden {
			names = append(names, f.Name)
		}
	}
	cmd.Flags().VisitAll(collect)
	cmd.InheritedFlags().VisitAll(collect)
	sort.Strings(names)

	allowed := len(typed) / 4
	if allowed < 1 {
		allowed = 1
	}
	best, bestDistance := "", allowed+1
	for _, name := range names {
		if d := editDistance(typed, name); d < bestDistance {
			best, bestDistance = name, d
		}
	}
	if best != "" {
		return best
	}
	if len(typed) >= minAffixLength {
		for _, name := range names {
			if len(name) >= minAffixLength && strings.Contains(typed, name) && len(name) > len(best) {
				best = name
			}
		}
		if best != "" {
			return best
		}
		for _, name := range names {
			if strings.Contains(name, typed) && (best == "" || len(name) < len(best)) {
				best = name
			}
		}
	}
	return best
}

// editDistance is the optimal string alignment distance: Levenshtein with an
// adjacent transposition counted as one edit, so "sicne" is one step from
// "since" rather than two.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	d := make([][]int, len(ra)+1)
	for i := range d {
		d[i] = make([]int, len(rb)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(ra)][len(rb)]
}
