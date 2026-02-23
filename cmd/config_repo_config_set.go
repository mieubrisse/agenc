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
  agenc config repoConfig set github.com/owner/repo --window-title="my-repo"
  agenc config repoConfig set github.com/owner/repo --always-synced=true --window-title="my-repo"
`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigRepoConfigSet,
}

func init() {
	configRepoConfigCmd.AddCommand(configRepoConfigSetCmd)
	configRepoConfigSetCmd.Flags().Bool(repoConfigAlwaysSyncedFlagName, false, "keep this repo continuously synced by the daemon")
	configRepoConfigSetCmd.Flags().String(repoConfigWindowTitleFlagName, "", "custom tmux window title for missions using this repo")
	configRepoConfigSetCmd.Flags().String(repoConfigTrustedMcpServersFlagName, "", `MCP server trust: "all", comma-separated server names, or "" to clear`)
	configRepoConfigSetCmd.Flags().String(repoConfigDefaultModelFlagName, "", `default Claude model for missions using this repo (e.g., "opus", "sonnet")`)
}

func runConfigRepoConfigSet(cmd *cobra.Command, args []string) error {
	repoName := args[0]

	if !config.IsCanonicalRepoName(repoName) {
		return stacktrace.NewError("repo must be in canonical format 'github.com/owner/repo'; got '%s'", repoName)
	}

	alwaysSyncedChanged := cmd.Flags().Changed(repoConfigAlwaysSyncedFlagName)
	windowTitleChanged := cmd.Flags().Changed(repoConfigWindowTitleFlagName)
	trustedChanged := cmd.Flags().Changed(repoConfigTrustedMcpServersFlagName)
	defaultModelChanged := cmd.Flags().Changed(repoConfigDefaultModelFlagName)

	if !alwaysSyncedChanged && !windowTitleChanged && !trustedChanged && !defaultModelChanged {
		return stacktrace.NewError("at least one of --%s, --%s, --%s, or --%s must be provided",
			repoConfigAlwaysSyncedFlagName, repoConfigWindowTitleFlagName, repoConfigTrustedMcpServersFlagName, repoConfigDefaultModelFlagName)
	}

	cfg, cm, err := readConfigWithComments()
	if err != nil {
		return err
	}

	rc, _ := cfg.GetRepoConfig(repoName)

	if alwaysSyncedChanged {
		synced, err := cmd.Flags().GetBool(repoConfigAlwaysSyncedFlagName)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read --%s flag", repoConfigAlwaysSyncedFlagName)
		}
		rc.AlwaysSynced = synced
	}

	if windowTitleChanged {
		title, err := cmd.Flags().GetString(repoConfigWindowTitleFlagName)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read --%s flag", repoConfigWindowTitleFlagName)
		}
		rc.WindowTitle = title
	}

	if trustedChanged {
		raw, err := cmd.Flags().GetString(repoConfigTrustedMcpServersFlagName)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read --%s flag", repoConfigTrustedMcpServersFlagName)
		}
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
	}

	if defaultModelChanged {
		model, err := cmd.Flags().GetString(repoConfigDefaultModelFlagName)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read --%s flag", repoConfigDefaultModelFlagName)
		}
		rc.DefaultModel = model
	}

	cfg.SetRepoConfig(repoName, rc)

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("Updated repo config for '%s'\n", repoName)
	return nil
}
