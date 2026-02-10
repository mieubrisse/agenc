Credential Sync Daemon Loop
===========================

Problem
-------

When multiple AgenC missions run concurrently, they all start with the same OAuth refresh token (cloned from the global Keychain entry at mission creation). OAuth refresh tokens are single-use — when mission A refreshes its token, the old refresh token is invalidated server-side. Missions B, C, D still hold the old token in their Keychain entries. When their access tokens expire and they attempt to refresh, the server rejects the stale refresh token and Claude Code prompts for re-login.

**Root cause:** Token writeback only happens on mission exit (`wrapper.writeBackCredentials`). Running missions never share refreshed tokens with each other. There is no real-time token coordination between concurrent missions.

**Upstream issue:** https://github.com/anthropics/claude-code/issues/24317

Solution Overview
-----------------

Add a new daemon goroutine — the **credential sync loop** — that runs every 30 seconds. Each cycle:

1. Reads the global Keychain entry + all running missions' Keychain entries
2. Merges them to find the freshest credentials (by `expiresAt` comparison)
3. Writes the merged result back to any entry that changed

This ensures that when one mission refreshes its token, all other missions' Keychain entries are updated within 30 seconds.

### Why this helps

- If Claude Code re-reads from the Keychain before attempting a refresh, a mission finds a fresh token instead of a stale one
- Even if Claude Code caches tokens in memory, keeping Keychain entries in sync is good hygiene and prepares for upstream improvements
- Significantly reduces the race window where multiple missions hold stale tokens

Design
------

### New merge function: `MergeCredentialsByFreshness`

**File:** `internal/claudeconfig/merge.go`

```go
func MergeCredentialsByFreshness(a, b []byte) ([]byte, bool, error)
```

This is separate from the existing `MergeCredentialJSON` function because the semantics are different:

- `MergeCredentialJSON` uses "overlay wins" semantics for non-mcpOAuth keys — appropriate for the write-back-on-exit flow where the overlay is known to be the newer side
- `MergeCredentialsByFreshness` must be **symmetric** — the fresher credential wins regardless of argument order, because the sync loop has no way to know which side is "newer" without inspecting timestamps

**Merge rules:**

| Key | Strategy |
|-----|----------|
| `claudeAiOauth` | Compare `expiresAt` inside the object. Keep whichever has the larger value. If neither has `expiresAt` or they're equal, keep `a` (no-op). |
| `mcpOAuth` | Per-server merge using existing `mergeMcpOAuth` helper (already timestamp-aware). |
| All other top-level keys | Keep from whichever side had the fresher `claudeAiOauth`. Rationale: these keys (e.g., `primaryApiKey`) are associated with the auth session that refreshed most recently. |

**Return values:**

- Merged JSON bytes
- Boolean: whether `a` was modified (useful to decide whether to write back)
- Error if JSON parsing fails

### New daemon goroutine: credential sync loop

**File:** `internal/daemon/credential_sync.go`

```go
const credentialSyncInterval = 30 * time.Second

func (d *Daemon) runCredentialSyncLoop(ctx context.Context)
func (d *Daemon) runCredentialSyncCycle(ctx context.Context)
```

Follows the same loop+cycle pattern as `runRepoUpdateLoop` / `runRepoUpdateCycle`.

**Sync cycle algorithm:**

1. Query `d.db.ListMissions(ListMissionsParams{IncludeArchived: false})` for active missions
2. Filter to running missions using heartbeat freshness (< 5 min), same threshold as `heartbeatStalenessThreshold` in `template_updater.go`
3. For each running mission, compute the Keychain service name:
   ```go
   claudeconfig.ComputeCredentialServiceName(
       claudeconfig.GetMissionClaudeConfigDirpath(d.agencDirpath, m.ID),
   )
   ```
4. Read all credentials (global + per-mission) via `claudeconfig.ReadKeychainCredentials`. Skip any that fail to read (mission may have just exited).
5. Iteratively merge all credential sets using `MergeCredentialsByFreshness` to produce the "best" combined credentials
6. For each entry (global + per-mission) that differs from the merged result, write back via `claudeconfig.WriteKeychainCredentials`
7. Log propagation: `"Credential sync: propagated fresh credentials to N entries"`

### Registration in daemon.go

**File:** `internal/daemon/daemon.go`

Add another `wg.Add(1)` + goroutine calling `d.runCredentialSyncLoop(ctx)` in the `Run` method. This becomes the 6th daemon goroutine (increasing from current five).

### Architecture doc update

**File:** `docs/system-architecture.md`

Update the daemon goroutine count from five to six, and add:

> **6. Credential sync loop** (`internal/daemon/credential_sync.go`) — Runs every 30 seconds. Reads global + running missions' Keychain credentials, merges by freshness (largest `expiresAt` wins), and propagates the freshest credentials to all entries that are stale. Mitigates the OAuth refresh token race condition when multiple missions run concurrently.

Design Decisions
----------------

### Separate merge function (not modifying `MergeCredentialJSON`)

The existing `MergeCredentialJSON` has "overlay wins" semantics: when two entries have the same key, the overlay always wins regardless of timestamps. This is correct for its use case (mission exit writes back to global), but would produce incorrect results in the sync loop where there's no inherent ordering between entries.

`MergeCredentialsByFreshness` ensures the fresher credential always wins regardless of argument order.

### Heartbeat-based running mission detection

Uses the same `heartbeatStalenessThreshold` (< 5 min) as `runRepoUpdateCycle`:

- Avoids filesystem PID reads
- Naturally handles crashed wrappers (stale heartbeats are excluded)
- Consistent with existing daemon patterns

### 30-second interval

- Fast enough propagation for typical OAuth token refresh patterns (access tokens typically last hours)
- Not so frequent as to cause excessive Keychain queries
- Matches general daemon background task cadence

### No daemon struct changes

The daemon already has `agencDirpath` and `db` — everything needed to compute service names and query missions. No new fields required.

Files to Modify
---------------

| File | Change |
|------|--------|
| `internal/claudeconfig/merge.go` | Add `MergeCredentialsByFreshness` function |
| `internal/claudeconfig/merge_test.go` | Add tests for freshness-based merge |
| `internal/daemon/credential_sync.go` | **New file:** credential sync loop + cycle |
| `internal/daemon/daemon.go` | Register 6th goroutine |
| `docs/system-architecture.md` | Document new loop, update goroutine count |

Tests
-----

### Unit tests for `MergeCredentialsByFreshness`

| Case | Expected |
|------|----------|
| `a` has newer `claudeAiOauth.expiresAt` | `a` wins; changed = false |
| `b` has newer `claudeAiOauth.expiresAt` | `b` wins; changed = true |
| Neither has `expiresAt` | `a` preserved; changed = false |
| Equal `expiresAt` | `a` preserved; changed = false |
| `mcpOAuth` with mixed per-server freshness | Per-server merge (reuses existing behavior) |
| One side has extra top-level keys | All keys included in result |
| One side is empty `{}` | Other side wins |
| Both are empty `{}` | Empty result; changed = false |

### Integration verification

1. Start 2+ missions
2. Check daemon logs for "Credential sync" messages: `tail -f ~/.agenc/daemon/daemon.log`
3. Verify all running missions' Keychain entries stay in sync
4. End-to-end: run missions long enough for token expiry (~15 hours) and confirm re-login prompts are eliminated or significantly reduced

Open Questions
--------------

1. **Does Claude Code re-read from Keychain on token refresh?** If it caches the refresh token in memory and never re-reads, the sync loop helps less (though it still keeps Keychain entries consistent for the next restart). The upstream issue may clarify this.

2. **Should the interval be configurable?** 30 seconds is a sensible default. Could be made configurable via `agenc config set credential-sync-interval` if users need tuning, but this is low priority.

3. **Linux support?** This spec assumes macOS Keychain. On Linux, Claude Code stores credentials in `$CLAUDE_CONFIG_DIR/.credentials.json` files. The sync loop would need a parallel implementation that reads/writes files instead of Keychain entries. Deferring Linux support to a follow-up since AgenC currently targets macOS.
