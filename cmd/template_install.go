package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

var templateInstallCmd = &cobra.Command{
	Use:   "install owner/repo-name",
	Short: "Install an agent template from a GitHub repository",
	Args:  cobra.ExactArgs(1),
	RunE:  runTemplateInstall,
}

func init() {
	templateCmd.AddCommand(templateInstallCmd)
}

func runTemplateInstall(cmd *cobra.Command, args []string) error {
	ownerRepo := args[0]

	parts := strings.SplitN(ownerRepo, "/", 3)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return stacktrace.NewError("argument must be in the format 'owner/repo-name'")
	}

	repoName := "github.com/" + ownerRepo
	targetDirpath := config.GetRepoDirpath(agencDirpath, repoName)

	if _, err := os.Stat(targetDirpath); err == nil {
		return stacktrace.NewError("template '%s' already exists at %s", repoName, targetDirpath)
	}

	// Create intermediate directories (repos/github.com/owner/)
	if err := os.MkdirAll(targetDirpath, 0755); err != nil {
		return stacktrace.Propagate(err, "failed to create directory '%s'", targetDirpath)
	}
	// Remove the final directory so git clone can create it
	if err := os.Remove(targetDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to remove placeholder directory")
	}

	cloneURL := fmt.Sprintf("https://github.com/%s.git", ownerRepo)
	gitCmd := exec.Command("git", "clone", cloneURL, targetDirpath)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr

	if err := gitCmd.Run(); err != nil {
		return stacktrace.Propagate(err, "failed to clone repository '%s'", ownerRepo)
	}

	// Register as an agent template in the database
	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open database")
	}
	defer db.Close()

	if _, err := db.CreateAgentTemplate(repoName); err != nil {
		return stacktrace.Propagate(err, "failed to register agent template in database")
	}

	fmt.Printf("Installed template '%s' from %s\n", repoName, cloneURL)
	return nil
}
