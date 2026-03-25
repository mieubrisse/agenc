package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/tableprinter"
)

// FzfPickerConfig defines the configuration for an fzf picker.
//
// NOTE: fzf strips leading whitespace from displayed lines. If the first column
// may be empty or contain only spaces, use a visible placeholder character
// instead to maintain column alignment.
type FzfPickerConfig struct {
	Prompt       string     // The prompt displayed to the user
	Headers      []string   // Column headers for the table (if provided, none may be blank)
	Rows         [][]string // Data rows (each row is a slice of column values)
	MultiSelect  bool       // If true, enables multi-select with TAB
	InitialQuery string     // Optional initial query to pre-filter results
}

// validateHeaders returns an error if any header in the slice is blank (empty
// or whitespace-only). An empty headers slice is valid (no headers displayed).
func validateHeaders(headers []string) error {
	for i, h := range headers {
		if strings.TrimSpace(h) == "" {
			return stacktrace.NewError("header at index %d is blank; fzf strips leading whitespace which breaks column alignment", i)
		}
	}
	return nil
}

// runFzfPicker presents an fzf picker with tableprinter-formatted data.
// Returns nil, nil if the user cancels (Ctrl-C/Escape).
// Returns the selected row indices (0-based, corresponding to cfg.Rows).
func runFzfPicker(cfg FzfPickerConfig) ([]int, error) {
	return runFzfPickerCore(cfg, nil)
}

// runFzfPickerWithSentinel is like runFzfPicker but prepends a sentinel row
// (e.g., "NONE") at index -1. Returns the selected indices where -1 represents
// the sentinel row. Use this when you need a "none/skip" option.
func runFzfPickerWithSentinel(cfg FzfPickerConfig, sentinelRow []string) ([]int, error) {
	return runFzfPickerCore(cfg, sentinelRow)
}

// toAnySlice converts a string slice to an any slice for tableprinter.
func toAnySlice(strs []string) []any {
	result := make([]any, len(strs))
	for i, s := range strs {
		result[i] = s
	}
	return result
}

// buildFzfTableInput renders cfg.Rows (and optional sentinelRow) through the
// tableprinter, then prepends hidden index columns for fzf selection.
func buildFzfTableInput(cfg FzfPickerConfig, sentinelRow []string) string {
	hasSentinel := len(sentinelRow) > 0

	var buf bytes.Buffer
	tbl := tableprinter.NewTable(toAnySlice(cfg.Headers)...).WithWriter(&buf)

	if hasSentinel {
		tbl.AddRow(toAnySlice(sentinelRow)...)
	}
	for _, row := range cfg.Rows {
		tbl.AddRow(toAnySlice(row)...)
	}
	tbl.Print()

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}

	var fzfInput strings.Builder
	headerIdx := "-1"
	if hasSentinel {
		headerIdx = "-2"
	}
	fzfInput.WriteString(headerIdx)
	fzfInput.WriteString("\t")
	fzfInput.WriteString(lines[0])
	fzfInput.WriteString("\n")

	dataStartIdx := 1
	if hasSentinel && len(lines) > 1 {
		fzfInput.WriteString("-1\t")
		fzfInput.WriteString(lines[1])
		fzfInput.WriteString("\n")
		dataStartIdx = 2
	}

	for i, line := range lines[dataStartIdx:] {
		fzfInput.WriteString(strconv.Itoa(i))
		fzfInput.WriteString("\t")
		fzfInput.WriteString(line)
		fzfInput.WriteString("\n")
	}

	return fzfInput.String()
}

// buildFzfArgs assembles the fzf command-line arguments from the picker config.
func buildFzfArgs(cfg FzfPickerConfig) []string {
	args := []string{
		"--ansi",
		"--header-lines", "1",
		"--with-nth", "2..",
		"--prompt", cfg.Prompt,
	}
	if cfg.MultiSelect {
		args = append(args, "--multi")
	}
	if cfg.InitialQuery != "" {
		args = append(args, "--query", cfg.InitialQuery)
	}
	return args
}

// parseFzfOutput extracts selected row indices from fzf's tab-prefixed output.
func parseFzfOutput(output []byte) []int {
	var indices []int
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
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
	return indices
}

// runFzfPickerCore is the shared implementation for fzf pickers. When sentinelRow
// is nil, it behaves like runFzfPicker. When sentinelRow is provided, it behaves
// like runFzfPickerWithSentinel.
func runFzfPickerCore(cfg FzfPickerConfig, sentinelRow []string) ([]int, error) {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return nil, stacktrace.NewError("interactive selection requires a terminal; provide arguments directly instead")
	}

	if err := validateHeaders(cfg.Headers); err != nil {
		return nil, stacktrace.Propagate(err, "")
	}

	hasSentinel := len(sentinelRow) > 0
	if len(cfg.Rows) == 0 && !hasSentinel {
		return nil, nil
	}

	fzfBinary, err := exec.LookPath("fzf")
	if err != nil {
		return nil, stacktrace.Propagate(err, "'fzf' binary not found in PATH")
	}

	fzfInput := buildFzfTableInput(cfg, sentinelRow)
	if fzfInput == "" {
		return nil, nil
	}

	fzfCmd := exec.Command(fzfBinary, buildFzfArgs(cfg)...)
	fzfCmd.Stdin = strings.NewReader(fzfInput)
	fzfCmd.Stderr = os.Stderr

	output, err := fzfCmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
			return nil, nil
		}
		return nil, stacktrace.Propagate(err, "fzf selection failed")
	}

	return parseFzfOutput(output), nil
}
