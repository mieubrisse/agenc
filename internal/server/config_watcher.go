package server

import (
	"context"
	"path/filepath"
	"time"

	"github.com/rjeczalik/notify"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
)

const (
	// ingestDebounce is the delay after the last filesystem event before
	// triggering an ingest. This batches rapid changes (e.g., writing
	// multiple files in quick succession).
	ingestDebounce = 500 * time.Millisecond
)

// runConfigWatcherLoop initializes the shadow repo (if needed), performs an
// initial ingest from ~/.claude, then watches for ongoing changes.
// It also watches the agenc config.yml file for changes to trigger cron syncing.
func (s *Server) runConfigWatcherLoop(ctx context.Context) {
	userClaudeDirpath, err := config.GetUserClaudeDirpath()
	if err != nil {
		s.logger.Printf("Config watcher: failed to determine ~/.claude path: %v", err)
		return
	}

	// Ensure shadow repo exists (creates on first run)
	if err := claudeconfig.EnsureShadowRepo(s.agencDirpath); err != nil {
		s.logger.Printf("Config watcher: failed to initialize shadow repo: %v", err)
		return
	}

	shadowDirpath := claudeconfig.GetShadowRepoDirpath(s.agencDirpath)

	// Always ingest on startup so the shadow repo is current — EnsureShadowRepo
	// only ingests when creating a brand-new shadow repo, so restarts would
	// otherwise serve stale content until a notify event fires.
	s.ingestClaudeConfig(userClaudeDirpath, shadowDirpath)

	s.logger.Println("Config watcher: shadow repo ready, starting watch")

	s.watchBothConfigs(ctx, userClaudeDirpath, shadowDirpath)
}

// watchBothConfigs sets up notify watches for both ~/.claude and agenc config.yml.
func (s *Server) watchBothConfigs(ctx context.Context, userClaudeDirpath string, shadowDirpath string) {
	eventCh := make(chan notify.EventInfo, 256)
	// Defer-undo: if any setup step below adds an early-return path (currently
	// none do — Watch failures are logged-and-continued), the conditional Stop
	// here cleans up successful Watches that already registered against eventCh.
	// On normal completion we flip shouldCleanup and rely on the unconditional
	// Stop instead. Keep this pattern intact if you add early returns.
	shouldCleanup := true
	defer func() {
		if shouldCleanup {
			notify.Stop(eventCh)
		}
	}()

	// Watch the agenc config.yml file directly.
	agencConfigPath := config.GetConfigFilepath(s.agencDirpath)
	if err := notify.Watch(agencConfigPath, eventCh, notify.Create|notify.Write); err != nil {
		s.logger.Printf("Config watcher: failed to watch agenc config.yml: %v", err)
	}

	// Watch the ~/.claude tracked directories (recursive on macOS via FSEvents).
	s.watchTrackedDirs(eventCh, userClaudeDirpath)

	shouldCleanup = false // transfer ownership to the deferred final Stop below
	defer notify.Stop(eventCh)

	var claudeDebounceTimer *time.Timer
	var agencDebounceTimer *time.Timer

	for {
		select {
		case <-ctx.Done():
			if claudeDebounceTimer != nil {
				claudeDebounceTimer.Stop()
			}
			if agencDebounceTimer != nil {
				agencDebounceTimer.Stop()
			}
			return

		case event := <-eventCh:
			if event.Path() == agencConfigPath {
				if agencDebounceTimer != nil {
					agencDebounceTimer.Stop()
				}
				agencDebounceTimer = time.AfterFunc(ingestDebounce, func() {
					s.reloadConfig()
				})
				continue
			}

			if !isTrackedPath(event.Path(), userClaudeDirpath) {
				continue
			}

			if claudeDebounceTimer != nil {
				claudeDebounceTimer.Stop()
			}
			claudeDebounceTimer = time.AfterFunc(ingestDebounce, func() {
				s.ingestClaudeConfig(userClaudeDirpath, shadowDirpath)
				// No need to re-add watches: FSEvents-recursive picks up new dirs automatically on macOS.
			})
		}
	}
}

// watchTrackedDirs registers recursive notify.Watch calls for each tracked
// ~/.claude subdirectory (resolved through symlinks). Each Watch is best-effort
// — failure to add one directory does not block the rest. All events stream
// into the shared eventCh.
func (s *Server) watchTrackedDirs(eventCh chan<- notify.EventInfo, userClaudeDirpath string) {
	// Watch ~/.claude itself for file creates/deletes at the top level.
	_ = notify.Watch(userClaudeDirpath, eventCh, notify.All)

	for _, dirName := range claudeconfig.TrackedDirNames {
		dirpath := filepath.Join(userClaudeDirpath, dirName)
		resolved, err := filepath.EvalSymlinks(dirpath)
		if err != nil {
			continue
		}
		// Recursive watch using rjeczalik/notify's "/..." syntax.
		_ = notify.Watch(resolved+"/...", eventCh, notify.All)
	}

	// Watch directories containing tracked individual files (symlink targets).
	for _, fileName := range claudeconfig.TrackedFileNames {
		filePath := filepath.Join(userClaudeDirpath, fileName)
		resolved, err := filepath.EvalSymlinks(filePath)
		if err != nil {
			continue
		}
		_ = notify.Watch(filepath.Dir(resolved), eventCh, notify.All)
	}
}

// isTrackedPath returns true if the filesystem event path corresponds to a
// tracked file or is inside a tracked directory.
func isTrackedPath(eventPath string, userClaudeDirpath string) bool {
	// Check if it's a tracked file
	for _, fileName := range claudeconfig.TrackedFileNames {
		trackedFilepath := filepath.Join(userClaudeDirpath, fileName)
		resolved, err := filepath.EvalSymlinks(trackedFilepath)
		if err == nil && eventPath == resolved {
			return true
		}
		if eventPath == trackedFilepath {
			return true
		}
	}

	// Check if it's inside a tracked directory
	for _, dirName := range claudeconfig.TrackedDirNames {
		trackedDirpath := filepath.Join(userClaudeDirpath, dirName)
		resolved, err := filepath.EvalSymlinks(trackedDirpath)
		if err == nil {
			if isPathUnder(eventPath, resolved) {
				return true
			}
		}
		if isPathUnder(eventPath, trackedDirpath) {
			return true
		}
	}

	return false
}

// isPathUnder returns true if child is under or equal to parent.
func isPathUnder(child string, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	// rel should not start with ".." if child is under parent
	return rel != ".." && (len(rel) < 3 || rel[:3] != "../")
}

// ingestClaudeConfig runs the ingest from ~/.claude to the shadow repo.
func (s *Server) ingestClaudeConfig(userClaudeDirpath string, shadowDirpath string) {
	if err := claudeconfig.IngestFromClaudeDir(userClaudeDirpath, shadowDirpath); err != nil {
		s.logger.Printf("Config watcher: ingest failed: %v", err)
	}
}

// reloadConfig re-reads config.yml, updates the cached config, and re-syncs crons.
func (s *Server) reloadConfig() {
	cfg, _, err := config.ReadAgencConfig(s.agencDirpath)
	if err != nil {
		s.logger.Printf("Config watcher: failed to read config after change: %v", err)
		return
	}

	s.cachedConfig.Store(cfg)

	if err := s.cronSyncer.SyncCronsToLaunchd(cfg.Crons, s.logger); err != nil {
		s.logger.Printf("Config watcher: failed to sync crons: %v", err)
	}

	// Reconcile writeable copies — start watchers for newly added entries,
	// stop watchers for removed entries.
	s.reconcileWriteableCopiesFromConfig(context.Background())
}
