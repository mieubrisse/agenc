# MCP Credential Sync Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Resurrect the Keychain-based MCP OAuth credential sync machinery so that per-mission Keychain entries are cloned at spawn, propagated up to the global entry when Claude acquires new MCP tokens, and pulled down from the global entry when another mission updates it.

**Architecture:** Recreate `internal/wrapper/credential_sync.go` with the upward and downward sync goroutines. Wire `cloneCredentials()`, `initCredentialHash()`, and the two goroutines into `Run()`/`RunHeadless()` in `wrapper.go`. Stop `cleanupOldAuthFiles()` from deleting the `global-credentials-expiry` broadcast file — that file is now the inter-mission change signal. The key behavioural delta from the original code: downward sync no longer restarts Claude and no longer tracks `tokenExpiresAt` (Claude's auth uses the env var, not Keychain).

**Tech Stack:** Go, macOS Keychain (`security` CLI), `github.com/fsnotify/fsnotify`, existing `claudeconfig` and `config` packages.

---

### Task 1: Stop cleanupOldAuthFiles from deleting the broadcast file

**Files:**
- Modify: `internal/config/config.go:432-443`

The `cleanupOldAuthFiles` function currently deletes the `global-credentials-expiry` file on every OAuth token setup. That file is our inter-mission broadcast signal for downward sync. Remove the deletion so it persists.

**Step 1: Open `internal/config/config.go` and find `cleanupOldAuthFiles` (lines 432-443)**

Current content:

```go
func cleanupOldAuthFiles(agencDirpath string) {
	// Remove the global-credentials-expiry file used for broadcasting Keychain
	// credential changes across missions. No longer needed with token-file auth.
	expiryFilepath := GetGlobalCredentialsExpiryFilepath(agencDirpath)
	if err := os.Remove(expiryFilepath); err != nil && !os.IsNotExist(err) {
		// Log suppressed: cleanup is best-effort and should not produce noise
	}

	// Note: Per-mission Keychain entries ("Claude Code-credentials-<hash>") are
	// cleaned up automatically when missions are removed via `agenc mission rm`.
	// Old entries from before this cleanup was added are harmless and left in place.
}
```

**Step 2: Replace with a no-op + comment explaining why**

```go
func cleanupOldAuthFiles(_ string) {
	// The global-credentials-expiry file is now used as a broadcast signal for
	// MCP credential downward sync across missions. Do not delete it.
	//
	// Note: Per-mission Keychain entries ("Claude Code-credentials-<hash>") are
	// cleaned up automatically when missions are removed via `agenc mission rm`.
	// Old entries from before this cleanup was added are harmless and left in place.
}
```

**Step 3: Build to confirm it compiles**

```bash
make build
```
Expected: binary builds without errors. (Requires `dangerouslyDisableSandbox: true` because make needs Go build cache access.)

**Step 4: Commit**

```bash
git add internal/config/config.go
git commit -m "Stop cleanupOldAuthFiles from deleting global-credentials-expiry broadcast file"
git pull --rebase
git push
```

---

### Task 2: Add credential sync fields to Wrapper struct

**Files:**
- Modify: `internal/wrapper/wrapper.go:41-84` (the `Wrapper` struct)

The struct currently lacks `perMissionCredentialHash`, `credentialHashMu`, and `lastDownwardSyncTimestamp`. These are needed by `credential_sync.go`.

**Step 1: Locate the `Wrapper` struct in `internal/wrapper/wrapper.go` (lines 41-84)**

Find the block that ends with the window coloring fields:

```go
	// Window coloring configuration for tmux state feedback. Read from config.yml at startup.
	// Empty strings mean that specific color setting is disabled.
	windowBusyBackgroundColor      string
	windowBusyForegroundColor      string
	windowAttentionBackgroundColor string
	windowAttentionForegroundColor string
}
```

**Step 2: Add the three new fields before the window coloring block**

Insert after `pendingRestart *Command` and its comment, before the window coloring block:

```go
	// perMissionCredentialHash caches the SHA-256 hash of the per-mission
	// Keychain credential JSON. The upward sync goroutine compares the current
	// Keychain contents against this hash to detect when Claude updates MCP
	// OAuth tokens. Protected by credentialHashMu since both the upward and
	// downward sync goroutines access it.
	perMissionCredentialHash string
	credentialHashMu         sync.Mutex

	// lastDownwardSyncTimestamp is the broadcast file timestamp from the most
	// recent downward sync. Used to skip stale broadcasts and avoid re-applying
	// the same global credential update twice.
	lastDownwardSyncTimestamp float64
```

**Step 3: Add `sync` to the imports in wrapper.go**

Find the existing import block and add `"sync"` to the standard library imports section. The import block is near the top of the file.

**Step 4: Build to confirm it compiles**

```bash
make build
```
Expected: binary builds without errors.

**Step 5: Commit**

```bash
git add internal/wrapper/wrapper.go
git commit -m "Add credential sync fields to Wrapper struct"
git pull --rebase
git push
```

---

### Task 3: Recreate credential_sync.go

**Files:**
- Create: `internal/wrapper/credential_sync.go`

This file implements the three sync functions. It is based on the version that existed at commit `8001114`, with these changes:
- Remove `tokenExpiresAt` references (field no longer exists)
- Remove the restart logic from `handleDownwardSync` (no restart on downward sync)
- Remove the statusline message write from `handleDownwardSync`
- Remove `clearStatuslineMessage()` (was only called before restart)
- Replace the expiry-comparison check in `handleDownwardSync` with a monotonic timestamp comparison using `lastDownwardSyncTimestamp`
- Keep `initCredentialHash`, `watchCredentialUpwardSync`, `checkUpwardSync`, `watchCredentialDownwardSync`, `handleDownwardSync`

**Step 1: Create `internal/wrapper/credential_sync.go` with this content**

```go
package wrapper

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/mieubrisse/agenc/internal/claudeconfig"
	"github.com/mieubrisse/agenc/internal/config"
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
```

**Step 2: Check the module path**

Before writing the file, verify the correct module import path:

```bash
head -3 go.mod
```

Replace `github.com/mieubrisse/agenc` in the import block with whatever `go.mod` says if it differs.

**Step 3: Build to confirm it compiles**

```bash
make build
```
Expected: binary builds without errors.

**Step 4: Commit**

```bash
git add internal/wrapper/credential_sync.go
git commit -m "Resurrect credential_sync.go for MCP OAuth credential sync"
git pull --rebase
git push
```

---

### Task 4: Wire credential sync into wrapper.go Run() and RunHeadless()

**Files:**
- Modify: `internal/wrapper/wrapper.go`

Add `cloneCredentials()` and `writeBackCredentials()` methods, then wire them and the two goroutines into the startup sequence.

**Step 1: Add cloneCredentials() and writeBackCredentials() methods to wrapper.go**

Add these two methods anywhere in `wrapper.go` (after `NewWrapper` is a good place):

```go
// cloneCredentials copies fresh credentials from the global Keychain into the
// per-mission entry so Claude has access to current MCP OAuth tokens at spawn.
func (w *Wrapper) cloneCredentials() {
	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(w.agencDirpath, w.missionID)
	if err := claudeconfig.CloneKeychainCredentials(claudeConfigDirpath); err != nil {
		w.logger.Warn("Failed to clone Keychain credentials", "error", err)
	}
}

// writeBackCredentials merges per-mission Keychain credentials back into the
// global entry so MCP OAuth tokens acquired in this mission persist.
func (w *Wrapper) writeBackCredentials() {
	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(w.agencDirpath, w.missionID)
	if err := claudeconfig.WriteBackKeychainCredentials(claudeConfigDirpath); err != nil {
		if w.logger != nil {
			w.logger.Warn("Failed to write back Keychain credentials", "error", err)
		}
	}
}
```

**Step 2: Wire into Run()**

In `Run()`, after `go listenSocket(...)` (currently line 174) and before the `if isResume` block, add:

```go
	// Clone global MCP credentials into per-mission Keychain and start sync goroutines
	w.cloneCredentials()
	w.initCredentialHash()
	go w.watchCredentialUpwardSync(ctx)
	go w.watchCredentialDownwardSync(ctx)
```

Also add `w.writeBackCredentials()` at the two exit points in `Run()` — at any `defer` or explicit return that represents mission end (search for `defer` and the main loop's exit). The cleanest approach is to add:

```go
defer w.writeBackCredentials()
```

immediately after `w.cloneCredentials()` / `w.initCredentialHash()`.

**Step 3: Wire into RunHeadless()**

Apply the same pattern in `RunHeadless()`. After `go w.writeHeartbeat(ctx)` (currently line 579), add:

```go
	// Clone global MCP credentials into per-mission Keychain and start sync goroutines
	w.cloneCredentials()
	w.initCredentialHash()
	go w.watchCredentialUpwardSync(ctx)
	go w.watchCredentialDownwardSync(ctx)
	defer w.writeBackCredentials()
```

**Step 4: Add claudeconfig import to wrapper.go if not present**

Check the import block:

```bash
grep "claudeconfig" internal/wrapper/wrapper.go
```

If absent, add `"github.com/mieubrisse/agenc/internal/claudeconfig"` (adjust module path per go.mod) to the imports.

**Step 5: Build to confirm it compiles**

```bash
make build
```
Expected: binary builds without errors.

**Step 6: Commit**

```bash
git add internal/wrapper/wrapper.go
git commit -m "Wire MCP credential sync goroutines into Run and RunHeadless"
git pull --rebase
git push
```

---

### Task 5: Update system-architecture.md

**Files:**
- Modify: `docs/system-architecture.md`

**Step 1: Find the credential sync description under `internal/wrapper/` (around line 323-324)**

Current text:

```
- `token_expiry.go` — (disabled) `watchTokenExpiry` goroutine, previously checked Keychain token expiry; disabled as part of the token file auth migration
- `credential_sync.go` — (disabled) credential upward/downward sync goroutines, previously synchronized Keychain credentials between missions; disabled as part of the token file auth migration
```

Replace with:

```
- `token_expiry.go` — (disabled) `watchTokenExpiry` goroutine, previously checked Keychain token expiry; disabled as part of the token file auth migration
- `credential_sync.go` — MCP OAuth credential sync goroutines: `initCredentialHash` (baseline hash at spawn), `watchCredentialUpwardSync` (polls per-mission Keychain every 60s; when hash changes, merges to global and writes broadcast timestamp to `global-credentials-expiry`), `watchCredentialDownwardSync` (fsnotify on `global-credentials-expiry`; when another mission broadcasts, pulls global into per-mission Keychain)
```

**Step 2: Find the credentials paragraph under "Per-mission config merging" (around line 362)**

Current text:

```
Credentials are handled separately: `.claude.json` is copied from the user's account identity file and patched with a trust entry for the mission's agent directory. Authentication uses a token file at `$AGENC_DIRPATH/cache/oauth-token` — the wrapper reads this file at spawn time and passes the value as `CLAUDE_CODE_OAUTH_TOKEN` in the child environment. No per-mission Keychain entries are created.
```

Replace with:

```
Credentials are handled in two layers. Claude's own authentication uses a token file at `$AGENC_DIRPATH/cache/oauth-token` — the wrapper reads this at spawn time and passes it as `CLAUDE_CODE_OAUTH_TOKEN` in the child environment. MCP server OAuth tokens (`mcpOAuth`) use the macOS Keychain: at spawn time the wrapper clones the global `"Claude Code-credentials"` entry into a per-mission entry (`"Claude Code-credentials-<8hexchars>"`). Two goroutines keep these in sync: upward sync detects hash changes in the per-mission entry and merges them to global; downward sync watches a broadcast file (`global-credentials-expiry`) for changes made by other missions and pulls the updated global entry into the per-mission entry.
```

**Step 3: Find the `build.go` note about Keychain functions being unused (around line 277)**

Current text:

```
Note: Keychain credential functions (`CloneKeychainCredentials`, `DeleteKeychainCredentials`) still exist but are unused — authentication now uses the token file approach (see `internal/config/`)
```

Replace with:

```
Keychain credential functions (`CloneKeychainCredentials`, `WriteBackKeychainCredentials`, `DeleteKeychainCredentials`) handle MCP OAuth token propagation across missions — used by the credential sync goroutines in `internal/wrapper/credential_sync.go`. Claude's own authentication uses the token file approach (see `internal/config/`).
```

**Step 4: Build to confirm it still compiles (architecture doc change only, but good habit)**

```bash
make build
```

**Step 5: Commit**

```bash
git add docs/system-architecture.md
git commit -m "Update architecture doc: credential sync re-enabled for MCP OAuth tokens"
git pull --rebase
git push
```

---

### Task 6: Manual smoke test

No automated tests required — the sync logic is integration-level (Keychain + inter-process file watching). Perform this manually after deployment:

**Verification steps:**

1. Start two missions: `agenc mission new "test-a"` and `agenc mission new "test-b"`
2. In mission A, trigger an MCP OAuth flow (e.g. connect to a Google Drive MCP server or run `agenc login`)
3. Watch `~/.agenc/global-credentials-expiry` — it should be written within 60 seconds of the OAuth completing
4. Check mission B's per-mission Keychain entry: `security find-generic-password -a $USER -s "Claude Code-credentials-<hash-for-B>" -w` — it should contain the `mcpOAuth` tokens from mission A
5. Verify the `global-credentials-expiry` file is NOT deleted when a new mission is started (the `cleanupOldAuthFiles` change from Task 1)

**Wrapper log locations:** `~/.agenc/missions/<uuid>/agent/wrapper.log` — check for "Upward sync:" and "Downward sync:" log lines.
