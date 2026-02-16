package daemon

import (
	"context"
	"log"
	"sync"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/launchd"
)

// Daemon runs background loops for config sync and repo updates.
type Daemon struct {
	agencDirpath         string
	db                   *database.DB
	logger               *log.Logger
	repoUpdateCycleCount int
	cronSyncer           *CronSyncer
}

// NewDaemon creates a new Daemon instance.
func NewDaemon(agencDirpath string, db *database.DB, logger *log.Logger) *Daemon {
	return &Daemon{
		agencDirpath: agencDirpath,
		db:           db,
		logger:       logger,
		cronSyncer:   NewCronSyncer(agencDirpath),
	}
}

// Run starts all daemon loops. Each loop runs in its own goroutine, and Run
// blocks until ctx is cancelled and all loops have returned.
func (d *Daemon) Run(ctx context.Context) {
	d.logger.Println("Daemon started")

	// Verify launchctl is available (required for cron scheduling)
	if err := launchd.VerifyLaunchctlAvailable(); err != nil {
		d.logger.Printf("Warning: %v - cron scheduling will not work", err)
	}

	// Initial cron sync on startup
	d.syncCronsOnStartup()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runRepoUpdateLoop(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runConfigAutoCommitLoop(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runConfigWatcherLoop(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runKeybindingsWriterLoop(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runMissionSummarizerLoop(ctx)
	}()

	wg.Wait()
	d.logger.Println("Daemon stopping")
}

// syncCronsOnStartup performs an initial sync of cron jobs to launchd on daemon startup.
func (d *Daemon) syncCronsOnStartup() {
	cfg, _, err := config.ReadAgencConfig(d.agencDirpath)
	if err != nil {
		d.logger.Printf("Failed to read config on startup: %v", err)
		return
	}

	if len(cfg.Crons) == 0 {
		d.logger.Println("Cron syncer: no cron jobs configured")
		return
	}

	if err := d.cronSyncer.SyncCronsToLaunchd(cfg.Crons, d.logger); err != nil {
		d.logger.Printf("Failed to sync crons on startup: %v", err)
	}
}
