package cmd

import (
	"errors"
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
)

var (
	agentTemplateFlag string
	errEditorAborted  = errors.New("editor closed without saving")
)

var missionNewCmd = &cobra.Command{
	Use:   "new [prompt]",
	Short: "Create a new mission and launch claude",
	Long:  "Create a new mission, optionally selecting an agent template, and launch claude with the given prompt.",
	RunE:  runMissionNew,
}

func init() {
	missionNewCmd.Flags().StringVar(&agentTemplateFlag, "agent", "", "agent template name to use")
	missionCmd.AddCommand(missionNewCmd)
}

func runMissionNew(cmd *cobra.Command, args []string) error {
	// Idempotently start the daemon so descriptions get generated
	ensureDaemonRunning(agencDirpath)

	agentTemplate := agentTemplateFlag

	// If no --agent flag, interactively select with fzf
	if agentTemplate == "" {
		templates, err := config.ListAgentTemplates(agencDirpath)
		if err != nil {
			return stacktrace.Propagate(err, "failed to list agent templates")
		}

		if len(templates) == 0 {
			fmt.Println("No agent templates found. Proceeding without a template.")
			fmt.Printf("Create templates in: %s\n", config.GetAgentTemplatesDirpath(agencDirpath))
			agentTemplate = ""
		} else {
			selected, err := selectWithFzf(templates)
			if err != nil {
				return stacktrace.Propagate(err, "failed to select agent template")
			}
			if selected == "NONE" {
				agentTemplate = ""
			} else {
				agentTemplate = selected
			}
		}
	} else {
		// Validate the provided template exists
		templates, err := config.ListAgentTemplates(agencDirpath)
		if err != nil {
			return stacktrace.Propagate(err, "failed to list agent templates")
		}
		if !slices.Contains(templates, agentTemplate) {
			return stacktrace.NewError("agent template '%s' not found", agentTemplate)
		}
	}

	// Get prompt from args or interactively via vim
	var prompt string
	if len(args) > 0 {
		prompt = strings.Join(args, " ")
	} else {
		var err error
		prompt, err = getPromptFromEditor()
		if errors.Is(err, errEditorAborted) {
			fmt.Println("Aborted.")
			return nil
		}
		if err != nil {
			return stacktrace.Propagate(err, "failed to get prompt from editor")
		}
		if strings.TrimSpace(prompt) == "" {
			return stacktrace.NewError("prompt cannot be empty")
		}
	}

	// Open database and create mission record
	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open database")
	}
	defer db.Close()

	missionRecord, err := db.CreateMission(agentTemplate, prompt)
	if err != nil {
		return stacktrace.Propagate(err, "failed to create mission record")
	}

	fmt.Printf("Created mission: %s\n", missionRecord.ID)

	// Create mission directory structure
	missionDirpath, err := mission.CreateMissionDir(agencDirpath, missionRecord.ID, agentTemplate)
	if err != nil {
		return stacktrace.Propagate(err, "failed to create mission directory")
	}

	fmt.Printf("Mission directory: %s\n", missionDirpath)
	fmt.Println("Launching claude...")

	// Exec into claude (replaces this process)
	return mission.ExecClaude(agencDirpath, missionDirpath, prompt)
}

func selectWithFzf(templates []string) (string, error) {
	options := append([]string{"NONE"}, templates...)
	input := strings.Join(options, "\n")

	fzfBinary, err := exec.LookPath("fzf")
	if err != nil {
		return "", stacktrace.Propagate(err, "'fzf' binary not found in PATH; install fzf or use --agent flag")
	}

	fzfCmd := exec.Command(fzfBinary, "--prompt", "Select agent template: ")
	fzfCmd.Stdin = strings.NewReader(input)
	fzfCmd.Stderr = os.Stderr

	output, err := fzfCmd.Output()
	if err != nil {
		return "", stacktrace.Propagate(err, "fzf selection failed")
	}

	return strings.TrimSpace(string(output)), nil
}

func getPromptFromEditor() (string, error) {
	tmpFile, err := os.CreateTemp("", "agenc-prompt-*.md")
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to create temp file for prompt")
	}
	tmpFilepath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpFilepath)

	initialStat, err := os.Stat(tmpFilepath)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to stat temp file")
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}

	editorParts := strings.Fields(editor)
	editorBinary, err := exec.LookPath(editorParts[0])
	if err != nil {
		return "", stacktrace.Propagate(err, "'%s' not found in PATH", editorParts[0])
	}

	editorName := filepath.Base(editorParts[0])
	if editorName == "vim" || editorName == "nvim" {
		editorParts = append(editorParts, "+startinsert")
	}

	editorArgs := append(editorParts[1:], tmpFilepath)
	editorCmd := exec.Command(editorBinary, editorArgs...)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr

	if err := editorCmd.Run(); err != nil {
		return "", stacktrace.Propagate(err, "editor exited with error")
	}

	finalStat, err := os.Stat(tmpFilepath)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to stat temp file after editing")
	}

	if finalStat.ModTime().Equal(initialStat.ModTime()) {
		return "", errEditorAborted
	}

	content, err := os.ReadFile(tmpFilepath)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to read prompt file")
	}

	return string(content), nil
}
