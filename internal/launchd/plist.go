package launchd

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mieubrisse/stacktrace"
)

// Plist represents a launchd plist file for scheduling cron jobs.
type Plist struct {
	Label                 string
	ProgramArguments      []string
	StartCalendarInterval *CalendarInterval
	EnvironmentVariables  map[string]string
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

	// EnvironmentVariables (sorted by key for deterministic output)
	if len(p.EnvironmentVariables) > 0 {
		entries = append(entries, key{Value: "EnvironmentVariables"})
		var envEntries []interface{}
		// Sort keys for deterministic XML output
		sortedKeys := make([]string, 0, len(p.EnvironmentVariables))
		for k := range p.EnvironmentVariables {
			sortedKeys = append(sortedKeys, k)
		}
		sort.Strings(sortedKeys)
		for _, k := range sortedKeys {
			envEntries = append(envEntries, key{Value: k}, stringValue{Value: p.EnvironmentVariables[k]})
		}
		entries = append(entries, dictValue{Entries: envEntries})
	}

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

// LegacyCronPlistPrefix is the old prefix used before the UUID-based naming switch.
// Used only for migration cleanup.
const LegacyCronPlistPrefix = "agenc-cron-"

// CronToPlistFilename returns the plist filename for a cron job identified by its UUID.
// cronPlistPrefix is the namespace-aware prefix (e.g., "agenc-cron." or "agenc-a1b2c3d4-cron.").
func CronToPlistFilename(cronPlistPrefix string, cronID string) string {
	return fmt.Sprintf("%s%s.plist", cronPlistPrefix, cronID)
}

// CronToLabel returns the launchd label for a cron job identified by its UUID.
// cronPlistPrefix is the namespace-aware prefix (e.g., "agenc-cron." or "agenc-a1b2c3d4-cron.").
func CronToLabel(cronPlistPrefix string, cronID string) string {
	return cronPlistPrefix + cronID
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

// cronFieldDescriptor defines how to parse and validate a single cron field.
type cronFieldDescriptor struct {
	name   string
	min    int
	max    int
	target **int
	// normalize is an optional post-parse transform (e.g., weekday 7→0).
	// nil means no transform is applied.
	normalize func(int) int
}

// ParseCronExpression parses a cron expression into a CalendarInterval.
// Returns error for unsupported expressions.
// Supports: "minute hour * * *" format only (no */N, ranges, or lists)
func ParseCronExpression(cronExpr string) (*CalendarInterval, error) {
	parts := strings.Fields(cronExpr)
	if len(parts) != 5 {
		return nil, stacktrace.NewError("cron expression must have 5 fields (minute hour day month weekday)")
	}

	interval := &CalendarInterval{}

	fields := []cronFieldDescriptor{
		{name: "minute", min: 0, max: 59, target: &interval.Minute},
		{name: "hour", min: 0, max: 23, target: &interval.Hour},
		{name: "day", min: 1, max: 31, target: &interval.Day},
		{name: "month", min: 1, max: 12, target: &interval.Month},
		{name: "weekday", min: 0, max: 7, target: &interval.Weekday, normalize: func(v int) int {
			// Normalize 7 to 0 (both represent Sunday)
			if v == 7 {
				return 0
			}
			return v
		}},
	}

	for i, field := range fields {
		raw := parts[i]
		if raw == "*" {
			continue
		}

		val, err := strconv.Atoi(raw)
		if err != nil {
			return nil, stacktrace.NewError("unsupported %s format: %s (must be integer or *)", field.name, raw)
		}
		if val < field.min || val > field.max {
			return nil, stacktrace.NewError("%s must be between %d and %d", field.name, field.min, field.max)
		}
		if field.normalize != nil {
			val = field.normalize(val)
		}
		*field.target = &val
	}

	return interval, nil
}
