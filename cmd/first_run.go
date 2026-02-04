package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
)

// handleFirstRun checks whether this is the first time agenc is running
// (i.e. the agenc directory does not exist yet). If stdin is a TTY, it
// prompts the user to optionally clone an existing agenc-config repo
// into the config directory. If stdin is not a TTY, it silently proceeds
// with default creation.
func handleFirstRun(agencDirpath string) error {
	isFirst, err := config.IsFirstRun(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to check first-run status")
	}
	if !isFirst {
		return nil
	}

	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return nil
	}

	fmt.Println("Welcome to agenc! Setting up for the first time.")
	fmt.Println()
	fmt.Print("Do you have an existing agenc config repo to clone? [y/N] ")

	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return stacktrace.Propagate(err, "failed to read user input")
	}
	answer = strings.TrimSpace(strings.ToLower(answer))

	if answer != "y" && answer != "yes" {
		return nil
	}

	fmt.Print("Enter the repo (owner/repo, github.com/owner/repo, or full URL): ")
	repoRef, err := reader.ReadString('\n')
	if err != nil {
		return stacktrace.Propagate(err, "failed to read repo reference")
	}
	repoRef = strings.TrimSpace(repoRef)

	if repoRef == "" {
		fmt.Println("No repo provided, proceeding with default setup.")
		return nil
	}

	return cloneConfigRepo(agencDirpath, repoRef)
}

// cloneConfigRepo clones the given repo reference into the config directory.
func cloneConfigRepo(agencDirpath string, repoRef string) error {
	configDirpath := config.GetConfigDirpath(agencDirpath)

	// Ensure parent directories exist so git clone can create the config dir
	if err := os.MkdirAll(agencDirpath, 0755); err != nil {
		return stacktrace.Propagate(err, "failed to create agenc directory '%s'", agencDirpath)
	}

	// Build clone URL from the repo reference
	cloneURL, err := buildCloneURL(repoRef)
	if err != nil {
		return stacktrace.Propagate(err, "invalid repo reference")
	}

	fmt.Printf("Cloning %s into config directory...\n", cloneURL)

	gitCmd := exec.Command("git", "clone", cloneURL, configDirpath)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	if err := gitCmd.Run(); err != nil {
		return stacktrace.Propagate(err, "failed to clone config repo '%s'", cloneURL)
	}

	fmt.Println("Config repo cloned successfully.")
	return nil
}

// buildCloneURL converts a repo reference into an HTTPS clone URL.
// Accepts "owner/repo", "github.com/owner/repo", or a full URL.
func buildCloneURL(repoRef string) (string, error) {
	// If it already looks like a full URL, use it directly
	if strings.HasPrefix(repoRef, "https://") || strings.HasPrefix(repoRef, "git@") || strings.HasPrefix(repoRef, "ssh://") {
		return repoRef, nil
	}

	parts := strings.Split(repoRef, "/")
	switch len(parts) {
	case 2:
		// owner/repo
		return fmt.Sprintf("https://github.com/%s/%s.git", parts[0], parts[1]), nil
	case 3:
		// github.com/owner/repo
		if parts[0] != "github.com" {
			return "", stacktrace.NewError("unsupported host '%s'; only github.com is supported", parts[0])
		}
		return fmt.Sprintf("https://github.com/%s/%s.git", parts[1], parts[2]), nil
	default:
		return "", stacktrace.NewError("invalid repo reference '%s'; expected 'owner/repo', 'github.com/owner/repo', or a full URL", repoRef)
	}
}
