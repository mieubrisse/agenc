package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)


var (
	templateNewPublicFlag   bool
	templateNewNicknameFlag string
	templateNewDefaultFlag  string
	templateNewCloneFlag    string
)

var templateNewCmd = &cobra.Command{
	Use:   newCmdStr + " <repo>",
	Short: "Create a new agent template repository",
	Long: fmt.Sprintf(`Create a new agent template repository on GitHub.

Accepts any of these formats:
  owner/repo                           - shorthand (e.g., mieubrisse/my-agent)
  github.com/owner/repo                - canonical name
  https://github.com/owner/repo        - HTTPS URL
  git@github.com:owner/repo.git        - SSH URL

Behavior depends on the repository state:
  - If the repo does NOT exist on GitHub: prompts to create it, then initializes
    it with template files (CLAUDE.md, .claude/settings.json, .mcp.json)
  - If the repo exists but is EMPTY: clones it and initializes with template files
  - If the repo exists and is NOT empty: fails with an error

Use --%s to copy files from an existing template in your library. The --%s
flag accepts the same formats as above, or search terms to match against your
template library.

The new template is automatically added to your template library and a mission
is launched to edit it (same as 'template edit').`, cloneFlagName, cloneFlagName),
	Args: cobra.ExactArgs(1),
	RunE: runTemplateNew,
}

func init() {
	templateNewCmd.Flags().BoolVar(&templateNewPublicFlag, publicFlagName, false, "create a public repository (default is private)")
	templateNewCmd.Flags().StringVar(&templateNewNicknameFlag, nicknameFlagName, "", nicknameFlagDesc)
	templateNewCmd.Flags().StringVar(&templateNewDefaultFlag, defaultFlagName, "", defaultFlagDesc())
	templateNewCmd.Flags().StringVar(&templateNewCloneFlag, cloneFlagName, "", "copy files from an existing template (accepts repo reference or search terms)")
	templateCmd.AddCommand(templateNewCmd)
}

func runTemplateNew(cmd *cobra.Command, args []string) error {
	input := args[0]

	// Ensure gh CLI is available
	if _, err := exec.LookPath("gh"); err != nil {
		return stacktrace.NewError("'gh' CLI not found in PATH; install it from https://cli.github.com/")
	}

	// If --clone is provided, resolve the source template
	var sourceTemplateName string
	var sourceTemplateDirpath string
	if templateNewCloneFlag != "" {
		cfg, _, err := config.ReadAgencConfig(agencDirpath)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read config")
		}

		if len(cfg.AgentTemplates) == 0 {
			return stacktrace.NewError("no templates in library to clone from")
		}

		// Parse --clone value as search terms (split on spaces)
		cloneArgs := strings.Fields(templateNewCloneFlag)
		resolved, err := resolveOrPickTemplate(cfg.AgentTemplates, cloneArgs)
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve source template")
		}

		sourceTemplateName = resolved
		sourceTemplateDirpath = config.GetRepoDirpath(agencDirpath, sourceTemplateName)

		// Verify the source template directory exists
		if _, statErr := os.Stat(sourceTemplateDirpath); os.IsNotExist(statErr) {
			return stacktrace.NewError("source template '%s' not found on disk at '%s'; run '%s %s %s' first",
				sourceTemplateName, sourceTemplateDirpath, agencCmdStr, templateCmdStr, updateCmdStr)
		}
	}

	// Parse the repo reference to get canonical name
	preferSSH, err := getProtocolPreference(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to determine protocol preference")
	}

	repoName, cloneURL, err := mission.ParseRepoReference(input, preferSSH)
	if err != nil {
		return stacktrace.Propagate(err, "invalid repo reference '%s'", input)
	}

	// Extract owner/repo from canonical name (github.com/owner/repo)
	ownerRepo := strings.TrimPrefix(repoName, "github.com/")

	// Check if repo exists on GitHub
	repoExists, isEmpty, err := checkGitHubRepoState(ownerRepo)
	if err != nil {
		return stacktrace.Propagate(err, "failed to check repository state")
	}

	cloneDirpath := config.GetRepoDirpath(agencDirpath, repoName)

	if !repoExists {
		// Repo doesn't exist - ask if user wants to create it
		if !promptYesNo(fmt.Sprintf("Repository '%s' does not exist. Create it?", ownerRepo)) {
			fmt.Println("Aborted.")
			return nil
		}

		// If --public not specified, confirm private repo creation
		if !templateNewPublicFlag {
			if !promptYesNo(fmt.Sprintf("Repository will be private. Continue? (use --%s to create a public repo)", publicFlagName)) {
				fmt.Println("Aborted.")
				return nil
			}
		}

		// Create the repo on GitHub (private by default, public if --public flag is set)
		if err := createGitHubRepo(ownerRepo, !templateNewPublicFlag); err != nil {
			return stacktrace.Propagate(err, "failed to create repository on GitHub")
		}

		// Clone the newly created repo
		if err := cloneRepo(cloneURL, cloneDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to clone repository")
		}

		// Initialize with template files (from source template or empty)
		if sourceTemplateDirpath != "" {
			if err := copyTemplateFiles(sourceTemplateDirpath, cloneDirpath); err != nil {
				return stacktrace.Propagate(err, "failed to copy template files from '%s'", sourceTemplateName)
			}
		} else {
			if err := initializeTemplateFiles(cloneDirpath); err != nil {
				return stacktrace.Propagate(err, "failed to initialize template files")
			}
		}

		// Commit and push
		if err := commitAndPushTemplateFiles(cloneDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to commit and push template files")
		}
	} else if isEmpty {
		// Repo exists but is empty - clone and initialize
		fmt.Printf("Repository '%s' exists but is empty. Initializing with template files...\n", ownerRepo)

		// Clone the empty repo
		if err := cloneRepo(cloneURL, cloneDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to clone repository")
		}

		// Initialize with template files (from source template or empty)
		if sourceTemplateDirpath != "" {
			if err := copyTemplateFiles(sourceTemplateDirpath, cloneDirpath); err != nil {
				return stacktrace.Propagate(err, "failed to copy template files from '%s'", sourceTemplateName)
			}
		} else {
			if err := initializeTemplateFiles(cloneDirpath); err != nil {
				return stacktrace.Propagate(err, "failed to initialize template files")
			}
		}

		// Commit and push
		if err := commitAndPushTemplateFiles(cloneDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to commit and push template files")
		}
	} else {
		// Repo exists and is not empty - error
		return stacktrace.NewError(
			"repository '%s' already exists and is not empty; cannot create a new agent template from a non-empty repository",
			ownerRepo,
		)
	}

	// Add to template library (reuses template add infrastructure)
	added, err := addTemplateToLibrary(agencDirpath, repoName, templateNewNicknameFlag, templateNewDefaultFlag)
	if err != nil {
		return stacktrace.Propagate(err, "failed to add template to library")
	}
	if added {
		printTemplateAdded(repoName)
	}

	// Generate initial prompt based on repo name to guide agent template creation
	var initialPrompt string
	if sourceTemplateName != "" {
		initialPrompt = buildTemplateClonePrompt(ownerRepo, sourceTemplateName)
	} else {
		initialPrompt = buildTemplateNewPrompt(ownerRepo)
	}

	// Launch a mission to edit the new template (reuses template edit infrastructure)
	return launchTemplateEditMission(agencDirpath, repoName, initialPrompt)
}

// buildTemplateNewPrompt generates an initial prompt for creating a new agent
// template, incorporating the repo name to infer user intent.
func buildTemplateNewPrompt(ownerRepo string) string {
	// Extract just the repo name (after the slash)
	repoName := ownerRepo
	if idx := strings.LastIndex(ownerRepo, "/"); idx != -1 {
		repoName = ownerRepo[idx+1:]
	}

	return fmt.Sprintf(`I just created a new agent template repository called "%s".

Based on the repository name, I'd like you to help me build out this agent template. Before writing any code, please:

1. Analyze the repo name and share your interpretation of what kind of agent I might be trying to build
2. Ask me clarifying questions about:
   - The agent's primary purpose and use cases
   - What tools/capabilities the agent should have access to
   - Any constraints or guardrails that should be in place
   - The target users or contexts where this agent will be used

Once you understand my requirements, help me create a well-structured CLAUDE.md with clear instructions, and configure .claude/settings.json and .mcp.json appropriately.`, repoName)
}

// buildTemplateClonePrompt generates an initial prompt for creating a new agent
// template that was cloned from an existing template.
func buildTemplateClonePrompt(ownerRepo string, sourceTemplate string) string {
	// Extract just the repo names (after the slash)
	repoName := ownerRepo
	if idx := strings.LastIndex(ownerRepo, "/"); idx != -1 {
		repoName = ownerRepo[idx+1:]
	}

	sourceName := sourceTemplate
	if idx := strings.LastIndex(sourceTemplate, "/"); idx != -1 {
		sourceName = sourceTemplate[idx+1:]
	}

	return fmt.Sprintf(`I just created a new agent template repository called "%s", cloned from "%s".

The template files have been copied from the source template. Please:

1. Review the copied CLAUDE.md, .claude/settings.json, and .mcp.json files
2. Help me understand what the original template was designed for
3. Ask me clarifying questions about how I want to modify or customize this template:
   - What should change about the agent's purpose or behavior?
   - What capabilities should be added or removed?
   - What constraints or guardrails need adjustment?
   - Who will be using this variant and in what contexts?

Once you understand my customization needs, help me update the configuration files appropriately.`, repoName, sourceName)
}

// checkGitHubRepoState checks if a GitHub repo exists and whether it's empty.
// Returns (exists, isEmpty, error).
func checkGitHubRepoState(ownerRepo string) (bool, bool, error) {
	// Check if repo exists using gh repo view
	viewCmd := exec.Command("gh", "repo", "view", ownerRepo, "--json", "isEmpty")
	output, err := viewCmd.Output()
	if err != nil {
		// If the command fails, the repo likely doesn't exist
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			if strings.Contains(stderr, "Could not resolve") || strings.Contains(stderr, "not found") {
				return false, false, nil
			}
		}
		return false, false, stacktrace.Propagate(err, "failed to query repository")
	}

	// Parse the JSON response to check if empty
	// Response looks like: {"isEmpty":true} or {"isEmpty":false}
	outputStr := strings.TrimSpace(string(output))
	isEmpty := strings.Contains(outputStr, `"isEmpty":true`)

	return true, isEmpty, nil
}

// createGitHubRepo creates a new repository on GitHub.
func createGitHubRepo(ownerRepo string, private bool) error {
	visibility := "public"
	visibilityFlag := "--public"
	if private {
		visibility = "private"
		visibilityFlag = "--private"
	}

	fmt.Printf("Creating %s repository '%s' on GitHub...\n", visibility, ownerRepo)

	// Use gh repo create with visibility flag
	// The ownerRepo format (owner/repo) works directly with gh
	createCmd := exec.Command("gh", "repo", "create", ownerRepo, visibilityFlag)
	createCmd.Stdout = os.Stdout
	createCmd.Stderr = os.Stderr

	if err := createCmd.Run(); err != nil {
		return stacktrace.Propagate(err, "gh repo create failed")
	}

	return nil
}

// cloneRepo clones a repository into the specified directory.
func cloneRepo(cloneURL string, cloneDirpath string) error {
	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(cloneDirpath), 0755); err != nil {
		return stacktrace.Propagate(err, "failed to create parent directories")
	}

	// Remove the target directory if it exists (for fresh clone)
	if _, err := os.Stat(cloneDirpath); err == nil {
		if err := os.RemoveAll(cloneDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to remove existing directory")
		}
	}

	fmt.Printf("Cloning repository...\n")
	gitCmd := exec.Command("git", "clone", cloneURL, cloneDirpath)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr

	if err := gitCmd.Run(); err != nil {
		return stacktrace.Propagate(err, "git clone failed")
	}

	return nil
}

// initializeTemplateFiles creates the template files in the repository.
func initializeTemplateFiles(repoDirpath string) error {
	// Create CLAUDE.md (empty)
	claudeMdFilepath := filepath.Join(repoDirpath, "CLAUDE.md")
	if err := os.WriteFile(claudeMdFilepath, []byte{}, 0644); err != nil {
		return stacktrace.Propagate(err, "failed to create CLAUDE.md")
	}

	// Create .claude directory
	claudeDirpath := filepath.Join(repoDirpath, ".claude")
	if err := os.MkdirAll(claudeDirpath, 0755); err != nil {
		return stacktrace.Propagate(err, "failed to create .claude directory")
	}

	// Create .claude/settings.json (empty object)
	settingsFilepath := filepath.Join(claudeDirpath, "settings.json")
	if err := os.WriteFile(settingsFilepath, []byte("{}\n"), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to create .claude/settings.json")
	}

	// Create .mcp.json (empty object)
	mcpFilepath := filepath.Join(repoDirpath, ".mcp.json")
	if err := os.WriteFile(mcpFilepath, []byte("{}\n"), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to create .mcp.json")
	}

	return nil
}

// copyTemplateFiles copies template files from a source template directory to
// the destination. Copies CLAUDE.md, .claude/ directory, and .mcp.json.
func copyTemplateFiles(srcDirpath string, dstDirpath string) error {
	fmt.Printf("Copying template files from source...\n")

	// Copy CLAUDE.md if it exists
	srcClaudeMd := filepath.Join(srcDirpath, "CLAUDE.md")
	dstClaudeMd := filepath.Join(dstDirpath, "CLAUDE.md")
	if err := copyFileIfExists(srcClaudeMd, dstClaudeMd); err != nil {
		return stacktrace.Propagate(err, "failed to copy CLAUDE.md")
	}

	// Copy .claude directory if it exists
	srcClaudeDir := filepath.Join(srcDirpath, ".claude")
	dstClaudeDir := filepath.Join(dstDirpath, ".claude")
	if err := copyDirIfExists(srcClaudeDir, dstClaudeDir); err != nil {
		return stacktrace.Propagate(err, "failed to copy .claude directory")
	}

	// Copy .mcp.json if it exists
	srcMcp := filepath.Join(srcDirpath, ".mcp.json")
	dstMcp := filepath.Join(dstDirpath, ".mcp.json")
	if err := copyFileIfExists(srcMcp, dstMcp); err != nil {
		return stacktrace.Propagate(err, "failed to copy .mcp.json")
	}

	return nil
}

// copyFileIfExists copies a file from src to dst if the source file exists.
// If the source doesn't exist, it returns nil without error.
func copyFileIfExists(srcFilepath string, dstFilepath string) error {
	srcInfo, err := os.Stat(srcFilepath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return stacktrace.Propagate(err, "failed to stat source file")
	}

	content, err := os.ReadFile(srcFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read source file")
	}

	if err := os.WriteFile(dstFilepath, content, srcInfo.Mode()); err != nil {
		return stacktrace.Propagate(err, "failed to write destination file")
	}

	return nil
}

// copyDirIfExists recursively copies a directory from src to dst if the source
// directory exists. If the source doesn't exist, it returns nil without error.
func copyDirIfExists(srcDirpath string, dstDirpath string) error {
	srcInfo, err := os.Stat(srcDirpath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return stacktrace.Propagate(err, "failed to stat source directory")
	}

	if err := os.MkdirAll(dstDirpath, srcInfo.Mode()); err != nil {
		return stacktrace.Propagate(err, "failed to create destination directory")
	}

	entries, err := os.ReadDir(srcDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read source directory")
	}

	for _, entry := range entries {
		srcPath := filepath.Join(srcDirpath, entry.Name())
		dstPath := filepath.Join(dstDirpath, entry.Name())

		if entry.IsDir() {
			if err := copyDirIfExists(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFileIfExists(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// commitAndPushTemplateFiles commits and pushes the template files.
func commitAndPushTemplateFiles(repoDirpath string) error {
	// For empty repos, ensure we're on a main branch
	// Check if we have any commits yet
	checkCmd := exec.Command("git", "rev-parse", "HEAD")
	checkCmd.Dir = repoDirpath
	if err := checkCmd.Run(); err != nil {
		// No commits yet - this is an empty repo. Create the main branch.
		branchCmd := exec.Command("git", "checkout", "-b", "main")
		branchCmd.Dir = repoDirpath
		if output, err := branchCmd.CombinedOutput(); err != nil {
			return stacktrace.Propagate(err, "git checkout -b main failed: %s", strings.TrimSpace(string(output)))
		}
	}

	// Stage all files
	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = repoDirpath
	if output, err := addCmd.CombinedOutput(); err != nil {
		return stacktrace.Propagate(err, "git add failed: %s", strings.TrimSpace(string(output)))
	}

	// Commit
	commitCmd := exec.Command("git", "commit", "-m", "Initialize agent template")
	commitCmd.Dir = repoDirpath
	if output, err := commitCmd.CombinedOutput(); err != nil {
		return stacktrace.Propagate(err, "git commit failed: %s", strings.TrimSpace(string(output)))
	}

	// Push - for empty repos, push to main branch explicitly
	fmt.Println("Pushing to remote...")
	pushCmd := exec.Command("git", "push", "-u", "origin", "HEAD")
	pushCmd.Dir = repoDirpath
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return stacktrace.Propagate(err, "git push failed")
	}

	return nil
}

// promptYesNo prompts the user for a yes/no response.
func promptYesNo(prompt string) bool {
	fmt.Printf("%s [y/N]: ", prompt)

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes"
}
