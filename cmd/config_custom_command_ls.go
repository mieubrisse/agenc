package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/tableprinter"
)

var configCustomCommandLsCmd = &cobra.Command{
	Use:   lsCmdStr,
	Short: "List custom palette commands",
	RunE:  runConfigCustomCommandLs,
}

func init() {
	configCustomCommandCmd.AddCommand(configCustomCommandLsCmd)
}

func runConfigCustomCommandLs(cmd *cobra.Command, args []string) error {
	cfg, err := readConfig()
	if err != nil {
		return err
	}

	if len(cfg.CustomCommands) == 0 {
		fmt.Println("No custom commands defined.")
		fmt.Printf("\nTo add one: %s %s %s %s <name> --%s=\"...\" --%s=\"...\"\n",
			agencCmdStr, configCmdStr, customCommandCmdStr, addCmdStr,
			customCommandArgsFlagName, customCommandPaletteNameFlagName)
		return nil
	}

	// Sort by name for stable output
	names := make([]string, 0, len(cfg.CustomCommands))
	for name := range cfg.CustomCommands {
		names = append(names, name)
	}
	sort.Strings(names)

	tbl := tableprinter.NewTable("NAME", "PALETTE NAME", "ARGS")
	for _, name := range names {
		cmdCfg := cfg.CustomCommands[name]
		tbl.AddRow(name, cmdCfg.PaletteName, cmdCfg.Args)
	}

	tbl.Print()
	return nil
}
