package cmd

import "strings"

// matchesSequentialSubstrings returns true if all substrings appear in text
// in order, case-insensitively.
func matchesSequentialSubstrings(text string, substrings []string) bool {
	lower := strings.ToLower(text)
	pos := 0
	for _, sub := range substrings {
		idx := strings.Index(lower[pos:], strings.ToLower(sub))
		if idx == -1 {
			return false
		}
		pos += idx + len(sub)
	}
	return true
}
