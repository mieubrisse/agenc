package launchd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/mieubrisse/stacktrace"
)

// exitCodeNeverExitedSentinel is what launchd prints for a job that is loaded
// but has not yet run to completion even once.
const exitCodeNeverExitedSentinel = "(never exited)"

const lastExitCodePrefix = "last exit code = "
const lastExitReasonPrefix = "last exit reason = "

// JobStatus is what launchd knows about a loaded job's most recent run.
//
// The distinction that matters for cron health: a spawn can fail before the
// job's program ever executes, in which case launchd records the failure here
// and nothing else in the system observes it — no stdout, no mission row, no
// notification. This struct is the only place that failure is visible.
type JobStatus struct {
	Loaded bool
	// NeverExited is true when the job is loaded but has not completed a run.
	NeverExited bool
	// ExitCode is the process's exit status. Nil when the job never exited, or
	// when launchd recorded a termination reason instead of an exit code.
	ExitCode *int
	// ExitReason is launchd's symbolic termination reason (e.g.
	// "OS_REASON_CODESIGNING"), set when the job was killed rather than exiting
	// on its own. Empty when an exit code was recorded instead.
	ExitReason string
}

// IsHealthy reports whether the job's last run finished the way a working cron
// job does: it either has not run yet, or it exited zero.
func (s JobStatus) IsHealthy() bool {
	if !s.Loaded {
		return false
	}
	if s.NeverExited {
		return true
	}
	if s.ExitReason != "" {
		return false
	}
	return s.ExitCode != nil && *s.ExitCode == 0
}

// Describe renders the status for a human reading a report.
func (s JobStatus) Describe() string {
	if !s.Loaded {
		return "not loaded in launchd"
	}
	if s.NeverExited {
		return "loaded, no completed run yet"
	}
	if s.ExitReason != "" {
		return fmt.Sprintf("last run killed: %v", s.ExitReason)
	}
	if s.ExitCode == nil {
		return "loaded, exit status unknown"
	}
	return fmt.Sprintf("last run exited %v", *s.ExitCode)
}

// GetJobStatus asks launchd what happened on a job's most recent run.
//
// launchctl has no machine-readable output mode, so this parses the human
// output of `launchctl print`.
func (m *Manager) GetJobStatus(label string) (JobStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	domainTarget := fmt.Sprintf("gui/%d/%v", os.Getuid(), label)
	cmd := exec.CommandContext(ctx, "launchctl", "print", domainTarget)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// launchctl exits nonzero when the service is absent from the domain,
		// which is a finding to report rather than a failure to gather one.
		if strings.Contains(string(output), "Could not find service") {
			return JobStatus{Loaded: false}, nil
		}
		return JobStatus{}, stacktrace.Propagate(
			err,
			"failed to read launchd status for job '%v': %v",
			label,
			string(output),
		)
	}

	return parseJobStatus(string(output)), nil
}

// parseJobStatus extracts the last-run outcome from `launchctl print` output.
func parseJobStatus(output string) JobStatus {
	status := JobStatus{Loaded: true, NeverExited: true}

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if rest, isExitCodeLine := strings.CutPrefix(line, lastExitCodePrefix); isExitCodeLine {
			if rest == exitCodeNeverExitedSentinel {
				continue
			}
			status.NeverExited = false
			// The value is either "78" or "78: EX_CONFIG"; take the number.
			numeric, _, _ := strings.Cut(rest, ":")
			code, err := strconv.Atoi(strings.TrimSpace(numeric))
			if err != nil {
				continue
			}
			status.ExitCode = &code
			continue
		}

		if rest, isExitReasonLine := strings.CutPrefix(line, lastExitReasonPrefix); isExitReasonLine {
			status.NeverExited = false
			status.ExitReason = strings.TrimSpace(rest)
		}
	}

	return status
}

// ReloadJob unloads and reloads a job so launchd rebuilds its registration from
// the plist on disk.
//
// This is the repair for a job whose loaded registration has gone bad while the
// plist itself is still correct — the failure mode where launchd fires on
// schedule but the spawn aborts before the program ever runs.
func (m *Manager) ReloadJob(label string, plistPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	domainTarget := fmt.Sprintf("gui/%d/%v", os.Getuid(), label)
	bootoutCmd := exec.CommandContext(ctx, "launchctl", "bootout", domainTarget)
	if output, err := bootoutCmd.CombinedOutput(); err != nil {
		// A job that is not currently loaded is the state we are about to
		// create, so booting it out failing for that reason is not an error.
		if !strings.Contains(string(output), "Could not find service") &&
			!strings.Contains(string(output), "No such process") {
			return stacktrace.Propagate(
				err,
				"failed to bootout job '%v' before reloading: %v",
				label,
				string(output),
			)
		}
	}

	guiDomain := fmt.Sprintf("gui/%d", os.Getuid())
	bootstrapCmd := exec.CommandContext(ctx, "launchctl", "bootstrap", guiDomain, plistPath)
	if output, err := bootstrapCmd.CombinedOutput(); err != nil {
		return stacktrace.Propagate(
			err,
			"failed to bootstrap job '%v' from plist '%v': %v",
			label,
			plistPath,
			string(output),
		)
	}

	return nil
}
