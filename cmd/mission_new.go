package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
var embeddedAgentFlag bool

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
	missionNewCmd.Flags().BoolVar(&embeddedAgentFlag, "embedded-agent", false, "use agent config from the worktree repo instead of a template")
	missionCmd.AddCommand(missionNewCmd)
}

func runMissionNew(cmd *cobra.Command, args []string) error {
	ensureDaemonRunning(agencDirpath)

	// Validate --embedded-agent flag constraints
	if embeddedAgentFlag {
		if worktreeFlag == "" {
			return stacktrace.NewError("--worktree is required when using --embedded-agent")
		}
		if agentFlag != "" {
			return stacktrace.NewError("--embedded-agent and --agent are mutually exclusive")
		}
		if len(args) > 0 {
			return stacktrace.NewError("--embedded-agent and positional [agent-template] argument are mutually exclusive")
		}
	}

	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open database")
	}
	defer db.Close()

	var agentTemplate string

	if embeddedAgentFlag {
		// Skip template selection entirely for embedded-agent missions
	} else {
		templateRecords, err := db.ListAgentTemplates()
		if err != nil {
			return stacktrace.Propagate(err, "failed to list agent templates")
		}

		if agentFlag != "" {
			// --agent flag: match by repo or nickname
			resolved, resolveErr := resolveTemplate(templateRecords, agentFlag)
			if resolveErr != nil {
				return stacktrace.NewError("agent template '%s' not found", agentFlag)
			}
			agentTemplate = resolved
		} else if len(templateRecords) == 0 {
			fmt.Println("No agent templates found. Proceeding without a template.")
			fmt.Printf("Install templates with: agenc template install owner/repo\n")
		} else if len(args) == 1 {
			resolved, resolveErr := resolveTemplate(templateRecords, args[0])
			if resolveErr != nil {
				// No match found — fall through to fzf with initial query
				selected, fzfErr := selectWithFzf(templateRecords, args[0], true)
				if fzfErr != nil {
					return stacktrace.Propagate(fzfErr, "failed to select agent template")
				}
				if selected != "" {
					agentTemplate = selected
				}
			} else {
				agentTemplate = resolved
			}
		} else {
			selected, fzfErr := selectWithFzf(templateRecords, "", true)
			if fzfErr != nil {
				return stacktrace.Propagate(fzfErr, "failed to select agent template")
			}
			if selected != "" {
				agentTemplate = selected
			}
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

	return createAndLaunchMission(agencDirpath, agentTemplate, promptFlag, worktreeSourceAbsDirpath, embeddedAgentFlag)
}

// createAndLaunchMission validates the worktree (if any), creates the mission
// record and directory, and launches the wrapper process.
func createAndLaunchMission(
	agencDirpath string,
	agentTemplate string,
	prompt string,
	worktreeSourceAbsDirpath string,
	embeddedAgent bool,
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

	missionRecord, err := db.CreateMission(agentTemplate, prompt, worktreeSourceAbsDirpath, embeddedAgent)
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
	var missionDirpath string
	if embeddedAgent {
		missionDirpath, err = mission.CreateEmbeddedAgentMissionDir(agencDirpath, missionRecord.ID, worktreeSourceAbsDirpath)
	} else {
		missionDirpath, err = mission.CreateMissionDir(agencDirpath, missionRecord.ID, agentTemplate, worktreeSourceAbsDirpath)
	}
	if err != nil {
		return stacktrace.Propagate(err, "failed to create mission directory")
	}

	fmt.Printf("Mission directory: %s\n", missionDirpath)
	fmt.Println("Launching claude...")

	w := wrapper.NewWrapper(agencDirpath, missionRecord.ID, agentTemplate, embeddedAgent)
	return w.Run(prompt, false)
}

// selectWithFzf presents templates in fzf and returns the selected repo name.
// If allowNone is true, a "NONE" option is prepended. Returns empty string if
// NONE is selected.
func selectWithFzf(templates []*database.AgentTemplate, initialQuery string, allowNone bool) (string, error) {
	var lines []string
	if allowNone {
		lines = append(lines, "NONE")
	}
	for _, t := range templates {
		lines = append(lines, formatTemplateFzfLine(t))
	}
	input := strings.Join(lines, "\n")

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

	selected := strings.TrimSpace(string(output))
	if selected == "NONE" {
		return "", nil
	}
	return extractRepoFromFzfLine(selected), nil
}

// matchTemplatesSubstring returns templates whose Repo or Nickname contain the
// given substring (case-sensitive).
func matchTemplatesSubstring(templates []*database.AgentTemplate, substr string) []*database.AgentTemplate {
	var matches []*database.AgentTemplate
	for _, t := range templates {
		if strings.Contains(t.Repo, substr) || strings.Contains(t.Nickname, substr) {
			matches = append(matches, t)
		}
	}
	return matches
}

// resolveTemplate attempts to find exactly one template matching the given
// query. It tries exact match on repo, then exact match on nickname, then
// single substring match on either field.
func resolveTemplate(templates []*database.AgentTemplate, query string) (string, error) {
	// Exact match by repo
	for _, t := range templates {
		if t.Repo == query {
			return t.Repo, nil
		}
	}
	// Exact match by nickname
	for _, t := range templates {
		if t.Nickname == query {
			return t.Repo, nil
		}
	}
	// Single substring match
	matches := matchTemplatesSubstring(templates, query)
	if len(matches) == 1 {
		return matches[0].Repo, nil
	}
	return "", stacktrace.NewError("no unique template match for '%s'", query)
}

