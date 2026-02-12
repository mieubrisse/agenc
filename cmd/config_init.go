package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

var configInitCmd = &cobra.Command{
	Use:   initCmdStr,
	Short: "Initialize agenc configuration (interactive)",
	Long: `Initialize agenc configuration through an interactive wizard.

If your config directory isn't backed by a git repo, prompts you to clone an
existing agenc-config repo or create a new one. The command is idempotent: if
already configured, it simply prints the current state.
`,
	RunE: runConfigInit,
}

func init() {
	configCmd.AddCommand(configInitCmd)
}

func runConfigInit(cmd *cobra.Command, args []string) error {
	dirpath, err := ensureConfigured()
	if err != nil {
		return err
	}

	// Always print summary (the only difference from auto-onboarding)
	configIsGitRepo := isGitRepo(config.GetConfigDirpath(dirpath))
	fmt.Println()
	printConfigSummary(configIsGitRepo)
	return nil
}

// ensureConfigured is the single idempotent function that gets agenc into a
// working state. It resolves the agenc directory, ensures the directory
// structure, and verifies all required config is present.
//
// If config is incomplete:
//   - TTY available: runs the interactive setup wizard
//   - No TTY: returns an error
//
// If config is already complete: returns immediately.
func ensureConfigured() (string, error) {
	dirpath, err := config.GetAgencDirpath()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to get agenc directory path")
	}

	agencDirpath = dirpath

	if err := handleFirstRun(dirpath); err != nil {
		return "", stacktrace.Propagate(err, "first-run setup failed")
	}

	if err := config.EnsureDirStructure(dirpath); err != nil {
		return "", stacktrace.Propagate(err, "failed to ensure directory structure")
	}

	// Check config completeness
	configDirpath := config.GetConfigDirpath(dirpath)
	configIsGitRepo := isGitRepo(configDirpath)

	if configIsGitRepo {
		return dirpath, nil // Already configured
	}

	// Config incomplete — need TTY for interactive setup
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return "", stacktrace.NewError(
			"configuration is incomplete; run '%s %s %s' interactively",
			agencCmdStr, configCmdStr, initCmdStr,
		)
	}

	reader := bufio.NewReader(os.Stdin)

	changed, err := setupConfigRepo(reader, configDirpath)
	if err != nil {
		return "", stacktrace.Propagate(err, "config repo setup failed")
	}
	if changed {
		configIsGitRepo = true
	}

	fmt.Println()
	printConfigSummary(configIsGitRepo)

	return dirpath, nil
}

// isGitRepo returns true if the directory contains a .git directory or file.
func isGitRepo(dirpath string) bool {
	_, err := os.Stat(filepath.Join(dirpath, ".git"))
	return err == nil
}

// setupConfigRepo prompts the user to clone an agenc-config repo into the
// config directory. If the user doesn't have an existing repo, offers to
// create one. Returns true if a repo was cloned.
func setupConfigRepo(reader *bufio.Reader, configDirpath string) (bool, error) {
	fmt.Println("Your config directory is not backed by a git repo.")
	fmt.Print("Do you have an existing agenc config repo to clone? [y/N] ")

	answer, err := reader.ReadString('\n')
	if err != nil {
		return false, stacktrace.Propagate(err, "failed to read input")
	}
	answer = strings.TrimSpace(strings.ToLower(answer))

	if answer == "y" || answer == "yes" {
		return promptAndCloneConfigRepo(reader, configDirpath)
	}

	// No existing repo — offer to create one
	return offerCreateConfigRepo(reader, configDirpath)
}

// promptAndCloneConfigRepo asks the user for a repo reference and clones it
// into the config directory.
func promptAndCloneConfigRepo(reader *bufio.Reader, configDirpath string) (bool, error) {
	fmt.Println()
	printRepoFormatHelp()
	fmt.Print("\nRepo: ")

	repoRef, err := reader.ReadString('\n')
	if err != nil {
		return false, stacktrace.Propagate(err, "failed to read input")
	}
	repoRef = strings.TrimSpace(repoRef)

	if repoRef == "" {
		fmt.Println("No repo provided, skipping.")
		return false, nil
	}

	if err := cloneIntoConfigDir(configDirpath, repoRef); err != nil {
		return false, err
	}

	return true, nil
}

// offerCreateConfigRepo asks the user if they'd like to create a new config
// repo on GitHub. If yes, creates it as a private repo and clones it into the
// config directory.
func offerCreateConfigRepo(reader *bufio.Reader, configDirpath string) (bool, error) {
	fmt.Print("Would you like to create one? [y/N] ")

	answer, err := reader.ReadString('\n')
	if err != nil {
		return false, stacktrace.Propagate(err, "failed to read input")
	}
	answer = strings.TrimSpace(strings.ToLower(answer))

	if answer != "y" && answer != "yes" {
		fmt.Println("Skipping config repo setup.")
		return false, nil
	}

	fmt.Print("Repo name (owner/repo): ")
	repoRef, err := reader.ReadString('\n')
	if err != nil {
		return false, stacktrace.Propagate(err, "failed to read input")
	}
	repoRef = strings.TrimSpace(repoRef)

	if repoRef == "" {
		fmt.Println("No repo name provided, skipping.")
		return false, nil
	}

	if err := createAndCloneConfigRepo(configDirpath, repoRef); err != nil {
		return false, err
	}

	return true, nil
}

// createAndCloneConfigRepo creates a new private GitHub repo using the gh CLI,
// then clones it into the config directory.
func createAndCloneConfigRepo(configDirpath string, repoRef string) error {
	// Validate the repo reference parses correctly
	_, _, err := mission.ParseRepoReference(repoRef, false)
	if err != nil {
		return stacktrace.Propagate(err, "invalid repo name")
	}

	// Create the repo as private via gh CLI
	fmt.Printf("Creating private repo %s...\n", repoRef)
	ghCmd := exec.Command("gh", "repo", "create", repoRef, "--private")
	ghCmd.Stdout = os.Stdout
	ghCmd.Stderr = os.Stderr
	if err := ghCmd.Run(); err != nil {
		return stacktrace.Propagate(err, "failed to create GitHub repo (is the 'gh' CLI installed and authenticated?)")
	}

	fmt.Println("Repo created.")

	// Clone the newly created repo into the config directory
	if err := cloneIntoConfigDir(configDirpath, repoRef); err != nil {
		return err
	}

	return nil
}

// cloneIntoConfigDir clones the given repo reference into the config directory,
// backing up any existing seed files first and re-seeding missing files after.
func cloneIntoConfigDir(configDirpath string, repoRef string) error {
	// Parse the repo reference. On first setup there may be no existing repos
	// to detect protocol from, so shorthand defaults to HTTPS.
	_, cloneURL, err := mission.ParseRepoReference(repoRef, false)
	if err != nil {
		return stacktrace.Propagate(err, "invalid repo reference")
	}

	// Back up the existing config directory (contains seed files from EnsureDirStructure)
	backupDirpath := configDirpath + ".bak"
	if err := os.Rename(configDirpath, backupDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to back up config directory")
	}

	fmt.Printf("Cloning %s into config directory...\n", cloneURL)

	gitCmd := exec.Command("git", "clone", cloneURL, configDirpath)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	if err := gitCmd.Run(); err != nil {
		// Restore backup on failure
		if restoreErr := os.Rename(backupDirpath, configDirpath); restoreErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to restore config backup: %v\n", restoreErr)
		}
		return stacktrace.Propagate(err, "failed to clone config repo")
	}

	// Remove backup
	os.RemoveAll(backupDirpath)

	// Re-seed any files the clone might not have (config.yml, claude-modifications/)
	agencDirpath := filepath.Dir(configDirpath)
	if err := config.EnsureConfigFile(agencDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to seed config file after clone")
	}
	if err := config.EnsureClaudeModificationsFiles(agencDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to seed claude-modifications after clone")
	}

	fmt.Println("Config repo cloned successfully.")
	return nil
}

// printRepoFormatHelp prints the accepted repo reference formats. Use this
// anywhere we prompt the user for a repo reference so the guidance is
// consistent.
func printRepoFormatHelp() {
	fmt.Println("Accepted formats:")
	fmt.Println("  owner/repo                         shorthand (uses HTTPS)")
	fmt.Println("  github.com/owner/repo              canonical name")
	fmt.Println("  https://github.com/owner/repo      HTTPS URL")
	fmt.Println("  git@github.com:owner/repo.git      SSH URL")
}

// printConfigSummary prints the current configuration state.
func printConfigSummary(configIsGitRepo bool) {
	fmt.Println("Configuration summary:")

	if configIsGitRepo {
		fmt.Println("  Config repo: set up")
	} else {
		fmt.Println("  Config repo: not configured")
	}
}
