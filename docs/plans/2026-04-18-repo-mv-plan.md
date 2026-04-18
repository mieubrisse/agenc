Repo Move Implementation Plan
=============================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `agenc repo mv <old-name> <new-name>` to rename repos in the library after a GitHub rename/transfer.

**Architecture:** CLI command resolves both args to canonical names, calls the server. Server handler renames the filesystem directory, migrates the config entry, and updates the git remote URL — all under the config lock with defer-undo rollback on the rename.

**Tech Stack:** Go, Cobra CLI, net/http server, stacktrace error handling

---

### Task 1: Add `mvCmdStr` constant

**Files:**
- Modify: `cmd/command_str_consts.go`

**Step 1: Add the constant**

Add `mvCmdStr = "mv"` to the "Subcommands shared across multiple parent commands" block, alongside `lsCmdStr`, `rmCmdStr`, `addCmdStr`, etc.

**Step 2: Commit**

```bash
git add cmd/command_str_consts.go
git commit -m "Add mvCmdStr constant for repo mv command"
```

---

### Task 2: Add server types and handler

**Files:**
- Modify: `internal/server/repos.go`

**Step 1: Add the request type**

Add `MoveRepoRequest` struct after the existing `AddRepoResponse`:

```go
// MoveRepoRequest is the JSON body for POST /repos/{name...}/mv.
type MoveRepoRequest struct {
	NewName string `json:"new_name"`
}
```

**Step 2: Write the handler**

Add `handleMoveRepo` to `repos.go`. The handler must:

1. Extract the old repo name from the URL path (same pattern as `handlePushEvent` — trim `/repos/` prefix and `/mv` suffix).
2. Decode `MoveRepoRequest` from the body to get `newName`.
3. Validate: old name exists on disk, new name does NOT exist on disk or in config.
4. Acquire config lock.
5. Create parent directories for the new path (`os.MkdirAll` on the parent of the new repo dirpath).
6. `os.Rename` old dirpath to new dirpath.
7. Immediately defer rollback: `shouldRollbackRename := true; defer func() { if shouldRollbackRename { os.Rename(newDirpath, oldDirpath) } }()`
8. Read config, migrate `repoConfigs[oldName]` to `repoConfigs[newName]`, delete old key, write config.
9. Derive the new clone URL: read the current remote URL from the moved clone via `repo.GetOriginRemoteURL(newDirpath)`, then use `mission.ParseRepoReference(newName, isSSH, "")` to get the correct new clone URL (where `isSSH` is determined by checking if the current remote starts with `git@`). Run `git remote set-url origin <newCloneURL>` on the moved clone. If this fails, return an error with an ACTION REQUIRED message containing the manual fix command — but do NOT roll back the rename or config (those succeeded).
10. Clean up empty parent directories from the old path (walk up from the old repo dirpath, removing empty dirs until we hit the repos root).
11. Set `shouldRollbackRename = false`.
12. Return 200 with `{"old_name": "...", "new_name": "..."}`.

For running `git remote set-url`, use `exec.CommandContext` with a timeout, same pattern as `GetOriginRemoteURL` in `internal/repo/resolution.go`.

**Step 3: Commit**

```bash
git add internal/server/repos.go
git commit -m "Add handleMoveRepo server handler with defer-undo rollback"
```

---

### Task 3: Update route dispatcher for `POST /repos/`

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/repos.go`

**Step 1: Add a dispatcher handler**

Currently `POST /repos/` routes directly to `handlePushEvent`. Since both push-event and mv need to share this catch-all (repo names contain slashes), add a dispatcher:

```go
func (s *Server) handleRepoAction(w http.ResponseWriter, r *http.Request) error {
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, "/push-event"):
		return s.handlePushEvent(w, r)
	case strings.HasSuffix(path, "/mv"):
		return s.handleMoveRepo(w, r)
	default:
		return newHTTPError(http.StatusNotFound, "unknown repo action")
	}
}
```

**Step 2: Update route registration**

In `registerRoutes`, change:
```go
mux.Handle("POST /repos/", appHandler(s.requestLogger, s.handlePushEvent))
```
to:
```go
mux.Handle("POST /repos/", appHandler(s.requestLogger, s.handleRepoAction))
```

Update the comment above to reflect that this is now a dispatcher.

**Step 3: Commit**

```bash
git add internal/server/server.go internal/server/repos.go
git commit -m "Add POST /repos/ dispatcher for push-event and mv actions"
```

---

### Task 4: Add client method

**Files:**
- Modify: `internal/server/client.go`

**Step 1: Add MoveRepo method**

Add after `RemoveRepo`:

```go
// MoveRepo renames a repo in the library via the server.
func (c *Client) MoveRepo(oldName, newName string) error {
	req := MoveRepoRequest{NewName: newName}
	return c.Post("/repos/"+oldName+"/mv", req, nil)
}
```

**Step 2: Commit**

```bash
git add internal/server/client.go
git commit -m "Add MoveRepo client method"
```

---

### Task 5: Add CLI command

**Files:**
- Create: `cmd/repo_mv.go`

**Step 1: Write the command**

Follow the pattern of `cmd/repo_add.go`. Two required positional args (`cobra.ExactArgs(2)`). Resolve both through `mission.ParseRepoReference` to get canonical names (use `repo.GetDefaultGitHubUser()` for the default owner, same as `repo_rm.go`). Call `client.MoveRepo(oldName, newName)`. Print confirmation.

Use the `repo_rm.go` pattern for checking `LooksLikeRepoReference` and calling `ParseRepoReference`. Use `preferSSH: false` — the SSH preference only matters for clone URLs, and the server handler determines the protocol from the existing remote.

Help text should document accepted formats (same as `repo rm`) and explain that this is for GitHub renames/transfers.

**Step 2: Commit**

```bash
git add cmd/repo_mv.go
git commit -m "Add repo mv CLI command"
```

---

### Task 6: Add E2E tests

**Files:**
- Modify: `scripts/e2e-test.sh`

**Step 1: Add repo mv tests**

Add to the "Repo commands" section, after the existing `repo ls` test. The test flow:

1. Add a real public repo (e.g., `mieubrisse/stacktrace` — small, fast to clone).
2. Verify it appears in `repo ls`.
3. Run `repo mv mieubrisse/stacktrace mieubrisse/stacktrace-renamed` — this is a synthetic rename (the remote won't match, but the filesystem + config migration is what we're testing).
4. Verify `repo ls` shows the new name, NOT the old name.
5. Run `repo mv nonexistent/repo foo/bar` — verify it fails (exit 1).
6. Clean up: `repo rm mieubrisse/stacktrace-renamed`.

Note: The `git remote set-url` step will construct a URL based on the new name. Since this is a synthetic rename (the GitHub repo wasn't actually renamed), the new remote URL will point to a nonexistent repo. That's fine for E2E testing — we're testing the rename mechanics, not the remote.

**Step 2: Commit**

```bash
git add scripts/e2e-test.sh
git commit -m "Add E2E tests for repo mv"
```

---

### Task 7: Build and verify

**Step 1: Run `make build`**

Verify the project compiles.

**Step 2: Run `make e2e`**

Verify all E2E tests pass, including the new repo mv tests.

**Step 3: Final commit if any fixes needed**
