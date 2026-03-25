package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
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

// readPromptLine reads a line from the reader, trims whitespace, and returns it.
func readPromptLine(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to read input")
	}
	return strings.TrimSpace(input), nil
}

// promptCronName returns the cron name from args or prompts the user interactively.
func promptCronName(reader *bufio.Reader, args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	return readPromptLine(reader, "Cron job name: ")
}

// promptCronSchedule displays schedule instructions and reads the schedule from stdin.
func promptCronSchedule(reader *bufio.Reader) (string, error) {
	fmt.Println("\nEnter cron schedule (e.g., '0 9 * * *' for 9am daily):")
	fmt.Println("  Format: minute hour day-of-month month day-of-week")
	fmt.Println("  Only simple integers and '*' are supported (no ranges, lists, or step values).")
	fmt.Println("  Common examples:")
	fmt.Println("    0 9 * * *     - 9am every day")
	fmt.Println("    0 9 * * 1     - 9am every Monday")
	fmt.Println("    0 0 * * 0     - midnight on Sundays")
	fmt.Println("    0 0 1 * *     - midnight on the 1st of each month")

	schedule, err := readPromptLine(reader, "\nSchedule: ")
	if err != nil {
		return "", stacktrace.Propagate(err, "")
	}
	if err := config.ValidateCronSchedule(schedule); err != nil {
		return "", stacktrace.Propagate(err, "")
	}
	return schedule, nil
}

// promptCronGitRepo prompts for an optional git repo and resolves it if provided.
func promptCronGitRepo(reader *bufio.Reader) (string, error) {
	gitRepo, err := readPromptLine(reader, "\nGit repo to clone (press Enter to skip): ")
	if err != nil {
		return "", stacktrace.Propagate(err, "")
	}
	if gitRepo == "" {
		return "", nil
	}
	result, err := ResolveRepoInput(gitRepo, "Select repo: ")
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to resolve git repo")
	}
	return result.RepoName, nil
}

func runCronNew(cmd *cobra.Command, args []string) error {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return stacktrace.NewError("interactive mode requires a terminal; provide arguments or edit config.yml directly")
	}

	cfg, cm, release, err := readConfigWithComments()
	if err != nil {
		return stacktrace.Propagate(err, "")
	}
	defer release()
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get agenc directory path")
	}

	reader := bufio.NewReader(os.Stdin)

	name, err := promptCronName(reader, args)
	if err != nil {
		return stacktrace.Propagate(err, "")
	}
	if err := config.ValidateCronName(name); err != nil {
		return stacktrace.Propagate(err, "")
	}
	if _, exists := cfg.Crons[name]; exists {
		return stacktrace.NewError("cron job '%s' already exists", name)
	}

	schedule, err := promptCronSchedule(reader)
	if err != nil {
		return stacktrace.Propagate(err, "")
	}

	prompt, err := readPromptLine(reader, "\nPrompt (what should the agent do?): ")
	if err != nil {
		return stacktrace.Propagate(err, "")
	}
	if prompt == "" {
		return stacktrace.NewError("prompt cannot be empty")
	}

	gitRepo, err := promptCronGitRepo(reader)
	if err != nil {
		return stacktrace.Propagate(err, "")
	}

	// Create the cron config
	cronCfg := config.CronConfig{
		ID:       uuid.New().String(),
		Schedule: schedule,
		Prompt:   prompt,
		Repo:     gitRepo,
	}

	if cfg.Crons == nil {
		cfg.Crons = make(map[string]config.CronConfig)
	}
	cfg.Crons[name] = cronCfg

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("\nCreated cron job '%s'\n", name)
	fmt.Printf("\nTo disable: %s %s %s %s\n", agencCmdStr, cronCmdStr, disableCmdStr, name)
	fmt.Printf("To run now: %s %s %s %s\n", agencCmdStr, cronCmdStr, runCmdStr, name)

	return nil
}
