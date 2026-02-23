package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:        daemonCmdStr,
	Short:      "Manage the background daemon (deprecated: use 'server' instead)",
	Deprecated: "use 'agenc server' instead. The daemon has been replaced by the server.",
}

func init() {
	rootCmd.AddCommand(daemonCmd)
}

func printDaemonDeprecation() {
	fmt.Fprintln(os.Stderr, "Warning: 'agenc daemon' is deprecated. Use 'agenc server' instead.")
}
