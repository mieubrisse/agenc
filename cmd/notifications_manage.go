package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/server"
	"github.com/odyssey/agenc/internal/tableprinter"
)

const (
	notificationsManageMissionMissingPlaceholder = "—"
	notificationsManagePromptText                = "Notification Center > "
	notificationsManageHeaderText                = "ENTER attach │ ESC cancel"
	notificationsManagePreviewWindow             = "right:60%:wrap"
	// missionIDColorANSI tints the mission column cyan so rows actionable by
	// ENTER pop out from rows with no linked mission.
	missionIDColorANSI = "\x1b[36m"
	missionIDResetANSI = "\x1b[0m"
)

var notificationsManageCmd = &cobra.Command{
	Use:   manageCmdStr,
	Short: "Interactive notification picker — ENTER attaches to the linked mission",
	Long: `Open the Notification Center: an fzf picker over all notifications,
sorted by recency, with a preview pane for the body. Press ENTER on a row to
attach to its linked mission. Notifications without a linked mission are not
actionable from this view.`,
	Args: cobra.NoArgs,
	RunE: runNotificationsManage,
}

func init() {
	notificationsCmd.AddCommand(notificationsManageCmd)
}

func runNotificationsManage(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	notifs, err := client.ListNotifications(false, "", "")
	if err != nil {
		return stacktrace.Propagate(err, "failed to list notifications")
	}
	if len(notifs) == 0 {
		fmt.Println("No notifications. Schedule a cron with `agenc cron new` and the next run will appear here.")
		// Wait for a keypress so the message is readable inside a
		// `tmux display-popup -E` (which closes when the command exits).
		// In non-TTY contexts (e2e tests) ReadByte returns immediately on EOF
		// so the function still exits cleanly.
		if isatty.IsTerminal(os.Stdin.Fd()) {
			fmt.Println()
			fmt.Print("Press any key to close...")
		}
		_, _ = bufio.NewReader(os.Stdin).ReadByte()
		return nil
	}

	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return stacktrace.NewError("notifications manage requires an interactive terminal")
	}
	fzfBinary, err := exec.LookPath("fzf")
	if err != nil {
		return stacktrace.NewError("'fzf' binary not found in PATH; install fzf to use the Notification Center")
	}

	fzfInput := buildNotificationsManageFzfInput(notifs)

	execPath, err := os.Executable()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get agenc executable path")
	}

	previewCmd := fmt.Sprintf("%s notifications show {1}", execPath)
	fzfArgs := []string{
		"--ansi",
		"--header-lines", "1",
		"--with-nth", "2..",
		"--header", notificationsManageHeaderText,
		"--prompt", notificationsManagePromptText,
		"--preview", previewCmd,
		"--preview-window", notificationsManagePreviewWindow,
	}

	fzfCmd := exec.Command(fzfBinary, fzfArgs...)
	fzfCmd.Stdin = strings.NewReader(fzfInput)
	fzfCmd.Stderr = os.Stderr

	output, err := fzfCmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// fzf exits 130 on Ctrl-C/Esc, 1 on no match — both are clean cancel
			if exitErr.ExitCode() == 1 || exitErr.ExitCode() == 130 {
				return nil
			}
		}
		return stacktrace.Propagate(err, "fzf selection failed")
	}

	selected := strings.TrimSpace(string(output))
	if selected == "" {
		return nil
	}
	shortID, _, _ := strings.Cut(selected, "\t")
	shortID = strings.TrimSpace(shortID)

	notif, err := client.GetNotification(shortID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to fetch notification '%v'", shortID)
	}
	if notif.MissionID == "" {
		fmt.Println("Notification has no linked mission.")
		return nil
	}

	attachCmd := exec.Command(execPath, "mission", "attach", notif.MissionID)
	attachCmd.Stdin = os.Stdin
	attachCmd.Stdout = os.Stdout
	attachCmd.Stderr = os.Stderr
	if err := attachCmd.Run(); err != nil {
		return stacktrace.Propagate(err, "failed to attach to mission '%v'", notif.MissionID)
	}
	return nil
}

// buildNotificationsManageFzfInput renders the notifications as a tableprinter
// table, then prepends a hidden notification short-ID column (tab-separated)
// to each row so fzf's `{1}` placeholder resolves to the short ID for both
// preview and selection.
func buildNotificationsManageFzfInput(notifs []server.NotificationResponse) string {
	var buf bytes.Buffer
	tbl := tableprinter.NewTable("WHEN", "KIND", "MISSION", "TITLE").WithWriter(&buf)
	shortIDs := make([]string, 0, len(notifs))
	for _, n := range notifs {
		shortIDs = append(shortIDs, database.ShortID(n.ID))
		tbl.AddRow(formatNotificationWhen(n.CreatedAt), n.Kind, formatMissionCell(n.MissionID), n.Title)
	}
	tbl.Print()

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}

	var out strings.Builder
	// Header row gets a placeholder index that won't appear in the result.
	out.WriteString("HEADER\t")
	out.WriteString(lines[0])
	out.WriteByte('\n')
	for i, line := range lines[1:] {
		out.WriteString(shortIDs[i])
		out.WriteByte('\t')
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

// formatMissionCell renders the MISSION column. Empty IDs become a placeholder
// so the user can tell at a glance which rows are ENTER-actionable; populated
// IDs are colored to make them pop.
func formatMissionCell(missionID string) string {
	if missionID == "" {
		return notificationsManageMissionMissingPlaceholder
	}
	return missionIDColorANSI + database.ShortID(missionID) + missionIDResetANSI
}
