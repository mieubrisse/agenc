package daemon

import (
	"context"
	"log"
	"sync"

	"github.com/odyssey/agenc/internal/database"
)

// Daemon runs background loops for config sync and repo updates.
type Daemon struct {
	agencDirpath         string
	db                   *database.DB
	logger               *log.Logger
	repoUpdateCycleCount int
}

// NewDaemon creates a new Daemon instance.
func NewDaemon(agencDirpath string, db *database.DB, logger *log.Logger) *Daemon {
	return &Daemon{
		agencDirpath: agencDirpath,
		db:           db,
		logger:       logger,
	}
}

// Run starts all daemon loops. Each loop runs in its own goroutine, and Run
// blocks until ctx is cancelled and all loops have returned.
func (d *Daemon) Run(ctx context.Context) {
	d.logger.Println("Daemon started")

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
		d.runCronSchedulerLoop(ctx)
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

	wg.Wait()
	d.logger.Println("Daemon stopping")
}
