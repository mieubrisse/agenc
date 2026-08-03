package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

// oauthTokenPrefix is the expected prefix for valid Claude Code OAuth tokens.
// Duplicated here (from config package internal const) so validation lives
// next to the user-facing command that needs it.
const tokenCmdOAuthPrefix = "sk-ant-"

// validateOAuthToken is the single source of truth for token-shape validation.
// It rejects empty tokens and tokens without the expected prefix. Both
// runTokenSet and its tests call this function so the tests exercise the real
// production validation rather than a reimplementation.
func validateOAuthToken(token string) error {
	if token == "" {
		return stacktrace.NewError("token cannot be empty; use 'agenc token clear' to remove the token")
	}
	if !strings.HasPrefix(token, tokenCmdOAuthPrefix) {
		return stacktrace.NewError(
			"token does not look valid — expected a value starting with %q\n"+
				"Run 'agenc token setup' for an interactive setup wizard, or 'claude auth login' for native auth",
			tokenCmdOAuthPrefix,
		)
	}
	return nil
}

var tokenCmd = &cobra.Command{
	Use:   tokenCmdStr,
	Short: "Manage the long-lived OAuth token (opt-in fallback for headless or multi-session use)",
	Long: `Manage the AgenC OAuth token.

AgenC missions default to using your native Claude Code authentication
(via 'claude auth login'). The OAuth token is an opt-in fallback for
headless or multi-session workflows where native auth is unavailable or
causes refresh thrashing.

When a token is configured, AgenC passes it to Claude via the
CLAUDE_CODE_OAUTH_TOKEN environment variable. When no token is set,
Claude uses its own native authentication.

Subcommands:
  agenc token set <token>   Store a long-lived OAuth token
  agenc token clear         Remove the stored token (revert to native auth)
  agenc token setup         Interactive wizard to obtain and store a token`,
}

var tokenSetCmd = &cobra.Command{
	Use:   setCmdStr + " <token>",
	Short: "Store a long-lived Claude Code OAuth token",
	Long: `Store a long-lived Claude Code OAuth token for use by AgenC missions.

The token must start with "sk-ant-". It is stored at
$AGENC_DIRPATH/cache/oauth-token with mode 600 (owner-only read/write)
and is never committed to Git.

When a token is set, all new missions receive it via CLAUDE_CODE_OAUTH_TOKEN.
Running missions pick it up on their next restart.

To obtain a long-lived token interactively, run: agenc token setup

You can also manage the token via the config alias:
  agenc config set claudeCodeOAuthToken <token>
  agenc config get claudeCodeOAuthToken`,
	Args: cobra.ExactArgs(1),
	RunE: runTokenSet,
}

var tokenClearCmd = &cobra.Command{
	Use:   clearCmdStr,
	Short: "Remove the stored OAuth token (revert to native Claude authentication)",
	Long: `Remove the stored OAuth token file.

After clearing, AgenC missions will use Claude's native authentication
(set up via 'claude auth login') rather than an explicit token.

Equivalent to: agenc config set claudeCodeOAuthToken ""`,
	Args: cobra.NoArgs,
	RunE: runTokenClear,
}

var tokenSetupCmd = &cobra.Command{
	Use:   setupCmdStr,
	Short: "Interactive wizard to obtain and store a long-lived OAuth token",
	Long: `Run the interactive token-setup wizard.

This walks you through running 'claude setup-token' and storing the
resulting long-lived token. Requires a TTY (interactive terminal).

Use this when native Claude authentication doesn't work for your
multi-session AgenC workflow. For most users, 'claude auth login'
followed by normal AgenC usage is sufficient.`,
	Args: cobra.NoArgs,
	RunE: runTokenSetup,
}

func init() {
	tokenCmd.AddCommand(tokenSetCmd)
	tokenCmd.AddCommand(tokenClearCmd)
	tokenCmd.AddCommand(tokenSetupCmd)
	rootCmd.AddCommand(tokenCmd)
}

func runTokenSet(cmd *cobra.Command, args []string) error {
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get agenc directory path")
	}

	token := strings.TrimSpace(args[0])
	if err := validateOAuthToken(token); err != nil {
		return err
	}

	if err := config.WriteOAuthToken(agencDirpath, token); err != nil {
		return stacktrace.Propagate(err, "failed to write OAuth token")
	}

	fmt.Println("OAuth token stored successfully.")
	fmt.Println()
	fmt.Println("New missions will use this token automatically.")
	fmt.Println("Running missions will pick it up on their next restart.")
	fmt.Println()
	fmt.Println("To remove the token later (revert to native auth):")
	fmt.Println("  agenc token clear")
	return nil
}

func runTokenClear(cmd *cobra.Command, args []string) error {
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get agenc directory path")
	}

	if err := config.WriteOAuthToken(agencDirpath, ""); err != nil {
		return stacktrace.Propagate(err, "failed to clear OAuth token")
	}

	fmt.Println("OAuth token cleared.")
	fmt.Println()
	fmt.Println("Missions will now use native Claude authentication.")
	fmt.Println("Ensure you are logged in with: claude auth login")
	return nil
}

func runTokenSetup(cmd *cobra.Command, args []string) error {
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get agenc directory path")
	}

	if err := config.SetupOAuthToken(agencDirpath); err != nil {
		return stacktrace.Propagate(err, "token setup failed")
	}
	return nil
}
