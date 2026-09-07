package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/session"
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

	if sessionLsMissionFlag != "" {
		return printMissionSessions(sessions, session.FindSessionJSONLPath, os.Stdout, os.Stderr)
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

// printMissionSessions lists one mission's sessions with what their transcripts
// hold, so a reader can tell which session to open before opening any: when it
// started, how much conversation it carries, whether it was compacted, which
// session it was forked from, how many subagents it spawned and its size.
// The unfiltered listing keeps its cheap shape: reading every session's
// transcript is affordable for one mission, not for a whole machine.
//
// lookup resolves a session ID to its JSONL path; a session whose transcript
// cannot be found is still listed, with dashes, and the reason goes to stderr.
func printMissionSessions(sessions []*database.Session, lookup func(string) (string, error), stdout io.Writer, stderr io.Writer) error {
	tbl := tableprinter.NewTable("UPDATED", "SESSION", "STARTED", "MSGS", "TOOLS", "ERR", "COMPACT", "AGENTS", "FORKED-FROM", "SIZE", "TITLE").WithWriter(stdout)
	for _, s := range sessions {
		row := []string{"-", "-", "-", "-", "-", "-", "-", "-"}
		if jsonlFilepath, err := lookup(s.ID); err != nil {
			fmt.Fprintf(stderr, "warning: session %s: %v\n", database.ShortID(s.ID), err)
		} else if facts, err := describeSessionTranscript(jsonlFilepath); err != nil {
			fmt.Fprintf(stderr, "warning: session %s: %v\n", database.ShortID(s.ID), err)
		} else {
			row = facts.cells()
		}
		tbl.AddRow(append(append([]interface{}{s.UpdatedAt.Local().Format("2006-01-02 15:04"), database.ShortID(s.ID)}, toCells(row)...), truncatePrompt(resolveSessionTitle(s), 40))...)
	}
	tbl.Print()
	return nil
}

func toCells(values []string) []interface{} {
	out := make([]interface{}, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
