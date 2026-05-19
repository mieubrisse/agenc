package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
)

// notificationsToggleReadCmd flips the read state of a notification — used by
// the Notification Center picker's Ctrl-R bind so the same keystroke marks
// unread rows as read and re-marks read rows as unread. Hidden from the
// public CLI because the user-facing entry points are `notification read`
// (one-way mark) and, eventually, an `unread` sibling if one is needed.
var notificationsToggleReadCmd = &cobra.Command{
	Use:    "toggle-read <id>",
	Short:  "Toggle the read state of a notification (internal)",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE:   runNotificationsToggleRead,
}

func init() {
	notificationsCmd.AddCommand(notificationsToggleReadCmd)
}

func runNotificationsToggleRead(cmd *cobra.Command, args []string) error {
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

	if existing.ReadAt == "" {
		if err := client.MarkNotificationRead(existing.ID); err != nil {
			return stacktrace.Propagate(err, "failed to mark notification '%v' as read", id)
		}
		fmt.Printf("Marked notification '%s' as read.\n", shortID)
		return nil
	}

	if err := client.MarkNotificationUnread(existing.ID); err != nil {
		return stacktrace.Propagate(err, "failed to mark notification '%v' as unread", id)
	}
	fmt.Printf("Marked notification '%s' as unread.\n", shortID)
	return nil
}
