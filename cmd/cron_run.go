package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var cronRunCmd = &cobra.Command{
	Use:   runCmdStr + " <name>",
	Short: "Manually trigger a cron job",
	Long: `Manually trigger a cron job to run immediately as a headless mission.

The mission will be tracked with source flags so it appears in 'cron history'.

Example:
  agenc cron run daily-report
`,
	Args: cobra.ExactArgs(1),
	RunE: runCronRun,
}

func init() {
	cronCmd.AddCommand(cronRunCmd)
}

func runCronRun(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, err := readConfig()
	if err != nil {
		return err
	}

	cronCfg, exists := cfg.Crons[name]
	if !exists {
		return stacktrace.NewError("cron job '%s' not found", name)
	}

	if cronCfg.ID == "" {
		return stacktrace.NewError("cron job '%s' has no ID — re-create it or add an 'id' field to config.yml", name)
	}

	// Build source metadata as proper JSON to avoid injection from cron names
	sourceMetadata, err := json.Marshal(map[string]string{"cron_name": name, "trigger": "manual"})
	if err != nil {
		return stacktrace.Propagate(err, "failed to marshal source metadata")
	}

	// Build the command arguments with source tracking
	cmdArgs := []string{
		"mission", "new",
		"--headless",
		"--source", "cron",
		"--source-id", cronCfg.ID,
		"--source-metadata", string(sourceMetadata),
		"--prompt", cronCfg.Prompt,
	}

	if cronCfg.Repo != "" {
		cmdArgs = append(cmdArgs, cronCfg.Repo)
	} else {
		cmdArgs = append(cmdArgs, "--"+blankFlagName)
	}

	// Get the path to the agenc binary
	execPath, err := os.Executable()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get executable path")
	}

	fmt.Printf("Running cron job '%s'...\n", name)
	fmt.Println()

	// Run in foreground so user can see output
	execCmd := exec.Command(execPath, cmdArgs...)
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	execCmd.Stdin = os.Stdin

	startTime := time.Now()
	err = execCmd.Run()
	duration := time.Since(startTime)

	fmt.Println()
	if err != nil {
		fmt.Printf("Cron job '%s' failed after %v: %v\n", name, duration.Round(time.Second), err)
		return err
	}

	fmt.Printf("Cron job '%s' completed successfully in %v\n", name, duration.Round(time.Second))
	return nil
}
