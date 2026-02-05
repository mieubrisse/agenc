package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/tableprinter"
)

// FzfPickerConfig defines the configuration for an fzf picker.
type FzfPickerConfig struct {
	Prompt       string     // The prompt displayed to the user
	Headers      []string   // Column headers for the table
	Rows         [][]string // Data rows (each row is a slice of column values)
	MultiSelect  bool       // If true, enables multi-select with TAB
	InitialQuery string     // Optional initial query to pre-filter results
}

// runFzfPicker presents an fzf picker with tableprinter-formatted data.
// Returns nil, nil if the user cancels (Ctrl-C/Escape).
// Returns the selected row indices (0-based, corresponding to cfg.Rows).
func runFzfPicker(cfg FzfPickerConfig) ([]int, error) {
	if len(cfg.Rows) == 0 {
		return nil, nil
	}

	fzfBinary, err := exec.LookPath("fzf")
	if err != nil {
		return nil, stacktrace.Propagate(err, "'fzf' binary not found in PATH")
	}

	// Build the table output with hidden index column
	// Format: <index>\t<formatted row>
	// The index is hidden from the user via --with-nth 2..
	var buf bytes.Buffer

	// Convert headers to any slice for tableprinter
	headerRow := make([]any, len(cfg.Headers))
	for i, h := range cfg.Headers {
		headerRow[i] = h
	}
	tbl := tableprinter.NewTable(headerRow...).WithWriter(&buf)

	// Add all data rows to the table
	for _, row := range cfg.Rows {
		rowAny := make([]any, len(row))
		for i, v := range row {
			rowAny[i] = v
		}
		tbl.AddRow(rowAny...)
	}
	tbl.Print()

	// Parse the table output and prepend indices
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) == 0 {
		return nil, nil
	}

	// Build fzf input with index prefixes
	// Header line gets index -1 (will be shown as header, not selectable)
	// Data lines get 0-based indices
	var fzfInput strings.Builder
	fzfInput.WriteString("-1\t")
	fzfInput.WriteString(lines[0]) // header line
	fzfInput.WriteString("\n")

	for i, line := range lines[1:] {
		fzfInput.WriteString(strconv.Itoa(i))
		fzfInput.WriteString("\t")
		fzfInput.WriteString(line)
		fzfInput.WriteString("\n")
	}

	// Build fzf arguments
	fzfArgs := []string{
		"--ansi",
		"--header-lines", "1",
		"--with-nth", "2..",
		"--prompt", cfg.Prompt,
	}

	if cfg.MultiSelect {
		fzfArgs = append(fzfArgs, "--multi")
	}

	if cfg.InitialQuery != "" {
		fzfArgs = append(fzfArgs, "--query", cfg.InitialQuery)
	}

	fzfCmd := exec.Command(fzfBinary, fzfArgs...)
	fzfCmd.Stdin = strings.NewReader(fzfInput.String())
	fzfCmd.Stderr = os.Stderr

	output, err := fzfCmd.Output()
	if err != nil {
		// fzf returns exit code 130 on Ctrl-C, and exit code 1 when no match
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
			return nil, nil
		}
		return nil, stacktrace.Propagate(err, "fzf selection failed")
	}

	// Parse selected indices from output
	var indices []int
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Extract the index from the first tab-separated field
		idxStr, _, found := strings.Cut(line, "\t")
		if !found {
			continue
		}
		idx, parseErr := strconv.Atoi(idxStr)
		if parseErr != nil {
			continue
		}
		indices = append(indices, idx)
	}

	return indices, nil
}

// runFzfPickerWithSentinel is like runFzfPicker but prepends a sentinel row
// (e.g., "NONE") at index -1. Returns the selected indices where -1 represents
// the sentinel row. Use this when you need a "none/skip" option.
func runFzfPickerWithSentinel(cfg FzfPickerConfig, sentinelRow []string) ([]int, error) {
	if len(cfg.Rows) == 0 && len(sentinelRow) == 0 {
		return nil, nil
	}

	fzfBinary, err := exec.LookPath("fzf")
	if err != nil {
		return nil, stacktrace.Propagate(err, "'fzf' binary not found in PATH")
	}

	// Build the table output with hidden index column
	var buf bytes.Buffer

	// Convert headers to any slice for tableprinter
	headerRow := make([]any, len(cfg.Headers))
	for i, h := range cfg.Headers {
		headerRow[i] = h
	}
	tbl := tableprinter.NewTable(headerRow...).WithWriter(&buf)

	// Add sentinel row first
	if len(sentinelRow) > 0 {
		sentinelAny := make([]any, len(sentinelRow))
		for i, v := range sentinelRow {
			sentinelAny[i] = v
		}
		tbl.AddRow(sentinelAny...)
	}

	// Add all data rows to the table
	for _, row := range cfg.Rows {
		rowAny := make([]any, len(row))
		for i, v := range row {
			rowAny[i] = v
		}
		tbl.AddRow(rowAny...)
	}
	tbl.Print()

	// Parse the table output and prepend indices
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) == 0 {
		return nil, nil
	}

	// Build fzf input with index prefixes
	// Header line gets index -2 (placeholder, will be shown as header)
	// Sentinel row gets index -1
	// Data lines get 0-based indices
	var fzfInput strings.Builder
	fzfInput.WriteString("-2\t")
	fzfInput.WriteString(lines[0]) // header line
	fzfInput.WriteString("\n")

	dataStartIdx := 1
	if len(sentinelRow) > 0 && len(lines) > 1 {
		fzfInput.WriteString("-1\t")
		fzfInput.WriteString(lines[1]) // sentinel row
		fzfInput.WriteString("\n")
		dataStartIdx = 2
	}

	for i, line := range lines[dataStartIdx:] {
		fzfInput.WriteString(strconv.Itoa(i))
		fzfInput.WriteString("\t")
		fzfInput.WriteString(line)
		fzfInput.WriteString("\n")
	}

	// Build fzf arguments
	fzfArgs := []string{
		"--ansi",
		"--header-lines", "1",
		"--with-nth", "2..",
		"--prompt", cfg.Prompt,
	}

	if cfg.MultiSelect {
		fzfArgs = append(fzfArgs, "--multi")
	}

	if cfg.InitialQuery != "" {
		fzfArgs = append(fzfArgs, "--query", cfg.InitialQuery)
	}

	fzfCmd := exec.Command(fzfBinary, fzfArgs...)
	fzfCmd.Stdin = strings.NewReader(fzfInput.String())
	fzfCmd.Stderr = os.Stderr

	output, err := fzfCmd.Output()
	if err != nil {
		// fzf returns exit code 130 on Ctrl-C, and exit code 1 when no match
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
			return nil, nil
		}
		return nil, stacktrace.Propagate(err, "fzf selection failed")
	}

	// Parse selected indices from output
	var indices []int
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Extract the index from the first tab-separated field
		idxStr, _, found := strings.Cut(line, "\t")
		if !found {
			continue
		}
		idx, parseErr := strconv.Atoi(idxStr)
		if parseErr != nil {
			continue
		}
		indices = append(indices, idx)
	}

	return indices, nil
}
