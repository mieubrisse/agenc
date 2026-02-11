package wrapper

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/odyssey/agenc/internal/config"
)

const (
	tokenExpiryCheckInterval = 1 * time.Minute
	tokenExpiryWarningWindow = 1 * time.Hour
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

	// Run an immediate check so expiry warnings appear at mission start,
	// not after the first ticker interval (1 minute).
	w.checkTokenExpiry(messageFilepath)

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
		w.logger.Warn("Token expiry check skipped: no expiresAt timestamp available (credential may lack claudeAiOauth.expiresAt)")
		return
	}

	nowUnix := float64(time.Now().Unix())
	remaining := w.tokenExpiresAt - nowUnix

	if remaining <= tokenExpiryWarningWindow.Seconds() {
		remainingMinutes := int(remaining / 60)
		if remainingMinutes < 0 {
			remainingMinutes = 0
		}
		msg := fmt.Sprintf("\u26a0\ufe0f  Claude Code token expires in %d minutes; run /login to avoid agenc interruptions", remainingMinutes)
		w.logger.Info("Token expiry warning triggered", "remaining_seconds", remaining, "expiresAt", w.tokenExpiresAt)
		if err := os.WriteFile(messageFilepath, []byte(msg), 0644); err != nil {
			w.logger.Warn("Failed to write statusline warning", "error", err)
		}
	} else {
		// Token is fresh — remove any stale warning
		_ = os.Remove(messageFilepath)
	}
}
