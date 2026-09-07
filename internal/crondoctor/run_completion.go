package crondoctor

import (
	"bufio"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
)

// The wrapper log records Claude's hook events as they arrive. A mission that
// did any work at all fired PostToolUse at least once; a mission that died
// before doing anything — an API error at spawn, a connection dropped
// mid-response, a model that went unavailable — fired none.
//
// This is the only completion signal AgenC records that is independent of what
// the cron's skill was supposed to produce. The alternatives do not work:
//
//   - the mission row cannot be used at all: status is computed at read time,
//     and prompt_count is 1 for dead and healthy runs alike
//   - the Stop hook looks like the natural signal and is not. Measured across
//     recent missions, several that plainly did work (30-82 tool calls) logged
//     zero Stop events, so keying on it reports healthy runs as dead
//   - the idle_prompt notification fires even for missions that died at spawn
//
// Zero tool calls is also the right thing to mean semantically: a cron skill
// that reads nothing, calls nothing and writes nothing delivered nothing,
// whatever its exit looked like.
const wrapperToolUseEventMarker = `"event":"PostToolUse"`

// wrapperExitedMarker is logged when the wrapper shuts down. Until it appears,
// the run is still in flight and its completion cannot be judged yet.
const wrapperExitedMarker = `"msg":"Wrapper exiting"`

// RunCompletion is the verdict on a single cron run.
type RunCompletion struct {
	// StillRunning is true when the wrapper has not exited, so there is
	// nothing to judge yet.
	StillRunning bool
	// DidWork is true when the run made at least one tool call. Meaningless
	// while StillRunning is true.
	DidWork bool
}

// InspectRunCompletion reads a mission's wrapper log and reports whether the
// run did any work or died before doing any.
//
// A missing wrapper log is reported as still-running rather than as a failure:
// the log is created as the mission starts, so its absence means the doctor
// looked too early, not that the run died.
func InspectRunCompletion(wrapperLogFilepath string) (RunCompletion, error) {
	file, err := os.Open(wrapperLogFilepath)
	if err != nil {
		if os.IsNotExist(err) {
			return RunCompletion{StillRunning: true}, nil
		}
		return RunCompletion{}, stacktrace.Propagate(
			err,
			"failed to open wrapper log '%v' to judge run completion",
			wrapperLogFilepath,
		)
	}
	defer file.Close()

	sawToolUse := false
	sawWrapperExit := false

	scanner := bufio.NewScanner(file)
	// Wrapper log lines can carry prompt text, which exceeds the scanner's
	// default 64KB line cap and would otherwise abort the scan partway.
	const maxWrapperLogLineBytes = 4 * 1024 * 1024
	scanner.Buffer(make([]byte, 0, 64*1024), maxWrapperLogLineBytes)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, wrapperToolUseEventMarker) {
			sawToolUse = true
		}
		if strings.Contains(line, wrapperExitedMarker) {
			sawWrapperExit = true
		}
	}
	if err := scanner.Err(); err != nil {
		return RunCompletion{}, stacktrace.Propagate(
			err,
			"failed to read wrapper log '%v' to judge run completion",
			wrapperLogFilepath,
		)
	}

	if !sawWrapperExit {
		return RunCompletion{StillRunning: true}, nil
	}

	return RunCompletion{DidWork: sawToolUse}, nil
}
