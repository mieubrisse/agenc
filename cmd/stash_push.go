package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/tableprinter"
)

var stashPushForceFlag bool

var stashPushCmd = &cobra.Command{
	Use:   pushCmdStr,
	Short: "Snapshot running missions and stop them",
	Long: `Snapshot all running missions — recording which tmux sessions they are
linked into — then stop them. The snapshot is saved to a stash file that
can be restored later with 'agenc stash pop'.

If any missions are actively busy (not idle), you will be warned before
proceeding. Use --force to skip the warning.`,
	RunE: runStashPush,
}

func init() {
	stashPushCmd.Flags().BoolVar(&stashPushForceFlag, forceFlagName, false, "skip warning for non-idle missions")
	stashCmd.AddCommand(stashPushCmd)
}

func runStashPush(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	// First attempt (respects --force flag)
	pushResp, conflict, err := client.PushStash(stashPushForceFlag)
	if err != nil {
		return stacktrace.Propagate(err, "failed to push stash")
	}

	// Handle non-idle warning
	if conflict != nil {
		fmt.Println("The following missions are not idle:")
		fmt.Println()

		tbl := tableprinter.NewTable("ID", "STATE", "SESSION")
		for _, m := range conflict.NonIdleMissions {
			tbl.AddRow(m.ShortID, m.ClaudeState, truncatePrompt(m.SessionName, 60))
		}
		tbl.Print()

		fmt.Println()
		fmt.Print("Stashing will stop these missions. Continue? [y/N] ")

		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}

		// Retry with force
		pushResp, _, err = client.PushStash(true)
		if err != nil {
			return stacktrace.Propagate(err, "failed to push stash")
		}
	}

	if pushResp.MissionsStashed == 0 {
		fmt.Println("No running missions to stash.")
	} else {
		fmt.Printf("Stashed %d mission(s). Restore with '%s %s %s'.\n",
			pushResp.MissionsStashed, agencCmdStr, stashCmdStr, popCmdStr)
	}
	return nil
}
