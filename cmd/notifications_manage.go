package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sort"
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
	notificationsManageHeaderText                = "ENTER attach │ Ctrl-R toggle read │ ESC cancel"
	notificationsManagePreviewWindow             = "right:50%:wrap"
	// notificationsManageUnreadMarker is shown in the READ column for any
	// notification whose ReadAt is empty. Read notifications get a blank cell.
	notificationsManageUnreadMarker = "🔔"
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

	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return stacktrace.NewError("notification manage requires an interactive terminal")
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

	previewCmd := fmt.Sprintf("%s notification show {1}", execPath)
	toggleReadCmd := fmt.Sprintf("%s notification toggle-read {1}", execPath)
	reloadCmd := fmt.Sprintf("%s notification manage-fzf-input", execPath)
	fzfArgs := []string{
		"--ansi",
		"--header-lines", "1",
		// Tab delimiter keeps the tableprinter-rendered row (field 2) intact
		// as a single field — otherwise fzf's default whitespace tokenizer
		// would collapse empty leading cells and re-join with single spaces,
		// destroying column alignment.
		"--delimiter", "\t",
		"--with-nth", "2",
		"--header", notificationsManageHeaderText,
		"--prompt", notificationsManagePromptText,
		"--preview", previewCmd,
		"--preview-window", notificationsManagePreviewWindow,
		"--bind", fmt.Sprintf("ctrl-r:execute-silent(%s)+reload(%s)", toggleReadCmd, reloadCmd),
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

	// Mark read before attach: once we hand control to tmux the user won't
	// return here, so deferring would mean the row stays unread until detach.
	// Non-fatal — failing to mark read shouldn't block the attach the user
	// actually pressed ENTER for.
	if err := client.MarkNotificationRead(notif.ID); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark notification '%v' read: %v\n", shortID, err)
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
// preview and selection. Rows are sorted so unread notifications appear first
// (stable within each group); the leading READ column shows an unread marker
// on unread rows so the user can scan them at a glance.
func buildNotificationsManageFzfInput(notifs []server.NotificationResponse) string {
	sorted := make([]server.NotificationResponse, len(notifs))
	copy(sorted, notifs)
	sort.SliceStable(sorted, func(i, j int) bool {
		iUnread := sorted[i].ReadAt == ""
		jUnread := sorted[j].ReadAt == ""
		if iUnread != jUnread {
			return iUnread
		}
		return false
	})

	var buf bytes.Buffer
	tbl := tableprinter.NewTable("READ", "WHEN", "MISSION", "TITLE").WithWriter(&buf)
	shortIDs := make([]string, 0, len(sorted))
	for _, n := range sorted {
		shortIDs = append(shortIDs, database.ShortID(n.ID))
		tbl.AddRow(formatReadCell(n.ReadAt), formatNotificationWhen(n.CreatedAt), formatMissionCell(n.MissionID), n.Title)
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

// formatReadCell renders the READ column. Empty ReadAt means unread — show
// the marker; otherwise the cell is blank so read rows visually recede.
func formatReadCell(readAt string) string {
	if readAt == "" {
		return notificationsManageUnreadMarker
	}
	return ""
}
