Palette "Open Repo" Entries — Implementation Plan
==================================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add dynamic "Open owner/repo" entries to the command palette for each registered repo, so users can launch a mission without going through the New Mission picker.

**Architecture:** Modify `buildPaletteEntries()` in `cmd/tmux_palette.go` to scan the repo library at palette display time and append synthetic `ResolvedPaletteCommand` entries after the existing command entries. Add a `plainGitRepoName()` helper for ANSI-free repo name formatting.

**Tech Stack:** Go, existing `config` and `repo` packages, fzf-based palette UI

---

Task 1: Add `plainGitRepoName` helper
--------------------------------------

**Files:**
- Modify: `cmd/mission_ls.go` (add function near `displayGitRepo` at line 120)

**Step 1: Write the function**

Add `plainGitRepoName` directly below `displayGitRepo` in `cmd/mission_ls.go`. This returns the human-friendly repo name without any ANSI codes — needed for palette entry titles where `formatPaletteEntryLine` adds its own formatting.

```go
// plainGitRepoName returns a human-friendly repo name without ANSI codes.
// GitHub repos have their "github.com/" prefix stripped; non-GitHub repos
// are shown in full. Returns empty string for empty input.
func plainGitRepoName(gitRepo string) string {
	if gitRepo == "" {
		return ""
	}
	return strings.TrimPrefix(gitRepo, "github.com/")
}
```

**Step 2: Verify build**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS — new function compiles, no test failures

**Step 3: Commit**

```bash
git add cmd/mission_ls.go
git commit -m "Add plainGitRepoName helper for ANSI-free repo name display"
```

Task 2: Inject repo entries into `buildPaletteEntries`
------------------------------------------------------

**Files:**
- Modify: `cmd/tmux_palette.go:36-67` (`buildPaletteEntries` function)

**Step 1: Update `buildPaletteEntries` to append repo entries**

Replace the `buildPaletteEntries` function with the following. The only change is after the existing command-entry loop: scan the repo library, construct a `ResolvedPaletteCommand` per repo, and append them.

```go
// buildPaletteEntries returns the resolved palette entries from config,
// followed by "Open <repo>" entries for each repo in the library.
// Only entries with a non-empty Title are included in the palette.
// Mission-scoped entries are excluded when callingMissionUUID is empty (i.e.
// the palette was opened from a pane that is not running a mission).
// On config read failure, returns an error.
func buildPaletteEntries(callingMissionUUID string) ([]config.ResolvedPaletteCommand, error) {
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to get agenc dirpath")
	}

	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to read config for palette commands")
	}

	resolved := cfg.GetResolvedPaletteCommands()

	var entries []config.ResolvedPaletteCommand
	for _, cmd := range resolved {
		if cmd.Title == "" {
			continue
		}
		// Hide mission-scoped commands when not in a mission pane
		if cmd.IsMissionScoped() && callingMissionUUID == "" {
			continue
		}
		entries = append(entries, cmd)
	}

	// Append "Open <repo>" entries for each repo in the library.
	// These appear at the bottom of the palette, after all command entries.
	repoEntries := listRepoLibrary(agencDirpath)
	for _, repoEntry := range repoEntries {
		emoji := "📦"
		if cfg != nil {
			if e := cfg.GetRepoEmoji(repoEntry.RepoName); e != "" {
				emoji = e
			}
		}

		title := fmt.Sprintf("%s  Open %s", emoji, plainGitRepoName(repoEntry.RepoName))
		command := fmt.Sprintf("agenc mission new %s", repoEntry.RepoName)

		entries = append(entries, config.ResolvedPaletteCommand{
			Name:    "open-repo-" + repoEntry.RepoName,
			Title:   title,
			Command: command,
		})
	}

	return entries, nil
}
```

**Step 2: Verify build**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS — all tests pass, no compilation errors

**Step 3: Commit**

```bash
git add cmd/tmux_palette.go
git commit -m "Add Open Repo entries to command palette"
```

Task 3: Manual verification
----------------------------

This task is for the human to verify interactively. The following should be confirmed:

1. Open the command palette (prefix + a, k)
2. Repo entries appear at the bottom, below all command entries
3. Each repo shows as "{emoji}  Open owner/repo" (with github.com stripped)
4. Repos with configured emoji show that emoji; others show 📦
5. Selecting a repo entry launches a new mission for that repo
6. With zero repos registered, the palette shows only command entries (no errors)
7. Typing in fzf filters repo entries by the "Open owner/repo" text
