package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/adhocore/gronx"
	"github.com/goccy/go-yaml"
	"github.com/mieubrisse/stacktrace"
)

// canonicalRepoRegex matches the canonical repo format: github.com/owner/repo
var canonicalRepoRegex = regexp.MustCompile(`^github\.com/[^/]+/[^/]+$`)

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

// PaletteCommandConfig represents a palette command entry in config.yml.
// For custom commands, all fields are user-provided.
// For builtin overrides, non-empty fields override the builtin defaults.
// An entry with all fields empty (IsEmpty() == true) disables the builtin.
type PaletteCommandConfig struct {
	Title          string `yaml:"title,omitempty"`
	Description    string `yaml:"description,omitempty"`
	Command        string `yaml:"command,omitempty"`
	TmuxKeybinding string `yaml:"tmuxKeybinding,omitempty"`
}

// IsEmpty returns true if all fields are empty (used to detect disable entries).
func (c *PaletteCommandConfig) IsEmpty() bool {
	return c.Title == "" && c.Description == "" && c.Command == "" && c.TmuxKeybinding == ""
}

// BuiltinPaletteCommands defines the default palette commands shipped with agenc.
// Keys match the config.yml paletteCommands keys for override/disable purposes.
var BuiltinPaletteCommands = map[string]PaletteCommandConfig{
	"do": {
		Title:          "✅ Do",
		Description:    "tell AgenC what it should do",
		Command:        "agenc tmux window new -- agenc do",
		TmuxKeybinding: "d",
	},
	"quickClaude": {
		Title:       "🦀 Quick Claude",
		Description: "Launch a blank mission instantly",
		Command:     "agenc tmux window new -- agenc mission new --blank",
	},
	"talkToAgenc": {
		Title:       "🤖 Talk to AgenC",
		Description: "Launch an AgenC assistant mission",
		Command:     "agenc tmux window new -- agenc mission new --assistant",
	},
	"startMission": {
		Title:          "🚀 Start mission",
		Description:    "Create a new mission and launch Claude",
		Command:        "agenc tmux window new -- agenc mission new",
		TmuxKeybinding: "n",
	},
	"resumeMission": {
		Title:       "🟢 Resume mission",
		Description: "Reactivate an archived mission",
		Command:     "agenc tmux window new -- agenc mission resume",
	},
	"stopMission": {
		Title:       "🛑 Stop this mission",
		Description: "Stop the mission in the focused pane",
		Command:     "agenc mission stop $AGENC_CALLING_MISSION_UUID",
	},
	"reloadMission": {
		Title:       "🔄 Reload mission",
		Description: "Stop and restart the mission in the focused pane",
		Command:     "agenc mission stop $AGENC_CALLING_MISSION_UUID && agenc tmux window new -- agenc mission resume $AGENC_CALLING_MISSION_UUID",
	},
	"removeMission": {
		Title:       "❌ Remove mission",
		Description: "Remove a mission and its directory",
		Command:     "agenc tmux window new -- agenc mission rm",
	},
	"nukeMissions": {
		Title:       "💥 Nuke missions",
		Description: "Remove all archived missions",
		Command:     "agenc tmux window new -- agenc mission nuke",
	},
}

// builtinPaletteCommandOrder controls the display order of builtin commands
// in the palette and ls output.
var builtinPaletteCommandOrder = []string{
	"do",
	"quickClaude",
	"talkToAgenc",
	"startMission",
	"resumeMission",
	"stopMission",
	"reloadMission",
	"removeMission",
	"nukeMissions",
}

// BuiltinPaletteCommandOrder returns the display order of builtin commands.
func BuiltinPaletteCommandOrder() []string {
	result := make([]string, len(builtinPaletteCommandOrder))
	copy(result, builtinPaletteCommandOrder)
	return result
}

// CallingMissionUUIDEnvVar is the environment variable name that carries the
// focused mission's UUID into palette and keybinding commands.
const CallingMissionUUIDEnvVar = "AGENC_CALLING_MISSION_UUID"

// ResolvedPaletteCommand is a palette command with all defaults applied and
// the agenc binary substituted in the command string.
type ResolvedPaletteCommand struct {
	Name           string
	Title          string
	Description    string
	Command        string
	TmuxKeybinding string
	IsBuiltin      bool
}

// IsMissionScoped returns true if the command references the calling mission
// UUID environment variable, meaning it should only be available when a
// mission pane is focused.
func (c ResolvedPaletteCommand) IsMissionScoped() bool {
	return strings.Contains(c.Command, CallingMissionUUIDEnvVar)
}

// FormatKeybinding returns the human-readable keybinding string for this
// command (e.g. "prefix → a → d"), or an empty string if no keybinding is set.
func (c ResolvedPaletteCommand) FormatKeybinding() string {
	if c.TmuxKeybinding == "" {
		return ""
	}
	return fmt.Sprintf("prefix → a → %s", c.TmuxKeybinding)
}

// DefaultPaletteTmuxKeybinding is the default bind-key arguments for the
// command palette keybinding. The value is inserted verbatim after "bind-key"
// in the generated tmux keybindings file, so it can include table specifiers
// (e.g. "-T agenc k") or bind directly on the prefix table (e.g. "C-k").
const DefaultPaletteTmuxKeybinding = "-T agenc k"

// IsCanonicalRepoName reports whether the given string is in canonical format (github.com/owner/repo).
func IsCanonicalRepoName(name string) bool {
	return canonicalRepoRegex.MatchString(name)
}

// RepoConfig represents per-repo configuration in the repoConfig map.
// Both fields are optional: alwaysSynced controls whether the daemon keeps
// the repo continuously fetched, and windowTitle overrides the tmux window name.
type RepoConfig struct {
	AlwaysSynced bool   `yaml:"alwaysSynced,omitempty"`
	WindowTitle  string `yaml:"windowTitle,omitempty"`
}

// AgencConfig represents the contents of config.yml.
type AgencConfig struct {
	RepoConfigs            map[string]RepoConfig            `yaml:"repoConfig,omitempty"`
	TmuxAgencFilepath      string                           `yaml:"tmuxAgencFilepath,omitempty"`
	Crons                  map[string]CronConfig            `yaml:"crons,omitempty"`
	CronsMaxConcurrent     int                              `yaml:"cronsMaxConcurrent,omitempty"`
	PaletteCommands        map[string]PaletteCommandConfig  `yaml:"paletteCommands,omitempty"`
	PaletteTmuxKeybinding  string                           `yaml:"paletteTmuxKeybinding,omitempty"`
	DoAutoConfirm          bool                             `yaml:"doAutoConfirm,omitempty"`
}

// GetPaletteTmuxKeybinding returns the tmux key for the command palette,
// defaulting to "k" when not configured.
func (c *AgencConfig) GetPaletteTmuxKeybinding() string {
	if c.PaletteTmuxKeybinding == "" {
		return DefaultPaletteTmuxKeybinding
	}
	return c.PaletteTmuxKeybinding
}

// GetTmuxAgencBinary returns the agenc binary name/path used in tmux
// keybindings. Defaults to "agenc" when not configured.
func (c *AgencConfig) GetTmuxAgencBinary() string {
	if c.TmuxAgencFilepath == "" {
		return "agenc"
	}
	return c.TmuxAgencFilepath
}

// GetCronsMaxConcurrent returns the max concurrent cron missions, using the default if not set.
func (c *AgencConfig) GetCronsMaxConcurrent() int {
	if c.CronsMaxConcurrent <= 0 {
		return DefaultCronsMaxConcurrent
	}
	return c.CronsMaxConcurrent
}

// GetAllSyncedRepos returns the sorted list of repo names that have alwaysSynced enabled.
func (c *AgencConfig) GetAllSyncedRepos() []string {
	var repos []string
	for repoName, rc := range c.RepoConfigs {
		if rc.AlwaysSynced {
			repos = append(repos, repoName)
		}
	}
	sort.Strings(repos)
	return repos
}

// GetWindowTitle returns the configured window title for a repo, or empty
// string if no custom title is set.
func (c *AgencConfig) GetWindowTitle(repoName string) string {
	if rc, ok := c.RepoConfigs[repoName]; ok {
		return rc.WindowTitle
	}
	return ""
}

// GetRepoConfig returns the config for a repo and whether it exists.
func (c *AgencConfig) GetRepoConfig(repoName string) (RepoConfig, bool) {
	rc, ok := c.RepoConfigs[repoName]
	return rc, ok
}

// SetRepoConfig sets or updates the config for a repo.
func (c *AgencConfig) SetRepoConfig(repoName string, rc RepoConfig) {
	if c.RepoConfigs == nil {
		c.RepoConfigs = make(map[string]RepoConfig)
	}
	c.RepoConfigs[repoName] = rc
}

// RemoveRepoConfig removes the config entry for a repo.
// Returns true if the entry existed and was removed.
func (c *AgencConfig) RemoveRepoConfig(repoName string) bool {
	if _, ok := c.RepoConfigs[repoName]; !ok {
		return false
	}
	delete(c.RepoConfigs, repoName)
	return true
}

// IsAlwaysSynced reports whether the given repo has alwaysSynced enabled.
func (c *AgencConfig) IsAlwaysSynced(repoName string) bool {
	if rc, ok := c.RepoConfigs[repoName]; ok {
		return rc.AlwaysSynced
	}
	return false
}

// SetAlwaysSynced sets the alwaysSynced flag for a repo, creating the entry if needed.
func (c *AgencConfig) SetAlwaysSynced(repoName string, synced bool) {
	if c.RepoConfigs == nil {
		c.RepoConfigs = make(map[string]RepoConfig)
	}
	rc := c.RepoConfigs[repoName]
	rc.AlwaysSynced = synced
	c.RepoConfigs[repoName] = rc
}

// GetConfigFilepath returns the path to config.yml inside the config directory.
func GetConfigFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ConfigDirname, ConfigFilename)
}

// ReadAgencConfig reads and parses config.yml. Returns an empty config if the
// file does not exist. Returns an error if any repoConfig key is not in
// canonical format (github.com/owner/repo).
// The returned yaml.CommentMap captures any YAML comments for round-trip
// preservation; callers that only read config may discard it with _.
func ReadAgencConfig(agencDirpath string) (*AgencConfig, yaml.CommentMap, error) {
	configFilepath := GetConfigFilepath(agencDirpath)

	data, err := os.ReadFile(configFilepath)
	if err != nil {
		if os.IsNotExist(err) {
			return &AgencConfig{}, nil, nil
		}
		return nil, nil, stacktrace.Propagate(err, "failed to read config file '%s'", configFilepath)
	}

	var cfg AgencConfig
	cm := yaml.CommentMap{}
	if err := yaml.UnmarshalWithOptions(data, &cfg, yaml.CommentToMap(cm)); err != nil {
		return nil, nil, stacktrace.Propagate(err, "failed to parse config file '%s'", configFilepath)
	}

	if cfg.RepoConfigs == nil {
		cfg.RepoConfigs = make(map[string]RepoConfig)
	}
	for repoName := range cfg.RepoConfigs {
		if !canonicalRepoRegex.MatchString(repoName) {
			return nil, nil, stacktrace.NewError(
				"invalid repoConfig key '%s' in %s; must be in canonical format 'github.com/owner/repo'",
				repoName, configFilepath,
			)
		}
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

	// Validate palette commands
	if cfg.PaletteCommands == nil {
		cfg.PaletteCommands = make(map[string]PaletteCommandConfig)
	}
	for name, cmdCfg := range cfg.PaletteCommands {
		if err := ValidatePaletteCommandName(name); err != nil {
			return nil, nil, stacktrace.Propagate(err, "invalid palette command name in %s", configFilepath)
		}
		// Custom (non-builtin) entries with content must have title and command
		_, isBuiltin := BuiltinPaletteCommands[name]
		if !isBuiltin && !cmdCfg.IsEmpty() {
			if cmdCfg.Title == "" {
				return nil, nil, stacktrace.NewError("palette command '%s' in %s must have a title", name, configFilepath)
			}
			if cmdCfg.Command == "" {
				return nil, nil, stacktrace.NewError("palette command '%s' in %s must have a command", name, configFilepath)
			}
		}
	}

	// Validate uniqueness of titles and keybindings across the resolved set
	if err := validatePaletteUniqueness(&cfg, configFilepath); err != nil {
		return nil, nil, err
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

// EnsureConfigFile creates config.yml with a minimal empty configuration if it
// does not already exist.
func EnsureConfigFile(agencDirpath string) error {
	configFilepath := GetConfigFilepath(agencDirpath)

	if _, err := os.Stat(configFilepath); err == nil {
		return nil
	}

	if err := os.WriteFile(configFilepath, []byte("{}\n"), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to create config file '%s'", configFilepath)
	}

	return nil
}

// cronNameRegex matches valid cron names: alphanumeric, hyphens, underscores.
var cronNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)

// ValidatePaletteCommandName checks whether a palette command name is valid.
// Names follow the same rules as cron names: start with a letter,
// contain only letters, numbers, hyphens, and underscores, max 64 characters.
func ValidatePaletteCommandName(name string) error {
	if name == "" {
		return stacktrace.NewError("palette command name cannot be empty")
	}
	if len(name) > 64 {
		return stacktrace.NewError("palette command name too long (max 64 characters)")
	}
	if !cronNameRegex.MatchString(name) {
		return stacktrace.NewError("palette command name '%s' is invalid; must start with a letter and contain only letters, numbers, hyphens, and underscores", name)
	}
	return nil
}

// IsBuiltinPaletteCommand returns true if the name is a builtin palette command.
func IsBuiltinPaletteCommand(name string) bool {
	_, ok := BuiltinPaletteCommands[name]
	return ok
}

// GetResolvedPaletteCommands returns the fully merged list of palette commands:
// builtins (with user overrides applied) followed by custom entries (alphabetical).
// Commands with "agenc" in their command string have it replaced with the
// configured tmux agenc binary. Disabled builtins (empty override) are excluded.
func (c *AgencConfig) GetResolvedPaletteCommands() []ResolvedPaletteCommand {
	agencBinary := c.GetTmuxAgencBinary()
	var result []ResolvedPaletteCommand

	// Process builtins in defined order
	for _, name := range builtinPaletteCommandOrder {
		builtin := BuiltinPaletteCommands[name]
		override, hasOverride := c.PaletteCommands[name]

		if hasOverride && override.IsEmpty() {
			// Disabled — skip entirely
			continue
		}

		resolved := ResolvedPaletteCommand{
			Name:           name,
			Title:          builtin.Title,
			Description:    builtin.Description,
			Command:        builtin.Command,
			TmuxKeybinding: builtin.TmuxKeybinding,
			IsBuiltin:      true,
		}

		// Apply overrides — non-empty fields replace defaults
		if hasOverride {
			if override.Title != "" {
				resolved.Title = override.Title
			}
			if override.Description != "" {
				resolved.Description = override.Description
			}
			if override.Command != "" {
				resolved.Command = override.Command
			}
			if override.TmuxKeybinding != "" {
				resolved.TmuxKeybinding = override.TmuxKeybinding
			}
		}

		// Substitute agenc binary in command
		resolved.Command = substituteAgencBinary(resolved.Command, agencBinary)

		result = append(result, resolved)
	}

	// Process custom entries (non-builtin keys) in alphabetical order
	customNames := make([]string, 0)
	for name := range c.PaletteCommands {
		if _, isBuiltin := BuiltinPaletteCommands[name]; !isBuiltin {
			customNames = append(customNames, name)
		}
	}
	sort.Strings(customNames)

	for _, name := range customNames {
		cmdCfg := c.PaletteCommands[name]
		if cmdCfg.IsEmpty() {
			continue
		}
		resolved := ResolvedPaletteCommand{
			Name:           name,
			Title:          cmdCfg.Title,
			Description:    cmdCfg.Description,
			Command:        substituteAgencBinary(cmdCfg.Command, agencBinary),
			TmuxKeybinding: cmdCfg.TmuxKeybinding,
			IsBuiltin:      false,
		}
		result = append(result, resolved)
	}

	return result
}

// substituteAgencBinary replaces standalone "agenc" tokens in a command string
// with the configured binary path. Only space-delimited tokens that are exactly
// "agenc" are replaced — substrings like "mieubrisse/agenc" are left alone.
func substituteAgencBinary(command, agencBinary string) string {
	if agencBinary == "agenc" {
		return command
	}
	parts := strings.Split(command, " ")
	for i, part := range parts {
		if part == "agenc" {
			parts[i] = agencBinary
		}
	}
	return strings.Join(parts, " ")
}

// validatePaletteUniqueness checks that titles and keybindings are unique across
// the resolved palette command set, and that the palette keybinding doesn't
// conflict with any command keybinding.
func validatePaletteUniqueness(cfg *AgencConfig, configFilepath string) error {
	resolved := cfg.GetResolvedPaletteCommands()
	paletteKey := cfg.GetPaletteTmuxKeybinding()

	seenTitles := make(map[string]string)    // title → command name
	seenKeybindings := make(map[string]string) // keybinding → command name

	for _, cmd := range resolved {
		if cmd.Title != "" {
			if existingName, ok := seenTitles[cmd.Title]; ok {
				return stacktrace.NewError(
					"duplicate palette title '%s' in %s: used by both '%s' and '%s'",
					cmd.Title, configFilepath, existingName, cmd.Name,
				)
			}
			seenTitles[cmd.Title] = cmd.Name
		}

		if cmd.TmuxKeybinding != "" {
			if existingName, ok := seenKeybindings[cmd.TmuxKeybinding]; ok {
				return stacktrace.NewError(
					"duplicate palette keybinding '%s' in %s: used by both '%s' and '%s'",
					cmd.TmuxKeybinding, configFilepath, existingName, cmd.Name,
				)
			}
			seenKeybindings[cmd.TmuxKeybinding] = cmd.Name
		}
	}

	// Check that the palette keybinding doesn't conflict with any command keybinding.
	// Command keybindings are bare keys within the agenc table, so a conflict is
	// only possible when the palette is also bound in the agenc table.
	if paletteAgencKey := extractAgencTableKey(paletteKey); paletteAgencKey != "" {
		if conflictName, ok := seenKeybindings[paletteAgencKey]; ok {
			return stacktrace.NewError(
				"palette keybinding '%s' conflicts with command '%s' in %s; set paletteTmuxKeybinding to a different key",
				paletteKey, conflictName, configFilepath,
			)
		}
	}

	return nil
}

// extractAgencTableKey returns the bare key from a palette keybinding string
// if it targets the agenc key table (e.g. "-T agenc k" → "k"). Returns ""
// if the keybinding is not in the agenc table.
func extractAgencTableKey(paletteKeybinding string) string {
	const prefix = "-T agenc "
	if strings.HasPrefix(paletteKeybinding, prefix) {
		return strings.TrimPrefix(paletteKeybinding, prefix)
	}
	return ""
}

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
