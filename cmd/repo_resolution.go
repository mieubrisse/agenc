package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

// RepoResolutionResult holds the outcome of resolving a repo input.
type RepoResolutionResult struct {
	RepoName      string // Canonical repo name (github.com/owner/repo)
	CloneDirpath  string // Path to the clone in ~/.agenc/repos/
	WasNewlyCloned bool   // True if the repo was cloned as part of resolution
}

// ResolveRepoInput intelligently resolves user input to a canonical repo.
// The input can be:
//   - A repo reference: owner/repo, github.com/owner/repo, https://..., git@..., ssh://..., or local path
//   - Search terms: one or more space-separated words to match against the repo library
//
// For repo references, the repo is cloned into ~/.agenc/repos/ if not already present.
// For search terms, the repo library is searched using glob-style matching (*TERM1*TERM2*).
// If exactly one repo matches, it's auto-selected. Otherwise, the user is dropped into fzf.
//
// If templateOnly is true, only agent templates are considered for search-term matching.
// If fzfPrompt is empty, a default prompt is used.
func ResolveRepoInput(agencDirpath string, input string, templateOnly bool, fzfPrompt string) (*RepoResolutionResult, error) {
	// Normalize the input
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, stacktrace.NewError("empty input")
	}

	// Check if input looks like a repo reference (vs search terms)
	if looksLikeRepoReference(input) {
		return resolveAsRepoReference(agencDirpath, input)
	}

	// Treat input as search terms
	return resolveAsSearchTerms(agencDirpath, input, templateOnly, fzfPrompt)
}

// ResolveRepoInputs handles multiple inputs, where each input can be a repo
// reference or search terms. Returns the resolved repos in order.
func ResolveRepoInputs(agencDirpath string, inputs []string, templateOnly bool, fzfPrompt string) ([]*RepoResolutionResult, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	// If there's only one input, resolve it directly
	if len(inputs) == 1 {
		result, err := ResolveRepoInput(agencDirpath, inputs[0], templateOnly, fzfPrompt)
		if err != nil {
			return nil, err
		}
		return []*RepoResolutionResult{result}, nil
	}

	// Multiple inputs: first check if they should be joined as search terms
	// If the first input doesn't look like a repo reference, join all as search terms
	if !looksLikeRepoReference(inputs[0]) {
		joined := strings.Join(inputs, " ")
		result, err := ResolveRepoInput(agencDirpath, joined, templateOnly, fzfPrompt)
		if err != nil {
			return nil, err
		}
		return []*RepoResolutionResult{result}, nil
	}

	// Each input is a separate repo reference
	var results []*RepoResolutionResult
	for _, input := range inputs {
		result, err := ResolveRepoInput(agencDirpath, input, templateOnly, fzfPrompt)
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
//
// Search terms are characterized by:
//   - Multiple space-separated words (e.g., "my repo")
//   - A single word that doesn't match repo reference patterns
func looksLikeRepoReference(input string) bool {
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
// Clones the repo into ~/.agenc/repos/ if not already present.
func resolveAsRepoReference(agencDirpath string, input string) (*RepoResolutionResult, error) {
	// Local filesystem path
	if isLocalPath(input) {
		return resolveLocalPathRepo(agencDirpath, input)
	}

	// Repo reference (URL or shorthand)
	return resolveRemoteRepoReference(agencDirpath, input)
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
func resolveRemoteRepoReference(agencDirpath string, ref string) (*RepoResolutionResult, error) {
	// Determine protocol preference for shorthand references
	preferSSH, err := getProtocolPreference(agencDirpath)
	if err != nil {
		return nil, err
	}

	// Parse the reference
	repoName, cloneURL, err := mission.ParseRepoReference(ref, preferSSH)
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
// If no repos exist in the library, prompts the user to choose.
func getProtocolPreference(agencDirpath string) (bool, error) {
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

// promptForProtocolPreference asks the user to choose SSH or HTTPS for cloning.
func promptForProtocolPreference() (bool, error) {
	fmt.Println("No repos in your library yet. How would you like to clone repos?")
	fmt.Println("  1) SSH (git@github.com:...) - requires SSH key setup")
	fmt.Println("  2) HTTPS (https://github.com/...) - uses GitHub credentials")
	fmt.Print("Choice [1/2]: ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, stacktrace.Propagate(err, "failed to read input")
	}

	input = strings.TrimSpace(input)
	switch input {
	case "1", "ssh", "SSH":
		return true, nil
	case "2", "https", "HTTPS", "":
		return false, nil
	default:
		fmt.Println("Invalid choice, defaulting to HTTPS")
		return false, nil
	}
}

// resolveAsSearchTerms handles input that looks like search terms.
// Searches the repo library using glob-style matching (*TERM1*TERM2*).
func resolveAsSearchTerms(agencDirpath string, input string, templateOnly bool, fzfPrompt string) (*RepoResolutionResult, error) {
	terms := strings.Fields(input)

	// Get the list of repos to search
	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to read config")
	}

	var repos []string
	if templateOnly {
		// Only agent templates
		for repoName := range cfg.AgentTemplates {
			repos = append(repos, repoName)
		}
	} else {
		// All repos on disk
		repos, err = findReposOnDisk(agencDirpath)
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to scan repos directory")
		}
	}

	if len(repos) == 0 {
		if templateOnly {
			return nil, stacktrace.NewError("no agent templates found")
		}
		return nil, stacktrace.NewError("no repos in library")
	}

	// Match using glob-style pattern: *term1*term2*...
	matches := matchReposGlob(repos, terms)

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

// matchReposGlob filters repos using glob-style matching (*term1*term2*...).
// Each term must appear in the repo name in order, case-insensitively.
func matchReposGlob(repos []string, terms []string) []string {
	if len(terms) == 0 {
		return repos
	}

	var matches []string
	for _, repo := range repos {
		if matchesGlobTerms(repo, terms) {
			matches = append(matches, repo)
		}
	}
	return matches
}

// matchesGlobTerms checks if repo matches the pattern *term1*term2*...
func matchesGlobTerms(repo string, terms []string) bool {
	lower := strings.ToLower(repo)
	pos := 0
	for _, term := range terms {
		termLower := strings.ToLower(term)
		idx := strings.Index(lower[pos:], termLower)
		if idx == -1 {
			return false
		}
		pos += idx + len(termLower)
	}
	return true
}

// getOriginRemoteURL reads the origin remote URL from a local git repo.
func getOriginRemoteURL(repoDirpath string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = repoDirpath
	output, err := cmd.Output()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to read origin remote URL")
	}
	return strings.TrimSpace(string(output)), nil
}
