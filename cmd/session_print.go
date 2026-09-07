package cmd

import (
	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/session"
)

const defaultTailLines = 20

var sessionPrintOpts transcriptPrintOptions

var sessionPrintCmd = &cobra.Command{
	Use:   printCmdStr + " <session-id>",
	Short: "Print a Claude session transcript (human-readable text by default)",
	Long: `Print a Claude session transcript.

Accepts a full session UUID or an 8-character short ID.

By default, outputs a human-readable text summary. Use --format=jsonl for
raw JSONL output.

Outputs the last 20 lines by default. Use --tail to change the line count,
or --all to print the entire session.

A session is not a single transcript: every subagent it spawns writes its own,
and those subagents spawn subagents. --agents lists that tree, --agent prints
one subagent's transcript, and --expand-agents inlines them all at their spawn
sites. --verbose additionally renders hooks, turn timings, attachments,
thinking blocks and successful tool results.

Example:
  agenc session print 18749fb5
  agenc session print 18749fb5-02ba-4b19-b989-4e18fbf8ea92
  agenc session print 18749fb5 --format=jsonl
  agenc session print 18749fb5 --tail 50
  agenc session print 18749fb5 --all
  agenc session print 18749fb5 --agents
  agenc session print 18749fb5 --agent aa8d6202
  agenc session print 18749fb5 --all --expand-agents`,
	Args: cobra.ExactArgs(1),
	RunE: runSessionPrint,
}

func init() {
	sessionPrintCmd.Flags().IntVar(&sessionPrintOpts.tailLines, tailFlagName, defaultTailLines, "number of lines to print from end of session")
	sessionPrintCmd.Flags().BoolVar(&sessionPrintOpts.all, allFlagName, false, "print entire session")
	sessionPrintCmd.Flags().StringVar(&sessionPrintOpts.format, formatFlagName, textFormat, "output format: text or jsonl")
	registerTranscriptPrintFlags(sessionPrintCmd, &sessionPrintOpts)
	sessionCmd.AddCommand(sessionPrintCmd)
}

func runSessionPrint(cmd *cobra.Command, args []string) error {
	sessionID := args[0]

	if !sessionPrintOpts.all && sessionPrintOpts.tailLines <= 0 {
		return stacktrace.NewError("--%s value must be positive", tailFlagName)
	}

	// Validate before touching the server so an unusable flag combination is
	// reported as itself, rather than as whatever the session lookup happens
	// to fail with first.
	if err := sessionPrintOpts.validate(); err != nil {
		return err
	}

	// Resolve short IDs to full UUIDs via the server
	client, err := serverClient()
	if err != nil {
		return err
	}
	resolvedID, err := client.ResolveSessionID(sessionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve session ID '%s'", sessionID)
	}

	jsonlFilepath, err := session.FindSessionJSONLPath(resolvedID)
	if err != nil {
		return stacktrace.Propagate(err, "")
	}

	return printTranscript(jsonlFilepath, sessionPrintOpts)
}
