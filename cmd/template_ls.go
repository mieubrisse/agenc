package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var templateLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List installed agent templates",
	RunE:  runTemplateLs,
}

func init() {
	templateCmd.AddCommand(templateLsCmd)
}

func runTemplateLs(cmd *cobra.Command, args []string) error {
	templateNames, err := config.ListAgentTemplates(agencDirpath)
	if err != nil {
		return err
	}

	if len(templateNames) == 0 {
		fmt.Println("No agent templates installed.")
		return nil
	}

	templatesDirpath := config.GetAgentTemplatesDirpath(agencDirpath)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tREPO")
	for _, name := range templateNames {
		repoURL := getGitRepoURL(filepath.Join(templatesDirpath, name))
		fmt.Fprintf(w, "%s\t%s\n", name, repoURL)
	}
	w.Flush()

	return nil
}

// getGitRepoURL returns the HTTPS URL of the origin remote for the git repo
// at the given path, or "(unknown)" if it cannot be determined.
func getGitRepoURL(repoDirpath string) string {
	gitCmd := exec.Command("git", "-C", repoDirpath, "remote", "get-url", "origin")
	output, err := gitCmd.Output()
	if err != nil {
		return "(unknown)"
	}

	rawURL := strings.TrimSpace(string(output))
	return normalizeGitURL(rawURL)
}

// normalizeGitURL converts a git remote URL (SSH or HTTPS) into a clean HTTPS URL.
func normalizeGitURL(rawURL string) string {
	// Handle SSH format: git@github.com:user/repo.git
	if strings.HasPrefix(rawURL, "git@") {
		rawURL = strings.TrimPrefix(rawURL, "git@")
		rawURL = strings.Replace(rawURL, ":", "/", 1)
		rawURL = "https://" + rawURL
	}

	rawURL = strings.TrimSuffix(rawURL, ".git")
	return rawURL
}
