Attach Picker — tmux "attached" indicator
==========================================

Provenance
----------

Designed in AgenC mission `7d584435-fb76-437c-93b3-3829a72de40f`, session `88c57726`.
Run `agenc session print 88c57726 --all` for the full design discussion.

Related cleanup deferred to bead **agenc-8vj3** (P3): separating
`database.Mission` persisted columns from server-computed transient fields.


Problem
-------

The `agenc mission attach` interactive picker lists missions but gives no
signal about which ones are *currently attached* — i.e. linked into a tmux
session outside the pool. The user wants a quick visual scan of attached vs.
unattached missions without reading any extra detail.


Goal
----

Add a narrow column to the attach picker that shows a green dot (`●`) when a
mission is currently linked into a tmux session other than the pool session,
and is blank otherwise. Minimal visual noise — the dot only signals
attached/not-attached, nothing more.


Non-goals
---------

- The dot is **not** added to `mission ls` or to the other pickers
  (stop / rm / print / detach). Attach picker only.
- The dot does **not** show *which* session(s) a mission is linked into — just
  whether it is attached at all. (`getLinkedPaneSessions` could supply names
  later if desired; out of scope here.)
- No broader refactor of `database.Mission` — that is deferred to agenc-8vj3.


Sources of truth
----------------

The attached-state is derived by joining two independent sources of truth, each
queried server-side:

- **tmux** is the source of truth for "is pane X linked into a non-pool
  session." Queried live via the existing `getLinkedPaneIDs(poolName)` helper
  (`internal/server/pool.go`), which runs a single `tmux list-panes -a` and
  returns the set of pane IDs visible in any session besides the pool.
- **The database** is the source of truth for "which pane belongs to which
  mission" (`tmux_pane` column). tmux cannot answer this reliably — window
  names get renamed by title reconciliation, which is why the codebase keys off
  immutable pane IDs stored in the DB.

A mission is **attached** iff `mission.TmuxPane != nil` and that pane ID is in
the live linked-pane set. The value is computed fresh on every request and is
never persisted.

The CLI never queries tmux. All tmux access stays on the server, consistent
with the thin-CLI / thick-server rule in `agent/CLAUDE.md` and
`docs/system-architecture.md`.


Design
------

### Server

Add a transient `IsAttached` bool to the mission response types, populated from
the linked-pane set:

- `MissionResponse` (`internal/server/missions.go`) gains `is_attached`.
- `SearchMissionsResponse` (`internal/server/search.go`) gains `is_attached`.
- `database.Mission` gains a transient `IsAttached` field, carried through
  `ToMission()` / `toMissionResponse()` exactly like the existing transient
  fields (`ResolvedSessionTitle`, `IsAdjutant`, `ClaudeState`). This rides the
  existing transient-field rail rather than introducing a parallel mechanism
  for one field; the rail itself is flagged for cleanup in agenc-8vj3.

Each handler computes the linked-pane set **once per request** (one tmux call,
independent of mission count) and sets the per-mission bool:

- `handleListMissions` — the recent-list and substring-merge data source.
- `handleSearchMissions` — the live full-text data source. It already loads each
  mission via `GetMission`, so the pane ID is already in hand.
- `handleGetMission` / the single-pane path — for direct-ID resolution in the
  picker.

If the tmux query fails (no server, pool missing), `getLinkedPaneIDs` returns an
empty set, so every mission reports `IsAttached = false` and the column is
simply blank. Graceful degradation, no error surfaced.

### CLI

The attach picker draws rows from two response types, and both must render the
new column **identically** or the fzf table misaligns across the
initial-render → live-search reload boundary:

- `cmd/mission_attach.go` (`runMissionSearchPicker`) — initial recent-missions
  render. Reads `IsAttached` off the `database.Mission` values returned by
  `client.ListMissions`.
- `cmd/mission_search_fzf.go` (`runMissionSearchFzf`) — live render. Reads
  `IsAttached` off both the `GetMission` result and the `SearchMissions`
  results.

Both add a column (header `●`, placed immediately after `ID`) whose cell is a
green `●` when attached and an empty string otherwise. The green is applied with
the same ANSI color helper used by `colorizeStatus`.

The shared `buildMissionPickerEntries` may carry the `IsAttached` value on
`missionPickerEntry`, but only the attach-picker call sites render the column;
the stop / rm / print / detach call sites keep their current column sets.


Edge cases
----------

- **Stopped / archived mission** — no pane (`TmuxPane == nil`) → never attached
  → blank.
- **tmux not running / pool missing** — empty linked set → all blank.
- **Mission linked into multiple sessions** — still just attached → one dot
  (the dot is binary by design).
- **Pane in the pool only** — not in the linked set (pool is excluded) → blank.
  "In the pool" is the unattached resting state, correctly not flagged.


Testing
-------

- **Unit:** server-side computation of `IsAttached` from a linked-pane set —
  attached pane → true, pool-only pane → false, nil pane → false.
- **E2E** (`scripts/e2e-test.sh`): create a mission, attach it to a session,
  and assert the picker / search-fzf output marks it attached; assert an
  unattached mission is not marked. Where the dot rendering itself can only be
  verified through a live fzf/tmux interaction, flag that slice for manual
  testing per the repo's tmux-integration testing rule.
