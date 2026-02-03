package tableprinter

import (
	"regexp"
	"unicode/utf8"

	"github.com/rodaine/table"
)

// ansiPattern matches ANSI SGR escape sequences (e.g. \033[32m).
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// VisibleWidth returns the number of visible characters in s, excluding any
// ANSI SGR escape sequences. This allows table column width calculations to
// work correctly with colored text.
func VisibleWidth(s string) int {
	stripped := ansiPattern.ReplaceAllString(s, "")
	return utf8.RuneCountInString(stripped)
}

// NewTable creates a new table with the given column headers, pre-configured
// with an ANSI-aware width function so that colored cell values don't break
// column alignment.
func NewTable(headers ...interface{}) table.Table {
	return table.New(headers...).WithWidthFunc(VisibleWidth)
}
