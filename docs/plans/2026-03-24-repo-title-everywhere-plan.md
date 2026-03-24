Repo Title Display Everywhere — Implementation Plan
====================================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Show the configured repo title across all user-facing surfaces — mission ls, mission attach, mission inspect, mission new picker, repo ls, repo rm, config repo-config ls, and tmux window titles.

**Architecture:** Each surface already displays repo names via `displayGitRepo` or similar. Add config reads where missing, look up `cfg.GetRepoTitle(repoName)`, and use the title when set. For space-constrained surfaces (mission ls, attach), title replaces the repo name. For repo-management surfaces, title is a new column alongside the canonical name.

**Tech Stack:** Go, Cobra CLI, fzf pickers, tmux

---

Task 1: mission ls — show title in REPO column
------------------------------------------------

**Files:**
- Modify: `cmd/mission_ls.go:47-114`

**Step 1: Add config read and use title in REPO column**

In `runMissionLs`, add a config read after fetching missions. Then when building the repo display, prefer title over `displayGitRepo`.

In `runMissionLs`, after the empty-check block (after line 60), add:

```go
	cfg, _ := readConfig()
```

Replace the repo display block (lines 77-80):

```go
		repo := displayGitRepo(m.GitRepo)
		if m.IsAdjutant {
			repo = "🤖  Adjutant"
		}
```

With:

```go
		repo := displayGitRepo(m.GitRepo)
		if m.IsAdjutant {
			repo = "🤖  Adjutant"
		} else if cfg != nil {
			if t := cfg.GetRepoTitle(m.GitRepo); t != "" {
				repo = t
			}
		}
```

**Step 2: Verify build**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 3: Commit**

```bash
git add cmd/mission_ls.go
git commit -m "Show repo title in mission ls REPO column when set"
```

Task 2: mission attach picker — show title in REPO column
-----------------------------------------------------------

**Files:**
- Modify: `cmd/mission_helpers.go:55-78`

**Step 1: Add config parameter and use title**

`buildMissionPickerEntries` doesn't have config access. Add a `cfg *config.AgencConfig` parameter.

Update the function signature (line 59):

```go
func buildMissionPickerEntries(missions []*database.Mission, sessionMaxLen int, cfg *config.AgencConfig) []missionPickerEntry {
```

Replace the repo display block (lines 64-67):

```go
		repo := displayGitRepo(m.GitRepo)
		if m.IsAdjutant {
			repo = "🤖  Adjutant"
		}
```

With:

```go
		repo := displayGitRepo(m.GitRepo)
		if m.IsAdjutant {
			repo = "🤖  Adjutant"
		} else if cfg != nil {
			if t := cfg.GetRepoTitle(m.GitRepo); t != "" {
				repo = t
			}
		}
```

**Step 2: Update all callers of `buildMissionPickerEntries`**

Search for all call sites and add the `cfg` argument. Each caller needs to read config if it doesn't already.

Run `grep -rn "buildMissionPickerEntries" cmd/` to find all callers. For each caller, add config access and pass it through.

**Step 3: Verify build**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 4: Commit**

```bash
git add cmd/
git commit -m "Show repo title in mission attach and other mission pickers"
```

Task 3: mission inspect — add Title line
------------------------------------------

**Files:**
- Modify: `cmd/mission_inspect.go:114-118`

**Step 1: Add title display**

The inspect command already reads `agencDirpath`. Add a config read and title line.

After line 113 (`getMissionStatus` line), add config read:

```go
	cfg, _, _ := config.ReadAgencConfig(agencDirpath)
```

Replace lines 114-118:

```go
	if config.IsMissionAdjutant(agencDirpath, missionID) {
		fmt.Printf("Type:        🤖  Adjutant\n")
	} else if mission.GitRepo != "" {
		fmt.Printf("Git repo:    %s\n", displayGitRepo(mission.GitRepo))
	}
```

With:

```go
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

**Step 2: Verify build**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 3: Commit**

```bash
git add cmd/mission_inspect.go
git commit -m "Show repo title in mission inspect output"
```

Task 4: mission new picker — show title in picker rows
--------------------------------------------------------

**Files:**
- Modify: `cmd/mission_new.go:288-302`

**Step 1: Use title in picker rows**

In `selectFromRepoLibrary`, the config is already read at line 293. Use title when set.

Replace line 301:

```go
		rows = append(rows, []string{icon, displayGitRepo(entry.RepoName)})
```

With:

```go
		displayName := displayGitRepo(entry.RepoName)
		if cfg != nil {
			if t := cfg.GetRepoTitle(entry.RepoName); t != "" {
				displayName = t
			}
		}
		rows = append(rows, []string{icon, displayName})
```

**Step 2: Verify build**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 3: Commit**

```bash
git add cmd/mission_new.go
git commit -m "Show repo title in mission new picker"
```

Task 5: repo ls — add TITLE column
------------------------------------

**Files:**
- Modify: `cmd/repo_ls.go:40-50`

**Step 1: Add TITLE column**

Replace lines 40-49:

```go
	tbl := tableprinter.NewTable("EMOJI", "REPO", "SYNCED")
	for _, r := range repos {
		emoji := "--"
		if cfg != nil {
			if e := cfg.GetRepoEmoji(r.Name); e != "" {
				emoji = e
			}
		}
		synced := formatCheckmark(r.Synced)
		tbl.AddRow(emoji, displayGitRepo(r.Name), synced)
	}
```

With:

```go
	tbl := tableprinter.NewTable("EMOJI", "TITLE", "REPO", "SYNCED")
	for _, r := range repos {
		emoji := "--"
		title := "--"
		if cfg != nil {
			if e := cfg.GetRepoEmoji(r.Name); e != "" {
				emoji = e
			}
			if t := cfg.GetRepoTitle(r.Name); t != "" {
				title = t
			}
		}
		synced := formatCheckmark(r.Synced)
		tbl.AddRow(emoji, title, displayGitRepo(r.Name), synced)
	}
```

**Step 2: Verify build**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 3: Commit**

```bash
git add cmd/repo_ls.go
git commit -m "Add TITLE column to repo ls output"
```

Task 6: repo rm picker — add title column
-------------------------------------------

**Files:**
- Modify: `cmd/repo_rm.go:64-82`

**Step 1: Add config read and title column to picker**

After the `syncedMap` block (after line 62), add config read:

```go
	cfg, _ := readConfig()
```

Replace the `FormatRow` and `FzfHeaders` in the Resolver (lines 77-79):

```go
		FormatRow:         func(repoName string) []string { return []string{displayGitRepo(repoName)} },
		FzfPrompt:         "Select repos to remove (TAB to multi-select): ",
		FzfHeaders:        []string{"REPO"},
```

With:

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
		FzfPrompt:  "Select repos to remove (TAB to multi-select): ",
		FzfHeaders: []string{"TITLE", "REPO"},
```

**Step 2: Verify build**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 3: Commit**

```bash
git add cmd/repo_rm.go
git commit -m "Add title column to repo rm picker"
```

Task 7: config repo-config ls — add TITLE column
--------------------------------------------------

**Files:**
- Modify: `cmd/config_repo_config_ls.go:43-56`

**Step 1: Add TITLE column**

Replace lines 43-55:

```go
	tbl := tableprinter.NewTable("REPO", "ALWAYS SYNCED", "EMOJI", "DEFAULT MODEL", "TRUSTED MCP SERVERS")
	for _, name := range repoNames {
		rc := cfg.RepoConfigs[name]
		synced := formatCheckmark(rc.AlwaysSynced)
		emoji := rc.Emoji
		if emoji == "" {
			emoji = "--"
		}
		defaultModel := rc.DefaultModel
		if defaultModel == "" {
			defaultModel = "--"
		}
		tbl.AddRow(displayGitRepo(name), synced, emoji, defaultModel, formatTrustedMcpServers(rc.TrustedMcpServers))
	}
```

With:

```go
	tbl := tableprinter.NewTable("REPO", "TITLE", "ALWAYS SYNCED", "EMOJI", "DEFAULT MODEL", "TRUSTED MCP SERVERS")
	for _, name := range repoNames {
		rc := cfg.RepoConfigs[name]
		synced := formatCheckmark(rc.AlwaysSynced)
		title := rc.Title
		if title == "" {
			title = "--"
		}
		emoji := rc.Emoji
		if emoji == "" {
			emoji = "--"
		}
		defaultModel := rc.DefaultModel
		if defaultModel == "" {
			defaultModel = "--"
		}
		tbl.AddRow(displayGitRepo(name), title, synced, emoji, defaultModel, formatTrustedMcpServers(rc.TrustedMcpServers))
	}
```

**Step 2: Verify build**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 3: Commit**

```bash
git add cmd/config_repo_config_ls.go
git commit -m "Add TITLE column to config repo-config ls output"
```

Task 8: tmux window title — use title as fallback
---------------------------------------------------

**Files:**
- Modify: `internal/server/tmux.go:28-83` (reconcileTmuxWindowTitle and determineBestTitle)

**Step 1: Pass repo title into determineBestTitle**

Add a `repoTitle` parameter to `determineBestTitle`:

```go
func determineBestTitle(activeSession *database.Session, mission *database.Mission, repoTitle string) string {
```

Replace the "Priority 4: repo short name" block (lines 73-79):

```go
	// Priority 4: repo short name
	if mission.GitRepo != "" {
		repoName := extractRepoShortName(mission.GitRepo)
		if repoName != "" {
			return repoName
		}
	}
```

With:

```go
	// Priority 4: repo title (from config), falling back to repo short name
	if repoTitle != "" {
		return repoTitle
	}
	if mission.GitRepo != "" {
		repoName := extractRepoShortName(mission.GitRepo)
		if repoName != "" {
			return repoName
		}
	}
```

**Step 2: Update the caller in reconcileTmuxWindowTitle**

In `reconcileTmuxWindowTitle`, before calling `determineBestTitle` (line 43), read the repo title from config:

```go
	// Read repo title from config for window title fallback
	repoTitle := ""
	cfg, _, cfgErr := config.ReadAgencConfig(s.agencDirpath)
	if cfgErr == nil && cfg != nil && mission.GitRepo != "" {
		repoTitle = cfg.GetRepoTitle(mission.GitRepo)
	}

	bestTitle := determineBestTitle(activeSession, mission, repoTitle)
```

Note: `reconcileTmuxWindowTitle` is a `Server` method, and `s.agencDirpath` is available. The config import may already be present in this file (check imports — `config` is used for `IsMissionAdjutant`).

**Step 3: Verify build**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 4: Commit**

```bash
git add internal/server/tmux.go
git commit -m "Use repo title in tmux window title fallback"
```

Task 9: Manual verification
-----------------------------

1. Set a title: `agenc config repo-config set github.com/owner/repo --title="My App"`
2. Check each surface:
   - `agenc mission ls` — REPO column shows "My App" for missions using that repo
   - `agenc mission attach` picker — shows "My App" in REPO column
   - `agenc mission inspect <id>` — shows "Title:       My App" line
   - `agenc mission new` picker — shows "My App" instead of "owner/repo"
   - `agenc repo ls` — shows TITLE column with "My App"
   - `agenc repo rm` picker — shows TITLE column with "My App"
   - `agenc config repo-config ls` — shows TITLE column with "My App"
   - Tmux window title — shows "My App" instead of "repo" for missions without a custom/auto title
3. Clear the title: `agenc config repo-config set github.com/owner/repo --title=""`
4. Verify all surfaces fall back to `owner/repo` display
