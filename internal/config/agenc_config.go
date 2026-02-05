package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/mieubrisse/stacktrace"
)

// canonicalRepoRegex matches the canonical repo format: github.com/owner/repo
var canonicalRepoRegex = regexp.MustCompile(`^github\.com/[^/]+/[^/]+$`)

// ValidDefaultForValues lists the recognized values for the defaultFor field.
// This is the single source of truth; all validation and help text derive from it.
var ValidDefaultForValues = []string{"emptyMission", "repo", "agentTemplate"}

// AgentTemplateProperties holds optional properties for an agent template.
type AgentTemplateProperties struct {
	Nickname   string `yaml:"nickname,omitempty"`
	DefaultFor string `yaml:"defaultFor,omitempty"`
}

// AgencConfig represents the contents of config.yml.
type AgencConfig struct {
	AgentTemplates map[string]AgentTemplateProperties `yaml:"agentTemplates"`
	SyncedRepos    []string                           `yaml:"syncedRepos,omitempty"`
}

// GetConfigFilepath returns the path to config.yml inside the config directory.
func GetConfigFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ConfigDirname, ConfigFilename)
}

// ReadAgencConfig reads and parses config.yml. Returns an empty config if the
// file does not exist. Returns an error if any agentTemplates key is not in
// canonical format (github.com/owner/repo), if a defaultFor value is
// unrecognized, or if two templates share the same defaultFor value.
// The returned yaml.CommentMap captures any YAML comments for round-trip
// preservation; callers that only read config may discard it with _.
func ReadAgencConfig(agencDirpath string) (*AgencConfig, yaml.CommentMap, error) {
	configFilepath := GetConfigFilepath(agencDirpath)

	data, err := os.ReadFile(configFilepath)
	if err != nil {
		if os.IsNotExist(err) {
			return &AgencConfig{
				AgentTemplates: make(map[string]AgentTemplateProperties),
			}, nil, nil
		}
		return nil, nil, stacktrace.Propagate(err, "failed to read config file '%s'", configFilepath)
	}

	var cfg AgencConfig
	cm := yaml.CommentMap{}
	if err := yaml.UnmarshalWithOptions(data, &cfg, yaml.CommentToMap(cm)); err != nil {
		return nil, nil, stacktrace.Propagate(err, "failed to parse config file '%s'", configFilepath)
	}

	if cfg.AgentTemplates == nil {
		cfg.AgentTemplates = make(map[string]AgentTemplateProperties)
	}

	for repo := range cfg.AgentTemplates {
		if !canonicalRepoRegex.MatchString(repo) {
			return nil, nil, stacktrace.NewError(
				"invalid agentTemplates key '%s' in %s; must be in canonical format 'github.com/owner/repo'",
				repo, configFilepath,
			)
		}
	}

	for _, repo := range cfg.SyncedRepos {
		if !canonicalRepoRegex.MatchString(repo) {
			return nil, nil, stacktrace.NewError(
				"invalid syncedRepos entry '%s' in %s; must be in canonical format 'github.com/owner/repo'",
				repo, configFilepath,
			)
		}
	}

	// Validate defaultFor values
	seenDefaultFor := make(map[string]string) // defaultFor value -> repo that claimed it
	for repo, props := range cfg.AgentTemplates {
		if props.DefaultFor == "" {
			continue
		}
		if !IsValidDefaultForValue(props.DefaultFor) {
			return nil, nil, stacktrace.NewError(
				"invalid defaultFor value '%s' on template '%s' in %s; must be one of: %s",
				props.DefaultFor, repo, configFilepath, FormatDefaultForValues(),
			)
		}
		if otherRepo, exists := seenDefaultFor[props.DefaultFor]; exists {
			return nil, nil, stacktrace.NewError(
				"duplicate defaultFor value '%s' in %s: claimed by both '%s' and '%s'",
				props.DefaultFor, configFilepath, otherRepo, repo,
			)
		}
		seenDefaultFor[props.DefaultFor] = repo
	}

	return &cfg, cm, nil
}

// WriteAgencConfig marshals and writes config.yml. Pass the yaml.CommentMap
// returned by ReadAgencConfig to preserve YAML comments through round-trips;
// pass nil if no comments need preserving.
func WriteAgencConfig(agencDirpath string, cfg *AgencConfig, cm yaml.CommentMap) error {
	configFilepath := GetConfigFilepath(agencDirpath)

	var (
		data []byte
		err  error
	)
	if cm != nil {
		data, err = yaml.MarshalWithOptions(cfg, yaml.WithComment(cm))
	} else {
		data, err = yaml.Marshal(cfg)
	}
	if err != nil {
		return stacktrace.Propagate(err, "failed to marshal config")
	}

	if err := os.WriteFile(configFilepath, data, 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write config file '%s'", configFilepath)
	}

	return nil
}

// FindDefaultTemplate returns the repo key of the template whose DefaultFor
// matches the given context value (e.g. "emptyMission", "repo", "agentTemplate").
// Returns an empty string if no template claims that context.
func FindDefaultTemplate(templates map[string]AgentTemplateProperties, context string) string {
	for repo, props := range templates {
		if props.DefaultFor == context {
			return repo
		}
	}
	return ""
}

// EnsureConfigFile creates config.yml with a default configuration if it does
// not already exist. The default configuration includes the AgenC Engineer
// template pre-installed as the default for editing agent templates.
func EnsureConfigFile(agencDirpath string) error {
	configFilepath := GetConfigFilepath(agencDirpath)

	if _, err := os.Stat(configFilepath); err == nil {
		return nil
	}

	seed := `agentTemplates:
  github.com/mieubrisse/agenc-agent-template_agenc-engineer:
    nickname: "🤖 AgenC Engineer"
    defaultFor: agentTemplate
`
	if err := os.WriteFile(configFilepath, []byte(seed), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to create config file '%s'", configFilepath)
	}

	return nil
}

// IsValidDefaultForValue returns true if the given value is a recognized defaultFor value.
func IsValidDefaultForValue(value string) bool {
	for _, v := range ValidDefaultForValues {
		if v == value {
			return true
		}
	}
	return false
}

// FormatDefaultForValues returns a human-readable comma-separated list of valid defaultFor values.
func FormatDefaultForValues() string {
	return strings.Join(ValidDefaultForValues, ", ")
}
