package launchd

import (
	"strings"
	"testing"
)

// The fixtures below are trimmed from real `launchctl print` output. They are
// the two shapes the September 2026 cron outage produced, plus the healthy one.

const launchctlPrintHealthyOutput = `gui/501/agenc-cron.04af416d = {
	active count = 0
	path = /Users/odyssey/Library/LaunchAgents/agenc-cron.04af416d.plist
	state = not running
	runs = 5
	last exit code = 0
}`

const launchctlPrintExitConfigOutput = `gui/501/agenc-cron.04af416d = {
	active count = 0
	state = not running
	runs = 17
	last exit code = 78: EX_CONFIG
}`

const launchctlPrintCodesigningKillOutput = `gui/501/agenc-cron.9f1124cb = {
	active count = 0
	state = not running
	runs = 3
	last exit reason = OS_REASON_CODESIGNING
}`

const launchctlPrintNeverExitedOutput = `gui/501/agenc-cron.3c92d1ba = {
	active count = 0
	state = not running
	runs = 0
	last exit code = (never exited)
}`

func TestParseJobStatusReadsACleanExit(t *testing.T) {
	status := parseJobStatus(launchctlPrintHealthyOutput)
	if !status.Loaded {
		t.Error("expected the job to read as loaded")
	}
	if status.NeverExited {
		t.Error("expected a job with a recorded exit code to not read as never-exited")
	}
	if status.ExitCode == nil || *status.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %v", status.ExitCode)
	}
	if status.ExitReason != "" {
		t.Errorf("expected no exit reason, got '%v'", status.ExitReason)
	}
	if status.Describe() != "launchd's last spawn of it exited cleanly" {
		t.Errorf("unexpected description: '%v'", status.Describe())
	}
}

func TestParseJobStatusReadsTheExitConfigFailureFromTheOutage(t *testing.T) {
	status := parseJobStatus(launchctlPrintExitConfigOutput)
	if status.NeverExited {
		t.Error("expected a failed spawn to read as having exited")
	}
	if status.ExitCode == nil || *status.ExitCode != 78 {
		t.Fatalf("expected exit code 78, got %v", status.ExitCode)
	}
	if status.Describe() != "launchd's last spawn of it exited 78" {
		t.Errorf("unexpected description: '%v'", status.Describe())
	}
}

func TestParseJobStatusReadsACodeSigningKill(t *testing.T) {
	status := parseJobStatus(launchctlPrintCodesigningKillOutput)
	if status.NeverExited {
		t.Error("expected a killed spawn to read as having exited")
	}
	if status.ExitCode != nil {
		t.Errorf("expected no exit code when launchd recorded a kill reason, got %v", *status.ExitCode)
	}
	if status.ExitReason != "OS_REASON_CODESIGNING" {
		t.Errorf("expected the code-signing kill reason, got '%v'", status.ExitReason)
	}
	if !strings.Contains(status.Describe(), "OS_REASON_CODESIGNING") {
		t.Errorf("expected the description to carry the kill reason, got '%v'", status.Describe())
	}
}

func TestParseJobStatusReadsAJobThatHasNotRunYet(t *testing.T) {
	status := parseJobStatus(launchctlPrintNeverExitedOutput)
	if !status.Loaded {
		t.Error("expected the job to read as loaded")
	}
	if !status.NeverExited {
		t.Error("expected a job that has not completed a run to read as never-exited")
	}
	if status.ExitCode != nil {
		t.Errorf("expected no exit code for a job that never exited, got %v", *status.ExitCode)
	}
	if !strings.Contains(status.Describe(), "has not completed a run") {
		t.Errorf("unexpected description: '%v'", status.Describe())
	}
}

func TestDescribeReportsAnUnregisteredJob(t *testing.T) {
	status := &JobStatus{Loaded: false}
	if status.Describe() != "launchd has no job registered for it" {
		t.Errorf("unexpected description: '%v'", status.Describe())
	}
}
