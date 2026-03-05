Send Keys Feature Design
========================

Date: 2026-03-05

Overview
--------

Add a `send-keys` subcommand to `agenc mission` that forwards keystrokes to a
running mission's tmux pane. This enables humans and agents to interact with
missions programmatically — sending text prompts, control sequences (`C-c`,
`Enter`, `Escape`), or piping content via stdin.

Architecture
------------

Follows the existing CLI -> Server -> tmux pattern (same as attach/detach).

```
CLI                              Server                           tmux
 |                                 |                                |
 |-- POST /missions/{id}/send-keys -->                              |
 |   body: { "keys": [...] }      |                                |
 |                                 |-- tmux send-keys -t %<pane> keys... -->
 |                                 |<-- exit code ------------------|
 |<-- 200 OK / error --------------|                                |
```

The mission must already be running (have a live tmux pane). No auto-start —
send-keys is a "talk to what's already there" operation.

Components
----------

### CLI command: `cmd/mission_send_keys.go`

- Registered as `agenc mission send-keys <mission-id> [keys...]`
- Positional args after mission ID are the keys to send
- Stdin support: if stdin is not a TTY and has data, reads it as a single text
  entry prepended to any positional key args
- Does NOT require being inside a tmux session
- Calls `client.SendKeys(missionID, keys)`

### Server endpoint: `POST /missions/{id}/send-keys`

- Handler in `internal/server/missions.go`
- Request body: `SendKeysRequest { Keys []string }`
- Resolves mission ID -> mission record -> pane ID
- Validates: mission exists, not archived, has a live tmux pane
- Calls `sendKeysToPane(paneID, keys)`

### Pool helper: `sendKeysToPane()` in `internal/server/pool.go`

- Executes `exec.Command("tmux", "send-keys", "-t", "%<paneID>", keys...)`
- Keys passed as separate args to exec.Command — no shell interpolation
- Returns tmux exit code/error verbatim

### Client method: `SendKeys()` in `internal/server/client.go`

- `func (c *Client) SendKeys(missionID string, keys []string) error`
- POSTs to `/missions/{id}/send-keys`

Data Flow
---------

### CLI args

```
agenc mission send-keys abc123 "hello world" Enter
```

1. Cobra parses: `missionID = "abc123"`, `args = ["hello world", "Enter"]`
2. CLI calls `client.SendKeys("abc123", ["hello world", "Enter"])`
3. Server resolves abc123 -> full UUID -> pane ID
4. Server executes: `exec.Command("tmux", "send-keys", "-t", "%3043", "hello world", "Enter")`
5. Returns `200 {"status": "ok"}`

### Stdin pipe

```
echo "fix the auth bug" | agenc mission send-keys abc123
```

1. CLI detects stdin is not a TTY
2. Reads stdin, trims trailing newline
3. Sends `keys = ["fix the auth bug"]`

### Combined (stdin + trailing args)

```
echo "fix the auth bug" | agenc mission send-keys abc123 Enter
```

1. Reads stdin -> `"fix the auth bug"`
2. Positional args -> `["Enter"]`
3. Sends `keys = ["fix the auth bug", "Enter"]`
4. Naturally supports piping text and pressing Enter to submit

Passthrough Semantics
---------------------

Keys are forwarded to `tmux send-keys` verbatim. No custom key syntax — tmux's
own key names (`Enter`, `C-c`, `Escape`, etc.) work directly. Users who know
tmux get exactly what they expect.

Note: tmux's `Enter` key name likely sends `\r` (carriage return), while a
literal newline in piped text sends `\n` (line feed). Most terminal applications
treat them equivalently, but verify during implementation.

Error Handling
--------------

| Condition                          | HTTP | Message                                                                        |
|------------------------------------|------|--------------------------------------------------------------------------------|
| Mission ID not found               | 404  | Mission not found: abc123                                                      |
| Mission is archived                | 400  | Cannot send keys to archived mission abc123. Unarchive it with: agenc mission unarchive abc123 |
| Mission not running (no pane)      | 400  | Mission abc123 is not running. Start it with: agenc mission attach abc123      |
| Pane in DB but dead in tmux        | 500  | Mission abc123 has a stale pane reference. Try: agenc mission reload abc123    |
| tmux send-keys fails               | 500  | tmux send-keys failed: <tmux stderr>                                           |
| No keys provided (TTY, no args)    | CLI  | Usage message with examples                                                    |
| Empty stdin pipe                   | CLI  | Same as no keys — treat empty pipe as no input                                 |

Every error includes the command to fix it (self-serve resolution).

Stale pane detection: before calling tmux send-keys, server checks
`poolWindowExistsByPane(paneID)`.

Testing
-------

### Unit tests

- `cmd/mission_send_keys_test.go`: stdin detection, argument parsing,
  stdin+args combination
- `internal/server/missions_test.go`: handler validation — missing keys,
  archived mission, missing pane

### Manual integration tests

- Literal text, `Enter`, `C-c`, `Escape`, multi-word strings
- Stdin piping: `echo "text" | ./agenc mission send-keys <id>`
- Combined: `echo "text" | ./agenc mission send-keys <id> Enter`
- Newline vs Enter behavior verification
- Error messages for each failure case

No database migrations required — reads existing fields only.
