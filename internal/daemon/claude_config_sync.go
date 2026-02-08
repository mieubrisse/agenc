package daemon

import (
	"context"
	"path/filepath"
	"time"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
)

const credentialSyncInterval = 30 * time.Minute

// runCredentialSyncLoop periodically refreshes the central credentials file
// from the platform source (Keychain on macOS, file on Linux). This ensures
// that all per-mission config symlinks pick up credential changes (e.g., after
// `claude login`).
func (d *Daemon) runCredentialSyncLoop(ctx context.Context) {
	ticker := time.NewTicker(credentialSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			centralCredentialsFilepath := filepath.Join(
				config.GetGlobalClaudeDirpath(d.agencDirpath),
				".credentials.json",
			)
			if err := claudeconfig.DumpCredentials(centralCredentialsFilepath); err != nil {
				d.logger.Printf("Credential sync: failed to refresh: %v", err)
			}
		}
	}
}
