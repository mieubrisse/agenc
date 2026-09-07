package launchd

import "testing"

// The fixtures below are trimmed from real `launchctl print` output on
// macOS 26.6. This is screen-scraping of a tool with no machine-readable mode,
// so pinning the exact line shapes is what catches an OS-side format change
// before it silently turns every cron into "healthy".

const healthyJobPrintOutput = `gui/501/agenc-cron.39ee4356 = {
	active count = 0
	state = not running
	domain = gui/501 [100015]
	runs = 5
	last exit code = 0
}`

const configFailureJobPrintOutput = `gui/501/agenc-cron.04af416d = {
	active count = 0
	state = not running
	runs = 16
	last exit code = 78: EX_CONFIG
}`

const killedJobPrintOutput = `gui/501/agenc-cron.72efab32 = {
	active count = 0
	state = not running
	runs = 3
	last exit reason = OS_REASON_CODESIGNING
}`

const neverExitedJobPrintOutput = `gui/501/agenc-cron.bf6eac47 = {
	active count = 0
	state = not running
	runs = 0
	last exit code = (never exited)
}`

func TestParseJobStatus(t *testing.T) {
	tests := []struct {
		name            string
		output          string
		wantHealthy     bool
		wantNeverExited bool
		wantExitCode    *int
		wantExitReason  string
	}{
		{
			name:         "clean exit",
			output:       healthyJobPrintOutput,
			wantHealthy:  true,
			wantExitCode: intPointer(0),
		},
		{
			name:         "spawn aborted with EX_CONFIG",
			output:       configFailureJobPrintOutput,
			wantHealthy:  false,
			wantExitCode: intPointer(78),
		},
		{
			name:           "killed for a code-signing violation",
			output:         killedJobPrintOutput,
			wantHealthy:    false,
			wantExitReason: "OS_REASON_CODESIGNING",
		},
		{
			name:            "loaded but never run",
			output:          neverExitedJobPrintOutput,
			wantHealthy:     true,
			wantNeverExited: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status := parseJobStatus(test.output)

			if !status.Loaded {
				t.Fatal("parsed status should always be Loaded when print succeeded")
			}
			if status.IsHealthy() != test.wantHealthy {
				t.Errorf("IsHealthy() = %v, want %v", status.IsHealthy(), test.wantHealthy)
			}
			if status.NeverExited != test.wantNeverExited {
				t.Errorf("NeverExited = %v, want %v", status.NeverExited, test.wantNeverExited)
			}
			if test.wantExitCode == nil && status.ExitCode != nil {
				t.Errorf("ExitCode = %v, want nil", *status.ExitCode)
			}
			if test.wantExitCode != nil {
				if status.ExitCode == nil {
					t.Fatalf("ExitCode = nil, want %v", *test.wantExitCode)
				}
				if *status.ExitCode != *test.wantExitCode {
					t.Errorf("ExitCode = %v, want %v", *status.ExitCode, *test.wantExitCode)
				}
			}
			if status.ExitReason != test.wantExitReason {
				t.Errorf("ExitReason = %q, want %q", status.ExitReason, test.wantExitReason)
			}
		})
	}
}

func TestJobStatus_UnloadedIsNeverHealthy(t *testing.T) {
	if (JobStatus{Loaded: false}).IsHealthy() {
		t.Error("a job launchd has no registration for cannot be healthy")
	}
}

func intPointer(v int) *int { return &v }
