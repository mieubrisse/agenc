Wrapper Auto-Regen of Per-Mission Claude Config
================================================

Context
-------

> Designed in AgenC mission `01911da4-de7e-4c68-b2a1-328c7aaf3a51`. Run
> `agenc mission print 01911da4-de7e-4c68-b2a1-328c7aaf3a51` for the full
> brainstorm.

Problem
-------

Today, picking up a `~/.claude` change inside a running mission requires the
user (or agent) to run `agenc mission reconfig` *and then* restart/reload the
mission. The reconfig step is easy to forget and adds mental load. Worse, the
guidance currently injected into every mission's CLAUDE.md only mentions the
"Reconfig & Reload" tmux palette command — there is no documented way for an
agent to update its own running config without user involvement.

We also have unnecessary architectural debt:

- The CLI calls `claudeconfig.EnsureShadowRepo` in two places
  (`cmd/mission_new.go:80`, `cmd/mission_update_config.go:58`).
- `internal/mission/mission.go:54` calls `claudeconfig.BuildMissionConfigDir`
  during mission creation, even though the wrapper rebuilds the same dir
  before every Claude spawn (for containerized missions).
- The server already runs an fsnotify watcher (`internal/server/config_watcher.go`)
  that ingests `~/.claude` → shadow repo on startup and on every change. The
  CLI-side calls are leftovers from before that watcher existed.

The result is three independent "make the shadow current" callers, none of
which are wired into the natural reload path for non-containerized missions.

Goals
-----

1. Eliminate the manual `mission reconfig` step. Agents and users should never
   need to think about it.
2. Make `~/.claude` the single source of truth for global Claude config; the
   per-mission `claude-config/` directory becomes a read-only snapshot that is
   transparently regenerated on every reload.
3. Teach every agent (not just the Adjutant) how to self-reload after a config
   change, using `agenc mission reload --async --prompt "..."`.
4. Tidy the architecture so the shadow repo has exactly one writer (the
   server's watcher) and one read-only consumer (the wrapper).

Non-goals
---------

- **Historical reproducibility per mission.** With auto-regen on every reload,
  a long-lived mission's on-disk config drifts continuously toward `~/.claude`.
  We accept this trade — the simpler mental model is worth more than rewinding
  a mission to its original config snapshot.
- **Background config propagation to running missions.** Nothing reads the
  per-mission `claude-config/` between spawns, so there is no behavioral value
  in proactively rebuilding it. The wrapper rebuilds it on the only path that
  matters: the next spawn.

Architecture
------------

### Today

```
~/.claude
   │
   ├─[fsnotify+debounce]──► server.config_watcher
   │                              │
   │                              └──► IngestFromClaudeDir ──► shadow repo
   │                                                                │
   └─[direct call]──► CLI mission_new                                │
   └─[direct call]──► CLI mission_reconfig ────────────────► EnsureShadowRepo
                              │                                      │
                              ▼                                      ▼
                         BuildMissionConfigDir ◄──────────── shadow repo
                              │
                              ▼
                         per-mission claude-config/
                              ▲
                              │
   wrapper.spawnClaude ───────┘  (only when isContainerized)
```

### After

```
~/.claude
   │
   └─[fsnotify+debounce]──► server.config_watcher
                                  │
                                  └──► IngestFromClaudeDir ──► shadow repo
                                                                    │
                                                                    ▼
                              wrapper.spawnClaude (every spawn)
                                  │
                                  ├─ GetShadowRepoCommitHash
                                  ├─ BuildMissionConfigDir  ──────► per-mission
                                  ├─ UpdateMission(config_commit)   claude-config/
                                  ├─ logger.Info(shadow_commit)
                                  └─ spawn Claude
```

The shadow repo gains a single writer (the server's watcher) and a single
read-only consumer (the wrapper). The CLI no longer touches the shadow.

Components
----------

### Wrapper (`internal/wrapper/wrapper.go`)

`spawnClaude` is rewritten to perform the rebuild for **every** spawn — not
just containerized ones. The current `if isContainerized { BuildMissionConfigDir... }`
block is replaced with an unconditional preamble:

```go
commitHash := claudeconfig.GetShadowRepoCommitHash(w.agencDirpath)
if commitHash == "" {
    return stacktrace.NewError("shadow repo missing or empty — restart the agenc server")
}

trustedMcpServers := w.loadTrustedMcpServers()
if err := claudeconfig.BuildMissionConfigDir(
    w.agencDirpath, w.missionID, trustedMcpServers, isContainerized,
); err != nil {
    return stacktrace.Propagate(err, "failed to rebuild claude-config")
}

if err := w.client.UpdateMission(w.missionID, server.UpdateMissionRequest{
    ConfigCommit: &commitHash,
}); err != nil {
    w.logger.Warn("failed to update config_commit in DB; on-disk config is correct",
        "error", err)
}

w.logger.Info("claude-config rebuilt", "shadow_commit", commitHash[:12])
```

The wrapper does not write to the shadow. It does not RPC the server for an
"ensure shadow" operation. It trusts the server has kept the shadow current
via fsnotify and rebuilds the per-mission dir from whatever it reads.

### Server (`internal/server/`)

No changes. The existing `config_watcher.go` already covers the
`~/.claude` → shadow path: startup ingest plus debounced fsnotify-driven
ingest on subsequent edits.

### CLI cleanup

- `cmd/mission_new.go:80` — delete the `claudeconfig.EnsureShadowRepo` call.
  The server's startup ingest plus watcher already keeps the shadow current.
- `internal/mission/mission.go:54` — delete the `claudeconfig.BuildMissionConfigDir`
  call. The wrapper rebuilds on first spawn, so pre-building at create-time is
  redundant.
- `cmd/mission_update_config.go` — delete entirely. Remove its registration
  in `cmd/mission.go` (the `AddCommand` call and the `update` /
  `update-config` aliases). Delete the corresponding test file if any.

### Agent-facing docs

#### `internal/claudeconfig/agent_instructions.md`

Rewrite the **Configuration Boundaries** section. The replacement explains
two things every agent in every mission needs to know:

1. **`~/.claude` is canonical.** The `CLAUDE_CONFIG_DIR` env var inside a
   mission points at `$AGENC_DIRPATH/missions/$MISSION_UUID/claude-config/`,
   but that directory is a read-only snapshot rebuilt from `~/.claude` on
   every reload. Edit `~/.claude/` to change global Claude config; never
   edit the per-mission snapshot.

2. **To pick up changes, reload yourself.** Show the canonical pattern:

   ```bash
   agenc mission reload --async --prompt "<what to do after reload>" $AGENC_MISSION_UUID
   ```

   Document the `--async` rationale (synchronous self-reload kills Claude
   mid-tool-call and discards the calling tool result; async preserves it
   and the prompt arrives cleanly on the next turn). Mention that
   `mission reconfig` no longer exists — reload is the only step.

#### `internal/claudeconfig/adjutant_claude.md`

Two paragraphs need rewriting:

- The **trustedMcpServers** section currently says "existing missions are not
  retroactively updated unless their config is rebuilt with `agenc mission
  reconfig`." Replace with: "existing missions pick up the new trust setting
  on next reload."
- The **AgenC-Specific Claude Instructions and Settings** section currently
  says "to propagate changes to existing missions, run `agenc mission reconfig`.
  Running missions must be restarted after reconfig." Replace with:
  "running missions pick up changes on the next reload — use
  `agenc mission reload --async` to apply immediately."

#### `docs/system-architecture.md`

Update the Claude config section to describe the new flow: fsnotify-driven
shadow ingestion in the server, wrapper rebuilds per-mission on every spawn,
two-step pipeline `~/.claude` → shadow → per-mission. Note that `mission
reconfig` was removed and the `config_commit` DB column is now updated by the
wrapper on each spawn.

Data flow
---------

Every wrapper spawn (initial start, reload-via-tmux-respawn-pane, devcontainer
rebuild) executes:

1. `claudeconfig.GetShadowRepoCommitHash(agencDirpath)` — local file read,
   no RPC.
2. Fail-fast if hash is empty (server bug — surface clearly so the user can
   restart the server).
3. `claudeconfig.BuildMissionConfigDir(...)` — copies tracked files from
   shadow repo into per-mission `claude-config/`, applies AgenC modifications,
   path rewriting, and credential dump.
4. `client.UpdateMission` to write the new `config_commit` to the DB.
5. Log line at `Info` level: `claude-config rebuilt shadow_commit=<hash[:12]>`.
6. Existing `spawnClaudeDirectly` / `spawnClaudeInContainer`.

The server's `config_watcher.go` runs independently in the background:
- On startup: `EnsureShadowRepo` (creates shadow if missing, ingests once).
- On fsnotify event in `~/.claude/`: debounced ingest into the shadow repo.

Error handling
--------------

| Failure | Behavior |
|---------|----------|
| `GetShadowRepoCommitHash` returns empty / error | Wrapper aborts spawn with a clear message. User restarts the server, which re-creates the shadow on its startup ingest. |
| `BuildMissionConfigDir` fails | Wrapper aborts spawn. Mission fails to start; the user sees the stacktrace in the tmux pane. Existing self-healing: next spawn retries with a fresh `RemoveAll`. |
| `UpdateMission(config_commit)` RPC fails | Logged at `Warn` level; spawn proceeds. The on-disk config is correct; only the DB column drifts stale until the next successful spawn. |
| Server is down | RPCs fail. The wrapper already depends on the server for heartbeats and notifications; this is not a new failure mode. |
| Shadow repo `.git` corruption | Watcher logs the error; wrapper detects via empty/error commit hash and fails loudly. Recovery: stop server, `rm -rf $AGENC_DIRPATH/claude-config-shadow`, restart server (next startup recreates it). |
| User edits `~/.claude` mid-ingest (TOCTOU) | Documented limitation. Most editors write atomically via temp+rename, so the realistic window is small. Worst case: shadow gets a partial file once; next event re-ingests. |
| Reload fires within fsnotify debounce window | Wrapper reads the pre-edit shadow. The next reload picks up the new state. Documented as a minor latency, not a correctness bug. |

Testing
-------

### Unit tests

- `internal/wrapper/wrapper_integration_test.go` — extend to assert the new
  spawn preamble runs in non-containerized mode (verify
  `BuildMissionConfigDir` is called, the per-mission config dir contains
  expected files, and `UpdateMission` is invoked with the shadow's HEAD).
- Existing `mission_update_config_test.go` (if any) — delete with the
  command.
- `internal/claudeconfig/build_test.go` — already covers
  `BuildMissionConfigDir` correctness; no change needed.

### E2E tests (`scripts/e2e-test.sh`)

- **Remove** all `agenc mission reconfig` test cases.
- **Add** an auto-regen regression test:
  1. Create a mission, capture `config_commit` from `mission ls -a`.
  2. Modify a tracked file under the test env's `~/.claude` mock
     (e.g., add a stub skill file).
  3. Wait briefly for fsnotify debounce to flush ingestion.
  4. `agenc mission reload --async --prompt "ack"` against the mission.
  5. Poll `mission ls` until the reload has fired (status returns to
     `running` after the brief restart).
  6. Re-read `config_commit` — assert it advanced beyond the captured value.
  7. Assert the new skill file appears under
     `$AGENC_DIRPATH/missions/$MISSION_UUID/claude-config/skills/`.

### Manual verification

- Edit `~/.claude/CLAUDE.md`, run `agenc mission reload --async --prompt
  "what changed in your CLAUDE.md?"` against a real mission, and confirm
  Claude's response references the new content.
- Tail wrapper logs and verify the `claude-config rebuilt
  shadow_commit=...` line on each spawn.

Files touched
-------------

| File | Change |
|------|--------|
| `internal/wrapper/wrapper.go` | Rewrite top of `spawnClaude` per data flow above |
| `cmd/mission_new.go` | Remove `claudeconfig.EnsureShadowRepo` call |
| `internal/mission/mission.go` | Remove `claudeconfig.BuildMissionConfigDir` call |
| `cmd/mission_update_config.go` | Delete |
| `cmd/mission_update_config_test.go` (if exists) | Delete |
| `cmd/mission.go` | Remove `missionUpdateConfigCmd` registration + aliases |
| `internal/claudeconfig/agent_instructions.md` | Rewrite Configuration Boundaries section |
| `internal/claudeconfig/adjutant_claude.md` | Strip `mission reconfig` references; describe auto-regen |
| `docs/system-architecture.md` | Update Claude config section to reflect new flow |
| `scripts/e2e-test.sh` | Remove reconfig tests; add auto-regen regression test |
| `cmd/command_str_consts.go` (if `reconfigCmdStr` lives here) | Remove unused command-string constants |
