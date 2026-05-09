package server

import (
	"regexp"
	"strings"
)

// notificationTitleMaxRunes caps notification titles to a length that fits
// comfortably on one fzf row.
const notificationTitleMaxRunes = 200

var ansiSequenceRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// sanitizeNotificationTitle strips ANSI escape sequences and replaces
// CR / LF / tab with single spaces, then truncates to notificationTitleMaxRunes
// runes. Used at the cron-notification write site as defense-in-depth — cron
// names come from user-edited config and can contain control characters that
// would corrupt fzf row rendering or notification list output.
func sanitizeNotificationTitle(s string) string {
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
