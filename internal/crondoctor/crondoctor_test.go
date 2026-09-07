package crondoctor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/odyssey/agenc/internal/launchd"
)

func intPtr(v int) *int    { return &v }
func boolPtr(v bool) *bool { return &v }

// healthyJob is a launchd job whose last run exited cleanly.
func healthyJob() launchd.JobStatus {
	return launchd.JobStatus{Loaded: true, ExitCode: intPtr(0)}
}

func mustParse(t *testing.T, cronExpr string) *launchd.CalendarInterval {
	t.Helper()
	interval, err := launchd.ParseCronExpression(cronExpr)
	if err != nil {
		t.Fatalf("failed to parse cron expression '%v': %v", cronExpr, err)
	}
	return interval
}

func TestPreviousFireTime(t *testing.T) {
	// 2026-09-06 is a Sunday; 2026-09-04 is a Friday.
	tests := []struct {
		name       string
		cronExpr   string
		now        time.Time
		wantFireAt time.Time
	}{
		{
			name:       "daily schedule earlier the same day",
			cronExpr:   "0 2 * * *",
			now:        time.Date(2026, 9, 6, 21, 15, 0, 0, time.UTC),
			wantFireAt: time.Date(2026, 9, 6, 2, 0, 0, 0, time.UTC),
		},
		{
			name:       "daily schedule not yet reached today rolls to yesterday",
			cronExpr:   "30 23 * * *",
			now:        time.Date(2026, 9, 6, 21, 15, 0, 0, time.UTC),
			wantFireAt: time.Date(2026, 9, 5, 23, 30, 0, 0, time.UTC),
		},
		{
			name:       "weekly schedule rolls back to its weekday",
			cronExpr:   "0 13 * * 5",
			now:        time.Date(2026, 9, 6, 21, 15, 0, 0, time.UTC),
			wantFireAt: time.Date(2026, 9, 4, 13, 0, 0, 0, time.UTC),
		},
		{
			name:       "monthly schedule rolls back to the prior month",
			cronExpr:   "11 5 17 * *",
			now:        time.Date(2026, 9, 6, 21, 15, 0, 0, time.UTC),
			wantFireAt: time.Date(2026, 8, 17, 5, 11, 0, 0, time.UTC),
		},
		{
			name:       "fire time exactly equal to now counts as fired",
			cronExpr:   "0 7 * * *",
			now:        time.Date(2026, 9, 6, 7, 0, 0, 0, time.UTC),
			wantFireAt: time.Date(2026, 9, 6, 7, 0, 0, 0, time.UTC),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, hasFired := PreviousFireTime(mustParse(t, test.cronExpr), test.now)
			if !hasFired {
				t.Fatalf("expected a previous fire time for '%v'", test.cronExpr)
			}
			if !got.Equal(test.wantFireAt) {
				t.Errorf("previous fire time = %v, want %v", got, test.wantFireAt)
			}
		})
	}
}

func TestPreviousFireTime_NilIntervalHasNoFire(t *testing.T) {
	if _, hasFired := PreviousFireTime(nil, time.Now()); hasFired {
		t.Error("a nil interval should report no previous fire time")
	}
}

// TestDiagnose_UnloadedJobIsCritical covers a cron launchd knows nothing about.
func TestDiagnose_UnloadedJobIsCritical(t *testing.T) {
	now := time.Date(2026, 9, 6, 21, 15, 0, 0, time.UTC)
	findings := Diagnose([]CronState{{
		Name:      "hn-daily-pull",
		Schedule:  mustParse(t, "0 2 * * *"),
		JobStatus: launchd.JobStatus{Loaded: false},
	}}, now)

	if len(findings) != 1 {
		t.Fatalf("expected exactly 1 finding, got %d: %v", len(findings), findings)
	}
	if findings[0].Kind != FindingJobNotLoaded {
		t.Errorf("finding kind = %v, want %v", findings[0].Kind, FindingJobNotLoaded)
	}
	if findings[0].Severity != SeverityCritical {
		t.Errorf("severity = %v, want %v", findings[0].Severity, SeverityCritical)
	}
}

// TestDiagnose_SpawnFailureIsCritical is the outage that motivated this package:
// launchd fires on schedule, the spawn aborts before agenc runs, and every
// AgenC-side view of the cron looks normal.
func TestDiagnose_SpawnFailureIsCritical(t *testing.T) {
	now := time.Date(2026, 9, 6, 21, 15, 0, 0, time.UTC)

	exitConfigCode := 78
	spawnFailures := []launchd.JobStatus{
		{Loaded: true, ExitCode: &exitConfigCode},
		{Loaded: true, ExitReason: "OS_REASON_CODESIGNING"},
	}

	for _, jobStatus := range spawnFailures {
		findings := Diagnose([]CronState{{
			Name:      "daily-state-summary",
			Schedule:  mustParse(t, "30 23 * * *"),
			JobStatus: jobStatus,
		}}, now)

		if len(findings) != 1 {
			t.Fatalf("expected exactly 1 finding for %+v, got %d: %v", jobStatus, len(findings), findings)
		}
		if findings[0].Kind != FindingJobSpawnFailed {
			t.Errorf("finding kind = %v, want %v", findings[0].Kind, FindingJobSpawnFailed)
		}
	}
}

func TestDiagnose_HealthyCronProducesNoFindings(t *testing.T) {
	now := time.Date(2026, 9, 6, 21, 15, 0, 0, time.UTC)
	ranAfterLastFire := time.Date(2026, 9, 6, 7, 0, 5, 0, time.UTC)

	findings := Diagnose([]CronState{{
		Name:           "flight-watcher",
		Schedule:       mustParse(t, "0 7 * * *"),
		JobStatus:      healthyJob(),
		LastRunAt:      &ranAfterLastFire,
		LastRunDidWork: boolPtr(true),
	}}, now)

	if len(findings) != 0 {
		t.Errorf("expected no findings for a healthy cron, got %v", findings)
	}
}

func TestDiagnose_MissedRunIsCritical(t *testing.T) {
	now := time.Date(2026, 9, 6, 21, 15, 0, 0, time.UTC)
	staleRun := time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC)

	findings := Diagnose([]CronState{{
		Name:           "hn-daily-pull",
		Schedule:       mustParse(t, "0 2 * * *"),
		JobStatus:      healthyJob(),
		LastRunAt:      &staleRun,
		LastRunDidWork: boolPtr(true),
	}}, now)

	if len(findings) != 1 {
		t.Fatalf("expected exactly 1 finding, got %d: %v", len(findings), findings)
	}
	if findings[0].Kind != FindingMissedRun {
		t.Errorf("finding kind = %v, want %v", findings[0].Kind, FindingMissedRun)
	}
}

// TestDiagnose_RunWithinGraceIsNotYetMissed guards against alerting on a cron
// whose mission is still being created.
func TestDiagnose_RunWithinGraceIsNotYetMissed(t *testing.T) {
	firedAt := time.Date(2026, 9, 6, 7, 0, 0, 0, time.UTC)
	justAfterFiring := firedAt.Add(2 * time.Minute)
	previousRun := time.Date(2026, 9, 5, 7, 0, 0, 0, time.UTC)

	findings := Diagnose([]CronState{{
		Name:           "flight-watcher",
		Schedule:       mustParse(t, "0 7 * * *"),
		JobStatus:      healthyJob(),
		LastRunAt:      &previousRun,
		LastRunDidWork: boolPtr(true),
	}}, justAfterFiring)

	if len(findings) != 0 {
		t.Errorf("expected no findings inside the grace window, got %v", findings)
	}
}

// TestDiagnose_DiedMidRunIsWarned is the defect beads agenc-inrf and
// exobrain-6g1 describe: the cron fires, the mission starts, and it dies
// without delivering anything.
func TestDiagnose_DiedMidRunIsWarned(t *testing.T) {
	now := time.Date(2026, 9, 6, 21, 15, 0, 0, time.UTC)
	ranOnSchedule := time.Date(2026, 9, 6, 7, 0, 3, 0, time.UTC)

	findings := Diagnose([]CronState{{
		Name:           "flight-watcher",
		Schedule:       mustParse(t, "0 7 * * *"),
		JobStatus:      healthyJob(),
		LastRunAt:      &ranOnSchedule,
		LastRunDidWork: boolPtr(false),
	}}, now)

	if len(findings) != 1 {
		t.Fatalf("expected exactly 1 finding, got %d: %v", len(findings), findings)
	}
	if findings[0].Kind != FindingRunDied {
		t.Errorf("finding kind = %v, want %v", findings[0].Kind, FindingRunDied)
	}
	if findings[0].Severity != SeverityWarning {
		t.Errorf("severity = %v, want %v", findings[0].Severity, SeverityWarning)
	}
}

// TestDiagnose_InFlightRunIsNotJudged: a run still executing has no completion
// verdict yet and must not be reported as dead.
func TestDiagnose_InFlightRunIsNotJudged(t *testing.T) {
	now := time.Date(2026, 9, 6, 21, 15, 0, 0, time.UTC)
	ranOnSchedule := time.Date(2026, 9, 6, 7, 0, 3, 0, time.UTC)

	findings := Diagnose([]CronState{{
		Name:           "flight-watcher",
		Schedule:       mustParse(t, "0 7 * * *"),
		JobStatus:      healthyJob(),
		LastRunAt:      &ranOnSchedule,
		LastRunDidWork: nil,
	}}, now)

	if len(findings) != 0 {
		t.Errorf("expected no findings for an in-flight run, got %v", findings)
	}
}

func TestInspectRunCompletion(t *testing.T) {
	// Line shapes taken from real wrapper logs.
	const startedLine = `{"time":"2026-08-30T02:07:10-04:00","level":"INFO","msg":"Wrapper started","mission_id":"3052f4f1"}`
	const promptLine = `{"time":"2026-08-30T02:07:11-04:00","level":"INFO","msg":"Received claude_update","event":"UserPromptSubmit"}`
	const toolUseLine = `{"time":"2026-09-01T02:15:57-04:00","level":"INFO","msg":"Received claude_update","event":"PostToolUse"}`
	const exitingLine = `{"time":"2026-08-30T03:59:39-04:00","level":"INFO","msg":"Wrapper exiting","reason":"signal","signal":"terminated"}`

	tests := []struct {
		name             string
		lines            []string
		wantStillRunning bool
		wantDidWork      bool
	}{
		{
			name:             "died before doing any work",
			lines:            []string{startedLine, promptLine, exitingLine},
			wantStillRunning: false,
			wantDidWork:      false,
		},
		{
			name:             "did work",
			lines:            []string{startedLine, promptLine, toolUseLine, exitingLine},
			wantStillRunning: false,
			wantDidWork:      true,
		},
		{
			name:             "wrapper has not exited yet",
			lines:            []string{startedLine, promptLine},
			wantStillRunning: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logFilepath := filepath.Join(t.TempDir(), "wrapper.log")
			contents := ""
			for _, line := range test.lines {
				contents += line + "\n"
			}
			if err := os.WriteFile(logFilepath, []byte(contents), 0644); err != nil {
				t.Fatalf("failed to write test wrapper log: %v", err)
			}

			completion, err := InspectRunCompletion(logFilepath)
			if err != nil {
				t.Fatalf("InspectRunCompletion failed: %v", err)
			}
			if completion.StillRunning != test.wantStillRunning {
				t.Errorf("StillRunning = %v, want %v", completion.StillRunning, test.wantStillRunning)
			}
			if !test.wantStillRunning && completion.DidWork != test.wantDidWork {
				t.Errorf("DidWork = %v, want %v", completion.DidWork, test.wantDidWork)
			}
		})
	}
}

// TestInspectRunCompletion_MissingLogIsInFlight: the log is created as the
// mission starts, so its absence means we looked too early.
func TestInspectRunCompletion_MissingLogIsInFlight(t *testing.T) {
	completion, err := InspectRunCompletion(filepath.Join(t.TempDir(), "absent.log"))
	if err != nil {
		t.Fatalf("InspectRunCompletion failed: %v", err)
	}
	if !completion.StillRunning {
		t.Error("a missing wrapper log should read as still running, not as a dead run")
	}
}
