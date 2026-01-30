package config

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/kurtosis-tech/stacktrace"
)

const (
	agencDirpathEnvVar  = "AGENC_DIRPATH"
	defaultAgencDirname = ".agenc"

	ConfigDirname         = "config"
	ClaudeDirname         = "claude"
	MissionsDirname       = "missions"
	ArchiveDirname        = "ARCHIVE"
	AgentTemplatesDirname = "agent-templates"
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
		filepath.Join(agencDirpath, ConfigDirname),
		filepath.Join(agencDirpath, ConfigDirname, AgentTemplatesDirname),
		filepath.Join(agencDirpath, ClaudeDirname),
		filepath.Join(agencDirpath, MissionsDirname),
		filepath.Join(agencDirpath, MissionsDirname, ArchiveDirname),
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

// GetArchiveDirpath returns the path to the archive directory.
func GetArchiveDirpath(agencDirpath string) string {
	return filepath.Join(agencDirpath, MissionsDirname, ArchiveDirname)
}

// GetAgentTemplatesDirpath returns the path to the agent-templates directory.
func GetAgentTemplatesDirpath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ConfigDirname, AgentTemplatesDirname)
}

// GetGlobalClaudeDirpath returns the path to the global claude config directory.
func GetGlobalClaudeDirpath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ClaudeDirname)
}

// ListAgentTemplates scans the agent-templates directory and returns a sorted
// list of template names (subdirectory names).
func ListAgentTemplates(agencDirpath string) ([]string, error) {
	templatesDirpath := GetAgentTemplatesDirpath(agencDirpath)
	entries, err := os.ReadDir(templatesDirpath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, stacktrace.Propagate(err, "failed to read agent-templates directory")
	}

	var templateNames []string
	for _, entry := range entries {
		if entry.IsDir() {
			templateNames = append(templateNames, entry.Name())
		}
	}
	sort.Strings(templateNames)
	return templateNames, nil
}

// GetDatabaseFilepath returns the path to the SQLite database file.
func GetDatabaseFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, "database.sqlite")
}
