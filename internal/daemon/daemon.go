package daemon

import (
	"context"
	"log"
	"sync"
)

// Daemon runs background loops for config sync and repo updates.
type Daemon struct {
	agencDirpath         string
	logger               *log.Logger
	repoUpdateCycleCount int
}

// NewDaemon creates a new Daemon instance.
func NewDaemon(agencDirpath string, logger *log.Logger) *Daemon {
	return &Daemon{
		agencDirpath: agencDirpath,
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
		d.runConfigSyncLoop(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runRepoUpdateLoop(ctx)
	}()

	wg.Wait()
	d.logger.Println("Daemon stopping")
}
