package server

import (
	"context"
	"time"

	"github.com/rjeczalik/notify"

	"github.com/odyssey/agenc/internal/config"
)

const (
	// ingestDebounce is the delay after the last filesystem event before
	// triggering a reload. This batches rapid changes (e.g., writing
	// multiple files in quick succession).
	ingestDebounce = 500 * time.Millisecond
)

// runConfigWatcherLoop watches config.yml for changes and triggers cron sync
// and writeable-copy reconciliation on every change.
func (s *Server) runConfigWatcherLoop(ctx context.Context) {
	s.logger.Println("Config watcher: starting config.yml watch")
	s.watchAgencConfig(ctx)
}

// watchAgencConfig sets up a notify watch for config.yml and calls reloadConfig
// (debounced) on every write/create event.
func (s *Server) watchAgencConfig(ctx context.Context) {
	eventCh := make(chan notify.EventInfo, 256)
	shouldCleanup := true
	defer func() {
		if shouldCleanup {
			notify.Stop(eventCh)
		}
	}()

	agencConfigPath := config.GetConfigFilepath(s.agencDirpath)
	if err := notify.Watch(agencConfigPath, eventCh, notify.Create|notify.Write); err != nil {
		s.logger.Printf("Config watcher: failed to watch agenc config.yml: %v", err)
	}

	shouldCleanup = false // transfer ownership to the deferred final Stop below
	defer notify.Stop(eventCh)

	var agencDebounceTimer *time.Timer

	for {
		select {
		case <-ctx.Done():
			if agencDebounceTimer != nil {
				agencDebounceTimer.Stop()
			}
			return

		case <-eventCh:
			if agencDebounceTimer != nil {
				agencDebounceTimer.Stop()
			}
			agencDebounceTimer = time.AfterFunc(ingestDebounce, func() {
				s.reloadConfig()
			})
		}
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
