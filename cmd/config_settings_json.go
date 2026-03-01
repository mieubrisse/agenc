package cmd

import (
	"github.com/spf13/cobra"
)

var configSettingsJsonCmd = &cobra.Command{
	Use:   settingsJsonCmdStr,
	Short: "Manage AgenC-specific settings.json overrides",
	Long: `Read and write the AgenC-specific settings.json that gets merged into every mission's config.

This file contains settings overrides that apply to all AgenC missions but not
to Claude Code sessions outside of AgenC. Settings are deep-merged over the
user's ~/.claude/settings.json when building per-mission config (objects merge
recursively, arrays are concatenated, scalars from this file win).

Changes take effect for new missions automatically. Use 'agenc mission reconfig'
to propagate changes to existing missions.`,
}

func init() {
	configCmd.AddCommand(configSettingsJsonCmd)
}
