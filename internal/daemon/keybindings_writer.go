package daemon

import (
	"context"
	"time"

	"github.com/odyssey/agenc/internal/tmux"
)

const (
	// keybindingsWriteInterval controls how often the daemon regenerates the
	// tmux keybindings file. This ensures that after an agenc upgrade and
	// daemon restart, keybindings stay current.
	keybindingsWriteInterval = 5 * time.Minute
)

// runKeybindingsWriterLoop writes the tmux keybindings file on startup and
// then periodically to keep it current after binary upgrades.
func (d *Daemon) runKeybindingsWriterLoop(ctx context.Context) {
	d.writeAndSourceKeybindings()

	ticker := time.NewTicker(keybindingsWriteInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.writeAndSourceKeybindings()
		}
	}
}

// writeAndSourceKeybindings regenerates the keybindings file and sources it
// into any running tmux server.
func (d *Daemon) writeAndSourceKeybindings() {
	if err := tmux.RefreshKeybindings(d.agencDirpath); err != nil {
		d.logger.Printf("Keybindings writer: %v", err)
	}
}

