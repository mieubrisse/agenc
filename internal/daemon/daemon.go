package daemon

import (
	"context"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/kurtosis-tech/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

const (
	scanInterval           = 10 * time.Second
	configSyncInterval     = 5 * time.Minute
	maxPromptLen           = 2000
	descriptionSystemPrompt = "Generate a one-line summary (max 80 chars) of what this mission prompt is asking for. Output ONLY the summary, no quotes, no prefix."
)

// Daemon runs background loops for mission description generation and config
// repository synchronization.
type Daemon struct {
	db            *database.DB
	agencDirpath  string
	mutexes       sync.Map // map[string]*sync.Mutex
	logger        *log.Logger
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
		d.runDescriptionLoop(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runConfigSyncLoop(ctx)
	}()

	wg.Wait()
	d.logger.Println("Daemon stopping")
}

// runDescriptionLoop generates descriptions for missions that lack them.
func (d *Daemon) runDescriptionLoop(ctx context.Context) {
	d.runDescriptionCycle(ctx)

	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.runDescriptionCycle(ctx)
		}
	}
}

// runConfigSyncLoop force-pulls the latest main branch of the config repo.
func (d *Daemon) runConfigSyncLoop(ctx context.Context) {
	d.syncConfig(ctx)

	ticker := time.NewTicker(configSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.syncConfig(ctx)
		}
	}
}

func (d *Daemon) runDescriptionCycle(ctx context.Context) {
	missions, err := d.db.ListMissionsWithoutDescription()
	if err != nil {
		d.logger.Printf("Error listing undescribed missions: %v", err)
		return
	}

	for _, m := range missions {
		if ctx.Err() != nil {
			return
		}
		d.generateDescription(ctx, m)
	}
}

func (d *Daemon) generateDescription(ctx context.Context, m *database.Mission) {
	// Get or create per-mission mutex
	val, _ := d.mutexes.LoadOrStore(m.ID, &sync.Mutex{})
	mu := val.(*sync.Mutex)

	// Skip if another goroutine is already generating for this mission
	if !mu.TryLock() {
		return
	}
	defer mu.Unlock()

	// Double-check that a description hasn't been created since we queried
	existing, err := d.db.GetMissionDescription(m.ID)
	if err != nil {
		d.logger.Printf("Error checking description for mission %s: %v", m.ID, err)
		return
	}
	if existing != nil {
		return
	}

	d.logger.Printf("Generating description for mission %s", m.ID)

	prompt := m.Prompt
	if len(prompt) > maxPromptLen {
		prompt = prompt[:maxPromptLen]
	}

	description, err := d.callClaude(ctx, prompt)
	if err != nil {
		d.logger.Printf("Error calling claude for mission %s: %v", m.ID, err)
		return
	}

	description = strings.TrimSpace(description)
	if description == "" {
		d.logger.Printf("Empty description returned for mission %s", m.ID)
		return
	}

	if err := d.db.CreateMissionDescription(m.ID, description); err != nil {
		d.logger.Printf("Error storing description for mission %s: %v", m.ID, err)
		return
	}

	d.logger.Printf("Stored description for mission %s: %s", m.ID, description)
}

func (d *Daemon) callClaude(ctx context.Context, prompt string) (string, error) {
	fullPrompt := descriptionSystemPrompt + "\n\n" + prompt

	cmd := exec.CommandContext(ctx, "claude", "-p", fullPrompt, "--output-format", "text")
	output, err := cmd.Output()
	if err != nil {
		return "", stacktrace.Propagate(err, "claude command failed")
	}

	return string(output), nil
}

// syncConfig force-pulls the latest main branch of the config repository.
func (d *Daemon) syncConfig(ctx context.Context) {
	configDirpath := config.GetConfigDirpath(d.agencDirpath)

	fetchCmd := exec.CommandContext(ctx, "git", "-C", configDirpath, "fetch", "origin")
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		d.logger.Printf("Config sync: git fetch failed: %v: %s", err, string(output))
		return
	}

	resetCmd := exec.CommandContext(ctx, "git", "-C", configDirpath, "reset", "--hard", "origin/main")
	if output, err := resetCmd.CombinedOutput(); err != nil {
		d.logger.Printf("Config sync: git reset failed: %v: %s", err, string(output))
		return
	}

	d.logger.Println("Config sync: pulled latest origin/main")
}
