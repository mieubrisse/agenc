Eliminate agencDirpath Global
=============================

Bead: agenc-0jkz

Problem
-------

The `cmd` package has a package-level `var agencDirpath string` in `root.go` that gets set as a side effect of `ensureConfigured()` in `config_init.go:68`. Functions throughout the package reach for this global. The `--run-wrapper` code path never called `ensureConfigured()`, so the global was empty, causing a crash when creating new missions.

Root Cause Analysis
-------------------

Two concerns are tangled together:

1. **"What is the agenc directory?"** — pure derivation, already solved by `config.GetAgencDirpath()` (reads `$AGENC_DIRPATH` env var, falls back to `~/.agenc`). Cheap, no side effects.
2. **"Has first-run setup been done?"** — `ensureConfigured()` creates dirs, runs the interactive wizard, sets up OAuth. Only matters on first-ever invocation.

The global `agencDirpath` coupled these — you had to call `ensureConfigured()` (concern 2) just to get the path (concern 1).

Design
------

**Every `cmd/` function that needs the agenc directory path calls `config.GetAgencDirpath()` directly at point of use.** No global, no threading through return values or parameters within `cmd/`.

**`internal/` functions continue to receive `agencDirpath` as a parameter** (dependency injection). The `cmd/` caller resolves the path and passes it in.

Key constraints:

- **`serverClient()` must stay lightweight.** It's called from the `--run-wrapper` process which runs in a sandboxed environment that cannot write to `~/.agenc`. It must NOT call `ensureConfigured()`. It already correctly calls `config.GetAgencDirpath()` directly.
- **`ensureConfigured()` stays as a separate function.** It handles first-run setup (dir creation, wizard, OAuth). Only commands that might be the user's first-ever invocation need it.
- **`ensureServerRunning()` stays lightweight.** Cannot fold `ensureConfigured()` into it because `serverClient()` calls it from sandboxed contexts.

What gets deleted:

- `var agencDirpath string` from `root.go`
- `agencDirpath = dirpath` side effect from `ensureConfigured()` in `config_init.go`
- `getAgencContext()` from `mission_helpers.go` — its callers use `ensureConfigured()` directly (if they need setup) or `config.GetAgencDirpath()` (if they just need the path)

What gets modified:

- **`readConfig()` / `readConfigWithComments()`** — call `ensureConfigured()` + `config.GetAgencDirpath()` internally. Keep original return signatures.
- **`ensureServerRunning()`** — changes from `func(agencDirpath string)` to `func()`. Resolves path internally via `config.GetAgencDirpath()`.
- **All command handlers** that used the global or `getAgencContext()` — call `config.GetAgencDirpath()` directly when they need the path.
- **`cmd/` helper functions** that took `agencDirpath` as a parameter just to pass through — resolve it internally instead.

Future work (separate bead: agenc-4ex2):

- Move `config get/set/edit` to server API endpoints so the CLI doesn't access `~/.agenc/config/` files directly.

Current State (as of 2026-03-20)
---------------------------------

**What's been done:**

- Subagent commit `243c7d1` changed ~13 files. Mixed approach:
  - Some files correctly use `config.GetAgencDirpath()` at point of use (mission_ls, mission_attach, mission_inspect, mission_print)
  - Some files thread `agencDirpath` as a new function parameter (mission_new, mission_update_config, summary) — needs to be changed to direct resolution
  - Server commands (server_run, server_start, server_status, server_stop) capture from `ensureConfigured()` — correct pattern for those
- `doRunWrapperDirect` in `mission_resume.go` already correctly calls `config.GetAgencDirpath()` directly (this was the original bug fix)
- The global `var agencDirpath string` still exists in `root.go`
- `getAgencContext()` still exists in `mission_helpers.go`
- `ensureConfigured()` still sets `agencDirpath = dirpath` as a side effect

**What remains:**

1. Remove the global from `root.go` (started, need to revert uncommitted partial work first)
2. Remove `agencDirpath = dirpath` from `ensureConfigured()`
3. Delete `getAgencContext()` from `mission_helpers.go`
4. Fix `readConfig()` / `readConfigWithComments()` to call `ensureConfigured()` + `config.GetAgencDirpath()` directly
5. Fix `ensureServerRunning()` to resolve path internally
6. Fix `mission_new.go` — revert subagent's parameter-threading, use `config.GetAgencDirpath()` instead
7. Fix `mission_update_config.go` — same as above
8. Fix `summary.go` — same as above
9. Fix `config_edit.go`, `config_get.go`, `config_set.go`, `config_unset.go` — call `ensureConfigured()` + `config.GetAgencDirpath()` instead of `getAgencContext()`
10. Fix `config_init.go:printConfigSummary` — needs `agencDirpath` as parameter (it's called from `runConfigInit` which has the value from `ensureConfigured()`)
11. Fix any remaining callers of `getAgencContext()` or the global
12. Build, test, commit, push

**Files with uncommitted changes that need to be reverted first:**

After `git checkout -- .`, the working tree should match commit `243c7d1`. Start the remaining work from there.
