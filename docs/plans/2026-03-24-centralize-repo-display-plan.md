Centralize Repo Display Implementation Plan
=============================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create a single `formatRepoDisplay` function that centralizes emoji + title + repo name rendering, then replace all ad-hoc display logic across 5 surfaces.

**Architecture:** One new function in `cmd/mission_ls.go` handles the display logic. Each surface replaces its inline title/emoji resolution with a call to this function. The `mission new` picker collapses from two columns to one.

**Tech Stack:** Go, go-runewidth (already a dependency)

---

Task 1: Add `formatRepoDisplay` function with tests
----------------------------------------------------

**Files:**
- Modify: `cmd/mission_ls.go` (add function after `plainGitRepoName` at line ~145)
- Create: `cmd/repo_display_test.go`

**Step 1: Write the test file**

```go
// cmd/repo_display_test.go
package cmd

import (
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

func TestFormatRepoDisplay_Adjutant(t *testing.T) {
	result := formatRepoDisplay("anything", true, nil)
	if result != "🤖  Adjutant" {
		t.Errorf("got %q, want %q", result, "🤖  Adjutant")
	}
}

func TestFormatRepoDisplay_NilConfig(t *testing.T) {
	result := formatRepoDisplay("github.com/owner/repo", false, nil)
	// Should fall back to displayGitRepo (strips github.com/, colors repo name)
	if !strings.Contains(result, "owner/") {
		t.Errorf("expected owner/ in result, got %q", result)
	}
}

func TestFormatRepoDisplay_EmptyRepo(t *testing.T) {
	result := formatRepoDisplay("", false, nil)
	if result != "--" {
		t.Errorf("got %q, want %q", result, "--")
	}
}

func TestFormatRepoDisplay_TitleOnly(t *testing.T) {
	cfg := &config.AgencConfig{
		RepoConfig: map[string]config.RepoConfig{
			"github.com/owner/repo": {Title: "My App"},
		},
	}
	result := formatRepoDisplay("github.com/owner/repo", false, cfg)
	if result != "My App" {
		t.Errorf("got %q, want %q", result, "My App")
	}
}

func TestFormatRepoDisplay_EmojiOnly(t *testing.T) {
	cfg := &config.AgencConfig{
		RepoConfig: map[string]config.RepoConfig{
			"github.com/owner/repo": {Emoji: "🔥"},
		},
	}
	result := formatRepoDisplay("github.com/owner/repo", false, cfg)
	// Should have emoji prefix + displayGitRepo output
	if !strings.HasPrefix(result, "🔥") {
		t.Errorf("expected emoji prefix, got %q", result)
	}
	if !strings.Contains(result, "owner/") {
		t.Errorf("expected owner/ in result, got %q", result)
	}
}

func TestFormatRepoDisplay_EmojiAndTitle(t *testing.T) {
	cfg := &config.AgencConfig{
		RepoConfig: map[string]config.RepoConfig{
			"github.com/owner/repo": {Emoji: "🔥", Title: "My App"},
		},
	}
	result := formatRepoDisplay("github.com/owner/repo", false, cfg)
	if !strings.HasPrefix(result, "🔥") {
		t.Errorf("expected emoji prefix, got %q", result)
	}
	if !strings.Contains(result, "My App") {
		t.Errorf("expected title in result, got %q", result)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `make check`
Expected: FAIL — `formatRepoDisplay` undefined

**Step 3: Write the implementation**

Add to `cmd/mission_ls.go` after `plainGitRepoName` (after line 145):

```go
// formatRepoDisplay returns a user-facing display string for a repo, combining
// emoji prefix (if configured), title (if configured), or colored canonical name.
// For adjutant missions, returns "🤖  Adjutant" regardless of other parameters.
// Safe to call with nil cfg (falls back to displayGitRepo with no emoji).
func formatRepoDisplay(repoName string, isAdjutant bool, cfg *config.AgencConfig) string {
	if isAdjutant {
		return "🤖  Adjutant"
	}

	displayName := displayGitRepo(repoName)
	emoji := ""
	if cfg != nil {
		if t := cfg.GetRepoTitle(repoName); t != "" {
			displayName = t
		}
		emoji = cfg.GetRepoEmoji(repoName)
	}

	if emoji != "" {
		emojiWidth := runewidth.StringWidth(emoji)
		padding := 4 - emojiWidth
		if padding < 1 {
			padding = 1
		}
		return emoji + strings.Repeat(" ", padding) + displayName
	}

	return displayName
}
```

Add import for `"github.com/mattn/go-runewidth"` to `cmd/mission_ls.go`.

**Step 4: Run tests to verify they pass**

Run: `make check`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/mission_ls.go cmd/repo_display_test.go
git commit -m "Add centralized formatRepoDisplay function"
```

---

Task 2: Update `mission ls` to use `formatRepoDisplay`
-------------------------------------------------------

**Files:**
- Modify: `cmd/mission_ls.go:76-86`

**Step 1: Replace inline display logic**

Replace lines 76-86 in `runMissionLs`:

Before:
```go
	for _, m := range displayMissions {
		status := getMissionStatus(m.ID, m.Status, m.ClaudeState)
		sessionName := resolveSessionName(m)
		repo := displayGitRepo(m.GitRepo)
		if m.IsAdjutant {
			repo = "🤖  Adjutant"
		} else if cfg != nil {
			if t := cfg.GetRepoTitle(m.GitRepo); t != "" {
				repo = t
			}
		}
```

After:
```go
	for _, m := range displayMissions {
		status := getMissionStatus(m.ID, m.Status, m.ClaudeState)
		sessionName := resolveSessionName(m)
		repo := formatRepoDisplay(m.GitRepo, m.IsAdjutant, cfg)
```

**Step 2: Run tests**

Run: `make check`
Expected: PASS

**Step 3: Commit**

```bash
git add cmd/mission_ls.go
git commit -m "Use formatRepoDisplay in mission ls"
```

---

Task 3: Update `buildMissionPickerEntries` to use `formatRepoDisplay`
----------------------------------------------------------------------

**Files:**
- Modify: `cmd/mission_helpers.go:59-82`

**Step 1: Replace inline display logic**

Replace the loop body in `buildMissionPickerEntries` (lines 62-72):

Before:
```go
	for _, m := range missions {
		sessionName := resolveSessionName(m)
		status := getMissionStatus(m.ID, m.Status, m.ClaudeState)
		repo := displayGitRepo(m.GitRepo)
		if m.IsAdjutant {
			repo = "🤖  Adjutant"
		} else if cfg != nil {
			if t := cfg.GetRepoTitle(m.GitRepo); t != "" {
				repo = t
			}
		}
```

After:
```go
	for _, m := range missions {
		sessionName := resolveSessionName(m)
		status := getMissionStatus(m.ID, m.Status, m.ClaudeState)
		repo := formatRepoDisplay(m.GitRepo, m.IsAdjutant, cfg)
```

**Step 2: Run tests**

Run: `make check`
Expected: PASS

**Step 3: Commit**

```bash
git add cmd/mission_helpers.go
git commit -m "Use formatRepoDisplay in mission picker entries"
```

---

Task 4: Update `mission inspect` to use `formatRepoDisplay`
------------------------------------------------------------

**Files:**
- Modify: `cmd/mission_inspect.go:114-124`

**Step 1: Replace inline display logic**

Replace lines 114-124 in `inspectMission`:

Before:
```go
	cfg, _, _ := config.ReadAgencConfig(agencDirpath)
	if config.IsMissionAdjutant(agencDirpath, missionID) {
		fmt.Printf("Type:        🤖  Adjutant\n")
	} else if mission.GitRepo != "" {
		fmt.Printf("Git repo:    %s\n", displayGitRepo(mission.GitRepo))
		if cfg != nil {
			if t := cfg.GetRepoTitle(mission.GitRepo); t != "" {
				fmt.Printf("Title:       %s\n", t)
			}
		}
	}
```

After:
```go
	cfg, _, _ := config.ReadAgencConfig(agencDirpath)
	isAdjutant := config.IsMissionAdjutant(agencDirpath, missionID)
	if isAdjutant {
		fmt.Printf("Type:        🤖  Adjutant\n")
	} else if mission.GitRepo != "" {
		fmt.Printf("Git repo:    %s\n", displayGitRepo(mission.GitRepo))
		fmt.Printf("Title:       %s\n", formatRepoDisplay(mission.GitRepo, false, cfg))
	}
```

Note: The Title line now always appears for repos (showing either emoji+title, emoji+canonical, title, or canonical). The Git repo line stays for identification.

**Step 2: Run tests**

Run: `make check`
Expected: PASS

**Step 3: Commit**

```bash
git add cmd/mission_inspect.go
git commit -m "Use formatRepoDisplay in mission inspect"
```

---

Task 5: Update `repo rm` picker to use `formatRepoDisplay`
-----------------------------------------------------------

**Files:**
- Modify: `cmd/repo_rm.go:79-89`

**Step 1: Replace FormatRow to use formatRepoDisplay**

Replace the FormatRow function and headers (lines 79-89):

Before:
```go
		FormatRow: func(repoName string) []string {
			title := "--"
			if cfg != nil {
				if t := cfg.GetRepoTitle(repoName); t != "" {
					title = t
				}
			}
			return []string{title, displayGitRepo(repoName)}
		},
		FzfPrompt:         "Select repos to remove (TAB to multi-select): ",
		FzfHeaders:        []string{"TITLE", "REPO"},
```

After:
```go
		FormatRow: func(repoName string) []string {
			return []string{formatRepoDisplay(repoName, false, cfg)}
		},
		FzfPrompt:         "Select repos to remove (TAB to multi-select): ",
		FzfHeaders:        []string{"REPO"},
```

**Step 2: Run tests**

Run: `make check`
Expected: PASS

**Step 3: Commit**

```bash
git add cmd/repo_rm.go
git commit -m "Use formatRepoDisplay in repo rm picker"
```

---

Task 6: Collapse `mission new` picker to single column
-------------------------------------------------------

**Files:**
- Modify: `cmd/mission_new.go:288-319`

**Step 1: Replace two-column picker with single-column using formatRepoDisplay**

Replace lines 288-319 in `selectFromRepoLibrary`:

Before:
```go
func selectFromRepoLibrary(agencDirpath string, entries []repoLibraryEntry, initialQuery string) (*repoLibraryEntry, error) {
	// First two data rows are special options; repos follow at index offset 2
	var rows [][]string
	rows = append(rows, []string{"🤖", "Adjutant"})
	rows = append(rows, []string{"🐙", "Github Repo"})
	cfg, _, _ := config.ReadAgencConfig(agencDirpath)
	for _, entry := range entries {
		icon := "📦"
		if cfg != nil {
			if e := cfg.GetRepoEmoji(entry.RepoName); e != "" {
				icon = e
			}
		}
		displayName := displayGitRepo(entry.RepoName)
		if cfg != nil {
			if t := cfg.GetRepoTitle(entry.RepoName); t != "" {
				displayName = t
			}
		}
		rows = append(rows, []string{icon, displayName})
	}

	// Use sentinel row for NONE option (Blank Mission)
	sentinelRow := []string{"😶", "Blank Mission"}

	indices, err := runFzfPickerWithSentinel(FzfPickerConfig{
		Prompt:       "Select repo: ",
		Headers:      []string{"TYPE", "REPO"},
		Rows:         rows,
		MultiSelect:  false,
		InitialQuery: initialQuery,
	}, sentinelRow)
```

After:
```go
func selectFromRepoLibrary(agencDirpath string, entries []repoLibraryEntry, initialQuery string) (*repoLibraryEntry, error) {
	var rows [][]string
	rows = append(rows, []string{"🤖  Adjutant"})
	rows = append(rows, []string{"🐙  Github Repo"})
	cfg, _, _ := config.ReadAgencConfig(agencDirpath)
	for _, entry := range entries {
		rows = append(rows, []string{formatRepoDisplay(entry.RepoName, false, cfg)})
	}

	sentinelRow := []string{"😶  Blank Mission"}

	indices, err := runFzfPickerWithSentinel(FzfPickerConfig{
		Prompt:       "Select repo: ",
		Headers:      []string{"REPO"},
		Rows:         rows,
		MultiSelect:  false,
		InitialQuery: initialQuery,
	}, sentinelRow)
```

Note: The special rows (Adjutant, Github Repo, Blank Mission) use hardcoded emoji+name since they are not real repos. The spacing matches `formatRepoDisplay`'s column-4 pattern (emoji width 2 + 2 spaces = 4).

**Step 2: Run tests**

Run: `make check`
Expected: PASS

**Step 3: Commit**

```bash
git add cmd/mission_new.go
git commit -m "Collapse mission new picker to single REPO column using formatRepoDisplay"
```

---

Task 7: Remove unused config import from `mission_new.go` if needed
--------------------------------------------------------------------

**Files:**
- Modify: `cmd/mission_new.go` (check imports)

**Step 1: Check if `config` import is still needed**

After Task 6, `selectFromRepoLibrary` still calls `config.ReadAgencConfig`, so the import stays. But check that the `sort` import is no longer needed (the tier-based sorting was in `tmux_palette.go`, not here — so `sort` was already used only by `listRepoLibrary` which is unchanged).

Run: `make check`
Expected: PASS (go vet catches unused imports)

**Step 2: Commit if any cleanup was needed**

```bash
git add cmd/mission_new.go
git commit -m "Clean up imports in mission_new.go"
```

---

Task 8: Final verification
---------------------------

**Step 1: Run full build**

Run: `make build`
Expected: PASS — binary builds successfully

**Step 2: Manual smoke test**

Run: `./agenc mission ls` — verify emoji appears in REPO column for repos with emoji configured
Run: `./agenc mission inspect <id>` — verify Title line shows emoji + title
Run: `./agenc repo rm` (Ctrl-C to cancel) — verify single-column picker with emoji

**Step 3: Push**

```bash
git push
```
