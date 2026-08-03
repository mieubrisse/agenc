# State Y — Flip + Deletions + Migration: Task-by-Task Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the State Y native passthrough — wire the already-landed inert machinery (op-settings generator, trust writer), flip the two spawn env knobs, delete the shadow/snapshot/credential-sync machinery, and migrate existing missions — so AgenC missions run plain `claude` against the user's real `~/.claude` with `CLAUDE_CONFIG_DIR` unset and trust seeded into the real `~/.claude.json`.

**Epic:** `docs/plans/2026-07-28-state-y-shadow-removal-plan.md` (bead agenc-lh8p). Read it first — this doc is the task-level execution of its §3 sequencing, §4 trust design, §5 op-settings, §6 auth, §7 migration. The decision (agenc-tcoh) is LOCKED; do not relitigate.

**Format exemplar:** `docs/plans/2026-07-28-devcontainer-removal-plan.md` (same per-task Files/Steps/Verification/Commit rigor).

**What is already DONE (do not re-plan — these are what the flip wires/uses):**
- Devcontainer teardown (agenc-ok7h, closed): `mission.go` is already container-free; `BuildClaudeCmd` has only the host path.
- Increment 0 (commit e748641): session readers repointed to `claudeconfig.GetMissionProjectDirpath`/`ComputeProjectDirpath`.
- Increment 1 (commit 1f9050c): `claudeconfig.BuildOperationalSettings(agencDirpath, agentDirpath, hookScriptsBaseDirpath) ([]byte, error)` exists in `internal/claudeconfig/operational_settings.go`, unit-tested, UNWIRED, carries no nolint (all symbols it calls are already used elsewhere).
- Increment 2 (commit b181b26): `internal/server/trust.go` has `writeTrustEntry`/`pruneTrustEntry` (pure, atomic temp+rename + verify-retry) and `Server.seedMissionTrust`/`pruneMissionTrust` wrappers + `homeClaudeJSONFilepath`, unit-tested, UNWIRED, carrying `//nolint:unused`. `Server.claudeJSONMu sync.Mutex` exists in `server.go:65` also carrying `//nolint:unused`.

---

## Global Constraints

- **Build only via the Makefile.** Run `make check` and `make e2e` with the Bash tool's `dangerouslyDisableSandbox: true` (they need the Go build cache at `~/.cache/go-build`, outside the sandbox). Never run `go build`/`go test` directly. Never pass `--no-verify`.
- **Update `docs/system-architecture.md` in the same commit** as the code that invalidates each section (repo rule). Doc surface: shadow-repo sections (`:507-519`), per-mission config merging (`:482-505`), credential/keychain narrative (`:505`, `:229-230`, `:465`), the runtime-tree blocks (`:289`, `:312-344`), the spawn sequence (`:219-223`, `:705-706`), token passthrough (`:260`), and package descriptions (`:398-427`, `:464-465`).
- **`/brainstorm` before implementation** (repo rule: any non-trivial change). The executor runs it once at the start; it is not repeated per task.
- **`/config-key-checklist`** governs the `agenc token` CLI + `claudeCodeOAuthToken` key change in Task 5 (CLI + README + arch-doc coverage).
- **Solo repo → work on `main`.** `git shortlog -sn --all | wc -l` to confirm; commit directly to the default branch, no feature branch unless it reports 2+ contributors.
- **Commit sequence every task:** `git add -A` → `git commit` → `git pull --rebase` → `git push` (multiple agents commit concurrently). Commit message: concise summary line + a single `AgenC mission: <uuid>` trailer. No other trailers.
- **`bd` note:** call `bd` only as a direct top-level command, never inside `bash some_script.sh`.
- **The tree compiles + `make check` passes at every task boundary.** New wiring lands as behavior-preserving where possible; the one behavioral go-live is Task 4 (the flip). Deletions (Tasks 6–10) come after the flip is green. `golangci-lint`'s `unused` and `deadcode` linters (both part of `make check`) are the nets for orphaned symbols — every `//nolint:unused` removed in Task 4 must correspond to a now-live caller, or `make check` fails.

### Go-live model (state this in the plan, act on it)

**Landing these tasks on `main` does NOT change the developer's live behavior.** The developer's running missions keep using whatever binary is currently installed. State Y activates only when the developer **builds and installs the new binary and spawns a mission** — at which point they self-test connectors and watch `~/.claude.json` under real concurrency (Kevin's 2026-07-31 prerequisite: he is OK with the atomic write and will empirically check the refresh race before rolling wider). So: **these tasks land + verify in CI/test terms (`make check` + `make e2e`); real-world activation is a separate human step (Kevin's), not part of this plan.** The e2e suite must therefore exercise the mission lifecycle **without touching the developer's real `~/.claude.json`** — see Task 3 (test isolation), which is a hard prerequisite for Task 4.

---

## Task ordering rationale

```
Task 1  Prep: op-settings file lifecycle helper (net-new, inert)         [green: additive]
Task 2  Prep: trust migration/reconcile startup pass (net-new, inert)    [green: additive, gated behind a flag not yet flipped]
Task 3  Test isolation: HOME override for test-env spawns (net-new)      [green: additive, no behavior change until flip]
Task 4  THE FLIP — single behavioral increment (+ mandatory E2E)         [green: requires Tasks 1–3 + new E2E]
Task 5  Auth CLI + onboarding flip (agenc token set/clear)               [green: after flip]
Task 6  Delete credential sync + keychain                                [green: dead after flip]
Task 7  Delete snapshot builder (BuildMissionConfigDir + helpers)        [green: dead after flip]
Task 8  Delete shadow repo + SPLIT config_watcher (keep config.yml half) [green: dead after flip]
Task 9  Delete claude-modifications merge layer (after content migrated) [green: dead after flip]
Task 10 Delete SetupOAuthToken forced flow + GetMissionClaudeConfigDirpath; final verify
```

Tasks 1–3 are inert additions that leave `make check`/`make e2e` green because the new code is unreferenced (or referenced only behind the not-yet-flipped switch). Task 4 flips atomically. Tasks 6–10 delete code that Task 4 made dead, in dependency order, each ending green.

---

## Review fixes (independent Fable review, 2026-07-31) — apply these; they override the task text where they conflict

The plan was independently reviewed against the tree (verdict: READY WITH FIXES; no Critical, no `~/.claude.json`-corruption path). Apply these corrections during execution:

- **BASELINE CHECK (was finding I-3, a false alarm — but do the check):** run `make check` as the very first action. `BuildOperationalSettings` is **exported**, so `golangci-lint unused` does NOT flag it (it only flags unexported unused symbols); Increment 1 landed green with no nolint, confirming the baseline. No annotation needed on it. This same exported-vs-unexported rule drives fix I-1 below.

- **FIX I-1 (Task 4 / Task 6 boundary — credential subsystem):** Delete the **credential-sync subsystem in Task 4**, not Task 6. When Task 4 removes the `go w.watchCredential*Sync(ctx)` starts + `initCredentialHash()`, the methods in `credential_sync.go` and the wrapper struct fields (`perMissionCredentialHash`, `credentialHashMu`, `lastDownwardSyncTimestamp`) are **unexported** → `golangci-lint unused` (HARD gate) fails `make check` unless they're deleted in the same task. So Task 4 deletes: `credential_sync.go` (+ its test), the `cloneCredentials`/`writeBackCredentials`/`initCredentialHash` methods, and those three struct fields. **Task 6 is rescoped** to only the EXPORTED, `unused`-immune credential functions in `build.go`/`merge.go`/`config.go` (`CloneKeychainCredentials`, `WriteBackKeychainCredentials`, `DeleteKeychainCredentials`, `ReadKeychainCredentials`, `WriteKeychainCredentials`, `ComputeCredentialServiceName`, `GlobalCredentialServiceName`, `MergeCredentialJSON`, `ExtractExpiresAtFromJSON`, `GetCredentialExpiresAt`, `GetGlobalCredentialsExpiryFilepath`, and the unexported `mergeMcpOAuth`/`extractExpiresAt`/`ComputeCredentialHash` helpers they keep referenced) — those stay green after Task 4 (exported-dead is not `unused`-flagged) and get deleted in Task 6. Keep `GetCredentialExpiresAt`/`ExtractExpiresAtFromJSON` if Task 5's `EnsureClaudeAuth` uses them as the native-auth probe.

- **FIX I-2 (Task 9 — claude-modifications is a whole feature, not just merge functions):** Retiring `claude-modifications` also removes its live edit surface. Task 9 must additionally delete: `internal/server/claude_modifications.go` (whole file: `handleGetClaudeMd`/`handleUpdateClaudeMd`/`handleGetSettingsJson`/`handleUpdateSettingsJson` + helpers); the four route registrations in `server.go:311-314` (`GET/PUT /config/claude-md`, `GET/PUT /config/settings-json`); the claude-modifications client methods in `internal/server/client.go` (~:444); `EnsureClaudeModificationsFiles` (`config.go:332`) and its call in `EnsureDirStructure` (`config.go:97`). **Grep `ClaudeModificationsDirname` / `claude-md` / `settings-json` across `cmd/` for any CLI command** that calls those client methods and delete/repoint it too. (This is consistent with Kevin's decision to retire the layer — the edit feature retires with it. If any of these are shared with the writeable-copy/config-sync path, verify before deleting.)

- **FIX M-2 (Task 5) — SUPERSEDED 2026-08-03 (mission 363f43aa), do NOT apply.** M-2 originally said to drop `session_summarizer.go`'s `CLAUDE_CODE_OAUTH_TOKEN` injection (`:71-78`). That was written under the original "rip the token out entirely" model. Kevin's 2026-07-31 refinement made token injection a CONDITIONAL opt-in fallback (present → use token, absent → native). The summarizer's injection is ALREADY conditional (`if oauthToken != ""`) and is therefore CORRECT under the refined model: if a user sets a token (native-auth refresh race intolerable at high concurrency), the host-side auto-summary `claude -p` must ALSO use it, or it authenticates differently from every spawn (and hits the same native-auth unreliability the token exists to avoid). The summarizer needs no connectors, so the token's connector-breakage is irrelevant to it. **KEEP the summarizer's conditional injection unchanged.**

- **FIX M-1 (Task 3):** when Task 3 converts `homeClaudeJSONFilepath` → `Server.claudeJSONFilepath()`, remember `reconcileMissionTrust` (added in Task 2) is a caller too, not just `seedMissionTrust`/`pruneMissionTrust`.

- **FIX M-3 (Task 10):** before deleting `GetGlobalClaudeDirpath`, the grep must cover its intermediate caller `GetHistoryFilepath` (`config.go:241`) — delete both together only if `GetHistoryFilepath` is itself dead.

---

### Task 1: Add the per-mission op-settings file lifecycle helper (net-new, inert)

The flip passes `claude --settings <file>`; something must write `BuildOperationalSettings(...)` output to a per-mission file and write the repo-library-guard script somewhere the hook's absolute path resolves. This task adds those writers as an unreferenced function so the flip (Task 4) just calls it. Under State Y the natural home is the mission directory (not a snapshot): `missions/<uuid>/agenc-settings.json` for the settings file and `missions/<uuid>/agenc-hooks/repo-library-guard.sh` for the guard script.

**Files:**
- Modify: `internal/config/config.go` — add two path helpers near `GetOAuthTokenFilepath` (`:361`): `GetMissionOpSettingsFilepath(agencDirpath, missionID) → missions/<uuid>/agenc-settings.json` and `GetMissionAgencHooksDirpath(agencDirpath, missionID) → missions/<uuid>/agenc-hooks`. Derive both from `GetMissionDirpath`.
- Modify: `internal/claudeconfig/build.go` — `WriteAgencHookScripts` (`:283`) currently writes into `<claudeConfigDirpath>/agenc-hooks/`. It already takes the base dir as its argument and joins `AgencHooksDirname` itself, so it works unchanged when handed the mission-dir base. Leave the function; the new caller passes the mission-dir path.
- Create: `internal/claudeconfig/op_settings_file.go` (or add to `operational_settings.go`) — `WriteMissionOpSettings(agencDirpath, missionID) error` that: (1) resolves `agentDirpath` via `config.GetMissionAgentDirpath`, (2) resolves the hook-scripts base via the new `GetMissionAgencHooksDirpath`'s parent (the mission dir), (3) calls `WriteAgencHookScripts(<missionDir>)` to lay down the guard script, (4) calls `BuildOperationalSettings(agencDirpath, agentDirpath, <missionDir>)`, (5) writes the bytes to `GetMissionOpSettingsFilepath` via a plain `os.WriteFile` (0644). NOTE the `hookScriptsBaseDirpath` arg to `BuildOperationalSettings` must equal the dir whose `agenc-hooks/` subdir `WriteAgencHookScripts` populated — pass the mission dir consistently to both.
- Test: `internal/claudeconfig/op_settings_file_test.go` — write into a temp `agencDirpath`, assert the settings file exists + parses as JSON with `hooks`/`permissions` keys, and the guard script exists at `<missionDir>/agenc-hooks/repo-library-guard.sh` and is executable.

**Interfaces:**
- Consumes: `BuildOperationalSettings` (Increment 1), `WriteAgencHookScripts` (existing).
- Produces: `WriteMissionOpSettings` — called by Task 4 at mission-create and on reload. Inert until then (deadcode/unused will flag it → add `//nolint:unused` on `WriteMissionOpSettings` and the two new config helpers, to be removed in Task 4 when wired).

- [ ] **Step 1: Add the two `config` path helpers.** In `internal/config/config.go`, add `GetMissionOpSettingsFilepath` and `GetMissionAgencHooksDirpath` deriving from `GetMissionDirpath(agencDirpath, missionID)`. Use a shared `agenc-settings.json` / `agenc-hooks` constant pair (define the settings filename constant alongside the other `*Filename` consts near `:57`).
- [ ] **Step 2: Add `WriteMissionOpSettings`.** Implement per the Files note. Pass the mission dir as the single `hookScriptsBaseDirpath` value to BOTH `WriteAgencHookScripts` and `BuildOperationalSettings` so the guard-hook's absolute path in the generated settings points at the script actually written.
- [ ] **Step 3: Mark inert symbols.** Add `//nolint:unused // wired at the State Y flip (Task 4)` to `WriteMissionOpSettings` and the two new config helpers (if `unused` flags them). Verify which get flagged by running `make check` and adding annotations only where required.
- [ ] **Step 4: Unit-test.** Add `op_settings_file_test.go` asserting file existence, JSON shape, and guard-script executability under a temp dir.
- [ ] **Step 5: Verify green.** `make check` (sandbox disabled). Expected PASS.
- [ ] **Step 6: Commit.** `"Add per-mission op-settings file writer for State Y (agenc-lh8p)"`.

---

### Task 2: Add the trust migration + reconcile startup pass (net-new, inert)

§7 requires a boot-time idempotent pass that seeds trust entries into the real `~/.claude.json` for all existing missions (so in-flight missions respawn without a trust dialog after the update) AND prunes entries for missions no longer in the DB (the §4-layer-2 reconcile = the "Pair Every Loop" check-loop). This task adds the pass as a method that is NOT yet called from server startup — wiring the call is part of Task 4 so it activates atomically with the flip.

**Files:**
- Modify: `internal/server/trust.go` — add `Server.reconcileMissionTrust() error`: (1) list all non-archived missions from `s.db` (reuse `ListMissions` with a param, per the repo's single-Read-function rule), (2) under `s.claudeJSONMu`, read `~/.claude.json` once, seed `projects[<agentDir>]` for every existing mission's agent dir and delete any `projects[k]` whose key is under `$AGENC_DIRPATH/missions/` but whose mission is absent from the DB, (3) atomic write once. Do the whole reconcile as a SINGLE read-modify-write under the mutex (not per-mission writeTrustEntry calls) to avoid N atomic renames and N verify-retries on boot. Reuse the `buildTrustEntry` helper and `atomicWriteFile`.
- Modify: `internal/server/trust.go` — remove the `//nolint:unused` on `homeClaudeJSONFilepath` (it becomes reachable from `reconcileMissionTrust`); keep `//nolint:unused` on `seedMissionTrust`/`pruneMissionTrust`/`claudeJSONMu` until Task 4 wires them (they are still unreferenced after this task — `reconcileMissionTrust` uses the mutex and the pure funcs directly, not the wrappers). VERIFY with `make check` which annotations are still required; the `unused` linter is authoritative.
- Test: `internal/server/trust_test.go` — add a test constructing a temp `~/.claude.json` (via a `homeDir` override or by testing the pure inner reconcile against a temp filepath — prefer factoring the file-level logic into a pure `reconcileTrustEntries(claudeJSONFilepath, existingAgentDirs, missionsPrefix) error` that the method wraps, so it is testable without touching real `$HOME`). Assert: existing-mission dirs get seeded; a stale `missions/<uuid>` key gets pruned; unrelated `projects` keys (e.g. the user's own repos outside `$AGENC_DIRPATH/missions/`) are preserved untouched.

**Interfaces:**
- Consumes: `buildTrustEntry`, `atomicWriteFile`, `homeClaudeJSONFilepath`, `s.db` mission listing.
- Produces: `reconcileMissionTrust` + the pure `reconcileTrustEntries`. Inert until Task 4 calls it from the server startup goroutine set.

- [ ] **Step 1: Factor a pure `reconcileTrustEntries(claudeJSONFilepath, existingAgentDirs []string, missionsDirPrefix string) error`.** Single read-modify-write: seed each `existingAgentDirs` entry, prune `projects` keys under `missionsDirPrefix` not in `existingAgentDirs`, leave all other keys byte-for-byte. This is the unit-testable core.
- [ ] **Step 2: Add `Server.reconcileMissionTrust()`** that resolves `homeClaudeJSONFilepath`, gathers existing non-archived mission agent dirs (`config.GetMissionAgentDirpath` per mission), computes `missionsDirPrefix = config.GetMissionsDirpath(s.agencDirpath)` (confirm the helper name; else derive), takes `s.claudeJSONMu`, and calls the pure func. Trusted-MCP-servers for seeded entries: match today's create-time behavior — look up each mission's repo `TrustedMcpServers` (as `spawnClaude`'s `loadTrustedMcpServers` does) OR seed with a bare `hasTrustDialogAccepted=true` entry (the migration only needs trust; MCP-consent is re-established on next real create). RECOMMEND bare trust entry for the migration pass to keep it cheap; document the choice inline.
- [ ] **Step 3: Adjust nolint annotations** per `make check`'s `unused` output (remove from `homeClaudeJSONFilepath`; keep on the still-unwired wrappers/mutex).
- [ ] **Step 4: Unit-test** the pure reconcile per the Files note (seed / prune / preserve).
- [ ] **Step 5: Verify green.** `make check` (sandbox disabled). Expected PASS.
- [ ] **Step 6: Commit.** `"Add inert trust reconcile+migration startup pass for State Y (agenc-lh8p)"`.

---

### Task 3: Test isolation — isolate e2e's Claude config via CLAUDE_CONFIG_DIR at the harness level (net-new, PREREQUISITE for the flip)

> **REDESIGNED 2026-07-31 (mission 363f43aa-b09e-41b0-bf41-89b1479dc26a), supersedes the original HOME-override approach.** The original Task 3 overrode `HOME` for the spawned Claude to isolate `~/.claude`/`~/.claude.json`. When exercised by `make e2e`, that broke Claude Code's macOS keychain access — the login keychain resolves via `$HOME/Library/Keychains`, so pointing `HOME` at an empty test dir made Claude unable to find any keychain, producing an unbounded stack of "a keychain cannot be found to store 'odyssey'" popups. Root cause + full trace in the mission ledger. `HOME` is a blunt lever (it also moves keychains, caches, `~/Library`). **The surgical lever is `CLAUDE_CONFIG_DIR`**, empirically proven (State X wrote trust to `<CLAUDE_CONFIG_DIR>/.claude.json` at `build.go:421` and Claude honored it): when set, Claude reads AND writes its `.claude.json` at `<CLAUDE_CONFIG_DIR>/.claude.json`, leaving `$HOME`/keychain untouched. And it's set at the **test harness** level, so production spawn/auth code carries **zero `IsTestEnv` awareness** — Kevin's second correction.

**The problem, precisely.** Post-flip (Task 4), a spawned Claude with `CLAUDE_CONFIG_DIR` UNSET reads the real `~/.claude` and reads/writes the real `~/.claude.json`; the server-side trust-writer also targets `~/.claude.json`. `make e2e` runs a real headless mission that spawns a real Claude — so once flipped, e2e would read the developer's real `~/.claude` and splatter test trust entries (plus Claude's own heavy churn) into the real `~/.claude.json`. That pollutes/risks corrupting the developer's live config on every CI run. This must be closed BEFORE Task 4.

**Resolved approach (verified against the tree): set `CLAUDE_CONFIG_DIR=<isolated test config dir>` in the `agenc-test` harness; production sets nothing.** The var flows to every process that matters, and the two AgenC consumers resolve their `.claude.json` path from it uniformly — no `IsTestEnv` in the spawn/auth path. Verified facts driving this:
- **Nothing in the repo READS `CLAUDE_CONFIG_DIR`** (grep: only SET at `mission.go:96`; sole consumer is the `claude` subprocess). So the harness setting it perturbs no existing code.
- **The auto-started server inherits the CLI's env** (`ForkServer`, `process.go`, leaves `cmd.Env` nil → inherits `os.Environ()`), so a test-env server started by an `agenc-test` CLI call has `CLAUDE_CONFIG_DIR`.
- **`BuildClaudeCmd` already does `append(os.Environ(), …)`** — it inherits `CLAUDE_CONFIG_DIR` when present and adds nothing when absent; no change needed for the inherit path.
- **The wrapper runs in a tmux pane** that does NOT auto-inherit the server's env; AgenC already bridges this with `tmuxEnvPrefix()` (`missions.go`), which `export`s `AGENC_DIRPATH`/`AGENC_TEST_ENV` into the pane. Extend it to generically forward `CLAUDE_CONFIG_DIR` **if set** (production-safe: unset ⇒ nothing forwarded).

**Why this option (pre-scored against the abandoned HOME approach and the earlier rejects):**
- **(chosen) `CLAUDE_CONFIG_DIR` set at the harness.** Pro: surgical (moves only Claude's config + `.claude.json`, never `$HOME`/keychain); zero `IsTestEnv` in production spawn/auth code; the trust-writer's env-based resolution is *production-correct* (mirrors Claude Code's own config resolution, so AgenC writes trust to the file Claude actually reads — even for a real user who runs with `CLAUDE_CONFIG_DIR` set). Con: e2e spawns Claude WITH `CLAUDE_CONFIG_DIR` set while production leaves it unset, so e2e doesn't exercise the literal no-`CLAUDE_CONFIG_DIR` env — covered by a cheap unit assertion on `BuildClaudeCmd`'s production output (Task 4).
- **(rejected) HOME override** — broke Claude's macOS keychain resolution (the 2026-07-31 popup incident); blunt; put `IsTestEnv` in production spawn code.
- **(rejected) Stub/skip the real spawn** — defeats the point of the State-Y e2e (proving a real mission spawns, is trusted with no dialog, `--settings` union fires); makes it theater.
- **(rejected) Gate the trust-write behind `!IsTestEnv()`** — leaves the corruption-prone trust write unexercised.

**How e2e still meaningfully exercises the lifecycle:** the headless mission really spawns Claude (config isolated via `CLAUDE_CONFIG_DIR`, real `$HOME`/keychain intact so no popups), the server really writes a trust entry (into `<test config dir>/.claude.json`), and the `--settings` union is asserted by dropping a probe hook into `<test config dir>/settings.json` and an agenc hook via the op-settings file, then checking both fired. The lifecycle is real; only Claude's config directory moves.

**Files:**
- Modify: `internal/server/trust.go` — replace `homeClaudeJSONFilepath() (string, error)` with a resolver that reads `CLAUDE_CONFIG_DIR`: if `os.Getenv("CLAUDE_CONFIG_DIR") != ""` → `<it>/.claude.json`; else → `<UserHomeDir>/.claude.json`. Keep the `(string, error)` return (prod branch still calls `os.UserHomeDir()`). This can stay a package-level func (no `Server`/`agencDirpath` needed — it reads the process env). Update ALL THREE callers (`seedMissionTrust`, `pruneMissionTrust`, `reconcileMissionTrust`) and the trust unit tests. Update the doc comments at `trust.go:262,295` (they currently say "~/.claude.json … when CLAUDE_CONFIG_DIR is unset" — extend to name the set case).
- Modify: `internal/server/missions.go` — `tmuxEnvPrefix()`: after the `AGENC_TEST_ENV` line, add `if cd := os.Getenv("CLAUDE_CONFIG_DIR"); cd != "" { prefix += fmt.Sprintf(" CLAUDE_CONFIG_DIR='%s'", cd) }`. Generic; no `IsTestEnv`. Update/extend the `tmuxEnvPrefix` unit test if one exists.
- Modify: `Makefile` — the `agenc-test` wrapper target (`:136` area, alongside `AGENC_DIRPATH`/`AGENC_TEST_ENV`) exports `CLAUDE_CONFIG_DIR=$(TEST_ENV_DIR)/claude-config`; the `test-env` target creates that dir (`mkdir -p $(TEST_ENV_DIR)/claude-config`). This is Claude's config home in the test-env; `.claude.json` lives at `$(TEST_ENV_DIR)/claude-config/.claude.json`, skills at `$(TEST_ENV_DIR)/claude-config/skills/`, settings at `$(TEST_ENV_DIR)/claude-config/settings.json`.
- NO change to `internal/mission/mission.go` in this task (no HOME override; the pre-flip `CLAUDE_CONFIG_DIR=<snapshot>` setting stays until Task 4 removes it — its explicit append wins over the inherited harness value for `claude`, so this task is inert for the spawned Claude pre-flip).
- NO `config.GetClaudeHomeDirpath` seam, NO `ClaudeHomeDirname` const, NO `IsTestEnv` branch in spawn code.

**Interfaces:**
- Consumes: `os.Getenv("CLAUDE_CONFIG_DIR")` (uniform env read).
- Produces: env-based `.claude.json` resolver (still `//nolint:unused` — transitively unused via the unwired trust wrappers until Task 4) + generic `CLAUDE_CONFIG_DIR` tmux propagation (live but a no-op in production/non-test) + the test-env config dir. All inert for the spawned Claude pre-flip.

- [ ] **Step 1: Trust-writer env resolver.** Replace `homeClaudeJSONFilepath` with the `CLAUDE_CONFIG_DIR`-or-`$HOME` resolver; update the 3 callers + trust unit tests + the two doc comments. Follow `make check`'s `unused` output for the nolint annotation (expect it still needs `//nolint:unused`, transitively unused pre-flip — verify empirically, do NOT blind-copy).
- [ ] **Step 2: Generic `CLAUDE_CONFIG_DIR` propagation** in `tmuxEnvPrefix`.
- [ ] **Step 3: Harness setup** — `agenc-test` wrapper exports `CLAUDE_CONFIG_DIR=$(TEST_ENV_DIR)/claude-config`; `test-env` target creates the dir.
- [ ] **Step 4: Verify green — including e2e.** `make check` AND `make e2e` (both sandbox-disabled). This task does NOT override `HOME`, so the pre-flip e2e runs Claude with real `$HOME` + the existing snapshot `CLAUDE_CONFIG_DIR` exactly as the green baseline — no keychain popups. (Grep-confirm the spawn path has no `HOME=` override before running e2e.)
- [ ] **Step 5: Commit.** `"Isolate e2e Claude config via CLAUDE_CONFIG_DIR at harness for State Y (agenc-lh8p)"`.

---

### Task 4: THE FLIP — unset CLAUDE_CONFIG_DIR, conditional token, wire op-settings + trust (single behavioral increment)

This is the one go-live task. It changes what a spawned mission actually does. It wires the inert machinery from Tasks 1–3, removes the two env knobs, and ships the mandatory E2E. After this task the shadow/snapshot/credential machinery is dead code (deleted in Tasks 6–10) but STILL PRESENT and compiling — do not delete anything here beyond what the wiring strictly requires.

**Files:**
- Modify: `internal/mission/mission.go` `BuildClaudeCmd`:
  - Drop `"CLAUDE_CONFIG_DIR="+claudeConfigDirpath` from `cmd.Env` (`:96`) and the `claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(...)` line (`:68`) that feeds only it. (`GetMissionClaudeConfigDirpath` stays defined for now — still referenced by dead code deleted in Tasks 6/10.)
  - Make the token CONDITIONAL: keep `oauthToken, err := config.ReadOAuthToken(agencDirpath)`; **remove the `if oauthToken == "" { return ...error }` block (`:106-114`)**; wrap the inject as `if oauthToken != "" { cmd.Env = append(cmd.Env, "CLAUDE_CODE_OAUTH_TOKEN="+oauthToken) }`. (Matches `session_summarizer.go:71-78`, which is already conditional — leave it.)
  - Add `--settings <op-settings file>` to the claude args. The file path is `config.GetMissionOpSettingsFilepath(agencDirpath, missionID)`; prepend `--settings <path>` into `fullArgs` (before `extraClaudeArgs`/`claudeArgs`) so it applies to every spawn shape (interactive, resume, headless — all route through `BuildClaudeCmd`).
  - Add a UNIT assertion that the production `BuildClaudeCmd` output (env WITHOUT `CLAUDE_CONFIG_DIR`) contains NO `CLAUDE_CONFIG_DIR` entry — this is the faithful test of the flip's env change (the e2e itself runs with `CLAUDE_CONFIG_DIR` set by the harness, so it cannot assert absence). NO `HOME` override anywhere (Task 3 redesign). In the test-env, isolation now works automatically: once `BuildClaudeCmd` stops appending `CLAUDE_CONFIG_DIR=<snapshot>`, the value the `agenc-test` harness set (forwarded by `tmuxEnvPrefix`) is the one the spawned Claude inherits, pointing it at the isolated `<test config dir>` with real `$HOME`/keychain intact.
- Modify: `internal/wrapper/wrapper.go`:
  - `spawnClaude` (`:288`): replace the `rebuildClaudeConfig()` call with "ensure the op-settings file exists" — `claudeconfig.WriteMissionOpSettings(w.agencDirpath, w.missionID)` (write at every spawn so reloads regenerate it after a config change). Then `return w.spawnClaudeDirectly(isResume)`.
  - Delete `rebuildClaudeConfig` (`:300-328`) — its only job was the snapshot rebuild + shadow-commit DB write, both gone. (The `config_commit` DB column becomes vestigial; leave the column + `UpdateMission ConfigCommit` plumbing for a later cleanup — out of scope here to keep the flip tight.)
  - `setupRun` (`:224-228`): delete `w.cloneCredentials()`, `w.initCredentialHash()`, and the two `go w.watchCredential*Sync(ctx)` lines. In `cleanup` (`:269`): delete `w.writeBackCredentials()`.
  - `RunHeadless` (`:712-717`): delete the same clone/hash/sync block and the `defer w.writeBackCredentials()`.
  - Delete `SetupOAuthToken` calls at `setupRun:170` and `RunHeadless:657` — replaced by the conditional model. (Task 5 replaces `SetupOAuthToken` itself with a login-presence check; here, just stop calling the forced flow. If a pre-spawn auth check is wanted now, add a minimal `config.EnsureClaudeAuth(agencDirpath)` stub in Task 5; for Task 4, removing the forced flow is sufficient because `BuildClaudeCmd` no longer errors on an absent token.)
  - Delete `cloneCredentials`/`writeBackCredentials` methods (`:134-152`) — now unreferenced.
  - Remove the wrapper struct's credential fields (`perMissionCredentialHash`, `credentialHashMu`, `lastDownwardSyncTimestamp`, `:76-87`) IF they become unused after the above (deadcode will confirm; they are also read by `credential_sync.go`, which is deleted in Task 6 — so either delete `credential_sync.go` in THIS task, or leave the fields until Task 6. RECOMMEND: leave `credential_sync.go` + the fields intact this task, and delete the whole credential subsystem atomically in Task 6, to keep the flip diff focused on the env/settings/trust wiring. The two sync goroutines simply never start once the `go w.watchCredential*` lines are removed, so they are inert.)
- Modify: `internal/server/missions.go`:
  - `handleCreateMission` (`:405-408`): after `mission.CreateMissionDir(...)` and before `s.spawnWrapper(...)` (`:411`), call `if err := s.seedMissionTrust(agentDirpath, trustedMcpServers); err != nil { ... }`. Resolve `agentDirpath` via `config.GetMissionAgentDirpath(s.agencDirpath, missionRecord.ID)` and `trustedMcpServers` via the repo's `TrustedMcpServers` (look up `s.getConfig().GetRepoConfig(gitRepoName)`, mirroring `loadTrustedMcpServers`). On seed failure: log loudly and continue (the mission is created; a missing trust entry surfaces as a one-time dialog, not data loss) — but per P-4 (fail loudly), log at error level with the mission short-ID.
  - `handleCreateClonedMission` (`:543-545`): same seed call after `CreateMissionDir` / `CopyAgentDir`, before `spawnWrapper` (`:554`). Use the cloned mission's agent dir and the source mission's repo trust config.
  - `handleDeleteMission` (`:742-746`): replace the `claudeconfig.GetMissionClaudeConfigDirpath` + `claudeconfig.DeleteKeychainCredentials` block with `s.pruneMissionTrust(agentDirpath)` (agent dir of the deleted mission), log-and-continue on error.
- Modify: `internal/server/server.go` startup (`:208-218`): add a one-shot `s.reconcileMissionTrust()` call (Task 2) — either as a direct call before/after the goroutine block in `Start`/`Run`, or as the first action inside `runConfigWatcherLoop`. RECOMMEND a direct one-shot call in the startup path (not a loop) since it is a boot migration; log-and-continue on error. This is simultaneously the existing-mission migration (§7) and the reconcile check-loop (§4 layer 2).
- Modify: `internal/server/trust.go` — remove `//nolint:unused` from `seedMissionTrust`, `pruneMissionTrust` (now wired), and from `server.go:65` `claudeJSONMu`. Remove any remaining `//nolint:unused` on Task-1/2 symbols now reachable.
- Modify: `internal/claudeconfig/operational_settings.go` + `op_settings_file.go` — remove the `//nolint:unused` on `WriteMissionOpSettings` and the two config helpers (now wired).
- Modify: `docs/system-architecture.md` — update the spawn sequence (`:219-223`, `:705-706`): no `CLAUDE_CONFIG_DIR`; conditional `CLAUDE_CODE_OAUTH_TOKEN`; `--settings <op-settings file>`; trust seeded server-side at create; op-settings written per spawn instead of snapshot rebuild. (Full shadow/credential section rewrites happen in Tasks 6–9 as those are deleted; here, update only the spawn-sequence + trust-write narrative.)
- Test (MANDATORY — this is the behavioral change): `scripts/e2e-test.sh` — add a "State Y native passthrough" section that, against the isolated test-env config dir (Task 3, `$(TEST_ENV_DIR)/claude-config`):
  1. Drops a probe SessionStart (or PreToolUse) hook into `$(TEST_ENV_DIR)/claude-config/settings.json` that writes a sentinel file.
  2. Creates a real headless mission with a prompt that triggers a tool use.
  3. Asserts: (a) the spawned claude command does NOT carry a per-mission SNAPSHOT `CLAUDE_CONFIG_DIR` (the only `CLAUDE_CONFIG_DIR` is the harness `<test config dir>`, not a `BuildMissionConfigDir` snapshot path) — paired with the UNIT assertion above that production `BuildClaudeCmd` sets no `CLAUDE_CONFIG_DIR` at all; (b) `$(TEST_ENV_DIR)/claude-config/.claude.json` contains a `projects[<agentDir>].hasTrustDialogAccepted=true` entry written by the server; (c) BOTH the user probe hook AND an agenc hook fired (the `--settings` union — acceptance criterion 3 / epic R5); (d) a skill dropped into `$(TEST_ENV_DIR)/claude-config/skills/` AFTER create is visible to the mission without a reload; (e) GUARD: the developer's real `~/.claude.json` mtime is unchanged across the run (proves isolation held). Use the existing `run_test*` helpers; group under `echo "--- State Y native passthrough ---"`.

**Interfaces:**
- Consumes: `WriteMissionOpSettings` (Task 1), `seedMissionTrust`/`pruneMissionTrust`/`reconcileMissionTrust` (Tasks 2/3), the `CLAUDE_CONFIG_DIR`-harness isolation (Task 3).
- Produces: live State Y. Tasks 6–10 delete the now-dead shadow/snapshot/credential machinery.

- [ ] **Step 1: Flip `BuildClaudeCmd`.** Drop `CLAUDE_CONFIG_DIR` + its dirpath line; make the token conditional and delete the no-token error; add `--settings <op-settings file>`; add the UNIT assertion that production `BuildClaudeCmd` sets no `CLAUDE_CONFIG_DIR`. NO HOME override (Task 3 redesign).
- [ ] **Step 2: Rewire the wrapper spawn.** `spawnClaude` writes op-settings then spawns directly; delete `rebuildClaudeConfig`; remove the credential clone/hash/sync starts + writeBack; remove the `SetupOAuthToken` forced calls. **⚠ Correction to the earlier "leave credential_sync.go for Task 6" note:** removing the `go w.watchCredential*Sync(ctx)` starts (and the `initCredentialHash` call) orphans the unexported methods in `credential_sync.go` (`watchCredentialUpwardSync`, `watchCredentialDownwardSync`, `checkUpwardSync`, `handleDownwardSync`, `initCredentialHash`). `golangci-lint`'s `unused` is a HARD gate (unlike informational `deadcode`) and WILL fail `make check` on these. So you MUST either (a) pull Task 6's credential-subsystem deletion forward into this task (delete `credential_sync.go` + the struct fields now — cleanest, keeps `make check` green), or (b) temporarily `//nolint:unused` those methods + fields here and delete in Task 6. RECOMMEND (a): delete the credential subsystem here rather than "leaving it inert" — inert unexported code does not exist as far as `unused` is concerned. If you take (a), Task 6 shrinks to just the keychain/credential-JSON functions in `build.go`/`merge.go`.
- [ ] **Step 3: Wire trust at create/delete.** Seed after `CreateMissionDir` in both create handlers; prune (replacing keychain delete) in the delete handler. Log-and-continue on seed/prune error, at error level with the short-ID (P-4).
- [ ] **Step 4: Wire the migration/reconcile pass** into server startup (one-shot).
- [ ] **Step 5: Strip the wired-symbol `//nolint:unused` annotations** across `trust.go`, `server.go:65`, `operational_settings.go`, `op_settings_file.go`, and the Task-1 config helpers. `make check`'s `unused` linter fails if any wired symbol still carries the annotation OR if any is still genuinely unused — this is the structural proof the wiring is complete.
- [ ] **Step 6: Update the arch-doc spawn sequence** (`:219-223`, `:705-706`) for the new env/settings/trust behavior.
- [ ] **Step 7: Write the mandatory State-Y E2E section** in `scripts/e2e-test.sh` per the Test note (no `CLAUDE_CONFIG_DIR`; trust entry present; hook union fires; post-create skill visible).
- [ ] **Step 8: Verify green.** `make check` THEN `make e2e` (both sandbox-disabled). The e2e MUST write only into `$(TEST_ENV_DIR)/claude-config/.claude.json` — verify by the guard assertion (e) that the developer's real `~/.claude.json` mtime is unchanged across the run. Before running e2e, grep-confirm the spawn path has NO `HOME=` override and that post-flip `BuildClaudeCmd` sets no snapshot `CLAUDE_CONFIG_DIR`. Expected: all green, real `~/.claude.json` untouched, no keychain popups (real `$HOME` intact).
- [ ] **Step 9: Commit.** `"Flip AgenC to State Y native passthrough (agenc-lh8p)"`.

---

### Task 5: Auth CLI + onboarding flip — `agenc token set/clear`, conditional login-presence check

Replaces the forced `claude setup-token` onboarding with the conditional model (§6, Kevin's 2026-07-31 refinement): default = native `~/.claude` auth; the machine token is an opt-in runtime fallback toggled by a dedicated CLI. Governed by `/config-key-checklist`.

**Files:**
- Create: `cmd/token.go` — `agenc token set <token>` (writes via `config.WriteOAuthToken`) and `agenc token clear` (deletes via `config.WriteOAuthToken(agencDirpath, "")`). Register under `rootCmd` (`cmd/root.go` / a new `tokenCmd` parent). Reuse the `sk-ant-` prefix validation from the old `SetupOAuthToken`.
- Modify: `internal/config/config.go` — ADD `EnsureClaudeAuth(agencDirpath) error` (ADDITIVE; does NOT replace or delete `SetupOAuthToken` — Kevin's 2026-08-03 decision: KEEP `SetupOAuthToken`): if the token file is present+non-empty → OK (State-X fallback). Else check for native auth (a `claudeAiOauth` credential — reuse `claudeconfig.GetCredentialExpiresAt() != 0` as the presence probe, or stat the real `~/.claude/.credentials.json`); if present → OK. If NEITHER and no TTY (headless) → **fail loudly** with guidance (`claude auth login`, or `agenc token set <token>`). Interactive → allow Claude's own native login prompt to handle it (return nil; do not force setup-token). **KEEP `SetupOAuthToken` defined** (Task 4 already removed its FORCED spawn-path calls; it stays available as the interactive obtain-and-store-a-token utility — wire it to a command if it would otherwise be an orphaned exported func: e.g. keep `cmd/login.go` calling it, or add `agenc token setup`; executor's call, but do not delete it). Note: e2e does NOT run `setup-token` — `make test-env` copies an existing token; `setup-token` is kept for the human user's State-X-fallback onboarding, not for e2e.
- Modify: `cmd/config_init.go` (`:99`): replace `config.SetupOAuthToken(dirpath)` with `config.EnsureClaudeAuth(dirpath)` (or drop the forced call and print login guidance).
- Modify: `internal/wrapper/wrapper.go` — if Task 4 left a placeholder, call `config.EnsureClaudeAuth` at `setupRun`/`RunHeadless` start (headless fails loudly if unauthenticated; interactive proceeds).
- **`claudeCodeOAuthToken` config key — DECISION (resolve per `/config-key-checklist`): keep as a thin alias, do not retire.** `agenc config set/get claudeCodeOAuthToken` already routes to `WriteOAuthToken`/`ReadOAuthToken` (config_set.go:75, config_get.go:89) — those keep working unchanged and cost nothing. `agenc token set/clear` is the new, discoverable, documented surface. Retiring the key would break any existing user muscle-memory/scripts for zero benefit (YAGNI in reverse — removal is the gratuitous change). So: ADD `agenc token`, KEEP the alias, and update both help texts to cross-reference (`config_set.go:23`, `config_get.go:38`, plus the `supportedConfigKeys` list stays). *(Flag for Kevin below — this is the one CLI-surface judgment call.)*
- Modify: `cmd/login.go` — already deprecated; update its message to point at both `agenc token set` and `claude auth login`.
- Modify: `cmd/doctor.go` (`:99-113` `checkOAuthTokenPermissions`): keep the permission check (still valid when a token file exists); add a note that an absent token file is fine under State Y (native auth). No structural change needed beyond message wording.
- Modify: `docs/authentication.md` (whole file) + `README.md` (`:410`, `:442-444`) + `docs/system-architecture.md` (`:260` token passthrough): rewrite for the conditional model — default native auth, `agenc token set/clear` as the State-X fallback toggle, headless fail-loud behavior.
- Test: `cmd/token_test.go` (set/clear round-trip via a temp agencDir); `internal/config/config_test.go` (KEEP the existing `SetupOAuthToken` tests — the function stays; ADD `EnsureClaudeAuth` behavior tests where feasible).

**Interfaces:**
- Consumes: `config.WriteOAuthToken`/`ReadOAuthToken` (unchanged), `claudeconfig.GetCredentialExpiresAt` (native-auth probe).
- Produces: `agenc token` CLI + `EnsureClaudeAuth`. `SetupOAuthToken` deleted.

- [ ] **Step 1: Add `cmd/token.go`** with `set`/`clear` subcommands + validation; register under root.
- [ ] **Step 2: Replace `SetupOAuthToken` with `EnsureClaudeAuth`;** delete the forced setup-token flow. Update `config_init.go` + wrapper call sites.
- [ ] **Step 3: Keep the `claudeCodeOAuthToken` alias;** update help texts to cross-reference `agenc token`.
- [ ] **Step 4: Update `login.go`, `doctor.go` messaging;** rewrite `docs/authentication.md`, `README.md`, arch-doc token sections.
- [ ] **Step 5: Tests** — `token_test.go` round-trip; adapt config OAuth tests.
- [ ] **Step 6: `/config-key-checklist` sweep** — confirm CLI (`get`/`set` + new `token`), README, and arch-doc all reflect the change.
- [ ] **Step 7: Verify green.** `make check` + `make e2e` (sandbox-disabled).
- [ ] **Step 8: Commit.** `"Add agenc token CLI + conditional auth model for State Y (agenc-lh8p)"`.

---

### Task 6: Delete the credential-sync subsystem + keychain functions

All dead after Task 4 (no goroutines started, no callers). Delete atomically so `deadcode`/`unused` go clean.

**Files:**
- Delete: `internal/wrapper/credential_sync.go` (whole file) + its `_test.go`.
- Modify: `internal/wrapper/wrapper.go` — remove the credential struct fields (`perMissionCredentialHash`, `credentialHashMu`, `lastDownwardSyncTimestamp`, `:76-87`) now that `credential_sync.go` is gone.
- Modify: `internal/claudeconfig/build.go` — delete `ComputeCredentialServiceName` (`:434`), `GlobalCredentialServiceName` (`:442`), `ReadKeychainCredentials` (`:447`), `WriteKeychainCredentials` (`:465`), `CloneKeychainCredentials` (`:486`), `WriteBackKeychainCredentials` (`:508`), `DeleteKeychainCredentials` (`:542`), `ExtractExpiresAtFromJSON` (`:621`), `GetCredentialExpiresAt` (`:650`) — UNLESS `GetCredentialExpiresAt`/`ExtractExpiresAtFromJSON` are retained as the native-auth probe in Task 5's `EnsureClaudeAuth`; if so, KEEP those two and delete the rest. (deadcode will flag whichever are truly orphaned.)
- Delete: `internal/claudeconfig/merge.go` `MergeCredentialJSON` (`:25`), `mergeMcpOAuth` (`:73`), `extractExpiresAt` (`:121`), and `ComputeCredentialHash` (in whichever file) — all credential-JSON helpers with no remaining caller.
- Modify: `internal/config/config.go` — delete `GetGlobalCredentialsExpiryFilepath` (`:265`) + the `GlobalCredentialsExpiryFilename` const if orphaned.
- Modify: `docs/system-architecture.md` — delete the credential-sync narrative (`:229-230`, `:505`), the `credential_sync.go` package description (`:465`), and the keychain sentences in the `build.go` description (`:400`).

- [ ] **Step 1: Delete `credential_sync.go` + test;** remove wrapper credential fields.
- [ ] **Step 2: Delete keychain + credential-JSON functions** across `build.go`/`merge.go`/`config.go` (keep the native-auth probe pair if Task 5 uses it).
- [ ] **Step 3: Update the arch-doc credential sections in the same commit.**
- [ ] **Step 4: Verify green.** `make check` (deadcode must be clean) + `make e2e`.
- [ ] **Step 5: Commit.** `"Delete credential-sync subsystem and keychain functions (agenc-lh8p)"`.

---

### Task 7: Delete the snapshot builder (`BuildMissionConfigDir` + copy/patch/symlink/merge helpers)

**Files:**
- Modify: `internal/claudeconfig/build.go` — delete `BuildMissionConfigDir` (`:41-128`), `buildMergedSettings` (`:219-253`), `symlinkToGlobalClaudeDir` (`:259-277`), `copyDirWithRewriting` (`:299-337`), `copyAndPatchClaudeJSON` (`:345-427`), the `symlinkDirNames` list, and `buildMergedClaudeMd` (`:192-214`) IF not already deleted with the claude-modifications layer (Task 9 — sequence so whichever runs first leaves the tree coherent; RECOMMEND deleting `buildMergedClaudeMd` in Task 9 with the rest of the mods layer, and here delete only the snapshot-copy machinery). Keep `WriteAgencHookScripts` (still called by Task 1's `WriteMissionOpSettings`), `GetLastSessionID`, `GetMissionProjectDirpath`, `ComputeProjectDirpath`, `ProjectDirectoryExists`, `ResolveConfigCommitHash`, `findGitRoot`, `CountCommitsBehind` (verify each caller before deleting).
- Modify: `internal/claudeconfig/merge.go` — delete `RewriteSettingsPaths` (`:464`), `MergeSettings` (`:227`), `MergeSettingsWithAgencOverrides` (`:427`), `mergeAgencHooks` (`:270`), `mergeAgencPermissions` (`:310`), `MergeClaudeMd` (`:203`), `DeepMergeJSON` (`:143`) — whichever have no caller after the snapshot builder and the mods layer are gone. KEEP `mergeAgencSandbox` (`:362`) — it is called by `BuildOperationalSettings`. KEEP `WriteIfChanged` if still used. (deadcode is authoritative; delete iteratively until clean.)
- Modify: `internal/claudeconfig/shadow.go` — delete `RewriteClaudePaths` (`:135`) + `isTextFile`/`getFileExtension` if orphaned (they may be — `copyDirWithRewriting` was their only caller).
- Modify: `internal/claudeconfig/build.go` — delete `TrackableItemNames` (`:27`) if orphaned after `claudeConfigProtectedItems` (overrides.go) is reduced. NOTE `claudeConfigProtectedItems` (overrides.go:33) and `BuildClaudeConfigDenyEntries` (overrides.go:198) are ALSO dead under State Y (no snapshot to protect — `BuildOperationalSettings` intentionally omits the claude-config deny) — delete `BuildClaudeConfigDenyEntries` + `claudeConfigProtectedItems` + `isFileName` here too if orphaned.
- Modify: `docs/system-architecture.md` — delete the "Per-mission config merging" section (`:482-505`), the per-mission `claude-config/` runtime-tree block (`:336-344`), and rewrite the `build.go` package description (`:400`).

- [ ] **Step 1: Delete the snapshot builder + copy/patch/symlink helpers** in `build.go`.
- [ ] **Step 2: Delete the now-orphaned merge + path-rewrite helpers** in `merge.go`/`shadow.go`/`overrides.go` (iterate until `deadcode` is clean; keep `mergeAgencSandbox`, `BuildAgencHookEntries`, `BuildAgentDirAllowEntries`, `BuildRepoLibraryDenyEntries`, `WriteAgencHookScripts`).
- [ ] **Step 3: Update the arch-doc per-mission-config + runtime-tree sections in the same commit.**
- [ ] **Step 4: Verify green.** `make check` (deadcode clean) + `make e2e`.
- [ ] **Step 5: Commit.** `"Delete per-mission claude-config snapshot builder (agenc-lh8p)"`.

---

### Task 8: Delete the shadow repo + SPLIT `config_watcher.go` (keep the config.yml/writeable-copy half)

**This is surgery, not a delete** — `config_watcher.go` owns two unrelated jobs; only the shadow-ingest half goes.

**Files:**
- Delete: `internal/claudeconfig/shadow.go` remaining surface — `GetShadowRepoDirpath`, `InitShadowRepo`, `IngestFromClaudeDir`, `EnsureShadowRepo` (build.go:132), `GetShadowRepoCommitHash` (build.go:161), `TrackedFileNames`, `TrackedDirNames`, and all ingest helpers (`ingestFile`, `ingestDir`, `handleSymlinkEntry`, `removeStaleEntries`, `resolveSymlink`, `commitShadowChanges`). Delete `shadow_test.go`.
- Modify: `internal/server/config_watcher.go` — **KEEP** `runConfigWatcherLoop`'s config.yml watch + `reloadConfig` (cron sync + `reconcileWriteableCopiesFromConfig`, `:194-211`). **DELETE** the shadow half: `EnsureShadowRepo`/`ingestClaudeConfig` calls (`:32-42`), `watchTrackedDirs` (`:120`), `isTrackedPath` (`:147`), `isPathUnder` (`:178`) if orphaned, and the `~/.claude` watch branch inside `watchBothConfigs`. Rewrite `runConfigWatcherLoop` to watch ONLY `config.yml`; rewrite `watchBothConfigs` → `watchAgencConfig` (single watch). Remove the `claudeconfig` import if now unused.
- Modify: `internal/server/missions.go` — `handleCreateMission` (`:359-361`): delete the `claudeconfig.GetShadowRepoCommitHash` → `createParams.ConfigCommit` block (the `config_commit` column stops being populated; leave the column). 
- Modify: `internal/claudeconfig/build.go` — delete `ResolveConfigCommitHash`/`CountCommitsBehind`/`findGitRoot`/`gitOperationTimeout` if orphaned once the shadow repo is gone (verify callers — `config_commit` staleness reporting may reference `CountCommitsBehind`; grep before deleting).
- Modify: `docs/system-architecture.md` — delete the "Shadow repo" section (`:507-519`), the `claude-config-shadow/` runtime-tree line (`:320`), the config-watcher shadow description (`:427`), the config-watcher process bullets (`:127-128`), and the `shadow.go` package line (`:409`). Rewrite the config-watcher description to the config.yml/writeable-copy half only.

- [ ] **Step 1: Split `config_watcher.go`** — keep config.yml + writeable-copy reconcile; delete the shadow-ingest half. Rewrite the watch setup to config.yml-only.
- [ ] **Step 2: Delete `shadow.go` + the shadow entrypoints in `build.go`** (`EnsureShadowRepo`, `GetShadowRepoCommitHash`, commit-hash helpers if orphaned) + `shadow_test.go`.
- [ ] **Step 3: Remove the shadow-commit `ConfigCommit` seeding** in `handleCreateMission`.
- [ ] **Step 4: Update the arch-doc shadow + config-watcher sections in the same commit.**
- [ ] **Step 5: Verify green.** `make check` (deadcode clean) + `make e2e` — assert the config.yml → cron-sync + writeable-copy reconcile path STILL works (add/confirm an e2e that changes config.yml and observes a cron sync, if one exists; else verify via the existing `config cron add` test at `e2e-test.sh:266`).
- [ ] **Step 6: Commit.** `"Delete shadow repo; split config_watcher to config.yml-only (agenc-lh8p)"`.

---

### Task 9: Delete the `claude-modifications` merge layer (after migrating its content to `~/.claude`)

§5 / Q2 (Kevin 2026-07-28): the content is not AgenC-specific — migrate it into `~/.claude`, then delete the whole layer.

**Files:**
- **Content migration (Kevin's one-time action, OR a documented executor step):** move `$AGENC_DIRPATH/config/claude-modifications/CLAUDE.md` → `~/.claude/CLAUDE.md` (append/merge) and `.../settings.json` → `~/.claude/settings.json` (merge). This is a data move, not code — flag it as a required human/ops step in the commit body and the plan's needs-Kevin list. The code deletion below is safe once the content lives in `~/.claude` (which State Y already reads natively).
- Modify: `internal/claudeconfig/build.go` — delete `buildMergedClaudeMd` (`:192-214`) if not already gone in Task 7.
- Modify: `internal/config/config.go` — delete `GetClaudeModificationsDirpath` (`:257`) + `ClaudeModificationsDirname` const.
- Modify: `internal/claudeconfig/merge.go` — delete `MergeClaudeMd`/`DeepMergeJSON`/the mods branches if not already gone in Task 7 (they should be — this task mostly removes the `config` path helper + the `config/claude-modifications/` directory reference).
- Delete: the `config/claude-modifications/` directory handling — there is no code that CREATES it (it is user-populated), so deletion is: remove the path helper + any `EnsureDirStructure` line that mkdir's it (grep `ClaudeModificationsDirname` / `claude-modifications`).
- Modify: `docs/system-architecture.md` — delete the `claude-modifications/` runtime-tree line (`:316`), the "AgenC modifications" source in per-mission-config (`:487` — already gone with Task 7), and any package-desc mention.

- [ ] **Step 1: Confirm content migrated to `~/.claude`** (Kevin's action — see needs-Kevin). Do NOT delete the code path until confirmed, or the content is silently dropped.
- [ ] **Step 2: Delete `GetClaudeModificationsDirpath` + const + any mkdir;** delete `buildMergedClaudeMd`/`MergeClaudeMd` if still present.
- [ ] **Step 3: Update the arch-doc in the same commit.**
- [ ] **Step 4: Verify green.** `make check` (deadcode clean) + `make e2e`.
- [ ] **Step 5: Commit.** `"Delete claude-modifications merge layer (content migrated to ~/.claude) (agenc-lh8p)"`.

---

### Task 10: Delete `SetupOAuthToken` remnants + `GetMissionClaudeConfigDirpath`; final verification

**Files:**
- Modify: `internal/config/config.go` — delete any `SetupOAuthToken` remnants (should be gone in Task 5), `cleanupOldAuthFiles` (`:437`, no-op), and `oauthTokenPrefix` if now only used by `cmd/token.go` (move it there or keep in config — executor's call).
- Modify: `internal/claudeconfig/build.go` — delete `GetMissionClaudeConfigDirpath` (`:170`) once NO caller remains. Verify: `mission.go:68` (deleted Task 4), `missions.go:743` (deleted Task 4), `wrapper.go:137,146` (deleted Task 4/6), `credential_sync.go` (deleted Task 6). Grep to confirm zero callers before deleting.
- Modify: `internal/config/config.go` — evaluate `GetGlobalClaudeDirpath` (`:120`, = `$AGENC_DIRPATH/.claude`): it was the fallback target of `GetMissionClaudeConfigDirpath`. Grep for other callers; delete if orphaned. (This is the C2 hazard from the epic — with the repoint done in Increment 0 and `GetMissionClaudeConfigDirpath` gone here, nothing should resolve session paths to `$AGENC_DIRPATH/.claude` anymore.)
- Modify: `docs/system-architecture.md` — final consistency pass: the `oauth-token` runtime-tree line stays (token still supported as fallback); confirm no dangling shadow/snapshot/credential/claude-modifications references remain anywhere.
- Verify: `internal/claudeconfig/overrides.go` — confirm the surviving surface is exactly `BuildAgencHookEntries`, `BuildAgentDirAllowEntries`, `BuildRepoLibraryDenyEntries`, `mergeAgencSandbox` (in merge.go), `WriteAgencHookScripts`, the SessionStart prime hook, the state-tracking hooks, and the embedded `repo_library_guard.sh` — and that `BuildClaudeConfigDenyEntries`/`claudeConfigProtectedItems`/container variants are all gone.

- [ ] **Step 1: Delete `GetMissionClaudeConfigDirpath`** (grep-confirm zero callers) + `GetGlobalClaudeDirpath` if orphaned + `cleanupOldAuthFiles`.
- [ ] **Step 2: Final arch-doc consistency pass** — grep `docs/system-architecture.md` for `shadow`, `credential`, `keychain`, `claude-modifications`, `CLAUDE_CONFIG_DIR`, `claude-config/` and confirm every survivor is intentional.
- [ ] **Step 3: Full verification.** `make build` + `make check` (deadcode + unused clean — the structural proof nothing shadow/snapshot/credential survives) + `make e2e` (all green, real `~/.claude.json` untouched).
- [ ] **Step 4: Manual tmux check (repo rule — Task 4 touched spawn):** in a live tmux session, create a mission via the palette ("Quick Claude" or "Open <repo>"), confirm it spawns in the foreground, connectors work, and `~/.claude.json` gains the trust entry. Report to Kevin; this is the real-world go-live self-test (separate human step per the go-live model).
- [ ] **Step 5: Commit.** `"Delete GetMissionClaudeConfigDirpath and SetupOAuthToken remnants; final State Y cleanup (agenc-lh8p)"`.
- [ ] **Step 6: Close the bead.** Close agenc-lh8p with a substantive reason once Kevin confirms the manual go-live self-test passes. Note the follow-up cleanup of the now-vestigial `config_commit` column if worth a separate bead.

---

## Self-Review

**Spec coverage** (against the epic plan §3–§7):
- **§3 flip surface** — env switch (`mission.go` `CLAUDE_CONFIG_DIR` drop + conditional token + `--settings`) → Task 4 Step 1; op-settings file + guard script under mission dir → Task 1 + Task 4 Step 2; prime via SessionStart hook in op-settings (no CLAUDE.md merge) → carried by `BuildOperationalSettings`/`BuildAgencHookEntries`, wired Task 4. ✓
- **§3 stop the snapshot rebuild** — `spawnClaude`/`rebuildClaudeConfig` no longer call `BuildMissionConfigDir`; shadow precondition removed; credential clone/writeback/sync goroutines removed → Task 4 Step 2 (removal of starts) + Task 6 (delete subsystem). ✓
- **§4 trust write** — `seedMissionTrust` wired into BOTH create paths after `CreateMissionDir` before `spawnWrapper` (`missions.go:406-411`, `:543-554`); `pruneMissionTrust` replaces `DeleteKeychainCredentials` (`:742-746`); `//nolint:unused` removed → Task 4 Steps 3, 5. ✓
- **§5 op-settings via `--settings`** — Task 1 (file writer) + Task 4 (wire); claude-modifications retired not re-homed → Task 9. ✓
- **§6 auth** — conditional token (default native) done at the flip (Task 4); `agenc token set/clear` + `EnsureClaudeAuth` + headless fail-loud + `claudeCodeOAuthToken` alias-not-retire decision → Task 5. ✓
- **§7 migration** — startup idempotent seed-trust-for-all-existing-missions + prune (= reconcile check-loop) → Task 2 (add) + Task 4 Step 4 (wire). ✓
- **Deletions list (§3 Inc4)** — credential_sync + keychain + MergeCredentialJSON (Task 6); BuildMissionConfigDir + copyDirWithRewriting + copyAndPatchClaudeJSON + symlinkToGlobalClaudeDir + RewriteClaudePaths/RewriteSettingsPaths + old MergeSettings path (Task 7); shadow + config_watcher SPLIT (Task 8); claude-modifications layer (Task 9); SetupOAuthToken forced flow (Task 5) + GetMissionClaudeConfigDirpath (Task 10). ✓
- **Test isolation** — resolved in Task 3 (the `GetClaudeHomeDirpath` seam + test-env HOME override), a hard prerequisite for Task 4. ✓

**`//nolint:unused` removals** — enumerated at Task 4 Step 5: `seedMissionTrust`, `pruneMissionTrust` (trust.go), `claudeJSONMu` (server.go:65), `WriteMissionOpSettings` + Task-1 config helpers, and `homeClaudeJSONFilepath` (removed earlier at Task 2 when `reconcileMissionTrust` references it). The `unused` linter in `make check` is the structural verifier that every removal corresponds to a live caller. ✓

**Placeholder scan** — no TBD/TODO; every task cites file:line against the current tree (verified in this mission); every deletion names the symbol; the one un-file-pinned item (the claude-modifications content move, Task 9) is explicitly flagged as a data/ops step, not code. ✓

**Ordering / compile-safety** — Tasks 1–3 are additive-inert (new code unreferenced or behind the not-yet-flipped switch; the test-env HOME override is inert while `CLAUDE_CONFIG_DIR` is still set). Task 4 flips atomically and wires all three. Tasks 6–10 delete only what Task 4 made dead, in dependency order (credential subsystem → snapshot builder → shadow/watcher → mods layer → final orphans), each ending at a green `make check` with `deadcode`+`unused` clean. The known cross-task coupling — wrapper credential fields read by `credential_sync.go` — is resolved by deleting the file + fields together in Task 6 and merely NOT starting the goroutines in Task 4 (they go inert, not dangling). `buildMergedClaudeMd` ownership between Tasks 7 and 9 is called out to avoid a double-delete/dangling-reference. ✓

**Guidance the executor must honor** — `/brainstorm` first; thin-CLI/thick-server (trust write is server-side ✓); Makefile builds sandbox-disabled; mandatory E2E for the behavioral change (Task 4); `/config-key-checklist` (Task 5); arch-doc same-commit (every deletion task). ✓

---

## Open questions / needs-Kevin

1. **`claudeCodeOAuthToken` config key: keep as alias (my recommendation) vs retire.** I chose keep-as-thin-alias (Task 5) — `agenc config set claudeCodeOAuthToken` already routes to the token file, costs nothing, and retiring it breaks existing muscle-memory/scripts for no benefit. `agenc token set/clear` becomes the primary documented surface. **Confirm you're OK keeping the alias** (reversible either way; this is the one CLI-surface judgment call `/config-key-checklist` surfaces).

2. **claude-modifications content migration (Task 9) is a manual data move you own.** The code deletion is safe ONLY after `config/claude-modifications/{CLAUDE.md,settings.json}` content lands in `~/.claude/{CLAUDE.md,settings.json}`. Per your 2026-07-28 note ("all stuff that could go into my global Claude") this is your call to make once — **do the move, then tell the executor to proceed with Task 9**, or authorize the executor to do the merge for you. Until confirmed, Task 9 holds.

3. **Test-isolation approach — sanity-check the chosen seam.** I picked "isolated HOME under the test-env (`<agencDirpath>/claude-home`), one `GetClaudeHomeDirpath` seam used by both the spawn and the trust-writer, gated on `IsTestEnv()`." This mirrors how `IsTestEnv` already gates cron plists / tmux injection. Tradeoff: the e2e Claude runs against an empty `~/.claude` (so the "skill visible after create" assertion uses a skill the test itself drops into the isolated home — a *more* hermetic test, not a weaker one). The rejected alternatives (stub the spawn / no-op the trust write in tests) would leave acceptance criteria 1–3 unexercised. **This is reversible and I'd proceed without blocking — flagging only so you can veto the seam before Task 3 lands.**

4. **Go-live is your separate step (by design).** Landing Tasks 1–10 on `main` does not change your live behavior; State Y activates when you build+install the new binary and spawn a mission, at which point you self-test connectors + watch `~/.claude.json` under real concurrency (your 2026-07-31 prerequisite). No action needed now — just confirming the model so no one expects the merge itself to flip your running fleet.

---

## Provenance

- Epic / decision source: `docs/plans/2026-07-28-state-y-shadow-removal-plan.md`; `bd show agenc-lh8p`, `bd show agenc-tcoh` (2026-07-26 DECISION + 2026-07-31 auth refinement). Auth half: agenc-lm3d. Cleanup→prune: agenc-c4ko. Devcontainer prereq (done): agenc-ok7h. GitHub issue #13.
- Format exemplar: `docs/plans/2026-07-28-devcontainer-removal-plan.md`.
- Tree state verified in mission cc8e4c39-e2ca-45d5-8818-907de7cfbece (2026-07-31): Increments 0/1/2 confirmed landed; all file:line references checked against the working tree at plan-authoring time.
- This plan: mission cc8e4c39-e2ca-45d5-8818-907de7cfbece.
