package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"
)

// ValidateWriteableCopyPath validates a user-supplied writeable-copy path and
// returns the canonical absolute path on success. Validation rejects:
//   - empty input
//   - paths that remain relative after ~ expansion
//   - paths under agencDirpath (would create a recursive sync)
//   - paths overlapping with another configured writeable copy
//   - paths whose parent directory does not exist
//   - paths that resolve through a symlink into agencDirpath
//
// otherWriteableCopies is a map of repo name → writeable-copy path for repos
// other than the one being validated. Callers must filter out the repo
// currently being configured before passing this map.
func ValidateWriteableCopyPath(input, agencDirpath string, otherWriteableCopies map[string]string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", stacktrace.NewError("writeable-copy path is empty")
	}

	expanded, err := expandTilde(input)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to expand path '%v'", input)
	}

	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to make path '%v' absolute", input)
	}
	abs = filepath.Clean(abs)
	if !filepath.IsAbs(abs) {
		return "", stacktrace.NewError("path '%v' is relative after expansion; provide an absolute path", input)
	}

	cleanedAgencDir := filepath.Clean(agencDirpath)
	if isSubpath(abs, cleanedAgencDir) {
		return "", stacktrace.NewError(
			"path '%v' is inside the AgenC directory '%v'; pick a path outside ~/.agenc/",
			abs, cleanedAgencDir,
		)
	}

	for otherRepo, otherPath := range otherWriteableCopies {
		cleaned := filepath.Clean(otherPath)
		if isSubpath(abs, cleaned) || isSubpath(cleaned, abs) {
			return "", stacktrace.NewError(
				"path '%v' overlaps with the writeable copy for '%v' at '%v'",
				abs, otherRepo, cleaned,
			)
		}
	}

	parent := filepath.Dir(abs)
	if _, err := os.Stat(parent); err != nil {
		return "", stacktrace.NewError(
			"parent directory '%v' does not exist; create it before configuring a writeable copy",
			parent,
		)
	}

	if info, err := os.Lstat(abs); err == nil && info.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return "", stacktrace.Propagate(err, "failed to resolve symlink at '%v'", abs)
		}
		resolved = filepath.Clean(resolved)
		// On macOS, /var resolves to /private/var via a system symlink; resolve
		// the agenc dir too so the prefix comparison is apples-to-apples.
		resolvedAgencDir := cleanedAgencDir
		if r, err := filepath.EvalSymlinks(cleanedAgencDir); err == nil {
			resolvedAgencDir = filepath.Clean(r)
		}
		if isSubpath(resolved, resolvedAgencDir) {
			return "", stacktrace.NewError(
				"path '%v' is a symlink resolving into the AgenC directory '%v'; pick a non-symlinked path",
				abs, cleanedAgencDir,
			)
		}
		abs = resolved
	}

	return abs, nil
}

// expandTilde expands a leading ~ in the path to the user's home directory.
// Returns the input unchanged if it does not start with ~.
func expandTilde(input string) (string, error) {
	if !strings.HasPrefix(input, "~") {
		return input, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to resolve home directory")
	}
	return filepath.Join(home, strings.TrimPrefix(input, "~")), nil
}

// isSubpath reports whether candidate is inside (or equal to) parent. Both
// inputs are expected to be cleaned absolute paths.
func isSubpath(candidate, parent string) bool {
	if candidate == parent {
		return true
	}
	rel, err := filepath.Rel(parent, candidate)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..")
}
