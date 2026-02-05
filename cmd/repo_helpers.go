package cmd

import (
	"github.com/mieubrisse/stacktrace"
)

// selectReposWithFzf presents an fzf multi-select picker for repos from the
// repo library and returns the selected canonical repo names. Returns nil (no
// error) if the user cancels with Ctrl-C or Escape.
func selectReposWithFzf(repoNames []string, prompt string) ([]string, error) {
	if len(repoNames) == 0 {
		return nil, nil
	}

	// Build rows for the picker (single column: repo display name)
	var rows [][]string
	for _, name := range repoNames {
		rows = append(rows, []string{displayGitRepo(name)})
	}

	indices, err := runFzfPicker(FzfPickerConfig{
		Prompt:      prompt,
		Headers:     []string{"REPO"},
		Rows:        rows,
		MultiSelect: true,
	})
	if err != nil {
		return nil, stacktrace.Propagate(err, "'fzf' binary not found in PATH; pass repo names as arguments instead")
	}
	if indices == nil {
		return nil, nil
	}

	var selected []string
	for _, idx := range indices {
		selected = append(selected, repoNames[idx])
	}
	return selected, nil
}
