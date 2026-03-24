package cmd

import (
	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

// anyFlagChanged returns true if at least one of the given flag names was
// explicitly set on the command line.
func anyFlagChanged(cmd *cobra.Command, flagNames []string) bool {
	for _, name := range flagNames {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

// applyStringFlag reads a string flag if it was changed and calls the apply
// function with its value. If the flag was not changed, it is a no-op.
func applyStringFlag(cmd *cobra.Command, flagName string, apply func(string) error) error {
	if !cmd.Flags().Changed(flagName) {
		return nil
	}
	value, err := cmd.Flags().GetString(flagName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read --%s flag", flagName)
	}
	return apply(value)
}

// applyBoolFlag reads a bool flag if it was changed and calls the apply
// function with its value. If the flag was not changed, it is a no-op.
func applyBoolFlag(cmd *cobra.Command, flagName string, apply func(bool) error) error {
	if !cmd.Flags().Changed(flagName) {
		return nil
	}
	value, err := cmd.Flags().GetBool(flagName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read --%s flag", flagName)
	}
	return apply(value)
}
