Mission Picker Sorting Design
==============================

Problem
-------

The mission attach picker sorts by `COALESCE(last_heartbeat, created_at) DESC`.
Since heartbeats fire every 10 seconds, all running missions cluster at the top
in effectively random order. The missions the user is actively iterating on are
indistinguishable from idle ones, and missions needing user attention (permission
prompts) don't surface.

Approach
--------

Replace the single-column sort with a three-tier sort that surfaces the most
actionable missions first:

1. **Needs attention** — missions where Claude is blocked waiting on the user
   (permission prompt, elicitation dialog) float to the top
2. **Most recently interacted** — sorted by the timestamp of the user's last
   prompt submission, so missions the user is actively working with appear next
3. **Fallback** — `COALESCE(last_heartbeat, created_at)` for missions with no
   prompt history or no running wrapper

Claude state (`needs_attention`) is queried from running wrappers at picker
time, not persisted to the database. If this proves too slow, a future pass can
cache it in a DB column.

Additionally, `idle_prompt` is removed from the `needsAttention` group in the
wrapper. Only `permission_prompt` and `elicitation_dialog` trigger the orange
window and the `needs_attention` state.

Components
----------

### 1. Database: `last_user_prompt_at` column

New nullable TEXT column on the missions table, same format as `last_heartbeat`.
Updated in two places:

- **RecordPrompt** (immediate) — set when `UserPromptSubmit` fires, for
  responsiveness
- **Heartbeat** (periodic) — the wrapper includes `last_user_prompt_at` in every
  heartbeat payload as a consistency backstop after server restarts

The heartbeat handler only writes `last_user_prompt_at` when the wrapper sends a
non-empty value. A freshly resumed mission with no prompts yet sends an empty
value, which does not clobber the previous timestamp from the prior session.

### 2. Wrapper: track and report `lastUserPromptAt`

The wrapper gains a `lastUserPromptAt` field (protected by `stateMu`). Set to
`time.Now().UTC()` on each `UserPromptSubmit` event. Included in every heartbeat
payload:

```json
{"pane_id": "42", "last_user_prompt_at": "2026-03-10T15:04:05Z"}
```

When `lastUserPromptAt` is zero (no prompts yet this session), the field is
omitted or sent as empty string.

### 3. Wrapper: remove `idle_prompt` from attention group

The `handleClaudeUpdate` Notification switch changes from:

```go
case "permission_prompt", "idle_prompt", "elicitation_dialog":
```

to:

```go
case "permission_prompt", "elicitation_dialog":
```

`idle_prompt` no longer sets `needsAttention = true` or colors the window.

### 4. Mission picker: client-side three-tier sort

The `buildMissionPickerEntries` call site already receives missions enriched with
`ClaudeState` from the server API. A new sort function orders them:

1. Missions with `claude_state == "needs_attention"` first
2. Within each group, by `last_user_prompt_at` DESC (nulls last)
3. Ties broken by `COALESCE(last_heartbeat, created_at)` DESC

The SQL `ORDER BY` in `buildListMissionsQuery` remains as a reasonable default
for non-picker consumers, but the picker applies its own sort after enrichment.

Data Flow
---------

### Prompt submission

```
User submits prompt
  -> Claude Code fires UserPromptSubmit hook
  -> agenc mission send-claude-update <id> UserPromptSubmit
  -> Wrapper sets lastUserPromptAt = now
  -> Wrapper calls client.RecordPrompt(missionID)
  -> Server sets last_user_prompt_at in DB (immediate)
```

### Heartbeat (every 10s)

```
Wrapper sends POST /missions/{id}/heartbeat
  body: {"pane_id": "42", "last_user_prompt_at": "2026-03-10T15:04:05Z"}
  -> Server updates last_heartbeat (always)
  -> Server updates last_user_prompt_at (only if non-empty)
```

### Mission attach picker

```
User runs "mission attach"
  -> CLI calls server ListMissions API
  -> Server fetches missions from DB (includes last_user_prompt_at)
  -> Server enriches each running mission with claude_state via wrapper /status
  -> CLI receives enriched list
  -> Go sort function applies 3-tier ordering
  -> fzf picker displays sorted list
```

Error Handling
--------------

- **Freshly resumed mission (no prompts yet):** Wrapper has zero-value
  `lastUserPromptAt`. Heartbeat sends empty `last_user_prompt_at`. Server skips
  the update. DB retains the value from the previous session.
- **Server restart:** DB retains `last_user_prompt_at`. Next heartbeat from each
  running wrapper re-confirms the value from wrapper memory.
- **Wrapper crash / stopped mission:** No wrapper to query. `claude_state` is
  nil. Mission does not appear in tier 1. Sorts by `last_user_prompt_at` from DB
  or falls through to tier 3.
- **Multiple missions with `needs_attention`:** All float to tier 1, sorted
  among themselves by `last_user_prompt_at` then heartbeat fallback.
- **Wrapper /status query fails or times out:** Mission gets nil `claude_state`.
  Same behavior as stopped mission — sorts by tier 2/3.

Testing
-------

- **DB migration test:** Verify `last_user_prompt_at` column is added
  idempotently.
- **RecordPrompt test:** Verify `last_user_prompt_at` is set when
  `RecordPrompt` is called.
- **Heartbeat test:** Verify `last_user_prompt_at` is updated when non-empty,
  and preserved when empty.
- **Sort function test:** Verify three-tier ordering with combinations of
  `claude_state` values, `last_user_prompt_at` timestamps, and heartbeat
  timestamps. Cover: needs_attention vs. not, null vs. present prompt
  timestamps, null vs. present heartbeats.
- **Wrapper integration test:** Verify `lastUserPromptAt` is included in
  heartbeat payload after a `UserPromptSubmit` event.
