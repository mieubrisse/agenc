package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/tableprinter"
)

var sessionLsMissionFlag string

var sessionLsCmd = &cobra.Command{
	Use:   lsCmdStr,
	Short: "List sessions",
	RunE:  runSessionLs,
}

func init() {
	sessionLsCmd.Flags().StringVar(&sessionLsMissionFlag, "mission", "", "filter by mission ID or short ID")
	sessionCmd.AddCommand(sessionLsCmd)
}

func runSessionLs(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	var sessions []*database.Session
	if sessionLsMissionFlag != "" {
		sessions, err = client.ListMissionSessions(sessionLsMissionFlag)
	} else {
		sessions, err = client.ListSessions()
	}
	if err != nil {
		return stacktrace.Propagate(err, "failed to list sessions")
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions.")
		return nil
	}

	tbl := tableprinter.NewTable("UPDATED", "MISSION", "SESSION", "TITLE", "SUMMARY")
	for _, s := range sessions {
		title := resolveSessionTitle(s)
		summary := truncatePrompt(s.AutoSummary, 60)

		tbl.AddRow(
			s.UpdatedAt.Local().Format("2006-01-02 15:04"),
			database.ShortID(s.MissionID),
			database.ShortID(s.ID),
			truncatePrompt(title, 40),
			summary,
		)
	}
	tbl.Print()
	return nil
}

// resolveSessionTitle returns the best available title for a session,
// preferring custom_title over agenc_custom_title.
func resolveSessionTitle(s *database.Session) string {
	if s.CustomTitle != "" {
		return s.CustomTitle
	}
	return s.AgencCustomTitle
}
