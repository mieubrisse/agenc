package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var configCronAddCmd = &cobra.Command{
	Use:   addCmdStr + " <name>",
	Short: "Add a new cron job",
	Long: `Add a new cron job to config.yml.

The name must be unique and follow the naming rules (letters, numbers, hyphens,
underscores; max 64 characters). Both --schedule and --prompt are required.

Examples:
  agenc config cron add daily-report \
    --schedule="0 9 * * *" \
    --prompt="Generate the daily status report" \
    --repo=github.com/owner/my-repo \
    --timeout=30m

  agenc config cron add weekly-cleanup \
    --schedule="0 0 * * SUN" \
    --prompt="Clean up old temporary files" \
    --overlap=skip
`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigCronAdd,
}

func init() {
	configCronCmd.AddCommand(configCronAddCmd)
	configCronAddCmd.Flags().String(cronConfigScheduleFlagName, "", "cron schedule expression (e.g., '0 9 * * *') (required)")
	configCronAddCmd.Flags().String(cronConfigPromptFlagName, "", "initial prompt for the Claude mission (required)")
	configCronAddCmd.Flags().String(cronConfigDescriptionFlagName, "", "human-readable description (optional)")
	configCronAddCmd.Flags().String(cronConfigRepoFlagName, "", "repository to clone (e.g., github.com/owner/repo) (optional)")
	configCronAddCmd.Flags().String(cronConfigTimeoutFlagName, "", "maximum runtime (e.g., '1h', '30m') (optional)")
	configCronAddCmd.Flags().String(cronConfigOverlapFlagName, "", "overlap policy: 'skip' or 'allow' (optional)")
	_ = configCronAddCmd.MarkFlagRequired(cronConfigScheduleFlagName)
	_ = configCronAddCmd.MarkFlagRequired(cronConfigPromptFlagName)
}

func runConfigCronAdd(cmd *cobra.Command, args []string) error {
	name := args[0]

	if err := config.ValidateCronName(name); err != nil {
		return err
	}

	cfg, cm, err := readConfigWithComments()
	if err != nil {
		return err
	}

	if _, exists := cfg.Crons[name]; exists {
		return stacktrace.NewError("cron job '%s' already exists; use '%s %s %s %s %s' to modify it",
			name, agencCmdStr, configCmdStr, cronCmdStr, updateCmdStr, name)
	}

	schedule, err := cmd.Flags().GetString(cronConfigScheduleFlagName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read --%s flag", cronConfigScheduleFlagName)
	}
	if err := config.ValidateCronSchedule(schedule); err != nil {
		return err
	}

	prompt, err := cmd.Flags().GetString(cronConfigPromptFlagName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read --%s flag", cronConfigPromptFlagName)
	}
	if prompt == "" {
		return stacktrace.NewError("prompt cannot be empty")
	}

	description, _ := cmd.Flags().GetString(cronConfigDescriptionFlagName)
	timeoutStr, _ := cmd.Flags().GetString(cronConfigTimeoutFlagName)
	overlapStr, _ := cmd.Flags().GetString(cronConfigOverlapFlagName)

	repo, _ := cmd.Flags().GetString(cronConfigRepoFlagName)
	if repo != "" {
		result, err := ResolveRepoInput(agencDirpath, repo, "Select repo: ")
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve repo")
		}
		repo = result.RepoName
	}

	if timeoutStr != "" {
		if err := config.ValidateCronTimeout(timeoutStr); err != nil {
			return err
		}
	}

	var overlapPolicy config.CronOverlapPolicy
	if overlapStr != "" {
		overlapPolicy = config.CronOverlapPolicy(overlapStr)
		if err := config.ValidateCronOverlapPolicy(overlapPolicy); err != nil {
			return err
		}
	}

	cronCfg := config.CronConfig{
		Schedule:    schedule,
		Prompt:      prompt,
		Description: description,
		Git:         repo,
		Timeout:     timeoutStr,
		Overlap:     overlapPolicy,
	}

	if cfg.Crons == nil {
		cfg.Crons = make(map[string]config.CronConfig)
	}
	cfg.Crons[name] = cronCfg

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("Added cron job '%s'\n", name)

	nextRun, err := config.GetNextCronRun(schedule)
	if err == nil {
		fmt.Printf("Next run: %s\n", nextRun.Local().Format("2006-01-02 15:04:05"))
	}

	return nil
}
