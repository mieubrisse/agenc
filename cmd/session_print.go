package cmd

import (
	"os"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/session"
)

const defaultTailLines = 20

var sessionPrintTailFlag int
var sessionPrintAllFlag bool
var sessionPrintFormatFlag string

var sessionPrintCmd = &cobra.Command{
	Use:   printCmdStr + " <session-id>",
	Short: "Print a Claude session transcript (human-readable text by default)",
	Long: `Print a Claude session transcript.

Accepts a full session UUID or an 8-character short ID.

By default, outputs a human-readable text summary. Use --format=jsonl for
raw JSONL output.

Outputs the last 20 lines by default. Use --tail to change the line count,
or --all to print the entire session.

Example:
  agenc session print 18749fb5
  agenc session print 18749fb5-02ba-4b19-b989-4e18fbf8ea92
  agenc session print 18749fb5 --format=jsonl
  agenc session print 18749fb5 --tail 50
  agenc session print 18749fb5 --all`,
	Args: cobra.ExactArgs(1),
	RunE: runSessionPrint,
}

func init() {
	sessionPrintCmd.Flags().IntVar(&sessionPrintTailFlag, tailFlagName, defaultTailLines, "number of lines to print from end of session")
	sessionPrintCmd.Flags().BoolVar(&sessionPrintAllFlag, allFlagName, false, "print entire session")
	sessionPrintCmd.Flags().StringVar(&sessionPrintFormatFlag, formatFlagName, "text", "output format: text or jsonl")
	sessionCmd.AddCommand(sessionPrintCmd)
}

func runSessionPrint(cmd *cobra.Command, args []string) error {
	sessionID := args[0]

	if !sessionPrintAllFlag && sessionPrintTailFlag <= 0 {
		return stacktrace.NewError("--tail value must be positive")
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

	return printSession(jsonlFilepath, sessionPrintTailFlag, sessionPrintAllFlag, sessionPrintFormatFlag)
}

// printSession is the shared printing logic used by both
// session print and mission print commands.
func printSession(jsonlFilepath string, tailLines int, all bool, format string) error {
	n := tailLines
	if all {
		n = 0
	}

	switch format {
	case "text":
		if err := session.FormatConversation(jsonlFilepath, n, os.Stdout); err != nil {
			return stacktrace.Propagate(err, "")
		}
		return nil
	case "jsonl":
		_, err := session.TailJSONLFile(jsonlFilepath, n, os.Stdout)
		if err != nil {
			return stacktrace.Propagate(err, "")
		}
		return nil
	default:
		return stacktrace.NewError("invalid format %q: must be %q or %q", format, "text", "jsonl")
	}
}
