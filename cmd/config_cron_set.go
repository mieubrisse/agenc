package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var configCronSetCmd = &cobra.Command{
	Use:   setCmdStr + " <name>",
	Short: "Set or update a cron job configuration",
	Long: `Set or update configuration for a cron job.

If the cron job doesn't exist, it will be created. At least one configuration
flag must be provided. When creating a new cron job, both --schedule and
--prompt are required.

Examples:
  # Create a new cron job
  agenc config cron set daily-report \
    --schedule="0 9 * * *" \
    --prompt="Generate the daily status report" \
    --git=github.com/owner/my-repo \
    --timeout=30m

  # Update an existing cron job's schedule
  agenc config cron set daily-report --schedule="0 10 * * *"

  # Disable a cron job
  agenc config cron set daily-report --enabled=false

  # Update multiple fields at once
  agenc config cron set weekly-cleanup \
    --schedule="0 0 * * SUN" \
    --prompt="Clean up old files" \
    --overlap=skip \
    --timeout=1h
`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigCronSet,
}

func init() {
	configCronCmd.AddCommand(configCronSetCmd)
	configCronSetCmd.Flags().String(cronConfigScheduleFlagName, "", "cron schedule expression (e.g., '0 9 * * *')")
	configCronSetCmd.Flags().String(cronConfigPromptFlagName, "", "initial prompt for the Claude mission")
	configCronSetCmd.Flags().String(cronConfigDescriptionFlagName, "", "human-readable description")
	configCronSetCmd.Flags().String(cronConfigGitFlagName, "", "git repository to clone (e.g., github.com/owner/repo)")
	configCronSetCmd.Flags().String(cronConfigTimeoutFlagName, "", "maximum runtime (e.g., '1h', '30m')")
	configCronSetCmd.Flags().String(cronConfigOverlapFlagName, "", "overlap policy: 'skip' or 'allow'")
	configCronSetCmd.Flags().Bool(cronConfigEnabledFlagName, true, "whether the cron job is enabled")
}

func runConfigCronSet(cmd *cobra.Command, args []string) error {
	name := args[0]

	if err := config.ValidateCronName(name); err != nil {
		return err
	}

	cfg, cm, err := readConfigWithComments()
	if err != nil {
		return err
	}

	// Get existing config or create new one
	cronCfg, exists := cfg.Crons[name]
	isNew := !exists

	// Track which flags were changed
	scheduleChanged := cmd.Flags().Changed(cronConfigScheduleFlagName)
	promptChanged := cmd.Flags().Changed(cronConfigPromptFlagName)
	descriptionChanged := cmd.Flags().Changed(cronConfigDescriptionFlagName)
	gitChanged := cmd.Flags().Changed(cronConfigGitFlagName)
	timeoutChanged := cmd.Flags().Changed(cronConfigTimeoutFlagName)
	overlapChanged := cmd.Flags().Changed(cronConfigOverlapFlagName)
	enabledChanged := cmd.Flags().Changed(cronConfigEnabledFlagName)

	// Check that at least one flag was provided
	if !scheduleChanged && !promptChanged && !descriptionChanged &&
		!gitChanged && !timeoutChanged && !overlapChanged && !enabledChanged {
		return stacktrace.NewError("at least one configuration flag must be provided")
	}

	// For new cron jobs, require schedule and prompt
	if isNew {
		if !scheduleChanged || !promptChanged {
			return stacktrace.NewError("new cron jobs require both --%s and --%s",
				cronConfigScheduleFlagName, cronConfigPromptFlagName)
		}
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

	if gitChanged {
		git, err := cmd.Flags().GetString(cronConfigGitFlagName)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read --%s flag", cronConfigGitFlagName)
		}
		if git != "" {
			// Resolve git repo
			result, err := ResolveRepoInput(agencDirpath, git, "Select repo: ")
			if err != nil {
				return stacktrace.Propagate(err, "failed to resolve git repo")
			}
			git = result.RepoName
		}
		cronCfg.Git = git
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

	// Save to config
	if cfg.Crons == nil {
		cfg.Crons = make(map[string]config.CronConfig)
	}
	cfg.Crons[name] = cronCfg

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	if isNew {
		fmt.Printf("Created cron job '%s'\n", name)
	} else {
		fmt.Printf("Updated cron job '%s'\n", name)
	}

	return nil
}
