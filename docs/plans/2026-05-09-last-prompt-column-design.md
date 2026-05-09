Last Prompt Column — Design
============================

Context
-------

The Mission Attach picker currently has no last-activity column at all (matching
bug `agenc-vhkg`). Other pickers (`mission ls`, `rm`, `stop`, etc.) display a
LAST_ACTIVE column derived from `last_heartbeat`, but heartbeats fire constantly
while the wrapper is alive — they don't track *user interaction*. The
`last_user_prompt_at` column already exists in the DB (populated by the
`UserPromptSubmit` hook → wrapper → `RecordPrompt` → `UpdateLastUserPromptAt`)
and is already used as a sort key inside `sortMissionsForPicker`, but is never
displayed.

Separately, unprompted missions disappear from the Mission Attach picker as
soon as the user types into fzf. The picker's empty-query view uses
`ListMissions` (which includes them), but typed queries route through FTS5,
which only indexes prompted conversation transcripts. This is the same code
shape as bead `agenc-re9n`.

Provenance: designed in AgenC mission `b56b93c5-3d19-4e9c-87fa-0a1993a928f5`.

Decisions
---------

1. Replace the LAST_ACTIVE column with a LAST_PROMPT column across **all**
   pickers (`mission attach`, `ls`, `rm`, `stop`, `inspect`, `archive`,
   `detach`, `print`, `reload`). Mission Attach gains the column; others
   change which timestamp they show.
2. Display: render the local-formatted `last_user_prompt_at` when non-nil;
   render `--` when nil (matching `displayGitRepo("")`).
3. Sort: `COALESCE(last_user_prompt_at, created_at) DESC` with `created_at
   DESC` as a stable secondary tiebreak. A brand-new unprompted mission
   sorts as if it were prompted at creation time, so freshly-spawned
   missions surface at the top.
4. Picker tier-1 (`needs_attention` floats to top) is preserved.
5. Fix the unprompted-mission disappearance in search-fzf by merging
   case-insensitive substring matches over `ListMissions` with FTS results,
   capped at 30 rows. This absorbs `agenc-re9n`.
6. No DB schema change. No migration. No backfill — `last_user_prompt_at`
   is already populated for active missions; only ancient pre-migration
   rows are nil and will fall back to `created_at`.

Out of scope
------------

- `last_heartbeat` remains in the DB and continues to be written by the
  wrapper. It is no longer used for picker display, but other liveness
  logic still depends on it.
- FTS indexer behavior is unchanged. The merge approach sidesteps the
  index for unprompted missions rather than re-indexing them.

Components touched
------------------

- `cmd/mission_ls.go`
  - Rename `formatLastActive(lastHeartbeat *time.Time, createdAt time.Time)`
    to `formatLastPrompt(lastUserPromptAt *time.Time, createdAt time.Time)`.
    Returns formatted timestamp when non-nil; returns `--` otherwise. The
    `createdAt` parameter is unused for display (only the sort path uses
    the COALESCE) — kept on the signature for symmetry with the sort key.
  - Update column header literal `LAST_ACTIVE` to `LAST_PROMPT` in the
    `mission ls` table.
- `cmd/mission_helpers.go`
  - Rename `missionPickerEntry.LastActive` to `LastPrompt`.
  - In `buildMissionPickerEntries`, change the timestamp source to
    `formatLastPrompt(m.LastUserPromptAt, m.CreatedAt)`.
- All picker call sites — replace `LAST_ACTIVE` header with `LAST_PROMPT`,
  use `e.LastPrompt`:
  - `cmd/mission_attach.go` — *adds* the column for the first time;
    closes `agenc-vhkg`.
  - `cmd/mission_search_fzf.go` (the `printRecentMissionsForFzf` and
    initial-input branches).
  - `cmd/mission_archive.go`, `cmd/mission_detach.go`,
    `cmd/mission_inspect.go`, `cmd/mission_print.go`, `cmd/mission_rm.go`,
    `cmd/mission_reload.go`, `cmd/mission_stop.go`.
- `cmd/mission_sort.go`
  - Tier 2 simplifies to a single COALESCE-style key: sort descending on
    `LastUserPromptAt` if non-nil, else `CreatedAt`. Tier 3 (heartbeat
    fallback) is removed.
- `internal/database/queries.go`
  - `buildListMissionsQuery` ORDER BY changes from
    `COALESCE(last_heartbeat, created_at) DESC` to
    `COALESCE(last_user_prompt_at, created_at) DESC, created_at DESC`.
- `cmd/mission_search_fzf.go` — `runMissionSearchFzf`
  - After FTS results and the existing `seenMissionIDs` de-dup, call
    `client.ListMissions(IncludeArchived: true)`. For each mission not yet
    seen, lowercase its `ResolvedSessionTitle`, `Prompt`, and `GitRepo`
    and check `strings.Contains` against the lowercased query. Append
    matches with empty MATCH column. Cap appended count at 30.

Edge cases addressed
--------------------

| Edge case | Resolution |
|-----------|------------|
| Stable sort across tied timestamps | SQL adds `, created_at DESC` secondary tiebreak; Go-side uses `sort.SliceStable`. |
| FTS unavailable | Substring-match merge still runs; user gets degraded-but-non-empty results. |
| Multiple rapid `UserPromptSubmit` events | Most-recent-wins (existing semantic from archived `feature-mission-last-active.md` spec). |
| Adjutant missions | UserPromptSubmit fires for Adjutant like any Claude — no special handling. |
| Failed-wrapper missions (`agenc-gvhe`) | Adjacent but unaffected; row appears in DB and renders with `--`. That bead remains open and out of scope. |
| Display vs. sort divergence | Intentional: a row with `--` may sort high if `created_at` is recent. Documented for the test plan. |
| LAST_PROMPT header wider than LAST_ACTIVE | tableprinter handles realignment automatically. |
| Empty `ResolvedSessionTitle`/`Prompt`/`GitRepo` in substring merge | `strings.Contains("", non-empty)` is false; no false matches. |

Testing plan
------------

Unit:
- `formatLastPrompt(nil, _)` → `"--"`
- `formatLastPrompt(&t, _)` → local-formatted time
- `sortMissionsForPicker`: brand-new unprompted mission sorts above older
  prompted mission; ties resolve by `created_at`
- `buildListMissionsQuery` ORDER BY shape
- Search-fzf merge: substring match on each of title/repo/prompt; de-dup
  against FTS; 30-row cap

E2E (`scripts/e2e-test.sh`):
- Create an unprompted mission, run `mission ls`, verify column shows `--`
  and header is `LAST_PROMPT`
- Run `mission search-fzf <substring of repo name>` against an unprompted
  mission, verify the mission appears in output

Manual (tmux integration — per CLAUDE.md):
- Open `agenc mission attach`: LAST_PROMPT column appears; prompted rows
  show timestamps; unprompted show `--`; sort matches expectation
- Type a partial repo/title/prompt fragment in the picker; unprompted
  missions still surface

Beads cleanup
-------------

- Close `agenc-vhkg` (superseded — column restored under new name)
- Close `agenc-re9n` (absorbed — substring-match merge implemented here)
- Confirm `agenc-297` is already closed
