package tmux

import (
	"os/exec"
	"strconv"
	"strings"

	"github.com/mieubrisse/stacktrace"
)

// ParseVersion extracts the major and minor version numbers from
// the output of `tmux -V` (e.g. "tmux 3.4" or "tmux 3.3a").
func ParseVersion(versionStr string) (major int, minor int, err error) {
	versionStr = strings.TrimSpace(versionStr)
	// Typical format: "tmux 3.4" or "tmux 3.3a"
	parts := strings.Fields(versionStr)
	if len(parts) < 2 {
		return 0, 0, stacktrace.NewError("unexpected tmux -V output: %q", versionStr)
	}
	versionPart := parts[1]

	// Strip any trailing non-numeric characters (e.g. "3.3a" -> "3.3")
	dotIdx := strings.Index(versionPart, ".")
	if dotIdx < 0 {
		major, err = strconv.Atoi(versionPart)
		if err != nil {
			return 0, 0, stacktrace.Propagate(err, "failed to parse tmux major version from %q", versionPart)
		}
		return major, 0, nil
	}

	major, err = strconv.Atoi(versionPart[:dotIdx])
	if err != nil {
		return 0, 0, stacktrace.Propagate(err, "failed to parse tmux major version from %q", versionPart)
	}

	minorStr := versionPart[dotIdx+1:]
	// Strip trailing non-digit characters (e.g. "3a" -> "3")
	trimmed := strings.TrimRight(minorStr, "abcdefghijklmnopqrstuvwxyz")
	if trimmed == "" {
		return major, 0, nil
	}
	minor, err = strconv.Atoi(trimmed)
	if err != nil {
		return 0, 0, stacktrace.Propagate(err, "failed to parse tmux minor version from %q", minorStr)
	}

	return major, minor, nil
}

// DetectVersion runs `tmux -V` and returns the parsed major and minor version.
func DetectVersion() (major int, minor int, err error) {
	output, err := exec.Command("tmux", "-V").Output()
	if err != nil {
		return 0, 0, stacktrace.NewError("tmux is not installed or not in PATH")
	}
	return ParseVersion(string(output))
}
