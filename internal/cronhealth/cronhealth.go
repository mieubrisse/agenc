// Package cronhealth decides whether AgenC's scheduled cron jobs have stopped
// happening.
//
// A cron loop that stops running emits no signal by itself. In the failure this
// package exists to catch, launchd fired each job on schedule and the spawn
// aborted before agenc ever executed, so there was no stdout to log to, no
// mission row, and no notification — the absence was the only evidence. Six of
// seven crons were dead for a week before anyone noticed.
//
// So the question asked here is not "did something fail" but "has this cron
// produced anything lately". The judgment is kept free of I/O: the caller
// gathers the facts, this package rules on them, which is what makes the ruling
// testable without a launchd, a database, or a clock.
package cronhealth

import (
	"fmt"
	"time"

	"github.com/odyssey/agenc/internal/launchd"
)

// maxScheduleLookbackDays bounds the search for a schedule's fire times. Monthly
// crons are the longest cycle AgenC's cron expressions can express, so a year is
// well past the point where "it never fires" is the answer.
const maxScheduleLookbackDays = 400

// cadenceSampleSize is how many recent fire times are sampled to estimate a
// cron's cadence. More than two because some schedules fire unevenly — a
// weekday-only cron has one-day gaps and one three-day gap — and the largest
// gap is the one that must not be mistaken for silence.
const cadenceSampleSize = 5

// missedCyclesBeforeQuiet is how many of a cron's own cycles must pass with
// nothing produced before it is called quiet.
//
// One is too few. macOS defers launchd jobs across sleep and replays one on
// wake, but does not replay fires missed across a full shutdown, so a machine
// that is off overnight genuinely misses a fire through no fault of the cron
// system. Reporting that would put a false note in front of Kevin after every
// such reboot, and a channel that cries wolf stops being read.
const missedCyclesBeforeQuiet = 2

// CronState is everything needed to judge one cron. The caller gathers these;
// keeping the gathering out of this package is what makes the judgment testable.
type CronState struct {
	Name string
	// ScheduleExpression is the cron expression as written in config.yml,
	// carried so a finding can quote it back to the reader.
	ScheduleExpression string
	// Schedule is that expression parsed into the same calendar interval the
	// syncer writes into the launchd plist. Nil when the expression could not
	// be parsed, which means launchd was never told to run this cron at all.
	Schedule *launchd.CalendarInterval
	// LastMissionAt is when this cron most recently produced a mission. Nil
	// when it has never produced one.
	LastMissionAt *time.Time
	// FirstSeenAt is when the server first observed this cron enabled. It is
	// the reference point for a cron that has never run, and it is what stops a
	// cron created this afternoon from reading as overdue since this morning.
	FirstSeenAt time.Time
}

// Finding is one cron that has gone quiet.
type Finding struct {
	CronName           string
	ScheduleExpression string
	// QuietSince is the moment the cron was last known to be alive: its most
	// recent mission, or failing that, when the server first saw it.
	QuietSince time.Time
	// HasEverRun distinguishes "it stopped" from "it never started", which read
	// very differently to someone deciding whether to worry.
	HasEverRun bool
	// Cadence is the estimated gap between scheduled fires. Zero when the
	// schedule could not be read.
	Cadence time.Duration
	// Detail is the finding as a sentence, for a human reading the note.
	Detail string
}

// Evaluate returns a finding for every cron that has gone quiet, in the order
// the crons were given. An empty result means every cron is producing missions.
//
// Callers wanting only the crons that are *newly* quiet filter the result
// against what they have already reported; that state deliberately lives with
// the caller so this function stays a pure ruling on the present.
func Evaluate(states []CronState, now time.Time) []Finding {
	findings := []Finding{}
	for _, state := range states {
		finding, isQuiet := evaluateOne(state, now)
		if isQuiet {
			findings = append(findings, finding)
		}
	}
	return findings
}

// evaluateOne judges a single cron.
func evaluateOne(state CronState, now time.Time) (Finding, bool) {
	quietSince := state.FirstSeenAt
	hasEverRun := state.LastMissionAt != nil
	if hasEverRun && state.LastMissionAt.After(quietSince) {
		quietSince = *state.LastMissionAt
	}

	// A cron whose schedule AgenC cannot read was never handed to launchd, so
	// it cannot fire and never will. That is worth saying whatever its run
	// history looks like — the syncer skips these silently today.
	cadence, isScheduleReadable := estimateCadence(state.Schedule, now)
	if !isScheduleReadable {
		return Finding{
			CronName:           state.Name,
			ScheduleExpression: state.ScheduleExpression,
			QuietSince:         quietSince,
			HasEverRun:         hasEverRun,
			Detail: fmt.Sprintf(
				"AgenC cannot read this cron's schedule ('%v'), so launchd was never told to run it and it cannot fire",
				state.ScheduleExpression,
			),
		}, true
	}

	quietFor := now.Sub(quietSince)
	if quietFor <= time.Duration(missedCyclesBeforeQuiet)*cadence {
		return Finding{}, false
	}

	return Finding{
		CronName:           state.Name,
		ScheduleExpression: state.ScheduleExpression,
		QuietSince:         quietSince,
		HasEverRun:         hasEverRun,
		Cadence:            cadence,
		Detail:             describeQuiet(state, quietSince, quietFor, cadence, hasEverRun),
	}, true
}

// describeQuiet renders a finding as the sentence a human reads.
func describeQuiet(
	state CronState,
	quietSince time.Time,
	quietFor time.Duration,
	cadence time.Duration,
	hasEverRun bool,
) string {
	skippedCycles := int(quietFor / cadence)

	if !hasEverRun {
		return fmt.Sprintf(
			"has not produced a mission since AgenC first saw it on %v (%v ago) — about %v missed runs on its '%v' schedule",
			quietSince.Local().Format("2006-01-02 15:04"),
			describeApproximateDuration(quietFor),
			skippedCycles,
			state.ScheduleExpression,
		)
	}

	return fmt.Sprintf(
		"last produced a mission on %v (%v ago) — about %v missed runs on its '%v' schedule",
		quietSince.Local().Format("2006-01-02 15:04"),
		describeApproximateDuration(quietFor),
		skippedCycles,
		state.ScheduleExpression,
	)
}

// describeApproximateDuration renders a duration the way someone would say it
// out loud, since the note is prose rather than a metric.
func describeApproximateDuration(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	if d < 48*time.Hour {
		return fmt.Sprintf("%d hours", int(d.Hours()))
	}
	return fmt.Sprintf("%d days", int(d.Hours()/24))
}

// estimateCadence returns the longest gap between consecutive scheduled fires in
// the recent past, and whether the schedule fires often enough to have a cadence
// at all.
//
// The longest gap rather than the most recent one because an uneven schedule —
// weekdays only, say — must not have its three-day weekend gap mistaken for the
// cron having gone quiet.
func estimateCadence(interval *launchd.CalendarInterval, now time.Time) (time.Duration, bool) {
	if interval == nil {
		return 0, false
	}

	fireTimes := make([]time.Time, 0, cadenceSampleSize)
	cursor := now
	for len(fireTimes) < cadenceSampleSize {
		fireTime, hasFired := PreviousFireTime(interval, cursor)
		if !hasFired {
			break
		}
		fireTimes = append(fireTimes, fireTime)
		cursor = fireTime.Add(-time.Minute)
	}

	// Two fires are the minimum that define a gap. A schedule with fewer inside
	// a 400-day lookback (an impossible date such as February 30th) has no
	// cadence to speak of and is reported as unreadable.
	if len(fireTimes) < 2 {
		return 0, false
	}

	longestGap := time.Duration(0)
	for i := 0; i < len(fireTimes)-1; i++ {
		gap := fireTimes[i].Sub(fireTimes[i+1])
		if gap > longestGap {
			longestGap = gap
		}
	}
	return longestGap, true
}

// PreviousFireTime returns the most recent moment at or before now that the
// given calendar interval should have fired, and whether such a moment exists
// inside the lookback horizon.
//
// This reads the same CalendarInterval that is written into the launchd plist
// rather than re-parsing the cron expression, so the monitor's notion of "should
// have fired" cannot drift from what launchd was actually told to do.
func PreviousFireTime(interval *launchd.CalendarInterval, now time.Time) (time.Time, bool) {
	if interval == nil {
		return time.Time{}, false
	}

	nowAtMinute := now.Truncate(time.Minute)
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	for dayOffset := 0; dayOffset <= maxScheduleLookbackDays; dayOffset++ {
		day := startOfToday.AddDate(0, 0, -dayOffset)
		if !dayMatchesInterval(interval, day) {
			continue
		}

		for hour := 23; hour >= 0; hour-- {
			if interval.Hour != nil && *interval.Hour != hour {
				continue
			}
			for minute := 59; minute >= 0; minute-- {
				if interval.Minute != nil && *interval.Minute != minute {
					continue
				}
				candidate := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, now.Location())
				if !candidate.After(nowAtMinute) {
					return candidate, true
				}
			}
		}
	}

	return time.Time{}, false
}

// dayMatchesInterval reports whether the calendar interval's date fields all
// admit the given day. A nil field is a wildcard, matching every value.
func dayMatchesInterval(interval *launchd.CalendarInterval, day time.Time) bool {
	if interval.Day != nil && *interval.Day != day.Day() {
		return false
	}
	if interval.Month != nil && *interval.Month != int(day.Month()) {
		return false
	}
	if interval.Weekday != nil && *interval.Weekday != int(day.Weekday()) {
		return false
	}
	return true
}
