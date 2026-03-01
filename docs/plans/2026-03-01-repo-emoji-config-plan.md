Repo Emoji Config Implementation Plan
======================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the `windowTitle` per-repo config field with `emoji` — prepended to tmux window titles, shown in fzf picker, and displayed in `repo ls`.

**Architecture:** The server maintains a cached `AgencConfig` via `atomic.Pointer`, updated on fsnotify changes. Emoji is prepended to window titles at the `applyTmuxTitle` choke point with fixed-column-4 padding. Wrapper window-renaming is removed entirely — only the server reconciliation loop manages window names.

**Tech Stack:** Go, `go-runewidth` (existing dep), `fsnotify` (existing dep), `sync/atomic`

---

Task 1: Config Schema Change — WindowTitle → Emoji
----------------------------------------------------

**Files:**
- Modify: `internal/config/agenc_config.go:324-329` (RepoConfig struct)
- Modify: `internal/config/agenc_config.go:422-429` (GetWindowTitle → GetRepoEmoji)
- Modify: `internal/config/agenc_config_test.go:19-53` (update tests)

**Step 1: Update RepoConfig struct**

In `internal/config/agenc_config.go`, replace the `WindowTitle` field in `RepoConfig`:

```go
type RepoConfig struct {
	AlwaysSynced      bool               `yaml:"alwaysSynced,omitempty"`
	Emoji             string             `yaml:"emoji,omitempty"`
	TrustedMcpServers *TrustedMcpServers `yaml:"trustedMcpServers,omitempty"`
	DefaultModel      string             `yaml:"defaultModel,omitempty"`
	PostUpdateHook    string             `yaml:"postUpdateHook,omitempty"`
}
```

**Step 2: Rename GetWindowTitle → GetRepoEmoji**

Replace `GetWindowTitle` (lines 422-429) with:

```go
// GetRepoEmoji returns the configured emoji for a repo, or empty string if none is set.
func (c *AgencConfig) GetRepoEmoji(repoName string) string {
	if rc, ok := c.RepoConfigs[repoName]; ok {
		return rc.Emoji
	}
	return ""
}
```

**Step 3: Update tests**

In `internal/config/agenc_config_test.go`, update `TestReadWriteAgencConfig`:

- Line 22: Change `WindowTitle: "Custom"` → `Emoji: "🔥"`
- Line 51: Change `rc2.WindowTitle != "Custom"` → `rc2.Emoji != "🔥"`
- Line 52: Update error message accordingly

**Step 4: Run tests to verify**

Run: `go test ./internal/config/ -run TestReadWriteAgencConfig -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/agenc_config.go internal/config/agenc_config_test.go
git commit -m "Replace WindowTitle with Emoji in RepoConfig"
```


Task 2: CLI Flag and Command Updates
--------------------------------------

**Files:**
- Modify: `cmd/command_str_consts.go:119`
- Modify: `cmd/config_repo_config_set.go:23-24,34,48,73-79`
- Modify: `cmd/config_repo_config_ls.go:43,47-49,55`
- Modify: `cmd/repo_add.go:32-33,40,63-68`
- Modify: `cmd/repo_ls.go:38-41`

**Step 1: Rename flag constant**

In `cmd/command_str_consts.go` line 119, replace:

```go
repoConfigWindowTitleFlagName       = "window-title"
```

with:

```go
repoConfigEmojiFlagName             = "emoji"
```

**Step 2: Update config_repo_config_set.go**

Update the Long description (line 23-24): change `--window-title="my-repo"` examples to `--emoji="🔥"`.

Update `init()` (line 34): change flag registration:
```go
configRepoConfigSetCmd.Flags().String(repoConfigEmojiFlagName, "", "emoji displayed before tmux window titles and in repo lists")
```

Update `runConfigRepoConfigSet`:
- Line 48: `windowTitleChanged` → `emojiChanged`, use `repoConfigEmojiFlagName`
- Lines 53-55: Update the error message to reference `repoConfigEmojiFlagName`
- Lines 73-79: Rename the flag handler block — change `windowTitleChanged` to `emojiChanged`, `repoConfigWindowTitleFlagName` to `repoConfigEmojiFlagName`, and `rc.WindowTitle = title` to `rc.Emoji = emoji` (rename local var too)

**Step 3: Update config_repo_config_ls.go**

Line 43: Change table header `"WINDOW TITLE"` → `"EMOJI"`

Lines 47-49: Change:
```go
windowTitle := rc.WindowTitle
if windowTitle == "" {
    windowTitle = "--"
}
```
to:
```go
emoji := rc.Emoji
if emoji == "" {
    emoji = "--"
}
```

Line 55: Change `windowTitle` → `emoji` in `tbl.AddRow()`

**Step 4: Update repo_add.go**

Line 32-33: Update Long description to reference `--emoji` instead of `--window-title`:
```go
Use --%s to set an emoji for visual identification.`,
    repoConfigAlwaysSyncedFlagName, repoConfigEmojiFlagName),
```

Line 40: Change flag registration:
```go
repoAddCmd.Flags().String(repoConfigEmojiFlagName, "", "emoji displayed before tmux window titles and in repo lists")
```

Lines 63-68: Rename handler from `repoConfigWindowTitleFlagName` to `repoConfigEmojiFlagName`, and `req.WindowTitle` to `req.Emoji`:
```go
if cmd.Flags().Changed(repoConfigEmojiFlagName) {
    emoji, err := cmd.Flags().GetString(repoConfigEmojiFlagName)
    if err != nil {
        return stacktrace.Propagate(err, "failed to read --%s flag", repoConfigEmojiFlagName)
    }
    req.Emoji = &emoji
}
```

**Step 5: Update repo_ls.go — add EMOJI column**

Change `runRepoLs` to add EMOJI as the first column:

```go
tbl := tableprinter.NewTable("EMOJI", "REPO", "SYNCED")

cfg, _, _ := config.ReadAgencConfig(agencDirpath)

for _, r := range repos {
    synced := formatCheckmark(r.Synced)
    emoji := "--"
    if cfg != nil {
        if e := cfg.GetRepoEmoji(r.Name); e != "" {
            emoji = e
        }
    }
    tbl.AddRow(emoji, displayGitRepo(r.Name), synced)
}
```

This needs `config` imported and access to `agencDirpath`. Check how other commands access it — it's available as a package-level var in the `cmd` package.

**Step 6: Run tests**

Run: `go test ./cmd/ -v`
Expected: PASS (or at least no regressions from our changes)

**Step 7: Commit**

```bash
git add cmd/command_str_consts.go cmd/config_repo_config_set.go cmd/config_repo_config_ls.go cmd/repo_add.go cmd/repo_ls.go
git commit -m "Rename --window-title CLI flag to --emoji, add EMOJI column to repo ls"
```


Task 3: Server API — WindowTitle → Emoji
------------------------------------------

**Files:**
- Modify: `internal/server/repos.go:57,119,129-130`

**Step 1: Update AddRepoRequest struct**

In `internal/server/repos.go` line 57, change:
```go
WindowTitle  *string `json:"window_title,omitempty"`
```
to:
```go
Emoji  *string `json:"emoji,omitempty"`
```

**Step 2: Update handleAddRepo handler**

Line 119: Change `req.WindowTitle` → `req.Emoji`
Lines 129-130: Change:
```go
if req.WindowTitle != nil {
    rc.WindowTitle = *req.WindowTitle
}
```
to:
```go
if req.Emoji != nil {
    rc.Emoji = *req.Emoji
}
```

**Step 3: Run tests**

Run: `go test ./internal/server/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/server/repos.go
git commit -m "Rename WindowTitle to Emoji in AddRepoRequest"
```


Task 4: Cached AgencConfig in Server
--------------------------------------

**Files:**
- Modify: `internal/server/server.go:1-46,104-105,182-197`
- Modify: `internal/server/config_watcher.go:87-89,211-222`

**Step 1: Add cachedConfig field and imports**

In `internal/server/server.go`, add `"sync/atomic"` to imports and add the field to Server struct:

```go
type Server struct {
	agencDirpath  string
	socketPath    string
	logger        *log.Logger
	requestLogger *slog.Logger
	httpServer    *http.Server
	listener      net.Listener
	db            *database.DB

	// cachedConfig holds the most recent parsed AgencConfig, updated via fsnotify.
	// Reads are lock-free via atomic.Pointer; only the config watcher goroutine writes.
	cachedConfig atomic.Pointer[config.AgencConfig]

	// Background loop state (formerly in the Daemon struct)
	repoUpdateCycleCount int
	cronSyncer           *CronSyncer

	// Repo update worker
	repoUpdateCh chan repoUpdateRequest
}
```

**Step 2: Add getConfig helper**

Add after `NewServer`:

```go
// getConfig returns the cached AgencConfig. Returns an empty config if the
// cache has not been populated yet (should not happen after startup).
func (s *Server) getConfig() *config.AgencConfig {
	cfg := s.cachedConfig.Load()
	if cfg == nil {
		return &config.AgencConfig{}
	}
	return cfg
}
```

**Step 3: Populate cache on startup**

In `syncCronsOnStartup` (lines 182-197), the config is already read. After the read, store it in the cache. Rename the function to `loadConfigOnStartup` and store the config:

```go
// loadConfigOnStartup reads the config, caches it, and performs initial cron sync.
func (s *Server) loadConfigOnStartup() {
	cfg, _, err := config.ReadAgencConfig(s.agencDirpath)
	if err != nil {
		s.logger.Printf("Failed to read config on startup: %v", err)
		return
	}

	s.cachedConfig.Store(cfg)

	if len(cfg.Crons) == 0 {
		s.logger.Println("Cron syncer: no cron jobs configured")
		return
	}

	if err := s.cronSyncer.SyncCronsToLaunchd(cfg.Crons, s.logger); err != nil {
		s.logger.Printf("Failed to sync crons on startup: %v", err)
	}
}
```

Update the call site at line 105: `s.syncCronsOnStartup()` → `s.loadConfigOnStartup()`

**Step 4: Update config watcher to refresh cache**

In `internal/server/config_watcher.go`, rename `syncCronsAfterConfigChange` to `reloadConfig` and update it to also store the config:

```go
// reloadConfig re-reads config.yml, updates the cached config, and re-syncs crons.
func (s *Server) reloadConfig() {
	cfg, _, err := config.ReadAgencConfig(s.agencDirpath)
	if err != nil {
		s.logger.Printf("Config watcher: failed to read config after change: %v", err)
		return
	}

	s.cachedConfig.Store(cfg)

	if err := s.cronSyncer.SyncCronsToLaunchd(cfg.Crons, s.logger); err != nil {
		s.logger.Printf("Config watcher: failed to sync crons: %v", err)
	}
}
```

Update the debounce callback at line 88: `s.syncCronsAfterConfigChange()` → `s.reloadConfig()`

**Step 5: Run tests**

Run: `go test ./internal/server/ -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/server/server.go internal/server/config_watcher.go
git commit -m "Add cached AgencConfig with fsnotify-driven refresh"
```


Task 5: Remove Wrapper Window-Renaming
----------------------------------------

**Files:**
- Modify: `internal/wrapper/tmux.go:22-52,152-171` (delete functions)
- Modify: `internal/wrapper/wrapper.go:48,105-108,124,254`
- Modify: `cmd/mission_resume.go:181-186`
- Modify: `cmd/mission_helpers.go:188-199` (delete function)
- Modify: `internal/server/missions.go:343-354` (delete method)

**Step 1: Delete wrapper window-renaming functions**

In `internal/wrapper/tmux.go`, delete these three functions entirely:
- `renameWindowForTmux` (lines 22-52)
- `extractRepoName` (lines 152-160)
- `applyWindowTitle` (lines 162-171)

Keep everything else in the file (isSolePaneInWindow, registerTmuxPane, clearTmuxPane, setWindowBusy, setWindowNeedsAttention, resetWindowTabStyle, setWindowTabColors, resolveWindowID).

**Step 2: Remove windowTitle from Wrapper struct**

In `internal/wrapper/wrapper.go`:
- Line 48: Delete `windowTitle    string`
- Lines 105-107: Remove from NewWrapper doc comment — delete the `windowTitle` parameter description
- Line 108: Remove `windowTitle string` from NewWrapper signature: `func NewWrapper(agencDirpath string, missionID string, gitRepoName string, initialPrompt string) *Wrapper`
- Line 124: Delete `windowTitle:                    windowTitle,`

**Step 3: Remove call to renameWindowForTmux**

In `internal/wrapper/wrapper.go` lines 252-254, delete:
```go
// Rename the tmux window to "<short_id> <repo-name>" when inside the
// AgenC tmux session.
w.renameWindowForTmux()
```

**Step 4: Update mission_resume.go**

In `cmd/mission_resume.go`, replace lines 181-186:
```go
windowTitle := lookupWindowTitle(agencDirpath, missionRecord.GitRepo)
if config.IsMissionAdjutant(agencDirpath, missionID) {
    windowTitle = "🤖  Adjutant"
}

w := wrapper.NewWrapper(agencDirpath, missionID, missionRecord.GitRepo, windowTitle, initialPrompt)
```

with:
```go
w := wrapper.NewWrapper(agencDirpath, missionID, missionRecord.GitRepo, initialPrompt)
```

Remove unused imports (`config` if no longer needed — check other usages first).

**Step 5: Delete lookupWindowTitle from mission_helpers.go**

Delete the entire `lookupWindowTitle` function (lines 188-199).

**Step 6: Delete lookupWindowTitle from server/missions.go**

Delete `s.lookupWindowTitle` method (lines 343-354).

**Step 7: Build to verify no compile errors**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: All tests pass, no compile errors

**Step 8: Commit**

```bash
git add internal/wrapper/tmux.go internal/wrapper/wrapper.go cmd/mission_resume.go cmd/mission_helpers.go internal/server/missions.go
git commit -m "Remove wrapper window-renaming — server reconciliation is now sole owner"
```


Task 6: Emoji Prepending in Server-Side Tmux Reconciliation
-------------------------------------------------------------

**Files:**
- Modify: `internal/server/tmux.go:1-9,83-107`
- Modify: `internal/server/session_scanner_test.go` (add new test)

**Step 1: Add imports**

In `internal/server/tmux.go`, add to imports:
```go
"github.com/mattn/go-runewidth"
"github.com/odyssey/agenc/internal/config"
```

**Step 2: Add resolveEmojiForMission helper**

Add after `determineBestTitle`:

```go
// resolveEmojiForMission returns the emoji to prepend to the tmux window title
// for a mission. Returns empty string if no emoji applies.
//
// Priority:
//  1. Adjutant missions → 🤖
//  2. Repo with configured emoji → that emoji
//  3. Blank missions (no repo, not adjutant) → 🦀
//  4. Repo without configured emoji → "" (no prefix)
func resolveEmojiForMission(agencDirpath string, mission *database.Mission, cfg *config.AgencConfig) string {
	if config.IsMissionAdjutant(agencDirpath, mission.ID) {
		return "🤖"
	}
	if mission.GitRepo != "" {
		return cfg.GetRepoEmoji(mission.GitRepo)
	}
	// Blank mission (no repo, not adjutant)
	return "🦀"
}
```

**Step 3: Add prependEmoji helper**

```go
// prependEmoji prepends an emoji with fixed-column-4 padding to a title.
// The title text always starts at column 4 (minimum 1 space after emoji).
// Returns the original title unchanged if emoji is empty.
func prependEmoji(emoji string, title string) string {
	if emoji == "" {
		return title
	}
	emojiWidth := runewidth.StringWidth(emoji)
	padding := 4 - emojiWidth
	if padding < 1 {
		padding = 1
	}
	return emoji + strings.Repeat(" ", padding) + title
}
```

**Step 4: Update applyTmuxTitle to prepend emoji**

Replace `applyTmuxTitle` (lines 83-107) with:

```go
// applyTmuxTitle applies a title to the tmux window for a mission, subject to
// a sole-pane guard. Prepends the mission's emoji (if any) before truncation.
func (s *Server) applyTmuxTitle(mission *database.Mission, title string) {
	// No tmux pane registered -- mission is not running in tmux
	if mission.TmuxPane == nil || *mission.TmuxPane == "" {
		s.logger.Printf("Tmux reconcile [%s]: skipping — no tmux pane registered", mission.ShortID)
		return
	}

	// Database stores pane IDs without the "%" prefix (e.g. "3043"), but tmux
	// commands require it (e.g. "%3043") to identify panes.
	paneID := "%" + *mission.TmuxPane

	// Guard: only rename if this pane is the sole pane in its window
	if !isSolePaneInTmuxWindow(paneID) {
		s.logger.Printf("Tmux reconcile [%s]: skipping — pane %s is not the sole pane in its window", mission.ShortID, paneID)
		return
	}

	// Prepend emoji if configured
	emoji := resolveEmojiForMission(s.agencDirpath, mission, s.getConfig())
	fullTitle := prependEmoji(emoji, title)

	truncatedTitle := truncateTitle(fullTitle, maxTmuxWindowTitleLen)

	if err := exec.Command("tmux", "rename-window", "-t", paneID, truncatedTitle).Run(); err != nil {
		s.logger.Printf("Tmux reconcile [%s]: tmux rename-window failed for pane %s: %v", mission.ShortID, paneID, err)
	}
}
```

**Step 5: Write tests for prependEmoji**

Add to `internal/server/session_scanner_test.go`:

```go
func TestPrependEmoji(t *testing.T) {
	tests := []struct {
		emoji string
		title string
		want  string
	}{
		{"", "my-title", "my-title"},                    // no emoji → unchanged
		{"🔥", "my-title", "🔥  my-title"},              // width-2 emoji → 2 spaces
		{"🇺🇸", "my-title", "🇺🇸  my-title"},          // flag emoji (width 2) → 2 spaces
		{"A", "my-title", "A   my-title"},               // width-1 → 3 spaces
		{"🤖", "my-title", "🤖  my-title"},              // standard emoji
	}

	for _, tt := range tests {
		got := prependEmoji(tt.emoji, tt.title)
		if got != tt.want {
			t.Errorf("prependEmoji(%q, %q) = %q, want %q", tt.emoji, tt.title, got, tt.want)
		}
	}
}
```

**Step 6: Run tests**

Run: `go test ./internal/server/ -run TestPrependEmoji -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/server/tmux.go internal/server/session_scanner_test.go
git commit -m "Prepend emoji to tmux window titles in server reconciliation"
```


Task 7: fzf Picker — Use Repo Emoji
-------------------------------------

**Files:**
- Modify: `cmd/mission_new.go:313-323`

**Step 1: Update selectFromRepoLibrary**

The function needs access to the config to look up emojis. Read the config at the top of the function, then use per-repo emoji in the loop.

Replace lines 321-323:
```go
for _, entry := range entries {
    rows = append(rows, []string{"📦", displayGitRepo(entry.RepoName)})
}
```

with:
```go
cfg, _, _ := config.ReadAgencConfig(agencDirpath)
for _, entry := range entries {
    icon := "📦"
    if cfg != nil {
        if e := cfg.GetRepoEmoji(entry.RepoName); e != "" {
            icon = e
        }
    }
    rows = append(rows, []string{icon, displayGitRepo(entry.RepoName)})
}
```

This requires the `config` import and access to `agencDirpath`. The `agencDirpath` variable is package-level in `cmd/` — verify this is accessible within `selectFromRepoLibrary`. If not, add it as a parameter.

**Step 2: Build to verify**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 3: Commit**

```bash
git add cmd/mission_new.go
git commit -m "Use repo emoji in mission new fzf picker"
```


Task 8: Update Documentation
------------------------------

**Files:**
- Modify: `docs/configuration.md:19-22,85-107`
- Modify: `docs/system-architecture.md` (lines referencing windowTitle)

**Step 1: Update docs/configuration.md**

Replace all `windowTitle` references with `emoji`:

Line 21: `windowTitle: "my-repo"` → `emoji: "🔥"`
Line 87: Update the windowTitle description to describe emoji instead
Lines 94-96: Update the example config
Line 105: `--window-title="my-repo"` → `--emoji="🔥"`

**Step 2: Update docs/system-architecture.md**

Line 344: Update the `agenc_config.go` description — replace "windowTitle" reference with "emoji"
Line 392: Update tmux reconciliation description to mention emoji prepending
Line 424: Update wrapper `tmux.go` description — remove reference to `windowTitle` and startup window naming. Note that only color management remains in the wrapper.
Line 490: Remove "updates tmux window title" from the Stop event description if applicable

**Step 3: Regenerate CLI docs**

Run: `make docs`

This will regenerate `docs/cli/` from the updated Go source (flag names, help text).

**Step 4: Build to verify everything compiles and tests pass**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 5: Commit**

```bash
git add docs/ internal/claudeconfig/prime_content.md
git commit -m "Update docs for windowTitle → emoji rename"
```


Task 9: Final Verification
----------------------------

**Step 1: Full build**

Run: `make build` (with `dangerouslyDisableSandbox: true`)
Expected: Build succeeds, all tests pass, binary is produced

**Step 2: Verify config round-trip**

Create a test config with emoji field and verify it reads/writes correctly:

Run: `go test ./internal/config/ -v`
Expected: All tests pass

**Step 3: Push all changes**

```bash
git pull --rebase
git push
```
