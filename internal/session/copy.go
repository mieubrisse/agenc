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
// replacing "sessionId" JSON keys with a new UUID. This creates a forked copy
// of the conversation that Claude Code will treat as an independent session.
//
// It copies:
//   - The JSONL transcript file (with sessionId key replacement)
//   - The session subdirectory (subagents/, tool-results/) with sessionId
//     key replacement in JSONL files
//   - memory/ directory if it exists (auto-memory)
//
// sessions-index.json is intentionally NOT copied. Claude Code regenerates it
// on session activity, and the source's index may contain stale entries for
// sessions that weren't forked.
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

	// Copy the session subdirectory (subagents, tool-results) if it exists.
	// JSONL files inside get sessionId replacement; other files are copied as-is.
	srcSessionDirpath := filepath.Join(srcProjectDirpath, srcSessionID)
	dstSessionDirpath := filepath.Join(dstProjectDirpath, newSessionID)
	if _, err := os.Stat(srcSessionDirpath); err == nil {
		if err := copyDirWithSessionIDReplacement(srcSessionDirpath, dstSessionDirpath, srcSessionID, newSessionID); err != nil {
			return stacktrace.Propagate(err, "failed to copy session subdirectory")
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

// copyFileWithSessionIDReplacement copies a file line-by-line, replacing
// "sessionId" JSON keys that reference oldSessionID with newSessionID. Only the
// JSON key is targeted (e.g. "sessionId":"<uuid>") so UUIDs appearing in user
// messages or tool output are preserved. Streams line-by-line for large files.
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

// copyDirWithSessionIDReplacement recursively copies a directory tree,
// applying sessionId key replacement to .jsonl files and copying everything
// else as-is. Used for session subdirectories containing subagent logs.
func copyDirWithSessionIDReplacement(srcDirpath string, dstDirpath string, oldSessionID string, newSessionID string) error {
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

		// Apply sessionId replacement to JSONL files (subagent logs reference the parent session)
		if strings.HasSuffix(path, ".jsonl") {
			return copyFileWithSessionIDReplacement(path, dstPath, oldSessionID, newSessionID)
		}

		return copyFile(path, dstPath, info.Mode())
	})
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
