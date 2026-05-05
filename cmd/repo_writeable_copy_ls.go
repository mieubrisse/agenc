package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/tableprinter"
)

var repoWriteableCopyLsCmd = &cobra.Command{
	Use:   lsCmdStr,
	Short: "List configured writeable copies and their sync status",
	RunE:  runRepoWriteableCopyLs,
}

func init() {
	repoWriteableCopyCmd.AddCommand(repoWriteableCopyLsCmd)
}

func runRepoWriteableCopyLs(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	copies, err := client.ListWriteableCopies()
	if err != nil {
		return stacktrace.Propagate(err, "failed to list writeable copies")
	}

	if len(copies) == 0 {
		fmt.Println("No writeable copies configured.")
		fmt.Println()
		fmt.Println("Configure one with:")
		fmt.Println("  agenc repo writeable-copy set <repo> <absolute-path>")
		return nil
	}

	tbl := tableprinter.NewTable("REPO", "PATH", "STATUS", "DETAIL")
	pausedCount := 0
	for _, c := range copies {
		statusDisplay := "✅ ok"
		detail := "--"
		switch c.Status {
		case "paused":
			statusDisplay = "⚠ paused"
			detail = c.PausedReason
			pausedCount++
		case "missing":
			statusDisplay = "❌ missing"
			detail = "path does not exist"
		}
		tbl.AddRow(c.RepoName, c.Path, statusDisplay, detail)
	}
	tbl.Print()

	if pausedCount > 0 {
		fmt.Println()
		fmt.Printf("%d writeable copy needs attention. See:\n", pausedCount)
		fmt.Println("  agenc notifications ls")
	}

	return nil
}
