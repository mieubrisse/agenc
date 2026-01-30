package daemon

import (
	"context"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/kurtosis-tech/stacktrace"

	"github.com/odyssey/agenc/internal/database"
)

const (
	scanInterval        = 10 * time.Second
	maxPromptLen        = 2000
	descriptionSystemPrompt = "Generate a one-line summary (max 80 chars) of what this mission prompt is asking for. Output ONLY the summary, no quotes, no prefix."
)

// Daemon generates one-line mission descriptions in the background.
type Daemon struct {
	db      *database.DB
	mutexes sync.Map // map[string]*sync.Mutex
	logger  *log.Logger
}

// NewDaemon creates a new Daemon instance.
func NewDaemon(db *database.DB, logger *log.Logger) *Daemon {
	return &Daemon{
		db:     db,
		logger: logger,
	}
}

// Run starts the daemon loop. It runs an immediate cycle, then ticks every
// scanInterval until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) {
	d.logger.Println("Daemon started")

	d.runCycle(ctx)

	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.logger.Println("Daemon stopping")
			return
		case <-ticker.C:
			d.runCycle(ctx)
		}
	}
}

func (d *Daemon) runCycle(ctx context.Context) {
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
