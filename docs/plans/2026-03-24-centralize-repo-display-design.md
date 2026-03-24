Centralize Repo Display with Emoji
====================================

Problem
-------

Repo display logic is scattered across 5+ surfaces, each doing its own ad-hoc
version of "get emoji, get title, fall back to canonical name." Some surfaces
show emoji, some don't, and the pattern is inconsistent.

Design
------

### New function

Add `formatRepoDisplay(repoName string, isAdjutant bool, cfg *config.AgencConfig) string`
to `cmd/mission_ls.go` (where `displayGitRepo` already lives).

Logic:

1. If `isAdjutant` -> return `"🤖  Adjutant"`
2. Resolve display name: `cfg.GetRepoTitle(repoName)` if non-empty, else `displayGitRepo(repoName)`
3. Resolve emoji: `cfg.GetRepoEmoji(repoName)` if non-empty
4. If emoji exists, prepend with fixed-width spacing (same column-4 pattern as `prependEmoji` in tmux.go)
5. If `cfg` is nil, fall back to `displayGitRepo(repoName)` with no emoji

### Surfaces updated

| Surface | File | Change |
|---------|------|--------|
| `mission ls` | cmd/mission_ls.go | Replace inline title lookup with `formatRepoDisplay` |
| `mission attach` picker | cmd/mission_helpers.go | Replace inline title lookup with `formatRepoDisplay` |
| `mission inspect` | cmd/mission_inspect.go | Title line uses `formatRepoDisplay`; Git repo line stays as-is |
| `repo rm` picker | cmd/repo_rm.go | Replace `FormatRow` to use `formatRepoDisplay` (gains emoji) |
| `mission new` picker | cmd/mission_new.go | Collapse TYPE + REPO columns into single REPO column |

### Surfaces NOT affected

| Surface | Reason |
|---------|--------|
| `repo ls` | Separate columns — showing config values |
| `tmux_palette` | Custom format with "Open" prefix |
| `tmux.go` window title | Different priority chain (custom title > auto summary > repo title) |

### Edge cases

- Empty repoName + not adjutant -> `displayGitRepo("")` returns `"--"`, no emoji
- Emoji configured but no title -> emoji + colored canonical name
- Title configured but no emoji -> just the title string
- Nil config -> pure fallback to `displayGitRepo`, no emoji

### Error handling

None beyond what exists. Config read failures already handled by callers (they
pass nil cfg). `formatRepoDisplay` treats nil cfg as "no title, no emoji."
