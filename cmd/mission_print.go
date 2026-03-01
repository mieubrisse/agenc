package cmd

import (
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/session"
)

var missionPrintTailFlag int
var missionPrintAllFlag bool
var missionPrintFormatFlag string

var missionPrintCmd = &cobra.Command{
	Use:   printCmdStr + " [mission-id]",
	Short: "Print a mission's current session transcript (human-readable text by default)",
	Long: `Print a mission's current session transcript.

By default, outputs a human-readable text summary. Use --format=jsonl for
raw JSONL output.

Without arguments, opens an interactive fzf picker to select a mission.
With arguments, accepts a mission ID (short 8-char hex or full UUID).

Outputs the last 20 lines by default. Use --tail to change the line count,
or --all to print the entire session.

Example:
  agenc mission print
  agenc mission print 2571d5d8
  agenc mission print 2571d5d8 --format=jsonl
  agenc mission print 2571d5d8 --tail 50
  agenc mission print 2571d5d8 --all`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionPrint,
}

func init() {
	missionPrintCmd.Flags().IntVar(&missionPrintTailFlag, tailFlagName, defaultTailLines, "number of lines to print from end of session")
	missionPrintCmd.Flags().BoolVar(&missionPrintAllFlag, allFlagName, false, "print entire session")
	missionPrintCmd.Flags().StringVar(&missionPrintFormatFlag, formatFlagName, "text", "output format: text or jsonl")
	missionCmd.AddCommand(missionPrintCmd)
}

func runMissionPrint(cmd *cobra.Command, args []string) error {
	if !missionPrintAllFlag && missionPrintTailFlag <= 0 {
		return stacktrace.NewError("--tail value must be positive")
	}

	client, err := serverClient()
	if err != nil {
		return err
	}

	missions, err := client.ListMissions(false, "")
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		return stacktrace.NewError("no missions found")
	}

	entries := buildMissionPickerEntries(missions, defaultPromptMaxLen)

	input := strings.Join(args, " ")
	result, err := Resolve(input, Resolver[missionPickerEntry]{
		TryCanonical: func(input string) (missionPickerEntry, bool, error) {
			if !looksLikeMissionID(input) {
				return missionPickerEntry{}, false, nil
			}
			missionID, err := client.ResolveMissionID(input)
			if err != nil {
				return missionPickerEntry{}, false, stacktrace.Propagate(err, "failed to resolve mission ID")
			}
			for _, e := range entries {
				if e.MissionID == missionID {
					return e, true, nil
				}
			}
			return missionPickerEntry{}, false, stacktrace.NewError("mission %s not found", input)
		},
		GetItems: func() ([]missionPickerEntry, error) { return entries, nil },
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Status, e.Session, e.Repo}
		},
		FzfPrompt:         "Select mission to print session: ",
		FzfHeaders:        []string{"LAST ACTIVE", "ID", "STATUS", "SESSION", "REPO"},
		MultiSelect:       false,
		NotCanonicalError: "not a valid mission ID",
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	missionID := result.Items[0].MissionID

	// Resolve mission's current session ID
	sessionID := claudeconfig.GetLastSessionID(agencDirpath, missionID)
	if sessionID == "" {
		return stacktrace.NewError("no current session found for mission %s", missionID)
	}

	// Find and print the session JSONL
	jsonlFilepath, err := session.FindSessionJSONLPath(sessionID)
	if err != nil {
		return stacktrace.Propagate(err, "")
	}

	return printSession(jsonlFilepath, missionPrintTailFlag, missionPrintAllFlag, missionPrintFormatFlag)
}
