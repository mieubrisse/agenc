Repo Server API
===============

Problem
-------

`agenc repo ls` (and other repo commands) fail when called from inside a sandboxed Claude Code mission. Every repo command calls `getAgencContext()` → `ensureConfigured()` → `EnsureDirStructure()`, which unconditionally writes to `~/.agenc/statusline-wrapper.sh`. The mission sandbox blocks writes outside the mission directory, so the command fails with "operation not permitted" before it does any actual work.

Repo commands currently bypass the server entirely — they do direct filesystem operations (scanning `$AGENC_DIRPATH/repos/`, git cloning, config read/write). Meanwhile, mission commands already go through the server via HTTP over unix socket, which runs outside the sandbox.

Decision
--------

Move all three repo commands (`ls`, `add`, `rm`) to server API endpoints. The CLI becomes a thin client that calls `serverClient()` — same pattern as mission commands. The server handles filesystem access, git operations, and config writes.

This also establishes the server as a config writer for repo operations (previously the server only read config.yml). Config commands (`config get/set/unset`, `config cron *`, etc.) are out of scope and stay as-is.

API Endpoints
-------------

| Method | Path | Purpose |
|--------|------|---------|
| `GET /repos` | List repos with synced status | Replaces `findReposOnDisk()` + config read |
| `POST /repos` | Clone a repo, optionally set config | Replaces `resolveAsRepoReference()` + config write |
| `DELETE /repos/{name...}` | Remove repo from disk and config | Replaces `removeSingleRepo()` |

### GET /repos

Response:

```json
[
  {"name": "github.com/owner/repo", "synced": true},
  {"name": "github.com/other/lib", "synced": false}
]
```

### POST /repos

Request:

```json
{
  "reference": "owner/repo",
  "always_synced": true,
  "window_title": "my-title"
}
```

The `reference` field accepts any format: shorthand (`repo`, `owner/repo`), canonical (`github.com/owner/repo`), URL, or local path. The server handles resolution, protocol detection, and git cloning.

`always_synced` and `window_title` are optional (pointer fields). When omitted, no config change is made for that field.

Response:

```json
{
  "name": "github.com/owner/repo",
  "was_newly_cloned": true
}
```

### DELETE /repos/{name...}

The `{name...}` wildcard captures the full canonical name (e.g., `github.com/owner/repo`). No request body. Returns 204 No Content on success.

Client Methods
--------------

New methods on `server.Client` in `internal/server/client.go`:

```go
func (c *Client) ListRepos() ([]RepoResponse, error)
func (c *Client) AddRepo(req AddRepoRequest) (*AddRepoResponse, error)
func (c *Client) RemoveRepo(repoName string) error
```

Types in `internal/server/repos.go`:

```go
type RepoResponse struct {
    Name   string `json:"name"`
    Synced bool   `json:"synced"`
}

type AddRepoRequest struct {
    Reference    string  `json:"reference"`
    AlwaysSynced *bool   `json:"always_synced,omitempty"`
    WindowTitle  *string `json:"window_title,omitempty"`
}

type AddRepoResponse struct {
    Name           string `json:"name"`
    WasNewlyCloned bool   `json:"was_newly_cloned"`
}
```

Code Organization
-----------------

### New package: `internal/repo/`

Extract repo resolution and filesystem logic into a standalone package the server imports. Keeps server handlers thin and the logic independently testable.

Moves from `cmd/` to `internal/repo/`:
- `resolveAsRepoReference()` and all sub-functions (`resolveRemoteRepoReference`, `resolveLocalPathRepo`)
- `looksLikeRepoReference()` — input classification
- `getDefaultGitHubUser()` / `readGhHostsConfig()` — gh config reading for shorthand resolution
- `findReposOnDisk()` / `listSubdirs()` — filesystem walk of repos directory
- Protocol detection logic

### Stays in `cmd/`

- Cobra command definitions and flag parsing
- `serverClient()` calls — thin client pattern
- Table formatting (`tableprinter`) for `repo ls` display
- fzf picker infrastructure for `repo rm` — fed by `GET /repos` data
- Interactive confirmation for synced repos in `repo rm` — CLI prompts user locally, then calls `DELETE`
- `displayGitRepo()`, `formatCheckmark()` — display formatting
- `Resolve()` generic infrastructure — used by other commands too

CLI Command Changes
-------------------

### `repo ls`

Calls `client.ListRepos()`, formats result into table. No more `readConfig()` or `findReposOnDisk()`.

### `repo add`

Validates args are repo references locally (basic format check). Loops through args calling `client.AddRepo()` for each. Prints "Added" or "Already exists" based on `WasNewlyCloned`. No more `readConfigWithComments()`, `resolveAsRepoReference()`, or `WriteAgencConfig()`.

### `repo rm`

Calls `client.ListRepos()` to populate fzf picker and check synced status. After user selects repos and confirms (prompted locally for synced repos), calls `client.RemoveRepo()` for each. No more `readConfigWithComments()` or direct filesystem deletion.

All three commands use `serverClient()` which handles `getAgencContext()` + `ensureServerRunning()`. The server process — not the CLI — runs `EnsureDirStructure()`, so sandbox restrictions don't apply.

Scope Boundaries
----------------

**Not in scope:**
- Config commands (`config get/set/unset`, `config cron *`, `config palette-command *`, `config repo-config *`) — stay as local CLI operations
- `ensureConfigured()` / `EnsureDirStructure()` refactoring — the fix is that repo commands now go through the server
- General config write API — server writes config.yml only for repo add/rm
- `Resolve()` generic infrastructure — stays in `cmd/`
- Existing `POST /repos/{name...}/push-event` endpoint — unchanged
