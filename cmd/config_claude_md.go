package cmd

import (
	"github.com/spf13/cobra"
)

var configClaudeMdCmd = &cobra.Command{
	Use:   claudeMdCmdStr,
	Short: "Manage AgenC-specific CLAUDE.md instructions",
	Long: `Read and write the AgenC-specific CLAUDE.md that gets merged into every mission's config.

This file contains instructions that apply to all AgenC missions but not to
Claude Code sessions outside of AgenC. Content is appended after the user's
~/.claude/CLAUDE.md when building per-mission config.

Changes propagate to existing missions automatically — running missions pick them up on their next reload.`,
}

func init() {
	configCmd.AddCommand(configClaudeMdCmd)
}
