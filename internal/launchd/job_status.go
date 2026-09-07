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

// serviceNotFoundMarker is how launchctl reports a label it has no service for.
// launchctl has no machine-readable output mode, so this is matched on prose.
const serviceNotFoundMarker = "Could not find service"

// JobStatus is what launchd knows about a loaded job's most recent spawn.
//
// The distinction that matters: a spawn can fail before the job's program ever
// executes — a code-signing requirement that will not resolve, a bad job
// registration — and when it does, launchd records the failure here and nowhere
// else in the system observes it. There is no stdout yet, so no log line; no
// mission row; no notification. This is the only place that failure is visible.
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

// Describe renders the status as a phrase for a human reading a report.
func (s *JobStatus) Describe() string {
	if !s.Loaded {
		return "launchd has no job registered for it"
	}
	if s.NeverExited {
		return "launchd has it registered but has not completed a run of it yet"
	}
	if s.ExitReason != "" {
		return fmt.Sprintf("launchd's last spawn of it was killed: %v", s.ExitReason)
	}
	if s.ExitCode == nil {
		return "launchd has it registered but reports no exit status"
	}
	if *s.ExitCode == 0 {
		return "launchd's last spawn of it exited cleanly"
	}
	return fmt.Sprintf("launchd's last spawn of it exited %v", *s.ExitCode)
}

// GetJobStatus asks launchd what happened on a job's most recent spawn.
//
// launchctl has no machine-readable output mode, so this parses the human
// output of `launchctl print`.
func (m *Manager) GetJobStatus(label string) (*JobStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	domainTarget := fmt.Sprintf("gui/%d/%v", os.Getuid(), label)
	cmd := exec.CommandContext(ctx, "launchctl", "print", domainTarget)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// launchctl exits nonzero when the service is absent from the domain,
		// which is a status to report rather than a failure to read one.
		if strings.Contains(string(output), serviceNotFoundMarker) {
			return &JobStatus{Loaded: false}, nil
		}
		return nil, stacktrace.Propagate(
			err,
			"failed to read launchd status for job '%v': %v",
			label,
			string(output),
		)
	}

	return parseJobStatus(string(output)), nil
}

// parseJobStatus extracts the last-spawn outcome from `launchctl print` output.
func parseJobStatus(output string) *JobStatus {
	status := &JobStatus{Loaded: true, NeverExited: true}

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
