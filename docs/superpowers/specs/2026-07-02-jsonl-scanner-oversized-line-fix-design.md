JSONL Scanner: Uncap Per-Line Size for Sessions With Inline Screenshots
========================================================================

Date: 2026-07-02
Issue: https://github.com/mieubrisse/agenc/issues/12
Reporter: Yannik Zimmermann (ro0mquy)
AgenC mission: fc39356e-382d-42c0-8d52-f0cb13b09cd4

Motivation
----------

`agenc session print` and `agenc mission print` abort with `bufio.Scanner: token too long` on any transcript that contains at least one JSONL line exceeding 1 MB. This happens routinely: Claude Code stores one message per JSONL line, and any message containing an inline base64 screenshot easily crosses that threshold. The failure takes out the entire transcript, not just the oversized line, so replay for a large class of real missions is broken.

The reporter's root-cause analysis is correct: `bufio.Scanner` returns `ErrTooLong` when a single token exceeds the buffer cap, and every JSONL reader in the codebase uses `bufio.Scanner` with a 1 MB cap (10 MB in the summary command's case). The suggested fix — switching to `bufio.Reader` with `ReadBytes('\n')` — removes the per-line ceiling entirely. `bufio.Reader` grows its internal buffer as needed and has no hard cap.

Decision
--------

Add a single shared JSONL line reader in `internal/session/` that uses `bufio.Reader.ReadBytes('\n')` internally. Migrate all six existing `bufio.Scanner` call sites in `internal/session/` and `cmd/summary.go` onto that helper. Delete the ad-hoc `sync.Pool` of 1 MB buffers in `internal/session/conversation.go` — the pool exists only to feed the now-removed scanner.

The abstraction is not speculative. Six near-identical implementations of "read a Claude JSONL file line by line" exist today, and they all suffer the same bug class. Consolidating them into one call site makes the bug unrepeatable in the next reader added to the codebase.

Scope
-----

Six call sites migrate:

| File | Function | Currently caps at |
|---|---|---|
| `internal/session/format.go:97` | `collectJSONLLines` | 1 MB |
| `internal/session/session.go:168` | `findNamesInJSONL` | 1 MB |
| `internal/session/session.go:297` | `hasConversationData` | 1 MB |
| `internal/session/session.go:324` | `TailJSONLFile` | 1 MB |
| `internal/session/conversation.go:50` | `extractUserMessagesFromJSONL` | 1 MB (pooled) |
| `cmd/summary.go:275` | `analyzeJSONL` | 10 MB |

`internal/session/conversation.go` also loses its `scannerBufPool` and its `sync` import; the pool has no callers after the migration.

Out of scope
------------

- Other `bufio.Scanner` usages in the codebase that do not read Claude JSONL files (e.g., anything that reads short-line log output). They serve different file formats, and the pattern is fine there.
- Streaming assistant-message parsing. The migration preserves the read-all-lines-then-process shape of every existing call site; no changes to when parsing happens or which entries are kept.
- Reviewing whether all six call sites should still exist. They do different things (title lookup, tail, ring-buffer collect, summary); consolidating their higher-level behavior is a separate discussion.

Architecture
------------

### The shared reader

`internal/session/jsonl.go`:

```go
// scanJSONLLines opens the JSONL file at filepath and invokes fn for each line.
// Unlike bufio.Scanner, there is no per-line size cap — inline base64 screenshots
// that exceed 1 MB do not cause the read to abort.
//
// Lines are yielded without the trailing '\n' (and without a preceding '\r' on
// Windows-style line endings). The last line is yielded even if it has no
// trailing newline. If fn returns a non-nil error, iteration stops and that
// error is returned; callers that want to stop early on a match use a sentinel
// error and check for it at the call site.
func scanJSONLLines(jsonlFilepath string, fn func(line []byte) error) error
```

Implementation shape: `os.Open` → `defer Close` → `bufio.NewReader(f)` → loop calling `ReadBytes('\n')`. On each iteration, trim a trailing `\n` (and optional `\r`), invoke `fn` on non-empty lines, propagate its error. On `io.EOF`, invoke `fn` on the final line if non-empty, then return `nil`. Any other error from `ReadBytes` is wrapped with `stacktrace.Propagate` and returned.

The function is package-private (`scanJSONLLines`, lowercase). All current call sites live in the same package or in `cmd/`, and `cmd/` reaches into `internal/session` through named APIs like `FormatConversation` and `TailJSONLFile`. `cmd/summary.go` needs the helper too, so it either gets a small exported wrapper (`session.ScanJSONLLines`) or is refactored to route through an existing `session` API. The cleanest path is exporting the wrapper — one exported symbol; the summary command is the natural second caller and shouldn't have to reimplement the read.

### Ring-buffer callers (tail semantics)

`collectJSONLLines` and `TailJSONLFile` both keep the last N lines in a ring buffer while streaming. The callback body becomes:

```go
ring[total%n] = string(line)  // or append to lines when n == 0
total++
return nil
```

Behavior is identical to today.

### `hasConversationData` — early termination

Uses a sentinel error to stop iteration on the first `user` or `assistant` record:

```go
var errFoundConversation = errors.New("found conversation")

found := false
err := scanJSONLLines(path, func(line []byte) error {
    var r struct{ Type string `json:"type"` }
    if err := json.Unmarshal(line, &r); err != nil {
        return nil  // skip malformed lines, matching current behavior
    }
    if r.Type == "user" || r.Type == "assistant" {
        found = true
        return errFoundConversation
    }
    return nil
})
if err != nil && !errors.Is(err, errFoundConversation) {
    return false  // read error — treat as no conversation, matching current behavior
}
return found
```

### The other filter-and-collect callers

`findNamesInJSONL`, `extractUserMessagesFromJSONL`, and `analyzeJSONL` become straight callback bodies: parse the line, apply the filter, mutate outer state. No structural change, just less scanner boilerplate.

Testing
-------

### Unit test — the core regression

`internal/session/jsonl_test.go` writes a fixture JSONL to a temp dir containing:
- one small `summary` line at the top,
- one 2 MB `user` line (a base64-blob payload padded to length),
- one small `assistant` line at the bottom.

Then asserts that each of the six migrated functions completes without error and returns the expected content:

- `collectJSONLLines(path, 0)` returns 3 entries and the 2 MB payload appears in the middle entry.
- `collectJSONLLines(path, 2)` returns the last 2 entries.
- `TailJSONLFile(path, 0, w)` writes all 3 lines; `TailJSONLFile(path, 2, w)` writes the last 2.
- `findNamesInJSONL` returns the summary from the top line.
- `hasConversationData` returns true.
- `extractUserMessagesFromJSONL(path, 5)` returns the user message payload.
- `analyzeJSONL(path)` returns messageCount=2 (one user, one assistant).

The same test with the same fixture would fail against the current code with `bufio.Scanner: token too long`. That's the regression proof.

### E2E test

Append to `scripts/e2e-test.sh` a case that:
1. Spawns a minimal test mission,
2. Writes a synthetic JSONL to the test env's `~/.claude/projects/<mission-dir>/<session-id>.jsonl` containing at least one 2 MB line,
3. Runs `./_build/agenc-test session print <session-id> --all` and asserts exit 0 plus non-empty stdout,
4. Runs `./_build/agenc-test session print <session-id> --all --format=jsonl` and asserts exit 0 plus non-empty stdout.

If the test env doesn't have a clean way to hand-write a synthetic session file, the E2E can shrink to "print an existing session created by the test env" and the unit test carries the regression proof. The unit test is the load-bearing coverage; the E2E is defense-in-depth against a build-time regression that fails to wire the helper into the CLI path.

Backwards compatibility
-----------------------

None broken. The signatures of `FormatConversation`, `TailJSONLFile`, and the summary command's public surface are unchanged. Callers see:

- Sessions that used to abort with an error now succeed.
- Sessions that used to succeed still succeed with identical output.

Rollout
-------

Single PR against `main`. No feature flag, no staged rollout — the fix is either correct or the tests catch it. Revert is one commit.
