package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/session"
)

// emptySessionMessage is shown when a session JSONL exists but contains no
// formatted conversation content (e.g., only metadata entries from a
// freshly-spawned mission that has not yet produced user/assistant messages).
const emptySessionMessage = "(session has no conversation messages yet)\n"

// countingWriter wraps an io.Writer and tracks the number of bytes written.
// It allows printSession to detect that a formatter produced no output
// without buffering the entire transcript in memory.
type countingWriter struct {
	w     io.Writer
	count int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.count += int64(n)
	return n, err
}

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
//
// A session JSONL may exist but contain only metadata entries (e.g.,
// file-history-snapshot, progress) when a freshly-spawned mission has not
// yet produced any user or assistant messages. In that case the formatter
// writes nothing; printSession detects this and emits an explanatory message
// to stderr so callers see something instead of silent empty output.
func printSession(jsonlFilepath string, tailLines int, all bool, format string) error {
	return printSessionTo(jsonlFilepath, tailLines, all, format, os.Stdout, os.Stderr)
}

// printSessionTo is the testable core of printSession with explicit writers.
// It uses a counting wrapper to detect zero-byte output without buffering
// the entire (potentially large) transcript in memory.
func printSessionTo(jsonlFilepath string, tailLines int, all bool, format string, stdout io.Writer, stderr io.Writer) error {
	n := tailLines
	if all {
		n = 0
	}

	cw := &countingWriter{w: stdout}
	switch format {
	case "text":
		if err := session.FormatConversation(jsonlFilepath, n, cw); err != nil {
			return stacktrace.Propagate(err, "")
		}
	case "jsonl":
		if _, err := session.TailJSONLFile(jsonlFilepath, n, cw); err != nil {
			return stacktrace.Propagate(err, "")
		}
	default:
		return stacktrace.NewError("invalid format %q: must be %q or %q", format, "text", "jsonl")
	}

	if cw.count == 0 {
		fmt.Fprint(stderr, emptySessionMessage)
	}
	return nil
}
