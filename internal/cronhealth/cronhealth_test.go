package cronhealth

import (
	"strings"
	"testing"
	"time"

	"github.com/odyssey/agenc/internal/launchd"
)

// mustParseSchedule builds the calendar interval for a cron expression the way
// the syncer does, failing the test if the expression is not representable.
func mustParseSchedule(t *testing.T, cronExpression string) *launchd.CalendarInterval {
	t.Helper()
	interval, err := launchd.ParseCronExpression(cronExpression)
	if err != nil {
		t.Fatalf("failed to parse schedule '%v': %v", cronExpression, err)
	}
	return interval
}

// referenceNow is a fixed moment so schedule arithmetic in these tests is
// deterministic. A Wednesday, deliberately mid-week and mid-month. UTC rather
// than local so that a machine in a daylight-saving zone does not shift the gaps
// this file asserts on — the production code follows now.Location(), and pinning
// the location here is what keeps the arithmetic under test rather than the test
// runner's timezone.
var referenceNow = time.Date(2026, 9, 9, 14, 0, 0, 0, time.UTC)

func TestEvaluateStaysSilentForACronThatRanRecently(t *testing.T) {
	lastMissionAt := referenceNow.Add(-1 * time.Hour)
	states := []CronState{{
		Name:               "daily-state-summary",
		ScheduleExpression: "30 23 * * *",
		Schedule:           mustParseSchedule(t, "30 23 * * *"),
		LastMissionAt:      &lastMissionAt,
		FirstSeenAt:        referenceNow.AddDate(0, -2, 0),
	}}

	if findings := Evaluate(states, referenceNow); len(findings) != 0 {
		t.Fatalf("expected no findings for a cron that just ran, got %+v", findings)
	}
}

func TestEvaluateToleratesASingleMissedCycle(t *testing.T) {
	// 25 hours quiet on a daily cron is one skipped run, which a machine that
	// was shut down overnight produces innocently.
	lastMissionAt := referenceNow.Add(-25 * time.Hour)
	states := []CronState{{
		Name:               "daily-state-summary",
		ScheduleExpression: "30 23 * * *",
		Schedule:           mustParseSchedule(t, "30 23 * * *"),
		LastMissionAt:      &lastMissionAt,
		FirstSeenAt:        referenceNow.AddDate(0, -2, 0),
	}}

	if findings := Evaluate(states, referenceNow); len(findings) != 0 {
		t.Fatalf("expected one skipped cycle to be tolerated, got %+v", findings)
	}
}

func TestEvaluateReportsADailyCronThatMissedTwoCycles(t *testing.T) {
	lastMissionAt := referenceNow.Add(-50 * time.Hour)
	states := []CronState{{
		Name:               "hn-daily-pull",
		ScheduleExpression: "0 2 * * *",
		Schedule:           mustParseSchedule(t, "0 2 * * *"),
		LastMissionAt:      &lastMissionAt,
		FirstSeenAt:        referenceNow.AddDate(0, -2, 0),
	}}

	findings := Evaluate(states, referenceNow)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].CronName != "hn-daily-pull" {
		t.Errorf("expected the finding to name hn-daily-pull, got '%v'", findings[0].CronName)
	}
	if !findings[0].HasEverRun {
		t.Errorf("expected the finding to record that the cron has run before")
	}
	if findings[0].Cadence != 24*time.Hour {
		t.Errorf("expected a 24h cadence, got %v", findings[0].Cadence)
	}
	if !findings[0].QuietSince.Equal(lastMissionAt) {
		t.Errorf("expected quiet-since to be the last mission at %v, got %v", lastMissionAt, findings[0].QuietSince)
	}
	if !strings.Contains(findings[0].Detail, "last produced a mission") {
		t.Errorf("expected the detail to describe the last mission, got '%v'", findings[0].Detail)
	}
}

func TestEvaluateReportsACronThatHasNeverRun(t *testing.T) {
	states := []CronState{{
		Name:               "verify-workspace-mcp-denylist",
		ScheduleExpression: "0 2 * * *",
		Schedule:           mustParseSchedule(t, "0 2 * * *"),
		LastMissionAt:      nil,
		FirstSeenAt:        referenceNow.Add(-72 * time.Hour),
	}}

	findings := Evaluate(states, referenceNow)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].HasEverRun {
		t.Errorf("expected the finding to record that the cron has never run")
	}
	if !strings.Contains(findings[0].Detail, "since AgenC first saw it") {
		t.Errorf("expected the detail to measure from first sight, got '%v'", findings[0].Detail)
	}
}

func TestEvaluateStaysSilentForABrandNewCronWhoseFireTimeHasPassed(t *testing.T) {
	// The false positive the previous attempt left unfixed: a cron created this
	// afternoon on an overnight schedule has its most recent expected fire in
	// the past and no run history, which reads as long overdue without a
	// first-seen reference point.
	states := []CronState{{
		Name:               "just-created",
		ScheduleExpression: "0 2 * * *",
		Schedule:           mustParseSchedule(t, "0 2 * * *"),
		LastMissionAt:      nil,
		FirstSeenAt:        referenceNow.Add(-1 * time.Hour),
	}}

	if findings := Evaluate(states, referenceNow); len(findings) != 0 {
		t.Fatalf("expected a brand-new cron to be left alone, got %+v", findings)
	}
}

func TestEvaluateMeasuresFromFirstSightWhenTheLastMissionPredatesIt(t *testing.T) {
	// A cron disabled for months and re-enabled today has its state row deleted
	// while disabled, so first sight is today and its ancient last mission must
	// not make it look overdue.
	lastMissionAt := referenceNow.AddDate(0, -6, 0)
	states := []CronState{{
		Name:               "re-enabled",
		ScheduleExpression: "0 2 * * *",
		Schedule:           mustParseSchedule(t, "0 2 * * *"),
		LastMissionAt:      &lastMissionAt,
		FirstSeenAt:        referenceNow.Add(-1 * time.Hour),
	}}

	if findings := Evaluate(states, referenceNow); len(findings) != 0 {
		t.Fatalf("expected a just-re-enabled cron to be left alone, got %+v", findings)
	}
}

func TestEvaluateReportsAnUnreadableScheduleEvenWhenTheCronRanRecently(t *testing.T) {
	// A schedule AgenC cannot parse was never handed to launchd, so the cron
	// cannot fire regardless of what its history looks like.
	lastMissionAt := referenceNow.Add(-1 * time.Minute)
	states := []CronState{{
		Name:               "hand-edited",
		ScheduleExpression: "*/15 * * * *",
		Schedule:           nil,
		LastMissionAt:      &lastMissionAt,
		FirstSeenAt:        referenceNow.AddDate(0, -2, 0),
	}}

	findings := Evaluate(states, referenceNow)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if !strings.Contains(findings[0].Detail, "cannot read this cron's schedule") {
		t.Errorf("expected the detail to name the unreadable schedule, got '%v'", findings[0].Detail)
	}
	if findings[0].Cadence != 0 {
		t.Errorf("expected no cadence for an unreadable schedule, got %v", findings[0].Cadence)
	}
}

func TestEvaluateTreatsAScheduleThatCanNeverFireAsUnreadable(t *testing.T) {
	// February 30th parses field-by-field but no such date exists.
	states := []CronState{{
		Name:               "impossible-date",
		ScheduleExpression: "0 2 30 2 *",
		Schedule:           mustParseSchedule(t, "0 2 30 2 *"),
		LastMissionAt:      nil,
		FirstSeenAt:        referenceNow.AddDate(0, -2, 0),
	}}

	findings := Evaluate(states, referenceNow)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if !strings.Contains(findings[0].Detail, "cannot read this cron's schedule") {
		t.Errorf("expected an impossible date to read as unreadable, got '%v'", findings[0].Detail)
	}
}

func TestEvaluateScalesTheThresholdToAWeeklyCron(t *testing.T) {
	weeklySchedule := mustParseSchedule(t, "0 23 * * 4")

	quietTenDays := referenceNow.Add(-10 * 24 * time.Hour)
	stillWithinTolerance := []CronState{{
		Name:               "exobrain-update",
		ScheduleExpression: "0 23 * * 4",
		Schedule:           weeklySchedule,
		LastMissionAt:      &quietTenDays,
		FirstSeenAt:        referenceNow.AddDate(0, -6, 0),
	}}
	if findings := Evaluate(stillWithinTolerance, referenceNow); len(findings) != 0 {
		t.Fatalf("expected ten days quiet to be within a weekly cron's tolerance, got %+v", findings)
	}

	quietSixteenDays := referenceNow.Add(-16 * 24 * time.Hour)
	pastTolerance := []CronState{{
		Name:               "exobrain-update",
		ScheduleExpression: "0 23 * * 4",
		Schedule:           weeklySchedule,
		LastMissionAt:      &quietSixteenDays,
		FirstSeenAt:        referenceNow.AddDate(0, -6, 0),
	}}
	findings := Evaluate(pastTolerance, referenceNow)
	if len(findings) != 1 {
		t.Fatalf("expected sixteen days quiet to be reported, got %d findings", len(findings))
	}
	if findings[0].Cadence != 7*24*time.Hour {
		t.Errorf("expected a 7-day cadence, got %v", findings[0].Cadence)
	}
}

func TestEvaluateReportsEveryQuietCronSoASingleNoteCanNameThemAll(t *testing.T) {
	// The September outage took six crons at once. The caller batches these into
	// one note, which it can only do if it gets them all in one pass.
	longQuiet := referenceNow.Add(-100 * time.Hour)
	healthy := referenceNow.Add(-1 * time.Hour)
	states := []CronState{
		{Name: "first", ScheduleExpression: "0 2 * * *", Schedule: mustParseSchedule(t, "0 2 * * *"), LastMissionAt: &longQuiet, FirstSeenAt: referenceNow.AddDate(0, -2, 0)},
		{Name: "healthy", ScheduleExpression: "0 2 * * *", Schedule: mustParseSchedule(t, "0 2 * * *"), LastMissionAt: &healthy, FirstSeenAt: referenceNow.AddDate(0, -2, 0)},
		{Name: "second", ScheduleExpression: "0 7 * * *", Schedule: mustParseSchedule(t, "0 7 * * *"), LastMissionAt: &longQuiet, FirstSeenAt: referenceNow.AddDate(0, -2, 0)},
	}

	findings := Evaluate(states, referenceNow)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
	if findings[0].CronName != "first" || findings[1].CronName != "second" {
		t.Errorf("expected findings in input order naming first and second, got %v and %v", findings[0].CronName, findings[1].CronName)
	}
}

func TestEstimateCadenceUsesTheLongestGapForAnUnevenSchedule(t *testing.T) {
	// A monthly cron's gaps vary between 28 and 31 days. Taking the most recent
	// gap could pick February's, which would report a healthy March run as quiet
	// three days early.
	monthlySchedule := mustParseSchedule(t, "11 5 17 * *")

	// Evaluated in early March so the sampled window spans February.
	marchNow := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
	cadence, isReadable := estimateCadence(monthlySchedule, marchNow)
	if !isReadable {
		t.Fatal("expected a monthly schedule to have a readable cadence")
	}
	if cadence != 31*24*time.Hour {
		t.Errorf("expected the longest monthly gap of 31 days, got %v", cadence)
	}
}

func TestEstimateCadenceHandlesAnEveryMinuteSchedule(t *testing.T) {
	cadence, isReadable := estimateCadence(mustParseSchedule(t, "* * * * *"), referenceNow)
	if !isReadable {
		t.Fatal("expected an every-minute schedule to have a readable cadence")
	}
	if cadence != time.Minute {
		t.Errorf("expected a one-minute cadence, got %v", cadence)
	}
}

func TestPreviousFireTimeFindsTheMostRecentScheduledMoment(t *testing.T) {
	// referenceNow is 14:00; a 02:00 daily cron last fired 12 hours ago.
	fireTime, hasFired := PreviousFireTime(mustParseSchedule(t, "0 2 * * *"), referenceNow)
	if !hasFired {
		t.Fatal("expected a daily cron to have fired already today")
	}
	expected := time.Date(2026, 9, 9, 2, 0, 0, 0, time.UTC)
	if !fireTime.Equal(expected) {
		t.Errorf("expected the previous fire at %v, got %v", expected, fireTime)
	}
}

func TestPreviousFireTimeWalksBackToThePreviousMatchingWeekday(t *testing.T) {
	// referenceNow is a Wednesday; a Thursday cron last fired six days earlier.
	fireTime, hasFired := PreviousFireTime(mustParseSchedule(t, "0 23 * * 4"), referenceNow)
	if !hasFired {
		t.Fatal("expected a weekly cron to have fired within the lookback")
	}
	expected := time.Date(2026, 9, 3, 23, 0, 0, 0, time.UTC)
	if !fireTime.Equal(expected) {
		t.Errorf("expected the previous fire at %v, got %v", expected, fireTime)
	}
}

func TestPreviousFireTimeReportsNoFireForAnImpossibleDate(t *testing.T) {
	if _, hasFired := PreviousFireTime(mustParseSchedule(t, "0 2 30 2 *"), referenceNow); hasFired {
		t.Error("expected February 30th to never fire")
	}
}
