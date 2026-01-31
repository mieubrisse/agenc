package config

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/mieubrisse/stacktrace"
)

const (
	agencDirpathEnvVar  = "AGENC_DIRPATH"
	defaultAgencDirname = ".agenc"

	ClaudeDirname         = "claude"
	UserClaudeDirname     = ".claude"
	MissionsDirname       = "missions"
	ReposDirname = "repos"
	DaemonDirname         = "daemon"
	DaemonPIDFilename     = "daemon.pid"
	DaemonLogFilename     = "daemon.log"
	DaemonVersionFilename = "daemon.version"

	AgentDirname           = "agent"
	WorkspaceDirname       = "workspace"
	PIDFilename            = "pid"
	ClaudeStateFilename    = "claude-state"
	TemplateCommitFilename = "template-commit"
	WrapperLogFilename     = "wrapper.log"
	SettingsLocalFilename  = "settings.local.json"
)

// GetAgencDirpath returns the agenc config directory path, reading from
// the AGENC_DIRPATH environment variable or defaulting to ~/.agenc.
func GetAgencDirpath() (string, error) {
	if envVal := os.Getenv(agencDirpathEnvVar); envVal != "" {
		return envVal, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to determine home directory")
	}
	return filepath.Join(homeDir, defaultAgencDirname), nil
}

// EnsureDirStructure creates the required agenc directory structure if it
// doesn't already exist.
func EnsureDirStructure(agencDirpath string) error {
	dirs := []string{
		filepath.Join(agencDirpath, ReposDirname),
		filepath.Join(agencDirpath, ClaudeDirname),
		filepath.Join(agencDirpath, MissionsDirname),
		filepath.Join(agencDirpath, DaemonDirname),
	}
	for _, dirpath := range dirs {
		if err := os.MkdirAll(dirpath, 0755); err != nil {
			return stacktrace.Propagate(err, "failed to create directory '%s'", dirpath)
		}
	}
	return nil
}

// GetMissionsDirpath returns the path to the missions directory.
func GetMissionsDirpath(agencDirpath string) string {
	return filepath.Join(agencDirpath, MissionsDirname)
}

// GetReposDirpath returns the path to the repos directory.
func GetReposDirpath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ReposDirname)
}

// GetGlobalClaudeDirpath returns the path to the global claude config directory.
func GetGlobalClaudeDirpath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ClaudeDirname)
}

// ListRepos walks the repos/ directory 3 levels deep (host/owner/repo) and
// returns a sorted list of "host/owner/repo" entries.
func ListRepos(agencDirpath string) ([]string, error) {
	reposDirpath := GetReposDirpath(agencDirpath)
	hosts, err := os.ReadDir(reposDirpath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, stacktrace.Propagate(err, "failed to read repos directory")
	}

	var repos []string
	for _, host := range hosts {
		if !host.IsDir() {
			continue
		}
		hostDirpath := filepath.Join(reposDirpath, host.Name())
		owners, _ := os.ReadDir(hostDirpath)
		for _, owner := range owners {
			if !owner.IsDir() {
				continue
			}
			ownerDirpath := filepath.Join(hostDirpath, owner.Name())
			entries, _ := os.ReadDir(ownerDirpath)
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				repos = append(repos, filepath.Join(host.Name(), owner.Name(), entry.Name()))
			}
		}
	}
	sort.Strings(repos)
	return repos, nil
}

// GetDaemonDirpath returns the path to the daemon directory.
func GetDaemonDirpath(agencDirpath string) string {
	return filepath.Join(agencDirpath, DaemonDirname)
}

// GetDaemonPIDFilepath returns the path to the daemon PID file.
func GetDaemonPIDFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, DaemonDirname, DaemonPIDFilename)
}

// GetDaemonLogFilepath returns the path to the daemon log file.
func GetDaemonLogFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, DaemonDirname, DaemonLogFilename)
}

// GetDaemonVersionFilepath returns the path to the daemon version file.
func GetDaemonVersionFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, DaemonDirname, DaemonVersionFilename)
}

// GetDatabaseFilepath returns the path to the SQLite database file.
func GetDatabaseFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, "database.sqlite")
}

// GetUserClaudeDirpath returns the path to the user's standard Claude config
// directory (~/.claude/).
func GetUserClaudeDirpath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to determine home directory")
	}
	return filepath.Join(homeDir, UserClaudeDirname), nil
}

// GetMissionDirpath returns the path to a specific mission directory.
func GetMissionDirpath(agencDirpath string, missionID string) string {
	return filepath.Join(GetMissionsDirpath(agencDirpath), missionID)
}

// GetMissionAgentDirpath returns the path to the agent/ subdirectory within
// a mission. This is the Claude Code project root.
func GetMissionAgentDirpath(agencDirpath string, missionID string) string {
	return filepath.Join(GetMissionDirpath(agencDirpath, missionID), AgentDirname)
}

// GetMissionPIDFilepath returns the path to the wrapper PID file for a mission.
func GetMissionPIDFilepath(agencDirpath string, missionID string) string {
	return filepath.Join(GetMissionDirpath(agencDirpath, missionID), PIDFilename)
}

// GetMissionClaudeStateFilepath returns the path to the claude-state file for
// a mission.
func GetMissionClaudeStateFilepath(agencDirpath string, missionID string) string {
	return filepath.Join(GetMissionDirpath(agencDirpath, missionID), ClaudeStateFilename)
}

// GetMissionTemplateCommitFilepath returns the path to the template-commit file
// for a mission.
func GetMissionTemplateCommitFilepath(agencDirpath string, missionID string) string {
	return filepath.Join(GetMissionDirpath(agencDirpath, missionID), TemplateCommitFilename)
}

// GetMissionWrapperLogFilepath returns the path to the wrapper log file for
// a mission.
func GetMissionWrapperLogFilepath(agencDirpath string, missionID string) string {
	return filepath.Join(GetMissionDirpath(agencDirpath, missionID), WrapperLogFilename)
}

// GetMissionWorkspaceDirpath returns the path to the workspace/ subdirectory
// within a mission's agent directory.
func GetMissionWorkspaceDirpath(agencDirpath string, missionID string) string {
	return filepath.Join(GetMissionAgentDirpath(agencDirpath, missionID), WorkspaceDirname)
}

// GetRepoDirpath returns the path to a specific repo directory.
func GetRepoDirpath(agencDirpath string, repoName string) string {
	return filepath.Join(GetReposDirpath(agencDirpath), repoName)
}
