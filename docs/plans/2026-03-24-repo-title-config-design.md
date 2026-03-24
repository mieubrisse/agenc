Repo Title Config for Palette Entries
======================================

Status: Approved
Date: 2026-03-24

Problem
-------

Palette "Open repo" entries show `owner/repo` for every repo. Users want a friendly display name (e.g. "Dotfiles" instead of "mieubrisse/dotfiles") and want repos they care about most sorted to the top.

Design
------

### Config

Add `Title string` field to `RepoConfig` (`yaml:"title,omitempty"`). Add `GetRepoTitle(repoName) string` accessor following the `GetRepoEmoji` pattern.

### CLI

Add `--title` flag to `repo add` and `config repo-config set`, following the same pattern as `--emoji`.

### Palette Sorting

Repo entries in the palette are sorted into four tiers based on how much config the user has set:

1. Title + emoji both set (highest priority — user cares most)
2. Title set, no emoji
3. Emoji set, no title
4. Neither set (lowest priority)

Within each tier, entries are sorted alphabetically by the display name (title if set, otherwise `owner/repo`).

### Palette Display

When title is set: `"{emoji}  Open {title}"` (e.g. `"🔧  Open Dotfiles"`)
When title is not set: `"{emoji}  Open {owner/repo}"` (e.g. `"📦  Open mieubrisse/agenc"`)

### Touch Points

- `internal/config/agenc_config.go` — `RepoConfig` struct, `GetRepoTitle` accessor
- `internal/config/agenc_config_test.go` — test for `GetRepoTitle`
- `cmd/command_str_consts.go` — `repoConfigTitleFlagName` constant
- `cmd/repo_add.go` — `--title` flag
- `cmd/config_repo_config_set.go` — `--title` flag
- `cmd/tmux_palette.go` — title-aware display and tier sorting in `buildPaletteEntries`

### No Changes To

Server API, database, keybinding generation, mission creation, `repo ls` output.
