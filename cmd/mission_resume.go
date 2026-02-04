package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/daemon"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/tableprinter"
	"github.com/odyssey/agenc/internal/wrapper"
)

var missionResumeCmd = &cobra.Command{
	Use:   "resume [mission-id]",
	Short: "Unarchive (if needed) and resume a mission with claude --continue",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runMissionResume,
}

func init() {
	missionCmd.AddCommand(missionResumeCmd)
}

func runMissionResume(cmd *cobra.Command, args []string) error {
	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open database")
	}
	defer db.Close()

	var missionID string
	if len(args) == 1 {
		missionID = args[0]
	} else {
		selected, err := selectStoppedMissionWithFzf(db)
		if err != nil {
			return err
		}
		missionID = selected
	}

	missionRecord, err := db.GetMission(missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get mission")
	}

	if missionRecord.Status == "archived" {
		if err := db.UnarchiveMission(missionID); err != nil {
			return stacktrace.Propagate(err, "failed to unarchive mission")
		}
		fmt.Printf("Unarchived mission: %s\n", missionID)
	}

	// Check if the wrapper is already running for this mission
	pidFilepath := config.GetMissionPIDFilepath(agencDirpath, missionID)
	pid, err := daemon.ReadPID(pidFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read mission PID file")
	}
	if daemon.IsProcessRunning(pid) {
		return stacktrace.NewError("mission '%s' is already running (wrapper PID %d)", missionID, pid)
	}

	// Check for old-format mission (no agent/ subdirectory)
	agentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionID)
	if _, err := os.Stat(agentDirpath); os.IsNotExist(err) {
		return stacktrace.NewError(
			"mission '%s' uses the old directory format (no agent/ subdirectory); "+
				"please archive it with 'agenc mission archive %s' and create a new mission",
			missionID, missionID,
		)
	}

	fmt.Printf("Resuming mission: %s\n", missionID)
	fmt.Println("Launching claude --continue...")

	w := wrapper.NewWrapper(agencDirpath, missionID, missionRecord.AgentTemplate, missionRecord.GitRepo, db)
	return w.Run(true)
}

// selectStoppedMissionWithFzf queries stopped missions and presents them in fzf.
// Returns the selected mission ID.
func selectStoppedMissionWithFzf(db *database.DB) (string, error) {
	missions, err := db.ListMissions(false)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to list missions")
	}

	cfg, _, cfgErr := config.ReadAgencConfig(agencDirpath)
	if cfgErr != nil {
		return "", stacktrace.Propagate(cfgErr, "failed to read config")
	}
	nicknames := buildNicknameMap(cfg.AgentTemplates)

	// Filter to stopped missions only (already ordered by created_at DESC)
	var buf bytes.Buffer
	tbl := tableprinter.NewTable("ID", "AGENT", "REPO", "PROMPT").WithWriter(&buf)
	rowCount := 0
	for _, m := range missions {
		if getMissionStatus(m.ID, m.Status) != "STOPPED" {
			continue
		}
		prompt := truncatePrompt(resolveMissionPrompt(db, agencDirpath, m), 60)
		agent := displayAgentTemplate(m.AgentTemplate, nicknames)
		repo := displayGitRepo(m.GitRepo)
		tbl.AddRow(m.ID, agent, repo, prompt)
		rowCount++
	}

	if rowCount == 0 {
		return "", stacktrace.NewError("no stopped missions to resume")
	}

	tbl.Print()

	fzfBinary, err := exec.LookPath("fzf")
	if err != nil {
		return "", stacktrace.Propagate(err, "'fzf' binary not found in PATH; install fzf or pass the mission ID as an argument")
	}

	fzfCmd := exec.Command(fzfBinary, "--ansi", "--header-lines", "1",
		"--prompt", "Select mission to resume: ")
	fzfCmd.Stdin = strings.NewReader(buf.String())
	fzfCmd.Stderr = os.Stderr

	output, err := fzfCmd.Output()
	if err != nil {
		return "", stacktrace.Propagate(err, "fzf selection failed")
	}

	selected := strings.TrimSpace(string(output))
	// Extract mission ID from the first whitespace-separated field
	missionID := strings.Fields(selected)[0]
	return missionID, nil
}
