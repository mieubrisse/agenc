Agent Factory
=============

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

Accessing ~/.agenc
------------------

You have unrestricted `Read`, `Glob`, and `Grep` access to `~/.agenc/` (configured in `.claude/settings.json`). When you need to explore or search files under `~/.agenc/`, **always** use the `Glob` and `Grep` tools — never Bash commands like `ls`, `find`, or `grep`.

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
