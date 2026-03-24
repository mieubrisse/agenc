Repo Title Display Across All Surfaces
=======================================

Status: Approved
Date: 2026-03-24

Problem
-------

The repo `title` config field is only used in the command palette. All other user-facing surfaces still show `owner/repo` even when a friendly title is configured.

Design
------

### Per-Surface Behavior

| Surface | Title set | Title not set |
|---------|-----------|---------------|
| `mission ls` REPO column | Shows title | Shows `owner/repo` |
| `mission attach` picker | Shows title | Shows `owner/repo` |
| `mission inspect` | Adds "Title:" line | No title line |
| `mission new` picker | Shows title | Shows `owner/repo` |
| `repo ls` | Adds TITLE column | Shows `--` |
| `repo rm` picker | Adds title column | Shows `--` |
| `config repo-config ls` | Adds TITLE column | Shows `--` |
| Tmux window title | Uses title as fallback | Uses repo short name |
| Repo resolution message | No change | No change |
| Command palette | Already done | Already done |

### Key Distinction

- **mission ls / mission attach:** Replace the repo display with title when set (space-constrained UIs where the user just needs to identify the repo)
- **repo ls / config repo-config ls / repo rm:** Add a separate TITLE column alongside REPO (repo-management commands where the canonical name matters)
- **mission inspect:** Show both — title as its own line when set, canonical name always shown

### Touch Points

- `cmd/mission_ls.go` — title-or-repo in REPO column
- `cmd/mission_helpers.go` — title-or-repo in attach picker
- `cmd/mission_inspect.go` — add Title line
- `cmd/mission_new.go` — title-or-repo in picker rows
- `cmd/repo_ls.go` — add TITLE column
- `cmd/repo_rm.go` — add title column to picker
- `cmd/config_repo_config_ls.go` — add TITLE column
- `internal/server/tmux.go` — use title in window title fallback

### Config Access

All surfaces already read `AgencConfig` for emoji lookups or have trivial access to it. Title lookup follows the same `cfg.GetRepoTitle(repoName)` pattern. No new I/O or error paths.
