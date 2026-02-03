package tableprinter

import (
	"regexp"

	"github.com/mattn/go-runewidth"
	"github.com/rodaine/table"
)

// ansiPattern matches ANSI SGR escape sequences (e.g. \033[32m).
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// VisibleWidth returns the display width of s in terminal columns, excluding
// any ANSI SGR escape sequences. This correctly accounts for wide characters
// such as East Asian characters and emoji, which occupy two columns despite
// being a single glyph.
func VisibleWidth(s string) int {
	stripped := ansiPattern.ReplaceAllString(s, "")
	return runewidth.StringWidth(stripped)
}

// NewTable creates a new table with the given column headers, pre-configured
// with an ANSI-aware width function so that colored cell values don't break
// column alignment.
func NewTable(headers ...interface{}) table.Table {
	return table.New(headers...).WithWidthFunc(VisibleWidth)
}
