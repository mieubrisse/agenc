package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/server"
)

var configCronAddCmd = &cobra.Command{
	Use:   addCmdStr + " <name>",
	Short: "Add a new cron job",
	Long: `Add a new cron job to config.yml.

The name must be unique and follow the naming rules (letters, numbers, hyphens,
underscores; max 64 characters). Both --schedule and --prompt are required.

Schedule expressions use 5 fields: minute hour day month weekday.
Only simple integer values and '*' (any) are supported. Ranges (1-5),
lists (1,3,5), step values (*/15), and named days (SUN) are not supported
because macOS launchd cannot represent them.

Examples:
  agenc config cron add daily-report \
    --schedule="0 9 * * *" \
    --prompt="Generate the daily status report" \
    --repo=github.com/owner/my-repo

  agenc config cron add weekly-cleanup \
    --schedule="0 0 * * 0" \
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
	_ = configCronAddCmd.MarkFlagRequired(cronConfigScheduleFlagName)
	_ = configCronAddCmd.MarkFlagRequired(cronConfigPromptFlagName)
}

func runConfigCronAdd(cmd *cobra.Command, args []string) error {
	name := args[0]

	schedule, err := cmd.Flags().GetString(cronConfigScheduleFlagName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read --%s flag", cronConfigScheduleFlagName)
	}

	prompt, err := cmd.Flags().GetString(cronConfigPromptFlagName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read --%s flag", cronConfigPromptFlagName)
	}

	description, _ := cmd.Flags().GetString(cronConfigDescriptionFlagName)

	repo, _ := cmd.Flags().GetString(cronConfigRepoFlagName)
	if repo != "" {
		result, err := ResolveRepoInput(repo, "Select repo: ")
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve repo")
		}
		repo = result.RepoName
	}

	client, err := serverClient()
	if err != nil {
		return err
	}

	if _, err := client.CreateCron(server.CreateCronRequest{
		Name:        name,
		Schedule:    schedule,
		Prompt:      prompt,
		Description: description,
		Repo:        repo,
	}); err != nil {
		return stacktrace.Propagate(err, "failed to create cron job")
	}

	fmt.Printf("Added cron job '%s'\n", name)
	return nil
}
