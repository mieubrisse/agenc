Wrapper Auto-Regen of Per-Mission Claude Config — Implementation Plan
======================================================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the wrapper unconditionally rebuild the per-mission `claude-config/` directory from the shadow repo on every Claude spawn, kill the now-redundant `agenc mission reconfig` command, and update agent-facing docs so every agent knows how to self-reload after a `~/.claude` change.

**Architecture:** The server already runs an fsnotify watcher (`internal/server/config_watcher.go`) that ingests `~/.claude` → `claude-config-shadow/` git repo on startup and on every change. We hoist the existing containerized-only `BuildMissionConfigDir` call in `wrapper.go:331-338` out of its `if isContainerized` branch so it runs for *every* spawn (initial start, tmux-respawn-pane reload, and devcontainer rebuild). The wrapper also reads the shadow's HEAD commit, writes it to the mission's `config_commit` DB column, and logs it. CLI callers of `EnsureShadowRepo` and `BuildMissionConfigDir` are removed; the `mission reconfig` / `update` / `update-config` command is deleted entirely.

**Tech Stack:** Go, fsnotify, git, Cobra (CLI), existing `internal/server` HTTP client, existing `internal/claudeconfig` build/shadow helpers.

**Reference:** `docs/plans/2026-05-06-wrapper-auto-regen-claude-config-design.md`

---

## Pre-flight

Before starting any task: read the design doc at `docs/plans/2026-05-06-wrapper-auto-regen-claude-config-design.md` and these existing files for context:

- `internal/wrapper/wrapper.go` — the spawn path being modified (especially `spawnClaude` at line 326)
- `internal/claudeconfig/build.go` — `BuildMissionConfigDir`, `GetShadowRepoCommitHash`, `EnsureShadowRepo`
- `internal/claudeconfig/shadow.go` — `IngestFromClaudeDir`, `commitShadowChanges`
- `internal/server/config_watcher.go` — the fsnotify watcher already running server-side
- `internal/server/client.go` — `Client.UpdateMission`
- `internal/server/missions.go:1046-1075` — `UpdateMissionRequest` shape and PATCH handler
- `cmd/mission_new.go:80` — current direct `EnsureShadowRepo` call to be removed
- `internal/mission/mission.go:54` — current direct `BuildMissionConfigDir` call to be removed
- `cmd/mission_update_config.go` — file to be deleted
- `internal/claudeconfig/agent_instructions.md` — Configuration Boundaries section to rewrite
- `internal/claudeconfig/adjutant_claude.md` — paragraphs about reconfig to rewrite
- `docs/system-architecture.md` — Claude config section to update

**Build/test commands** (per `CLAUDE.md` — these commands need `dangerouslyDisableSandbox: true` because they touch `~/.cache/go-build`):

- `make build` — full build (genprime + check + compile)
- `make check` — quality checks only (formatting, vet, lint, vulncheck, deadcode, tests)
- `make e2e` — full E2E (builds binary, creates test env, runs `scripts/e2e-test.sh`, tears down)

**Coding conventions:** Invoke `/alembiq:software-engineer` and `/alembiq:go-coding` before any Go edit. Build only via `make` — never `go build` directly.

**Commits:** Single-line messages, no co-author lines. After every commit: `git pull --rebase && git push`.

---

## Task 1: Wrapper — unconditional rebuild on every spawn

**Files:**
- Modify: `internal/wrapper/wrapper.go:326-344` (the `spawnClaude` function)
- Test: `internal/wrapper/wrapper_integration_test.go` (extend with a new test)

**Goal:** Replace the `if isContainerized { BuildMissionConfigDir... }` block with an unconditional preamble that rebuilds the per-mission `claude-config/`, updates the mission's `config_commit` in the DB, and logs the commit.

**Step 1: Read the current `spawnClaude`**

Run: `Read internal/wrapper/wrapper.go offset=318 limit=40`

Confirm the current code matches this shape (lines may shift slightly):

```go
func (w *Wrapper) spawnClaude(isResume bool) error {
    isContainerized := w.devcontainer != nil

    // Regenerate claude-config before every containerized spawn so the
    // latest global config takes effect on reload.
    if isContainerized {
        trustedMcpServers := w.loadTrustedMcpServers()
        if err := claudeconfig.BuildMissionConfigDir(
            w.agencDirpath, w.missionID, trustedMcpServers, isContainerized,
        ); err != nil {
            return stacktrace.Propagate(err, "failed to regenerate claude-config for containerized mission")
        }
    }

    if isContainerized {
        return w.spawnClaudeInContainer(isResume)
    }
    return w.spawnClaudeDirectly(isResume)
}
```

**Step 2: Find an existing wrapper unit/integration test to model the new test on**

Run: `grep -n "func Test" internal/wrapper/wrapper_integration_test.go | head -10`

Pick the simplest passing test (one that constructs a `Wrapper` with a fake/mock server) as a template.

**Step 3: Write the failing test**

Add a new test `TestSpawnClaude_RebuildsConfigAndLogsCommit` to `internal/wrapper/wrapper_integration_test.go`.

The test should:
1. Set up a temporary `agencDirpath` with: a mock `~/.claude/CLAUDE.md` containing known content, a shadow repo (call `claudeconfig.EnsureShadowRepo(agencDirpath)` to bootstrap it — note: the test must point `IngestFromClaudeDir` at the temp `~/.claude`, which means setting `HOME` for the test or wiring up the source path explicitly via `IngestFromClaudeDir`), a mission directory under `$AGENC_DIRPATH/missions/<id>/`, and a fake server client that records calls to `UpdateMission`.
2. Construct a `Wrapper` whose `client` is the fake, `agencDirpath` and `missionID` point at the temp setup, and `devcontainer` is nil (non-containerized).
3. Replace the `spawnClaudeDirectly`/`spawnClaudeInContainer` tail so the test does not actually launch a Claude process — either via a hook the wrapper exposes for tests, or by structuring the test to call only the new preamble (extracted into a private helper if cleanest — see Step 5).
4. Assert that after running the preamble:
   - `<missionDir>/claude-config/CLAUDE.md` exists and contains the known content from `~/.claude/CLAUDE.md`.
   - The fake `UpdateMission` was called once with a non-empty `ConfigCommit` value matching `claudeconfig.GetShadowRepoCommitHash(agencDirpath)`.
   - A log entry at `Info` level was emitted with key `shadow_commit` (use a buffered logger or a test-friendly logger fake).

**Step 4: Run the test to verify it fails**

Run: `make check 2>&1 | grep -A5 TestSpawnClaude_RebuildsConfigAndLogsCommit` (with `dangerouslyDisableSandbox: true`).

Expected: FAIL — the wrapper currently only rebuilds in the containerized path; the non-containerized test will see no `claude-config/CLAUDE.md`, no `UpdateMission` call, and no log entry.

**Step 5: Implement — refactor `spawnClaude` for testability and behavior**

Extract the new preamble into a private helper to make it independently testable, then call it from `spawnClaude`. Replace lines 326-344 with:

```go
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

// rebuildClaudeConfig regenerates the per-mission claude-config/ directory
// from the shadow repo, updates the mission's config_commit in the DB, and
// logs the commit hash. Runs before every Claude spawn so each reload picks
// up the latest ~/.claude state (the server's config_watcher keeps the shadow
// current via fsnotify).
func (w *Wrapper) rebuildClaudeConfig(isContainerized bool) error {
    commitHash := claudeconfig.GetShadowRepoCommitHash(w.agencDirpath)
    if commitHash == "" {
        return stacktrace.NewError("shadow repo missing or empty at '%s' — restart the agenc server",
            claudeconfig.GetShadowRepoDirpath(w.agencDirpath))
    }

    trustedMcpServers := w.loadTrustedMcpServers()
    if err := claudeconfig.BuildMissionConfigDir(
        w.agencDirpath, w.missionID, trustedMcpServers, isContainerized,
    ); err != nil {
        return stacktrace.Propagate(err, "failed to build per-mission claude-config")
    }

    if err := w.client.UpdateMission(w.missionID, server.UpdateMissionRequest{
        ConfigCommit: &commitHash,
    }); err != nil {
        // Log and continue — on-disk config is correct; only the DB column drifts stale.
        w.logger.Warn("failed to update config_commit in DB; on-disk config is correct",
            "mission_id", w.missionID, "error", err)
    }

    shortHash := commitHash
    if len(shortHash) > 12 {
        shortHash = shortHash[:12]
    }
    w.logger.Info("claude-config rebuilt", "shadow_commit", shortHash)
    return nil
}
```

If `server.UpdateMissionRequest` is not yet imported in `wrapper.go`, add `"github.com/odyssey/agenc/internal/server"` to the imports.

**Step 6: Run the test to verify it passes**

Run: `make check` (with `dangerouslyDisableSandbox: true`).

Expected: PASS — the new test passes, all other wrapper tests still pass, no lint/vet/deadcode failures.

**Step 7: Commit**

```bash
git add internal/wrapper/wrapper.go internal/wrapper/wrapper_integration_test.go
git commit -m "Wrapper: rebuild per-mission claude-config on every spawn"
git pull --rebase && git push
```

---

## Task 2: Remove redundant `EnsureShadowRepo` call from CLI mission new

**Files:**
- Modify: `cmd/mission_new.go:79-82`

**Goal:** The server's `config_watcher` already calls `EnsureShadowRepo` on startup. The CLI's direct call here is leftover.

**Step 1: Confirm the current code**

Run: `Read cmd/mission_new.go offset=70 limit=20`

Confirm lines 79-82 contain:

```go
// Ensure shadow repo is initialized (auto-creates from ~/.claude if needed)
if err := claudeconfig.EnsureShadowRepo(agencDirpath); err != nil {
    return stacktrace.Propagate(err, "failed to ensure shadow repo")
}
```

**Step 2: Check the import**

Run: `grep -n "claudeconfig" cmd/mission_new.go`

Note whether `claudeconfig` is imported only for this call or has other uses in the file. If only used here, remove the import after the deletion.

**Step 3: Delete the lines**

Use the `Edit` tool to remove lines 79-82 (the comment and the if-block). Remove the `claudeconfig` import if unused after deletion.

**Step 4: Run `make check`**

Run: `make check` (with `dangerouslyDisableSandbox: true`).

Expected: PASS — no broken references. If `claudeconfig` is still imported elsewhere in the file, no import change needed.

**Step 5: Commit**

```bash
git add cmd/mission_new.go
git commit -m "CLI: drop redundant EnsureShadowRepo call from mission new (server watcher covers it)"
git pull --rebase && git push
```

---

## Task 3: Remove redundant `BuildMissionConfigDir` call from `internal/mission/mission.go`

**Files:**
- Modify: `internal/mission/mission.go:41-58`

**Goal:** The wrapper now rebuilds on every spawn, so pre-building at mission-create time is dead work.

**Step 1: Read the current code**

Run: `Read internal/mission/mission.go offset=20 limit=60`

Confirm the section that loads `trustedMcpServers` and calls `BuildMissionConfigDir`.

**Step 2: Delete the dead block**

Remove the lines that currently look like:

```go
// Look up MCP trust config for this repo
var trustedMcpServers *config.TrustedMcpServers
if gitRepoName != "" {
    cfg, _, err := config.ReadAgencConfig(agencDirpath)
    if err == nil {
        if rc, ok := cfg.GetRepoConfig(gitRepoName); ok {
            trustedMcpServers = rc.TrustedMcpServers
        }
    }
}

// Build per-mission claude config directory from shadow repo
isContainerized := false // containerization is set up later by the wrapper
if err := claudeconfig.BuildMissionConfigDir(agencDirpath, missionID, trustedMcpServers, isContainerized); err != nil {
    return "", stacktrace.Propagate(err, "failed to build per-mission claude config directory")
}
```

The wrapper handles all of this. The trustedMcpServers lookup happens inside the wrapper via `w.loadTrustedMcpServers()`.

**Step 3: Remove now-unused imports**

After the deletion, run `goimports` (handled by `make check`'s formatter) or manually remove any imports that are no longer used (likely `claudeconfig` and possibly `config` if no other callers in the file).

**Step 4: Run `make check`**

Run: `make check` (with `dangerouslyDisableSandbox: true`).

Expected: PASS. If a test in `internal/mission/` was asserting `claude-config/` exists after `Create`, update or delete that assertion (the wrapper now does this work).

**Step 5: Commit**

```bash
git add internal/mission/mission.go
git commit -m "mission.Create: stop pre-building claude-config (wrapper handles it on spawn)"
git pull --rebase && git push
```

---

## Task 4: Delete the `mission reconfig` command and its registration

**Files:**
- Delete: `cmd/mission_update_config.go`
- Delete: `cmd/mission_update_config_test.go` (if it exists)
- Modify: wherever `missionUpdateConfigCmd` is registered (likely `cmd/mission.go` — search for `missionUpdateConfigCmd`)
- Modify: `cmd/command_str_consts.go` — remove `reconfigCmdStr`, `updateCmdStr`, `updateConfigCmdStr` if no longer referenced anywhere

**Goal:** Remove the deprecated CLI surface entirely.

**Step 1: Find all references**

Run: `grep -rn "missionUpdateConfigCmd\|reconfigCmdStr\|updateConfigCmdStr\|mission_update_config\|mission update-config\|mission reconfig\|mission update " --include="*.go" --include="*.sh" --include="*.md"`

Note every hit. Plan to remove or update each one.

**Step 2: Delete the command file and its test**

Run: `rm cmd/mission_update_config.go` (and `cmd/mission_update_config_test.go` if it exists).

**Step 3: Remove the registration**

Open the file containing `missionUpdateConfigCmd.AddCommand` (likely `cmd/mission.go`). Use `Edit` to delete that line.

**Step 4: Remove the command-string constants**

Open `cmd/command_str_consts.go`. Remove `reconfigCmdStr`, `updateCmdStr`, `updateConfigCmdStr` if they exist there and are no longer referenced. Run `grep -n` first to confirm no other code references them.

**Step 5: Run `make check`**

Run: `make check` (with `dangerouslyDisableSandbox: true`).

Expected: PASS. Compilation errors here mean a remaining reference — track it down and remove or update it.

**Step 6: Commit**

```bash
git add -A
git commit -m "Delete agenc mission reconfig (auto-regen on reload makes it redundant)"
git pull --rebase && git push
```

---

## Task 5: Update agent-facing instructions — `agent_instructions.md`

**Files:**
- Modify: `internal/claudeconfig/agent_instructions.md` — the **Configuration Boundaries** section (currently around lines 142-146)

**Goal:** Tell every agent in every mission that `~/.claude` is the canonical config home and that they can self-reload via `agenc mission reload --async --prompt "..."`.

**Step 1: Read the current section**

Run: `Read internal/claudeconfig/agent_instructions.md offset=140 limit=15`

Confirm the section currently reads:

```markdown
Configuration Boundaries
------------------------

Your mission's `claude-config/` directory is **read-only**. It contains the Claude Code configuration that AgenC assembled for this mission — you cannot modify it directly. If you or the user needs to change Claude Code settings, skills, hooks, or the global CLAUDE.md, modify the source files in `~/.claude/` instead and then use the **"Reconfig & Reload"** palette command (accessible via the tmux command palette) to rebuild and apply the updated configuration to the running mission.
```

**Step 2: Replace the section body**

Use `Edit` to replace the body of the Configuration Boundaries section (everything between the heading and the next `---` separator) with the following text. Note: keep the existing `Configuration Boundaries` header line and underline intact.

```markdown
### `~/.claude` is canonical, the per-mission snapshot is read-only

Every mission gets a snapshot of the user's Claude config at
`$AGENC_DIRPATH/missions/$MISSION_UUID/claude-config/`. The `CLAUDE_CONFIG_DIR`
env var inside your mission points at that snapshot — but that snapshot is
**read-only and rebuilt from `~/.claude/` on every Claude reload**. Any direct
edit you make to the snapshot will be wiped out the next time the mission
reloads. To change global Claude config (CLAUDE.md, settings.json, skills,
hooks, commands, agents), edit the source files in `~/.claude/`. The AgenC
server watches `~/.claude/` with fsnotify and propagates changes into the
shadow repo automatically; the wrapper rebuilds your snapshot from the shadow
on every spawn.

### Reloading yourself to pick up config changes

When you (or the user) change `~/.claude/` and the mission needs to pick up
the new config, reload yourself:

```bash
{{CLI_NAME}} mission reload --async --prompt "<what to do after reload>" ${{MISSION_UUID_ENV_VAR}}
```

Always pass `--async`. A synchronous self-reload kills Claude mid-tool-call
and discards the result of the bash invocation that triggered it; `--async`
queues the reload for the next idle, so the calling tool result lands cleanly
and the prompt arrives on the next turn.

The `--prompt` flag carries your intent across the reload boundary — use it
to tell post-reload-you what to continue doing (e.g., `--prompt "continue
implementing the plan in docs/plans/foo.md from Task 3"`). Without it, the
reloaded session has no follow-up instruction.

There is no separate "reconfig" step. The wrapper rebuilds the per-mission
config directory from the shadow repo automatically on every reload.
```

**Step 3: Verify template variables are correct**

`agent_instructions.md` uses `{{CLI_NAME}}` and `{{MISSION_UUID_ENV_VAR}}` placeholders that get substituted by `internal/claudeconfig/agent_instructions.go`. Make sure the new text uses these exact placeholders.

Run: `grep -n "CLI_NAME\|MISSION_UUID_ENV_VAR" internal/claudeconfig/agent_instructions.go` to confirm the substitution variables.

**Step 4: Run `make check`**

Run: `make check` (with `dangerouslyDisableSandbox: true`).

Expected: PASS. The `genprime` step regenerates `prime_content.md` from the docs tree; if any rendering test asserts content, update it. (Search: `grep -rn "Reconfig & Reload\|palette command" internal/claudeconfig/`.)

**Step 5: Commit**

```bash
git add internal/claudeconfig/agent_instructions.md
git commit -m "Agent instructions: document ~/.claude canonical home and self-reload pattern"
git pull --rebase && git push
```

---

## Task 6: Update Adjutant prompt — `adjutant_claude.md`

**Files:**
- Modify: `internal/claudeconfig/adjutant_claude.md` — the trustedMcpServers paragraph (around line 180) and the AgenC-Specific Claude Instructions paragraph (around line 210)

**Goal:** Strip `agenc mission reconfig` from the Adjutant's vocabulary and replace with auto-regen-on-reload.

**Step 1: Find both passages**

Run: `grep -n "mission reconfig\|reconfig\|must be restarted" internal/claudeconfig/adjutant_claude.md`

Expected hits at lines ~180 and ~210 (per the design doc reference).

**Step 2: Rewrite the trustedMcpServers paragraph**

Find the line that currently reads (around line 180):

> **How it works:** When a mission is created, AgenC checks the repo's `trustedMcpServers` config and writes the appropriate consent entries into the mission's `.claude.json`. This means the trust setting applies to all **new** missions — existing missions are not retroactively updated unless their config is rebuilt with `agenc mission reconfig`.

Replace the trailing sentence so the full paragraph reads:

> **How it works:** When a mission is created, AgenC checks the repo's `trustedMcpServers` config and writes the appropriate consent entries into the mission's `.claude.json`. The trust setting applies to all **new** missions immediately. Existing running missions pick up the new trust setting on their next reload — the wrapper rebuilds the per-mission config directory from the shadow repo on every Claude spawn, so no separate reconfig step is required.

**Step 3: Rewrite the "When changes take effect" paragraph**

Find the line that currently reads (around line 210):

> **When changes take effect:** New missions pick up changes automatically. Existing missions keep their config snapshot from creation time. To propagate changes to existing missions, run `agenc mission reconfig`. Running missions must be restarted after reconfig.

Replace with:

> **When changes take effect:** New missions pick up changes automatically. Running missions pick up changes on their next reload — the wrapper rebuilds the per-mission `claude-config/` directory from the shadow repo on every Claude spawn. To apply a change to a running mission immediately, ask the user to reload it (or, if the agent is reloading itself in response to a config change, run `agenc mission reload --async --prompt "<continuation>"`).

**Step 4: Sanity-check for any remaining `mission reconfig` references**

Run: `grep -n "mission reconfig\|reconfig" internal/claudeconfig/adjutant_claude.md`

If any remain, audit and remove or update them.

**Step 5: Run `make check`**

Run: `make check` (with `dangerouslyDisableSandbox: true`).

Expected: PASS. Adjutant tests in `internal/claudeconfig/adjutant_test.go` may assert specific text; update them if so.

**Step 6: Commit**

```bash
git add internal/claudeconfig/adjutant_claude.md
git commit -m "Adjutant prompt: replace mission reconfig with auto-regen-on-reload model"
git pull --rebase && git push
```

---

## Task 7: Update `docs/system-architecture.md`

**Files:**
- Modify: `docs/system-architecture.md` — the section describing Claude config (search for "claude-config" or "shadow repo")

**Goal:** Reflect the new flow — server-owned fsnotify watcher → shadow repo → wrapper rebuilds per-mission on every spawn — and note that `mission reconfig` is gone.

**Step 1: Locate the relevant section**

Run: `grep -n "claude-config\|shadow\|reconfig" docs/system-architecture.md`

**Step 2: Update the section**

Edit the relevant section so it explains:

- `~/.claude/` is the canonical home of global Claude config.
- The server runs an fsnotify watcher (`internal/server/config_watcher.go`) that ingests `~/.claude/` into a shadow git repo at `$AGENC_DIRPATH/claude-config-shadow/` on startup and on every change (debounced).
- The wrapper rebuilds each mission's `$AGENC_DIRPATH/missions/<id>/claude-config/` from the shadow repo on every Claude spawn (initial start, tmux-respawn-pane reload, and devcontainer rebuild).
- The wrapper writes the shadow's HEAD commit to the mission's `config_commit` DB column on every rebuild and logs the short hash.
- There is no separate manual "reconfig" step. The `agenc mission reconfig` command was removed.

Keep the doc filepath-level (no code snippets, per `CLAUDE.md`'s architecture-doc rule).

**Step 3: Run `make check`**

Run: `make check` (with `dangerouslyDisableSandbox: true`).

Expected: PASS. (No code-test impact, but the deadcode/lint passes still need to pass.)

**Step 4: Commit**

```bash
git add docs/system-architecture.md
git commit -m "Architecture doc: document wrapper auto-regen flow, remove reconfig"
git pull --rebase && git push
```

---

## Task 8: E2E tests — remove reconfig coverage, add auto-regen regression

**Files:**
- Modify: `scripts/e2e-test.sh`

**Goal:** Stop testing the deleted `mission reconfig` command. Add a regression test that proves `~/.claude` edits propagate into a mission's `claude-config/` after a reload.

**Step 1: Find existing reconfig test cases**

Run: `grep -n "reconfig\|update-config\|mission update " scripts/e2e-test.sh`

**Step 2: Delete those test cases**

Use `Edit` to remove every test block (each `run_test*` invocation plus the surrounding `echo "--- ... ---"` headers if the entire section becomes empty) that exercises `mission reconfig`, `mission update`, or `mission update-config`.

**Step 3: Add the auto-regen regression test**

Append a new section to `scripts/e2e-test.sh` (before the final tear-down). Use the existing `${agenc_test}` variable and helper functions. The test:

```bash
echo "--- Auto-regen of claude-config on reload ---"

# Create a mission to test against. Use a blank mission so we don't depend on
# the user's real repos. Capture the mission ID from the create output.
mission_id="$(${agenc_test} mission new --blank --headless --prompt "wait" 2>&1 | grep -oE '[0-9a-f]{8}' | head -1)"
if [ -z "${mission_id}" ]; then
    echo "FAIL: could not extract mission ID from 'mission new --blank --headless'"
    exit 1
fi

# Capture the initial config_commit. mission ls -a --json (if available) is
# preferred; fall back to grep on the human output.
initial_commit="$(${agenc_test} mission ls -a 2>&1 | grep "${mission_id}" | awk '{print $NF}')"

# Modify a tracked file under the test env's ~/.claude. The test env's HOME
# is whatever the test harness configures; if AGENC_TEST_HOME or similar is
# in scope, use that. Otherwise, write into ~/.claude/skills/agenc-e2e-test/
# (a cleanup trap should remove it at end-of-test).
test_skill_dir="${HOME}/.claude/skills/agenc-e2e-autoregen-test"
mkdir -p "${test_skill_dir}"
trap 'rm -rf "${test_skill_dir}"' EXIT
cat > "${test_skill_dir}/SKILL.md" <<EOF
---
name: agenc-e2e-autoregen-test
description: marker file written by E2E auto-regen test
---
sentinel: $$
EOF

# Wait briefly for the server's fsnotify debounce to flush ingestion into
# the shadow repo. The watcher's debounce is on the order of a few hundred
# ms; sleep 2s for safety.
sleep 2

# Reload the mission async with a no-op prompt.
run_test "auto-regen reload returns success" \
    0 \
    "${agenc_test}" mission reload --async --prompt "ack" "${mission_id}"

# Wait for the reload to fire and the wrapper to re-spawn. Poll mission ls
# until the status returns to running (or a max timeout).
waited=0
while [ "${waited}" -lt 15 ]; do
    if ${agenc_test} mission ls -a 2>&1 | grep "${mission_id}" | grep -q running; then
        break
    fi
    sleep 1
    waited=$((waited+1))
done

# Verify config_commit advanced (or at minimum is non-empty and the new
# skill file appears in the mission's claude-config).
new_commit="$(${agenc_test} mission ls -a 2>&1 | grep "${mission_id}" | awk '{print $NF}')"
if [ "${new_commit}" = "${initial_commit}" ]; then
    echo "FAIL: config_commit did not advance after ~/.claude edit + reload"
    echo "  initial: ${initial_commit}"
    echo "  current: ${new_commit}"
    exit 1
fi

mission_skill_path="$(./_build/agenc-test config get agencDirpath 2>/dev/null)/missions/${mission_id}/claude-config/skills/agenc-e2e-autoregen-test/SKILL.md"
run_test "new skill file appears in mission claude-config after reload" \
    0 \
    test -f "${mission_skill_path}"
```

> The exact column position of `config_commit` in `mission ls -a` output may differ; if `awk '{print $NF}'` doesn't extract it cleanly, switch to `mission ls -a --json` (if supported) or a more targeted `grep -oE '[0-9a-f]{40}'`. Verify by running the command manually before committing.

**Step 4: Run `make e2e`**

Run: `make e2e` (with `dangerouslyDisableSandbox: true`).

Expected: PASS — both removals and the new regression case succeed.

**Step 5: Commit**

```bash
git add scripts/e2e-test.sh
git commit -m "E2E: drop reconfig tests, add auto-regen regression"
git pull --rebase && git push
```

---

## Task 9: Final verification

**Step 1: Full build + check**

Run: `make build` (with `dangerouslyDisableSandbox: true`).

Expected: PASS — clean build, all unit tests pass, no new deadcode entries beyond the pre-existing list.

**Step 2: Full E2E**

Run: `make e2e` (with `dangerouslyDisableSandbox: true`).

Expected: PASS — every test in `scripts/e2e-test.sh` succeeds.

**Step 3: Search for any remaining stale references**

Run:

```bash
grep -rn "mission reconfig\|missionUpdateConfigCmd\|reconfigCmdStr\|updateConfigCmdStr\|Reconfig & Reload" \
    --include="*.go" --include="*.md" --include="*.sh" \
    --exclude-dir=docs/plans \
    --exclude-dir=.git
```

Expected: zero hits (the exclude on `docs/plans` is intentional — the design and plan docs themselves describe the removed command in their narrative).

If any hit appears, address it (delete or update) and commit as a follow-up.

**Step 4: Manual smoke test (cannot be automated)**

Per `CLAUDE.md`'s tmux-integration rule: changes that touch `internal/wrapper/wrapper.go`'s spawn path or anything that reads `$TMUX_PANE` need manual verification.

Tell the user to:
1. Open the command palette and create a fresh mission via "Quick Claude" (real environment, not test env).
2. From a separate shell, edit `~/.claude/CLAUDE.md` to add a known marker (e.g., a fake instruction "Mention the word 'banana' in your next response").
3. From inside the mission, run: `agenc mission reload --async --prompt "what's in your CLAUDE.md?" $AGENC_MISSION_UUID`.
4. Wait for the reload to complete; the post-reload Claude should mention "banana" (proving the new content propagated).
5. Tail the wrapper log file (location: `$AGENC_DIRPATH/missions/<id>/wrapper.log` or wherever wrapper logs go — find via `grep -rn "wrapper.log\|WrapperLogFilepath" internal/`) and confirm a `claude-config rebuilt shadow_commit=<hash>` entry on each spawn.

This manual check is the gate for considering the change complete.

---

## Done

The work is complete when:
- All 9 tasks above are committed and pushed.
- `make build` and `make e2e` both pass.
- No grep hits for the removed command surface remain in code/scripts/docs (outside `docs/plans/`).
- Manual smoke test in step 9.4 succeeds.

## Execution Handoff

Use `superpowers:subagent-driven-development` to execute this plan task-by-task. Stay in this session. Spawn a fresh subagent for each Task; run code review after each commit. The tasks are sequenced so each one passes `make check` before the next begins — do not batch.
