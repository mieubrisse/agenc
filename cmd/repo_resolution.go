package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"gopkg.in/yaml.v3"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

// RepoResolutionResult holds the outcome of resolving a repo input.
type RepoResolutionResult struct {
	RepoName       string // Canonical repo name (github.com/owner/repo)
	CloneDirpath   string // Path to the clone in $AGENC_DIRPATH/repos/
	WasNewlyCloned bool   // True if the repo was cloned as part of resolution
}

// ResolveRepoInput intelligently resolves user input to a canonical repo.
// The input can be:
//   - A repo reference: owner/repo, github.com/owner/repo, https://..., git@..., ssh://..., or local path
//   - A shorthand repo name (e.g., "my-repo") if logged into gh CLI or defaultGitHubUser is configured
//   - Search terms: one or more space-separated words to match against the repo library
//
// For repo references, the repo is cloned into $AGENC_DIRPATH/repos/ if not already present.
// For search terms, the repo library is searched using glob-style matching (*TERM1*TERM2*).
// If exactly one repo matches, it's auto-selected. Otherwise, the user is dropped into fzf.
//
// If fzfPrompt is empty, a default prompt is used.
func ResolveRepoInput(agencDirpath string, input string, fzfPrompt string) (*RepoResolutionResult, error) {
	// Normalize the input
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, stacktrace.NewError("empty input")
	}

	// Check if input looks like a repo reference (vs search terms)
	// looksLikeRepoReference will lazily fetch defaultGitHubUser only if needed
	if looksLikeRepoReference(agencDirpath, input) {
		// Get default GitHub user now that we know we need it
		defaultGitHubUser := getDefaultGitHubUser()
		return resolveAsRepoReference(agencDirpath, input, defaultGitHubUser)
	}

	// Treat input as search terms
	return resolveAsSearchTerms(agencDirpath, input, fzfPrompt)
}

// ResolveRepoInputs handles multiple inputs, where each input can be a repo
// reference or search terms. Returns the resolved repos in order.
func ResolveRepoInputs(agencDirpath string, inputs []string, fzfPrompt string) ([]*RepoResolutionResult, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	// If there's only one input, resolve it directly
	if len(inputs) == 1 {
		result, err := ResolveRepoInput(agencDirpath, inputs[0], fzfPrompt)
		if err != nil {
			return nil, err
		}
		return []*RepoResolutionResult{result}, nil
	}

	// Multiple inputs: first check if they should be joined as search terms
	// If the first input doesn't look like a repo reference, join all as search terms
	if !looksLikeRepoReference(agencDirpath, inputs[0]) {
		joined := strings.Join(inputs, " ")
		result, err := ResolveRepoInput(agencDirpath, joined, fzfPrompt)
		if err != nil {
			return nil, err
		}
		return []*RepoResolutionResult{result}, nil
	}

	// Each input is a separate repo reference
	var results []*RepoResolutionResult
	for _, input := range inputs {
		result, err := ResolveRepoInput(agencDirpath, input, fzfPrompt)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

// looksLikeRepoReference returns true if the input appears to be a repo
// reference rather than search terms. A repo reference is one of:
//   - A local filesystem path (starts with /, ., or ~)
//   - A Git SSH URL (starts with git@ or ssh://)
//   - An HTTPS URL (starts with https://)
//   - A shorthand reference (owner/repo or github.com/owner/repo)
//   - A single word (repo name) if defaultGitHubUser is set
//
// Search terms are characterized by:
//   - Multiple space-separated words (e.g., "my repo")
//   - A single word when defaultGitHubUser is not set
//
// This function calls getDefaultGitHubUser lazily - only when needed for
// single-word input detection, avoiding unnecessary gh CLI calls.
func looksLikeRepoReference(agencDirpath string, input string) bool {
	// Contains spaces = search terms
	if strings.Contains(input, " ") {
		return false
	}

	// Local filesystem path
	if strings.HasPrefix(input, "/") || strings.HasPrefix(input, ".") || strings.HasPrefix(input, "~") {
		return true
	}

	// Git SSH URL (git@github.com:owner/repo.git)
	if strings.HasPrefix(input, "git@") {
		return true
	}

	// SSH protocol URL (ssh://git@github.com/owner/repo.git)
	if strings.HasPrefix(input, "ssh://") {
		return true
	}

	// HTTPS URL
	if strings.HasPrefix(input, "https://") {
		return true
	}

	// HTTP URL (will be upgraded to HTTPS)
	if strings.HasPrefix(input, "http://") {
		return true
	}

	// Shorthand: owner/repo or github.com/owner/repo
	// Must have exactly 1 or 2 slashes, no spaces
	parts := strings.Split(input, "/")
	switch len(parts) {
	case 1:
		// Single word with no slashes - only a repo reference if defaultGitHubUser is set
		// LAZY: only call getDefaultGitHubUser when we actually need it
		defaultGitHubUser := getDefaultGitHubUser()
		return defaultGitHubUser != "" && input != ""
	case 2:
		// owner/repo format - both parts must be non-empty
		return parts[0] != "" && parts[1] != ""
	case 3:
		// github.com/owner/repo format - all parts must be non-empty
		return parts[0] != "" && parts[1] != "" && parts[2] != ""
	}

	return false
}

// isLocalPath returns true if the string looks like a filesystem path rather
// than a repo reference (URL or shorthand).
func isLocalPath(s string) bool {
	return strings.HasPrefix(s, "/") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "~")
}

// resolveAsRepoReference resolves input that looks like a repo reference.
// Clones the repo into $AGENC_DIRPATH/repos/ if not already present.
// If defaultGitHubUser is set and input is a single word, expands to "defaultGitHubUser/input".
func resolveAsRepoReference(agencDirpath string, input string, defaultGitHubUser string) (*RepoResolutionResult, error) {
	// Local filesystem path
	if isLocalPath(input) {
		return resolveLocalPathRepo(agencDirpath, input)
	}

	// Repo reference (URL or shorthand)
	// Pass defaultGitHubUser for bare name expansion
	return resolveRemoteRepoReference(agencDirpath, input, defaultGitHubUser)
}

// resolveLocalPathRepo handles input that's a local filesystem path to a git repo.
func resolveLocalPathRepo(agencDirpath string, localPath string) (*RepoResolutionResult, error) {
	// Expand ~ and resolve to absolute path
	if strings.HasPrefix(localPath, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to get home directory")
		}
		localPath = filepath.Join(home, localPath[1:])
	}

	absDirpath, err := filepath.Abs(localPath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to resolve path '%s'", localPath)
	}

	// Validate it's a git repo
	if err := mission.ValidateGitRepo(absDirpath); err != nil {
		return nil, stacktrace.Propagate(err, "'%s' is not a valid git repository", absDirpath)
	}

	// Extract the GitHub repo name from the origin remote
	repoName, err := mission.ExtractGitHubRepoName(absDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to extract GitHub repo name from '%s'", absDirpath)
	}

	// Get the clone URL from the local repo (preserves SSH vs HTTPS preference)
	cloneURL, err := getOriginRemoteURL(absDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to read origin remote URL")
	}

	// Ensure the repo is cloned into the library
	cloneDirpath := config.GetRepoDirpath(agencDirpath, repoName)
	wasNewlyCloned := false
	if _, statErr := os.Stat(cloneDirpath); os.IsNotExist(statErr) {
		if _, cloneErr := mission.EnsureRepoClone(agencDirpath, repoName, cloneURL); cloneErr != nil {
			return nil, stacktrace.Propagate(cloneErr, "failed to clone '%s'", repoName)
		}
		wasNewlyCloned = true
	}

	return &RepoResolutionResult{
		RepoName:       repoName,
		CloneDirpath:   cloneDirpath,
		WasNewlyCloned: wasNewlyCloned,
	}, nil
}

// resolveRemoteRepoReference handles input that's a URL or shorthand repo reference.
func resolveRemoteRepoReference(agencDirpath string, ref string, defaultOwner string) (*RepoResolutionResult, error) {
	// Determine protocol preference for shorthand references
	preferSSH, err := getProtocolPreference(agencDirpath)
	if err != nil {
		return nil, err
	}

	// Parse the reference
	repoName, cloneURL, err := mission.ParseRepoReference(ref, preferSSH, defaultOwner)
	if err != nil {
		return nil, stacktrace.Propagate(err, "invalid repo reference '%s'", ref)
	}

	// Check if already cloned
	cloneDirpath := config.GetRepoDirpath(agencDirpath, repoName)
	wasNewlyCloned := false
	if _, statErr := os.Stat(cloneDirpath); os.IsNotExist(statErr) {
		if _, cloneErr := mission.EnsureRepoClone(agencDirpath, repoName, cloneURL); cloneErr != nil {
			return nil, stacktrace.Propagate(cloneErr, "failed to clone '%s'", repoName)
		}
		wasNewlyCloned = true
	}

	return &RepoResolutionResult{
		RepoName:       repoName,
		CloneDirpath:   cloneDirpath,
		WasNewlyCloned: wasNewlyCloned,
	}, nil
}

// getProtocolPreference determines the user's preferred clone protocol (SSH vs HTTPS).
// Priority order:
//  1. gh config get git_protocol (if gh CLI is available and configured)
//  2. Infer from existing repos in the library
//  3. Prompt the user to choose
func getProtocolPreference(agencDirpath string) (bool, error) {
	// First check gh config
	if preferSSH, hasGhConfig := getGhConfigProtocol(); hasGhConfig {
		return preferSSH, nil
	}

	reposDirpath := config.GetReposDirpath(agencDirpath)

	// Check if any repos exist
	hasRepos := false
	if hosts, err := os.ReadDir(reposDirpath); err == nil {
		for _, host := range hosts {
			if !host.IsDir() {
				continue
			}
			owners, _ := os.ReadDir(filepath.Join(reposDirpath, host.Name()))
			for _, owner := range owners {
				if !owner.IsDir() {
					continue
				}
				repos, _ := os.ReadDir(filepath.Join(reposDirpath, host.Name(), owner.Name()))
				for _, repo := range repos {
					if repo.IsDir() {
						hasRepos = true
						break
					}
				}
				if hasRepos {
					break
				}
			}
			if hasRepos {
				break
			}
		}
	}

	if hasRepos {
		// Infer from existing repos
		return mission.DetectPreferredProtocol(agencDirpath), nil
	}

	// No repos exist - ask the user
	return promptForProtocolPreference()
}

// ghHostsConfig represents the structure of ~/.config/gh/hosts.yml
type ghHostsConfig struct {
	Hosts map[string]ghHostConfig `yaml:",inline"`
}

type ghHostConfig struct {
	User        string `yaml:"user"`
	GitProtocol string `yaml:"git_protocol"`
}

// getGhConfig reads and parses ~/.config/gh/hosts.yml
// Returns nil if the file doesn't exist or can't be parsed.
func getGhConfig() *ghHostsConfig {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	hostsPath := filepath.Join(homeDir, ".config", "gh", "hosts.yml")
	data, err := os.ReadFile(hostsPath)
	if err != nil {
		return nil
	}

	var config ghHostsConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil
	}

	return &config
}

// getGhConfigProtocol reads the git_protocol setting from gh config.
// Returns (preferSSH, hasConfig) where hasConfig is false if gh is not installed
// or git_protocol is not set.
func getGhConfigProtocol() (bool, bool) {
	config := getGhConfig()
	if config == nil {
		return false, false
	}

	host, ok := config.Hosts["github.com"]
	if !ok {
		return false, false
	}

	if host.GitProtocol == "" {
		return false, false
	}

	return host.GitProtocol == "ssh", true
}

// getGhLoggedInUser returns the GitHub username of the logged-in gh CLI user.
// Returns empty string if gh is not installed, not logged in, or the check fails.
// Uses a singleton pattern to only read the config file once per process.
func getGhLoggedInUser() string {
	config := getGhConfig()
	if config == nil {
		return ""
	}

	host, ok := config.Hosts["github.com"]
	if !ok {
		return ""
	}

	return host.User
}

// getDefaultGitHubUser returns the default GitHub username to use for shorthand expansion.
// Returns the gh CLI logged-in user (from ~/.config/gh/hosts.yml), or empty string if not logged in.
func getDefaultGitHubUser() string {
	return getGhLoggedInUser()
}

// promptForProtocolPreference asks the user to choose SSH or HTTPS for cloning.
func promptForProtocolPreference() (bool, error) {
	fmt.Println("No repos in your library yet. How would you like to clone repos?")
	fmt.Println("  1) SSH (git@github.com:...) - requires SSH key setup")
	fmt.Println("  2) HTTPS (https://github.com/...) - uses GitHub credentials")
	fmt.Println()
	fmt.Println("Tip: Set a persistent default with: gh config set git_protocol <ssh|https>")
	fmt.Print("\nChoice [1/2]: ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, stacktrace.Propagate(err, "failed to read input")
	}

	input = strings.TrimSpace(input)
	switch input {
	case "1", "ssh", "SSH":
		fmt.Println("Using SSH. To make this permanent: gh config set git_protocol ssh")
		return true, nil
	case "2", "https", "HTTPS", "":
		fmt.Println("Using HTTPS. To make this permanent: gh config set git_protocol https")
		return false, nil
	default:
		fmt.Println("Invalid choice, defaulting to HTTPS")
		fmt.Println("To set a default: gh config set git_protocol https")
		return false, nil
	}
}

// resolveAsSearchTerms handles input that looks like search terms.
// Searches the repo library using glob-style matching (*TERM1*TERM2*).
func resolveAsSearchTerms(agencDirpath string, input string, fzfPrompt string) (*RepoResolutionResult, error) {
	terms := strings.Fields(input)

	// Get the list of repos to search
	repos, err := findReposOnDisk(agencDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to scan repos directory")
	}

	if len(repos) == 0 {
		return nil, stacktrace.NewError("no repos in library")
	}

	// Match using sequential substring matching
	var matches []string
	for _, repo := range repos {
		if matchesSequentialSubstrings(repo, terms) {
			matches = append(matches, repo)
		}
	}

	if len(matches) == 1 {
		// Auto-select the single match
		repoName := matches[0]
		fmt.Printf("Auto-selected: %s\n", displayGitRepo(repoName))
		cloneDirpath := config.GetRepoDirpath(agencDirpath, repoName)
		return &RepoResolutionResult{
			RepoName:       repoName,
			CloneDirpath:   cloneDirpath,
			WasNewlyCloned: false,
		}, nil
	}

	// Multiple or no matches - use fzf with the search terms as initial query
	if fzfPrompt == "" {
		fzfPrompt = "Select repo: "
	}

	initialQuery := strings.Join(terms, " ")
	selected, err := selectReposWithFzfAndQuery(repos, fzfPrompt, initialQuery)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to select repo")
	}
	if len(selected) == 0 {
		return nil, stacktrace.NewError("no repo selected")
	}

	repoName := selected[0]
	cloneDirpath := config.GetRepoDirpath(agencDirpath, repoName)
	return &RepoResolutionResult{
		RepoName:       repoName,
		CloneDirpath:   cloneDirpath,
		WasNewlyCloned: false,
	}, nil
}

// getOriginRemoteURL reads the origin remote URL from a local git repo.
func getOriginRemoteURL(repoDirpath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitOperationTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = repoDirpath
	output, err := cmd.Output()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to read origin remote URL")
	}
	return strings.TrimSpace(string(output)), nil
}
