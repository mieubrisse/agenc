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
	credentialSyncInterval           = 1 * time.Minute
	credentialDownwardDebouncePeriod = 1 * time.Second
)

// initCredentialHash reads the current per-mission Keychain entry and caches
// its hash. Called after cloneCredentials (at initial spawn and after restarts).
// Runs on the main goroutine before the sync goroutines start, so no mutex
// needed at init time.
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
// seconds. When the credential hash changes (e.g. after an MCP OAuth flow),
// it merges the fresh credentials into the global Keychain and broadcasts the
// change via the global-credentials-expiry file so other running missions can
// pull the update.
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

// checkUpwardSync reads the per-mission Keychain, compares its hash to the
// cached value, and merges to global if changed. Only broadcasts when the
// global Keychain was actually updated, to avoid spurious fsnotify events in
// other missions' downward sync watchers.
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
		// Per-mission changed but already matches global — no broadcast needed.
		return
	}

	if err := claudeconfig.WriteKeychainCredentials(claudeconfig.GlobalCredentialServiceName, string(merged)); err != nil {
		w.logger.Warn("Upward sync: failed to write merged credentials to global Keychain", "error", err)
		return
	}
	w.logger.Info("Upward sync: propagated per-mission credential changes to global Keychain")

	// Broadcast using the current Unix timestamp so other missions can detect the change.
	broadcastTimestamp := float64(time.Now().UnixNano()) / 1e9
	expiryFilepath := config.GetGlobalCredentialsExpiryFilepath(w.agencDirpath)
	broadcastStr := fmt.Sprintf("%f", broadcastTimestamp)
	if err := os.WriteFile(expiryFilepath, []byte(broadcastStr), 0644); err != nil {
		w.logger.Warn("Upward sync: failed to write credentials broadcast file", "error", err)
	}
}

// watchCredentialDownwardSync uses fsnotify to watch the global-credentials-expiry
// file. When another mission's upward sync updates global credentials, this
// mission pulls the fresh credentials into its per-mission Keychain entry.
// No restart is triggered — Claude reads MCP OAuth tokens from Keychain per-request.
func (w *Wrapper) watchCredentialDownwardSync(ctx context.Context) {
	expiryFilepath := config.GetGlobalCredentialsExpiryFilepath(w.agencDirpath)

	// Ensure the file exists so fsnotify has something to watch.
	if _, err := os.Stat(expiryFilepath); os.IsNotExist(err) {
		_ = os.WriteFile(expiryFilepath, []byte("0"), 0644)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		w.logger.Warn("Failed to create fsnotify watcher for credentials broadcast file", "error", err)
		return
	}
	defer watcher.Close()

	// Watch the parent directory so we catch file creation/deletion, not just
	// writes to an existing inode.
	if err := watcher.Add(w.agencDirpath); err != nil {
		w.logger.Warn("Failed to watch agenc directory for credential changes", "error", err)
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
			if event.Name != expiryFilepath {
				continue
			}
			if !(event.Has(fsnotify.Write) || event.Has(fsnotify.Create)) {
				continue
			}
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
			w.handleDownwardSync(expiryFilepath)

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			w.logger.Warn("fsnotify error watching credentials broadcast file", "error", err)
		}
	}
}

// handleDownwardSync reads the broadcast timestamp, skips if not newer than
// the last sync, then pulls fresh global credentials and merges them into
// the per-mission Keychain entry.
func (w *Wrapper) handleDownwardSync(expiryFilepath string) {
	data, err := os.ReadFile(expiryFilepath)
	if err != nil {
		w.logger.Warn("Downward sync: failed to read credentials broadcast file", "error", err)
		return
	}

	fileTimestamp, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		w.logger.Warn("Downward sync: failed to parse broadcast timestamp", "error", err, "raw", string(data))
		return
	}

	if fileTimestamp <= w.lastDownwardSyncTimestamp {
		return // Already processed this or an earlier broadcast
	}

	w.logger.Info("Downward sync: global credentials updated, pulling",
		"broadcastTimestamp", fileTimestamp, "lastSyncTimestamp", w.lastDownwardSyncTimestamp)

	globalCred, err := claudeconfig.ReadKeychainCredentials(claudeconfig.GlobalCredentialServiceName)
	if err != nil {
		w.logger.Warn("Downward sync: failed to read global credentials", "error", err)
		// Update timestamp anyway to avoid thrashing on a bad global entry
		w.lastDownwardSyncTimestamp = fileTimestamp
		return
	}

	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(w.agencDirpath, w.missionID)
	perMissionServiceName := claudeconfig.ComputeCredentialServiceName(claudeConfigDirpath)

	perMissionCred, err := claudeconfig.ReadKeychainCredentials(perMissionServiceName)
	if err != nil {
		w.logger.Warn("Downward sync: failed to read per-mission credentials", "error", err)
		w.lastDownwardSyncTimestamp = fileTimestamp
		return
	}

	merged, changed, err := claudeconfig.MergeCredentialJSON([]byte(perMissionCred), []byte(globalCred))
	if err != nil {
		w.logger.Warn("Downward sync: failed to merge credentials", "error", err)
		w.lastDownwardSyncTimestamp = fileTimestamp
		return
	}

	w.lastDownwardSyncTimestamp = fileTimestamp

	if !changed {
		return
	}

	if err := claudeconfig.WriteKeychainCredentials(perMissionServiceName, string(merged)); err != nil {
		w.logger.Warn("Downward sync: failed to write merged credentials to per-mission Keychain", "error", err)
		return
	}

	// Update cached hash so upward sync doesn't immediately re-broadcast what we just pulled.
	newHash, err := claudeconfig.ComputeCredentialHash(string(merged))
	if err == nil {
		w.credentialHashMu.Lock()
		w.perMissionCredentialHash = newHash
		w.credentialHashMu.Unlock()
	}

	w.logger.Info("Downward sync: merged global credentials into per-mission Keychain")
}
