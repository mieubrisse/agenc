Palette "Open Repo" Entries
===========================

Status: Approved
Date: 2026-03-24

Problem
-------

Users must go through "New Mission" → fzf repo picker to open a registered repo. The command palette should offer direct "Open owner/repo" entries for each registered repo, removing a navigation step.

Design
------

### Approach

Inject dynamic repo entries into the command palette at display time, inside `buildPaletteEntries()` in `cmd/tmux_palette.go`. No changes to config resolution, keybinding generation, or server code.

### Components

**Single-file change:** `cmd/tmux_palette.go`

After the existing loop that filters resolved palette commands, `buildPaletteEntries` will:

1. Call `listRepoLibrary(agencDirpath)` (already exists in `cmd/mission_new.go`, same package)
2. Use `cfg.GetRepoEmoji()` for per-repo emoji (default `📦`)
3. For each repo, construct a `ResolvedPaletteCommand`:
   - **Title:** `"{emoji} Open {owner/repo}"` — plain text, no ANSI (formatting added by `formatPaletteEntryLine`)
   - **Description:** empty
   - **Command:** `agenc mission new {canonical-name}`
   - **No keybinding**
4. Append repo entries after all command entries (repos always at bottom of palette)

**New helper:** `plainGitRepoName(canonicalName string) string` — strips `github.com/` prefix, returns plain text. Needed because `displayGitRepo` adds ANSI codes.

### Data Flow

1. User opens palette → `runTmuxPalette()` → `buildPaletteEntries()`
2. Config read + `listRepoLibrary()` scan repos dir (3-level `ReadDir`, sub-millisecond)
3. Repo entries appended after command entries → fzf displays repos at bottom
4. User selects repo entry → existing dispatch runs `agenc mission new github.com/owner/repo` via `tmux run-shell -b`

### Error Handling

- `listRepoLibrary` silently returns empty slice on missing/unreadable repos dir — no repo entries, clean degradation
- Config read failure already handled by existing error return — no new error paths

### Edge Cases

- Zero repos → no repo entries, palette shows only commands
- No configured emoji → falls back to `📦`
- Repo added/removed between palette opens → automatically reflected (scan at display time)

### Testing

Manual verification: open palette, confirm repo entries at bottom with correct emoji/name, select one, confirm mission launches. No unit test added — `buildPaletteEntries` is untested and its components (`listRepoLibrary`, `GetRepoEmoji`, `GetResolvedPaletteCommands`) are individually tested.
