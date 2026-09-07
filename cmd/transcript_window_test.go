package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestParseTimeBoundAcceptsEveryDocumentedForm(t *testing.T) {
	reference := time.Date(2026, 9, 7, 15, 12, 0, 0, time.Local)
	cases := []struct {
		value    string
		endOfDay bool
		want     time.Time
	}{
		{"2026-09-07T14:00:00Z", false, time.Date(2026, 9, 7, 14, 0, 0, 0, time.UTC)},
		{"2026-09-07 14:00", false, time.Date(2026, 9, 7, 14, 0, 0, 0, time.Local)},
		{"2026-09-07T14:00", false, time.Date(2026, 9, 7, 14, 0, 0, 0, time.Local)},
		{"2026-09-07", false, time.Date(2026, 9, 7, 0, 0, 0, 0, time.Local)},
		{"2026-09-07", true, time.Date(2026, 9, 7, 23, 59, 59, 999999999, time.Local)},
		{"14:00", false, time.Date(2026, 9, 7, 14, 0, 0, 0, time.Local)},
		{"2h", false, reference.Add(-2 * time.Hour)},
		{"45m", true, reference.Add(-45 * time.Minute)},
	}
	for _, c := range cases {
		got, err := parseTimeBound(c.value, reference, c.endOfDay)
		if err != nil {
			t.Errorf("parseTimeBound(%q): %v", c.value, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("parseTimeBound(%q) = %s, want %s", c.value, got, c.want)
		}
	}
	if _, err := parseTimeBound("yesterday-ish", reference, false); err == nil || !strings.Contains(err.Error(), "is not a time") || !strings.Contains(err.Error(), "'2h'") {
		t.Errorf("an unparseable value must name the accepted forms, got %v", err)
	}
	if z, _ := parseTimeBound("", reference, false); !z.IsZero() {
		t.Errorf("empty must be unbounded")
	}
}

func TestTimeWindowRelativeFormsAnchorOnTheTranscriptsLastRecord(t *testing.T) {
	mainFilepath := writeFakeSession(t, []string{
		fakeUserLine("2026-01-01T10:00:00.000Z", "EARLY"),
		fakeUserLine("2026-01-01T11:30:00.000Z", "MIDDLE"),
		fakeUserLine("2026-01-01T12:00:00.000Z", "LAST"),
	}, nil)

	out, _, err := runPrint(t, mainFilepath, transcriptPrintOptions{all: true, since: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "MIDDLE") || !strings.Contains(out, "LAST") || strings.Contains(out, "EARLY") {
		t.Errorf("--since 1h back from 12:00 must keep 11:30 and 12:00 only, got:\n%s", out)
	}

	out, _, err = runPrint(t, mainFilepath, transcriptPrintOptions{all: true, format: jsonlFormat, since: "2026-01-01T11:00:00Z", until: "2026-01-01T11:59:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "\n") != 1 || !strings.Contains(out, "MIDDLE") {
		t.Errorf("jsonl window must emit exactly the in-window record, got %q", out)
	}

	_, _, err = runPrint(t, mainFilepath, transcriptPrintOptions{all: true, since: "2026-01-01T12:00:00Z", until: "2026-01-01T11:00:00Z"})
	if err == nil || !strings.Contains(err.Error(), "is before --since") {
		t.Errorf("an inverted window must be refused, got %v", err)
	}
	if err := (transcriptPrintOptions{format: textFormat, all: true, since: "1h", listAgents: true}).validate(); err == nil || !strings.Contains(err.Error(), "apply to a transcript render") {
		t.Errorf("--since with --agents must be refused, got %v", err)
	}
}

func TestUntilBareDateEndsAtTheNextLocalMidnight(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no tz database")
	}
	saved := time.Local
	time.Local = berlin
	defer func() { time.Local = saved }()

	for _, day := range []string{"2026-10-25", "2026-03-29", "2026-09-07"} {
		got, err := parseTimeBound(day, time.Time{}, true)
		if err != nil {
			t.Fatal(err)
		}
		y, m, d := got.Date()
		if got.Format("2006-01-02") != day || got.Hour() != 23 || got.Minute() != 59 || got.Second() != 59 {
			t.Errorf("--until %s = %s (%d-%02d-%02d), want the last instant of that local day", day, got, y, m, d)
		}
	}
}

func TestWindowImpliesAllUnlessTailWasGiven(t *testing.T) {
	newCmd := func() (*cobra.Command, *transcriptPrintOptions) {
		opts := &transcriptPrintOptions{}
		cmd := &cobra.Command{Use: "print", Run: func(*cobra.Command, []string) {}}
		cmd.Flags().IntVar(&opts.tailLines, tailFlagName, defaultTailLines, "")
		registerTranscriptPrintFlags(cmd, opts)
		return cmd, opts
	}
	cmd, opts := newCmd()
	cmd.SetArgs([]string{"--since", "1h"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	applyWindowDefault(cmd, opts)
	if !opts.all {
		t.Errorf("a window with the default tail must print everything inside it")
	}

	cmd, opts = newCmd()
	cmd.SetArgs([]string{"--since", "1h", "--tail", "5"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	applyWindowDefault(cmd, opts)
	if opts.all || opts.tailLines != 5 {
		t.Errorf("an explicit --tail must still apply: all=%v tail=%d", opts.all, opts.tailLines)
	}
}
