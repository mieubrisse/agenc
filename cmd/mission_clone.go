package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/mission"
	"github.com/odyssey/agenc/internal/session"
	"github.com/odyssey/agenc/internal/wrapper"
)

const (
	cloneIdlePollInterval = 500 * time.Millisecond
	cloneIdleTimeout      = 5 * time.Minute
)

var missionCloneCmd = &cobra.Command{
	Use:   cloneCmdStr + " <mission-id>",
	Short: "Clone a mission with its conversation history",
	Long: `Clone a mission with its conversation history.

Creates a new mission that is a full copy of the source mission's agent
directory and Claude conversation. The cloned mission inherits the same
Claude config snapshot as the source.

If the source mission is currently running, the clone waits for Claude to
reach an idle state before copying to ensure a consistent snapshot.`,
	Args: cobra.ExactArgs(1),
	RunE: runMissionClone,
}

func init() {
	missionCmd.AddCommand(missionCloneCmd)
}

func runMissionClone(cmd *cobra.Command, args []string) error {
	if _, err := getAgencContext(); err != nil {
		return err
	}
	ensureDaemonRunning(agencDirpath)

	if err := claudeconfig.EnsureShadowRepo(agencDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to ensure shadow repo")
	}

	return resolveAndRunForMission(args[0], func(db *database.DB, sourceMissionID string) error {
		sourceMission, err := db.GetMission(sourceMissionID)
		if err != nil {
			return stacktrace.Propagate(err, "failed to get source mission")
		}

		// Wait for the source mission to reach idle state if it's running
		if err := waitForMissionIdle(sourceMission); err != nil {
			return stacktrace.Propagate(err, "failed waiting for source mission to become idle")
		}

		// Use the source mission's config_commit so the clone inherits the
		// same Claude config snapshot
		createParams := &database.CreateMissionParams{}
		if sourceMission.ConfigCommit != nil {
			createParams.ConfigCommit = sourceMission.ConfigCommit
		} else if commitHash := claudeconfig.GetShadowRepoCommitHash(agencDirpath); commitHash != "" {
			createParams.ConfigCommit = &commitHash
		}

		missionRecord, err := db.CreateMission(sourceMission.GitRepo, createParams)
		if err != nil {
			return stacktrace.Propagate(err, "failed to create mission record")
		}

		fmt.Printf("Created mission: %s (cloned from %s)\n", missionRecord.ShortID, sourceMission.ShortID)

		// Track cleanup artifacts so we can roll back on failure
		cleanup := &cloneCleanup{
			db:           db,
			missionID:    missionRecord.ID,
			agencDirpath: agencDirpath,
		}
		defer cleanup.runIfNeeded()

		// Create the mission directory (but not the config — that's built
		// separately at the source's config commit)
		missionDirpath := config.GetMissionDirpath(agencDirpath, missionRecord.ID)
		if err := os.MkdirAll(missionDirpath, 0755); err != nil {
			return stacktrace.Propagate(err, "failed to create mission directory")
		}

		// Copy the source mission's agent directory into the new mission
		srcAgentDirpath := config.GetMissionAgentDirpath(agencDirpath, sourceMission.ID)
		dstAgentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionRecord.ID)
		if err := mission.CopyAgentDir(srcAgentDirpath, dstAgentDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to copy agent directory from source mission")
		}

		// Build per-mission claude config at the source mission's config commit
		configCommit := ""
		if sourceMission.ConfigCommit != nil {
			configCommit = *sourceMission.ConfigCommit
		}
		if err := claudeconfig.BuildMissionConfigDirAtCommit(agencDirpath, missionRecord.ID, configCommit); err != nil {
			return stacktrace.Propagate(err, "failed to build per-mission claude config")
		}

		// Fork the conversation history from the source mission
		hasSession, dstProjectDirpath := forkSessionHistory(sourceMission.ID, missionRecord.ID)
		cleanup.sessionProjectDirpath = dstProjectDirpath

		fmt.Printf("Mission directory: %s\n", missionDirpath)
		fmt.Println("Launching claude...")

		gitRepoName := sourceMission.GitRepo
		windowTitle := lookupWindowTitle(agencDirpath, gitRepoName)
		w := wrapper.NewWrapper(agencDirpath, missionRecord.ID, gitRepoName, windowTitle, "", db)

		// All setup succeeded — disarm cleanup before launching
		cleanup.disarm()

		// Resume with the forked conversation if one was copied, otherwise start fresh
		return w.Run(hasSession)
	})
}

// cloneCleanup tracks artifacts created during a clone operation so they can
// be removed if the clone fails partway through.
type cloneCleanup struct {
	db                     *database.DB
	missionID              string
	agencDirpath           string
	sessionProjectDirpath  string
	disarmed               bool
}

// disarm prevents cleanup from running (call on success).
func (c *cloneCleanup) disarm() {
	c.disarmed = true
}

// runIfNeeded removes all clone artifacts if the clone was not disarmed.
func (c *cloneCleanup) runIfNeeded() {
	if c.disarmed {
		return
	}

	fmt.Printf("Clone failed, cleaning up mission %s...\n", c.missionID[:8])

	// Remove mission directory (includes agent dir and claude-config)
	missionDirpath := config.GetMissionDirpath(c.agencDirpath, c.missionID)
	os.RemoveAll(missionDirpath)

	// Remove forked session project directory
	if c.sessionProjectDirpath != "" {
		os.RemoveAll(c.sessionProjectDirpath)
	}

	// Remove database record
	if err := c.db.DeleteMission(c.missionID); err != nil {
		fmt.Printf("Warning: failed to clean up mission record: %v\n", err)
	}
}

// waitForMissionIdle checks if the source mission's wrapper is running and,
// if so, polls until Claude is idle. If the wrapper is not running (stopped
// mission), returns immediately. Times out after cloneIdleTimeout.
func waitForMissionIdle(sourceMission *database.Mission) error {
	socketFilepath := config.GetMissionSocketFilepath(agencDirpath, sourceMission.ID)

	// Check if the wrapper is running by attempting the first idle query
	idle, err := wrapper.QueryIdle(socketFilepath)
	if errors.Is(err, wrapper.ErrWrapperNotRunning) {
		return nil
	}
	if err != nil {
		return stacktrace.Propagate(err, "failed to query idle state")
	}
	if idle {
		return nil
	}

	fmt.Printf("Waiting for mission %s to reach idle state...\n", sourceMission.ShortID)
	deadline := time.Now().Add(cloneIdleTimeout)

	for {
		time.Sleep(cloneIdlePollInterval)

		if time.Now().After(deadline) {
			return stacktrace.NewError(
				"timed out after %s waiting for mission %s to become idle; "+
					"try again later, or stop the mission first with '%s %s %s %s'",
				cloneIdleTimeout, sourceMission.ShortID,
				agencCmdStr, missionCmdStr, stopCmdStr, sourceMission.ShortID,
			)
		}

		idle, err = wrapper.QueryIdle(socketFilepath)
		if errors.Is(err, wrapper.ErrWrapperNotRunning) {
			return nil
		}
		if err != nil {
			return stacktrace.Propagate(err, "failed to query idle state")
		}
		if idle {
			fmt.Println("Mission is idle, cloning...")
			return nil
		}
	}
}

// forkSessionHistory copies the latest session from the source mission's
// project directory into the new mission's project directory with a new
// session UUID. Returns true if a session was successfully forked, and the
// destination project directory path (for cleanup on failure).
func forkSessionHistory(sourceMissionID string, newMissionID string) (bool, string) {
	srcProjectDirpath, err := session.FindProjectDirpath(sourceMissionID)
	if err != nil {
		fmt.Printf("Note: no conversation history found to clone (source has no sessions)\n")
		return false, ""
	}

	srcSessionID, err := session.FindLatestSessionID(srcProjectDirpath)
	if err != nil {
		fmt.Printf("Note: no conversation history found to clone (no session files)\n")
		return false, ""
	}

	// Compute the destination project directory name by encoding the new mission's agent path
	newAgentDirpath := config.GetMissionAgentDirpath(agencDirpath, newMissionID)
	dstProjectDirname := session.EncodeProjectDirname(newAgentDirpath)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Warning: failed to determine home directory for session copy: %v\n", err)
		return false, ""
	}
	dstProjectDirpath := filepath.Join(homeDir, ".claude", "projects", dstProjectDirname)

	newSessionID := uuid.New().String()

	if err := session.CopyAndForkSession(srcProjectDirpath, dstProjectDirpath, srcSessionID, newSessionID); err != nil {
		fmt.Printf("Warning: failed to copy conversation history: %v\n", err)
		return false, dstProjectDirpath
	}

	fmt.Printf("Forked conversation history (session %s → %s)\n", srcSessionID[:8], newSessionID[:8])
	return true, dstProjectDirpath
}
