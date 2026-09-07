// Package crondoctor diagnoses whether AgenC's scheduled cron jobs are still
// doing their job.
//
// A cron loop that stops delivering emits no signal by itself. The three ways a
// cron stops delivering are each invisible in a different place:
//
//   - launchd never spawns the process (a bad job registration). Nothing is
//     written to the cron log and no mission row is created, so every
//     AgenC-side view of the cron looks exactly like a cron that simply has not
//     come due yet.
//   - launchd spawns it but the run produces no mission — visible only as a
//     gap in run history that nobody is watching.
//   - the mission starts and dies partway through. The mission row is
//     indistinguishable from a healthy one.
//
// This package turns each of those into an explicit finding.
package crondoctor

import (
	"fmt"
	"time"

	"github.com/odyssey/agenc/internal/launchd"
)

// maxScheduleLookbackDays bounds the search for a schedule's previous fire
// time. Monthly crons are the longest cycle AgenC's cron expressions can
// express, so a year is far past the point where "it never fired" is the
// answer.
const maxScheduleLookbackDays = 400

// Severity ranks a finding by whether the cron is currently broken or merely
// suspect.
type Severity string

const (
	// SeverityCritical means the cron is not delivering right now.
	SeverityCritical Severity = "critical"
	// SeverityWarning means a past run failed but the schedule is intact.
	SeverityWarning Severity = "warning"
)

// Finding kinds. These are stable machine-readable tags so a caller can group
// or filter findings without matching on prose.
const (
	FindingJobNotLoaded   = "launchd-job-not-loaded"
	FindingJobSpawnFailed = "launchd-spawn-failed"
	FindingMissedRun      = "missed-scheduled-run"
	FindingRunDied        = "run-died-before-completing"
)

// Finding is one thing wrong with one cron.
type Finding struct {
	CronName string
	Kind     string
	Severity Severity
	Detail   string
}

func (f Finding) String() string {
	return fmt.Sprintf("[%v] %v: %v", f.Severity, f.CronName, f.Detail)
}

// CronState is everything the doctor needs to know about one cron in order to
// judge it. The caller is responsible for gathering these facts; keeping the
// gathering out of this package is what makes the judgment testable.
type CronState struct {
	Name string
	// Schedule is the parsed launchd calendar interval. Nil when the cron's
	// schedule could not be parsed, in which case schedule-based checks are
	// skipped.
	Schedule *launchd.CalendarInterval
	// JobStatus is what launchd reports about the job's last spawn.
	JobStatus launchd.JobStatus
	// LastRunAt is when the most recent mission for this cron was created.
	// Nil when the cron has never produced a mission.
	LastRunAt *time.Time
	// LastRunDidWork reports whether that most recent run made at least one
	// tool call. Nil when the run is still in flight, or when there is no run
	// to judge.
	LastRunDidWork *bool
}

// missedRunGrace is how long after a scheduled fire time the doctor waits
// before calling the run missed. A cron takes a few seconds to create its
// mission row; this is slack for a machine that was briefly asleep or busy.
const missedRunGrace = 15 * time.Minute

// Diagnose returns every finding across the supplied crons, in the order the
// crons were given. An empty result means every cron is delivering.
func Diagnose(states []CronState, now time.Time) []Finding {
	findings := []Finding{}
	for _, state := range states {
		findings = append(findings, diagnoseOne(state, now)...)
	}
	return findings
}

// diagnoseOne judges a single cron.
//
// The checks are ordered by how early in the chain the failure occurs, and a
// broken launchd registration short-circuits the schedule check: when the job
// cannot spawn at all, "it also missed its last run" is the same fact reported
// twice.
func diagnoseOne(state CronState, now time.Time) []Finding {
	findings := []Finding{}

	if !state.JobStatus.Loaded {
		findings = append(findings, Finding{
			CronName: state.Name,
			Kind:     FindingJobNotLoaded,
			Severity: SeverityCritical,
			Detail:   "launchd has no job registered for this cron, so it can never fire",
		})
		return findings
	}

	if !state.JobStatus.IsHealthy() {
		findings = append(findings, Finding{
			CronName: state.Name,
			Kind:     FindingJobSpawnFailed,
			Severity: SeverityCritical,
			Detail: fmt.Sprintf(
				"launchd job is failing to start: %v. The schedule still fires, but the process dies before AgenC runs, so nothing is logged and no mission is created",
				state.JobStatus.Describe(),
			),
		})
		return findings
	}

	if missedFinding, didMiss := checkMissedRun(state, now); didMiss {
		findings = append(findings, missedFinding)
	}

	if state.LastRunDidWork != nil && !*state.LastRunDidWork {
		lastRunDescription := "its most recent run"
		if state.LastRunAt != nil {
			lastRunDescription = fmt.Sprintf("its run at %v", state.LastRunAt.Local().Format("2006-01-02 15:04"))
		}
		findings = append(findings, Finding{
			CronName: state.Name,
			Kind:     FindingRunDied,
			Severity: SeverityWarning,
			Detail: fmt.Sprintf(
				"%v made no tool calls at all — the mission started and died before doing any work, delivering nothing",
				lastRunDescription,
			),
		})
	}

	return findings
}

// checkMissedRun reports whether the cron's most recent scheduled fire produced
// no mission.
func checkMissedRun(state CronState, now time.Time) (Finding, bool) {
	if state.Schedule == nil {
		return Finding{}, false
	}

	expectedFireAt, hasFired := PreviousFireTime(state.Schedule, now)
	if !hasFired {
		return Finding{}, false
	}

	// Give the run time to start before judging it missing.
	if now.Before(expectedFireAt.Add(missedRunGrace)) {
		return Finding{}, false
	}

	if state.LastRunAt != nil && !state.LastRunAt.Before(expectedFireAt) {
		return Finding{}, false
	}

	lastRunDescription := "it has never run"
	if state.LastRunAt != nil {
		lastRunDescription = fmt.Sprintf("last ran %v", state.LastRunAt.Local().Format("2006-01-02 15:04"))
	}

	return Finding{
		CronName: state.Name,
		Kind:     FindingMissedRun,
		Severity: SeverityCritical,
		Detail: fmt.Sprintf(
			"no mission was created for the scheduled run at %v — %v",
			expectedFireAt.Format("2006-01-02 15:04"),
			lastRunDescription,
		),
	}, true
}

// PreviousFireTime returns the most recent moment at or before now that the
// given calendar interval should have fired, and whether such a moment exists
// inside the lookback horizon.
//
// This deliberately reads the same CalendarInterval that is written into the
// launchd plist, rather than re-parsing the cron expression, so the doctor's
// notion of "should have fired" cannot drift from what launchd was actually
// told to do.
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
