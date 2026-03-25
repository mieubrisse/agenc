package server

import (
	"context"
	"time"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/tmux"
)

const (
	// keybindingsWriteInterval controls how often the server regenerates the
	// tmux keybindings file. This ensures that after an agenc upgrade and
	// server restart, keybindings stay current.
	keybindingsWriteInterval = 5 * time.Minute
)

// runKeybindingsWriterLoop writes the tmux keybindings file on startup and
// then periodically to keep it current after binary upgrades.
func (s *Server) runKeybindingsWriterLoop(ctx context.Context) {
	s.writeAndSourceKeybindings()

	ticker := time.NewTicker(keybindingsWriteInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.writeAndSourceKeybindings()
		}
	}
}

// writeAndSourceKeybindings regenerates the keybindings file and sources it
// into any running tmux server. In test environments (AGENC_TEST_ENV set),
// keybinding injection is skipped to avoid modifying the global tmux config.
func (s *Server) writeAndSourceKeybindings() {
	if config.IsTestEnv() {
		return
	}
	if err := tmux.RefreshKeybindings(s.agencDirpath); err != nil {
		s.logger.Printf("Keybindings writer: %v", err)
	}
}
