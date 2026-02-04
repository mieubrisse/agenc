package cmd

import (
	"os"
	"os/exec"
	"strings"

	"github.com/mieubrisse/stacktrace"
)

// selectReposWithFzf presents an fzf multi-select picker for repos from the
// repo library and returns the selected canonical repo names. Returns nil (no
// error) if the user cancels with Ctrl-C or Escape.
func selectReposWithFzf(repoNames []string, prompt string) ([]string, error) {
	fzfBinary, err := exec.LookPath("fzf")
	if err != nil {
		return nil, stacktrace.Propagate(err, "'fzf' binary not found in PATH; pass repo names as arguments instead")
	}

	input := strings.Join(repoNames, "\n")

	fzfCmd := exec.Command(fzfBinary,
		"--multi",
		"--prompt", prompt,
	)
	fzfCmd.Stdin = strings.NewReader(input)
	fzfCmd.Stderr = os.Stderr

	output, err := fzfCmd.Output()
	if err != nil {
		// fzf returns exit code 130 on Ctrl-C, and exit code 1 when no match
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
			return nil, nil
		}
		return nil, stacktrace.Propagate(err, "fzf selection failed")
	}

	var selected []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			selected = append(selected, line)
		}
	}
	return selected, nil
}
