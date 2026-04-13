package devcontainer

import (
	"os"
	"path/filepath"
)

// DetectDevcontainer checks if a repository has a devcontainer configuration.
// It checks two locations per the devcontainer spec:
//  1. .devcontainer/devcontainer.json (preferred)
//  2. .devcontainer.json (root file)
//
// Returns the absolute path to the config file and whether one was found.
func DetectDevcontainer(repoDir string) (string, bool) {
	// Check .devcontainer/devcontainer.json first (spec-preferred location)
	subdirPath := filepath.Join(repoDir, ".devcontainer", "devcontainer.json")
	if _, err := os.Stat(subdirPath); err == nil {
		return subdirPath, true
	}

	// Fall back to .devcontainer.json in repo root
	rootPath := filepath.Join(repoDir, ".devcontainer.json")
	if _, err := os.Stat(rootPath); err == nil {
		return rootPath, true
	}

	return "", false
}
