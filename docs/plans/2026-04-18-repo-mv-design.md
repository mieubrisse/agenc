Repo Move (Rename) Command
==========================

Status: Approved
Date: 2026-04-18

Problem
-------

When a GitHub repository is renamed or transferred to a different owner, AgenC's local state (filesystem, config, git remote URL) becomes stale. Users currently must `repo rm` + `repo add` and manually re-apply config. There should be an atomic `repo mv` command.

Design
------

### CLI

`agenc repo mv <old-name> <new-name>` — two required positional args. Both are resolved through `ParseRepoReference` so shorthand (`owner/repo`) works. No fzf picker.

### Server Endpoint

`POST /repos/{old-name}/mv` with body `{"new_name": "github.com/new-owner/new-repo"}`.

Performs three operations under the config lock:

1. **Rename filesystem directory** — `os.Rename` from `repos/<old-host>/<old-owner>/<old-repo>` to `repos/<new-host>/<new-owner>/<new-repo>`, creating intermediate directories as needed.
2. **Migrate config entry** — read `repoConfigs[old-name]`, write to `repoConfigs[new-name]`, delete `repoConfigs[old-name]`, save.
3. **Update git remote** — run `git remote set-url origin <new-clone-url>` on the library clone. Detect SSH vs HTTPS from the current remote URL and match it.

After success, clean up empty parent directories from the old path.

### Error Handling

**Defer-undo pattern on rename:** After `os.Rename` succeeds, defer a rollback that renames back to the old path. Cancel the rollback (`shouldRollbackRename = false`) only when the entire operation succeeds.

**Git remote set-url failure:** If the remote URL update fails after rename + config succeed, the command prints an ACTION REQUIRED error with the exact `git -C <path> remote set-url origin <url>` command the user needs to run manually. The rename and config migration are not rolled back — the repo is usable, just pointing at the old remote.

### Validations

- Old name must exist on disk in the repo library
- New name must not already exist on disk or in config
- Both args must be valid repo references

### What It Does NOT Do

- Does not update existing missions' `git_repo` fields in the database
- Does not touch mission workspaces
- Existing missions that referenced the old name will lose auto-update-on-push detection but otherwise continue working via GitHub's URL redirect

### Client Method

`MoveRepo(oldName, newName string) error` — calls the server endpoint.

### Touch Points

- `cmd/repo_mv.go` — new CLI command
- `internal/server/repos.go` — `handleMoveRepo` handler, `MoveRepoRequest` type
- `internal/server/server.go` — route registration
- `internal/server/client.go` — `MoveRepo` client method
- `scripts/e2e-test.sh` — E2E tests for move

### No Changes To

Database schema, mission table, wrapper, keybindings, palette.
