package sleep

import (
	"strconv"
	"strings"

	"github.com/mieubrisse/stacktrace"
)

// validDays maps lowercase abbreviated day names to true.
var validDays = map[string]bool{
	"mon": true,
	"tue": true,
	"wed": true,
	"thu": true,
	"fri": true,
	"sat": true,
	"sun": true,
}

// WindowDef defines a recurring sleep window: which days of the week and the
// start/end times (HH:MM, 24-hour) during which sleep mode is active.
type WindowDef struct {
	Days  []string `json:"days" yaml:"days"`
	Start string   `json:"start" yaml:"start"`
	End   string   `json:"end" yaml:"end"`
}

// ValidateDays checks that days is non-empty, contains only valid day names
// (mon, tue, wed, thu, fri, sat, sun), and has no duplicates.
func ValidateDays(days []string) error {
	if len(days) == 0 {
		return stacktrace.NewError("days must not be empty")
	}
	seen := make(map[string]bool, len(days))
	for _, d := range days {
		if !validDays[d] {
			return stacktrace.NewError("invalid day %q", d)
		}
		if seen[d] {
			return stacktrace.NewError("duplicate day %q", d)
		}
		seen[d] = true
	}
	return nil
}

// ValidateTime checks that s is a non-empty string in HH:MM format with hour
// in 0-23 and minute in 0-59.
func ValidateTime(s string) error {
	if s == "" {
		return stacktrace.NewError("time must not be empty")
	}
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return stacktrace.NewError("time %q must be in HH:MM format", s)
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || len(parts[0]) != 2 {
		return stacktrace.NewError("time %q has invalid hour", s)
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || len(parts[1]) != 2 {
		return stacktrace.NewError("time %q has invalid minute", s)
	}
	if hour < 0 || hour > 23 {
		return stacktrace.NewError("time %q has hour out of range 0-23", s)
	}
	if minute < 0 || minute > 59 {
		return stacktrace.NewError("time %q has minute out of range 0-59", s)
	}
	return nil
}

// ValidateWindow validates a WindowDef by checking its days and times and
// rejecting windows where start equals end.
func ValidateWindow(w WindowDef) error {
	if err := ValidateDays(w.Days); err != nil {
		return stacktrace.Propagate(err, "invalid window days")
	}
	if err := ValidateTime(w.Start); err != nil {
		return stacktrace.Propagate(err, "invalid window start")
	}
	if err := ValidateTime(w.End); err != nil {
		return stacktrace.Propagate(err, "invalid window end")
	}
	if w.Start == w.End {
		return stacktrace.NewError("window start and end must differ (both are %q)", w.Start)
	}
	return nil
}
