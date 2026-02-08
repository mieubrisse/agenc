package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var configCustomCommandRmCmd = &cobra.Command{
	Use:   rmCmdStr + " <name>",
	Short: "Remove a custom palette command",
	Args:  cobra.ExactArgs(1),
	RunE:  runConfigCustomCommandRm,
}

func init() {
	configCustomCommandCmd.AddCommand(configCustomCommandRmCmd)
}

func runConfigCustomCommandRm(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, cm, err := readConfigWithComments()
	if err != nil {
		return err
	}

	if _, exists := cfg.CustomCommands[name]; !exists {
		return stacktrace.NewError("custom command '%s' not found", name)
	}

	delete(cfg.CustomCommands, name)

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("Removed custom command '%s'\n", name)
	return nil
}
