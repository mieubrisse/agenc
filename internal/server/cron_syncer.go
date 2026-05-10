package server

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

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
	ListAgencCronJobs(cronPlistPrefix string) ([]string, error)
	RemoveJobByLabel(label string) error
}

// CronSyncer manages synchronization of cron jobs to launchd plists.
type CronSyncer struct {
	agencDirpath    string
	cronPlistPrefix string
	manager         launchdManager
	mu              sync.Mutex
}

// NewCronSyncer creates a new CronSyncer.
func NewCronSyncer(agencDirpath string) *CronSyncer {
	return &CronSyncer{
		agencDirpath:    agencDirpath,
		cronPlistPrefix: config.GetCronPlistPrefix(agencDirpath),
		manager:         launchd.NewManager(),
	}
}

// newCronSyncerWithManager creates a CronSyncer with a custom manager (for testing).
func newCronSyncerWithManager(agencDirpath string, manager launchdManager) *CronSyncer {
	return &CronSyncer{
		agencDirpath:    agencDirpath,
		cronPlistPrefix: config.GetCronPlistPrefix(agencDirpath),
		manager:         manager,
	}
}

// SyncCronsToLaunchd synchronizes the cron configuration to launchd plists.
// This function is idempotent and can be called on server startup and whenever
// the config changes.
//
// In test environments (AGENC_TEST_ENV=1), plist creation is skipped entirely
// to avoid polluting ~/Library/LaunchAgents with test plists.
func (s *CronSyncer) SyncCronsToLaunchd(crons map[string]config.CronConfig, logger logger) error {
	if config.IsTestEnv() {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove plists for crons that no longer exist in config (also cleans up legacy-format plists)
	if err := s.removeUnmatchedPlists(crons, logger); err != nil {
		logger.Printf("Cron syncer: warning - failed to remove unmatched plists: %v", err)
	}

	// Remove launchd jobs that have no corresponding config entry (catches phantom jobs
	// where the plist was deleted but launchd still has the job loaded)
	if err := s.removeOrphanedLaunchdJobs(crons, logger); err != nil {
		logger.Printf("Cron syncer: warning - failed to remove orphaned launchd jobs: %v", err)
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
	plistFilename := launchd.CronToPlistFilename(s.cronPlistPrefix, cronCfg.ID)
	plistPath := filepath.Join(plistDirpath, plistFilename)
	label := launchd.CronToLabel(s.cronPlistPrefix, cronCfg.ID)

	// Build the plist and render it to XML
	xmlData, err := s.buildCronPlistXML(name, cronCfg, label, execPath)
	if err != nil {
		return err
	}
	if xmlData == nil {
		return nil // unsupported schedule — already logged by buildCronPlistXML
	}

	// Compare against existing file on disk
	contentChanged := true
	existingData, err := os.ReadFile(plistPath)
	if err == nil {
		contentChanged = !bytes.Equal(xmlData, existingData)
	}
	// If ReadFile fails (file doesn't exist, permissions), treat as changed

	// Write plist atomically (temp file + rename) only if content changed
	if contentChanged {
		tmpPath := plistPath + ".tmp"
		if err := os.WriteFile(tmpPath, xmlData, 0644); err != nil {
			return stacktrace.Propagate(err, "failed to write temp plist for '%s'", name)
		}
		if err := os.Rename(tmpPath, plistPath); err != nil {
			_ = os.Remove(tmpPath)
			return stacktrace.Propagate(err, "failed to rename temp plist for '%s'", name)
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
			// Content changed and job is already loaded — unload and reload.
			// Try UnloadPlist first; if that fails, fall back to RemoveJobByLabel
			// which works even when the plist file is already replaced on disk.
			if err := s.manager.UnloadPlist(plistPath); err != nil {
				logger.Printf("Cron syncer: UnloadPlist failed for '%s', trying RemoveJobByLabel: %v", name, err)
				if err := s.manager.RemoveJobByLabel(label); err != nil {
					return stacktrace.Propagate(err, "failed to remove job '%s' during reload (both unload and remove failed)", name)
				}
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

// buildCronPlistXML constructs the launchd plist for a cron job and renders it
// to XML. Returns (nil, nil) if the schedule is unsupported (caller should skip).
func (s *CronSyncer) buildCronPlistXML(name string, cronCfg config.CronConfig, label string, execPath string) ([]byte, error) {
	calInterval, err := launchd.ParseCronExpression(cronCfg.Schedule)
	if err != nil {
		// Unsupported schedule — not an error, just skip
		return nil, nil
	}

	sourceMetadata, err := json.Marshal(map[string]string{"cron_name": name})
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal source metadata for '%s'", name)
	}

	programArgs := []string{
		execPath, "mission", "new", "--headless",
		"--source", "cron",
		"--source-id", cronCfg.ID,
		"--source-metadata", string(sourceMetadata),
		"--prompt", cronCfg.Prompt,
	}
	if cronCfg.Repo != "" {
		programArgs = append(programArgs, cronCfg.Repo)
	} else {
		programArgs = append(programArgs, "--blank")
	}

	// launchd starts processes with a minimal environment (PATH only — no HOME, no USER).
	// The agenc binary needs HOME to locate ~/.agenc/ (see config.GetAgencDirpath), so
	// without these set the binary exits with EX_CONFIG (78) before producing any output.
	envVars := map[string]string{
		"HOME": os.Getenv("HOME"),
		"USER": os.Getenv("USER"),
		"PATH": "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin",
	}
	if config.GetNamespaceSuffix(s.agencDirpath) != "" {
		envVars["AGENC_DIRPATH"] = s.agencDirpath
	}

	logFilepath := config.GetCronLogFilepath(s.agencDirpath, cronCfg.ID)

	plist := &launchd.Plist{
		Label:                 label,
		ProgramArguments:      programArgs,
		StartCalendarInterval: calInterval,
		EnvironmentVariables:  envVars,
		StandardOutPath:       logFilepath,
		StandardErrorPath:     logFilepath,
	}

	xmlData, err := plist.GeneratePlistXML()
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to generate plist XML for '%s'", name)
	}
	return xmlData, nil
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

	// Scan for current-format plists: {cronPlistPrefix}*.plist
	pattern := filepath.Join(plistDirpath, s.cronPlistPrefix+"*.plist")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return stacktrace.Propagate(err, "failed to glob plist files")
	}

	for _, plistPath := range matches {
		filename := filepath.Base(plistPath)
		cronID := strings.TrimPrefix(filename, s.cronPlistPrefix)
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
		if strings.HasPrefix(filename, s.cronPlistPrefix) {
			continue
		}
		logger.Printf("Cron syncer: removing legacy plist '%s'", filename)
		if err := s.manager.RemovePlist(plistPath); err != nil {
			logger.Printf("Cron syncer: failed to remove legacy plist '%s': %v", plistPath, err)
		}
	}

	return nil
}

// removeOrphanedLaunchdJobs checks launchd for loaded agenc cron jobs that are not
// in the current config and removes them. This catches phantom jobs that persist
// when a plist file is deleted while the job is still loaded.
func (s *CronSyncer) removeOrphanedLaunchdJobs(crons map[string]config.CronConfig, logger logger) error {
	loadedJobs, err := s.manager.ListAgencCronJobs(s.cronPlistPrefix)
	if err != nil {
		return stacktrace.Propagate(err, "failed to list loaded cron jobs")
	}

	// Build set of known cron IDs
	knownIDs := make(map[string]bool, len(crons))
	for _, cronCfg := range crons {
		if cronCfg.ID != "" {
			knownIDs[cronCfg.ID] = true
		}
	}

	for _, label := range loadedJobs {
		// Extract the ID from the label ({cronPlistPrefix}{UUID})
		cronID := strings.TrimPrefix(label, s.cronPlistPrefix)

		// Legacy labels (agenc-cron-{name}) won't match any UUID — always remove them
		if strings.HasPrefix(label, launchd.LegacyCronPlistPrefix) && !strings.HasPrefix(label, s.cronPlistPrefix) {
			logger.Printf("Cron syncer: removing orphaned legacy launchd job '%s'", label)
			if err := s.manager.RemoveJobByLabel(label); err != nil {
				logger.Printf("Cron syncer: failed to remove orphaned job '%s': %v", label, err)
			}
			continue
		}

		if !knownIDs[cronID] {
			logger.Printf("Cron syncer: removing orphaned launchd job '%s'", label)
			if err := s.manager.RemoveJobByLabel(label); err != nil {
				logger.Printf("Cron syncer: failed to remove orphaned job '%s': %v", label, err)
			}
		}
	}

	return nil
}

// logger is a minimal interface for logging that's compatible with log.Logger.
type logger interface {
	Printf(format string, v ...any)
}
