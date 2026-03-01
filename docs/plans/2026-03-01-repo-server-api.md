# Repo Server API Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Move repo commands (ls, add, rm) from direct filesystem operations to server API calls, fixing sandbox permission failures when agents run `agenc repo ls`.

**Architecture:** Extract repo resolution logic from `cmd/` into `internal/repo/` package. Add three server endpoints (`GET /repos`, `POST /repos`, `DELETE /repos/`). Refactor CLI commands to be thin clients calling `serverClient()`.

**Tech Stack:** Go, HTTP over unix socket, existing server framework (`appHandler`, `writeJSON`, `newHTTPError`)

**Design doc:** `docs/plans/2026-03-01-repo-server-api-design.md`

---

### Task 1: Create `internal/repo/` package with `FindReposOnDisk`

**Files:**
- Create: `internal/repo/repo.go`
- Create: `internal/repo/repo_test.go`

**Step 1: Write the test**

```go
package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindReposOnDisk_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	reposDirpath := filepath.Join(tmpDir, "repos")
	os.MkdirAll(reposDirpath, 0755)

	repos, err := FindReposOnDisk(reposDirpath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("expected 0 repos, got %d", len(repos))
	}
}

func TestFindReposOnDisk_FindsRepos(t *testing.T) {
	tmpDir := t.TempDir()
	reposDirpath := filepath.Join(tmpDir, "repos")

	// Create host/owner/repo directory structure
	os.MkdirAll(filepath.Join(reposDirpath, "github.com", "alice", "foo"), 0755)
	os.MkdirAll(filepath.Join(reposDirpath, "github.com", "bob", "bar"), 0755)
	os.MkdirAll(filepath.Join(reposDirpath, "github.com", "alice", "baz"), 0755)

	repos, err := FindReposOnDisk(reposDirpath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be sorted alphabetically
	expected := []string{
		"github.com/alice/baz",
		"github.com/alice/foo",
		"github.com/bob/bar",
	}
	if len(repos) != len(expected) {
		t.Fatalf("expected %d repos, got %d: %v", len(expected), len(repos), repos)
	}
	for i, name := range expected {
		if repos[i] != name {
			t.Errorf("repos[%d] = %q, want %q", i, repos[i], name)
		}
	}
}

func TestFindReposOnDisk_MissingDir(t *testing.T) {
	repos, err := FindReposOnDisk("/nonexistent/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("expected 0 repos for missing dir, got %d", len(repos))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/repo/ -run TestFindReposOnDisk -v`
Expected: FAIL — package does not exist yet.

**Step 3: Write implementation**

Create `internal/repo/repo.go`:

```go
package repo

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/mieubrisse/stacktrace"
)

// FindReposOnDisk scans a repos directory for cloned repositories and returns
// their canonical names (e.g. "github.com/owner/repo"), sorted alphabetically.
// The expected directory layout is <reposDirpath>/<host>/<owner>/<repo>/.
// Returns an empty slice (not an error) if the directory does not exist.
func FindReposOnDisk(reposDirpath string) ([]string, error) {
	hosts, err := listSubdirs(reposDirpath)
	if err != nil {
		return nil, err
	}

	var repoNames []string
	for _, host := range hosts {
		hostDirpath := filepath.Join(reposDirpath, host)
		owners, err := listSubdirs(hostDirpath)
		if err != nil {
			return nil, err
		}
		for _, owner := range owners {
			ownerDirpath := filepath.Join(hostDirpath, owner)
			repos, err := listSubdirs(ownerDirpath)
			if err != nil {
				return nil, err
			}
			for _, repo := range repos {
				repoNames = append(repoNames, filepath.Join(host, owner, repo))
			}
		}
	}

	sort.Strings(repoNames)
	return repoNames, nil
}

// listSubdirs returns the names of immediate subdirectories within dirpath.
// Returns an empty slice (not an error) if dirpath does not exist.
func listSubdirs(dirpath string) ([]string, error) {
	entries, err := os.ReadDir(dirpath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, stacktrace.Propagate(err, "failed to read directory '%s'", dirpath)
	}

	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}
	return dirs, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/repo/ -run TestFindReposOnDisk -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/repo/repo.go internal/repo/repo_test.go
git commit -m "Add internal/repo package with FindReposOnDisk"
```

---

### Task 2: Move repo resolution logic to `internal/repo/`

**Files:**
- Modify: `internal/repo/repo.go` (add resolution functions)
- Modify: `cmd/repo_resolution.go` (delegate to internal/repo)
- Modify: `cmd/repo_ls.go` (use internal/repo)

This task moves the following functions from `cmd/repo_resolution.go` to `internal/repo/`:
- `RepoResolutionResult` struct
- `looksLikeRepoReference()` → `LooksLikeRepoReference()`
- `isLocalPath()` → `IsLocalPath()`
- `resolveAsRepoReference()` → `ResolveAsRepoReference()`
- `resolveLocalPathRepo()` → `resolveLocalPathRepo()` (unexported)
- `resolveRemoteRepoReference()` → `resolveRemoteRepoReference()` (unexported)
- `getProtocolPreference()` → `GetProtocolPreference()`
- `getGhConfig()` → `GetGhConfig()` / `GhHostsConfig` / `GhHostConfig` types
- `getGhConfigProtocol()` → `GetGhConfigProtocol()`
- `getGhLoggedInUser()` → `GetGhLoggedInUser()`
- `getDefaultGitHubUser()` → `GetDefaultGitHubUser()`
- `getOriginRemoteURL()` → `GetOriginRemoteURL()`
- `ResolveRepoInput()` / `ResolveRepoInputs()` — these use fzf (UI concern), so they stay in `cmd/` but call `internal/repo/` functions
- `resolveAsSearchTerms()` — also uses fzf, stays in `cmd/`
- `promptForProtocolPreference()` — interactive prompt, stays in `cmd/`

**Step 1: Create `internal/repo/resolution.go`** with the exported functions listed above. Move the logic from `cmd/repo_resolution.go`, adjusting function signatures:
- Functions that previously accessed the `agencDirpath` global now take it as a parameter
- Functions that called `promptForProtocolPreference()` (interactive) instead accept a `preferSSH bool` parameter or return an error that the caller handles
- `getProtocolPreference()` needs special handling: extract the non-interactive parts (gh config check + repo inference) and let the interactive prompt stay in `cmd/`

**Step 2: Create `internal/repo/gh_config.go`** with the GitHub CLI config reading logic (`GhHostsConfig`, `GhHostConfig`, `GetGhConfig`, `GetGhConfigProtocol`, `GetGhLoggedInUser`, `GetDefaultGitHubUser`).

**Step 3: Update `cmd/repo_resolution.go`** to import and delegate to `internal/repo/`. The functions that remain in `cmd/` (`ResolveRepoInput`, `ResolveRepoInputs`, `resolveAsSearchTerms`) call `repo.LooksLikeRepoReference()`, `repo.ResolveAsRepoReference()`, etc. Remove the moved functions. Keep `promptForProtocolPreference()` in `cmd/`.

**Step 4: Update `cmd/repo_ls.go`** to use `repo.FindReposOnDisk()` instead of local `findReposOnDisk()`. Remove the local copy.

**Step 5: Update all other callers in `cmd/`** that reference the moved functions:
- `cmd/repo_add.go` — `looksLikeRepoReference` → `repo.LooksLikeRepoReference`, `getDefaultGitHubUser` → `repo.GetDefaultGitHubUser`, `resolveAsRepoReference` → `repo.ResolveAsRepoReference`
- `cmd/repo_rm.go` — same pattern
- `cmd/mission_new.go` — same pattern
- `cmd/config_init.go` — `getDefaultGitHubUser` → `repo.GetDefaultGitHubUser`
- `cmd/config_cron_add.go` — uses `ResolveRepoInput` which stays in `cmd/`, no change needed
- `cmd/config_cron_update.go` — same, no change needed

**Step 6: Run all tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS — all existing tests pass, no behavioral changes

**Step 7: Commit**

```bash
git add internal/repo/ cmd/
git commit -m "Extract repo resolution logic to internal/repo package"
```

---

### Task 3: Add `GET /repos` server endpoint and client method

**Files:**
- Modify: `internal/server/repos.go` (add handler and types)
- Modify: `internal/server/server.go:199` (register route)
- Modify: `internal/server/client.go` (add `ListRepos` method)

**Step 1: Add types and handler to `internal/server/repos.go`**

Add above the existing `handlePushEvent` function:

```go
// RepoResponse represents a repo in the API response.
type RepoResponse struct {
	Name   string `json:"name"`
	Synced bool   `json:"synced"`
}

// handleListRepos handles GET /repos.
func (s *Server) handleListRepos(w http.ResponseWriter, r *http.Request) error {
	reposDirpath := config.GetReposDirpath(s.agencDirpath)
	repoNames, err := repo.FindReposOnDisk(reposDirpath)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, "failed to scan repos: "+err.Error())
	}

	cfg, _, err := config.ReadAgencConfig(s.agencDirpath)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, "failed to read config: "+err.Error())
	}

	repos := make([]RepoResponse, len(repoNames))
	for i, name := range repoNames {
		repos[i] = RepoResponse{
			Name:   name,
			Synced: cfg.IsAlwaysSynced(name),
		}
	}

	writeJSON(w, http.StatusOK, repos)
	return nil
}
```

**Step 2: Register the route in `internal/server/server.go`**

Add to `registerRoutes()` before the existing `POST /repos/` line:

```go
mux.Handle("GET /repos", appHandler(s.requestLogger, s.handleListRepos))
```

**Step 3: Add client method to `internal/server/client.go`**

Add after the existing mission client methods:

```go
// ============================================================================
// High-level repo API methods
// ============================================================================

// ListRepos fetches all repos from the server.
func (c *Client) ListRepos() ([]RepoResponse, error) {
	var repos []RepoResponse
	if err := c.Get("/repos", &repos); err != nil {
		return nil, err
	}
	return repos, nil
}
```

**Step 4: Run tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 5: Commit**

```bash
git add internal/server/repos.go internal/server/server.go internal/server/client.go
git commit -m "Add GET /repos server endpoint and client method"
```

---

### Task 4: Add `POST /repos` server endpoint and client method

**Files:**
- Modify: `internal/server/repos.go` (add handler and request type)
- Modify: `internal/server/server.go:199` (register route)
- Modify: `internal/server/client.go` (add `AddRepo` method)

**Step 1: Add request/response types and handler to `internal/server/repos.go`**

```go
// AddRepoRequest represents a request to add a repo.
type AddRepoRequest struct {
	Reference    string  `json:"reference"`
	AlwaysSynced *bool   `json:"always_synced,omitempty"`
	WindowTitle  *string `json:"window_title,omitempty"`
}

// AddRepoResponse represents the result of adding a repo.
type AddRepoResponse struct {
	Name           string `json:"name"`
	WasNewlyCloned bool   `json:"was_newly_cloned"`
}

// handleAddRepo handles POST /repos.
func (s *Server) handleAddRepo(w http.ResponseWriter, r *http.Request) error {
	var req AddRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
	}

	if req.Reference == "" {
		return newHTTPError(http.StatusBadRequest, "reference is required")
	}

	defaultGitHubUser := repo.GetDefaultGitHubUser()

	if !repo.LooksLikeRepoReference(req.Reference) {
		return newHTTPErrorf(http.StatusBadRequest,
			"'%s' is not a valid repo reference; expected owner/repo, a URL, or a local path", req.Reference)
	}

	result, err := repo.ResolveAsRepoReference(s.agencDirpath, req.Reference, defaultGitHubUser)
	if err != nil {
		return newHTTPErrorf(http.StatusBadRequest, "failed to resolve repo '%s': %v", req.Reference, err)
	}

	// Update config if flags were provided
	if req.AlwaysSynced != nil || req.WindowTitle != nil {
		cfg, cm, err := config.ReadAgencConfig(s.agencDirpath)
		if err != nil {
			return newHTTPError(http.StatusInternalServerError, "failed to read config: "+err.Error())
		}

		rc, _ := cfg.GetRepoConfig(result.RepoName)
		if req.AlwaysSynced != nil {
			rc.AlwaysSynced = *req.AlwaysSynced
		}
		if req.WindowTitle != nil {
			rc.WindowTitle = *req.WindowTitle
		}
		cfg.SetRepoConfig(result.RepoName, rc)

		if err := config.WriteAgencConfig(s.agencDirpath, cfg, cm); err != nil {
			return newHTTPError(http.StatusInternalServerError, "failed to write config: "+err.Error())
		}
	}

	writeJSON(w, http.StatusCreated, AddRepoResponse{
		Name:           result.RepoName,
		WasNewlyCloned: result.WasNewlyCloned,
	})
	return nil
}
```

Note: `repo.LooksLikeRepoReference` signature changes — the `agencDirpath` parameter is no longer needed since it was only used to scan repos for protocol detection, which is now handled inside `ResolveAsRepoReference`. The function only does string-format classification.

**Step 2: Register the route**

The existing `POST /repos/` (with trailing slash) handles the push-event catch-all. Add a new exact-match route:

```go
mux.Handle("POST /repos", appHandler(s.requestLogger, s.handleAddRepo))
```

Go's `ServeMux` distinguishes exact `POST /repos` from prefix `POST /repos/`.

**Step 3: Add client method to `internal/server/client.go`**

```go
// AddRepo clones a repo and optionally sets config via the server.
func (c *Client) AddRepo(req AddRepoRequest) (*AddRepoResponse, error) {
	var resp AddRepoResponse
	if err := c.Post("/repos", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
```

**Step 4: Run tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 5: Commit**

```bash
git add internal/server/repos.go internal/server/server.go internal/server/client.go
git commit -m "Add POST /repos server endpoint and client method"
```

---

### Task 5: Add `DELETE /repos/` server endpoint and client method

**Files:**
- Modify: `internal/server/repos.go` (add handler)
- Modify: `internal/server/server.go:199` (register route)
- Modify: `internal/server/client.go` (add `RemoveRepo` method)

**Step 1: Add handler to `internal/server/repos.go`**

```go
// handleRemoveRepo handles DELETE /repos/{name...}.
// Removes the repo from disk and config.
func (s *Server) handleRemoveRepo(w http.ResponseWriter, r *http.Request) error {
	repoName := strings.TrimPrefix(r.URL.Path, "/repos/")
	if repoName == "" {
		return newHTTPError(http.StatusBadRequest, "repo name is required")
	}

	repoDirpath := config.GetRepoDirpath(s.agencDirpath, repoName)
	_, statErr := os.Stat(repoDirpath)
	existsOnDisk := statErr == nil

	cfg, cm, err := config.ReadAgencConfig(s.agencDirpath)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, "failed to read config: "+err.Error())
	}

	_, hasRepoConfig := cfg.GetRepoConfig(repoName)

	if !existsOnDisk && !hasRepoConfig {
		return newHTTPError(http.StatusNotFound, "repo not found: "+repoName)
	}

	// Remove from config
	if hasRepoConfig {
		cfg.RemoveRepoConfig(repoName)
		if err := config.WriteAgencConfig(s.agencDirpath, cfg, cm); err != nil {
			return newHTTPError(http.StatusInternalServerError, "failed to write config: "+err.Error())
		}
	}

	// Remove from disk
	if existsOnDisk {
		if err := os.RemoveAll(repoDirpath); err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to remove repo directory: %v", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}
```

**Step 2: Register the route**

The `DELETE /repos/` route needs to coexist with `POST /repos/` (push-event). Both use catch-all prefixes but differ by HTTP method. Add:

```go
mux.Handle("DELETE /repos/", appHandler(s.requestLogger, s.handleRemoveRepo))
```

**Step 3: Add client method to `internal/server/client.go`**

```go
// RemoveRepo removes a repo from disk and config via the server.
func (c *Client) RemoveRepo(repoName string) error {
	return c.Delete("/repos/" + repoName)
}
```

**Step 4: Run tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 5: Commit**

```bash
git add internal/server/repos.go internal/server/server.go internal/server/client.go
git commit -m "Add DELETE /repos/ server endpoint and client method"
```

---

### Task 6: Refactor `repo ls` command to use server API

**Files:**
- Modify: `cmd/repo_ls.go`

**Step 1: Rewrite `runRepoLs` to use `serverClient()`**

Replace the entire `runRepoLs` function. Remove `readConfig()` and `findReposOnDisk()` calls. Instead:

```go
func runRepoLs(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	repos, err := client.ListRepos()
	if err != nil {
		return stacktrace.Propagate(err, "failed to list repos")
	}

	if len(repos) == 0 {
		fmt.Println("No repositories in the repo library.")
		return nil
	}

	tbl := tableprinter.NewTable("REPO", "SYNCED")
	for _, repo := range repos {
		synced := formatCheckmark(repo.Synced)
		tbl.AddRow(displayGitRepo(repo.Name), synced)
	}
	tbl.Print()

	return nil
}
```

Remove `findReposOnDisk` and `listSubdirs` functions from this file (they now live in `internal/repo/`). Keep `formatCheckmark` (display utility).

**Step 2: Update imports** — remove `os`, `path/filepath`, `sort`, `internal/config`. Add `internal/server`.

**Step 3: Run tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 4: Commit**

```bash
git add cmd/repo_ls.go
git commit -m "Refactor repo ls to use server API"
```

---

### Task 7: Refactor `repo add` command to use server API

**Files:**
- Modify: `cmd/repo_add.go`

**Step 1: Rewrite `runRepoAdd` to use `serverClient()`**

Replace the function body. Instead of `getAgencContext()`, `readConfigWithComments()`, and `resolveAsRepoReference()`, call `client.AddRepo()`:

```go
func runRepoAdd(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	for _, arg := range args {
		req := server.AddRepoRequest{
			Reference: arg,
		}

		if cmd.Flags().Changed(repoConfigAlwaysSyncedFlagName) {
			synced, err := cmd.Flags().GetBool(repoConfigAlwaysSyncedFlagName)
			if err != nil {
				return stacktrace.Propagate(err, "failed to read --%s flag", repoConfigAlwaysSyncedFlagName)
			}
			req.AlwaysSynced = &synced
		}

		if cmd.Flags().Changed(repoConfigWindowTitleFlagName) {
			title, err := cmd.Flags().GetString(repoConfigWindowTitleFlagName)
			if err != nil {
				return stacktrace.Propagate(err, "failed to read --%s flag", repoConfigWindowTitleFlagName)
			}
			req.WindowTitle = &title
		}

		resp, err := client.AddRepo(req)
		if err != nil {
			return stacktrace.Propagate(err, "failed to add repo '%s'", arg)
		}

		status := "Added"
		if !resp.WasNewlyCloned {
			status = "Already exists"
		}
		fmt.Printf("%s '%s'\n", status, resp.Name)
	}

	return nil
}
```

**Step 2: Update imports** — remove `goccy/go-yaml`, `internal/config`. Add `internal/server`.

**Step 3: Run tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 4: Commit**

```bash
git add cmd/repo_add.go
git commit -m "Refactor repo add to use server API"
```

---

### Task 8: Refactor `repo rm` command to use server API

**Files:**
- Modify: `cmd/repo_rm.go`

**Step 1: Rewrite `runRepoRm` to use `serverClient()`**

The command still needs to:
1. Get the list of repos (for fzf picker) — now via `client.ListRepos()`
2. Check synced status (for confirmation prompt) — available from the list response
3. Delete repos — via `client.RemoveRepo()`

```go
func runRepoRm(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	repoResponses, err := client.ListRepos()
	if err != nil {
		return stacktrace.Propagate(err, "failed to list repos")
	}

	if len(repoResponses) == 0 {
		fmt.Println("No repositories in the repo library.")
		return nil
	}

	// Build lookup map for synced status
	syncedMap := make(map[string]bool, len(repoResponses))
	var repoNames []string
	for _, r := range repoResponses {
		repoNames = append(repoNames, r.Name)
		syncedMap[r.Name] = r.Synced
	}

	result, err := Resolve(strings.Join(args, " "), Resolver[string]{
		TryCanonical: func(input string) (string, bool, error) {
			if !repo.LooksLikeRepoReference(input) {
				return "", false, nil
			}
			defaultOwner := repo.GetDefaultGitHubUser()
			name, _, err := mission.ParseRepoReference(input, false, defaultOwner)
			if err != nil {
				return "", false, stacktrace.Propagate(err, "invalid repo reference '%s'", input)
			}
			return name, true, nil
		},
		GetItems:          func() ([]string, error) { return repoNames, nil },
		FormatRow:         func(repoName string) []string { return []string{displayGitRepo(repoName)} },
		FzfPrompt:         "Select repos to remove (TAB to multi-select): ",
		FzfHeaders:        []string{"REPO"},
		MultiSelect:       true,
		NotCanonicalError: "not a valid repo reference",
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	for _, repoName := range result.Items {
		// Confirm for synced repos
		if syncedMap[repoName] {
			fmt.Printf("'%s' is a synced repo. Remove it? [y/N] ", repoName)
			reader := bufio.NewReader(os.Stdin)
			input, err := reader.ReadString('\n')
			if err != nil {
				return stacktrace.Propagate(err, "failed to read confirmation")
			}
			if strings.TrimSpace(input) != "y" {
				fmt.Printf("Skipped '%s'\n", repoName)
				continue
			}
		}

		if err := client.RemoveRepo(repoName); err != nil {
			return stacktrace.Propagate(err, "failed to remove repo '%s'", repoName)
		}
		fmt.Printf("Removed '%s'\n", repoName)
	}

	return nil
}
```

Remove the `removeSingleRepo` function — its logic is now split between the CLI (confirmation) and server (deletion).

**Step 2: Update imports** — remove `goccy/go-yaml`, `internal/config`. Add `internal/server`, `internal/repo`.

**Step 3: Run tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 4: Commit**

```bash
git add cmd/repo_rm.go
git commit -m "Refactor repo rm to use server API"
```

---

### Task 9: Clean up unused code

**Files:**
- Modify: `cmd/repo_resolution.go` (remove functions now in `internal/repo/`)
- Modify: `cmd/repo_ls.go` (verify `findReposOnDisk` removed)
- Check: all callers compile correctly

**Step 1: Remove from `cmd/repo_resolution.go`** any functions that were moved to `internal/repo/` in Task 2 and are no longer called from `cmd/`. Specifically, if any moved functions have no remaining callers in `cmd/`, delete them.

**Step 2: Check for unused imports** across all modified files.

**Step 3: Run full test suite**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS — no compilation errors, no test failures

**Step 4: Commit**

```bash
git add cmd/
git commit -m "Remove unused repo functions from cmd package"
```

---

### Task 10: End-to-end verification

**Step 1: Build the binary**

Run: `make build` (with `dangerouslyDisableSandbox: true`)
Expected: PASS — binary compiles

**Step 2: Test `repo ls`**

Run: `./agenc repo ls`
Expected: Lists repos with synced status, same output as before

**Step 3: Test `repo add`**

Run: `./agenc repo add mieubrisse/agenc` (or another test repo)
Expected: "Added" or "Already exists" message

**Step 4: Test `repo rm`**

Run: `./agenc repo rm` (with fzf picker)
Expected: Picker shows repos, deletion works

**Step 5: Verify server handles requests**

Check the server request log:
Run: `tail -5 ~/.agenc/logs/server-requests.log`
Expected: Shows GET /repos, POST /repos, DELETE /repos/ entries

**Step 6: Final commit if any fixes needed**

```bash
git add .
git commit -m "Fix issues found during end-to-end testing"
```
