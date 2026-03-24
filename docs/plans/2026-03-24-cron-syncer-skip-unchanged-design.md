Cron Syncer: Skip Unchanged Plists
====================================

Goal
----
Only write plist files and call launchctl when cron configuration actually changed. Ensure content changes (prompt, schedule, repo) propagate to the loaded launchd job via unload+reload.

Problem
-------
`SyncCronsToLaunchd` unconditionally writes every plist to disk on every sync (server startup + every config.yml change). It never reloads a job whose content changed — it only loads jobs that aren't loaded yet. This means: (1) unnecessary I/O on every sync, (2) cron config changes don't take effect until launchd happens to unload the job.

Approach
--------
Compare generated plist XML against the existing file on disk. If identical, skip the write and any load/unload. If different, write the new file, unload the old job, reload the new one. Stateless — no in-memory tracking needed.

Architecture
------------
One component changes: `SyncCronsToLaunchd` in `internal/server/cron_syncer.go`.

The sync loop for each cron becomes:

1. Generate plist XML in memory (already happens via `GeneratePlistXML()`)
2. Read existing plist file from disk (`os.ReadFile`)
3. Compare bytes — if identical, skip to the load-state check
4. If different (or file doesn't exist), write the new plist, set `contentChanged` flag
5. Handle load state:
   - Enabled + not loaded: load
   - Enabled + loaded + content changed: unload, then load
   - Enabled + loaded + no change: skip
   - Disabled + loaded: unload
   - Disabled + not loaded: skip

Data Flow
---------
`GeneratePlistXML()` returns `[]byte`. `os.ReadFile(plistPath)` returns `[]byte`. `bytes.Equal()` compares them. Write/reload only on mismatch. If `os.ReadFile` fails (file doesn't exist), treat as "changed".

Error Handling
--------------
- Read failure on existing plist (permissions, corruption): treat as changed, overwrite. Log a warning.
- Unload failure during reload: log and continue to load attempt — launchctl load on a fresh plist should still work.
- No new error paths beyond what exists today.

Testing
-------
- Extract a small interface from `*launchd.Manager` (`IsLoaded`, `LoadPlist`, `UnloadPlist`, `RemovePlist`) so tests can inject a mock.
- Test cases:
  - No change: sync twice with same config, verify `LoadPlist` called once (first sync), not on second
  - Content change: sync, modify prompt, sync again, verify unload+load on second sync
  - New cron: sync with no existing plist, verify write+load

Logging
-------
- "plist unchanged, skipping" for no-op syncs
- "plist changed, reloading" for content-change reloads
- Existing load/unload messages unchanged
