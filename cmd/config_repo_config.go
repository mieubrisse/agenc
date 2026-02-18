package cmd

import (
	"github.com/spf13/cobra"
)

var configRepoConfigCmd = &cobra.Command{
	Use:   repoConfigCmdStr,
	Short: "Manage per-repo configuration",
	Long: `Manage per-repo configuration in config.yml.

Each repo is identified by its canonical name (github.com/owner/repo) and
supports three optional settings:

  alwaysSynced       - daemon keeps the repo continuously fetched (every 60s)
  windowTitle        - custom tmux window name for missions using this repo
  trustedMcpServers  - pre-approve MCP servers to skip the consent prompt

Example config.yml:

  repoConfig:
    github.com/owner/repo:
      alwaysSynced: true
      windowTitle: "my-repo"
      trustedMcpServers: all
    github.com/owner/other:
      alwaysSynced: true
`,
}

func init() {
	configCmd.AddCommand(configRepoConfigCmd)
}
