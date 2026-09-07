package server

import (
	"regexp"
	"strings"
)

// notificationTitleMaxRunes caps notification titles to a length that fits
// comfortably on one fzf row.
const notificationTitleMaxRunes = 200

var ansiSequenceRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// sanitizeNotificationLine makes a single-line, user-sourced value safe to put
// in notification text: it strips ANSI escape sequences, replaces CR / LF / tab
// with single spaces, and truncates to notificationTitleMaxRunes runes. Applied
// to notification titles, and to the cron names and schedule expressions quoted
// inside cron notification bodies — all of them come from user-edited config and
// can carry control characters that would corrupt fzf row rendering or
// notification list output.
func sanitizeNotificationLine(s string) string {
	s = ansiSequenceRegexp.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, s)
	runes := []rune(s)
	if len(runes) > notificationTitleMaxRunes {
		runes = runes[:notificationTitleMaxRunes]
	}
	return string(runes)
}
