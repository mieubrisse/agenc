package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

const (
	testEnvVar     = "AGENC_TEST_ENV"
	baseNamePrefix = "agenc"
)

// GetNamespaceSuffix returns a deterministic suffix derived from agencDirpath.
// If agencDirpath is the default (~/.agenc), returns "" (empty string).
// Otherwise, returns "-" + first 8 hex characters of SHA256 of the resolved path.
func GetNamespaceSuffix(agencDirpath string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Can't determine home dir — assume non-default to be safe
		return computeHashSuffix(agencDirpath)
	}
	defaultPath := filepath.Join(homeDir, defaultAgencDirname)
	if agencDirpath == defaultPath {
		return ""
	}
	return computeHashSuffix(agencDirpath)
}

// computeHashSuffix returns "-" + first 8 hex chars of SHA256 of the path.
func computeHashSuffix(path string) string {
	hash := sha256.Sum256([]byte(path))
	return fmt.Sprintf("-%x", hash[:4])
}

// GetTmuxSessionName returns the user-facing tmux session name.
// Default: "agenc". Namespaced: "agenc-HASH".
func GetTmuxSessionName(agencDirpath string) string {
	return baseNamePrefix + GetNamespaceSuffix(agencDirpath)
}

// GetPoolSessionName returns the pool tmux session name.
// Default: "agenc-pool". Namespaced: "agenc-HASH-pool".
func GetPoolSessionName(agencDirpath string) string {
	return baseNamePrefix + GetNamespaceSuffix(agencDirpath) + "-pool"
}

// GetCronPlistPrefix returns the prefix for cron plist filenames and labels.
// Default: "agenc-cron.". Namespaced: "agenc-HASH-cron.".
func GetCronPlistPrefix(agencDirpath string) string {
	return baseNamePrefix + GetNamespaceSuffix(agencDirpath) + "-cron."
}

// IsTestEnv returns true if AGENC_TEST_ENV is set (to any non-empty value).
func IsTestEnv() bool {
	return os.Getenv(testEnvVar) != ""
}
