MCP Credential Sync Resurrection
=================================

Status: Approved
Date: 2026-02-19


Overview
--------

Resurrect the credential sync machinery that was removed when AgenC switched
from Keychain-based Claude auth to `CLAUDE_CODE_OAUTH_TOKEN`. The Keychain is
still needed for MCP server OAuth tokens (`mcpOAuth` entries). Without sync,
each mission gets a stale snapshot of MCP credentials at spawn time and never
learns about tokens acquired or refreshed in other missions.

Claude's own authentication (via `CLAUDE_CODE_OAUTH_TOKEN` env var) is
explicitly out of scope — it remains unchanged.


Scope
-----

Three behaviours to restore:

1. **Spawn clone** — at mission startup, copy the global Keychain entry
   (`"Claude Code-credentials"`) into the per-mission entry
   (`"Claude Code-credentials-<8hexchars>"`).

2. **Upward sync** — when Claude acquires or refreshes an MCP OAuth token
   (detectable by a hash change on the per-mission Keychain entry), merge the
   per-mission entry into the global entry and broadcast the change via the
   `global-credentials-expiry` file.

3. **Downward sync** — when the broadcast file changes (another mission did an
   upward sync), merge the fresh global entry into this mission's per-mission
   entry. No restart required.


Architecture
------------

### A. Spawn clone (`wrapper.go`)

At the top of `Run()` and `RunHeadless()`:

1. `cloneCredentials()` — calls `claudeconfig.CloneKeychainCredentials(claudeConfigDirpath)`
2. `initCredentialHash()` — reads per-mission Keychain, caches SHA-256 hash as baseline

### B. Upward sync goroutine (`credential_sync.go`)

`watchCredentialUpwardSync(ctx)`:
- Polls per-mission Keychain every 60 seconds (`credentialSyncInterval`)
- Computes current hash; compares to `w.perMissionCredentialHash` (mutex-protected)
- On change: `MergeCredentialJSON(global, perMission)` → write merged to global
  Keychain → write Unix timestamp to `global-credentials-expiry` broadcast file
- Updates cached hash

### C. Downward sync goroutine (`credential_sync.go`)

`watchCredentialDownwardSync(ctx)`:
- Uses `fsnotify` on the `global-credentials-expiry` file
- Debounces events by `credentialDownwardDebouncePeriod` (1 second)
- `handleDownwardSync()`: reads timestamp; skips if not newer than
  `w.lastDownwardSyncTimestamp`; reads global Keychain;
  `MergeCredentialJSON(perMission, global)` → write merged to per-mission
  Keychain → update `w.lastDownwardSyncTimestamp`
- **No restart** (unlike the original implementation)

### D. Cleanup (`internal/config/config.go`)

Remove the deletion of `global-credentials-expiry` from `cleanupOldAuthFiles()`.
That file is now a live operational artifact used for cross-mission broadcast,
not a legacy artifact to be cleaned up.


Wrapper Struct Changes
----------------------

Add back to `Wrapper` struct in `wrapper.go`:

```go
perMissionCredentialHash  string
credentialHashMu          sync.Mutex
lastDownwardSyncTimestamp float64
```

Drop (do not restore):

- `tokenExpiresAt float64` — was used to track Claude's token expiry via
  Keychain; Claude's auth now flows through `CLAUDE_CODE_OAUTH_TOKEN` env var


Data Flow
---------

**Spawn:**
```
global Keychain ("Claude Code-credentials")
  → CloneKeychainCredentials()
  → per-mission Keychain ("Claude Code-credentials-<8hexchars>")
  → initCredentialHash() → w.perMissionCredentialHash
```

**Upward (Mission A does MCP OAuth):**
```
Claude updates per-mission Keychain
  → 60s poll detects hash change
  → MergeCredentialJSON(global, perMission)   // mcpOAuth: newest expiresAt wins
  → WriteKeychainCredentials(global, merged)
  → write Unix timestamp → global-credentials-expiry
  → update w.perMissionCredentialHash
```

**Downward (Mission B sees broadcast):**
```
global-credentials-expiry changes (fsnotify)
  → debounce 1s
  → read timestamp; skip if not newer
  → ReadKeychainCredentials(global)
  → MergeCredentialJSON(perMission, global)   // mcpOAuth: newest expiresAt wins
  → WriteKeychainCredentials(perMission, merged)
  → update w.lastDownwardSyncTimestamp
```

**Merge semantics:** `MergeCredentialJSON` keeps the newer `expiresAt` per
MCP server; overlay wins on all other top-level keys.


Error Handling
--------------

All credential sync operations are best-effort and non-fatal:

- Spawn clone failure (e.g. no global entry yet) → log warning, continue
- Upward sync failure → log warning, do not update cached hash (retries next
  poll), continue goroutine
- Downward sync failure → log warning, update `lastDownwardSyncTimestamp`
  anyway to avoid thrashing, continue goroutine


Testing
-------

Existing `hash_test.go` and `merge_test.go` cover the underlying logic; no new
unit tests required.

Manual smoke test after implementation:
1. Spin up two missions
2. Perform MCP OAuth in Mission A (e.g. `agenc login` or trigger MCP auth)
3. Within ~60 seconds, verify Mission B's per-mission Keychain entry contains
   the new `mcpOAuth` token entries


Files Touched
-------------

| File | Change |
|------|--------|
| `internal/wrapper/credential_sync.go` | Recreate (resurrect from git history with modifications) |
| `internal/wrapper/wrapper.go` | Add struct fields; wire goroutines in `Run()`/`RunHeadless()` |
| `internal/config/config.go` | Remove `global-credentials-expiry` deletion from `cleanupOldAuthFiles()` |
| `docs/system-architecture.md` | Update credential sync section |
