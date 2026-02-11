package session

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"
)

// FindProjectDirpathByEncoding locates the Claude Code project directory for a
// mission by computing the expected encoded directory name from the agent
// dirpath. This is more reliable than substring matching on the mission UUID.
func FindProjectDirpathByEncoding(agentDirpath string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to determine home directory")
	}

	expectedDirname := EncodeProjectDirname(agentDirpath)
	projectDirpath := filepath.Join(homeDir, ".claude", "projects", expectedDirname)

	if _, err := os.Stat(projectDirpath); err != nil {
		return "", stacktrace.Propagate(err, "project directory not found at '%s'", projectDirpath)
	}

	return projectDirpath, nil
}

// EncodeProjectDirname replicates Claude Code's path-to-dirname encoding.
// Both '/' and '.' are replaced with '-'. Symlinks are resolved first so the
// encoded name matches what Claude Code sees at runtime.
// Example: "/Users/odyssey/.agenc/missions/<uuid>/agent" → "-Users-odyssey--agenc-missions-<uuid>-agent"
func EncodeProjectDirname(agentDirpath string) string {
	// Resolve symlinks so the encoded path matches Claude Code's view.
	// Best-effort: if the path doesn't exist yet, use it as-is.
	if resolved, err := filepath.EvalSymlinks(agentDirpath); err == nil {
		agentDirpath = resolved
	}
	result := strings.ReplaceAll(agentDirpath, "/", "-")
	result = strings.ReplaceAll(result, ".", "-")
	return result
}

// FindProjectDirpath locates the Claude Code project directory for a mission
// by scanning ~/.claude/projects/ for a directory whose name contains the
// mission UUID. Returns the full path to the project directory.
func FindProjectDirpath(missionID string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to determine home directory")
	}

	projectsDirpath := filepath.Join(homeDir, ".claude", "projects")
	entries, err := os.ReadDir(projectsDirpath)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to read projects directory")
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.Contains(entry.Name(), missionID) {
			return filepath.Join(projectsDirpath, entry.Name()), nil
		}
	}

	return "", stacktrace.NewError("no project directory found for mission %s", missionID)
}

// FindLatestSessionID scans the project directory for .jsonl files and returns
// the session UUID from the most recently modified one (extracted from the
// filename by stripping the .jsonl extension).
func FindLatestSessionID(projectDirpath string) (string, error) {
	entries, err := os.ReadDir(projectDirpath)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to read project directory '%s'", projectDirpath)
	}

	var latestFilename string
	var latestModTime int64

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if modTime := info.ModTime().UnixMilli(); modTime > latestModTime {
			latestModTime = modTime
			latestFilename = entry.Name()
		}
	}

	if latestFilename == "" {
		return "", stacktrace.NewError("no session files found in '%s'", projectDirpath)
	}

	sessionID := strings.TrimSuffix(latestFilename, ".jsonl")
	return sessionID, nil
}
