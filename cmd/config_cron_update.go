package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var configCronUpdateCmd = &cobra.Command{
	Use:   updateCmdStr + " <name>",
	Short: "Update an existing cron job",
	Long: `Update configuration for an existing cron job.

At least one configuration flag must be provided. Only the specified fields
will be updated; other fields remain unchanged.

Examples:
  # Update the schedule
  agenc config cron update daily-report --schedule="0 10 * * *"

  # Disable a cron job
  agenc config cron update daily-report --enabled=false

  # Update multiple fields at once
  agenc config cron update weekly-cleanup \
    --prompt="Clean up old files and logs" \
    --timeout=2h \
    --overlap=allow

  # Clear the repository
  agenc config cron update daily-report --repo=""
`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigCronUpdate,
}

func init() {
	configCronCmd.AddCommand(configCronUpdateCmd)
	configCronUpdateCmd.Flags().String(cronConfigScheduleFlagName, "", "cron schedule expression (e.g., '0 9 * * *')")
	configCronUpdateCmd.Flags().String(cronConfigPromptFlagName, "", "initial prompt for the Claude mission")
	configCronUpdateCmd.Flags().String(cronConfigDescriptionFlagName, "", "human-readable description")
	configCronUpdateCmd.Flags().String(cronConfigRepoFlagName, "", "repository to clone (e.g., github.com/owner/repo)")
	configCronUpdateCmd.Flags().String(cronConfigTimeoutFlagName, "", "maximum runtime (e.g., '1h', '30m')")
	configCronUpdateCmd.Flags().String(cronConfigOverlapFlagName, "", "overlap policy: 'skip' or 'allow'")
	configCronUpdateCmd.Flags().Bool(cronConfigEnabledFlagName, true, "whether the cron job is enabled")
}

func runConfigCronUpdate(cmd *cobra.Command, args []string) error {
	name := args[0]

	if err := config.ValidateCronName(name); err != nil {
		return err
	}

	cfg, cm, err := readConfigWithComments()
	if err != nil {
		return err
	}
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get agenc directory path")
	}

	cronCfg, exists := cfg.Crons[name]
	if !exists {
		return stacktrace.NewError("cron job '%s' not found; use '%s %s %s %s %s' to create it",
			name, agencCmdStr, configCmdStr, cronCmdStr, addCmdStr, name)
	}

	if err := applyCronUpdateFlags(cmd, &cronCfg); err != nil {
		return err
	}

	cfg.Crons[name] = cronCfg

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("Updated cron job '%s'\n", name)
	return nil
}

// applyCronUpdateFlags reads all changed flags from the command and applies
// them to the cron config. Returns an error if no flags were changed or if
// any flag value is invalid.
func applyCronUpdateFlags(cmd *cobra.Command, cronCfg *config.CronConfig) error {
	allFlags := []string{
		cronConfigScheduleFlagName, cronConfigPromptFlagName,
		cronConfigDescriptionFlagName, cronConfigRepoFlagName,
		cronConfigTimeoutFlagName, cronConfigOverlapFlagName,
		cronConfigEnabledFlagName,
	}
	if !anyFlagChanged(cmd, allFlags) {
		return stacktrace.NewError("at least one configuration flag must be provided")
	}

	if err := applyStringFlag(cmd, cronConfigScheduleFlagName, func(schedule string) error {
		if err := config.ValidateCronSchedule(schedule); err != nil {
			return err
		}
		cronCfg.Schedule = schedule
		return nil
	}); err != nil {
		return err
	}

	if err := applyStringFlag(cmd, cronConfigPromptFlagName, func(prompt string) error {
		if prompt == "" {
			return stacktrace.NewError("prompt cannot be empty")
		}
		cronCfg.Prompt = prompt
		return nil
	}); err != nil {
		return err
	}

	if err := applyStringFlag(cmd, cronConfigDescriptionFlagName, func(description string) error {
		cronCfg.Description = description
		return nil
	}); err != nil {
		return err
	}

	if err := applyStringFlag(cmd, cronConfigRepoFlagName, func(repo string) error {
		if repo != "" {
			result, err := ResolveRepoInput(repo, "Select repo: ")
			if err != nil {
				return stacktrace.Propagate(err, "failed to resolve repo")
			}
			repo = result.RepoName
		}
		cronCfg.Repo = repo
		return nil
	}); err != nil {
		return err
	}

	if err := applyStringFlag(cmd, cronConfigTimeoutFlagName, func(timeout string) error {
		if err := config.ValidateCronTimeout(timeout); err != nil {
			return err
		}
		cronCfg.Timeout = timeout
		return nil
	}); err != nil {
		return err
	}

	if err := applyStringFlag(cmd, cronConfigOverlapFlagName, func(overlap string) error {
		overlapPolicy := config.CronOverlapPolicy(overlap)
		if err := config.ValidateCronOverlapPolicy(overlapPolicy); err != nil {
			return err
		}
		cronCfg.Overlap = overlapPolicy
		return nil
	}); err != nil {
		return err
	}

	return applyBoolFlag(cmd, cronConfigEnabledFlagName, func(enabled bool) error {
		cronCfg.Enabled = &enabled
		return nil
	})
}
