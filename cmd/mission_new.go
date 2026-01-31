package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/mission"
	"github.com/odyssey/agenc/internal/wrapper"
)

var missionNewCmd = &cobra.Command{
	Use:   "new [agent-template]",
	Short: "Create a new mission and launch claude",
	Long:  "Create a new mission with an agent template and launch claude. If the template name matches exactly, it is used directly; otherwise an interactive selector is shown.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runMissionNew,
}

func init() {
	missionCmd.AddCommand(missionNewCmd)
}

func runMissionNew(cmd *cobra.Command, args []string) error {
	// Idempotently start the daemon so descriptions get generated
	ensureDaemonRunning(agencDirpath)

	templates, err := config.ListAgentTemplates(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to list agent templates")
	}

	var agentTemplate string

	if len(args) == 1 && slices.Contains(templates, args[0]) {
		// Exact match — use it directly
		agentTemplate = args[0]
	} else if len(templates) == 0 {
		fmt.Println("No agent templates found. Proceeding without a template.")
		fmt.Printf("Create templates in: %s\n", config.GetAgentTemplatesDirpath(agencDirpath))
	} else {
		initialQuery := ""
		if len(args) == 1 {
			initialQuery = args[0]
		}
		selected, err := selectWithFzf(templates, initialQuery)
		if err != nil {
			return stacktrace.Propagate(err, "failed to select agent template")
		}
		if selected != "NONE" {
			agentTemplate = selected
		}
	}

	// Open database and create mission record
	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open database")
	}
	defer db.Close()

	missionRecord, err := db.CreateMission(agentTemplate, "")
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

	w := wrapper.NewWrapper(agencDirpath, missionRecord.ID, agentTemplate)
	return w.Run("", false)
}

func selectWithFzf(templates []string, initialQuery string) (string, error) {
	options := append([]string{"NONE"}, templates...)
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

