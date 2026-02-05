package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/adhocore/gronx"
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

// CronOverlapPolicy defines how the scheduler handles overlapping cron runs.
type CronOverlapPolicy string

const (
	// CronOverlapSkip skips the new run if the previous one is still running (default).
	CronOverlapSkip CronOverlapPolicy = "skip"
	// CronOverlapAllow allows concurrent runs of the same cron job.
	CronOverlapAllow CronOverlapPolicy = "allow"
)

// DefaultCronsMaxConcurrent is the default maximum number of concurrent headless missions.
const DefaultCronsMaxConcurrent = 10

// DefaultCronTimeout is the default timeout for cron-spawned missions.
const DefaultCronTimeout = 1 * time.Hour

// CronConfig represents the configuration for a single cron job.
type CronConfig struct {
	Schedule    string            `yaml:"schedule"`              // Cron expression (5 or 6 fields)
	Agent       string            `yaml:"agent,omitempty"`       // Agent template (canonical repo name)
	Prompt      string            `yaml:"prompt"`                // Initial prompt for the mission
	Description string            `yaml:"description,omitempty"` // Human-readable description
	Git         string            `yaml:"git,omitempty"`         // Git repo to clone into workspace
	Timeout     string            `yaml:"timeout,omitempty"`     // Max runtime (e.g., "1h", "30m")
	Overlap     CronOverlapPolicy `yaml:"overlap,omitempty"`     // "skip" (default) or "allow"
	Enabled     *bool             `yaml:"enabled,omitempty"`     // Defaults to true if omitted
}

// IsEnabled returns whether the cron job is enabled. Defaults to true if not explicitly set.
func (c *CronConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// GetTimeout returns the parsed timeout duration, or the default if not set or invalid.
func (c *CronConfig) GetTimeout() time.Duration {
	if c.Timeout == "" {
		return DefaultCronTimeout
	}
	d, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return DefaultCronTimeout
	}
	return d
}

// GetOverlapPolicy returns the overlap policy, defaulting to skip.
func (c *CronConfig) GetOverlapPolicy() CronOverlapPolicy {
	if c.Overlap == "" || c.Overlap == CronOverlapSkip {
		return CronOverlapSkip
	}
	return c.Overlap
}

// AgencConfig represents the contents of config.yml.
type AgencConfig struct {
	AgentTemplates     map[string]AgentTemplateProperties `yaml:"agentTemplates"`
	SyncedRepos        []string                           `yaml:"syncedRepos,omitempty"`
	Crons              map[string]CronConfig              `yaml:"crons,omitempty"`
	CronsMaxConcurrent int                                `yaml:"cronsMaxConcurrent,omitempty"`
}

// GetCronsMaxConcurrent returns the max concurrent cron missions, using the default if not set.
func (c *AgencConfig) GetCronsMaxConcurrent() int {
	if c.CronsMaxConcurrent <= 0 {
		return DefaultCronsMaxConcurrent
	}
	return c.CronsMaxConcurrent
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

	// Validate cron configurations
	if cfg.Crons == nil {
		cfg.Crons = make(map[string]CronConfig)
	}
	for name, cronCfg := range cfg.Crons {
		if err := ValidateCronName(name); err != nil {
			return nil, nil, stacktrace.Propagate(err, "invalid cron name in %s", configFilepath)
		}
		if err := ValidateCronSchedule(cronCfg.Schedule); err != nil {
			return nil, nil, stacktrace.Propagate(err, "invalid schedule for cron '%s' in %s", name, configFilepath)
		}
		if cronCfg.Prompt == "" {
			return nil, nil, stacktrace.NewError("cron '%s' in %s must have a prompt", name, configFilepath)
		}
		if cronCfg.Agent != "" && !canonicalRepoRegex.MatchString(cronCfg.Agent) {
			return nil, nil, stacktrace.NewError(
				"invalid agent '%s' for cron '%s' in %s; must be in canonical format 'github.com/owner/repo'",
				cronCfg.Agent, name, configFilepath,
			)
		}
		if cronCfg.Git != "" && !canonicalRepoRegex.MatchString(cronCfg.Git) {
			return nil, nil, stacktrace.NewError(
				"invalid git repo '%s' for cron '%s' in %s; must be in canonical format 'github.com/owner/repo'",
				cronCfg.Git, name, configFilepath,
			)
		}
		if err := ValidateCronOverlapPolicy(cronCfg.Overlap); err != nil {
			return nil, nil, stacktrace.Propagate(err, "invalid overlap policy for cron '%s' in %s", name, configFilepath)
		}
		if err := ValidateCronTimeout(cronCfg.Timeout); err != nil {
			return nil, nil, stacktrace.Propagate(err, "invalid timeout for cron '%s' in %s", name, configFilepath)
		}
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

// cronNameRegex matches valid cron names: alphanumeric, hyphens, underscores.
var cronNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)

// ValidateCronName checks whether a cron name is valid.
// Cron names must start with a letter and contain only letters, numbers, hyphens, and underscores.
func ValidateCronName(name string) error {
	if name == "" {
		return stacktrace.NewError("cron name cannot be empty")
	}
	if len(name) > 64 {
		return stacktrace.NewError("cron name too long (max 64 characters)")
	}
	if !cronNameRegex.MatchString(name) {
		return stacktrace.NewError("cron name '%s' is invalid; must start with a letter and contain only letters, numbers, hyphens, and underscores", name)
	}
	return nil
}

// ValidateCronSchedule checks whether a cron schedule expression is valid.
// Supports standard 5-field cron expressions and 6-field expressions with seconds.
func ValidateCronSchedule(schedule string) error {
	if schedule == "" {
		return stacktrace.NewError("cron schedule cannot be empty")
	}
	gron := gronx.New()
	if !gron.IsValid(schedule) {
		return stacktrace.NewError("invalid cron schedule '%s'; use standard cron syntax (e.g., '0 9 * * *' for 9am daily)", schedule)
	}
	return nil
}

// ValidateCronOverlapPolicy checks whether an overlap policy is valid.
func ValidateCronOverlapPolicy(policy CronOverlapPolicy) error {
	if policy == "" || policy == CronOverlapSkip || policy == CronOverlapAllow {
		return nil
	}
	return stacktrace.NewError("invalid overlap policy '%s'; must be 'skip' or 'allow'", policy)
}

// ValidateCronTimeout checks whether a timeout string is valid.
func ValidateCronTimeout(timeout string) error {
	if timeout == "" {
		return nil
	}
	_, err := time.ParseDuration(timeout)
	if err != nil {
		return stacktrace.NewError("invalid timeout '%s'; use Go duration format (e.g., '1h', '30m', '2h30m')", timeout)
	}
	return nil
}

// GetNextCronRun returns the next scheduled run time for a cron expression.
func GetNextCronRun(schedule string) (time.Time, error) {
	return gronx.NextTick(schedule, false)
}

// IsCronDue checks if a cron schedule is due at the given time.
func IsCronDue(schedule string, t time.Time) bool {
	gron := gronx.New()
	due, err := gron.IsDue(schedule, t)
	if err != nil {
		return false
	}
	return due
}
