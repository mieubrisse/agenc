package config

import (
	"os"
	"path/filepath"
	"regexp"

	"github.com/mieubrisse/stacktrace"
	"gopkg.in/yaml.v3"
)

// canonicalRepoRegex matches the canonical repo format: github.com/owner/repo
var canonicalRepoRegex = regexp.MustCompile(`^github\.com/[^/]+/[^/]+$`)

// AgentTemplateProperties holds optional properties for an agent template.
type AgentTemplateProperties struct {
	Nickname string `yaml:"nickname,omitempty"`
}

// AgencConfig represents the contents of config.yml.
type AgencConfig struct {
	AgentTemplates map[string]AgentTemplateProperties `yaml:"agentTemplates"`
}

// GetConfigFilepath returns the path to config.yml inside the config directory.
func GetConfigFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ConfigDirname, ConfigFilename)
}

// ReadAgencConfig reads and parses config.yml. Returns an empty config if the
// file does not exist. Returns an error if any agentTemplates key is not in
// canonical format (github.com/owner/repo).
func ReadAgencConfig(agencDirpath string) (*AgencConfig, error) {
	configFilepath := GetConfigFilepath(agencDirpath)

	data, err := os.ReadFile(configFilepath)
	if err != nil {
		if os.IsNotExist(err) {
			return &AgencConfig{
				AgentTemplates: make(map[string]AgentTemplateProperties),
			}, nil
		}
		return nil, stacktrace.Propagate(err, "failed to read config file '%s'", configFilepath)
	}

	var cfg AgencConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, stacktrace.Propagate(err, "failed to parse config file '%s'", configFilepath)
	}

	if cfg.AgentTemplates == nil {
		cfg.AgentTemplates = make(map[string]AgentTemplateProperties)
	}

	for repo := range cfg.AgentTemplates {
		if !canonicalRepoRegex.MatchString(repo) {
			return nil, stacktrace.NewError(
				"invalid agentTemplates key '%s' in %s; must be in canonical format 'github.com/owner/repo'",
				repo, configFilepath,
			)
		}
	}

	return &cfg, nil
}

// WriteAgencConfig marshals and writes config.yml.
func WriteAgencConfig(agencDirpath string, cfg *AgencConfig) error {
	configFilepath := GetConfigFilepath(agencDirpath)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return stacktrace.Propagate(err, "failed to marshal config")
	}

	if err := os.WriteFile(configFilepath, data, 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write config file '%s'", configFilepath)
	}

	return nil
}

// EnsureConfigFile creates config.yml with an empty agentTemplates map if it
// does not already exist.
func EnsureConfigFile(agencDirpath string) error {
	configFilepath := GetConfigFilepath(agencDirpath)

	if _, err := os.Stat(configFilepath); err == nil {
		return nil
	}

	seed := "agentTemplates: {}\n"
	if err := os.WriteFile(configFilepath, []byte(seed), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to create config file '%s'", configFilepath)
	}

	return nil
}
