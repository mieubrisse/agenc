package config

import (
	"os"
	"path/filepath"

	"github.com/mieubrisse/stacktrace"
	"gopkg.in/yaml.v3"
)

// AgentTemplateEntry represents a single agent template in config.yml.
type AgentTemplateEntry struct {
	Repo     string `yaml:"repo"`
	Nickname string `yaml:"nickname,omitempty"`
}

// AgencConfig represents the contents of config.yml.
type AgencConfig struct {
	AgentTemplates []AgentTemplateEntry `yaml:"agent_templates"`
}

// GetConfigFilepath returns the path to config.yml inside the config directory.
func GetConfigFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ConfigDirname, ConfigFilename)
}

// ReadAgencConfig reads and parses config.yml. Returns an empty config if the
// file does not exist.
func ReadAgencConfig(agencDirpath string) (*AgencConfig, error) {
	configFilepath := GetConfigFilepath(agencDirpath)

	data, err := os.ReadFile(configFilepath)
	if err != nil {
		if os.IsNotExist(err) {
			return &AgencConfig{}, nil
		}
		return nil, stacktrace.Propagate(err, "failed to read config file '%s'", configFilepath)
	}

	var cfg AgencConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, stacktrace.Propagate(err, "failed to parse config file '%s'", configFilepath)
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

// EnsureConfigFile creates config.yml with an empty agent_templates list if it
// does not already exist.
func EnsureConfigFile(agencDirpath string) error {
	configFilepath := GetConfigFilepath(agencDirpath)

	if _, err := os.Stat(configFilepath); err == nil {
		return nil
	}

	seed := "agent_templates: []\n"
	if err := os.WriteFile(configFilepath, []byte(seed), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to create config file '%s'", configFilepath)
	}

	return nil
}
