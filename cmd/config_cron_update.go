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

	cronCfg, exists := cfg.Crons[name]
	if !exists {
		return stacktrace.NewError("cron job '%s' not found; use '%s %s %s %s %s' to create it",
			name, agencCmdStr, configCmdStr, cronCmdStr, addCmdStr, name)
	}

	// Track which flags were changed
	scheduleChanged := cmd.Flags().Changed(cronConfigScheduleFlagName)
	promptChanged := cmd.Flags().Changed(cronConfigPromptFlagName)
	descriptionChanged := cmd.Flags().Changed(cronConfigDescriptionFlagName)
	repoChanged := cmd.Flags().Changed(cronConfigRepoFlagName)
	timeoutChanged := cmd.Flags().Changed(cronConfigTimeoutFlagName)
	overlapChanged := cmd.Flags().Changed(cronConfigOverlapFlagName)
	enabledChanged := cmd.Flags().Changed(cronConfigEnabledFlagName)

	// Check that at least one flag was provided
	if !scheduleChanged && !promptChanged && !descriptionChanged &&
		!repoChanged && !timeoutChanged && !overlapChanged && !enabledChanged {
		return stacktrace.NewError("at least one configuration flag must be provided")
	}

	// Update fields
	if scheduleChanged {
		schedule, err := cmd.Flags().GetString(cronConfigScheduleFlagName)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read --%s flag", cronConfigScheduleFlagName)
		}
		if err := config.ValidateCronSchedule(schedule); err != nil {
			return err
		}
		cronCfg.Schedule = schedule
	}

	if promptChanged {
		prompt, err := cmd.Flags().GetString(cronConfigPromptFlagName)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read --%s flag", cronConfigPromptFlagName)
		}
		if prompt == "" {
			return stacktrace.NewError("prompt cannot be empty")
		}
		cronCfg.Prompt = prompt
	}

	if descriptionChanged {
		description, err := cmd.Flags().GetString(cronConfigDescriptionFlagName)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read --%s flag", cronConfigDescriptionFlagName)
		}
		cronCfg.Description = description
	}

	if repoChanged {
		repo, err := cmd.Flags().GetString(cronConfigRepoFlagName)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read --%s flag", cronConfigRepoFlagName)
		}
		if repo != "" {
			result, err := ResolveRepoInput(agencDirpath, repo, "Select repo: ")
			if err != nil {
				return stacktrace.Propagate(err, "failed to resolve repo")
			}
			repo = result.RepoName
		}
		cronCfg.Repo = repo
	}

	if timeoutChanged {
		timeout, err := cmd.Flags().GetString(cronConfigTimeoutFlagName)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read --%s flag", cronConfigTimeoutFlagName)
		}
		if err := config.ValidateCronTimeout(timeout); err != nil {
			return err
		}
		cronCfg.Timeout = timeout
	}

	if overlapChanged {
		overlap, err := cmd.Flags().GetString(cronConfigOverlapFlagName)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read --%s flag", cronConfigOverlapFlagName)
		}
		overlapPolicy := config.CronOverlapPolicy(overlap)
		if err := config.ValidateCronOverlapPolicy(overlapPolicy); err != nil {
			return err
		}
		cronCfg.Overlap = overlapPolicy
	}

	if enabledChanged {
		enabled, err := cmd.Flags().GetBool(cronConfigEnabledFlagName)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read --%s flag", cronConfigEnabledFlagName)
		}
		cronCfg.Enabled = &enabled
	}

	cfg.Crons[name] = cronCfg

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("Updated cron job '%s'\n", name)
	return nil
}
