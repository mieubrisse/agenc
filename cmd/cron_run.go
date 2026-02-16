package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var cronRunTimeoutFlag string

var cronRunCmd = &cobra.Command{
	Use:   runCmdStr + " <name>",
	Short: "Manually trigger a cron job (runs headless, untracked by cron_id)",
	Long: `Manually trigger a cron job to run immediately as a headless mission.

The mission will NOT be tracked as a cron run (no cron_id/cron_name will be set).
This is useful for testing cron jobs without affecting history/scheduling.

Example:
  agenc cron run daily-report
  agenc cron run daily-report --timeout 30m
`,
	Args: cobra.ExactArgs(1),
	RunE: runCronRun,
}

func init() {
	cronRunCmd.Flags().StringVar(&cronRunTimeoutFlag, timeoutFlagName, "", "override timeout (e.g., '1h', '30m')")
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

	// Determine timeout
	timeout := cronCfg.Timeout
	if cronRunTimeoutFlag != "" {
		timeout = cronRunTimeoutFlag
	}
	if timeout == "" {
		timeout = fmt.Sprintf("%v", config.DefaultCronTimeout)
	}

	// Validate timeout
	if err := config.ValidateCronTimeout(timeout); err != nil {
		return err
	}

	// Build the command arguments - note: no cron-id/cron-name flags
	cmdArgs := []string{
		"mission", "new",
		"--headless",
		"--prompt", cronCfg.Prompt,
		"--timeout", timeout,
	}

	if cronCfg.Repo != "" {
		cmdArgs = append(cmdArgs, cronCfg.Repo)
	}

	// Get the path to the agenc binary
	execPath, err := os.Executable()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get executable path")
	}

	fmt.Printf("Running cron job '%s' (timeout: %s)...\n", name, timeout)
	fmt.Println("Note: This is a manual run - it won't appear in 'cron history'")
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
