package session

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"
)

// CopyAndForkSession copies a session from one project directory to another,
// replacing all sessionId references with a new UUID. This creates a forked
// copy of the conversation that Claude Code will treat as an independent session.
//
// It copies:
//   - The JSONL transcript file (with sessionId replacement)
//   - The session subdirectory (subagents/, tool-results/) as-is
//   - sessions-index.json (with sessionId replacement) if it exists
//   - memory/ directory if it exists (auto-memory)
func CopyAndForkSession(
	srcProjectDirpath string,
	dstProjectDirpath string,
	srcSessionID string,
	newSessionID string,
) error {
	if err := os.MkdirAll(dstProjectDirpath, 0700); err != nil {
		return stacktrace.Propagate(err, "failed to create destination project directory")
	}

	// Copy and rewrite the JSONL transcript
	srcJSONLFilepath := filepath.Join(srcProjectDirpath, srcSessionID+".jsonl")
	dstJSONLFilepath := filepath.Join(dstProjectDirpath, newSessionID+".jsonl")
	if err := copyFileWithSessionIDReplacement(srcJSONLFilepath, dstJSONLFilepath, srcSessionID, newSessionID); err != nil {
		return stacktrace.Propagate(err, "failed to copy session JSONL")
	}

	// Copy the session subdirectory (subagents, tool-results) if it exists
	srcSessionDirpath := filepath.Join(srcProjectDirpath, srcSessionID)
	dstSessionDirpath := filepath.Join(dstProjectDirpath, newSessionID)
	if _, err := os.Stat(srcSessionDirpath); err == nil {
		if err := copyDirRecursive(srcSessionDirpath, dstSessionDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to copy session subdirectory")
		}
	}

	// Copy sessions-index.json if it exists, replacing session IDs
	srcIndexFilepath := filepath.Join(srcProjectDirpath, "sessions-index.json")
	dstIndexFilepath := filepath.Join(dstProjectDirpath, "sessions-index.json")
	if _, err := os.Stat(srcIndexFilepath); err == nil {
		if err := copyFileWithSessionIDReplacement(srcIndexFilepath, dstIndexFilepath, srcSessionID, newSessionID); err != nil {
			return stacktrace.Propagate(err, "failed to copy sessions-index.json")
		}
	}

	// Copy memory directory if it exists
	srcMemoryDirpath := filepath.Join(srcProjectDirpath, "memory")
	dstMemoryDirpath := filepath.Join(dstProjectDirpath, "memory")
	if _, err := os.Stat(srcMemoryDirpath); err == nil {
		if err := copyDirRecursive(srcMemoryDirpath, dstMemoryDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to copy memory directory")
		}
	}

	return nil
}

// copyFileWithSessionIDReplacement copies a file line-by-line, replacing all
// occurrences of oldSessionID with newSessionID. This handles large JSONL files
// efficiently by streaming rather than loading the entire file into memory.
func copyFileWithSessionIDReplacement(srcFilepath string, dstFilepath string, oldSessionID string, newSessionID string) error {
	srcFile, err := os.Open(srcFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open '%s'", srcFilepath)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dstFilepath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return stacktrace.Propagate(err, "failed to create '%s'", dstFilepath)
	}
	defer dstFile.Close()

	scanner := bufio.NewScanner(srcFile)
	// Use a large buffer for potentially large JSONL lines (e.g. assistant messages with tool results)
	scanner.Buffer(make([]byte, 0, 4*1024*1024), 4*1024*1024)

	writer := bufio.NewWriter(dstFile)
	defer writer.Flush()

	for scanner.Scan() {
		line := scanner.Text()
		// Target only the JSON "sessionId" key to avoid replacing UUIDs that
		// happen to appear inside user messages or tool output.
		line = strings.ReplaceAll(line, `"sessionId":"`+oldSessionID+`"`, `"sessionId":"`+newSessionID+`"`)
		line = strings.ReplaceAll(line, `"sessionId": "`+oldSessionID+`"`, `"sessionId": "`+newSessionID+`"`)
		if _, err := writer.WriteString(line + "\n"); err != nil {
			return stacktrace.Propagate(err, "failed to write to '%s'", dstFilepath)
		}
	}

	if err := scanner.Err(); err != nil {
		return stacktrace.Propagate(err, "failed to read '%s'", srcFilepath)
	}

	return nil
}

// copyDirRecursive recursively copies a directory tree from src to dst.
func copyDirRecursive(srcDirpath string, dstDirpath string) error {
	return filepath.Walk(srcDirpath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDirpath, path)
		if err != nil {
			return stacktrace.Propagate(err, "failed to compute relative path")
		}

		dstPath := filepath.Join(dstDirpath, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		// Handle symlinks
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return stacktrace.Propagate(err, "failed to read symlink '%s'", path)
			}
			return os.Symlink(linkTarget, dstPath)
		}

		return copyFile(path, dstPath, info.Mode())
	})
}

// copyFile copies a single file from src to dst with the given permissions.
func copyFile(srcFilepath string, dstFilepath string, mode os.FileMode) error {
	srcFile, err := os.Open(srcFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open '%s'", srcFilepath)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dstFilepath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return stacktrace.Propagate(err, "failed to create '%s'", dstFilepath)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return stacktrace.Propagate(err, "failed to copy '%s' to '%s'", srcFilepath, dstFilepath)
	}

	return nil
}
