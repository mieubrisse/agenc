package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var configInitCmd = &cobra.Command{
	Use:   initCmdStr,
	Short: "Register a Claude config source repo",
	Long: `Register a git repo containing your Claude configuration.

Non-interactive usage:
  agenc config init --repo github.com/owner/dotfiles --subdirectory claude/

Interactive usage (prompts for repo and subdirectory):
  agenc config init

The repo reference accepts the same formats as 'agenc repo add':
  owner/repo, github.com/owner/repo, https://..., git@..., or search terms.

If a subdirectory is specified, it must exist in the cloned repo.
`,
	RunE: runConfigInit,
}

func init() {
	configCmd.AddCommand(configInitCmd)

	configInitCmd.Flags().String(repoFlagName, "", "repo reference (any format accepted by 'agenc repo add')")
	configInitCmd.Flags().String(subdirectoryFlagName, "", "subdirectory within the repo containing Claude config")
}

func runConfigInit(cmd *cobra.Command, args []string) error {
	cfg, cm, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	repoFlag, _ := cmd.Flags().GetString(repoFlagName)
	subdirFlag, _ := cmd.Flags().GetString(subdirectoryFlagName)

	var repoInput string
	var subdirectory string

	if cmd.Flags().Changed(repoFlagName) {
		// Non-interactive: use flags
		repoInput = repoFlag
		subdirectory = subdirFlag
	} else {
		// Interactive mode
		if !isatty.IsTerminal(os.Stdin.Fd()) {
			return stacktrace.NewError("interactive mode requires a terminal; use --repo and --subdirectory flags instead")
		}

		reader := bufio.NewReader(os.Stdin)

		fmt.Print("Claude config repo (e.g., owner/repo or github.com/owner/repo): ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return stacktrace.Propagate(err, "failed to read input")
		}
		repoInput = strings.TrimSpace(input)
		if repoInput == "" {
			return stacktrace.NewError("repo cannot be empty")
		}

		fmt.Print("Subdirectory within repo (press Enter to skip): ")
		subdirInput, err := reader.ReadString('\n')
		if err != nil {
			return stacktrace.Propagate(err, "failed to read input")
		}
		subdirectory = strings.TrimSpace(subdirInput)
	}

	// Resolve the repo input (handles cloning, fzf selection, all formats)
	result, err := ResolveRepoInput(agencDirpath, repoInput, "Select Claude config repo: ")
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve repo")
	}

	// Validate subdirectory exists in the cloned repo
	if subdirectory != "" {
		subdirPath := result.CloneDirpath + "/" + subdirectory
		info, err := os.Stat(subdirPath)
		if err != nil {
			if os.IsNotExist(err) {
				return stacktrace.NewError("subdirectory '%s' does not exist in repo '%s'", subdirectory, result.RepoName)
			}
			return stacktrace.Propagate(err, "failed to check subdirectory '%s'", subdirectory)
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

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("Registered Claude config: %s", result.RepoName)
	if subdirectory != "" {
		fmt.Printf(" (subdirectory: %s)", subdirectory)
	}
	fmt.Println()

	return nil
}
