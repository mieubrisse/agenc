package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/mission"
	"github.com/odyssey/agenc/internal/wrapper"
)

var agentFlag string
var promptFlag string
var worktreeFlag string

var missionNewCmd = &cobra.Command{
	Use:   "new [agent-template]",
	Short: "Create a new mission and launch claude",
	Long:  "Create a new mission with an agent template and launch claude. If the template name matches exactly, it is used directly; otherwise an interactive selector is shown.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runMissionNew,
}

func init() {
	missionNewCmd.Flags().StringVar(&agentFlag, "agent", "", "exact agent template name (for programmatic use)")
	missionNewCmd.Flags().StringVarP(&promptFlag, "prompt", "p", "", "initial prompt to send to claude")
	missionNewCmd.Flags().StringVar(&worktreeFlag, "worktree", "", "path to git repo; workspace becomes a worktree")
	missionCmd.AddCommand(missionNewCmd)
}

func runMissionNew(cmd *cobra.Command, args []string) error {
	ensureDaemonRunning(agencDirpath)

	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open database")
	}
	defer db.Close()

	templateRecords, err := db.ListAgentTemplates()
	if err != nil {
		return stacktrace.Propagate(err, "failed to list agent templates")
	}

	templates := make([]string, len(templateRecords))
	for i, t := range templateRecords {
		templates[i] = t.Repo
	}

	var agentTemplate string

	if agentFlag != "" {
		// --agent flag: must match exactly
		if !slices.Contains(templates, agentFlag) {
			return stacktrace.NewError("agent template '%s' not found", agentFlag)
		}
		agentTemplate = agentFlag
	} else if len(args) == 1 && slices.Contains(templates, args[0]) {
		// Exact match — use it directly
		agentTemplate = args[0]
	} else if len(args) == 1 && len(matchTemplatesSubstring(templates, args[0])) == 1 {
		// Single substring match — use it directly
		agentTemplate = matchTemplatesSubstring(templates, args[0])[0]
	} else if len(templates) == 0 {
		fmt.Println("No agent templates found. Proceeding without a template.")
		fmt.Printf("Install templates with: agenc template install owner/repo\n")
	} else {
		initialQuery := ""
		if len(args) == 1 {
			initialQuery = args[0]
		}
		selected, err := selectWithFzf(templates, initialQuery, true)
		if err != nil {
			return stacktrace.Propagate(err, "failed to select agent template")
		}
		if selected != "NONE" {
			agentTemplate = selected
		}
	}

	var worktreeSourceAbsDirpath string
	if worktreeFlag != "" {
		absPath, err := filepath.Abs(worktreeFlag)
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve worktree path")
		}
		worktreeSourceAbsDirpath = absPath
	}

	return createAndLaunchMission(agencDirpath, agentTemplate, promptFlag, worktreeSourceAbsDirpath)
}

// createAndLaunchMission validates the worktree (if any), creates the mission
// record and directory, and launches the wrapper process.
func createAndLaunchMission(
	agencDirpath string,
	agentTemplate string,
	prompt string,
	worktreeSourceAbsDirpath string,
) error {
	if worktreeSourceAbsDirpath != "" {
		if err := mission.ValidateWorktreeRepo(worktreeSourceAbsDirpath); err != nil {
			return stacktrace.Propagate(err, "invalid worktree repository")
		}
	}

	// Open database and create mission record
	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open database")
	}
	defer db.Close()

	missionRecord, err := db.CreateMission(agentTemplate, prompt, worktreeSourceAbsDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to create mission record")
	}

	// Validate worktree branch doesn't already exist (needs mission ID for branch name)
	if worktreeSourceAbsDirpath != "" {
		branchName := mission.GetWorktreeBranchName(missionRecord.ID)
		if err := mission.ValidateWorktreeBranch(worktreeSourceAbsDirpath, branchName); err != nil {
			// Roll back the DB record
			_ = db.DeleteMission(missionRecord.ID)
			return stacktrace.Propagate(err, "worktree branch conflict")
		}
	}

	fmt.Printf("Created mission: %s\n", missionRecord.ID)

	// Create mission directory structure
	missionDirpath, err := mission.CreateMissionDir(agencDirpath, missionRecord.ID, agentTemplate, worktreeSourceAbsDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to create mission directory")
	}

	fmt.Printf("Mission directory: %s\n", missionDirpath)
	fmt.Println("Launching claude...")

	w := wrapper.NewWrapper(agencDirpath, missionRecord.ID, agentTemplate)
	return w.Run(prompt, false)
}

func selectWithFzf(templates []string, initialQuery string, allowNone bool) (string, error) {
	var options []string
	if allowNone {
		options = append([]string{"NONE"}, templates...)
	} else {
		options = append([]string{}, templates...)
	}
	input := strings.Join(options, "\n")

	fzfBinary, err := exec.LookPath("fzf")
	if err != nil {
		return "", stacktrace.Propagate(err, "'fzf' binary not found in PATH; install fzf or pass the template name as an argument")
	}

	fzfArgs := []string{"--prompt", "Select agent template: "}
	if initialQuery != "" {
		fzfArgs = append(fzfArgs, "--query", initialQuery)
	}

	fzfCmd := exec.Command(fzfBinary, fzfArgs...)
	fzfCmd.Stdin = strings.NewReader(input)
	fzfCmd.Stderr = os.Stderr

	output, err := fzfCmd.Output()
	if err != nil {
		return "", stacktrace.Propagate(err, "fzf selection failed")
	}

	return strings.TrimSpace(string(output)), nil
}

// matchTemplatesSubstring returns template names that contain the given
// substring (case-sensitive).
func matchTemplatesSubstring(templates []string, substr string) []string {
	var matches []string
	for _, name := range templates {
		if strings.Contains(name, substr) {
			matches = append(matches, name)
		}
	}
	return matches
}

