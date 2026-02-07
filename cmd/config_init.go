package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
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

This command walks through all configuration steps that haven't been completed yet:

1. Config repo — if your config directory isn't backed by a git repo, prompts
   you to clone an existing agenc-config repo.
2. Claude config — if no Claude config source is registered, prompts you to
   register a repo containing your Claude configuration files.

The command is idempotent: steps that are already configured are skipped.
`,
	RunE: runConfigInit,
}

func init() {
	configCmd.AddCommand(configInitCmd)
}

func runConfigInit(cmd *cobra.Command, args []string) error {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return stacktrace.NewError("config init requires an interactive terminal")
	}

	reader := bufio.NewReader(os.Stdin)

	// Step 1: Ensure config directory is backed by a git repo
	configDirpath := config.GetConfigDirpath(agencDirpath)
	configIsGitRepo := isGitRepo(configDirpath)

	if !configIsGitRepo {
		changed, err := setupConfigRepo(reader, configDirpath)
		if err != nil {
			return stacktrace.Propagate(err, "config repo setup failed")
		}
		if changed {
			configIsGitRepo = true
		}
	}

	// Step 2: Ensure claudeConfig is registered
	cfg, cm, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	if cfg.ClaudeConfig == nil || cfg.ClaudeConfig.Repo == "" {
		if err := setupClaudeConfig(reader, cfg, cm); err != nil {
			return stacktrace.Propagate(err, "Claude config setup failed")
		}
	}

	// Print summary
	fmt.Println()
	printConfigSummary(configIsGitRepo, cfg)

	return nil
}

// isGitRepo returns true if the directory contains a .git directory or file.
func isGitRepo(dirpath string) bool {
	_, err := os.Stat(filepath.Join(dirpath, ".git"))
	return err == nil
}

// setupConfigRepo prompts the user to clone an agenc-config repo into the
// config directory. Returns true if a repo was cloned.
func setupConfigRepo(reader *bufio.Reader, configDirpath string) (bool, error) {
	fmt.Println("Your config directory is not backed by a git repo.")
	fmt.Print("Do you have an existing agenc config repo to clone? [y/N] ")

	answer, err := reader.ReadString('\n')
	if err != nil {
		return false, stacktrace.Propagate(err, "failed to read input")
	}
	answer = strings.TrimSpace(strings.ToLower(answer))

	if answer != "y" && answer != "yes" {
		fmt.Println("Skipping config repo setup.")
		return false, nil
	}

	fmt.Println("\nEnter the repo reference. Examples:")
	fmt.Println("  owner/repo                         (uses HTTPS)")
	fmt.Println("  https://github.com/owner/repo      (HTTPS)")
	fmt.Println("  git@github.com:owner/repo.git      (SSH)")
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

// setupClaudeConfig prompts the user to register a Claude config source repo.
func setupClaudeConfig(reader *bufio.Reader, cfg *config.AgencConfig, cm yaml.CommentMap) error {
	fmt.Println("\nNo Claude config source registered.")
	fmt.Print("Claude config repo (e.g., owner/repo or github.com/owner/repo; press Enter to skip): ")

	input, err := reader.ReadString('\n')
	if err != nil {
		return stacktrace.Propagate(err, "failed to read input")
	}
	repoInput := strings.TrimSpace(input)

	if repoInput == "" {
		fmt.Println("Skipping Claude config setup.")
		return nil
	}

	// Resolve the repo input (handles cloning, fzf selection, all formats)
	result, err := ResolveRepoInput(agencDirpath, repoInput, "Select Claude config repo: ")
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve repo")
	}

	fmt.Print("Subdirectory within repo (press Enter to skip): ")
	subdirInput, err := reader.ReadString('\n')
	if err != nil {
		return stacktrace.Propagate(err, "failed to read input")
	}
	subdirectory := strings.TrimSpace(subdirInput)

	// Validate subdirectory exists in the cloned repo
	if subdirectory != "" {
		subdirFullpath := filepath.Join(result.CloneDirpath, subdirectory)
		info, statErr := os.Stat(subdirFullpath)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				return stacktrace.NewError("subdirectory '%s' does not exist in repo '%s'", subdirectory, result.RepoName)
			}
			return stacktrace.Propagate(statErr, "failed to check subdirectory '%s'", subdirectory)
		}
		if !info.IsDir() {
			return stacktrace.NewError("'%s' in repo '%s' is not a directory", subdirectory, result.RepoName)
		}
	}

	// Update config
	if cfg.ClaudeConfig == nil {
		cfg.ClaudeConfig = &config.ClaudeConfig{}
	}
	cfg.ClaudeConfig.Repo = result.RepoName
	cfg.ClaudeConfig.Subdirectory = subdirectory

	if err := writeConfigWithComments(cfg, cm); err != nil {
		return err
	}

	fmt.Printf("Registered Claude config: %s", result.RepoName)
	if subdirectory != "" {
		fmt.Printf(" (subdirectory: %s)", subdirectory)
	}
	fmt.Println()

	return nil
}

// writeConfigWithComments writes the config back, preserving YAML comments.
func writeConfigWithComments(cfg *config.AgencConfig, cm yaml.CommentMap) error {
	return config.WriteAgencConfig(agencDirpath, cfg, cm)
}

// printConfigSummary prints the current configuration state.
func printConfigSummary(configIsGitRepo bool, cfg *config.AgencConfig) {
	fmt.Println("Configuration summary:")

	if configIsGitRepo {
		fmt.Println("  Config repo:   set up")
	} else {
		fmt.Println("  Config repo:   not configured")
	}

	if cfg.ClaudeConfig != nil && cfg.ClaudeConfig.Repo != "" {
		detail := cfg.ClaudeConfig.Repo
		if cfg.ClaudeConfig.Subdirectory != "" {
			detail += " @ " + cfg.ClaudeConfig.Subdirectory
		}
		fmt.Printf("  Claude config: %s\n", detail)
	} else {
		fmt.Println("  Claude config: not configured")
	}
}
