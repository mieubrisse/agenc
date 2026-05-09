package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/mattn/go-isatty"
	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/server"
)

var notificationsCreateCmd = &cobra.Command{
	Use:   createCmdStr,
	Short: "Create a new notification (typically for agents)",
	Long: `Create a new notification.

Body content can be supplied either via --body=<string> for short content or
via --body-file=<path> for longer content. Use --body-file=- to read the body
from stdin (handy for piping):

  cat conflict-report.md | agenc notifications create \
      --kind=writeable_copy.conflict --title="Rebase conflict" --body-file=-`,
	RunE: runNotificationsCreate,
}

func init() {
	notificationsCmd.AddCommand(notificationsCreateCmd)
	notificationsCreateCmd.Flags().String(notificationsKindFlagName, "", "kind tag (required, e.g. writeable_copy.conflict)")
	notificationsCreateCmd.Flags().String(notificationsTitleFlagName, "", "one-line title (required)")
	notificationsCreateCmd.Flags().String(notificationsSourceRepoFlagName, "", "associated repo in canonical format (optional)")
	notificationsCreateCmd.Flags().String(notificationsMissionIDFlagName, "", "link this notification to a mission (UUID or short ID); ENTER on the notification in 'manage' attaches to it")
	notificationsCreateCmd.Flags().String(notificationsBodyFlagName, "", "body content (mutually exclusive with --body-file)")
	notificationsCreateCmd.Flags().String(notificationsBodyFileFlagName, "", "path to body content file; use - for stdin")
}

func runNotificationsCreate(cmd *cobra.Command, args []string) error {
	kind, _ := cmd.Flags().GetString(notificationsKindFlagName)
	title, _ := cmd.Flags().GetString(notificationsTitleFlagName)
	sourceRepo, _ := cmd.Flags().GetString(notificationsSourceRepoFlagName)
	missionIDFlag, _ := cmd.Flags().GetString(notificationsMissionIDFlagName)
	bodyFlag, _ := cmd.Flags().GetString(notificationsBodyFlagName)
	bodyFile, _ := cmd.Flags().GetString(notificationsBodyFileFlagName)

	if kind == "" {
		return stacktrace.NewError("--%s is required", notificationsKindFlagName)
	}
	if title == "" {
		return stacktrace.NewError("--%s is required", notificationsTitleFlagName)
	}
	if bodyFlag != "" && bodyFile != "" {
		return stacktrace.NewError("--%s and --%s are mutually exclusive", notificationsBodyFlagName, notificationsBodyFileFlagName)
	}

	body := bodyFlag
	if bodyFile != "" {
		raw, err := readBodyFile(bodyFile)
		if err != nil {
			return err
		}
		body = raw
	}

	client, err := serverClient()
	if err != nil {
		return err
	}

	resolvedMissionID := ""
	if missionIDFlag != "" {
		resolvedMissionID, err = client.ResolveMissionID(missionIDFlag)
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve mission '%v'", missionIDFlag)
		}
	}

	created, err := client.CreateNotification(server.CreateNotificationRequest{
		Kind:         kind,
		SourceRepo:   sourceRepo,
		MissionID:    resolvedMissionID,
		Title:        title,
		BodyMarkdown: body,
	})
	if err != nil {
		return stacktrace.Propagate(err, "failed to create notification")
	}

	fmt.Printf("Created notification '%s'.\n", database.ShortID(created.ID))
	return nil
}

// readBodyFile reads body content from the given path. The special path "-"
// reads from stdin, but only when stdin is piped — connecting to an interactive
// TTY would hang the agent.
func readBodyFile(path string) (string, error) {
	if path == "-" {
		if isatty.IsTerminal(os.Stdin.Fd()) {
			return "", stacktrace.NewError("no input on stdin — pipe content or use --%s", notificationsBodyFlagName)
		}
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", stacktrace.Propagate(err, "failed to read body from stdin")
		}
		return string(data), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to read body file '%v'", path)
	}
	return string(data), nil
}
