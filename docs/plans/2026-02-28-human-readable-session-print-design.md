Human-Readable Session Print
=============================

**Date:** 2026-02-28
**Type:** Feature
**Status:** Approved
**Related:** 2026-02-21-session-print-design.md

Purpose
-------

Make `agenc mission print` and `agenc session print` default to a
human-readable conversation view instead of raw JSONL. The primary consumer
is the Adjutant agent, which reads session transcripts to help users identify
friction points. Raw JSONL remains available via `--format=jsonl`.

CLI Interface
-------------

Both commands gain a `--format` flag:

```
--format string    output format: "text" or "jsonl" (default "text")
```

- `--format text` (default): human-readable conversation view
- `--format jsonl`: raw JSONL output (current behavior, backward compatible)

Existing `--tail` and `--all` flags continue to count raw JSONL entries.
In text mode, the last N JSONL entries are collected then rendered
human-readably.

Output Format
-------------

```
[USER]
find me a good hamburger shop in Novo Hamburgo...

[ASSISTANT]
Let me search for that.
  > WebSearch("melhores hamburguerias Novo Hamburgo RS")
  > WebFetch("https://tripadvisor.com/...")
  > WebSearch("Quiero Cafe Novo Hamburgo address")

[USER]
great, what about pizza?

[ASSISTANT]
  > WebSearch("best pizza Novo Hamburgo RS")

Here's what I found...
```

Formatting rules:

- `[USER]` and `[ASSISTANT]` tags on their own line, content on the next line
- Blank line between conversation blocks
- Text blocks rendered as-is (preserving markdown, newlines)
- Tool calls indented: `  > ToolName("key param")`
- Multiple tool calls listed sequentially (no blank lines between them)
- Tool call parameter values truncated at ~100 chars with `...`
- Tool result errors shown as `  > ERROR: <truncated message>` (truncated at ~200 chars)
- No color codes -- plain text, suitable for piping

JSONL Processing Pipeline
-------------------------

Each JSONL line is parsed and dispatched by its `type` field:

| Type | Action |
|------|--------|
| `user` | Extract `message.content` -- string or content block array |
| `assistant` | Extract `message.content` blocks: `text`, `tool_use` |
| `system` | Skip |
| `progress` | Skip |
| `file-history-snapshot` | Skip |
| `queue-operation` | Skip |
| Unknown | Skip |

For user messages with array content:

- `text` blocks: show the text
- `tool_result` blocks: skip unless `is_error: true`, then show truncated error

For assistant messages:

- `text` blocks: show full text
- `tool_use` blocks: one-line summary with key parameter
- `thinking` blocks: skip

Tool Parameter Map
------------------

Hardcoded map of tool name to key input field(s) for one-line summaries:

| Tool | Key Field(s) | Example |
|------|-------------|---------|
| `Bash` | `command` | `Bash("git status")` |
| `Read` | `file_path` | `Read("/src/main.go")` |
| `Edit` | `file_path` | `Edit("/src/main.go")` |
| `Write` | `file_path` | `Write("/src/main.go")` |
| `Glob` | `pattern` | `Glob("**/*.go")` |
| `Grep` | `pattern`, `path` | `Grep("TODO", path="src/")` |
| `WebSearch` | `query` | `WebSearch("best pizza RS")` |
| `WebFetch` | `url` | `WebFetch("https://example.com")` |
| `Task` | `description` | `Task("Explore codebase")` |
| `NotebookEdit` | `notebook_path` | `NotebookEdit("/analysis.ipynb")` |
| `Skill` | `skill` | `Skill("brainstorming")` |
| `TaskCreate` | `subject` | `TaskCreate("Fix auth bug")` |
| `TaskUpdate` | `taskId`, `status` | `TaskUpdate("3", status="completed")` |
| MCP tools | first string field | `mcp__todoist__add-tasks(...)` |
| Unknown | (none) | `UnknownTool()` |

File Changes
------------

### New files

- `internal/session/format.go` -- `FormatConversation()` function, JSONL
  parsing types, tool parameter map, all rendering logic

### Modified files

- `cmd/session_print.go` -- add `--format` flag, rename `printSessionJSONL`
  to `printSession`, dispatch on format
- `cmd/mission_print.go` -- add `--format` flag, pass format to shared
  print function

### No new dependencies

Uses existing `encoding/json`, `bufio`, `io`, `fmt`, `strings`.

### No database schema changes

### No architecture doc updates

No new packages, no process boundary changes -- just a new file in an
existing package.

Design Decisions
----------------

- **Default is human-readable.** The original design said "raw output only,
  YAGNI." The need has emerged: the Adjutant agent needs readable transcripts,
  and manual review of sessions required 4 iterations of ad-hoc Python parsing.
- **Hardcoded tool parameter map** matches how Claude Code itself renders
  tool calls (each tool has a `userFacingName(input)` method with per-tool
  key parameter logic).
- **Tail counts JSONL entries, not turns.** Keeps backward compatibility and
  avoids the complexity of pre-scanning for turn boundaries.
- **Thinking blocks omitted.** They're internal reasoning that adds noise.
  The text output already captures the conclusion.
- **No color codes.** Output is designed for piping and agent consumption,
  not just terminal display.
- **Tool result errors shown, successes hidden.** Errors are signal; success
  results are noise (often very long file contents, command output, etc.).
