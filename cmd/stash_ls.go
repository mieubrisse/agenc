package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/tableprinter"
)

var stashLsCmd = &cobra.Command{
	Use:   lsCmdStr,
	Short: "List saved stashes",
	RunE:  runStashLs,
}

func init() {
	stashCmd.AddCommand(stashLsCmd)
}

func runStashLs(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	stashes, err := client.ListStashes()
	if err != nil {
		return stacktrace.Propagate(err, "failed to list stashes")
	}

	if len(stashes) == 0 {
		fmt.Println("No stashed workspaces.")
		return nil
	}

	tbl := tableprinter.NewTable("CREATED", "ID", "MISSIONS")
	for _, s := range stashes {
		tbl.AddRow(
			s.CreatedAt.Local().Format("2006-01-02 15:04:05"),
			s.StashID,
			fmt.Sprintf("%d", s.MissionCount),
		)
	}
	tbl.Print()
	return nil
}
