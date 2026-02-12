Agent Factory
=============

Architecture Reference
----------------------

Read `docs/system-architecture.md` before making non-trivial changes to the codebase. It is the canonical map of how the system fits together — runtime processes, directory layout, package responsibilities, and cross-cutting patterns.

**Keep it current.** When you make a change that affects any of the following, update `docs/system-architecture.md` in the same commit:

- Adding, removing, or renaming an `internal/` package
- Changing process boundaries (CLI, daemon, wrapper) or their goroutine structure
- Modifying the runtime directory layout under `$AGENC_DIRPATH`
- Altering the database schema
- Adding or changing a key architectural pattern (config merging, idle detection, cron scheduling, etc.)

The architecture doc describes the system at the **filepath level** — no code snippets, no function signatures. If you need to reference something more specific than a file path, that detail belongs in code comments, not in the architecture doc.

Building the Binary
-------------------

Always build via the Makefile — never run `go build` directly. The Makefile injects the version string via ldflags based on git state.

```
# Correct
make build

# Wrong — version will show "unknown"
go build -o agenc .
```

Running the Binary
------------------

When running or testing the `agenc` binary, **always** use the relative path `./agenc` — never the full absolute path.

```
# Correct
./agenc mission new "my mission"
./agenc mission ls

# Wrong — will trigger unnecessary permission prompts
/Users/odyssey/code/agent-factory/agenc mission new "my mission"
```

The project's `.claude/settings.json` allows `Bash(./agenc:*)`. Using the absolute path does not match this pattern and will cause avoidable permission prompts on every invocation.

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

Releasing
---------

Releases are managed by `.goreleaser.yml`. To publish a new release, push a new Git tag to the remote:

```
git tag v1.2.3
git push origin v1.2.3
```

Do not run GoReleaser manually — CI handles the build and publish when it sees a new tag.

**Choosing a version:** Always ask the user what version to tag. Do not assume or auto-increment — the user decides the version number.

**Listing existing tags:** Use `git tag --sort=-v:refname` to list tags in descending semantic-version order. Do not use unsorted `git tag` output.

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
