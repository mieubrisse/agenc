package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
	"github.com/odyssey/agenc/internal/repo"
)

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
func ResolveRepoInput(agencDirpath string, input string, fzfPrompt string) (*repo.RepoResolutionResult, error) {
	// Normalize the input
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, stacktrace.NewError("empty input")
	}

	// Check if input looks like a repo reference (vs search terms)
	if repo.LooksLikeRepoReference(input) {
		defaultGitHubUser := repo.GetDefaultGitHubUser()
		return resolveAsRepoReferenceWithPrompt(agencDirpath, input, defaultGitHubUser)
	}

	// Treat input as search terms
	return resolveAsSearchTerms(agencDirpath, input, fzfPrompt)
}

// ResolveRepoInputs handles multiple inputs, where each input can be a repo
// reference or search terms. Returns the resolved repos in order.
func ResolveRepoInputs(agencDirpath string, inputs []string, fzfPrompt string) ([]*repo.RepoResolutionResult, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	// If there's only one input, resolve it directly
	if len(inputs) == 1 {
		result, err := ResolveRepoInput(agencDirpath, inputs[0], fzfPrompt)
		if err != nil {
			return nil, err
		}
		return []*repo.RepoResolutionResult{result}, nil
	}

	// Multiple inputs: first check if they should be joined as search terms
	// If the first input doesn't look like a repo reference, join all as search terms
	if !repo.LooksLikeRepoReference(inputs[0]) {
		joined := strings.Join(inputs, " ")
		result, err := ResolveRepoInput(agencDirpath, joined, fzfPrompt)
		if err != nil {
			return nil, err
		}
		return []*repo.RepoResolutionResult{result}, nil
	}

	// Each input is a separate repo reference
	var results []*repo.RepoResolutionResult
	for _, input := range inputs {
		result, err := ResolveRepoInput(agencDirpath, input, fzfPrompt)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

// resolveAsRepoReferenceWithPrompt resolves a repo reference, prompting for
// protocol preference interactively if no preference can be determined.
func resolveAsRepoReferenceWithPrompt(agencDirpath string, input string, defaultGitHubUser string) (*repo.RepoResolutionResult, error) {
	// For local paths, delegate directly (no protocol preference needed)
	if repo.IsLocalPath(input) {
		return repo.ResolveAsRepoReference(agencDirpath, input, defaultGitHubUser)
	}

	// For remote references, check if we need to prompt for protocol preference
	_, hasPreference, err := repo.GetProtocolPreference(agencDirpath)
	if err != nil {
		return nil, err
	}

	if !hasPreference {
		// Prompt the user interactively, then resolve with their choice
		preferSSH, promptErr := promptForProtocolPreference()
		if promptErr != nil {
			return nil, promptErr
		}
		// We have the user's preference now — use ParseRepoReference + clone directly
		return resolveWithProtocol(agencDirpath, input, defaultGitHubUser, preferSSH)
	}

	// Preference exists, let the library handle it
	return repo.ResolveAsRepoReference(agencDirpath, input, defaultGitHubUser)
}

// resolveWithProtocol resolves a remote repo reference with an explicit protocol preference.
func resolveWithProtocol(agencDirpath string, ref string, defaultOwner string, preferSSH bool) (*repo.RepoResolutionResult, error) {
	repoName, cloneURL, err := mission.ParseRepoReference(ref, preferSSH, defaultOwner)
	if err != nil {
		return nil, stacktrace.Propagate(err, "invalid repo reference '%s'", ref)
	}

	cloneDirpath := config.GetRepoDirpath(agencDirpath, repoName)
	wasNewlyCloned := false
	if _, statErr := os.Stat(cloneDirpath); os.IsNotExist(statErr) {
		if _, cloneErr := mission.EnsureRepoClone(agencDirpath, repoName, cloneURL); cloneErr != nil {
			return nil, stacktrace.Propagate(cloneErr, "failed to clone '%s'", repoName)
		}
		wasNewlyCloned = true
	}

	return &repo.RepoResolutionResult{
		RepoName:       repoName,
		CloneDirpath:   cloneDirpath,
		WasNewlyCloned: wasNewlyCloned,
	}, nil
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
func resolveAsSearchTerms(agencDirpath string, input string, fzfPrompt string) (*repo.RepoResolutionResult, error) {
	terms := strings.Fields(input)

	// Get the list of repos to search
	reposDirpath := config.GetReposDirpath(agencDirpath)
	repos, err := repo.FindReposOnDisk(reposDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to scan repos directory")
	}

	if len(repos) == 0 {
		return nil, stacktrace.NewError("no repos in library")
	}

	// Match using sequential substring matching
	var matches []string
	for _, r := range repos {
		if matchesSequentialSubstrings(r, terms) {
			matches = append(matches, r)
		}
	}

	if len(matches) == 1 {
		// Auto-select the single match
		repoName := matches[0]
		fmt.Printf("Auto-selected: %s\n", displayGitRepo(repoName))
		cloneDirpath := config.GetRepoDirpath(agencDirpath, repoName)
		return &repo.RepoResolutionResult{
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
	return &repo.RepoResolutionResult{
		RepoName:       repoName,
		CloneDirpath:   cloneDirpath,
		WasNewlyCloned: false,
	}, nil
}
