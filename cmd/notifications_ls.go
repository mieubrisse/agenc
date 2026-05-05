package cmd

import (
	"fmt"
	"time"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/tableprinter"
)

var notificationsLsCmd = &cobra.Command{
	Use:   lsCmdStr,
	Short: "List notifications (default: unread only)",
	Long: `List notifications. By default only unread notifications are shown.
Use --all to see read notifications too. Use --repo or --kind to filter.`,
	RunE: runNotificationsLs,
}

func init() {
	notificationsCmd.AddCommand(notificationsLsCmd)
	notificationsLsCmd.Flags().Bool(allFlagName, false, "include read notifications")
	notificationsLsCmd.Flags().String(notificationsRepoFilterFlagName, "", "filter by source repo (canonical name)")
	notificationsLsCmd.Flags().String(notificationsKindFlagName, "", "filter by kind (e.g. writeable_copy.conflict)")
}

func runNotificationsLs(cmd *cobra.Command, args []string) error {
	all, _ := cmd.Flags().GetBool(allFlagName)
	repoFilter, _ := cmd.Flags().GetString(notificationsRepoFilterFlagName)
	kindFilter, _ := cmd.Flags().GetString(notificationsKindFlagName)

	client, err := serverClient()
	if err != nil {
		return err
	}

	list, err := client.ListNotifications(!all, repoFilter, kindFilter)
	if err != nil {
		return stacktrace.Propagate(err, "failed to list notifications")
	}

	if len(list) == 0 {
		if all {
			fmt.Println("No notifications.")
		} else {
			fmt.Println("No unread notifications.")
			fmt.Println()
			fmt.Println("Show all (incl read): agenc notifications ls --all")
		}
		return nil
	}

	tbl := tableprinter.NewTable("ID", "WHEN", "KIND", "SOURCE", "TITLE")
	for _, n := range list {
		when := formatNotificationWhen(n.CreatedAt)
		source := n.SourceRepo
		if source == "" {
			source = "--"
		}
		tbl.AddRow(database.ShortID(n.ID), when, n.Kind, source, n.Title)
	}
	tbl.Print()

	fmt.Println()
	if all {
		fmt.Printf("%d notifications shown.\n", len(list))
	} else {
		fmt.Printf("%d unread notifications.\n", len(list))
	}
	fmt.Println()
	fmt.Println("View full content:    agenc notifications show <id>")
	fmt.Println("Mark as read:         agenc notifications read <id>")
	if !all {
		fmt.Println("Show all (incl read): agenc notifications ls --all")
	}

	return nil
}

// formatNotificationWhen renders an RFC3339 timestamp as a short relative
// description ("4m ago", "2h ago", "3d ago"). On parse failure, returns the
// raw input so the user still sees something useful.
func formatNotificationWhen(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Local().Format("2006-01-02")
	}
}
