package daemon

import (
	"context"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/database"
)

const (
	scanInterval           = 10 * time.Second
	maxPromptLen           = 2000
	descriptionSystemPrompt = "Generate a one-line summary (max 80 chars) of what this mission prompt is asking for. Output ONLY the summary, no quotes, no prefix."
)

// Daemon runs background loops for mission description generation.
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

