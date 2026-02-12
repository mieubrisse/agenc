package wrapper

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/odyssey/agenc/internal/claudeconfig"
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
//
// When the cached expiry looks stale (within the warning window), re-reads
// the per-mission Keychain to check whether Claude Code has auto-refreshed
// the token. This avoids a false "token expired" warning that would persist
// for up to 60 seconds until the upward credential sync detects the change.
//
// Suppresses warnings when a restart is pending — the credential sync system
// has already scheduled a restart that will deliver fresh credentials, so the
// expiry warning is moot.
func (w *Wrapper) checkTokenExpiry(messageFilepath string) {
	if w.tokenExpiresAt == 0 {
		w.logger.Warn("Token expiry check skipped: no expiresAt timestamp available (credential may lack claudeAiOauth.expiresAt)")
		return
	}

	// If a restart is pending (credential sync scheduled it), suppress the
	// expiry warning — the restart will deliver fresh credentials.
	if w.state == stateRestartPending || w.state == stateRestarting {
		return
	}

	nowUnix := float64(time.Now().Unix())
	remaining := w.tokenExpiresAt - nowUnix

	if remaining <= tokenExpiryWarningWindow.Seconds() {
		// Before warning the user, re-read the per-mission Keychain to check
		// whether Claude Code has auto-refreshed the token. This eliminates
		// false "token expired" warnings that appear when the cached
		// tokenExpiresAt is stale but the actual credential is fresh.
		freshExpiry := w.readPerMissionExpiry()
		if freshExpiry > w.tokenExpiresAt {
			w.logger.Info("Token expiry watcher: credential refreshed in Keychain, updating cached expiry",
				"oldExpiry", w.tokenExpiresAt, "newExpiry", freshExpiry)
			w.tokenExpiresAt = freshExpiry
			remaining = freshExpiry - nowUnix
		}

		if remaining > tokenExpiryWarningWindow.Seconds() {
			// Token is now fresh after re-read — remove any stale warning
			_ = os.Remove(messageFilepath)
			return
		}

		var msg string
		if remaining <= 0 {
			msg = "\u2757 Claude Code token expired; run /login to get a new token (agenc will propagate it)"
		} else {
			msg = fmt.Sprintf("\u26a0\ufe0f  Claude Code token expires in %d minutes; run /login to get a new token (agenc will propagate it)", int(remaining/60))
		}
		w.logger.Info("Token expiry warning triggered", "remaining_seconds", remaining, "expiresAt", w.tokenExpiresAt)
		if err := os.WriteFile(messageFilepath, []byte(msg), 0644); err != nil {
			w.logger.Warn("Failed to write statusline warning", "error", err)
		}
	} else {
		// Token is fresh — remove any stale warning
		_ = os.Remove(messageFilepath)
	}
}

// readPerMissionExpiry reads the per-mission Keychain credentials and extracts
// the expiresAt timestamp. Returns 0 if the credential cannot be read or
// parsed. Used by checkTokenExpiry to verify the cached expiry before showing
// a warning.
func (w *Wrapper) readPerMissionExpiry() float64 {
	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(w.agencDirpath, w.missionID)
	serviceName := claudeconfig.ComputeCredentialServiceName(claudeConfigDirpath)

	cred, err := claudeconfig.ReadKeychainCredentials(serviceName)
	if err != nil {
		w.logger.Warn("Token expiry watcher: failed to read per-mission credentials for verification", "error", err)
		return 0
	}

	return claudeconfig.ExtractExpiresAtFromJSON([]byte(cred))
}
