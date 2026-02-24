package cmd

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var cronLogsFollowFlag bool

var cronLogsCmd = &cobra.Command{
	Use:   logsCmdStr + " <name>",
	Short: "View output logs from the most recent cron run",
	Long: `View the Claude output logs from the most recent run of a cron job.

Use -f/--follow to tail the log file in real-time.

Example:
  agenc cron logs daily-report
  agenc cron logs daily-report -f
`,
	Args: cobra.ExactArgs(1),
	RunE: runCronLogs,
}

func init() {
	cronLogsCmd.Flags().BoolVarP(&cronLogsFollowFlag, followFlagName, "f", false, "follow log output (like tail -f)")
	cronCmd.AddCommand(cronLogsCmd)
}

func runCronLogs(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Verify cron exists
	cfg, err := readConfig()
	if err != nil {
		return err
	}

	if _, exists := cfg.Crons[name]; !exists {
		return stacktrace.NewError("cron job '%s' not found", name)
	}

	// Find the most recent mission for this cron
	client, err := serverClient()
	if err != nil {
		return err
	}

	missions, err := client.ListMissions(true, name)
	if err != nil {
		return stacktrace.Propagate(err, "failed to query missions")
	}
	if len(missions) == 0 {
		return stacktrace.NewError("no runs found for cron job '%s'", name)
	}
	mission := missions[0]

	// Get the log file path
	logFilepath := config.GetMissionClaudeOutputLogFilepath(agencDirpath, mission.ID)

	// Check if log file exists
	if _, err := os.Stat(logFilepath); os.IsNotExist(err) {
		fmt.Printf("No output log found for mission %s\n", mission.ShortID)
		fmt.Printf("Expected path: %s\n", logFilepath)
		return nil
	}

	fmt.Printf("=== Cron: %s | Mission: %s | Started: %s ===\n\n",
		name, mission.ShortID, mission.CreatedAt.Local().Format("2006-01-02 15:04:05"))

	if cronLogsFollowFlag {
		return tailLogFile(logFilepath)
	}

	return catLogFile(logFilepath)
}

// catLogFile prints the entire contents of the log file.
func catLogFile(filepath string) error {
	file, err := os.Open(filepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open log file")
	}
	defer file.Close()

	_, err = io.Copy(os.Stdout, file)
	return err
}

// tailLogFile follows the log file, printing new content as it's written.
func tailLogFile(filepath string) error {
	file, err := os.Open(filepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open log file")
	}
	defer file.Close()

	// Print existing content first
	if _, err := io.Copy(os.Stdout, file); err != nil {
		return stacktrace.Propagate(err, "failed to read log file")
	}

	// Now follow for new content
	fmt.Println("\n--- Following log (Ctrl+C to stop) ---")

	lastSize, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return stacktrace.Propagate(err, "failed to seek to end of file")
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		// Check current file size
		info, err := file.Stat()
		if err != nil {
			continue
		}

		currentSize := info.Size()
		if currentSize > lastSize {
			// Seek to where we left off
			file.Seek(lastSize, io.SeekStart)

			// Read and print new content
			buf := make([]byte, currentSize-lastSize)
			n, err := file.Read(buf)
			if err != nil && err != io.EOF {
				continue
			}
			if n > 0 {
				os.Stdout.Write(buf[:n])
			}

			lastSize = currentSize
		} else if currentSize < lastSize {
			// File was truncated/rotated, start from beginning
			file.Seek(0, io.SeekStart)
			lastSize = 0
		}
	}

	return nil
}
