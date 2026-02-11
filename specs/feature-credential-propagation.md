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
- **Downward sync**: Global credential changes trigger immediate notification via filesystem timestamp file watched by fsnotify
- **Graceful restart**: Sessions detecting new credentials restart upon next idle period

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

### Hybrid Polling + Timestamp File

**Upward sync** (per-mission -> global):
- Extend existing token expiry watcher goroutine to also poll per-mission credentials every 60 seconds
- Compute SHA-256 hash of per-mission credential JSON
- Compare against cached baseline (set at spawn + after each detected change)
- On mismatch: read per-mission, read global, merge per-mission -> global, write global, update baseline
- After successful write to global: touch `~/.agenc/credential-updated-at` timestamp file

**Downward sync** (global -> per-mission):
- New goroutine watches `~/.agenc/credential-updated-at` via fsnotify
- On file write event (debounced 1 second):
  - Read global Keychain entry
  - Compute hash, compare against cached global baseline
  - On mismatch: merge global -> per-mission, write per-mission Keychain
  - If per-mission actually changed: set `stateRestartPending`, write statusline message
- On restart (respawn): clear statusline message, refresh cached baselines from current Keychain

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

### Phase 1: Credential Hash Utilities

- [ ] Create `internal/claudeconfig/hash.go` with `ComputeCredentialHash(credentialJSON []byte) (string, error)` — normalize JSON via unmarshal/marshal, SHA-256 hash, hex encode
- [ ] Add `GetCredentialTimestampFilepath(agencDirpath string) string` to `internal/config/config.go` — returns `filepath.Join(agencDirpath, "credential-updated-at")`
- [ ] Unit tests for hash normalization in `internal/claudeconfig/hash_test.go`

### Phase 2: Upward Sync (Per-Mission -> Global)

- [ ] Add fields to `Wrapper` struct in `internal/wrapper/wrapper.go`: `perMissionCredentialHash`, `globalCredentialHash`, `justWroteTimestamp bool`, `timestampWriteTime time.Time`
- [ ] Initialize hash caches in `Run()` after `cloneCredentials()` — read both Keychain entries, compute and cache hashes
- [ ] Expand token expiry watcher in `internal/wrapper/token_expiry.go` (or rename to `credential_sync.go`) to also check per-mission credential hash every 60 seconds
- [ ] On hash mismatch: read global, merge per-mission -> global via `MergeCredentialJSON`, write to global Keychain, touch timestamp file, update cached hashes
- [ ] Log: "Propagated per-mission credential changes to global Keychain"

### Phase 3: Downward Sync (Global -> Per-Mission)

- [ ] Create `internal/wrapper/credential_sync.go` with `runCredentialDownwardSyncWatcher()` function
- [ ] Setup fsnotify watcher on `GetCredentialTimestampFilepath(agencDirpath)` — create file if missing, watch parent dir as fallback
- [ ] Debounce events: 1-second timer, reset on each event, process after silence
- [ ] Self-detection: if `justWroteTimestamp` and within 5 seconds of `timestampWriteTime`, skip processing
- [ ] On external timestamp change: read global, compare hash, merge global -> per-mission if different
- [ ] If per-mission actually changed: write to per-mission Keychain, set `stateRestartPending`, write statusline message
- [ ] Launch goroutine in `Run()` after credential initialization

### Phase 4: Restart Integration

- [ ] In respawn flow after `cloneCredentials()`: read fresh credentials, recompute and update both hash caches
- [ ] Clear statusline message on restart
- [ ] Skip credential sync operations when `w.state != stateRunning` (avoid double-restart)
- [ ] Existing `writeBackCredentials()` at exit remains as safety net (no changes)

### Phase 5: Testing

- [ ] Unit tests: `ComputeCredentialHash` normalization and uniqueness
- [ ] Unit tests: upward sync logic with mocked Keychain
- [ ] Unit tests: downward sync logic with mocked Keychain and filesystem
- [ ] Unit tests: self-detection avoidance
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

### Self-Detection Avoidance

When a wrapper touches the timestamp file during upward sync:
1. Set `w.justWroteTimestamp = true`
2. Record `w.timestampWriteTime = time.Now()`
3. In fsnotify handler: if event fires within 5 seconds of write time, skip
4. Otherwise: process as external write

### Debouncing fsnotify Events

Filesystem events may fire multiple times for a single logical change (especially on macOS). Debounce:
1. Start 1-second timer on first event
2. Reset timer on each subsequent event
3. Process only after 1 second of silence

### Keychain Read Cost

Each Keychain read shells out to `security find-generic-password` (~50ms). Cost profile:
- Upward sync: 1 read per wrapper per 60s
- Downward sync: 1 read per wrapper per timestamp update (rare)
- 10 concurrent missions: ~0.17 reads/sec amortized. Negligible.

### State Machine Interaction

- `stateRunning`: Normal sync operations proceed
- `stateRestartPending`: Skip sync to avoid double-restart
- `stateRestarting`: Skip sync, respawn will refresh credentials
- After respawn: Refresh cached hashes from current Keychain state

### Timestamp File Location

File: `~/.agenc/credential-updated-at` (root of agenc directory, not per-mission). Single file for all missions to watch.

### Statusline Message

On credential change detected: `"🔄 New authentication token detected; restarting upon next idle"`

On restart complete: clear message (remove file or write empty string)

Testing Strategy
----------------

### Unit Tests

- `internal/claudeconfig/hash_test.go`: Hash normalization, uniqueness, invalid JSON handling
- `internal/wrapper/credential_sync_test.go`: Mock Keychain read/write, test upward sync, downward sync, self-detection, debouncing

### Integration Tests

- Full upward sync cycle: per-mission change -> global merge -> timestamp touch
- Full downward sync cycle: global change -> per-mission merge -> restart
- Multi-mission convergence under rapid updates
- MCP OAuth token propagation (newer `expiresAt` wins)

### Manual Testing

1. Start 3 missions, run `/login` in one, observe propagation within 60s
2. Verify statusline messages appear and clear after restart
3. Kill mission mid-sync, verify no corruption
4. Delete timestamp file, verify self-healing on next write

Acceptance Criteria
-------------------

- [ ] When a user runs `/login` in one mission, all other missions detect the change within 60 seconds
- [ ] Missions detecting new credentials display statusline "🔄 New authentication token detected; restarting upon next idle"
- [ ] Missions restart automatically upon next idle period after credential change
- [ ] After restart, missions use the fresh credentials
- [ ] MCP OAuth token refreshes propagate correctly (newer `expiresAt` wins per server)
- [ ] Multiple rapid credential updates converge to consistent state
- [ ] No spurious restarts occur (self-detection avoidance works)
- [ ] Credential sync skips when wrapper state is not `stateRunning`
- [ ] Existing write-back at wrapper exit continues to function as safety net
- [ ] All unit and integration tests pass

Risks & Considerations
----------------------

**Keychain concurrent writes**: macOS Keychain operations are atomic at the OS level via Keychain Services API. Last write wins. Merge logic ensures no data loss — newest `expiresAt` wins for MCP OAuth, overlay semantics for top-level keys. Worst case: one wrapper's write overwrites another's within the same 60s window, corrected on next cycle.

**Fsnotify event storm**: 1-second debounce collapses rapid events. Credential updates are rare (human `/login` or hourly MCP OAuth refresh). Low risk.

**Race between upward and downward sync**: Merge logic is commutative and idempotent. Eventual consistency guaranteed within 60 seconds via polling + merge. Low risk.

**Timestamp file deletion**: fsnotify handles gracefully. Recreated on next upward sync. Transient disruption only.

**Token expiry during restart wait**: Token expiry watcher continues running. User sees expiry warning. System corrects on restart.

**Backward compatibility**: Existing missions without sync watchers continue with exit-time write-back. New wrappers activate automatically. No migration needed.

**CI testing**: Keychain operations require macOS. Integration tests should use temporary Keychain entries and clean up after completion.
