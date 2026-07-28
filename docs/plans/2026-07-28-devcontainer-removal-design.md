# Devcontainer Removal — Design (agenc-ok7h)

**Bead:** agenc-ok7h. **Part of:** State Y shadow removal (`docs/plans/2026-07-28-state-y-shadow-removal-plan.md`, bead agenc-lh8p). **Follow-up filed:** agenc-vhy8 (close GH #17 + #8 as wontfix once this merges).
**Origin:** brainstormed in mission cc8e4c39-e2ca-45d5-8818-907de7cfbece, 2026-07-28.

## Goal & constraints

Rip out AgenC's devcontainer-reading infrastructure entirely. Kevin uses no devcontainer missions and wants the feature gone; it is also the last thing forcing the materialized-config + path-rewriting machinery, so its removal clears the way for the shadow flip (State Y). **This is a deletion, not a rewrite.** Hard constraint: **host (non-containerized) missions must be completely unaffected** — no regression to normal spawn / attach / reload. Resist "improving" adjacent code.

## Approach

**Single atomic deletion** (one coherent commit). The surface is fully mapped and cohesive; `make check` (with deadcode analysis) plus a host-mission `make e2e` verify it green in one step. If the diff proves unwieldy at review it factors cleanly into: entry-points → spawn path → package + `containerized` param → docs — but start single.

## Deletion surface (file:line)

### Delete outright — container-only

- **`internal/devcontainer/`** (whole package): `detection.go`, `overlay.go`, `project_path_encoding.go` + their 3 `_test.go` files. (`EncodeProjectPath` here is a container-only duplicate of the preserved `claudeconfig.ComputeProjectDirpath`.)
- **`internal/wrapper/devcontainer.go`** (whole file): `devcontainerState`, `detectAndSetupDevcontainer`, `devcontainerUp`, `devcontainerExecClaude`, `devcontainerStop`, `devcontainerRebuild`.
- **`cmd/mission_rebuild.go`** (the `agenc mission rebuild` command) + the `rebuildCmdStr` const (`cmd/command_str_consts.go:72`).
- **Palette builtin `rebuildContainer`** (`internal/config/agenc_config.go:158-162`) + its entry in the ordered builtins list (`agenc_config.go:225`).
- **`WrapperClient.Rebuild()`** (`internal/wrapper/client.go:58`) + any rebuild-only request type.
- **Wrapper socket container-only handlers + routes** (`internal/wrapper/socket.go`): `GET /prime` (`handlePrime`, :82, :105-113), `POST /claude-update/{event}` (`handleClaudeUpdateWithPathEvent`, :84, :173-207), `POST /rebuild` (`handleRebuild`, :85, :158-171). **Keep** `GET /status` and `POST /claude-update` (host path).
- **Wrapper** (`internal/wrapper/wrapper.go`): fields `devcontainer *devcontainerState` (:97) and `rebuilding atomic.Bool` (:80); `spawnClaudeInContainer` (:401-427); `handleRebuildCommand` (:535-576); the `"rebuild"` case in `handleCommand` (:526-527); the detect+up blocks in `setupRun` (:240-261); the devcontainer-stop in the `cleanup` closure (:303-308); the `isContainerized` branch in `spawnClaude` (:328, :334-337).
- **`internal/claudeconfig/overrides.go`**: `staticContainerHookEntries` var (:56), its `init` block (:76-98), and `BuildContainerHookEntries` (:118-128).
- **`internal/server/missions.go`**: the devcontainer-stop block in `handleDeleteMission` (:751-763) + the now-unused `internal/devcontainer` import.
- **`internal/wrapper/credential_sync.go`**: the two `w.rebuilding.Load()` guards (:64, :187) — removed with the field (judgment call #2 below).

### Simplify — drop the `containerized` param, collapse branches to host-only

- **`internal/claudeconfig/build.go`**: `BuildMissionConfigDir(…, containerized bool)` → drop the param; delete the container symlink→empty-dir branch (:123-132); make `WriteAgencHookScripts` unconditional (remove the `if !containerized` at :81-85). `buildMergedSettings(…, containerized)` → drop.
- **`internal/claudeconfig/merge.go`**: `MergeSettings(…, containerized)` and `MergeSettingsWithAgencOverrides(…, containerized)` → drop the param; `hookEntries` is always `BuildAgencHookEntries(...)` (:433-438).
- **`internal/wrapper/wrapper.go`**: `rebuildClaudeConfig(isContainerized)` → drop the param; `spawnClaude` always calls `spawnClaudeDirectly`.

### Preserve — looks container-related, isn't

- **`claudeconfig.GetPrimeContent()`** — the host `agenc prime` CLI (`cmd/prime.go:16`) uses it. Only the socket's `GET /prime` wrapper goes.
- **`claudeconfig.ComputeProjectDirpath`** — host path encoder (used by session readers; the shadow work's Increment 0 leans on it).
- **Host `POST /claude-update`** (JSON body) + `agenc mission send claude-update`.
- **Host SessionStart `agenc prime` hook** (`overrides.go:70-74`).

## Two judgment calls (decided; reversible)

1. **Simplify `BuildMissionConfigDir` / `MergeSettings` now** (remove the `containerized` param) even though the shadow work later deletes these functions — this is the "clear the way" the epic plan intends; it leaves a clean host-only path the flip then removes. Alternative (leave the param dangling) rejected — it's the opposite of the goal.
2. **Remove the `rebuilding` field and its two `credential_sync.go` reads now** even though `credential_sync.go` is doomed in the shadow work — a 2-line edit that avoids leaving an always-false atomic in the tree. (`remove dead code` per the software-engineer skill.)

## Tests

- Delete `internal/devcontainer/*_test.go` (3) with the package.
- `internal/claudeconfig/overrides_test.go` — remove `BuildContainerHookEntries` / `staticContainerHookEntries` assertions.
- `internal/wrapper/wrapper_integration_test.go` and `internal/server/claude_config_sync_test.go` — remove devcontainer / `containerized`-param assertions.
- Compile + `deadcode` catch any orphaned helper missed above.

## Docs

`docs/system-architecture.md`: delete the `internal/devcontainer/` package section (:439-445); drop "devcontainer rebuild" from the per-spawn text (:221, :493); remove the container variants from the hook + `GET /prime` + `POST /claude-update/{event}` endpoint descriptions (:257, :534, :546); remove the `containerized` branch mentions in "Per-mission config merging"; remove the `rebuilding`-field mention in the wrapper section. Update in the **same commit** as the code (repo rule).

## Verification

- `make check` — compile, vet, lint, **deadcode** (primary safety net for orphaned helpers), vulncheck, unit tests.
- `make e2e` — host missions still create / attach / reload (the real risk is nicking the host path while removing container branches).
- **Manual** (repo rule: keybinding-generation changes need it): confirm the command palette no longer shows `🐳 Rebuild Container` and keybindings regenerate cleanly.

## Post-merge

Once this ships, GH issues **#17** (swade1987) and **#8** (omar-augur) — both the strict-JSON-parse-of-`devcontainer.json` JSONC bug — become wontfix-by-removal (no code path parses `devcontainer.json` anymore). Close both, kindly, per bead **agenc-vhy8** (uses `/addressing-user-github-issue` + `/github-posting` + `/user-support-voice`). **Do not close until this merges.**
