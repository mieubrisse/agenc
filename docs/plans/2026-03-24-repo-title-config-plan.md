Repo Title Config — Implementation Plan
========================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add an optional `title` config field to repos so the palette shows friendly names and sorts configured repos to the top.

**Architecture:** Add `Title` field to `RepoConfig`, wire it through CLI flags and server API, then update palette entry generation to use title for display and sort by configuration tier.

**Tech Stack:** Go, Cobra CLI, YAML config

---

Task 1: Add `Title` field to `RepoConfig` and `GetRepoTitle` accessor
----------------------------------------------------------------------

**Files:**
- Modify: `internal/config/agenc_config.go:362-372` (RepoConfig struct)
- Modify: `internal/config/agenc_config.go:464-470` (add GetRepoTitle after GetRepoEmoji)
- Test: `internal/config/agenc_config_test.go`

**Step 1: Write the failing test**

Add this test after `TestRepoConfig_GetRepoEmoji` (line 242) in `internal/config/agenc_config_test.go`:

```go
func TestRepoConfig_GetRepoTitle(t *testing.T) {
	cfg := &AgencConfig{
		RepoConfigs: map[string]RepoConfig{
			"github.com/owner/repo1": {Title: "My App"},
			"github.com/owner/repo2": {},
		},
	}

	if got := cfg.GetRepoTitle("github.com/owner/repo1"); got != "My App" {
		t.Errorf("expected 'My App', got '%s'", got)
	}
	if got := cfg.GetRepoTitle("github.com/owner/repo2"); got != "" {
		t.Errorf("expected empty string for repo without title, got '%s'", got)
	}
	if got := cfg.GetRepoTitle("github.com/owner/nonexistent"); got != "" {
		t.Errorf("expected empty string for nonexistent repo, got '%s'", got)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestRepoConfig_GetRepoTitle -v` (with `dangerouslyDisableSandbox: true`)
Expected: FAIL — `Title` field and `GetRepoTitle` don't exist

**Step 3: Write minimal implementation**

In `internal/config/agenc_config.go`, add `Title` field to `RepoConfig` (line 366):

```go
type RepoConfig struct {
	AlwaysSynced      bool               `yaml:"alwaysSynced,omitempty"`
	Emoji             string             `yaml:"emoji,omitempty"`
	Title             string             `yaml:"title,omitempty"`
	TrustedMcpServers *TrustedMcpServers `yaml:"trustedMcpServers,omitempty"`
	DefaultModel      string             `yaml:"defaultModel,omitempty"`
	PostUpdateHook    string             `yaml:"postUpdateHook,omitempty"`
}
```

Update the doc comment above `RepoConfig` to mention title.

Add `GetRepoTitle` after `GetRepoEmoji` (after line 470):

```go
// GetRepoTitle returns the configured title for a repo, or empty string if none is set.
func (c *AgencConfig) GetRepoTitle(repoName string) string {
	if rc, ok := c.RepoConfigs[repoName]; ok {
		return rc.Title
	}
	return ""
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestRepoConfig_GetRepoTitle -v` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/agenc_config.go internal/config/agenc_config_test.go
git commit -m "Add Title field to RepoConfig with GetRepoTitle accessor"
```

Task 2: Add `--title` CLI flag to `repo add` and `config repo-config set`
--------------------------------------------------------------------------

**Files:**
- Modify: `cmd/command_str_consts.go:123-128` (add constant)
- Modify: `cmd/repo_add.go` (add flag and request field)
- Modify: `cmd/config_repo_config_set.go` (add flag and handler)
- Modify: `internal/server/repos.go:54-58` (add Title to AddRepoRequest)
- Modify: `internal/server/repos.go:118-137` (handle Title in handleAddRepo)

**Step 1: Add flag constant**

In `cmd/command_str_consts.go`, add after `repoConfigEmojiFlagName` (line 125):

```go
repoConfigTitleFlagName             = "title"
```

**Step 2: Add `Title` to `AddRepoRequest` and server handler**

In `internal/server/repos.go`, add `Title` field to `AddRepoRequest`:

```go
type AddRepoRequest struct {
	Reference    string  `json:"reference"`
	AlwaysSynced *bool   `json:"always_synced,omitempty"`
	Emoji        *string `json:"emoji,omitempty"`
	Title        *string `json:"title,omitempty"`
}
```

In `handleAddRepo`, update the nil check (line 119) and add title handling:

```go
if req.AlwaysSynced != nil || req.Emoji != nil || req.Title != nil {
```

Add after the emoji block (after line 131):

```go
if req.Title != nil {
    rc.Title = *req.Title
}
```

**Step 3: Wire `--title` flag into `repo add`**

In `cmd/repo_add.go`, update the `Long` description (line 31-32) to mention `--title`:

```go
Use --%s to keep the repo continuously synced by the server.
Use --%s to set an emoji for the repo.
Use --%s to set a friendly title for the repo.`,
    repoConfigAlwaysSyncedFlagName, repoConfigEmojiFlagName, repoConfigTitleFlagName),
```

In `init()` (after line 40), add:

```go
repoAddCmd.Flags().String(repoConfigTitleFlagName, "", "friendly title for the repo (e.g., \"Dotfiles\")")
```

In `runRepoAdd`, add after the emoji block (after line 69):

```go
if cmd.Flags().Changed(repoConfigTitleFlagName) {
    title, err := cmd.Flags().GetString(repoConfigTitleFlagName)
    if err != nil {
        return stacktrace.Propagate(err, "failed to read --%s flag", repoConfigTitleFlagName)
    }
    req.Title = &title
}
```

**Step 4: Wire `--title` flag into `config repo-config set`**

In `cmd/config_repo_config_set.go`:

Add flag registration in `init()` (after line 34):

```go
configRepoConfigSetCmd.Flags().String(repoConfigTitleFlagName, "", "friendly title for the repo (e.g., \"Dotfiles\")")
```

Add `titleChanged` variable (after line 51):

```go
titleChanged := cmd.Flags().Changed(repoConfigTitleFlagName)
```

Update the "at least one flag" check (line 53) to include `titleChanged`:

```go
if !alwaysSyncedChanged && !emojiChanged && !titleChanged && !trustedChanged && !defaultModelChanged && !postUpdateHookChanged {
    return stacktrace.NewError("at least one of --%s, --%s, --%s, --%s, --%s, or --%s must be provided",
        repoConfigAlwaysSyncedFlagName, repoConfigEmojiFlagName, repoConfigTitleFlagName, repoConfigTrustedMcpServersFlagName, repoConfigDefaultModelFlagName, repoConfigPostUpdateHookFlagName)
}
```

Add title handler after the emoji block (after line 83):

```go
if titleChanged {
    title, err := cmd.Flags().GetString(repoConfigTitleFlagName)
    if err != nil {
        return stacktrace.Propagate(err, "failed to read --%s flag", repoConfigTitleFlagName)
    }
    rc.Title = title
}
```

**Step 5: Verify build**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 6: Commit**

```bash
git add cmd/command_str_consts.go cmd/repo_add.go cmd/config_repo_config_set.go internal/server/repos.go
git commit -m "Add --title flag to repo add and config repo-config set"
```

Task 3: Update palette to use title and sort by configuration tier
------------------------------------------------------------------

**Files:**
- Modify: `cmd/tmux_palette.go:67-86` (repo entry generation in `buildPaletteEntries`)

**Step 1: Replace the repo entry loop**

Replace the repo entry block (lines 67-86) in `cmd/tmux_palette.go` with:

```go
	// Append "Open <repo>" entries for each repo in the library.
	// These appear at the bottom of the palette, after all command entries.
	// Repos are sorted by configuration tier: (title+emoji) > (title) > (emoji) > (neither),
	// then alphabetically within each tier.
	repoEntries := listRepoLibrary(agencDirpath)

	type repoDisplayEntry struct {
		repoName    string
		emoji       string
		title       string
		displayName string
		tier        int
	}

	var repoDisplayEntries []repoDisplayEntry
	for _, repoEntry := range repoEntries {
		emoji := ""
		title := ""
		if cfg != nil {
			emoji = cfg.GetRepoEmoji(repoEntry.RepoName)
			title = cfg.GetRepoTitle(repoEntry.RepoName)
		}

		displayName := plainGitRepoName(repoEntry.RepoName)
		if title != "" {
			displayName = title
		}

		displayEmoji := "📦"
		if emoji != "" {
			displayEmoji = emoji
		}

		// Tier: 0 = title+emoji, 1 = title only, 2 = emoji only, 3 = neither
		tier := 3
		hasTitle := title != ""
		hasEmoji := emoji != ""
		if hasTitle && hasEmoji {
			tier = 0
		} else if hasTitle {
			tier = 1
		} else if hasEmoji {
			tier = 2
		}

		repoDisplayEntries = append(repoDisplayEntries, repoDisplayEntry{
			repoName:    repoEntry.RepoName,
			emoji:       displayEmoji,
			title:       title,
			displayName: displayName,
			tier:        tier,
		})
	}

	sort.Slice(repoDisplayEntries, func(i, j int) bool {
		if repoDisplayEntries[i].tier != repoDisplayEntries[j].tier {
			return repoDisplayEntries[i].tier < repoDisplayEntries[j].tier
		}
		return repoDisplayEntries[i].displayName < repoDisplayEntries[j].displayName
	})

	for _, rde := range repoDisplayEntries {
		paletteTitle := fmt.Sprintf("%s  Open %s", rde.emoji, rde.displayName)
		command := fmt.Sprintf("agenc mission new %s", rde.repoName)

		entries = append(entries, config.ResolvedPaletteCommand{
			Name:    "open-repo-" + rde.repoName,
			Title:   paletteTitle,
			Command: command,
		})
	}
```

**Step 2: Add `"sort"` import**

In `cmd/tmux_palette.go`, add `"sort"` to the import block.

**Step 3: Verify build**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 4: Commit**

```bash
git add cmd/tmux_palette.go
git commit -m "Sort palette repo entries by config tier, use title when set"
```

Task 4: Manual verification
----------------------------

1. Set a title on a repo: `agenc config repo-config set github.com/owner/repo --title="My App"`
2. Open the palette — verify it shows "Open My App" instead of "Open owner/repo"
3. Verify repos with title+emoji sort to top, unconfigured repos to bottom
4. Clear a title: `agenc config repo-config set github.com/owner/repo --title=""`
5. Verify it falls back to "Open owner/repo"
