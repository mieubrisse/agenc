package daemon

import (
	"context"
	"time"

	"github.com/odyssey/agenc/internal/config"
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
	keybindingsFilepath := config.GetTmuxKeybindingsFilepath(d.agencDirpath)

	// Detect tmux version for version-gated keybindings (e.g. display-popup).
	// On error, fall back to (0, 0) — palette keybinding is omitted but all
	// other keybindings are still emitted.
	tmuxMajor, tmuxMinor, _ := tmux.DetectVersion()

	// Read config for the tmuxAgencFilepath override and palette commands.
	agencBinary := "agenc"
	var keybindings []tmux.CustomKeybinding
	if cfg, _, err := config.ReadAgencConfig(d.agencDirpath); err == nil {
		agencBinary = cfg.GetTmuxAgencBinary()
		keybindings = tmux.BuildKeybindingsFromCommands(cfg.GetResolvedPaletteCommands())
	}

	if err := tmux.WriteKeybindingsFile(keybindingsFilepath, tmuxMajor, tmuxMinor, agencBinary, keybindings); err != nil {
		d.logger.Printf("Keybindings writer: failed to write: %v", err)
		return
	}

	if err := tmux.SourceKeybindings(keybindingsFilepath); err != nil {
		d.logger.Printf("Keybindings writer: failed to source: %v", err)
	}
}

