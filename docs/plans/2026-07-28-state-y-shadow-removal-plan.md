# State Y — Shadow-Config Removal: Implementation Plan

**Status:** planning only — no code changed by the investigation that produced this doc.
**Bead:** agenc-tcoh (decision), agenc-lm3d (auth half), agenc-pz0v (plugin propagation, fixed by this), agenc-c4ko (cleanup loop → becomes trust-entry pruning), agenc-ok7h (devcontainer teardown — separate, but coupled; see §7).
**Origin:** investigation mission 98e5b6ba-59d5-4857-9555-3e0ce89dd060; this doc written in mission cc8e4c39-e2ca-45d5-8818-907de7cfbece. GitHub issue #13.

> **The decision is locked and is NOT the executor's to relitigate.** Move AgenC to a native passthrough ("State Y"): a mission runs plain `claude` against the user's real `~/.claude` with `CLAUDE_CONFIG_DIR` **unset** and no `CLAUDE_CODE_OAUTH_TOKEN` injection. Read `bd show agenc-tcoh` (NOTES in full) and `bd show agenc-lm3d` before executing. This doc is the *how*, grounded in the current tree.

> **Before writing any code, the executor MUST run `/brainstorm`** (repo rule: any non-trivial change) and re-read the source-of-truth beads. This plan is intent + sequencing, not a line-by-line script — the Parable-of-the-Orange applies: a smarter executor will improve on the *how*.

---

## 1. What "State Y" is, in two knobs

Both knobs live in one function, `mission.BuildClaudeCmd` (`internal/mission/mission.go`):

| Knob | Today (State X) | State Y |
|------|-----------------|---------|
| `CLAUDE_CONFIG_DIR` | set to the per-mission snapshot dir (`mission.go:96`) | **unset** — Claude reads real `~/.claude` |
| `CLAUDE_CODE_OAUTH_TOKEN` | injected from the token file (`mission.go:115`) | **not injected** — native keychain login |

Everything else — the shadow repo, the per-spawn snapshot rebuild, per-mission keychain cloning, credential sync, path rewriting, the CLAUDE.md/settings merge — exists *only to service those two knobs*. Flip the knobs and all of it becomes dead code, **except** three things that must be re-homed, not deleted:

1. **The operational layer** (AgenC's state-tracking hooks, the `agenc prime` SessionStart hook, the repo-library guard + deny, agent-dir allow, server-socket `allowUnixSockets`) → delivered per-invocation via `claude --settings <file>`.
2. **The `.claude.json` trust entry** → written by the server into the *real* `~/.claude.json` at mission-create (the one unavoidable piece of new machinery).
3. **Session-transcript resolution** → repointed to `~/.claude/projects/<encoded-agent-dir>` via the existing `claudeconfig.ComputeProjectDirpath` (`build.go:699`).

---

## 2. Current-state surface map (file:line)

**Spawn / env (the flip point):**
- `internal/mission/mission.go:54-118` — `BuildClaudeCmd`: sets `CLAUDE_CONFIG_DIR` (:96), reads token + injects `CLAUDE_CODE_OAUTH_TOKEN` (:102-115). Resolves config dir via `GetMissionClaudeConfigDirpath` (:68).
- `internal/wrapper/wrapper.go:327-373` — `spawnClaude`→`rebuildClaudeConfig`→`BuildMissionConfigDir` runs on **every** spawn. `setupRun` (:177-316) and `RunHeadless` (:778-900) also call `config.SetupOAuthToken`, `cloneCredentials`, `writeBackCredentials`, and start the two credential-sync goroutines (:234-238, :835-840).

**The snapshot builder (to delete):**
- `internal/claudeconfig/build.go` — `BuildMissionConfigDir` (:41-144), `copyDirWithRewriting` (:315), `copyAndPatchClaudeJSON` (:361-443, writes trust into the *snapshot*), `symlinkToGlobalClaudeDir` (:275) + the 13-entry `symlinkDirNames` list (:106-121), keychain fns `CloneKeychainCredentials`/`WriteBackKeychainCredentials`/`DeleteKeychainCredentials`/`ComputeCredentialServiceName` (:445-577). `GetMissionClaudeConfigDirpath` (:186) with its fallback to `GetGlobalClaudeDirpath` = `$AGENC_DIRPATH/.claude` (**not** `~/.claude` — see §8).
- `internal/claudeconfig/shadow.go` — `EnsureShadowRepo`/`InitShadowRepo`/`IngestFromClaudeDir`/`RewriteClaudePaths` + whole ingest engine.
- `internal/claudeconfig/merge.go` — `RewriteSettingsPaths` (:469), `MergeSettings`/`MergeSettingsWithAgencOverrides` (:227, :427), plus `mergeAgencHooks`/`mergeAgencPermissions`/`mergeAgencSandbox` (the operational half to **keep + extract**). `MergeCredentialJSON` (:25) → dead with credential sync.
- `internal/claudeconfig/overrides.go` — hook + permission + deny builders. **Keep** `BuildAgencHookEntries`, `BuildAgentDirAllowEntries`, `BuildRepoLibraryDenyEntries`, `mergeAgencSandbox`, `WriteAgencHookScripts`, `repo_library_guard.sh`, the `agenc prime` SessionStart hook (:70-74). **Drop** `BuildClaudeConfigDenyEntries` (:237 — no snapshot to protect) and the container variants (`staticContainerHookEntries`, `BuildContainerHookEntries` — devcontainer, §7).

**The shadow watcher (to SPLIT, not delete):**
- `internal/server/config_watcher.go` — `runConfigWatcherLoop` does **two unrelated jobs**: (a) shadow ingest from `~/.claude` (`EnsureShadowRepo`, `ingestClaudeConfig`, `watchTrackedDirs`, `isTrackedPath`) — delete; (b) watch `config.yml` → `reloadConfig` (cron sync + `reconcileWriteableCopiesFromConfig`) — **must survive**. This file is surgery, not a delete.

**Credential sync (to delete):**
- `internal/wrapper/credential_sync.go` (whole file), `config.GetGlobalCredentialsExpiryFilepath`, the `rebuilding`/`credentialHashMu`/`perMissionCredentialHash`/`lastDownwardSyncTimestamp` wrapper fields.

**Auth / onboarding (to flip):**
- `internal/config/config.go:438-543` — `SetupOAuthToken` (`claude setup-token` → store machine token). Called from `cmd/config_init.go:99` and `wrapper.go:180,780`.
- `internal/server/session_summarizer.go:71-78` — **second token consumer** the bead's delete-list omits (host-side `claude -p` for auto-summary; runs from `os.TempDir()`, no `CLAUDE_CONFIG_DIR`). Drop the token injection; it uses native login.
- `internal/devcontainer/overlay.go:98` — third token consumer (devcontainer, §7).
- `cmd/config_get.go` / `cmd/config_set.go` / `cmd/login.go` / `cmd/doctor.go:99-113` — `claudeCodeOAuthToken` key + `checkOAuthTokenPermissions` doctor check.

**Trust write — current choke points (both server-side, in the create handler):**
- `internal/server/missions.go:409` (`handleCreateMission`) and `:546` (`handleCreateClonedMission`) both call `mission.CreateMissionDir`. Today the trust entry is written *later*, by the wrapper, into the *snapshot* (`copyAndPatchClaudeJSON`). State Y moves it *earlier* and into the *real* file.
- `internal/server/missions.go:719-780` (`handleDeleteMission`) — currently deletes the per-mission keychain entry (:746-747); becomes the trust-entry prune site.

**Session-transcript consumers (to repoint):**
- `cmd/mission_print.go:111`, `cmd/summary.go:197`, `cmd/mission_inspect.go:141`, `internal/server/idle_timeout.go:135`, `claudeconfig.GetLastSessionID` (`build.go:680`) — all pass `GetMissionClaudeConfigDirpath(...)` into `session.FindActiveJSONLPath` / `ListSessionIDs` / `FindSessionName`, which join `<dir>/projects` and scan for a dir containing the mission UUID (`session.go:84-98`).

---

## 3. §1 — Delete/change surface, sequenced green at every step

The State-X→Y difference is *coordinated*: spawn env + config source + trust location must flip together. So the strategy is: **land behavior-preserving prep and net-new code first, flip once, then delete dead code.** `make check` and `make e2e` stay green at every increment because new code is dead until the flip, and old code stays live until after it.

**Increment 0 — Repoint session readers to `ComputeProjectDirpath` (FIRST — see §6).**
Change `session.FindActiveJSONLPath` / `ListSessionIDs` / `FindSessionName` (and `GetLastSessionID`) to resolve the project dir from the **agent dirpath** via `ComputeProjectDirpath(agentDir)` instead of scanning `<claudeConfigDir>/projects`. Update the 5 call sites to pass the agent dir. Behavior-identical under State X (both resolve to `~/.claude/projects/<encoded>`), mandatory under State Y. E2E: `mission print` / `summary` / title-reconcile still resolve. *Green: yes — pure refactor.*

**Increment 1 — Extract the operational-settings-file generator (net-new, dead).**
New `claudeconfig` function, e.g. `BuildOperationalSettings(agencDirpath, agentDirpath) → []byte`, emitting a standalone settings.json = hooks (`BuildAgencHookEntries` incl. `agenc prime` SessionStart + repo-library guard) + `permissions.allow` (`BuildAgentDirAllowEntries`) + `permissions.deny` (`BuildRepoLibraryDenyEntries`) + `sandbox.network.allowUnixSockets` (server socket). **AgenC operational plumbing only** — no user overlay (see §5: `claude-modifications` is retired, not folded in here). **No** user-settings merge (`--settings` unions with `~/.claude/settings.json` natively), **no** path rewriting, **no** claude-config deny. Unit-test it. Not wired yet. *Green: yes — additive + tests.*

**Increment 2 — Add the server-side trust writer + prune (net-new, dead). See §4.**
`server.seedTrustEntry(agentDirpath, trustedMcpServers)` and `server.pruneTrustEntry(agentDirpath)`, both under one `claudeJSONMu sync.Mutex`, atomic temp+rename into real `~/.claude.json`, read-verify-retry. Unit-test with a temp home. Not wired yet. *Green: yes.*

**Increment 3 — THE FLIP (single behavior-changing increment).**
- `BuildClaudeCmd`: drop `CLAUDE_CONFIG_DIR` (:96) and `CLAUDE_CODE_OAUTH_TOKEN` (:102-115); append `--settings <mission-op-settings-file>`. (Prime stays a SessionStart hook inside the op-settings file — no `--append-system-prompt` needed; see §5.)
- Wrapper `spawnClaude`: replace `rebuildClaudeConfig`/`BuildMissionConfigDir` with "ensure the op-settings file exists" (write it at create, or regenerate on reload). Delete the `cloneCredentials`/`writeBackCredentials`/`initCredentialHash` calls and the two sync goroutines. Remove the shadow-repo precondition (`GetShadowRepoCommitHash` error at `wrapper.go:346`).
- Server create handlers (`missions.go:409` and `:546`): call `seedTrustEntry` right after `CreateMissionDir`, before `spawnWrapper`.
- Server delete handler (`missions.go:746-747`): replace `DeleteKeychainCredentials` with `pruneTrustEntry`.
- Auth: replace `SetupOAuthToken` with a "logged-in via `claude auth login`?" check (§6/§4); drop the `session_summarizer.go` token injection.
- **E2E (mandatory, this is the behavioral change):** a fresh mission spawns with no `CLAUDE_CONFIG_DIR`; a skill/hook added to `~/.claude` *after* create is visible without reload; the mission's agent dir is trusted (no trust dialog); a user hook AND an agenc hook both fire (the `--settings` union — see §9/R5). *Green requires these tests written.*

**Increment 4+ — Delete dead code, in dependency order, one green step each.**
`credential_sync.go` + wrapper fields + `MergeCredentialJSON` + keychain fns → `BuildMissionConfigDir` + `copyDirWithRewriting` + `copyAndPatchClaudeJSON` + `symlinkToGlobalClaudeDir` + `RewriteClaudePaths`/`RewriteSettingsPaths` + the old `MergeSettings` path + the `claude-modifications` merge layer (§5, after its content is migrated into `~/.claude`) → `shadow.go` + the shadow half of `config_watcher.go` (**preserve the config.yml/writeable-copy half**) + `EnsureShadowRepo` call in the config-watcher loop → `SetupOAuthToken` + `config.go` token-file plumbing (see §4 for the key's fate) → `GetMissionClaudeConfigDirpath` once no consumer remains. Delete tests alongside each. Update `docs/system-architecture.md` (the "Shadow repo", "Per-mission config merging", "Credentials"/"credential sync" sections, the runtime-tree `claude-config-shadow/` and per-mission `claude-config/` blocks, and the `internal/claudeconfig`/`credential_sync.go` package descriptions) in the **same commits** that remove the code (repo rule).

---

## 4. §2 — The `.claude.json` trust-write design

**Why it's unavoidable:** there is no global trust bypass in Claude Code (verified in agenc-tcoh: no settings key, env var, managed-settings option, or flag short of `--dangerously-skip-permissions`; `trusted-folders` is an open upstream FR). Trust is keyed to the git-repo root, so each mission's agent dir needs `projects["<agentDir>"].hasTrustDialogAccepted=true` in the file Claude actually reads. With `CLAUDE_CONFIG_DIR` unset, that file is **`~/.claude.json`** (home-level).

**Single choke point.** Both create paths (`handleCreateMission:409`, `handleCreateClonedMission:546`) call `mission.CreateMissionDir`. Seed the trust entry from the server immediately after that call, in both handlers, via one shared method — or, cleaner, seed it *inside* `CreateMissionDir` (it already knows the agent dir and `mission` already imports `claudeconfig`). One function, both paths, cannot be forgotten.

**Serialize.** "Single process" ≠ serialized: the server handles concurrent `POST /missions` in separate goroutines. Guard **all** reads/writes of `~/.claude.json` with one `Server.claudeJSONMu sync.Mutex` (create-seed, delete-prune, startup-reconcile). This makes AgenC's own writes race-free.

**Atomic write.** Under the mutex: read `~/.claude.json` → parse → set `projects[agentDir]` (`hasTrustDialogAccepted=true` + `enabledMcpjsonServers`/`disabledMcpjsonServers` from the repo's `trustedMcpServers`, same shape as today's `copyAndPatchClaudeJSON:405-416`) → marshal → write to a temp file **in the same directory** → `os.Rename` over `~/.claude.json` (atomic on one filesystem).

**Verify + bounded retry.** Claude holds no write lock and churns this file (agenc-tcoh: one `claude -p` wrote 500+ lines of GrowthBook cache; the real file already has 4 `.bak*` siblings). AgenC can't stop Claude's writes, only minimize the clobber window. After rename, re-read and confirm the entry is present; if a sibling mission's Claude clobbered it, retry the read-modify-rename a bounded number of times (e.g. 3, short backoff). Because we seed **pre-spawn**, this mission's own Claude isn't a racer yet — the racers are other missions' live Claudes.

**Prune dead entries (folds in agenc-c4ko).** Two layers: (1) on `mission rm`, `pruneTrustEntry` deletes `projects[agentDir]` under the mutex (replaces `DeleteKeychainCredentials` at `missions.go:746`); (2) a periodic server reconcile (or startup pass) that removes any `projects[...]` key whose path is under `$AGENC_DIRPATH/missions/` but whose mission is absent from the DB. Layer (2) is also the **existing-mission migration** seeder (§6) and the **check-loop** the "Pair Every Loop With a Check-Loop" rule requires — it converts silent trust drift into a self-healing sweep.

---

## 5. §3 — The operational layer via `--settings`, and prime delivery

**Empirically verified (agenc-tcoh, Claude Code v2.1.191):** `--settings <file>` **unions** hooks and permissions with the user's `~/.claude/settings.json` — both fire, the user's own hooks/permissions still apply, deny wins. Test that produced this: a SessionStart hook in the config-dir settings AND a different one via `--settings` both fired.

**Where the op-settings file lives.** Recommend a per-mission file written by the server at create (e.g. `missions/<uuid>/agenc-settings.json`), passed via `--settings` at every spawn, regenerated on reload. It contains only AgenC's operational overlay (Increment 1) — it references the mission's agent dir (allow), the repo library (deny), and the server socket. The repo-library guard **script** still needs to exist on disk somewhere the hook command can `bash` it; keep `WriteAgencHookScripts` writing it into the mission dir (e.g. `missions/<uuid>/agenc-hooks/`), just not into a snapshot.

**Prime (the AgenC operating context) needs no new mechanism.** It's already a SessionStart hook running `agenc prime` (`overrides.go:70-74`). Carry that same hook in the op-settings file — `--settings` unions it in. The bead's "`additionalContext` or `--append-system-prompt`" alternatives are unnecessary; the existing hook is simpler and already covers startup/resume/clear/compaction.

**The `claude-modifications` mechanism is retired, not re-homed.** Kevin confirmed (2026-07-28) its content is not AgenC-specific — *"I do use them, but it's all stuff that could go into my global Claude."* So the clean move is: **one-time migrate** `claude-modifications/CLAUDE.md` → `~/.claude/CLAUDE.md` and `claude-modifications/settings.json` → `~/.claude/settings.json` (Kevin's action or a one-time executor step), then **delete the whole `claude-modifications` merge layer** — `buildMergedClaudeMd` (`build.go:208`), the mods branches of `MergeSettings`/`buildMergedSettings`, `GetClaudeModificationsDirpath`, and the `config/claude-modifications/` directory. Under State Y, Claude reads that content natively from `~/.claude`, and the op-settings `--settings` file carries **only** AgenC operational plumbing — no user overlay. This removes an entire config layer rather than porting it (net simplification; resolves Q2).

---

## 6. §4 — The auth change + onboarding flip

**Spawn env:** remove both env lines from `BuildClaudeCmd`. Remove the `session_summarizer.go:71-78` token injection (host-side `claude -p` uses native login; it already runs from a temp dir with `--no-session-persistence`, so no `CLAUDE_CONFIG_DIR` concern).

**Onboarding flip (`claude setup-token` → `claude auth login`).** Replace `SetupOAuthToken` with a **login check**, not a token capture: is there a native `claudeAiOauth` credential in the global keychain (`ReadKeychainCredentials(GlobalCredentialServiceName)` parses to a claudeAiOauth blob) and/or does `~/.claude.json` exist? If logged out, print visually-obvious guidance: **"🚨 Run `claude auth login` first"**. Wire the check at `config_init.go:99` (interactive setup) and at spawn (`wrapper.go:180,780`, replacing `SetupOAuthToken`). Headless (no TTY) must **fail loudly** — you cannot run an interactive login mid-headless; the error tells the user to `claude auth login` before scheduling missions.

**The `claudeCodeOAuthToken` config key + the fallback.** agenc-tcoh names re-adding the machine-token injection as the **State-X fallback** (auth stable, connectors broken) if the refresh race proves intolerable at 10+ concurrent missions. Recommend **keeping the key dormant** (get/set/token-file plumbing intact) so the fallback is a one-line re-add of the `CLAUDE_CODE_OAUTH_TOKEN` env line, rather than deleting it. If instead you remove the key, the **config-key checklist** (`/config-key-checklist`) governs: update `config get`/`set`, README, and the arch doc. `cmd/doctor.go`'s `checkOAuthTokenPermissions` should become a "logged in via `claude auth login`?" check either way.

**Prerequisite (accepted cost, agenc-lm3d):** State Y inherits Claude's concurrent-refresh race — occasional `/login` at 10+ concurrent missions. The bargain is a clean passthrough; the race is Claude's to fix. Prerequisite: an active `claude auth login` in the global keychain.

---

## 7. §5 — Migration of existing missions

**The hazard.** An in-flight State-X mission has: a `claude-config/` snapshot dir, a per-mission keychain entry (`Claude Code-credentials-<hash>`), `CLAUDE_CONFIG_DIR` fixed in its already-spawned Claude's env, and a trust entry **inside its snapshot's** `.claude.json` — **not** in real `~/.claude.json`. When AgenC updates to State Y:
- **Running Claude processes are unaffected** — env is fixed at spawn; they keep using their snapshot until they respawn (reload/restart).
- **On first State-Y respawn**, the new wrapper won't build a snapshot and Claude reads real `~/.claude.json`, where the agent dir has **no** trust entry (it was created under State X) → **blocking trust dialog**.

**Recommendation: seed-on-startup, not new-missions-only.** On server boot (State-Y build), run a one-time idempotent pass over the `missions` table seeding `projects[<agentDir>].hasTrustDialogAccepted=true` into real `~/.claude.json` for every existing mission. Cheap (one pass), makes the fleet uniform, and lets existing missions respawn cleanly — avoiding the alternative's cost of keeping the entire snapshot+keychain+shadow machinery alive until the last State-X mission is deleted. This pass **is** the trust-reconcile check-loop (§4 layer 2) run at startup.

**Orphaned artifacts are inert — clean up cheaply, don't gate on it.** Old per-mission keychain entries and snapshot `claude-config/` dirs become dead; the bead already notes old keychain entries are harmless. Snapshot dirs are removed with the mission dir on `mission rm`. Optional one-time keychain cleanup can delete `Claude Code-credentials-<hash>` for known missions (hash computable from the old snapshot path). YAGNI unless it bites.

---

## 8. §6 — Recommended FIRST increment

**SHIPPED 2026-07-28 (commit e748641).** Increment 0 landed: session readers now resolve the project dir via `claudeconfig.GetMissionProjectDirpath` → `ComputeProjectDirpath`; the symlink-scan `findProjectDirpath` and 3 dead readers (`FindSessionName`, `FindCustomTitle`, `ExtractRecentUserMessages`) + helpers were removed (deadcode 29→22). Behavior-preserving (empirically confirmed: new path resolves to the same real `~/.claude/projects/<encoded>` transcripts); `make check` + `make e2e` (136/136) green. Also done first: devcontainer teardown (agenc-ok7h, closed). Next: Increments 1–2 (op-settings generator, trust-writer) are reversible dead-until-flip prep; the flip (Increment 3) gates on Kevin (write to real `~/.claude.json` + refresh-race confirmation, and an active native `claude auth login`).

**Increment 0: repoint session-transcript readers to `ComputeProjectDirpath`.**

Why this one first:
- **It's a genuine no-op today and load-bearing later.** Under State X both the snapshot's `projects/` symlink and `ComputeProjectDirpath(agentDir)` resolve to the identical physical dir `~/.claude/projects/<encoded-agent>` (the encoded path contains the mission UUID, which is why the current substring scan works). So it changes nothing now.
- **It's a correctness prerequisite, not a cleanup.** `GetMissionClaudeConfigDirpath` falls back to `GetGlobalClaudeDirpath` = **`$AGENC_DIRPATH/.claude`**, *not* `~/.claude` (`config.go:120`). After the flip there is no per-mission `claude-config/`, so the fallback would point session resolution at `$AGENC_DIRPATH/.claude/projects` — which doesn't exist — and **title/summary/search/idle-timeout would silently break.** The bead frames this as a "repoint"; it is actually load-bearing (see §9).
- **It shrinks the flip's blast radius.** The session pipeline (custom-title, auto-summary, search index, idle timeout) is the subsystem most likely to fail *silently* during the flip. Landing its repoint alone — independently E2E-verifiable now (`mission print`, `summary`, title reconcile all still resolve to the same transcripts) — means Increment 3 doesn't also have to touch session resolution.

It is the smallest independently-shippable, independently-verifiable step that de-risks the highest silent-failure surface of the whole migration.

---

## 9. Open questions, risks, and where the code extends the bead notes

**Where the code contradicts / extends agenc-tcoh's notes** (verify against tree — the notes were written from an earlier investigation and drifted):
- **C1 — a third token consumer the "TO DELETE" list omits:** `internal/server/session_summarizer.go:71-78` injects `CLAUDE_CODE_OAUTH_TOKEN` for the host-side auto-summary `claude -p`. Must be handled in the auth flip (drop it; native login covers it). (`devcontainer/overlay.go:98` is a fourth, in agenc-ok7h's scope.)
- **C2 — the repoint is load-bearing, not cosmetic:** `GetMissionClaudeConfigDirpath`'s fallback is `$AGENC_DIRPATH/.claude`, not `~/.claude`, so leaving consumers on it **breaks** session resolution post-flip (§8). Stronger than the note's "repoint … via existing ComputeProjectDirpath."
- **C3 — "single process → serialized" is insufficient:** concurrent `POST /missions` run in separate goroutines; an explicit `~/.claude.json` mutex is required (§4).
- **C4 — `config_watcher.go` is a SPLIT, not a delete:** it also owns the `config.yml` watch → cron sync + writeable-copy reconcile, which must survive (§2, §3-Inc4).
- **C5 — devcontainer is more coupled than "independent, either order":** the `containerized bool` threads through the exact functions the flip rewrites (`BuildMissionConfigDir`, `MergeSettings`, `overrides.go` container hook variants). Deleting `BuildMissionConfigDir` breaks `spawnClaudeInContainer`. **Recommend sequencing agenc-ok7h (devcontainer teardown) first or early**, so the flip and the op-settings generator have only the host path to reason about. (Kevin doesn't use devcontainers; agenc-ok7h wants them gone anyway.)

**Open questions:**
- **Q1 — op-settings file lifecycle:** per-mission file written at create + regenerated on reload (recommended), vs a per-invocation temp file. Reversible; recommend per-mission-at-create. *Executor's call.*
- **Q2 — `claude-modifications` fate: RESOLVED (Kevin, 2026-07-28).** Content is not AgenC-specific ("all stuff that could go into my global Claude"). Retire the mechanism: migrate content into `~/.claude`, delete the merge layer (§5). No re-home needed.
- **Q3 — devcontainer fate / sequencing (C5): RESOLVED (Kevin, 2026-07-28).** No devcontainer missions he cares about. Do agenc-ok7h first/early and remove the container spawn path outright — no need to preserve it or strand it on legacy machinery.
- **Q4 — keep vs remove `claudeCodeOAuthToken` key: RESOLVED (Kevin, 2026-07-28).** Keep the `claudeCodeOAuthToken` key + token-file plumbing **dormant** so re-injecting the machine token (fallback to State X: auth stable, connectors broken) stays a one-line change. Do not remove it.
- **Q5 — trust-write hardening depth:** retry count / backoff, plus whether the reconcile sweep runs periodically or startup-only. Recommend bounded retries + startup reconcile (doubles as migration). *Executor's call.*

**Risks:**
- **R1 — `~/.claude.json` concurrent-write corruption** (accepted, agenc-tcoh). Mitigate: serialize AgenC writes + atomic rename + verify-retry + startup reconcile. Residual (Claude's own unguarded writes) is unavoidable and accepted.
- **R2 — refresh race at 10+ concurrent missions** (accepted, agenc-lm3d). Fallback: re-add machine token → State X (Q4 keeps this one-line).
- **R3 — logged-out user** → missions can't spawn. Mitigate: clear detection + guidance at config-init AND spawn; headless fails loudly (§6).
- **R4 — devcontainer coupling** (C5/Q3).
- **R5 — the whole operational layer relies on `--settings` unioning hooks**, an empirically-verified but version-dependent behavior. **This is the paired check-loop the repo's "Pair Every Loop" rule demands:** add an E2E that asserts a user-defined hook AND an agenc hook both fire under `--settings`, so a future Claude-Code change to merge semantics surfaces as a red test, not a silent operational-layer failure.

---

## 10. Provenance

- Decision + mechanics: `bd show agenc-tcoh` (2026-07-26 DECISION note). Auth half: `bd show agenc-lm3d`. Plugin propagation fixed: agenc-pz0v. Cleanup→prune: agenc-c4ko. Devcontainer: agenc-ok7h. GitHub issue #13.
- Historical design docs for the machinery being removed (read for context): `specs/claude-config-shadow-repo.md`, `specs/per-mission-claude-config.md`, `specs/credential-sync-loop.md`, `docs/authentication.md`, `docs/plans/2026-05-06-wrapper-auto-regen-claude-config-plan.md`, `docs/plans/2026-02-19-mcp-credential-sync.md`, `specs/ARCHIVE/feature-token-file-auth.md`, `specs/ARCHIVE/feature-credential-propagation.md`.
- Investigation mission: 98e5b6ba-… (replayable via `agenc session print`). This plan: mission cc8e4c39-e2ca-45d5-8818-907de7cfbece.
- **Repo rules the executor must honor:** `/brainstorm` before implementation; thin-CLI/thick-server (the trust write belongs on the server); build via Makefile (`make build`/`make check`/`make e2e`, sandbox-disabled for build-cache access); mandatory E2E for behavioral changes; `/config-key-checklist` for any config-key change; keep `docs/system-architecture.md` current in the same commit.
