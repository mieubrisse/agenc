package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

// notificationsManageFzfInputCmd is a hidden subcommand used by the
// `notifications manage` picker as its fzf `reload` source. Each invocation
// fetches the full notifications list and prints the same tab-prefixed table
// format used for the picker's initial input, so a reload after marking a
// notification as read re-sorts the picker with the just-read row pushed
// below the remaining unread ones.
var notificationsManageFzfInputCmd = &cobra.Command{
	Use:    "manage-fzf-input",
	Short:  "Print the Notification Center fzf input (internal)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE:   runNotificationsManageFzfInput,
}

func init() {
	notificationsCmd.AddCommand(notificationsManageFzfInputCmd)
}

func runNotificationsManageFzfInput(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	notifs, err := client.ListNotifications(false, "", "")
	if err != nil {
		return stacktrace.Propagate(err, "failed to list notifications")
	}

	fmt.Print(buildNotificationsManageFzfInput(notifs))
	return nil
}
