package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var configRepoConfigCmd = &cobra.Command{
	Use:   repoConfigCmdStr,
	Short: "Manage per-repo configuration",
	Long: fmt.Sprintf(`Manage per-repo configuration in config.yml.

Each repo is identified by its canonical name (github.com/owner/repo) and
supports these optional settings:

  alwaysSynced       - server keeps the repo continuously fetched (every 60s)
  emoji              - emoji to display for missions using this repo
  title              - friendly title for the repo (e.g., "Dotfiles")
  description        - human/agent-readable description of what the repo is for
  trustedMcpServers  - pre-approve MCP servers to skip the consent prompt
  postUpdateHook     - shell command to run after the repo updates (e.g., "make setup")
  claudeArgs         - Claude CLI flags for missions using this repo, after the
                       global claudeArgs; a default model goes here
  writeableCopy      - absolute path to a writeable working copy the server syncs

%v

  repoConfig:
    github.com/owner/repo:
      alwaysSynced: true
      emoji: "🔥"
      title: "AgenC"
      description: "The AgenC orchestration system"
      trustedMcpServers: all
      postUpdateHook: "make setup"
      claudeArgs:
        - "--model"
        - "opus"
        - "--chrome"
    github.com/owner/other:
      alwaysSynced: true
      writeableCopy: /Users/me/app/other
`, configYAMLExampleMarker),
}

func init() {
	configCmd.AddCommand(configRepoConfigCmd)
}
