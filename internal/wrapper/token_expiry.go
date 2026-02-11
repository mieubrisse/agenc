package wrapper

import (
	"context"
	"os"
	"time"

	"github.com/odyssey/agenc/internal/config"
)

const (
	tokenExpiryCheckInterval = 1 * time.Minute
	tokenExpiryWarningWindow = 1 * time.Hour
	tokenExpiryWarningMsg    = "\u26a0\ufe0f  Claude token expires soon \u2014 run 'agenc login' to refresh"
)

// watchTokenExpiry periodically checks whether the stored token expiry
// timestamp is within the warning window. When it is, writes a warning
// message to the per-mission statusline message file. When the token is
// fresh (e.g. after a restart that cloned new credentials), removes the
// file so the user's original statusline returns.
func (w *Wrapper) watchTokenExpiry(ctx context.Context) {
	ticker := time.NewTicker(tokenExpiryCheckInterval)
	defer ticker.Stop()

	messageFilepath := config.GetMissionStatuslineMessageFilepath(w.agencDirpath, w.missionID)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.checkTokenExpiry(messageFilepath)
		}
	}
}

// checkTokenExpiry compares the stored token expiry timestamp against the
// current time. If within the warning window, writes the warning message.
// Otherwise, removes the message file (if it exists).
func (w *Wrapper) checkTokenExpiry(messageFilepath string) {
	if w.tokenExpiresAt == 0 {
		// No expiry timestamp available — nothing to warn about
		return
	}

	nowUnix := float64(time.Now().Unix())
	remaining := w.tokenExpiresAt - nowUnix

	if remaining <= tokenExpiryWarningWindow.Seconds() {
		if err := os.WriteFile(messageFilepath, []byte(tokenExpiryWarningMsg), 0644); err != nil {
			w.logger.Warn("Failed to write statusline warning", "error", err)
		}
	} else {
		// Token is fresh — remove any stale warning
		_ = os.Remove(messageFilepath)
	}
}
