package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

const (
	cronSchedulerInterval = 60 * time.Second
)

// runningCronMission tracks a headless mission spawned by the cron scheduler.
type runningCronMission struct {
	cronName  string
	missionID string
	pid       int
	startedAt time.Time
}

// CronScheduler manages scheduled cron job execution.
type CronScheduler struct {
	agencDirpath string
	db           *database.DB

	mu              sync.Mutex
	runningMissions map[string]*runningCronMission // cronName -> running mission
}

// NewCronScheduler creates a new CronScheduler.
func NewCronScheduler(agencDirpath string, db *database.DB) *CronScheduler {
	return &CronScheduler{
		agencDirpath:    agencDirpath,
		db:              db,
		runningMissions: make(map[string]*runningCronMission),
	}
}

// runCronSchedulerLoop is the main scheduler goroutine. It runs every 60 seconds,
// checking for due cron jobs and spawning headless missions.
func (d *Daemon) runCronSchedulerLoop(ctx context.Context) {
	scheduler := NewCronScheduler(d.agencDirpath, d.db)

	// Adopt any orphaned headless missions from a previous daemon instance
	scheduler.adoptOrphanedMissions(d.logger)

	// Run immediately on startup, then every 60 seconds
	scheduler.runSchedulerCycle(ctx, d.logger)

	ticker := time.NewTicker(cronSchedulerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			scheduler.shutdownRunningMissions(d.logger)
			return
		case <-ticker.C:
			scheduler.runSchedulerCycle(ctx, d.logger)
		}
	}
}

// runSchedulerCycle checks all enabled cron jobs and spawns missions for due ones.
func (s *CronScheduler) runSchedulerCycle(ctx context.Context, logger logger) {
	cfg, _, err := config.ReadAgencConfig(s.agencDirpath)
	if err != nil {
		logger.Printf("Cron scheduler: failed to read config: %v", err)
		return
	}

	if len(cfg.Crons) == 0 {
		return
	}

	now := time.Now()
	maxConcurrent := cfg.GetCronsMaxConcurrent()

	// Clean up finished missions first
	s.cleanupFinishedMissions(logger)

	// Count currently running missions
	s.mu.Lock()
	runningCount := len(s.runningMissions)
	s.mu.Unlock()

	for name, cronCfg := range cfg.Crons {
		if ctx.Err() != nil {
			return
		}

		// Skip disabled crons
		if !cronCfg.IsEnabled() {
			continue
		}

		// Check if this cron is due
		if !config.IsCronDue(cronCfg.Schedule, now) {
			continue
		}

		// Check max concurrent limit
		if runningCount >= maxConcurrent {
			logger.Printf("Cron scheduler: skipping '%s' - max concurrent limit (%d) reached", name, maxConcurrent)
			continue
		}

		// Double-fire guard: check if we already spawned a mission this minute
		if s.wasSpawnedThisMinute(name, now) {
			continue
		}

		// Overlap policy: check if previous run is still running
		s.mu.Lock()
		alreadyRunning := s.runningMissions[name] != nil
		s.mu.Unlock()

		if alreadyRunning && cronCfg.GetOverlapPolicy() == config.CronOverlapSkip {
			logger.Printf("Cron scheduler: skipping '%s' - previous run still in progress (overlap=skip)", name)
			continue
		}

		// Spawn the headless mission
		logger.Printf("Cron scheduler: spawning mission for cron '%s'", name)
		if err := s.spawnCronMission(ctx, logger, name, cronCfg); err != nil {
			logger.Printf("Cron scheduler: failed to spawn mission for '%s': %v", name, err)
			continue
		}

		runningCount++
	}
}

// wasSpawnedThisMinute checks if a mission was already spawned for this cron
// in the current minute (double-fire guard).
func (s *CronScheduler) wasSpawnedThisMinute(cronName string, now time.Time) bool {
	// Check the database for a mission created in this minute
	mission, err := s.db.GetMostRecentMissionForCron(cronName)
	if err != nil || mission == nil {
		return false
	}

	// Check if the mission was created in the same minute
	missionMinute := mission.CreatedAt.Truncate(time.Minute)
	currentMinute := now.Truncate(time.Minute)
	return missionMinute.Equal(currentMinute)
}

// spawnCronMission spawns a headless mission for a cron job.
func (s *CronScheduler) spawnCronMission(ctx context.Context, logger logger, cronName string, cronCfg config.CronConfig) error {
	// Generate a unique cron ID for this run
	cronID := uuid.New().String()

	// Build the command arguments
	args := []string{
		"mission", "new",
		"--headless",
		"--prompt", cronCfg.Prompt,
		"--timeout", cronCfg.Timeout,
		"--cron-id", cronID,
		"--cron-name", cronName,
	}

	if cronCfg.Timeout == "" {
		args[5] = fmt.Sprintf("%v", config.DefaultCronTimeout)
	}

	if cronCfg.Agent != "" {
		args = append(args, "--agent", cronCfg.Agent)
	}

	// If git repo is specified, use it as the positional argument
	if cronCfg.Git != "" {
		args = append(args, cronCfg.Git)
	}

	// Get the path to the agenc binary
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	cmd := exec.CommandContext(ctx, execPath, args...)

	// Detach the process so it survives daemon restarts
	cmd.SysProcAttr = nil // Let it inherit the daemon's process group

	// Log command for debugging
	logger.Printf("Cron scheduler: running command: %s %v", execPath, args)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start mission: %w", err)
	}

	pid := cmd.Process.Pid
	logger.Printf("Cron scheduler: spawned mission for '%s' with PID %d (cron_id=%s)", cronName, pid, cronID)

	// Track the running mission
	s.mu.Lock()
	s.runningMissions[cronName] = &runningCronMission{
		cronName:  cronName,
		missionID: cronID,
		pid:       pid,
		startedAt: time.Now(),
	}
	s.mu.Unlock()

	// Detach from the process
	if err := cmd.Process.Release(); err != nil {
		logger.Printf("Cron scheduler: warning - failed to release process: %v", err)
	}

	return nil
}

// cleanupFinishedMissions removes entries for missions that have exited.
func (s *CronScheduler) cleanupFinishedMissions(logger logger) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for cronName, mission := range s.runningMissions {
		if !IsProcessRunning(mission.pid) {
			logger.Printf("Cron scheduler: mission for '%s' (PID %d) has exited", cronName, mission.pid)
			delete(s.runningMissions, cronName)
		}
	}
}

// adoptOrphanedMissions checks for missions from a previous daemon instance
// that may still be running and adopts them.
func (s *CronScheduler) adoptOrphanedMissions(logger logger) {
	// Query for missions with cron_id set that might still be running
	missions, err := s.db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		logger.Printf("Cron scheduler: failed to list missions for orphan adoption: %v", err)
		return
	}

	for _, mission := range missions {
		if mission.CronName == nil {
			continue
		}

		// Check if this mission is still running
		pidFilepath := config.GetMissionPIDFilepath(s.agencDirpath, mission.ID)
		pid, err := ReadPID(pidFilepath)
		if err != nil || pid == 0 {
			continue
		}

		if !IsProcessRunning(pid) {
			continue
		}

		cronName := *mission.CronName
		logger.Printf("Cron scheduler: adopting orphaned mission '%s' for cron '%s' (PID %d)", mission.ShortID, cronName, pid)

		s.mu.Lock()
		s.runningMissions[cronName] = &runningCronMission{
			cronName:  cronName,
			missionID: mission.ID,
			pid:       pid,
			startedAt: mission.CreatedAt,
		}
		s.mu.Unlock()
	}
}

// shutdownRunningMissions gracefully shuts down all running headless missions.
func (s *CronScheduler) shutdownRunningMissions(logger logger) {
	s.mu.Lock()
	missions := make([]*runningCronMission, 0, len(s.runningMissions))
	for _, m := range s.runningMissions {
		missions = append(missions, m)
	}
	s.mu.Unlock()

	if len(missions) == 0 {
		return
	}

	logger.Printf("Cron scheduler: shutting down %d running missions", len(missions))

	// Send SIGINT to all running missions
	for _, m := range missions {
		if !IsProcessRunning(m.pid) {
			continue
		}
		process, err := os.FindProcess(m.pid)
		if err != nil {
			continue
		}
		logger.Printf("Cron scheduler: sending SIGINT to mission '%s' (PID %d)", m.cronName, m.pid)
		process.Signal(os.Interrupt)
	}

	// Wait up to 60 seconds for graceful shutdown
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		allExited := true
		for _, m := range missions {
			if IsProcessRunning(m.pid) {
				allExited = false
				break
			}
		}
		if allExited {
			logger.Printf("Cron scheduler: all missions exited gracefully")
			return
		}
		time.Sleep(time.Second)
	}

	// Force kill any remaining processes
	for _, m := range missions {
		if !IsProcessRunning(m.pid) {
			continue
		}
		process, err := os.FindProcess(m.pid)
		if err != nil {
			continue
		}
		logger.Printf("Cron scheduler: force killing mission '%s' (PID %d)", m.cronName, m.pid)
		process.Kill()
	}
}

// GetRunningCronMissions returns a copy of the currently running cron missions.
func (s *CronScheduler) GetRunningCronMissions() []runningCronMission {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]runningCronMission, 0, len(s.runningMissions))
	for _, m := range s.runningMissions {
		result = append(result, *m)
	}
	return result
}

// logger is a minimal interface for logging that's compatible with log.Logger.
type logger interface {
	Printf(format string, v ...any)
}

// pidFileForCron returns the path to the wrapper PID file for a cron-spawned mission.
func pidFileForCron(agencDirpath string, missionID string) string {
	return config.GetMissionPIDFilepath(agencDirpath, missionID)
}

// readCronMissionPID reads the PID from a cron-spawned mission's wrapper PID file.
func readCronMissionPID(agencDirpath string, missionID string) (int, error) {
	pidFilepath := pidFileForCron(agencDirpath, missionID)
	data, err := os.ReadFile(pidFilepath)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}
