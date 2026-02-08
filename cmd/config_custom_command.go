package cmd

import (
	"github.com/spf13/cobra"
)

var configCustomCommandCmd = &cobra.Command{
	Use:   customCommandCmdStr,
	Short: "Manage custom palette commands",
	Long: `Manage custom palette commands defined in config.yml.

Custom commands appear in the tmux command palette and execute agenc
subcommands with pre-configured arguments.

Example config.yml:

  customCommands:
    dotfiles:
      args: "mission new github.com/mieubrisse/dotfiles"
      paletteName: "Open dotfiles"
`,
}

func init() {
	configCmd.AddCommand(configCustomCommandCmd)
}
