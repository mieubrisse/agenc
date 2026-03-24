package server

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/launchd"
)

// launchdManager is the interface for launchd operations used by CronSyncer.
// Extracted from *launchd.Manager to enable testing with mocks.
type launchdManager interface {
	IsLoaded(label string) (bool, error)
	LoadPlist(plistPath string) error
	UnloadPlist(plistPath string) error
	RemovePlist(plistPath string) error
}

// CronSyncer manages synchronization of cron jobs to launchd plists.
type CronSyncer struct {
	agencDirpath string
	manager      launchdManager
}

// NewCronSyncer creates a new CronSyncer.
func NewCronSyncer(agencDirpath string) *CronSyncer {
	return &CronSyncer{
		agencDirpath: agencDirpath,
		manager:      launchd.NewManager(),
	}
}

// newCronSyncerWithManager creates a CronSyncer with a custom manager (for testing).
func newCronSyncerWithManager(agencDirpath string, manager launchdManager) *CronSyncer {
	return &CronSyncer{
		agencDirpath: agencDirpath,
		manager:      manager,
	}
}

// SyncCronsToLaunchd synchronizes the cron configuration to launchd plists.
// This function is idempotent and can be called on server startup and whenever
// the config changes.
func (s *CronSyncer) SyncCronsToLaunchd(crons map[string]config.CronConfig, logger logger) error {
	// Remove plists for crons that no longer exist in config (also cleans up legacy-format plists)
	if err := s.removeUnmatchedPlists(crons, logger); err != nil {
		logger.Printf("Cron syncer: warning - failed to remove unmatched plists: %v", err)
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
		if cronCfg.ID == "" {
			logger.Printf("Cron syncer: skipping '%s' - no ID configured (add an 'id' field to config.yml)", name)
			continue
		}

		if err := s.syncCronJob(name, cronCfg, plistDirpath, execPath, logger); err != nil {
			logger.Printf("Cron syncer: failed to sync '%s': %v", name, err)
		}
	}

	logger.Printf("Cron syncer: synced %d cron jobs to launchd", len(crons))
	return nil
}

// syncCronJob synchronizes a single cron job's plist to disk and manages its
// launchd load state. Only writes the plist file and reloads launchd when the
// generated content differs from the existing file on disk.
func (s *CronSyncer) syncCronJob(name string, cronCfg config.CronConfig, plistDirpath string, execPath string, logger logger) error {
	plistFilename := launchd.CronToPlistFilename(cronCfg.ID)
	plistPath := filepath.Join(plistDirpath, plistFilename)
	label := launchd.CronToLabel(cronCfg.ID)

	// Parse the cron schedule
	calInterval, err := launchd.ParseCronExpression(cronCfg.Schedule)
	if err != nil {
		logger.Printf("Cron syncer: skipping '%s' - unsupported schedule: %v", name, err)
		return nil
	}

	// Build the program arguments
	programArgs := []string{
		execPath,
		"mission",
		"new",
		"--headless",
		"--source", "cron",
		"--source-id", cronCfg.ID,
		"--source-metadata", fmt.Sprintf(`{"cron_name":"%s"}`, name),
		"--prompt", cronCfg.Prompt,
	}

	// Add git repo if specified, otherwise use --blank to skip the
	// interactive repo picker (which requires a terminal).
	if cronCfg.Repo != "" {
		programArgs = append(programArgs, cronCfg.Repo)
	} else {
		programArgs = append(programArgs, "--blank")
	}

	// Get log file path for this cron (single file for both stdout and stderr)
	logFilepath := config.GetCronLogFilepath(s.agencDirpath, cronCfg.ID)

	// Create the plist
	plist := &launchd.Plist{
		Label:                 label,
		ProgramArguments:      programArgs,
		StartCalendarInterval: calInterval,
		StandardOutPath:       logFilepath,
		StandardErrorPath:     logFilepath,
	}

	// Generate plist XML
	xmlData, err := plist.GeneratePlistXML()
	if err != nil {
		return stacktrace.Propagate(err, "failed to generate plist XML for '%s'", name)
	}

	// Compare against existing file on disk
	contentChanged := true
	existingData, err := os.ReadFile(plistPath)
	if err == nil {
		contentChanged = !bytes.Equal(xmlData, existingData)
	}
	// If ReadFile fails (file doesn't exist, permissions), treat as changed

	// Write plist only if content changed
	if contentChanged {
		if err := os.WriteFile(plistPath, xmlData, 0644); err != nil {
			return stacktrace.Propagate(err, "failed to write plist for '%s'", name)
		}
	}

	// Handle enabled/disabled state
	if cronCfg.IsEnabled() {
		loaded, err := s.manager.IsLoaded(label)
		if err != nil {
			return stacktrace.Propagate(err, "failed to check if '%s' is loaded", name)
		}

		if !loaded {
			if err := s.manager.LoadPlist(plistPath); err != nil {
				return stacktrace.Propagate(err, "failed to load plist for '%s'", name)
			}
			logger.Printf("Cron syncer: loaded plist for '%s'", name)
		} else if contentChanged {
			// Content changed and job is already loaded — unload and reload
			if err := s.manager.UnloadPlist(plistPath); err != nil {
				logger.Printf("Cron syncer: failed to unload plist for '%s' during reload: %v", name, err)
			}
			if err := s.manager.LoadPlist(plistPath); err != nil {
				return stacktrace.Propagate(err, "failed to reload plist for '%s'", name)
			}
			logger.Printf("Cron syncer: reloaded plist for '%s' (content changed)", name)
		}
	} else {
		// Disabled: unload if loaded, keep file
		loaded, err := s.manager.IsLoaded(label)
		if err != nil {
			return stacktrace.Propagate(err, "failed to check if '%s' is loaded", name)
		}

		if loaded {
			if err := s.manager.UnloadPlist(plistPath); err != nil {
				return stacktrace.Propagate(err, "failed to unload plist for '%s'", name)
			}
			logger.Printf("Cron syncer: unloaded plist for '%s'", name)
		}
	}

	return nil
}

// removeUnmatchedPlists removes plist files that don't correspond to any cron in the config.
// Matches by UUID extracted from the filename (agenc-cron.{UUID}.plist).
func (s *CronSyncer) removeUnmatchedPlists(crons map[string]config.CronConfig, logger logger) error {
	plistDirpath, err := launchd.PlistDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get plist directory")
	}

	// Build set of known cron IDs for fast lookup
	knownIDs := make(map[string]bool, len(crons))
	for _, cronCfg := range crons {
		if cronCfg.ID != "" {
			knownIDs[cronCfg.ID] = true
		}
	}

	// Scan for current-format plists: agenc-cron.*.plist
	pattern := filepath.Join(plistDirpath, launchd.CronPlistPrefix+"*.plist")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return stacktrace.Propagate(err, "failed to glob plist files")
	}

	for _, plistPath := range matches {
		filename := filepath.Base(plistPath)
		cronID := strings.TrimPrefix(filename, launchd.CronPlistPrefix)
		cronID = strings.TrimSuffix(cronID, ".plist")

		if !knownIDs[cronID] {
			logger.Printf("Cron syncer: removing orphaned plist for ID '%s'", cronID)
			if err := s.manager.RemovePlist(plistPath); err != nil {
				logger.Printf("Cron syncer: failed to remove plist '%s': %v", plistPath, err)
			}
		}
	}

	// Also clean up legacy-format plists: agenc-cron-*.plist
	// These were created before the UUID-based naming switch and should all be removed.
	legacyPattern := filepath.Join(plistDirpath, launchd.LegacyCronPlistPrefix+"*.plist")
	legacyMatches, err := filepath.Glob(legacyPattern)
	if err != nil {
		return stacktrace.Propagate(err, "failed to glob legacy plist files")
	}

	for _, plistPath := range legacyMatches {
		// Skip files that match the current format (agenc-cron.* starts with agenc-cron-)
		filename := filepath.Base(plistPath)
		if strings.HasPrefix(filename, launchd.CronPlistPrefix) {
			continue
		}
		logger.Printf("Cron syncer: removing legacy plist '%s'", filename)
		if err := s.manager.RemovePlist(plistPath); err != nil {
			logger.Printf("Cron syncer: failed to remove legacy plist '%s': %v", plistPath, err)
		}
	}

	return nil
}

// logger is a minimal interface for logging that's compatible with log.Logger.
type logger interface {
	Printf(format string, v ...any)
}
