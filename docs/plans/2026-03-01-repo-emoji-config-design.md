Repo Emoji Config Design
========================

Goal
----

Replace the `windowTitle` per-repo config field with `emoji`. The emoji serves three purposes:

1. **Tmux window titles** — prepended before the title text (with fixed-column padding) so users can identify repos at a glance via peripheral vision
2. **`mission new` fzf picker** — replaces the generic 📦 icon with the repo's configured emoji
3. **`repo ls` output** — new EMOJI column as the first column

Adjutant missions get a hardcoded 🤖 emoji. Blank missions (Quick Claude) get a hardcoded 🦀 emoji. These follow the same title logic as all other missions — they're not special-cased for title content, only for their emoji.


Design Decisions
----------------

- **Drop `windowTitle` entirely** — no migration. The old field was free-text; the new field is an emoji. Users who had `windowTitle` set will see it silently ignored by the YAML parser.
- **Emoji is prepended at apply time** — the `applyTmuxTitle` function in the server is the single injection point. Title computation (`determineBestTitle`) stays pure with no config dependency.
- **Fixed-column-4 padding** — the title text always starts at column 4. Standard emoji (width 2) get 2 trailing spaces. Wider emoji get fewer spaces (minimum 1). Uses `runewidth.StringWidth` from the existing `go-runewidth` dependency.
- **Cached AgencConfig via fsnotify** — the server already watches `config.yml`. Extend the watcher to maintain an in-memory `atomic.Pointer[config.AgencConfig]` that updates on file change. All emoji lookups (and eventually all server-side config reads) use this cache. Zero disk I/O per reconciliation cycle.
- **Wrapper window-renaming removed** — only the server-side reconciliation loop names windows. The wrapper's `renameWindowForTmux`, `extractRepoName`, `applyWindowTitle`, and `windowTitle` field are deleted.
- **`mission ls` / resume picker unchanged** — only `repo ls` and window titles change. Mission display keeps current behavior (option C from brainstorming).


Changes by Component
--------------------

### 1. Cached AgencConfig (`internal/server/`)

- Add `cachedConfig atomic.Pointer[config.AgencConfig]` to Server struct
- On startup (`server.go`), parse config once and store via `.Store()`
- In `config_watcher.go`, extend the agenc config debounce callback to re-parse and `.Store()` the new config alongside the existing `syncCronsAfterConfigChange()`
- Add `Server.getConfig() *config.AgencConfig` that returns `.Load()` — lock-free reads
- For this task, only the emoji lookup uses the cache. Migrating other `ReadAgencConfig` calls is a follow-up

### 2. Config Schema (`internal/config/agenc_config.go`)

- In `RepoConfig` struct: remove `WindowTitle string`, add `Emoji string` (`yaml:"emoji,omitempty"`)
- Rename `GetWindowTitle(repoName) string` → `GetRepoEmoji(repoName) string`
- Update `agenc_config_test.go` if any tests reference `WindowTitle`

### 3. CLI Flags and Commands (`cmd/`)

- `command_str_consts.go`: rename `repoConfigWindowTitleFlagName` → `repoConfigEmojiFlagName`, value `"emoji"`
- `config_repo_config_set.go`: rename `--window-title` flag to `--emoji`, update handler to set `rc.Emoji`
- `config_repo_config_ls.go`: replace "WINDOW TITLE" column header with "EMOJI", display `rc.Emoji`
- `repo_add.go`: rename `--window-title` flag to `--emoji`, update handler
- `repo_ls.go`: add "EMOJI" as first column, show configured emoji or `--`

### 4. Remove Wrapper Window-Renaming (`internal/wrapper/`, `cmd/`)

- `internal/wrapper/tmux.go`: delete `renameWindowForTmux()`, `extractRepoName()`, `applyWindowTitle()`
- `internal/wrapper/wrapper.go`: remove `windowTitle` field, remove `windowTitle` param from `NewWrapper()`
- `cmd/mission_helpers.go`: delete `lookupWindowTitle()` function
- `cmd/mission_resume.go`: remove `windowTitle` variable, remove Adjutant special case, update `NewWrapper()` call
- `internal/server/missions.go`: delete `s.lookupWindowTitle()` method

### 5. Emoji in Server-Side Tmux Reconciliation (`internal/server/tmux.go`)

- `applyTmuxTitle` resolves the emoji for the mission:
  - Adjutant (via `config.IsMissionAdjutant`) → 🤖
  - Repo with configured emoji → that emoji
  - Blank mission (no repo, not adjutant) → 🦀
  - Repo without configured emoji → no prefix
- If emoji is non-empty, prepend with column-4 padding:
  ```
  emojiWidth := runewidth.StringWidth(emoji)
  padding := max(1, 4 - emojiWidth)
  title = emoji + strings.Repeat(" ", padding) + title
  ```
- `applyTmuxTitle` needs access to: the Server (for `s.getConfig()` and `s.agencDirpath`), and the mission record
- The `truncateTitle` function applies after emoji prepending — emoji + padding counts toward the 30-char limit

### 6. fzf Picker (`cmd/mission_new.go`)

- In `selectFromRepoLibrary`: for each repo entry, look up the emoji from config. If set, use it; otherwise fall back to 📦
- 🤖 (Adjutant), 🐙 (Github Repo), 😶 (Blank Mission) remain hardcoded


Files Modified
--------------

| File | Change |
|------|--------|
| `internal/config/agenc_config.go` | `WindowTitle` → `Emoji` in `RepoConfig`, rename accessor |
| `internal/config/agenc_config_test.go` | Update tests for renamed field |
| `internal/server/server.go` | Add `cachedConfig` field, populate on startup |
| `internal/server/config_watcher.go` | Update cached config on fsnotify change |
| `internal/server/tmux.go` | Emoji prepending in `applyTmuxTitle` |
| `internal/server/missions.go` | Delete `lookupWindowTitle` |
| `internal/wrapper/tmux.go` | Delete `renameWindowForTmux`, `extractRepoName`, `applyWindowTitle` |
| `internal/wrapper/wrapper.go` | Remove `windowTitle` field and param |
| `cmd/mission_new.go` | Use repo emoji in fzf picker |
| `cmd/mission_resume.go` | Remove windowTitle/Adjutant special case, update `NewWrapper()` |
| `cmd/mission_helpers.go` | Delete `lookupWindowTitle` |
| `cmd/command_str_consts.go` | Rename flag constant |
| `cmd/config_repo_config_set.go` | `--window-title` → `--emoji` |
| `cmd/config_repo_config_ls.go` | "WINDOW TITLE" → "EMOJI" column |
| `cmd/repo_add.go` | `--window-title` → `--emoji` |
| `cmd/repo_ls.go` | Add EMOJI first column |
| `docs/configuration.md` | Update config reference |
| `docs/system-architecture.md` | Update architecture reference |
| CLI docs (`docs/cli/`) | Update generated docs |
