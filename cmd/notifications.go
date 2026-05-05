package cmd

import "github.com/spf13/cobra"

var notificationsCmd = &cobra.Command{
	Use:   notificationsCmdStr,
	Short: "List, read, and create AgenC notifications",
	Long: `Notifications surface events that need user awareness — most commonly,
sync conflicts in writeable copies, but extensible to anything an agent or
subsystem wants to flag. Notifications are append-only: they are created once
and either remain unread or get marked as read. They are never deleted.`,
}

func init() {
	rootCmd.AddCommand(notificationsCmd)
}
