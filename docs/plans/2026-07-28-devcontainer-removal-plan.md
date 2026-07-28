# Devcontainer Removal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove AgenC's devcontainer-reading infrastructure entirely so all missions run on the host path, clearing the materialized-config/path-rewriting machinery ahead of the State Y shadow flip.

**Architecture:** Pure deletion across three coherent slices — (1) the rebuild feature, (2) the container spawn path + `internal/devcontainer` package + container-only socket handlers, (3) the vestigial `containerized` config parameter and container hook variants. Each slice leaves the tree compiling and `make check`-green.

**Tech Stack:** Go; Makefile-driven build/check/e2e; Cobra CLI; unix-socket HTTP between CLI/wrapper/server.

**Spec:** `docs/plans/2026-07-28-devcontainer-removal-design.md` (bead agenc-ok7h). **Epic:** `docs/plans/2026-07-28-state-y-shadow-removal-plan.md` (agenc-lh8p). **Post-merge follow-up:** agenc-vhy8.

## Global Constraints

- **This is a deletion, not a rewrite.** Do not "improve" adjacent code; keep the diff traceable to removal.
- **Host (non-containerized) missions must be completely unaffected** — no regression to create / attach / reload.
- **Build only via the Makefile.** Run `make check` and `make e2e` with the Bash tool's `dangerouslyDisableSandbox: true` (they need the Go build cache). Never pass `--no-verify`.
- **Update `docs/system-architecture.md` in the same commit** as the code that invalidates each section (repo rule).
- **Preserve, do not delete:** `claudeconfig.GetPrimeContent()` (host `agenc prime` uses it), `claudeconfig.ComputeProjectDirpath`, the host `POST /claude-update` (JSON-body) socket path, and the host SessionStart `agenc prime` hook.
- **Deletion verification is at task end.** A partial deletion doesn't compile, so intra-task steps land together; the `make check` step at the end of each task is the gate. `deadcode` (part of `make check`) is the primary net for orphaned helpers.
- **Do NOT touch GitHub issues #17 / #8** in this plan. Their wontfix close is a separate, post-merge task (agenc-vhy8).
- **Commit messages:** concise summary line + a single `AgenC mission: <uuid>` trailer. No other trailers.

---

### Task 1: Remove the rebuild feature and its `rebuilding` state

Removes the user-triggered container rebuild end-to-end: CLI command, palette entry, wrapper-socket route, wrapper command handler, the `devcontainerRebuild` helper, and the `rebuilding` flag that only existed to pause credential sync during a rebuild.

**Files:**
- Delete: `cmd/mission_rebuild.go`
- Modify: `cmd/command_str_consts.go:72` (remove `rebuildCmdStr`)
- Modify: `internal/config/agenc_config.go:158-162` (remove `rebuildContainer` builtin) and `:225` (remove its ordered-list entry)
- Modify: `internal/wrapper/client.go:57-67` (remove `Rebuild()`)
- Modify: `internal/wrapper/socket.go` (remove `POST /rebuild` route `:85` and `handleRebuild` `:158-171`)
- Modify: `internal/wrapper/wrapper.go` (remove `handleRebuildCommand` `:535-576`, the `"rebuild"` case `:526-527`, and the `rebuilding atomic.Bool` field `:77-80`)
- Modify: `internal/wrapper/devcontainer.go` (remove `devcontainerRebuild` `:105-118`)
- Modify: `internal/wrapper/credential_sync.go` (remove the two `if w.rebuilding.Load() { continue }` guards at `:64-66` and `:187-189`)
- Modify: `docs/system-architecture.md` (remove the `POST /rebuild` endpoint bullet and any "Rebuild Container" palette mention)

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces: after this task, the wrapper struct no longer has a `rebuilding` field and `handleCommand` handles only `"claude_update"`. Task 2 relies on `devcontainerRebuild` being already gone.

- [ ] **Step 1: Delete the CLI command and its constant.** Remove the file `cmd/mission_rebuild.go`, then delete the `rebuildCmdStr = "rebuild"` line from `cmd/command_str_consts.go:72`.

- [ ] **Step 2: Remove the palette builtin.** In `internal/config/agenc_config.go`, delete the `"rebuildContainer": { ... }` map entry (`:158-162`) and remove the `"rebuildContainer",` string from the ordered builtins slice at `:225`.

- [ ] **Step 3: Remove the client method.** Delete `func (c *WrapperClient) Rebuild()` (`internal/wrapper/client.go:57-67`). Leave `postCommand` and `SendClaudeUpdate` untouched.

- [ ] **Step 4: Remove the socket route + handler.** In `internal/wrapper/socket.go`, delete the `mux.HandleFunc("POST /rebuild", handleRebuild(w, logger))` line (`:85`) and the entire `handleRebuild` function (`:158-171`).

- [ ] **Step 5: Remove the wrapper command handler and case.** In `internal/wrapper/wrapper.go`, delete the whole `handleRebuildCommand` method (`:535-576`) and the `case "rebuild": return w.handleRebuildCommand()` arm in `handleCommand` (`:526-527`), leaving `handleCommand` with the `"claude_update"` and `default` arms.

- [ ] **Step 6: Remove the `devcontainerRebuild` helper.** Delete `func devcontainerRebuild(...)` from `internal/wrapper/devcontainer.go:105-118`. (The rest of that file goes in Task 2.)

- [ ] **Step 7: Remove the `rebuilding` field and its readers.** Delete the `rebuilding atomic.Bool` field and its doc comment from the `Wrapper` struct (`internal/wrapper/wrapper.go:77-80`). In `internal/wrapper/credential_sync.go`, delete the guard blocks:
  ```go
  // watchCredentialUpwardSync loop (around :64)
  if w.rebuilding.Load() {
      continue
  }
  // watchCredentialDownwardSync loop (around :187)
  if w.rebuilding.Load() {
      continue
  }
  ```
  so both sync paths run unconditionally. Remove the now-unused `sync/atomic` import from `wrapper.go` **only if** no other `atomic.` reference remains (check first; `grep -n "atomic\." internal/wrapper/wrapper.go`).

- [ ] **Step 8: Update the architecture doc.** In `docs/system-architecture.md`, remove the `POST /rebuild` endpoint line from the wrapper HTTP API list and any "Rebuild Container" palette reference. Leave devcontainer-package and container-spawn text for Tasks 2–3.

- [ ] **Step 9: Verify green.** Run: `make check` (with `dangerouslyDisableSandbox: true`). Expected: PASS — compiles, `deadcode` clean, all unit tests pass. If `deadcode` flags a leftover (e.g. an orphaned helper reachable only from the removed rebuild path), delete it and re-run.

- [ ] **Step 10: Commit.**
  ```bash
  git add -A
  git commit -m "Remove the devcontainer rebuild feature (agenc-ok7h)

  AgenC mission: cc8e4c39-e2ca-45d5-8818-907de7cfbece"
  git pull --rebase && git push
  ```

---

### Task 2: Remove the container spawn path, the `internal/devcontainer` package, and the container-only socket handlers

Removes everything that reads or runs a container, passing `containerized=false` where the config parameter still exists (that parameter is deleted in Task 3). After this task, no code path detects, generates, starts, or execs a devcontainer.

**Files:**
- Modify: `internal/wrapper/wrapper.go` (collapse `spawnClaude`; remove `spawnClaudeInContainer`; remove the `detectAndSetupDevcontainer`/`devcontainerUp` blocks in `setupRun`; remove the devcontainer-stop in the `cleanup` closure; remove the `devcontainer *devcontainerState` field)
- Delete: `internal/wrapper/devcontainer.go`
- Delete: `internal/devcontainer/detection.go`, `internal/devcontainer/overlay.go`, `internal/devcontainer/project_path_encoding.go`, and their `_test.go` siblings
- Modify: `internal/wrapper/socket.go` (remove `GET /prime` route `:82` + `handlePrime` `:105-113`; remove `POST /claude-update/{event}` route `:84` + `handleClaudeUpdateWithPathEvent` `:173-207`)
- Modify: `internal/server/missions.go` (remove the devcontainer-stop block `:751-763` and the `internal/devcontainer` import)
- Test: `internal/wrapper/wrapper_integration_test.go`, `internal/server/claude_config_sync_test.go` (remove devcontainer assertions)
- Modify: `docs/system-architecture.md` (remove the `internal/devcontainer/` package section `:439-445`, the `GET /prime` and `POST /claude-update/{event}` endpoint lines, and container-spawn mentions in the wrapper narrative)

**Interfaces:**
- Consumes: Task 1's removal of `devcontainerRebuild` and the `rebuilding` field.
- Produces: after this task, `claudeconfig.BuildMissionConfigDir` / `MergeSettings` still take a `containerized bool` but are only ever called with `false`; `BuildContainerHookEntries` remains defined and textually referenced in the (never-taken) `if containerized` branch. Task 3 removes both.

- [ ] **Step 1: Collapse `spawnClaude` to the host path.** In `internal/wrapper/wrapper.go`, replace the body of `spawnClaude` (`:327-338`):
  ```go
  // BEFORE
  func (w *Wrapper) spawnClaude(isResume bool) error {
      isContainerized := w.devcontainer != nil
      if err := w.rebuildClaudeConfig(isContainerized); err != nil {
          return stacktrace.Propagate(err, "failed to rebuild claude-config before spawn")
      }
      if isContainerized {
          return w.spawnClaudeInContainer(isResume)
      }
      return w.spawnClaudeDirectly(isResume)
  }
  // AFTER
  func (w *Wrapper) spawnClaude(isResume bool) error {
      if err := w.rebuildClaudeConfig(false); err != nil {
          return stacktrace.Propagate(err, "failed to rebuild claude-config before spawn")
      }
      return w.spawnClaudeDirectly(isResume)
  }
  ```
  (The `false` literal is temporary — Task 3 drops the parameter.)

- [ ] **Step 2: Remove `spawnClaudeInContainer`.** Delete the whole method (`internal/wrapper/wrapper.go:401-427`).

- [ ] **Step 3: Remove devcontainer setup/teardown from the lifecycle.** In `setupRun` (`internal/wrapper/wrapper.go`), delete the `detectAndSetupDevcontainer` call and the `dcState`/`devcontainerUp` blocks (`:240-261`). In the `cleanup` closure, delete the `if w.devcontainer != nil { devcontainerStop... }` block (`:303-308`). Remove the `devcontainer *devcontainerState` field and its comment from the `Wrapper` struct (`:95-97`).

- [ ] **Step 4: Delete the wrapper devcontainer file.** Remove `internal/wrapper/devcontainer.go` in full (`devcontainerState`, `detectAndSetupDevcontainer`, `devcontainerUp`, `devcontainerExecClaude`, `devcontainerStop` — `devcontainerRebuild` already gone in Task 1).

- [ ] **Step 5: Remove the container-only socket handlers.** In `internal/wrapper/socket.go`, delete the `mux.HandleFunc("GET /prime", handlePrime())` line (`:82`) and the `handlePrime` function (`:105-113`); delete the `mux.HandleFunc("POST /claude-update/{event}", handleClaudeUpdateWithPathEvent(w, logger))` line (`:84`) and the `handleClaudeUpdateWithPathEvent` function (`:173-207`). Keep `GET /status` and `POST /claude-update`. Remove the now-unused `claudeconfig` import from `socket.go` **only if** nothing else in the file references it (`grep -n "claudeconfig\." internal/wrapper/socket.go`).

- [ ] **Step 6: Remove the server delete-handler devcontainer block.** In `internal/server/missions.go`, delete the `if _, found := devcontainer.DetectDevcontainer(agentDirpath); found { ... }` block in `handleDeleteMission` (`:751-763`) and remove the `internal/devcontainer` import.

- [ ] **Step 7: Delete the devcontainer package.** Remove `internal/devcontainer/detection.go`, `overlay.go`, `project_path_encoding.go`, `detection_test.go`, `overlay_test.go`, `project_path_encoding_test.go` (i.e. the whole `internal/devcontainer/` directory).

- [ ] **Step 8: Strip devcontainer assertions from tests.** In `internal/wrapper/wrapper_integration_test.go` and `internal/server/claude_config_sync_test.go`, remove any test cases or assertions that reference devcontainer detection, `spawnClaudeInContainer`, the `devcontainer` field, or the container socket routes. Run `grep -rn "devcontainer\|[Cc]ontaineriz\|/prime\|claude-update/{event}" internal/wrapper/*_test.go internal/server/*_test.go` and clear each hit; delete now-empty test functions rather than leaving no-op bodies.

- [ ] **Step 9: Update the architecture doc.** In `docs/system-architecture.md`: delete the `### internal/devcontainer/` package section (`:439-445`); remove the `GET /prime` endpoint line (`:257`) and the `POST /claude-update/{event}` mention; remove "devcontainer rebuild" from the per-spawn narrative (`:221`) and the container-curl variant from the SessionStart/hook description (`:534, :546`).

- [ ] **Step 10: Verify green.** Run: `make check` (with `dangerouslyDisableSandbox: true`). Expected: PASS. `deadcode` must be clean — if `handlePrime`, `devcontainerExecClaude`, or any devcontainer helper is flagged, it means a reference was missed; find and remove it.

- [ ] **Step 11: Commit.**
  ```bash
  git add -A
  git commit -m "Remove container spawn path and internal/devcontainer package (agenc-ok7h)

  AgenC mission: cc8e4c39-e2ca-45d5-8818-907de7cfbece"
  git pull --rebase && git push
  ```

---

### Task 3: Remove the `containerized` parameter and container hook variants; final verification

Deletes the now-vestigial `containerized bool` threaded through the config builders (always `false` after Task 2) and the container hook-entry machinery, collapsing every branch to the host path. Ends with the full `make check` + `make e2e` + manual palette verification the bead's acceptance criteria require.

**Files:**
- Modify: `internal/wrapper/wrapper.go` (`rebuildClaudeConfig` signature + `spawnClaude` call site)
- Modify: `internal/claudeconfig/build.go` (`BuildMissionConfigDir`, `buildMergedSettings` signatures; collapse the symlink/empty-dir branch; make `WriteAgencHookScripts` unconditional)
- Modify: `internal/claudeconfig/merge.go` (`MergeSettings`, `MergeSettingsWithAgencOverrides` signatures; hook-entries collapse)
- Modify: `internal/claudeconfig/overrides.go` (remove `staticContainerHookEntries`, its `init` block, and `BuildContainerHookEntries`)
- Test: `internal/claudeconfig/overrides_test.go` (remove container-hook tests); any `build_test.go`/`merge_test.go` call sites passing the `containerized` arg
- Modify: `docs/system-architecture.md` (remove `containerized` branch mentions in "Per-mission config merging")

**Interfaces:**
- Consumes: Task 2's guarantee that every caller passes `containerized=false`.
- Produces: the final host-only config builders — `BuildMissionConfigDir(agencDirpath, missionID, trustedMcpServers)`, `MergeSettings(userSettingsData, modsSettingsData, agencDirpath, agentDirpath, claudeConfigDirpath)`, `MergeSettingsWithAgencOverrides(settingsData, agencDirpath, agentDirpath, claudeConfigDirpath)`, `rebuildClaudeConfig()`.

- [ ] **Step 1: Drop the param from the wrapper.** In `internal/wrapper/wrapper.go`, change `func (w *Wrapper) rebuildClaudeConfig(isContainerized bool) error` to `rebuildClaudeConfig()`, delete the internal use of `isContainerized`, and change the `BuildMissionConfigDir(...)` call inside it to drop the trailing `isContainerized` arg. Update the `spawnClaude` call from `w.rebuildClaudeConfig(false)` to `w.rebuildClaudeConfig()`.

- [ ] **Step 2: Drop the param from `build.go` and collapse the symlink branch.** In `internal/claudeconfig/build.go`:
  - Change `BuildMissionConfigDir(agencDirpath string, missionID string, trustedMcpServers *config.TrustedMcpServers, containerized bool)` to remove `containerized`.
  - Make the hook-scripts write unconditional — replace `if !containerized { WriteAgencHookScripts(...) }` (`:81-85`) with a plain `if err := WriteAgencHookScripts(claudeConfigDirpath); err != nil { ... }`.
  - Replace the `if containerized { create empty dirs } else { symlink loop }` block (`:123-141`) with just the `else` symlink loop:
    ```go
    for _, dirName := range symlinkDirNames {
        if err := symlinkToGlobalClaudeDir(claudeConfigDirpath, dirName); err != nil {
            return stacktrace.Propagate(err, "failed to symlink %s", dirName)
        }
    }
    ```
  - Change `buildMergedSettings(...)` to drop its `containerized` param and drop it from the `MergeSettings(...)` call it makes.

- [ ] **Step 3: Drop the param from `merge.go` and collapse the hook-entries choice.** In `internal/claudeconfig/merge.go`:
  - Remove `containerized` from `MergeSettings(...)` and from the `MergeSettingsWithAgencOverrides(...)` call it makes.
  - Remove `containerized` from `MergeSettingsWithAgencOverrides(...)` and replace the branch (`:433-438`):
    ```go
    // BEFORE
    var hookEntries map[string]json.RawMessage
    if containerized {
        hookEntries = BuildContainerHookEntries()
    } else {
        hookEntries = BuildAgencHookEntries(claudeConfigDirpath)
    }
    // AFTER
    hookEntries := BuildAgencHookEntries(claudeConfigDirpath)
    ```

- [ ] **Step 4: Remove the container hook machinery.** In `internal/claudeconfig/overrides.go`, delete the `staticContainerHookEntries` var (`:55-56`), its entire construction block in `init` (`:76-98`), and the `BuildContainerHookEntries` function (`:118-128`). Verify `init` still builds `staticAgencHookEntries` and the `fmt` import is still needed (`grep -n "fmt\." internal/claudeconfig/overrides.go`); remove the import if now unused.

- [ ] **Step 5: Fix the tests.** In `internal/claudeconfig/overrides_test.go`, delete tests asserting on `BuildContainerHookEntries` / `staticContainerHookEntries`. In any `build_test.go` / `merge_test.go`, remove the trailing `containerized` argument (usually `false`) from `BuildMissionConfigDir` / `MergeSettings` / `MergeSettingsWithAgencOverrides` call sites. Find them: `grep -rn "MergeSettings\|MergeSettingsWithAgencOverrides\|BuildMissionConfigDir\|BuildContainerHookEntries" internal/claudeconfig/*_test.go`.

- [ ] **Step 6: Update the architecture doc.** In `docs/system-architecture.md`, remove references to the `containerized` branch / container variant in the "Per-mission config merging" and "Idle detection via socket" sections (`:493-534`), so the narrative describes only the host path.

- [ ] **Step 7: Verify compile + units + deadcode.** Run: `make check` (with `dangerouslyDisableSandbox: true`). Expected: PASS with `deadcode` clean — this is the confirmation that no container-only symbol survives anywhere in the tree.

- [ ] **Step 8: Verify host missions end-to-end.** Run: `make e2e` (with `dangerouslyDisableSandbox: true`). Expected: PASS — host missions create, attach, and reload unaffected. If any host-mission test regresses, a host branch was nicked during the container-branch collapse; fix before committing.

- [ ] **Step 9: Manual palette check (repo rule for keybinding-generation changes).** Open the command palette in a live tmux session and confirm the `🐳 Rebuild Container` entry is gone and keybindings regenerate without error. Report the result to the user; do not mark the task done until confirmed.

- [ ] **Step 10: Commit.**
  ```bash
  git add -A
  git commit -m "Remove containerized config parameter and container hook variants (agenc-ok7h)

  AgenC mission: cc8e4c39-e2ca-45d5-8818-907de7cfbece"
  git pull --rebase && git push
  ```

- [ ] **Step 11: Close the bead and note the follow-up.** Close agenc-ok7h with a substantive reason. Leave agenc-vhy8 (close GH #17 + #8 as wontfix) OPEN — it is unblocked by this merge but is a separate, user-facing post-merge task.

---

## Self-Review

**Spec coverage** (against `2026-07-28-devcontainer-removal-design.md`):
- Delete-outright list → Task 1 (rebuild feature) + Task 2 (spawn path, package, socket handlers, server block). ✓
- Simplify list (`containerized` param, branch collapse) → Task 3. ✓
- Two judgment calls: `containerized` param removed now → Task 3; `rebuilding` field + `credential_sync` reads removed now → Task 1. ✓
- Preserved trio (`GetPrimeContent`, `ComputeProjectDirpath`, host `POST /claude-update`, host prime hook) → never touched; called out in Global Constraints. ✓
- Tests → Task 2 Step 8, Task 3 Step 5. ✓
- Docs same-commit → each task has an arch-doc step. ✓
- Verification (`make check` + `make e2e` + manual palette) → Task 3 Steps 7–9. ✓
- Post-merge #17/#8 → out of code scope, flagged Task 3 Step 11 + agenc-vhy8. ✓

**Placeholder scan:** No TBD/TODO; every deletion cites file:line; every simplification shows before/after. ✓

**Type/signature consistency:** Final signatures declared in Task 3 "Produces" match the edits in Steps 1–3 (`rebuildClaudeConfig()`, `BuildMissionConfigDir(agencDirpath, missionID, trustedMcpServers)`, `MergeSettings(userSettingsData, modsSettingsData, agencDirpath, agentDirpath, claudeConfigDirpath)`). ✓
