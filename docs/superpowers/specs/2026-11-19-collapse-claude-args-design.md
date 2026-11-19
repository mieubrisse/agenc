Collapse the two per-mission Claude-args channels into one ordered list
=======================================================================

Issue: KEV-97 (https://linear.app/todayendeavors/issue/KEV-97). Decisions quoted
there are Kevin's, 2026-10-18; this doc records how they are built. Written by
an unattended burn-down worker (T3 thread
`mcp:launch:8d538ff5539bccd4787dad84163d98192c427b084694bc842578ab8be9a3bbc5`),
so "approved" below means "decided on the issue", not reviewed live.

Why
---

KEV-88 added `missions.claude_passthrough_args` (ordered argv) beside the older
`missions.claude_args` (a keyed object allowing only `--model` and `--effort`).
Two columns, two code paths and override logic between them, for one concept.
Kevin's reason for collapsing them is maintenance burden.

Decided on the issue
--------------------

- Reading 2: `--model` and `--effort` stop being `agenc mission new` flags.
  Claude flags go after `--`: `agenc mission new -- --model opus`.
- AgenC stops knowing what a model is. The `defaultModel` config keys go;
  defaults are expressed as `claudeArgs` lists.
- `--clone` keeps replace-wholesale: anything after `--` replaces the source's
  list. No merging. The clone reports which happened.
- Existing missions' `claude_args` data is migrated, not dropped.

Design
------

**One column.** `claude_passthrough_args` survives (renaming it is cosmetic and
out of scope). A migration folds each row's `claude_args` object into the front
of its list, in the order the old code emitted them (`--model`, then
`--effort`), then drops `claude_args`, inside one transaction, guarded on the
column existing. The add-column step stays in the history so a fresh database
walks the same path, as `tmux_window_title` did.

**One precedence rule.** Spawn argv is: global `claudeArgs`, then the repo's
`claudeArgs`, then the mission's list. AgenC dedupes nothing; Claude resolves a
repeated scalar flag last-wins, so later sources outrank earlier ones.
`--model` last-wins was verified live (2026-11-19).

**Legacy `defaultModel`.** The YAML loader ignores unknown keys, so deleting the
field outright would silently drop Kevin's `defaultModel: opus`. Instead a
leftover `defaultModel` (global or per-repo) fails config loading with the
exact `claudeArgs` replacement in the message. A translate-on-read shim was
rejected: it is the "special-casing that survives where Kevin cannot see it"
the issue warns about.

**Clone reporting.** `mission new --clone` prints either "Inherited Claude args:
…" (caller passed nothing) or "Replaced the source mission's Claude args: …"
naming the discarded list (caller passed args and the source had some).

Consequence worth naming
------------------------

Argv text is not byte-identical for every migrated mission. A mission stored as
`{"model":"fable"}` under `defaultModel: opus` used to emit `--model fable`
once; after the collapse the config contributes `--model opus` and the mission
`--model fable`, and Claude takes the last. Effective flags are identical.
Removing the duplicate would need an arity table, which reading 2 rules out.

Out of scope
------------

- The `populateMissionFields` scanner refactor the issue notes as a follow-up.
- Editing Kevin's live `~/.agenc/config/config.yml` (palette entries,
  `defaultModel`): the replacements go on the issue for Kevin.
