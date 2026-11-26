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
//   - paths that resolve through a symlink, at any depth, into agencDirpath
//     or onto another configured writeable copy
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

	// The checks above compare spellings. A symlink anywhere along either path
	// lets two different spellings name one directory, so repeat both checks on
	// the paths as the filesystem resolves them (KEV-435).
	resolved, err := resolveExistingPrefix(abs)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to resolve symlinks in '%v'", abs)
	}
	resolvedAgencDir := resolveExistingPrefixOrSelf(cleanedAgencDir)
	if isSubpath(resolved, resolvedAgencDir) {
		return "", stacktrace.NewError(
			"path '%v' resolves through a symlink to '%v', inside the AgenC directory '%v'; pick a path outside ~/.agenc/",
			abs, resolved, cleanedAgencDir,
		)
	}

	for otherRepo, otherPath := range otherWriteableCopies {
		cleaned := filepath.Clean(otherPath)
		resolvedOther := resolveExistingPrefixOrSelf(cleaned)
		if isSubpath(resolved, resolvedOther) || isSubpath(resolvedOther, resolved) {
			return "", stacktrace.NewError(
				"path '%v' resolves through a symlink to '%v', which overlaps with the writeable copy for '%v' at '%v'",
				abs, resolved, otherRepo, cleaned,
			)
		}
	}

	// A path that is itself a symlink is stored as its target, so the sync loop
	// operates on the real directory rather than through the link.
	if info, err := os.Lstat(abs); err == nil && info.Mode()&os.ModeSymlink != 0 {
		abs = resolved
	}

	return abs, nil
}

// resolveExistingPrefix returns path with every symlink resolved. path need
// not exist: its deepest existing ancestor is resolved and the missing
// elements are appended unchanged, since a writeable copy may name a directory
// that will be cloned later. path must be a cleaned absolute path.
func resolveExistingPrefix(path string) (string, error) {
	existing := path
	var missing []string
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", stacktrace.Propagate(err, "failed to stat '%v'", existing)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		missing = append([]string{filepath.Base(existing)}, missing...)
		existing = parent
	}

	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to resolve '%v'", existing)
	}
	return filepath.Join(append([]string{resolved}, missing...)...), nil
}

// resolveExistingPrefixOrSelf is resolveExistingPrefix for paths the caller did
// not supply — the AgenC directory and copies already registered. When one
// cannot be resolved (a dangling link, a permission error) its spelling is
// used, which the lexical checks have already compared, so one broken entry
// does not block configuring an unrelated repo.
func resolveExistingPrefixOrSelf(path string) string {
	resolved, err := resolveExistingPrefix(path)
	if err != nil {
		return path
	}
	return resolved
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
