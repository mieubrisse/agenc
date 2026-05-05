package server

import "regexp"

// ANSI CSI escape sequences (cursor movement, color codes, mode switches).
var ansiCSIRegex = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

// ANSI OSC escape sequences (window title, hyperlinks). Terminated by BEL
// (\x07) or ST (\x1b\\).
var ansiOSCRegex = regexp.MustCompile(`\x1b\][^\x07\x1b]*(\x07|\x1b\\)`)

// StripANSI removes ANSI escape sequences from a string. Used to sanitize
// notification body content at display time so terminal escape sequences
// in the stored body cannot affect the user's terminal.
func StripANSI(s string) string {
	s = ansiCSIRegex.ReplaceAllString(s, "")
	s = ansiOSCRegex.ReplaceAllString(s, "")
	return s
}
