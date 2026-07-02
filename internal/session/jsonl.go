package session

import (
	"bufio"
	"io"
	"os"

	"github.com/mieubrisse/stacktrace"
)

// ScanJSONLLines opens the JSONL file at jsonlFilepath and invokes fn for each
// non-empty line. Unlike bufio.Scanner (which has a per-token size cap),
// ScanJSONLLines has no per-line ceiling — JSONL lines containing inline
// base64 screenshots (routinely >1 MB) are yielded intact.
//
// Lines are yielded without the trailing '\n', and without a preceding '\r'
// on Windows-style line endings. Empty lines (after trimming) are silently
// skipped — fn is not invoked for them. The last line is yielded even when
// the file has no trailing newline.
//
// If fn returns a non-nil error, iteration stops and that error is returned
// unwrapped, so callers may use errors.Is against sentinel errors for early
// termination.
func ScanJSONLLines(jsonlFilepath string, fn func(line []byte) error) error {
	file, err := os.Open(jsonlFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open JSONL file '%s'", jsonlFilepath)
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadBytes('\n')
		line = trimLineEnding(line)
		if len(line) > 0 {
			if fnErr := fn(line); fnErr != nil {
				return fnErr
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return stacktrace.Propagate(readErr, "error reading JSONL file '%s'", jsonlFilepath)
		}
	}
}

// trimLineEnding strips a trailing '\n' and any preceding '\r'.
func trimLineEnding(line []byte) []byte {
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return line
}
