package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var cronNewCmd = &cobra.Command{
	Use:   newCmdStr + " [name]",
	Short: "Create a new cron job (interactive wizard)",
	Long: `Create a new cron job using an interactive wizard.

If a name is provided as an argument, the wizard will use it. Otherwise,
you'll be prompted to enter a name.

Example:
  agenc cron new daily-report
  agenc cron new
`,
	Args: cobra.MaximumNArgs(1),
	RunE: runCronNew,
}

func init() {
	cronCmd.AddCommand(cronNewCmd)
}

func runCronNew(cmd *cobra.Command, args []string) error {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return stacktrace.NewError("interactive mode requires a terminal; provide arguments or edit config.yml directly")
	}

	cfg, cm, err := readConfigWithComments()
	if err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)

	// Get cron name
	var name string
	if len(args) > 0 {
		name = args[0]
	} else {
		fmt.Print("Cron job name: ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return stacktrace.Propagate(err, "failed to read input")
		}
		name = strings.TrimSpace(input)
	}

	if err := config.ValidateCronName(name); err != nil {
		return err
	}

	if _, exists := cfg.Crons[name]; exists {
		return stacktrace.NewError("cron job '%s' already exists", name)
	}

	// Get schedule
	fmt.Println("\nEnter cron schedule (e.g., '0 9 * * *' for 9am daily):")
	fmt.Println("  Format: minute hour day-of-month month day-of-week")
	fmt.Println("  Common examples:")
	fmt.Println("    0 9 * * *     - 9am every day")
	fmt.Println("    0 9 * * 1-5   - 9am weekdays")
	fmt.Println("    0 0 * * SUN   - midnight on Sundays")
	fmt.Println("    */15 * * * *  - every 15 minutes")
	fmt.Print("\nSchedule: ")

	scheduleInput, err := reader.ReadString('\n')
	if err != nil {
		return stacktrace.Propagate(err, "failed to read input")
	}
	schedule := strings.TrimSpace(scheduleInput)

	if err := config.ValidateCronSchedule(schedule); err != nil {
		return err
	}

	// Get prompt
	fmt.Print("\nPrompt (what should the agent do?): ")
	promptInput, err := reader.ReadString('\n')
	if err != nil {
		return stacktrace.Propagate(err, "failed to read input")
	}
	prompt := strings.TrimSpace(promptInput)

	if prompt == "" {
		return stacktrace.NewError("prompt cannot be empty")
	}

	// Optional: git repo
	fmt.Print("\nGit repo to clone (press Enter to skip): ")
	gitInput, err := reader.ReadString('\n')
	if err != nil {
		return stacktrace.Propagate(err, "failed to read input")
	}
	gitRepo := strings.TrimSpace(gitInput)

	if gitRepo != "" {
		// Resolve git repo
		result, err := ResolveRepoInput(agencDirpath, gitRepo, "Select repo: ")
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve git repo")
		}
		gitRepo = result.RepoName
	}

	// Optional: timeout
	fmt.Print("\nTimeout (e.g., '1h', '30m'; press Enter for default 1h): ")
	timeoutInput, err := reader.ReadString('\n')
	if err != nil {
		return stacktrace.Propagate(err, "failed to read input")
	}
	timeout := strings.TrimSpace(timeoutInput)

	if timeout != "" {
		if err := config.ValidateCronTimeout(timeout); err != nil {
			return err
		}
	}

	// Create the cron config
	cronCfg := config.CronConfig{
		Schedule: schedule,
		Prompt:   prompt,
		Git:      gitRepo,
		Timeout:  timeout,
	}

	if cfg.Crons == nil {
		cfg.Crons = make(map[string]config.CronConfig)
	}
	cfg.Crons[name] = cronCfg

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("\nCreated cron job '%s'\n", name)
	fmt.Println()

	nextRun, err := config.GetNextCronRun(schedule)
	if err == nil {
		fmt.Printf("Next run: %s\n", nextRun.Local().Format("2006-01-02 15:04:05"))
	}

	fmt.Printf("\nTo disable: %s %s %s %s\n", agencCmdStr, cronCmdStr, disableCmdStr, name)
	fmt.Printf("To run now: %s %s %s %s\n", agencCmdStr, cronCmdStr, runCmdStr, name)

	return nil
}
