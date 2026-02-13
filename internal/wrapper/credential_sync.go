// NOTE: The goroutines in this file (watchCredentialUpwardSync,
// watchCredentialDownwardSync, initCredentialHash) are currently disabled.
// Token file auth via CLAUDE_CODE_OAUTH_TOKEN replaces Keychain-based
// credential syncing. The code is preserved for potential future re-enablement.

package wrapper

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
)

const (
	credentialSyncInterval          = 1 * time.Minute
	credentialDownwardDebouncePeriod = 1 * time.Second
	credentialRestartStatuslineMsg   = "\U0001f504 New authentication token detected; restarting upon next idle"
)

// initCredentialHash reads the current per-mission Keychain entry and caches
// its hash. Called after cloneCredentials (at initial spawn and after restarts).
// Runs on the main goroutine, so no mutex needed (no concurrent access yet at
// init time, and during respawn the sync goroutines skip work via state check).
func (w *Wrapper) initCredentialHash() {
	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(w.agencDirpath, w.missionID)
	serviceName := claudeconfig.ComputeCredentialServiceName(claudeConfigDirpath)

	cred, err := claudeconfig.ReadKeychainCredentials(serviceName)
	if err != nil {
		w.logger.Warn("Failed to read per-mission credentials for hash init", "error", err)
		return
	}

	hash, err := claudeconfig.ComputeCredentialHash(cred)
	if err != nil {
		w.logger.Warn("Failed to compute credential hash", "error", err)
		return
	}

	w.credentialHashMu.Lock()
	w.perMissionCredentialHash = hash
	w.credentialHashMu.Unlock()
}

// watchCredentialUpwardSync polls the per-mission Keychain entry every 60
// seconds. When the credential hash changes (e.g. after /login), it merges
// the fresh credentials into the global Keychain and broadcasts the new
// expiry via the global-credentials-expiry file.
func (w *Wrapper) watchCredentialUpwardSync(ctx context.Context) {
	ticker := time.NewTicker(credentialSyncInterval)
	defer ticker.Stop()

	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(w.agencDirpath, w.missionID)
	perMissionServiceName := claudeconfig.ComputeCredentialServiceName(claudeConfigDirpath)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if w.state != stateRunning {
				continue
			}
			w.checkUpwardSync(perMissionServiceName)
		}
	}
}

// checkUpwardSync reads the per-mission Keychain, compares hash, and merges
// to global if changed. Only broadcasts the new expiry when the global
// Keychain was actually updated, to avoid spurious fsnotify events.
func (w *Wrapper) checkUpwardSync(perMissionServiceName string) {
	perMissionCred, err := claudeconfig.ReadKeychainCredentials(perMissionServiceName)
	if err != nil {
		w.logger.Warn("Upward sync: failed to read per-mission credentials", "error", err)
		return
	}

	currentHash, err := claudeconfig.ComputeCredentialHash(perMissionCred)
	if err != nil {
		w.logger.Warn("Upward sync: failed to compute credential hash", "error", err)
		return
	}

	w.credentialHashMu.Lock()
	cachedHash := w.perMissionCredentialHash
	w.credentialHashMu.Unlock()

	if currentHash == cachedHash {
		return // No change
	}

	w.logger.Info("Upward sync: per-mission credentials changed, merging to global")

	// Read global and merge
	globalCred, err := claudeconfig.ReadKeychainCredentials(claudeconfig.GlobalCredentialServiceName)
	if err != nil {
		w.logger.Warn("Upward sync: failed to read global credentials", "error", err)
		return
	}

	merged, changed, err := claudeconfig.MergeCredentialJSON([]byte(globalCred), []byte(perMissionCred))
	if err != nil {
		w.logger.Warn("Upward sync: failed to merge credentials", "error", err)
		return
	}

	// Update cached hash regardless of whether global changed — our per-mission
	// credentials DID change from the baseline.
	w.credentialHashMu.Lock()
	w.perMissionCredentialHash = currentHash
	w.credentialHashMu.Unlock()

	if !changed {
		// Per-mission changed but matches global already — no broadcast needed.
		return
	}

	if err := claudeconfig.WriteKeychainCredentials(claudeconfig.GlobalCredentialServiceName, string(merged)); err != nil {
		w.logger.Warn("Upward sync: failed to write merged credentials to global Keychain", "error", err)
		return
	}
	w.logger.Info("Propagated per-mission credential changes to global Keychain")

	// Extract expiry from the merged result (no extra Keychain read needed)
	newExpiry := claudeconfig.ExtractExpiresAtFromJSON(merged)
	if newExpiry > 0 {
		w.tokenExpiresAt = newExpiry
		// Clear stale token-expiry statusline warning immediately. Without this,
		// the user sees "token expired" for up to 60s after /login while waiting
		// for the token expiry watcher's next tick.
		w.clearStatuslineMessage()
	}

	// Broadcast the new expiry so other wrappers detect the change
	expiryFilepath := config.GetGlobalCredentialsExpiryFilepath(w.agencDirpath)
	expiryStr := fmt.Sprintf("%f", w.tokenExpiresAt)
	if err := os.WriteFile(expiryFilepath, []byte(expiryStr), 0644); err != nil {
		w.logger.Warn("Upward sync: failed to write global credentials expiry file", "error", err)
	}
}

// watchCredentialDownwardSync uses fsnotify to watch the global-credentials-expiry
// file. When another wrapper updates global credentials, this wrapper detects
// the change, pulls fresh credentials, and schedules a graceful restart.
func (w *Wrapper) watchCredentialDownwardSync(ctx context.Context) {
	expiryFilepath := config.GetGlobalCredentialsExpiryFilepath(w.agencDirpath)

	// Ensure the file exists so fsnotify has something to watch.
	// Watch the parent directory to handle file creation/deletion.
	if _, err := os.Stat(expiryFilepath); os.IsNotExist(err) {
		_ = os.WriteFile(expiryFilepath, []byte("0"), 0644)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		w.logger.Warn("Failed to create fsnotify watcher for credential expiry file", "error", err)
		return
	}
	defer watcher.Close()

	// Watch the parent directory (agencDirpath) so we catch file
	// creation/deletion, not just writes to an existing inode.
	if err := watcher.Add(w.agencDirpath); err != nil {
		w.logger.Warn("Failed to watch agenc directory for credential expiry changes", "error", err)
		return
	}

	debounceTimer := time.NewTimer(0)
	if !debounceTimer.Stop() {
		<-debounceTimer.C
	}
	timerActive := false

	for {
		select {
		case <-ctx.Done():
			if !debounceTimer.Stop() && timerActive {
				<-debounceTimer.C
			}
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Only react to writes/creates of the expiry file
			if event.Name != expiryFilepath {
				continue
			}
			if !(event.Has(fsnotify.Write) || event.Has(fsnotify.Create)) {
				continue
			}
			// Debounce: reset timer on each event, process after silence
			if !debounceTimer.Stop() && timerActive {
				<-debounceTimer.C
			}
			debounceTimer.Reset(credentialDownwardDebouncePeriod)
			timerActive = true

		case <-debounceTimer.C:
			timerActive = false
			if w.state != stateRunning {
				continue
			}
			w.handleDownwardSync(ctx, expiryFilepath)

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			w.logger.Warn("fsnotify error watching credential expiry file", "error", err)
		}
	}
}

// handleDownwardSync reads the expiry from the broadcast file, compares it
// to the cached tokenExpiresAt, and pulls fresh credentials if the file
// contains a newer expiry.
func (w *Wrapper) handleDownwardSync(ctx context.Context, expiryFilepath string) {
	data, err := os.ReadFile(expiryFilepath)
	if err != nil {
		w.logger.Warn("Downward sync: failed to read credential expiry file", "error", err)
		return
	}

	fileExpiry, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		w.logger.Warn("Downward sync: failed to parse expiry value", "error", err, "raw", string(data))
		return
	}

	if fileExpiry <= w.tokenExpiresAt {
		return // We already have current or newer credentials
	}

	w.logger.Info("Downward sync: global credentials are newer, pulling",
		"fileExpiry", fileExpiry, "cachedExpiry", w.tokenExpiresAt)

	// Read global credentials and merge into per-mission
	globalCred, err := claudeconfig.ReadKeychainCredentials(claudeconfig.GlobalCredentialServiceName)
	if err != nil {
		w.logger.Warn("Downward sync: failed to read global credentials", "error", err)
		return
	}

	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(w.agencDirpath, w.missionID)
	perMissionServiceName := claudeconfig.ComputeCredentialServiceName(claudeConfigDirpath)

	perMissionCred, err := claudeconfig.ReadKeychainCredentials(perMissionServiceName)
	if err != nil {
		w.logger.Warn("Downward sync: failed to read per-mission credentials", "error", err)
		return
	}

	merged, changed, err := claudeconfig.MergeCredentialJSON([]byte(perMissionCred), []byte(globalCred))
	if err != nil {
		w.logger.Warn("Downward sync: failed to merge credentials", "error", err)
		return
	}

	if !changed {
		// Global is newer by expiry but merge produced no diff — update cached expiry
		w.tokenExpiresAt = fileExpiry
		return
	}

	// Write merged credentials to per-mission Keychain
	if err := claudeconfig.WriteKeychainCredentials(perMissionServiceName, string(merged)); err != nil {
		w.logger.Warn("Downward sync: failed to write merged credentials to per-mission Keychain", "error", err)
		return
	}

	// Update cached state
	w.tokenExpiresAt = fileExpiry
	newHash, err := claudeconfig.ComputeCredentialHash(string(merged))
	if err == nil {
		w.credentialHashMu.Lock()
		w.perMissionCredentialHash = newHash
		w.credentialHashMu.Unlock()
	}

	// Write statusline message before sending restart command, so the user
	// sees the reason immediately.
	messageFilepath := config.GetMissionStatuslineMessageFilepath(w.agencDirpath, w.missionID)
	if err := os.WriteFile(messageFilepath, []byte(credentialRestartStatuslineMsg), 0644); err != nil {
		w.logger.Warn("Downward sync: failed to write statusline message", "error", err)
	}

	// Send a graceful restart command through the command channel so the main
	// event loop handles the state transition (avoids data races on w.state,
	// w.claudeIdle, and w.claudeCmd). Block until the command is accepted or
	// the context is cancelled — do not silently drop the restart.
	w.logger.Info("Downward sync: sending graceful restart command for fresh credentials")
	restartCmd := Command{
		Command: "restart",
		Mode:    "graceful",
		Reason:  "credentials_changed",
	}
	responseCh := make(chan Response, 1)
	select {
	case w.commandCh <- commandWithResponse{cmd: restartCmd, responseCh: responseCh}:
		resp := <-responseCh
		if resp.Status != "ok" {
			w.logger.Warn("Downward sync: restart command failed", "error", resp.Error)
		}
	case <-ctx.Done():
		return
	}
}

// clearStatuslineMessage removes the per-mission statusline message file,
// allowing the user's original statusline command to take effect again.
// The token expiry watcher will re-evaluate on its next tick and re-write
// a warning if needed.
func (w *Wrapper) clearStatuslineMessage() {
	messageFilepath := config.GetMissionStatuslineMessageFilepath(w.agencDirpath, w.missionID)
	_ = os.Remove(messageFilepath)
}
