package launchd

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mieubrisse/stacktrace"
)

// Plist represents a launchd plist file for scheduling cron jobs.
type Plist struct {
	Label                 string
	ProgramArguments      []string
	StartCalendarInterval *CalendarInterval
	StandardOutPath       string
	StandardErrorPath     string
}

// CalendarInterval represents a launchd calendar interval for scheduling.
type CalendarInterval struct {
	Minute  *int
	Hour    *int
	Day     *int
	Month   *int
	Weekday *int
}

// plistDict is the internal XML structure for a launchd plist.
type plistDict struct {
	XMLName xml.Name   `xml:"plist"`
	Version string     `xml:"version,attr"`
	Dict    dictFields `xml:"dict"`
}

type dictFields struct {
	Entries []interface{}
}

type key struct {
	XMLName xml.Name `xml:"key"`
	Value   string   `xml:",chardata"`
}

type stringValue struct {
	XMLName xml.Name `xml:"string"`
	Value   string   `xml:",chardata"`
}

type arrayValue struct {
	XMLName xml.Name      `xml:"array"`
	Strings []stringValue `xml:"string"`
}

type dictValue struct {
	XMLName xml.Name `xml:"dict"`
	Entries []interface{}
}

// GeneratePlistXML renders the plist as XML.
func (p *Plist) GeneratePlistXML() ([]byte, error) {
	// Build the dict entries
	var entries []interface{}

	// Label
	entries = append(entries, key{Value: "Label"}, stringValue{Value: p.Label})

	// ProgramArguments
	entries = append(entries, key{Value: "ProgramArguments"})
	programArgs := arrayValue{}
	for _, arg := range p.ProgramArguments {
		programArgs.Strings = append(programArgs.Strings, stringValue{Value: arg})
	}
	entries = append(entries, programArgs)

	// StartCalendarInterval
	if p.StartCalendarInterval != nil {
		entries = append(entries, key{Value: "StartCalendarInterval"})
		calEntries := []interface{}{}
		if p.StartCalendarInterval.Minute != nil {
			calEntries = append(calEntries, key{Value: "Minute"})
			calEntries = append(calEntries, integerValue{Value: *p.StartCalendarInterval.Minute})
		}
		if p.StartCalendarInterval.Hour != nil {
			calEntries = append(calEntries, key{Value: "Hour"})
			calEntries = append(calEntries, integerValue{Value: *p.StartCalendarInterval.Hour})
		}
		if p.StartCalendarInterval.Day != nil {
			calEntries = append(calEntries, key{Value: "Day"})
			calEntries = append(calEntries, integerValue{Value: *p.StartCalendarInterval.Day})
		}
		if p.StartCalendarInterval.Month != nil {
			calEntries = append(calEntries, key{Value: "Month"})
			calEntries = append(calEntries, integerValue{Value: *p.StartCalendarInterval.Month})
		}
		if p.StartCalendarInterval.Weekday != nil {
			calEntries = append(calEntries, key{Value: "Weekday"})
			calEntries = append(calEntries, integerValue{Value: *p.StartCalendarInterval.Weekday})
		}
		entries = append(entries, dictValue{Entries: calEntries})
	}

	// StandardOutPath
	entries = append(entries, key{Value: "StandardOutPath"}, stringValue{Value: p.StandardOutPath})

	// StandardErrorPath
	entries = append(entries, key{Value: "StandardErrorPath"}, stringValue{Value: p.StandardErrorPath})

	plist := plistDict{
		Version: "1.0",
		Dict: dictFields{
			Entries: entries,
		},
	}

	// Marshal with proper XML header and DOCTYPE
	xmlData, err := xml.MarshalIndent(plist, "", "    ")
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal plist XML")
	}

	// Prepend XML declaration and DOCTYPE
	header := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
`
	return []byte(header + string(xmlData)), nil
}

type integerValue struct {
	XMLName xml.Name `xml:"integer"`
	Value   int      `xml:",chardata"`
}

// WriteToDisk writes the plist to the specified path atomically.
func (p *Plist) WriteToDisk(targetPath string) error {
	xmlData, err := p.GeneratePlistXML()
	if err != nil {
		return stacktrace.Propagate(err, "failed to generate plist XML")
	}

	// Write to temporary file first
	tempPath := targetPath + ".tmp"
	if err := os.WriteFile(tempPath, xmlData, 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write temporary plist file")
	}

	// Atomic rename
	if err := os.Rename(tempPath, targetPath); err != nil {
		os.Remove(tempPath) // Clean up on error
		return stacktrace.Propagate(err, "failed to rename temporary plist file")
	}

	return nil
}

// CronToPlistFilename converts a cron name to a plist filename.
// The cron name must already be validated (alphanumeric, dash, underscore only).
func CronToPlistFilename(cronName string) string {
	return fmt.Sprintf("agenc-cron-%s.plist", cronName)
}

// PlistDirpath returns the path to the LaunchAgents directory.
// Returns error if home directory cannot be determined.
func PlistDirpath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Try fallback to HOME env var
		homeDir = os.Getenv("HOME")
		if homeDir == "" {
			return "", stacktrace.NewError("cannot determine home directory: UserHomeDir failed and $HOME is unset")
		}
	}
	return filepath.Join(homeDir, "Library", "LaunchAgents"), nil
}

// ParseCronExpression parses a cron expression into a CalendarInterval.
// Returns error for unsupported expressions.
// Supports: "minute hour * * *" format only (no */N, ranges, or lists)
func ParseCronExpression(cronExpr string) (*CalendarInterval, error) {
	parts := strings.Fields(cronExpr)
	if len(parts) != 5 {
		return nil, stacktrace.NewError("cron expression must have 5 fields (minute hour day month weekday)")
	}

	minute, hour, day, month, weekday := parts[0], parts[1], parts[2], parts[3], parts[4]

	interval := &CalendarInterval{}

	// Parse minute
	if minute != "*" {
		val, err := strconv.Atoi(minute)
		if err != nil {
			return nil, stacktrace.NewError("unsupported minute format: %s (must be integer or *)", minute)
		}
		if val < 0 || val > 59 {
			return nil, stacktrace.NewError("minute must be between 0 and 59")
		}
		interval.Minute = &val
	}

	// Parse hour
	if hour != "*" {
		val, err := strconv.Atoi(hour)
		if err != nil {
			return nil, stacktrace.NewError("unsupported hour format: %s (must be integer or *)", hour)
		}
		if val < 0 || val > 23 {
			return nil, stacktrace.NewError("hour must be between 0 and 23")
		}
		interval.Hour = &val
	}

	// Parse day
	if day != "*" {
		val, err := strconv.Atoi(day)
		if err != nil {
			return nil, stacktrace.NewError("unsupported day format: %s (must be integer or *)", day)
		}
		if val < 1 || val > 31 {
			return nil, stacktrace.NewError("day must be between 1 and 31")
		}
		interval.Day = &val
	}

	// Parse month
	if month != "*" {
		val, err := strconv.Atoi(month)
		if err != nil {
			return nil, stacktrace.NewError("unsupported month format: %s (must be integer or *)", month)
		}
		if val < 1 || val > 12 {
			return nil, stacktrace.NewError("month must be between 1 and 12")
		}
		interval.Month = &val
	}

	// Parse weekday (0-7, where 0 and 7 are Sunday)
	if weekday != "*" {
		val, err := strconv.Atoi(weekday)
		if err != nil {
			return nil, stacktrace.NewError("unsupported weekday format: %s (must be integer or *)", weekday)
		}
		if val < 0 || val > 7 {
			return nil, stacktrace.NewError("weekday must be between 0 and 7")
		}
		// Normalize 7 to 0 (both represent Sunday)
		if val == 7 {
			val = 0
		}
		interval.Weekday = &val
	}

	return interval, nil
}
