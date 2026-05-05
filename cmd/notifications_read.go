package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
)

var notificationsReadCmd = &cobra.Command{
	Use:   readCmdStr + " <id>",
	Short: "Mark a notification as read",
	Args:  cobra.ExactArgs(1),
	RunE:  runNotificationsRead,
}

func init() {
	notificationsCmd.AddCommand(notificationsReadCmd)
}

func runNotificationsRead(cmd *cobra.Command, args []string) error {
	id := args[0]

	client, err := serverClient()
	if err != nil {
		return err
	}

	existing, err := client.GetNotification(id)
	if err != nil {
		return stacktrace.Propagate(err, "failed to fetch notification '%v'", id)
	}
	shortID := database.ShortID(existing.ID)
	if existing.ReadAt != "" {
		fmt.Printf("Notification '%s' was already marked as read at %s.\n", shortID, existing.ReadAt)
		return nil
	}

	if err := client.MarkNotificationRead(existing.ID); err != nil {
		return stacktrace.Propagate(err, "failed to mark notification '%v' as read", id)
	}
	fmt.Printf("Marked notification '%s' as read.\n", shortID)
	return nil
}
