package repo

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// GhHostsConfig represents the structure of ~/.config/gh/hosts.yml.
type GhHostsConfig struct {
	Hosts map[string]GhHostConfig `yaml:",inline"`
}

// GhHostConfig represents the per-host settings in the gh CLI config.
type GhHostConfig struct {
	User        string `yaml:"user"`
	GitProtocol string `yaml:"git_protocol"`
}

// GetGhConfig reads and parses ~/.config/gh/hosts.yml.
// Returns nil if the file doesn't exist or can't be parsed.
func GetGhConfig() *GhHostsConfig {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	hostsFilepath := filepath.Join(homeDir, ".config", "gh", "hosts.yml")
	data, err := os.ReadFile(hostsFilepath)
	if err != nil {
		return nil
	}

	var cfg GhHostsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil
	}

	return &cfg
}

// GetGhConfigProtocol reads the git_protocol setting from gh config.
// Returns (preferSSH, hasConfig) where hasConfig is false if gh is not installed
// or git_protocol is not set.
func GetGhConfigProtocol() (preferSSH bool, hasConfig bool) {
	cfg := GetGhConfig()
	if cfg == nil {
		return false, false
	}

	host, ok := cfg.Hosts["github.com"]
	if !ok {
		return false, false
	}

	if host.GitProtocol == "" {
		return false, false
	}

	return host.GitProtocol == "ssh", true
}

// GetGhLoggedInUser returns the GitHub username of the logged-in gh CLI user.
// Returns empty string if gh is not installed, not logged in, or the check fails.
func GetGhLoggedInUser() string {
	cfg := GetGhConfig()
	if cfg == nil {
		return ""
	}

	host, ok := cfg.Hosts["github.com"]
	if !ok {
		return ""
	}

	return host.User
}

// GetDefaultGitHubUser returns the default GitHub username to use for shorthand expansion.
// Returns the gh CLI logged-in user (from ~/.config/gh/hosts.yml), or empty string if not logged in.
func GetDefaultGitHubUser() string {
	return GetGhLoggedInUser()
}
