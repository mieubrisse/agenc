package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/server"
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
	configCronUpdateCmd.Flags().Bool(cronConfigEnabledFlagName, true, "whether the cron job is enabled")
}

func runConfigCronUpdate(cmd *cobra.Command, args []string) error {
	name := args[0]

	allFlags := []string{
		cronConfigScheduleFlagName, cronConfigPromptFlagName,
		cronConfigDescriptionFlagName, cronConfigRepoFlagName,
		cronConfigEnabledFlagName,
	}
	if !anyFlagChanged(cmd, allFlags) {
		return stacktrace.NewError("at least one configuration flag must be provided")
	}

	var req server.UpdateCronRequest

	if cmd.Flags().Changed(cronConfigScheduleFlagName) {
		schedule, _ := cmd.Flags().GetString(cronConfigScheduleFlagName)
		req.Schedule = &schedule
	}
	if cmd.Flags().Changed(cronConfigPromptFlagName) {
		prompt, _ := cmd.Flags().GetString(cronConfigPromptFlagName)
		req.Prompt = &prompt
	}
	if cmd.Flags().Changed(cronConfigDescriptionFlagName) {
		description, _ := cmd.Flags().GetString(cronConfigDescriptionFlagName)
		req.Description = &description
	}
	if cmd.Flags().Changed(cronConfigRepoFlagName) {
		repo, _ := cmd.Flags().GetString(cronConfigRepoFlagName)
		if repo != "" {
			result, err := ResolveRepoInput(repo, "Select repo: ")
			if err != nil {
				return stacktrace.Propagate(err, "failed to resolve repo")
			}
			repo = result.RepoName
		}
		req.Repo = &repo
	}
	if cmd.Flags().Changed(cronConfigEnabledFlagName) {
		enabled, _ := cmd.Flags().GetBool(cronConfigEnabledFlagName)
		req.Enabled = &enabled
	}

	client, err := serverClient()
	if err != nil {
		return err
	}

	if _, err := client.UpdateCron(name, req); err != nil {
		return stacktrace.Propagate(err, "failed to update cron job")
	}

	fmt.Printf("Updated cron job '%s'\n", name)
	return nil
}
