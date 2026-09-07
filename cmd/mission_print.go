package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/server"
	"github.com/odyssey/agenc/internal/session"
)

var missionPrintOpts transcriptPrintOptions
var missionPrintSessionFlag string

var missionPrintCmd = &cobra.Command{
	Use:   printCmdStr + " [mission-id]",
	Short: "Print a mission's current session transcript (human-readable text by default)",
	Long: `Print a mission's current session transcript.

By default, outputs the entire session as human-readable text.
Use --format=jsonl for raw JSONL output.
Use --tail to limit output to the last N lines.

A mission accumulates a session per reload, /clear or fork; this prints the most
recently active one. Use --session to print a different one, and
'agenc session ls --mission <id>' to list them.

A session is not a single transcript either: every subagent it spawns writes its
own. --agents lists that tree, --agent prints one subagent's transcript, and
--expand-agents inlines them all at their spawn sites. --verbose additionally
renders hooks, turn timings, attachments, thinking blocks and successful tool
results.

Without arguments, opens an interactive fzf picker to select a mission.
With arguments, accepts a mission ID (short 8-char hex or full UUID).

Example:
  agenc mission print
  agenc mission print 2571d5d8
  agenc mission print 2571d5d8 --format=jsonl
  agenc mission print 2571d5d8 --tail 50
  agenc mission print 2571d5d8 --agents
  agenc mission print 2571d5d8 --agent aa8d6202
  agenc mission print 2571d5d8 --expand-agents
  agenc mission print 2571d5d8 --session 18749fb5`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionPrint,
}

func init() {
	missionPrintCmd.Flags().IntVar(&missionPrintOpts.tailLines, tailFlagName, 0, "limit output to last N lines")
	missionPrintCmd.Flags().StringVar(&missionPrintOpts.format, formatFlagName, textFormat, "output format: text or jsonl")
	missionPrintCmd.Flags().StringVar(&missionPrintSessionFlag, sessionFlagName, "", "print a specific session of the mission, by session ID or short ID")
	registerTranscriptPrintFlags(missionPrintCmd, &missionPrintOpts)
	missionCmd.AddCommand(missionPrintCmd)
}

func runMissionPrint(cmd *cobra.Command, args []string) error {
	if missionPrintOpts.tailLines < 0 {
		return stacktrace.NewError("--%s value must be positive", tailFlagName)
	}
	missionPrintOpts.all = missionPrintOpts.tailLines == 0

	// Validate before touching the server so an unusable flag combination is
	// reported as itself, rather than as whatever the mission lookup happens
	// to fail with first.
	if err := missionPrintOpts.validate(); err != nil {
		return err
	}

	client, err := serverClient()
	if err != nil {
		return err
	}

	missions, err := client.ListMissions(server.ListMissionsRequest{})
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
			return []string{e.ShortID, e.LastPrompt, e.Status, e.Session, e.Repo}
		},
		FzfPrompt:         "Select mission to print session: ",
		FzfHeaders:        []string{"ID", "LAST PROMPT", "STATUS", "SESSION", "REPO"},
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

	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get agenc directory path")
	}
	projectDirpath, err := claudeconfig.GetMissionProjectDirpath(agencDirpath, missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get project directory for mission %s", missionID)
	}

	jsonlFilepath, err := resolveMissionSessionJSONL(client, projectDirpath, missionID, missionPrintSessionFlag)
	if err != nil {
		return err
	}

	warnOnUnprintedSessions(projectDirpath, jsonlFilepath, missionPrintSessionFlag, os.Stderr)

	missionPrintOpts.listCommand = fmt.Sprintf("agenc mission print %s --%s", database.ShortID(missionID), agentsFlagName)
	if missionPrintSessionFlag != "" {
		missionPrintOpts.listCommand += fmt.Sprintf(" --%s %s", sessionFlagName, missionPrintSessionFlag)
	}
	return printTranscript(jsonlFilepath, missionPrintOpts)
}

// sessionIDResolver is the slice of the server client that session selection
// needs: turning a short session ID into a full UUID. Narrowing it to an
// interface keeps the selection logic testable without a live server.
type sessionIDResolver interface {
	ResolveSessionID(id string) (string, error)
}

// resolveMissionSessionJSONL picks which of a mission's session transcripts to
// print. Without --session it takes the most recently modified one.
//
// It deliberately uses the newest JSONL on disk rather than the mission's last
// recorded session ID: a freshly-spawned mission's transcript contains only
// metadata entries and is filtered out of the session listing, and printing it
// with an explanatory empty-session message beats reporting that no session
// exists.
func resolveMissionSessionJSONL(client sessionIDResolver, projectDirpath string, missionID string, sessionFlag string) (string, error) {
	if sessionFlag == "" {
		jsonlFilepath := session.FindActiveJSONLPath(projectDirpath)
		if jsonlFilepath == "" {
			return "", stacktrace.NewError("no current session found for mission %s", missionID)
		}
		return jsonlFilepath, nil
	}

	resolvedID, err := client.ResolveSessionID(sessionFlag)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to resolve --%s '%s'", sessionFlagName, sessionFlag)
	}

	jsonlFilepath := filepath.Join(projectDirpath, resolvedID+".jsonl")
	if _, err := os.Stat(jsonlFilepath); err != nil {
		// The session resolved, but not to a transcript inside this mission.
		// Saying so is more useful than printing another mission's transcript
		// under this mission's ID.
		return "", stacktrace.Propagate(err, "session '%s' has no transcript in mission %s", resolvedID, missionID)
	}
	return jsonlFilepath, nil
}

// warnOnUnprintedSessions notes on stderr that the mission holds sessions other
// than the one being printed. A mission accumulates one per reload, /clear or
// fork, and printing only the newest silently hides the rest.
func warnOnUnprintedSessions(projectDirpath string, printedFilepath string, sessionFlag string, stderr io.Writer) {
	if sessionFlag != "" {
		return
	}
	sessionIDs := session.ListSessionIDs(projectDirpath)
	if len(sessionIDs) <= 1 {
		return
	}
	printedID := strings.TrimSuffix(filepath.Base(printedFilepath), ".jsonl")
	fmt.Fprintf(stderr,
		"(mission has %d sessions; printing %s - --%s <id> to print another, 'agenc session ls --mission' to list them)\n",
		len(sessionIDs), printedID, sessionFlagName)
}
