package claudeconfig

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"
)

// userClaudeDirname is the standard name for the user's Claude config directory.
const userClaudeDirname = ".claude"

const (
	// ShadowRepoDirname is the directory name for the internal shadow repo
	// that tracks the user's ~/.claude config files.
	ShadowRepoDirname = "claude-config-shadow"
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

	return shadowDirpath, nil
}

// IngestFromClaudeDir copies tracked files from the user's ~/.claude directory
// into the shadow repo as-is (no path transformation). If any files changed,
// it auto-commits them.
//
// userClaudeDirpath is the path to ~/.claude (or equivalent).
// shadowDirpath is the path to the shadow repo.
func IngestFromClaudeDir(userClaudeDirpath string, shadowDirpath string) error {
	changed := false

	// Ingest tracked files
	for _, fileName := range TrackedFileNames {
		srcFilepath := filepath.Join(userClaudeDirpath, fileName)
		dstFilepath := filepath.Join(shadowDirpath, fileName)

		didChange, err := ingestFile(srcFilepath, dstFilepath)
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

		didChange, err := ingestDir(srcDirpath, dstDirpath)
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

// RewriteClaudePaths replaces all forms of ~/.claude paths with the given
// target path. This is a one-way rewrite used at build time to redirect
// ~/.claude references to the per-mission config directory.
//
// Handles three path forms (most specific first to avoid partial matches):
//   - Absolute: /Users/name/.claude → targetDirpath
//   - ${HOME}/.claude → targetDirpath
//   - ~/.claude → targetDirpath
func RewriteClaudePaths(content []byte, targetDirpath string) []byte {
	homeDirpath, err := os.UserHomeDir()
	if err != nil {
		// If we can't determine home, only rewrite tilde form
		homeDirpath = ""
	}

	if homeDirpath != "" {
		claudeDirpath := filepath.Join(homeDirpath, userClaudeDirname)

		// 1. Absolute path with trailing slash
		content = bytes.ReplaceAll(content,
			[]byte(claudeDirpath+"/"),
			[]byte(targetDirpath+"/"))

		// 2. Absolute path without trailing slash
		content = bytes.ReplaceAll(content,
			[]byte(claudeDirpath),
			[]byte(targetDirpath))
	}

	// 3. ${HOME}/.claude/ with trailing slash
	content = bytes.ReplaceAll(content,
		[]byte("${HOME}/.claude/"),
		[]byte(targetDirpath+"/"))

	// 4. ${HOME}/.claude without trailing slash
	content = bytes.ReplaceAll(content,
		[]byte("${HOME}/.claude"),
		[]byte(targetDirpath))

	// 5. ~/.claude/ with trailing slash
	content = bytes.ReplaceAll(content,
		[]byte("~/.claude/"),
		[]byte(targetDirpath+"/"))

	// 6. ~/.claude without trailing slash
	content = bytes.ReplaceAll(content,
		[]byte("~/.claude"),
		[]byte(targetDirpath))

	return content
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

// ingestFile copies a single file from src to dst as-is (no path
// transformation). Returns true if the destination was changed.
// If the source doesn't exist, removes the destination if it exists.
func ingestFile(srcFilepath string, dstFilepath string) (bool, error) {
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

// ingestDir copies a directory from src to dst as-is (no path transformation).
// Returns true if any files were changed or removed.
// If the source doesn't exist, removes the destination if it exists.
func ingestDir(srcDirpath string, dstDirpath string) (bool, error) {
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

	// Copy new/changed files from source to destination
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

		origFilepath := filepath.Join(srcDirpath, relPath)
		didChange, err := ingestFile(path, dstPath)
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

	// Remove stale entries from destination that no longer exist in source
	if err := removeStaleEntries(dstDirpath, resolvedSrc, &changed); err != nil {
		return false, stacktrace.Propagate(err, "failed to remove stale entries from '%s'", dstDirpath)
	}

	return changed, nil
}

// removeStaleEntries walks the destination directory and removes any files or
// directories that don't have a corresponding entry in the source directory.
// Sets *changed to true if anything was removed.
func removeStaleEntries(dstDirpath string, srcDirpath string, changed *bool) error {
	if _, err := os.Stat(dstDirpath); os.IsNotExist(err) {
		return nil
	}

	// Collect stale paths first to avoid modifying the tree during the walk
	var stalePaths []string
	err := filepath.Walk(dstDirpath, func(dstPath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}

		relPath, err := filepath.Rel(dstDirpath, dstPath)
		if err != nil || relPath == "." {
			return nil
		}

		srcPath := filepath.Join(srcDirpath, relPath)
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			stalePaths = append(stalePaths, dstPath)
			if info.IsDir() {
				return filepath.SkipDir
			}
		}
		return nil
	})
	if err != nil {
		return stacktrace.Propagate(err, "failed to walk destination for stale entries")
	}

	for _, stalePath := range stalePaths {
		os.RemoveAll(stalePath)
		*changed = true
	}

	return nil
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
