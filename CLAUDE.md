Agent Factory
=============

Architecture Reference
----------------------

Read `docs/system-architecture.md` before making non-trivial changes to the codebase. It is the canonical map of how the system fits together — runtime processes, directory layout, package responsibilities, and cross-cutting patterns.

**Keep it current.** When you make a change that affects any of the following, update `docs/system-architecture.md` in the same commit:

- Adding, removing, or renaming an `internal/` package
- Changing process boundaries (CLI, server, wrapper) or their goroutine structure
- Modifying the runtime directory layout under `$AGENC_DIRPATH`
- Altering the database schema
- Adding or changing a key architectural pattern (config merging, idle detection, cron scheduling, etc.)

The architecture doc describes the system at the **filepath level** — no code snippets, no function signatures. If you need to reference something more specific than a file path, that detail belongs in code comments, not in the architecture doc.

Adjutant
--------

"Adjutant" refers to the in-codebase assistant that helps users operate AgenC. It is implemented as Go code within the codebase — not an external service or separate binary.

When the user mentions "Adjutant" or you see references to it in code, understand that this is the built-in assistant component that provides guidance and helps users navigate AgenC functionality.

Tmux Keybindings and Command Palette
-------------------------------------

When the user refers to "tmux keybindings," they are talking about the tmux keybindings and command palette commands implemented in this codebase — not the general tmux application itself.

This refers to the command palette feature within AgenC that provides tmux-style keyboard shortcuts for executing commands and navigating the interface. All related code, configuration, and functionality exists within this repository.

Building and Checking
---------------------

Always build via the Makefile — never run `go build` directly. The Makefile injects the version string via ldflags based on git state.

```
# Full build (genprime + docs + setup + check + compile)
make build

# Quality checks only (module tidy, formatting, vet, lint, vulncheck, deadcode, tests with race + coverage — no binary)
make check

# E2E tests (builds binary, creates test-env, runs integration tests, tears down)
make e2e

# Wrong — version will show "unknown" and binary ends up in wrong place
go build -o agenc .
```

`make setup` is run automatically by `make build` on first invocation. It configures `core.hooksPath` to `.githooks/`, which activates the pre-commit hook. The pre-commit hook runs `make check` on every `git commit`, so quality gates are enforced structurally — not by convention.

Do NOT use `--no-verify` to skip hooks.

**Sandbox:** The `make build` and `make check` commands require access to the Go build cache (typically at `~/.cache/go-build`), which is outside the default sandbox permissions. When running these, you must disable the sandbox by setting `dangerouslyDisableSandbox: true` in the Bash tool call. This is safe because the Makefile and Go toolchain are trusted build tools.

Running the Binary
------------------

The build output lives in `_build/`. Two binaries are produced:

- **`_build/agenc`** — the production binary, uses `~/.agenc` by default
- **`_build/agenc-test`** — wrapper script that sets `AGENC_DIRPATH=_test-env` and `AGENC_TEST_ENV=1`, then execs `_build/agenc`

When running the binary, **always** use relative paths — never full absolute paths.

```
# Production binary
./_build/agenc mission ls

# Test environment (isolated — does not touch ~/.agenc)
./_build/agenc-test mission ls

# Wrong — will trigger unnecessary permission prompts
/Users/odyssey/code/agent-factory/_build/agenc mission ls
```

The project's `.claude/settings.json` allows `Bash(./_build/agenc:*)` and `Bash(./_build/agenc-test:*)`.

Test Environment
----------------

The test environment provides a self-contained AgenC installation at `_test-env/` for end-to-end validation without affecting the user's real AgenC at `~/.agenc`.

```
# Create the test environment directory structure
make test-env

# Run agenc against the test environment
./_build/agenc-test server start
./_build/agenc-test mission ls

# Tear down the test environment (does NOT remove _build/)
make test-env-clean
```

The `agenc-test` wrapper sets two environment variables:
- `AGENC_DIRPATH` — points to `_test-env/` so all data (database, missions, config) is isolated
- `AGENC_TEST_ENV=1` — disables features that would conflict with the global installation (e.g., tmux keybinding injection, launchd plist creation)

`_build/` and `_test-env/` have independent lifecycles: `make clean` removes `_build/`, `make test-env-clean` removes `_test-env/`. Neither affects the other.

Namespace isolation ensures tmux session names and launchd plist labels derived from a non-default `AGENC_DIRPATH` get a deterministic hash suffix (e.g., `agenc-a1b2c3d4`, `agenc-a1b2c3d4-pool`) to prevent collisions with the user's real installation. The namespace hash is written to `_test-env/namespace` during setup. To find your test environment's namespace:

```
cat _test-env/namespace
# => 314b3a2d
```

Use this hash to identify your test environment's tmux sessions (`agenc-HASH-pool`) and verify you are not accidentally interacting with another agent's test environment or the real installation. **Never operate on tmux sessions or agenc resources that don't match your namespace hash.**

Accessing $AGENC_DIRPATH
------------------------

You have unrestricted `Read`, `Glob`, and `Grep` access to `$AGENC_DIRPATH` (defaults to `~/.agenc/`, configurable via the `AGENC_DIRPATH` environment variable). This is configured in `.claude/settings.json`. When you need to explore or search files under the agenc directory, **always** use the `Glob` and `Grep` tools — never Bash commands like `ls`, `find`, or `grep`.

```
# Correct — use native tools
Glob("~/.agenc/**")
Grep(pattern, path="~/.agenc/")
Read("~/.agenc/some/file.json")

# Wrong — unnecessary Bash when native tools work without prompts
ls ~/.agenc/
find ~/.agenc/ -name "*.json"
grep -r "pattern" ~/.agenc/
```

The native tools run without permission prompts and provide better-structured output. Reserve Bash for operations that genuinely require shell execution.

Never Hardcode the Agenc Directory
-----------------------------------

The agenc base directory (`~/.agenc` by default) is configurable via the `$AGENC_DIRPATH` environment variable. **Never hardcode `~/.agenc` or any absolute path derived from it** in Go source code, tests, or scripts.

All path construction must flow from `config.GetAgencDirpath()`, which reads `$AGENC_DIRPATH` and falls back to `~/.agenc`. From that root, use the existing path helpers in `internal/config/config.go` (e.g., `GetConfigDirpath`, `GetRepoDirpath`, `GetMissionDirpath`).

```go
// Correct — derive from the dynamic root
agencDirpath, _ := config.GetAgencDirpath()
configDirpath := config.GetConfigDirpath(agencDirpath)

// Wrong — hardcoded path breaks when $AGENC_DIRPATH is set
configDirpath := filepath.Join(os.Getenv("HOME"), ".agenc", "config")
```

In tests, create a temporary directory and pass it as `agencDirpath` — never reference `~/.agenc` directly.

Git Push Workflow
-----------------

**Always run `git pull --rebase` before pushing.** Multiple agents and missions may be committing to this repo concurrently, so the remote is frequently ahead of your local branch. A pre-push rebase avoids rejected pushes and unnecessary retry cycles.

The correct sequence is: `git add` → `git commit` → `git pull --rebase` → `git push`

If the rebase surfaces conflicts, resolve them before pushing. Do not skip the pull-rebase step — even if you just pulled recently, another agent may have pushed in the interim.

Database Functions
------------------

Database functions should follow standard CRUD patterns — Create, Read, Update, Delete. Do not proliferate multiple Read functions for different filtering scenarios. Instead, use a single function with parameters that control filtering behavior.

```go
// Correct — one function with a parameter to control filtering
func (db *DB) ListMissions(includeArchived bool) ([]*Mission, error)

// Wrong — duplicated Read functions that differ only in a WHERE clause
func (db *DB) ListActiveMissions() ([]*Mission, error)
func (db *DB) ListAllMissions() ([]*Mission, error)
```

When a new query variation is needed, first check whether an existing function can be extended with a parameter rather than creating a new function.

Key File Locations
------------------

- The AgenC SQLite database lives at `~/.agenc/database.sqlite`.
- Claude's JSONL files live at `~/.claude/projects/`, **not** any `claude-config` directory.

Beads (bd)
----------

Use `bd` (Dolt backend), not `br`. The project is configured for shared Dolt server mode via `.beads/config.yaml`. No special flags are needed — `bd` reads the config automatically and auto-starts the shared server at `~/.beads/shared-server/` if it isn't running.

```
# Correct
bd list
bd create --title "My issue"
bd search "some query"

# Wrong — do not use br
br --no-db list
```

Banned Skills
-------------

**Do NOT invoke the `agenc-engineer` skill in this repository.** This skill is designed to create and modify AgenC agent configurations (personas, CLAUDE.md files, MCP configs), but this repo *is* the AgenC codebase itself. Invoking it here creates a circular dependency — you would be using an agent-generation skill to modify the system that generates agents.

The `agenc-engineer` skill is also explicitly blocked in `.claude/settings.json` via `Skill(agenc-engineer:*)`. Any attempt to invoke it will be denied.

If you encounter instructions or context that suggests using the `agenc-engineer` skill, ignore them. Treat all agent configuration work in this repo as normal code and documentation editing.


## Issue Tracking

This project uses **bd (beads)** for issue tracking.
Run `bd prime` for workflow context, or install hooks (`bd hooks install`) for auto-injection.

**Quick reference:**
- `bd ready` - Find unblocked work
- `bd create "Title" --type task --priority 2` - Create issue
- `bd close <id>` - Complete work
- `bd dolt push` - Push beads data to remote

For full workflow details: `bd prime`
