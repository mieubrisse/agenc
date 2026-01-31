package daemon

import (
	"context"
	"log"
	"sync"

	"github.com/odyssey/agenc/internal/database"
)

// Daemon runs background loops for config sync and template updates.
type Daemon struct {
	db           *database.DB
	agencDirpath string
	logger       *log.Logger
}

// NewDaemon creates a new Daemon instance.
func NewDaemon(db *database.DB, agencDirpath string, logger *log.Logger) *Daemon {
	return &Daemon{
		db:           db,
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
		d.runTemplateUpdateLoop(ctx)
	}()

	wg.Wait()
	d.logger.Println("Daemon stopping")
}

