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

var sessionPrintCmd = &cobra.Command{
	Use:   printCmdStr + " <session-uuid>",
	Short: "Print the JSONL transcript for a Claude session",
	Long: `Print the JSONL transcript for a Claude session.

Outputs the last 20 lines by default. Use --tail to change the line count,
or --all to print the entire session.

Example:
  agenc session print 18749fb5-02ba-4b19-b989-4e18fbf8ea92
  agenc session print 18749fb5-02ba-4b19-b989-4e18fbf8ea92 --tail 50
  agenc session print 18749fb5-02ba-4b19-b989-4e18fbf8ea92 --all`,
	Args: cobra.ExactArgs(1),
	RunE: runSessionPrint,
}

func init() {
	sessionPrintCmd.Flags().IntVar(&sessionPrintTailFlag, tailFlagName, defaultTailLines, "number of lines to print from end of session")
	sessionPrintCmd.Flags().BoolVar(&sessionPrintAllFlag, allFlagName, false, "print entire session")
	sessionCmd.AddCommand(sessionPrintCmd)
}

func runSessionPrint(cmd *cobra.Command, args []string) error {
	sessionID := args[0]

	if !sessionPrintAllFlag && sessionPrintTailFlag <= 0 {
		return stacktrace.NewError("--tail value must be positive")
	}

	jsonlFilepath, err := session.FindSessionJSONLPath(sessionID)
	if err != nil {
		return stacktrace.Propagate(err, "")
	}

	return printSessionJSONL(jsonlFilepath, sessionPrintTailFlag, sessionPrintAllFlag)
}

// printSessionJSONL is the shared JSONL printing logic used by both
// session print and mission print commands.
func printSessionJSONL(jsonlFilepath string, tailLines int, all bool) error {
	n := tailLines
	if all {
		n = 0
	}

	_, err := session.TailJSONLFile(jsonlFilepath, n, os.Stdout)
	if err != nil {
		return stacktrace.Propagate(err, "")
	}
	return nil
}
