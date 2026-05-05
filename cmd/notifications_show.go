package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/server"
)

var notificationsShowCmd = &cobra.Command{
	Use:   showCmdStr + " <id>",
	Short: "Print the full body of a notification",
	Long: `Print the full Markdown body of a notification to stdout.

The body is sanitized of ANSI escape sequences before display so that a
malicious or malformed body cannot manipulate the terminal.`,
	Args: cobra.ExactArgs(1),
	RunE: runNotificationsShow,
}

func init() {
	notificationsCmd.AddCommand(notificationsShowCmd)
}

func runNotificationsShow(cmd *cobra.Command, args []string) error {
	id := args[0]

	client, err := serverClient()
	if err != nil {
		return err
	}

	n, err := client.GetNotification(id)
	if err != nil {
		return stacktrace.Propagate(err, "failed to fetch notification '%v'", id)
	}

	fmt.Print(server.StripANSI(n.BodyMarkdown))
	if len(n.BodyMarkdown) > 0 && n.BodyMarkdown[len(n.BodyMarkdown)-1] != '\n' {
		fmt.Println()
	}
	return nil
}
