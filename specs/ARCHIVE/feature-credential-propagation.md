Credential Propagation Across AgenC Sessions
========================================================

**Created**: 2026-02-11
**Type**: feature
**Status**: Open
**Priority**: High
**Related**: Token expiry watcher (`internal/wrapper/token_expiry.go`), Credential merge logic (`internal/claudeconfig/merge.go`), Supersedes `specs/credential-sync-loop.md` (daemon-based approach)

---

Description
-----------

Enable real-time credential propagation across all active AgenC missions. When a user runs `/login` in one Claude Code session to refresh an expired token, all other running sessions should automatically detect and adopt the new credentials without requiring manual intervention or waiting for session exit.

Currently, fresh credentials remain trapped in the originating session's per-mission Keychain entry until that session exits and writes back to the global Keychain. Other sessions continue using stale credentials until they restart, leading to authentication failures and poor user experience.

This feature implements a hybrid sync mechanism:
- **Upward sync**: Per-mission credentials periodically poll and merge to global Keychain
- **Downward sync**: Global credential changes broadcast via a `global-credentials-expiry` file watched by fsnotify; each wrapper compares the file's expiry to its own cached expiry to decide whether to pull fresh credentials
- **Graceful restart**: Sessions detecting newer credentials restart upon next idle period

Context
-------

### Current Architecture

AgenC stores Claude Code credentials in macOS Keychain with two service name patterns:
- **Global**: `"Claude Code-credentials"` — canonical token store shared across all sessions
- **Per-mission**: `"Claude Code-credentials-<hash>"` — session-scoped copy (hash derived from mission's claude-config directory path)

### Current Credential Flow

1. **Spawn time**: Wrapper calls `CloneKeychainCredentials()` to copy global -> per-mission
2. **During session**: Claude Code reads/writes only its per-mission Keychain entry
3. **Exit time**: Wrapper calls `WriteBackKeychainCredentials()` to merge per-mission -> global using `MergeCredentialJSON()`

### Why Current Flow Fails

When a user runs `/login` in one session:
1. Claude Code updates its per-mission Keychain entry directly
2. Fresh token stays in that per-mission entry until wrapper exit
3. Other sessions continue reading their stale per-mission entries
4. User must manually restart other sessions or wait for them to exit

### Existing Infrastructure

**Credential functions** (`internal/claudeconfig/build.go`):
- `ReadKeychainCredentials(serviceName) ([]byte, error)`
- `WriteKeychainCredentials(serviceName, data) error`
- `CloneKeychainCredentials(claudeConfigDirpath) error`
- `WriteBackKeychainCredentials(claudeConfigDirpath) error`
- `ComputeCredentialServiceName(claudeConfigDirpath) string`
- `GetCredentialExpiresAt(credentialJSON) (float64, error)`

**Merge logic** (`internal/claudeconfig/merge.go`):
- `MergeCredentialJSON(base, overlay []byte) ([]byte, error)`
- Top-level keys: overlay wins
- MCP OAuth tokens: newer `expiresAt` wins per server

**Token expiry watcher** (`internal/wrapper/token_expiry.go`):
- Goroutine running every 60 seconds
- Checks `w.tokenExpiresAt` against current time
- Writes warnings to statusline-message file
- Already has timer infrastructure we can extend

**Wrapper state machine** (`internal/wrapper/wrapper.go`):
- States: `stateRunning`, `stateRestartPending`, `stateRestarting`
- Graceful restart: sets `stateRestartPending`, waits for idle Stop event, sends SIGINT, respawns
- Restart reason propagated via `Command.Reason`

Design Decision
---------------

### Hybrid Polling + Expiry Broadcast File

**Upward sync** (per-mission -> global):
- Extend existing token expiry watcher goroutine to also poll per-mission credentials every 60 seconds
- Compute SHA-256 hash of per-mission credential JSON
- Compare against cached baseline (set at spawn + after each detected change)
- On mismatch: read per-mission, read global, merge per-mission -> global, write global, update baseline
- After successful write to global: extract the new `claudeAiOauth.expiresAt` and write it to `~/.agenc/global-credentials-expiry`
- Also update `w.tokenExpiresAt` to the new expiry (so this wrapper's cache matches the file)

**Downward sync** (global -> per-mission):
- New goroutine watches `~/.agenc/global-credentials-expiry` via fsnotify
- On file write event (debounced 1 second):
  - Read expiry value from the file
  - Compare to `w.tokenExpiresAt`
  - If file expiry is newer: read global Keychain, merge global -> per-mission, write per-mission Keychain
  - If per-mission actually changed: update `w.tokenExpiresAt`, set `stateRestartPending`, write statusline message
  - If file expiry <= `w.tokenExpiresAt`: no action (this wrapper already has current or newer credentials)
- On restart (respawn): clear statusline message, refresh cached hashes and `tokenExpiresAt` from fresh Keychain state

**Why self-detection is a non-issue**: When a wrapper performs upward sync, it updates `w.tokenExpiresAt` to the new expiry *before* writing the file. When fsnotify fires back on the same wrapper, the comparison `fileExpiry > w.tokenExpiresAt` is false — the wrapper already has the fresh credentials. No flags, no time windows, no special cases.

### Why This Design

- **No daemon required**: Avoids complexity of central process, permission model unchanged
- **Low overhead**: 1 Keychain read per wrapper per 60s for upward sync, rare reads on downward sync
- **Bounded latency**: Worst case 60s + idle detection for full propagation cycle
- **Reuses existing patterns**: Extends token expiry watcher, reuses restart machinery
- **Self-healing**: Each wrapper independently maintains consistency

User Stories
------------

### Story 1: Fresh Token Propagates to All Sessions

**As a** user, **I want** my `/login` token to propagate to all running missions automatically, **so that** I don't have to manually restart each session.

**Test Steps:**

1. **Setup**: Start three missions: mission-a, mission-b, mission-c
2. **Action**: In mission-a's Claude Code session, run `/login` and complete authentication
3. **Assert**: Within 60 seconds, mission-b and mission-c statuslines show "New authentication token detected; restarting upon next idle"
4. **Action**: Let mission-b and mission-c go idle
5. **Assert**: Both missions restart automatically and use the fresh token

### Story 2: MCP OAuth Token Refresh Propagates

**As a** user, **I want** MCP OAuth token refreshes to propagate across missions, **so that** I don't lose MCP access in other sessions.

**Test Steps:**

1. **Setup**: Configure MCP server with OAuth, start two missions using it
2. **Action**: In mission-a, trigger OAuth token refresh
3. **Assert**: Within 60 seconds, mission-b detects the change and schedules restart
4. **Action**: Let mission-b go idle and restart
5. **Assert**: Mission-b uses the refreshed MCP OAuth token

### Story 3: Multiple Rapid Token Updates Converge

**As a** developer, **I want** the system to handle rapid token updates gracefully, **so that** credential state remains consistent.

**Test Steps:**

1. **Setup**: Start two missions: mission-a, mission-b
2. **Action**: Run `/login` in mission-a, then immediately run `/login` in mission-b
3. **Assert**: After 90 seconds, global and both per-mission Keychain entries have consistent merged state

Implementation Plan
-------------------

### Phase 1: Credential Hash Utilities and Path Helper

- [ ] Create `internal/claudeconfig/hash.go` with `ComputeCredentialHash(credentialJSON []byte) (string, error)` — normalize JSON via unmarshal/marshal, SHA-256 hash, hex encode
- [ ] Add `GetGlobalCredentialsExpiryFilepath(agencDirpath string) string` to `internal/config/config.go` — returns `filepath.Join(agencDirpath, "global-credentials-expiry")`
- [ ] Unit tests for hash normalization in `internal/claudeconfig/hash_test.go`

### Phase 2: Upward Sync (Per-Mission -> Global)

- [ ] Add field to `Wrapper` struct in `internal/wrapper/wrapper.go`: `perMissionCredentialHash string`
- [ ] Initialize hash cache in `Run()` after `cloneCredentials()` — read per-mission Keychain entry, compute and cache hash
- [ ] Expand token expiry watcher in `internal/wrapper/token_expiry.go` (or rename to `credential_sync.go`) to also check per-mission credential hash every 60 seconds
- [ ] On hash mismatch: read global, merge per-mission -> global via `MergeCredentialJSON`, write to global Keychain, update cached hash
- [ ] After writing global: extract `expiresAt` from merged credentials, update `w.tokenExpiresAt`, write expiry value to `global-credentials-expiry` file
- [ ] Log: "Propagated per-mission credential changes to global Keychain"

### Phase 3: Downward Sync (Global -> Per-Mission)

- [ ] Create `internal/wrapper/credential_sync.go` with `runCredentialDownwardSyncWatcher()` function
- [ ] Setup fsnotify watcher on `GetGlobalCredentialsExpiryFilepath(agencDirpath)` — create file if missing, watch parent dir as fallback
- [ ] Debounce events: 1-second timer, reset on each event, process after silence
- [ ] On file change: read expiry from file, compare to `w.tokenExpiresAt`
- [ ] If file expiry > `w.tokenExpiresAt`: read global Keychain, merge global -> per-mission, write per-mission Keychain
- [ ] If per-mission actually changed: update `w.tokenExpiresAt`, update `w.perMissionCredentialHash`, set `stateRestartPending`, write statusline message
- [ ] If file expiry <= `w.tokenExpiresAt`: no action (already current)
- [ ] Launch goroutine in `Run()` after credential initialization

### Phase 4: Statusline Coordination and Restart Integration

- [ ] Modify token expiry watcher in `internal/wrapper/token_expiry.go`: before writing expiry warning, check `w.state != stateRunning` — if restart is pending, skip writing (restart will deliver fresh credentials)
- [ ] In downward sync (Phase 3): when setting `stateRestartPending`, write `"🔄 New authentication token detected; restarting upon next idle"` to statusline file (overwrites any existing expiry warning)
- [ ] In respawn flow after `cloneCredentials()`: remove (or truncate) the statusline message file — token expiry watcher will re-evaluate on next tick and re-write if needed
- [ ] In respawn flow after `cloneCredentials()`: read fresh per-mission credentials, recompute and update `perMissionCredentialHash` and `tokenExpiresAt`
- [ ] Skip credential sync operations when `w.state != stateRunning` (avoid double-restart)
- [ ] Existing `writeBackCredentials()` at exit remains as safety net (no changes)

### Phase 5: Testing

- [ ] Unit tests: `ComputeCredentialHash` normalization and uniqueness
- [ ] Unit tests: upward sync logic with mocked Keychain
- [ ] Unit tests: downward sync logic — file expiry newer triggers pull, file expiry equal/older is no-op
- [ ] Integration tests: full propagation cycle across multiple wrappers

Technical Details
-----------------

### Credential Hash Computation

Normalize JSON before hashing to ensure whitespace/key order differences don't cause false positives:

```go
func ComputeCredentialHash(credentialJSON []byte) (string, error) {
    var parsed map[string]interface{}
    if err := json.Unmarshal(credentialJSON, &parsed); err != nil {
        return "", stacktrace.Propagate(err, "failed to unmarshal credential JSON")
    }
    normalized, err := json.Marshal(parsed)
    if err != nil {
        return "", stacktrace.Propagate(err, "failed to marshal normalized JSON")
    }
    hash := sha256.Sum256(normalized)
    return hex.EncodeToString(hash[:]), nil
}
```

### Expiry Broadcast File

The file `~/.agenc/global-credentials-expiry` contains a single float64 value (Unix timestamp) representing the `claudeAiOauth.expiresAt` from the global Keychain entry. Written as a decimal string (e.g., `1739318400.0`).

When a wrapper performs upward sync:
1. Merge per-mission -> global in Keychain
2. Extract `expiresAt` from merged result
3. Update `w.tokenExpiresAt` to the new value
4. Write new expiry to file

When any wrapper's fsnotify fires:
1. Read expiry from file
2. Compare to `w.tokenExpiresAt`
3. If file > cached: pull fresh credentials from global Keychain
4. If file <= cached: no action (this wrapper already has current credentials)

The originating wrapper naturally short-circuits at step 3 because it updated `w.tokenExpiresAt` in step 3 above before writing the file. No self-detection flags or time windows needed.

### Debouncing fsnotify Events

Filesystem events may fire multiple times for a single logical change (especially on macOS). Debounce:
1. Start 1-second timer on first event
2. Reset timer on each subsequent event
3. Process only after 1 second of silence

### Keychain Read Cost

Each Keychain read shells out to `security find-generic-password` (~50ms). Cost profile:
- Upward sync: 1 read per wrapper per 60s (per-mission Keychain)
- Downward sync: 0 Keychain reads at rest; 1 read per wrapper only when expiry file changes and file expiry > cached expiry (rare — only on `/login` events)
- 10 concurrent missions: ~0.17 reads/sec amortized for upward sync. Negligible.

### State Machine Interaction

- `stateRunning`: Normal sync operations proceed
- `stateRestartPending`: Skip sync to avoid double-restart
- `stateRestarting`: Skip sync, respawn will refresh credentials
- After respawn: Refresh `perMissionCredentialHash` and `tokenExpiresAt` from current Keychain state

### Expiry File Location

File: `~/.agenc/global-credentials-expiry` (root of agenc directory, not per-mission). Single file for all missions to watch.

### Statusline Message Ownership

Two systems write to the same per-mission statusline file (`~/.agenc/missions/<uuid>/statusline-message`):

1. **Token expiry watcher**: `"⚠️  Claude token expires soon — run 'agenc login' to refresh"`
2. **Credential sync** (new): `"🔄 New authentication token detected; restarting upon next idle"`

Rules for resolving contention:

**Writing:**
- Credential sync writes its restart message when setting `stateRestartPending`
- This overwrites any existing expiry warning (the pending restart will fix the expiry, so the warning is moot)

**Suppression:**
- Token expiry watcher must check `w.state` before writing. If `stateRestartPending`, skip writing the expiry warning — a restart is already scheduled and will deliver fresh credentials

**Clearing after restart:**
- On respawn: remove the statusline file (or truncate to empty)
- Token expiry watcher re-evaluates on its next tick (~60s). If fresh credentials → no warning. If somehow still expiring → re-writes warning naturally
- This avoids needing the restart path to know which message was previously displayed

**Clearing after expiry resolves naturally:**
- If token is no longer within the expiry warning window (e.g., user ran `/login` in this session), token expiry watcher removes the file on its next tick (existing behavior)

Testing Strategy
----------------

### Unit Tests

- `internal/claudeconfig/hash_test.go`: Hash normalization, uniqueness, invalid JSON handling
- `internal/wrapper/credential_sync_test.go`: Mock Keychain read/write, test upward sync, downward sync (expiry comparison), debouncing

### Integration Tests

- Full upward sync cycle: per-mission change -> global merge -> expiry file write
- Full downward sync cycle: expiry file change -> expiry comparison -> global pull -> per-mission merge -> restart
- Multi-mission convergence under rapid updates
- MCP OAuth token propagation (newer `expiresAt` wins)

### Manual Testing

1. Start 3 missions, run `/login` in one, observe propagation within 60s
2. Verify statusline messages appear and clear after restart
3. Kill mission mid-sync, verify no corruption
4. Delete expiry file, verify self-healing on next upward sync write

Acceptance Criteria
-------------------

- [ ] When a user runs `/login` in one mission, all other missions detect the change within 60 seconds
- [ ] Missions detecting new credentials display statusline "🔄 New authentication token detected; restarting upon next idle"
- [ ] Missions restart automatically upon next idle period after credential change
- [ ] After restart, missions use the fresh credentials
- [ ] MCP OAuth token refreshes propagate correctly (newer `expiresAt` wins per server)
- [ ] Multiple rapid credential updates converge to consistent state
- [ ] No spurious restarts occur (originating wrapper's expiry matches file, short-circuits naturally)
- [ ] Credential sync skips when wrapper state is not `stateRunning`
- [ ] Statusline shows restart message when credential change detected, overwriting any expiry warning
- [ ] Token expiry watcher suppresses its warning when `stateRestartPending` (restart will fix it)
- [ ] Statusline file is cleared on restart; token expiry watcher re-evaluates naturally on next tick
- [ ] Existing write-back at wrapper exit continues to function as safety net
- [ ] All unit and integration tests pass

Risks & Considerations
----------------------

**Keychain concurrent writes**: macOS Keychain operations are atomic at the OS level via Keychain Services API. Last write wins. Merge logic ensures no data loss — newest `expiresAt` wins for MCP OAuth, overlay semantics for top-level keys. Worst case: one wrapper's write overwrites another's within the same 60s window, corrected on next cycle.

**Fsnotify event storm**: 1-second debounce collapses rapid events. Credential updates are rare (human `/login` or hourly MCP OAuth refresh). Low risk.

**Race between upward and downward sync**: Merge logic is commutative and idempotent. Eventual consistency guaranteed within 60 seconds via polling + merge. Low risk.

**Expiry file deletion**: fsnotify handles gracefully. Recreated on next upward sync. Transient disruption only.

**Token expiry during restart wait**: Token expiry watcher checks `w.state` and suppresses its warning when restart is pending. User sees the credential restart message instead. After restart, token expiry watcher re-evaluates with fresh credentials on next tick.

**Backward compatibility**: Existing missions without sync watchers continue with exit-time write-back. New wrappers activate automatically. No migration needed.

**CI testing**: Keychain operations require macOS. Integration tests should use temporary Keychain entries and clean up after completion.
