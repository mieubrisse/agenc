package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var configRepoConfigSetCmd = &cobra.Command{
	Use:   setCmdStr + " <repo>",
	Short: "Set per-repo configuration",
	Long: `Set or update configuration for a repository.

The repo must be specified in canonical format (github.com/owner/repo).
At least one flag must be provided.

Examples:
  agenc config repoConfig set github.com/owner/repo --always-synced=true
  agenc config repoConfig set github.com/owner/repo --emoji="🔥"
  agenc config repoConfig set github.com/owner/repo --always-synced=true --emoji="🔥"
  agenc config repoConfig set github.com/owner/repo --post-update-hook="make setup"
`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigRepoConfigSet,
}

func init() {
	configRepoConfigCmd.AddCommand(configRepoConfigSetCmd)
	configRepoConfigSetCmd.Flags().Bool(repoConfigAlwaysSyncedFlagName, false, "keep this repo continuously synced by the server")
	configRepoConfigSetCmd.Flags().String(repoConfigEmojiFlagName, "", "emoji to display for missions using this repo")
	configRepoConfigSetCmd.Flags().String(repoConfigTitleFlagName, "", `friendly title for the repo (e.g., "Dotfiles")`)
	configRepoConfigSetCmd.Flags().String(repoConfigTrustedMcpServersFlagName, "", `MCP server trust: "all", comma-separated server names, or "" to clear`)
	configRepoConfigSetCmd.Flags().String(repoConfigDefaultModelFlagName, "", `default Claude model for missions using this repo (e.g., "opus", "sonnet")`)
	configRepoConfigSetCmd.Flags().String(repoConfigPostUpdateHookFlagName, "", `shell command to run after repo updates (e.g., "make setup"); empty to clear`)
	configRepoConfigSetCmd.Flags().String(repoConfigClaudeArgsFlagName, "", `extra Claude CLI args: comma-separated (e.g., "--chrome,--verbose"); empty to clear`)
}

// applyAlwaysSyncedFlag enforces the invariant that a repo with a configured
// writeable copy cannot have always-synced disabled — writeable copies require
// continuous sync.
func applyAlwaysSyncedFlag(synced bool, rc *config.RepoConfig, repoName string) error {
	if !synced && rc.WriteableCopy != "" {
		return stacktrace.NewError(
			"cannot disable always-synced for '%s' while a writeable copy is configured at %s; "+
				"writeable copies require continuous sync. Remove the writeable copy first: "+
				"agenc repo writeable-copy unset %s",
			repoName, rc.WriteableCopy, repoName,
		)
	}
	rc.AlwaysSynced = synced
	return nil
}

func runConfigRepoConfigSet(cmd *cobra.Command, args []string) error {
	repoName := args[0]

	if !config.IsCanonicalRepoName(repoName) {
		return stacktrace.NewError("repo must be in canonical format 'github.com/owner/repo'; got '%s'", repoName)
	}

	allFlags := []string{
		repoConfigAlwaysSyncedFlagName, repoConfigEmojiFlagName,
		repoConfigTitleFlagName, repoConfigTrustedMcpServersFlagName,
		repoConfigDefaultModelFlagName, repoConfigPostUpdateHookFlagName,
		repoConfigClaudeArgsFlagName,
	}
	if !anyFlagChanged(cmd, allFlags) {
		return stacktrace.NewError("at least one of --%s, --%s, --%s, --%s, --%s, --%s, or --%s must be provided",
			repoConfigAlwaysSyncedFlagName, repoConfigEmojiFlagName, repoConfigTitleFlagName, repoConfigTrustedMcpServersFlagName, repoConfigDefaultModelFlagName, repoConfigPostUpdateHookFlagName, repoConfigClaudeArgsFlagName)
	}

	cfg, cm, release, err := readConfigWithComments()
	if err != nil {
		return err
	}
	defer release()
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get agenc directory path")
	}

	rc, _ := cfg.GetRepoConfig(repoName)

	if err := applyBoolFlag(cmd, repoConfigAlwaysSyncedFlagName, func(synced bool) error {
		return applyAlwaysSyncedFlag(synced, &rc, repoName)
	}); err != nil {
		return stacktrace.Propagate(err, "failed to apply always-synced flag")
	}

	// Simple string fields that map directly to struct fields
	simpleStringFields := []struct {
		flagName string
		target   *string
	}{
		{repoConfigEmojiFlagName, &rc.Emoji},
		{repoConfigTitleFlagName, &rc.Title},
		{repoConfigDefaultModelFlagName, &rc.DefaultModel},
		{repoConfigPostUpdateHookFlagName, &rc.PostUpdateHook},
	}
	for _, f := range simpleStringFields {
		if err := applyStringFlag(cmd, f.flagName, func(value string) error {
			*f.target = value
			return nil
		}); err != nil {
			return stacktrace.Propagate(err, "failed to apply repo config string flag")
		}
	}

	if err := applyStringFlag(cmd, repoConfigTrustedMcpServersFlagName, func(raw string) error {
		if raw == "" {
			rc.TrustedMcpServers = nil
		} else if raw == "all" {
			rc.TrustedMcpServers = &config.TrustedMcpServers{All: true}
		} else {
			parts := strings.Split(raw, ",")
			servers := make([]string, 0, len(parts))
			for _, p := range parts {
				if s := strings.TrimSpace(p); s != "" {
					servers = append(servers, s)
				}
			}
			if len(servers) == 0 {
				return stacktrace.NewError("--%s: no valid server names found in %q", repoConfigTrustedMcpServersFlagName, raw)
			}
			rc.TrustedMcpServers = &config.TrustedMcpServers{List: servers}
		}
		return nil
	}); err != nil {
		return stacktrace.Propagate(err, "failed to apply trusted MCP servers flag")
	}

	if err := applyStringFlag(cmd, repoConfigClaudeArgsFlagName, func(raw string) error {
		if raw == "" {
			rc.ClaudeArgs = nil
		} else {
			parts := strings.Split(raw, ",")
			args := make([]string, 0, len(parts))
			for _, p := range parts {
				if s := strings.TrimSpace(p); s != "" {
					args = append(args, s)
				}
			}
			if len(args) == 0 {
				return stacktrace.NewError("--%s: no valid args found in %q", repoConfigClaudeArgsFlagName, raw)
			}
			rc.ClaudeArgs = args
		}
		return nil
	}); err != nil {
		return stacktrace.Propagate(err, "failed to apply claude-args flag")
	}

	cfg.SetRepoConfig(repoName, rc)

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("Updated repo config for '%s'\n", repoName)
	return nil
}
