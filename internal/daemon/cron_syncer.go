package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/launchd"
)

// CronSyncer manages synchronization of cron jobs to launchd plists.
type CronSyncer struct {
	agencDirpath string
	manager      *launchd.Manager
}

// NewCronSyncer creates a new CronSyncer.
func NewCronSyncer(agencDirpath string) *CronSyncer {
	return &CronSyncer{
		agencDirpath: agencDirpath,
		manager:      launchd.NewManager(),
	}
}

// SyncCronsToLaunchd synchronizes the cron configuration to launchd plists.
// This function is idempotent and can be called on daemon startup and whenever
// the config changes.
func (s *CronSyncer) SyncCronsToLaunchd(crons map[string]config.CronConfig, logger logger) error {
	// First, reconcile orphaned plists on startup
	if err := s.reconcileOrphanedPlists(crons, logger); err != nil {
		logger.Printf("Cron syncer: warning - failed to reconcile orphaned plists: %v", err)
	}

	// Get the path to the agenc binary
	execPath, err := os.Executable()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get executable path")
	}

	plistDirpath, err := launchd.PlistDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get plist directory")
	}

	// Ensure cron log directory exists
	cronLogDir := config.GetCronLogDirpath(s.agencDirpath)
	if err := os.MkdirAll(cronLogDir, 0755); err != nil {
		return stacktrace.Propagate(err, "failed to create cron log directory")
	}

	// Process each cron job
	for name, cronCfg := range crons {
		plistFilename := launchd.CronToPlistFilename(name)
		plistPath := filepath.Join(plistDirpath, plistFilename)
		label := fmt.Sprintf("agenc-cron-%s", name)

		// Parse the cron schedule
		calInterval, err := launchd.ParseCronExpression(cronCfg.Schedule)
		if err != nil {
			logger.Printf("Cron syncer: skipping '%s' - unsupported schedule: %v", name, err)
			continue
		}

		// Build the program arguments
		programArgs := []string{
			execPath,
			"mission",
			"new",
			"--headless",
			"--cron-trigger", name,
			"--prompt", cronCfg.Prompt,
		}

		// Add timeout if specified
		if cronCfg.Timeout != "" {
			programArgs = append(programArgs, "--timeout", cronCfg.Timeout)
		}

		// Add git repo if specified
		if cronCfg.Git != "" {
			programArgs = append(programArgs, cronCfg.Git)
		}

		// Get log file paths for this cron
		stdoutPath, stderrPath := config.GetCronLogFilepaths(s.agencDirpath, name)

		// Create the plist
		plist := &launchd.Plist{
			Label:                 label,
			ProgramArguments:      programArgs,
			StartCalendarInterval: calInterval,
			StandardOutPath:       stdoutPath,
			StandardErrorPath:     stderrPath,
		}

		// Write the plist to disk
		if err := plist.WriteToDisk(plistPath); err != nil {
			logger.Printf("Cron syncer: failed to write plist for '%s': %v", name, err)
			continue
		}

		// Handle enabled/disabled state
		if cronCfg.IsEnabled() {
			// Load the plist if it's not already loaded
			loaded, err := s.manager.IsLoaded(label)
			if err != nil {
				logger.Printf("Cron syncer: failed to check if '%s' is loaded: %v", name, err)
				continue
			}

			if !loaded {
				if err := s.manager.LoadPlist(plistPath); err != nil {
					logger.Printf("Cron syncer: failed to load plist for '%s': %v", name, err)
					continue
				}
				logger.Printf("Cron syncer: loaded plist for '%s'", name)
			}
		} else {
			// Unload the plist if it's loaded (but keep the file)
			loaded, err := s.manager.IsLoaded(label)
			if err != nil {
				logger.Printf("Cron syncer: failed to check if '%s' is loaded: %v", name, err)
				continue
			}

			if loaded {
				if err := s.manager.UnloadPlist(plistPath); err != nil {
					logger.Printf("Cron syncer: failed to unload plist for '%s': %v", name, err)
					continue
				}
				logger.Printf("Cron syncer: unloaded plist for '%s'", name)
			}
		}
	}

	// Remove plists for crons that no longer exist in config
	if err := s.removeDeletedCronPlists(crons, logger); err != nil {
		logger.Printf("Cron syncer: warning - failed to remove deleted cron plists: %v", err)
	}

	logger.Printf("Cron syncer: synced %d cron jobs to launchd", len(crons))
	return nil
}

// removeUnmatchedPlists removes plist files that don't correspond to any cron in the config.
func (s *CronSyncer) removeUnmatchedPlists(crons map[string]config.CronConfig, logger logger, operation string) error {
	plistDirpath, err := launchd.PlistDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get plist directory")
	}

	// List all agenc-cron-*.plist files
	pattern := filepath.Join(plistDirpath, "agenc-cron-*.plist")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return stacktrace.Propagate(err, "failed to glob plist files")
	}

	for _, plistPath := range matches {
		// Extract the cron name from the filename
		filename := filepath.Base(plistPath)
		// Remove "agenc-cron-" prefix and ".plist" suffix
		cronName := strings.TrimPrefix(filename, "agenc-cron-")
		cronName = strings.TrimSuffix(cronName, ".plist")

		// Check if this cron exists in the config
		// Cron names are used directly without sanitization
		found := false
		for name := range crons {
			if name == cronName {
				found = true
				break
			}
		}

		if !found {
			logger.Printf("Cron syncer: %s plist for '%s'", operation, cronName)
			if err := s.manager.RemovePlist(plistPath); err != nil {
				logger.Printf("Cron syncer: failed to remove plist '%s': %v", plistPath, err)
			}
		}
	}

	return nil
}

// reconcileOrphanedPlists scans the LaunchAgents directory for agenc-cron-*.plist files
// that are not in the current config and removes them (unload + delete).
func (s *CronSyncer) reconcileOrphanedPlists(crons map[string]config.CronConfig, logger logger) error {
	return s.removeUnmatchedPlists(crons, logger, "removing orphaned")
}

// removeDeletedCronPlists removes plists for crons that no longer exist in the config.
func (s *CronSyncer) removeDeletedCronPlists(crons map[string]config.CronConfig, logger logger) error {
	return s.removeUnmatchedPlists(crons, logger, "removing deleted")
}

// logger is a minimal interface for logging that's compatible with log.Logger.
type logger interface {
	Printf(format string, v ...any)
}
