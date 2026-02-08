package claudeconfig

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"
)

const (
	// ShadowRepoDirname is the directory name for the internal shadow repo
	// that tracks the user's ~/.claude config files.
	ShadowRepoDirname = "claude-config-shadow"

	// claudeConfigDirVar is the placeholder used in the shadow repo for paths
	// that would otherwise reference ~/.claude.
	claudeConfigDirVar = "${CLAUDE_CONFIG_DIR}"
)

// TrackedFileNames lists the files that are shadowed from ~/.claude into the
// shadow repo. These are individual files (not directories).
var TrackedFileNames = []string{
	"CLAUDE.md",
	"settings.json",
}

// TrackedDirNames lists the directories that are shadowed from ~/.claude into
// the shadow repo. These are recursively copied.
var TrackedDirNames = []string{
	"skills",
	"hooks",
	"commands",
	"agents",
}

// GetShadowRepoDirpath returns the path to the shadow repo directory.
func GetShadowRepoDirpath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ShadowRepoDirname)
}

// InitShadowRepo creates and initializes the shadow repo at the standard
// location within agencDirpath. It runs git init and installs the pre-commit
// hook that rejects un-normalized paths. Returns the shadow repo dirpath.
//
// If the shadow repo already exists (has a .git directory), this is a no-op.
func InitShadowRepo(agencDirpath string) (string, error) {
	shadowDirpath := GetShadowRepoDirpath(agencDirpath)

	// Already initialized?
	gitDirpath := filepath.Join(shadowDirpath, ".git")
	if _, err := os.Stat(gitDirpath); err == nil {
		return shadowDirpath, nil
	}

	if err := os.MkdirAll(shadowDirpath, 0755); err != nil {
		return "", stacktrace.Propagate(err, "failed to create shadow repo directory")
	}

	cmd := exec.Command("git", "init")
	cmd.Dir = shadowDirpath
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", stacktrace.NewError("git init failed in '%s': %s (error: %v)", shadowDirpath, string(output), err)
	}

	if err := installPreCommitHook(shadowDirpath); err != nil {
		return "", stacktrace.Propagate(err, "failed to install pre-commit hook")
	}

	return shadowDirpath, nil
}

// IngestFromClaudeDir copies tracked files from the user's ~/.claude directory
// into the shadow repo, applying path normalization. If any files changed, it
// auto-commits them.
//
// userClaudeDirpath is the path to ~/.claude (or equivalent).
// shadowDirpath is the path to the shadow repo.
func IngestFromClaudeDir(userClaudeDirpath string, shadowDirpath string) error {
	homeDirpath := filepath.Dir(userClaudeDirpath)
	changed := false

	// Ingest tracked files
	for _, fileName := range TrackedFileNames {
		srcFilepath := filepath.Join(userClaudeDirpath, fileName)
		dstFilepath := filepath.Join(shadowDirpath, fileName)

		didChange, err := ingestFile(srcFilepath, dstFilepath, homeDirpath)
		if err != nil {
			return stacktrace.Propagate(err, "failed to ingest file '%s'", fileName)
		}
		if didChange {
			changed = true
		}
	}

	// Ingest tracked directories
	for _, dirName := range TrackedDirNames {
		srcDirpath := filepath.Join(userClaudeDirpath, dirName)
		dstDirpath := filepath.Join(shadowDirpath, dirName)

		didChange, err := ingestDir(srcDirpath, dstDirpath, homeDirpath)
		if err != nil {
			return stacktrace.Propagate(err, "failed to ingest directory '%s'", dirName)
		}
		if didChange {
			changed = true
		}
	}

	if changed {
		if err := commitShadowChanges(shadowDirpath, "Sync from ~/.claude"); err != nil {
			return stacktrace.Propagate(err, "failed to commit shadow repo changes")
		}
	}

	return nil
}

// NormalizePaths replaces absolute and shorthand ~/.claude paths with the
// ${CLAUDE_CONFIG_DIR} placeholder. homeDirpath is the user's home directory
// (e.g., /Users/odyssey).
func NormalizePaths(content []byte, homeDirpath string) []byte {
	claudeDirpath := filepath.Join(homeDirpath, ".claude")

	// Order matters: most specific (longest) first to avoid partial matches.

	// 1. Absolute path with trailing slash: /Users/odyssey/.claude/ → ${CLAUDE_CONFIG_DIR}/
	content = bytes.ReplaceAll(content,
		[]byte(claudeDirpath+"/"),
		[]byte(claudeConfigDirVar+"/"))

	// 2. Absolute path without trailing slash (end of value, before quote, etc.)
	content = bytes.ReplaceAll(content,
		[]byte(claudeDirpath),
		[]byte(claudeConfigDirVar))

	// 3. ${HOME}/.claude/ → ${CLAUDE_CONFIG_DIR}/
	content = bytes.ReplaceAll(content,
		[]byte("${HOME}/.claude/"),
		[]byte(claudeConfigDirVar+"/"))

	// 4. ${HOME}/.claude (no trailing slash)
	content = bytes.ReplaceAll(content,
		[]byte("${HOME}/.claude"),
		[]byte(claudeConfigDirVar))

	// 5. ~/.claude/ → ${CLAUDE_CONFIG_DIR}/
	content = bytes.ReplaceAll(content,
		[]byte("~/.claude/"),
		[]byte(claudeConfigDirVar+"/"))

	// 6. ~/.claude (no trailing slash)
	content = bytes.ReplaceAll(content,
		[]byte("~/.claude"),
		[]byte(claudeConfigDirVar))

	return content
}

// ExpandPaths replaces ${CLAUDE_CONFIG_DIR} placeholders with the actual
// config directory path.
func ExpandPaths(content []byte, claudeConfigDirpath string) []byte {
	return bytes.ReplaceAll(content,
		[]byte(claudeConfigDirVar),
		[]byte(claudeConfigDirpath))
}

// isTextFile returns true if the file extension suggests a text file that
// should have path normalization applied.
func isTextFile(filepath string) bool {
	textExtensions := map[string]bool{
		".json": true,
		".md":   true,
		".sh":   true,
		".bash": true,
		".py":   true,
		".yml":  true,
		".yaml": true,
		".toml": true,
		".txt":  true,
	}
	ext := strings.ToLower(strings.TrimSpace(getFileExtension(filepath)))
	return textExtensions[ext]
}

// getFileExtension returns the file extension including the dot.
func getFileExtension(path string) string {
	base := strings.TrimSuffix(path, "/")
	idx := strings.LastIndex(base, ".")
	if idx < 0 {
		return ""
	}
	return base[idx:]
}

// ingestFile copies a single file from src to dst, applying path normalization
// if it's a text file. Returns true if the destination was changed.
// If the source doesn't exist, removes the destination if it exists.
func ingestFile(srcFilepath string, dstFilepath string, homeDirpath string) (bool, error) {
	// Resolve symlinks so we read actual content
	resolvedSrc, err := resolveSymlink(srcFilepath)
	if err != nil {
		if os.IsNotExist(err) {
			// Source doesn't exist — remove destination if it exists
			if _, statErr := os.Stat(dstFilepath); statErr == nil {
				if removeErr := os.Remove(dstFilepath); removeErr != nil {
					return false, stacktrace.Propagate(removeErr, "failed to remove '%s'", dstFilepath)
				}
				return true, nil
			}
			return false, nil
		}
		return false, stacktrace.Propagate(err, "failed to resolve symlink for '%s'", srcFilepath)
	}

	data, err := os.ReadFile(resolvedSrc)
	if err != nil {
		return false, stacktrace.Propagate(err, "failed to read '%s'", resolvedSrc)
	}

	// Apply path normalization for text files
	if isTextFile(srcFilepath) {
		data = NormalizePaths(data, homeDirpath)
	}

	// Check if destination already has the same content
	existingData, readErr := os.ReadFile(dstFilepath)
	if readErr == nil && bytes.Equal(existingData, data) {
		return false, nil
	}

	if err := os.WriteFile(dstFilepath, data, 0644); err != nil {
		return false, stacktrace.Propagate(err, "failed to write '%s'", dstFilepath)
	}

	return true, nil
}

// ingestDir copies a directory from src to dst, applying path normalization
// to text files. Returns true if any files were changed.
// If the source doesn't exist, removes the destination if it exists.
func ingestDir(srcDirpath string, dstDirpath string, homeDirpath string) (bool, error) {
	// Resolve the source in case it's a symlink
	resolvedSrc, err := resolveSymlink(srcDirpath)
	if err != nil {
		if os.IsNotExist(err) {
			if _, statErr := os.Stat(dstDirpath); statErr == nil {
				if removeErr := os.RemoveAll(dstDirpath); removeErr != nil {
					return false, stacktrace.Propagate(removeErr, "failed to remove '%s'", dstDirpath)
				}
				return true, nil
			}
			return false, nil
		}
		return false, stacktrace.Propagate(err, "failed to resolve symlink for '%s'", srcDirpath)
	}

	info, err := os.Stat(resolvedSrc)
	if err != nil {
		return false, stacktrace.Propagate(err, "failed to stat '%s'", resolvedSrc)
	}
	if !info.IsDir() {
		return false, stacktrace.NewError("'%s' resolves to a non-directory", srcDirpath)
	}

	changed := false

	err = filepath.Walk(resolvedSrc, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(resolvedSrc, path)
		if err != nil {
			return stacktrace.Propagate(err, "failed to compute relative path")
		}

		dstPath := filepath.Join(dstDirpath, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		// For files within the directory, use the original source path
		// (before symlink resolution) for extension detection
		origFilepath := filepath.Join(srcDirpath, relPath)
		didChange, err := ingestFile(path, dstPath, homeDirpath)
		if err != nil {
			return stacktrace.Propagate(err, "failed to ingest '%s'", origFilepath)
		}
		if didChange {
			changed = true
		}
		return nil
	})

	if err != nil {
		return false, stacktrace.Propagate(err, "failed to walk directory '%s'", resolvedSrc)
	}

	return changed, nil
}

// resolveSymlink resolves a path through any symlinks. Returns the resolved
// path, or the original path if it's not a symlink. Returns os.ErrNotExist
// if the path doesn't exist.
func resolveSymlink(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

// commitShadowChanges stages all changes in the shadow repo and creates a
// commit with the given message.
func commitShadowChanges(shadowDirpath string, message string) error {
	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = shadowDirpath
	if output, err := addCmd.CombinedOutput(); err != nil {
		return stacktrace.NewError("git add failed: %s (error: %v)", string(output), err)
	}

	// Check if there are actually staged changes
	diffCmd := exec.Command("git", "diff", "--cached", "--quiet")
	diffCmd.Dir = shadowDirpath
	if err := diffCmd.Run(); err == nil {
		// No staged changes — nothing to commit
		return nil
	}

	commitCmd := exec.Command("git", "commit", "-m", message)
	commitCmd.Dir = shadowDirpath
	commitCmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=AgenC",
		"GIT_AUTHOR_EMAIL=agenc@local",
		"GIT_COMMITTER_NAME=AgenC",
		"GIT_COMMITTER_EMAIL=agenc@local",
	)
	if output, err := commitCmd.CombinedOutput(); err != nil {
		return stacktrace.NewError("git commit failed: %s (error: %v)", string(output), err)
	}

	return nil
}

// installPreCommitHook installs a pre-commit hook in the shadow repo that
// rejects commits containing un-normalized ~/.claude paths.
func installPreCommitHook(shadowDirpath string) error {
	hooksDirpath := filepath.Join(shadowDirpath, ".git", "hooks")
	if err := os.MkdirAll(hooksDirpath, 0755); err != nil {
		return stacktrace.Propagate(err, "failed to create hooks directory")
	}

	hookFilepath := filepath.Join(hooksDirpath, "pre-commit")
	hookScript := `#!/usr/bin/env bash
set -euo pipefail

# Reject commits that contain un-normalized ~/.claude paths.
# All paths in the shadow repo must use ${CLAUDE_CONFIG_DIR} instead.

# Get the home directory to check for absolute paths
home_dirpath="${HOME}"

# Check staged file contents for un-normalized paths
if git diff --cached -U0 --diff-filter=ACM | \
   grep -qE "(${home_dirpath}/\.claude[/\"]|\\$\{HOME\}/\.claude[/\"]|~/\.claude[/\"])"; then
    echo "ERROR: Staged changes contain un-normalized ~/.claude paths." >&2
    echo "All paths must use \${CLAUDE_CONFIG_DIR} instead of:" >&2
    echo "  - ${home_dirpath}/.claude/" >&2
    echo "  - \${HOME}/.claude/" >&2
    echo "  - ~/.claude/" >&2
    echo "" >&2
    echo "Run 'agenc config sync' to re-normalize, or fix manually." >&2
    exit 1
fi
`

	if err := os.WriteFile(hookFilepath, []byte(hookScript), 0755); err != nil {
		return stacktrace.Propagate(err, "failed to write pre-commit hook")
	}

	return nil
}
