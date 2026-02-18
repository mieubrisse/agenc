package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/tableprinter"
)

var configRepoConfigLsCmd = &cobra.Command{
	Use:   lsCmdStr,
	Short: "List per-repo configuration",
	Long:  `List all repos that have configuration entries in config.yml.`,
	RunE:  runConfigRepoConfigLs,
}

func init() {
	configRepoConfigCmd.AddCommand(configRepoConfigLsCmd)
}

func runConfigRepoConfigLs(cmd *cobra.Command, args []string) error {
	cfg, err := readConfig()
	if err != nil {
		return err
	}

	if len(cfg.RepoConfigs) == 0 {
		fmt.Println("No per-repo configuration.")
		return nil
	}

	// Sort repo names for stable output
	repoNames := make([]string, 0, len(cfg.RepoConfigs))
	for name := range cfg.RepoConfigs {
		repoNames = append(repoNames, name)
	}
	sort.Strings(repoNames)

	tbl := tableprinter.NewTable("REPO", "ALWAYS SYNCED", "WINDOW TITLE", "TRUSTED MCP SERVERS")
	for _, name := range repoNames {
		rc := cfg.RepoConfigs[name]
		synced := formatCheckmark(rc.AlwaysSynced)
		windowTitle := rc.WindowTitle
		if windowTitle == "" {
			windowTitle = "--"
		}
		tbl.AddRow(displayGitRepo(name), synced, windowTitle, formatTrustedMcpServers(rc.TrustedMcpServers))
	}
	tbl.Print()

	return nil
}

func formatTrustedMcpServers(t *config.TrustedMcpServers) string {
	if t == nil {
		return "--"
	}
	if t.All {
		return "all"
	}
	return strings.Join(t.List, ", ")
}
