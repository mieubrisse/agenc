Configurable Session Title Word Count — Design
================================================

Status: approved (Phase 1 + edge cases + Phase 2)
Beads: agenc-1pbf

Summary
-------

The auto-summarizer's word-count target is currently hardcoded as the literal string `"3-15 word"` inside the system prompt at `internal/server/session_summarizer.go:121-124`. This design adds a single global config field, `session_title_max_words`, that lets users tune the upper bound without recompiling. The lower bound stays at 3 (the value already baked into the prompt).

Scope
-----

In scope:

- New global field on `AgencConfig`: `SessionTitleMaxWords int` (YAML / user-facing key: `sessionTitleMaxWords`, matching existing camelCase convention).
- Wiring through `config get` / `config set` per the `/config-key-checklist` skill.
- Refactoring the inline system-prompt string into a small named helper so the rendering can be unit-tested.
- Validation at both the file-load path and the `config set` path.
- E2E tests for the CLI surface; unit tests for config and prompt rendering.

Out of scope:

- Per-mission override. Global config only.
- Configurable lower bound (stays implicit at 3).
- Configurable summarizer model, timeout, or other internals.
- Documentation in `docs/system-architecture.md` — this is a tuning knob on existing behavior, not a new concept.

Phase 1: Architecture and Components
------------------------------------

### Data flow

```
config.yml                         CLI (config get/set)
   │                                      │
   ▼                                      ▼
ReadAgencConfig() ──► ValidateAndPopulateDefaults()
   │                          │
   │                          └─ enforce 3 ≤ value ≤ 50; default 15 when missing
   ▼
Server.cachedConfig (atomic.Pointer[AgencConfig])
   │
   ▼
Server.getConfig().SessionTitleMaxWords
   │
   ▼
generateSessionSummary() builds systemPrompt:
   buildSummarizerSystemPrompt(maxWords int) string
   │
   ▼
Claude Haiku CLI
```

The summarizer reads the value once at the top of `generateSessionSummary()` via the existing lock-free `s.getConfig()` (atomic pointer at `internal/server/server.go:37`). No threading of new arguments through the call chain.

### Decisions

| Decision | Choice | Reason |
|----------|--------|--------|
| Shape | Single int (max only) | Lower bound has no real use case as a knob. |
| Scope | Global only | No use case for per-mission yet; can add later if needed. |
| Default | 15 | Preserves current behavior. |
| Validation range | `3 ≤ value ≤ 50` | Floor of 3 matches the prompt's hardcoded lower bound (an invariant — see edge case 1.4). Cap of 50 prevents nonsense. |
| Name | `sessionTitleMaxWords` (YAML, user-facing key), `SessionTitleMaxWords` (Go) | "Session title" is the user-facing concept (the tmux pane title). "Summarizer" is an implementation detail. CamelCase matches existing keys (`paletteTmuxKeybinding`, `defaultModel`, `claudeArgs`). |
| Prompt rendering | `fmt.Sprintf("...output a 3-%d word terminal window title...", maxWords)` inside a new helper `buildSummarizerSystemPrompt(maxWords int) string` | The helper makes unit testing the rendering possible without booting the server. |

### Components touched

| # | File | Change |
|---|------|--------|
| 1 | `internal/config/agenc_config.go` | Add `SessionTitleMaxWords int` field; add `GetSessionTitleMaxWords()` getter; add default + range validation in `ValidateAndPopulateDefaults()`; extract a small `validateSessionTitleMaxWords(int) error` helper so file-load and `config set` share the same range check. |
| 2 | `internal/server/session_summarizer.go` | Extract the inline system prompt at lines 121-124 into a new package-private helper `buildSummarizerSystemPrompt(maxWords int) string`. Add `maxWords int` parameter to `generateSessionSummary()`. The caller `handleSummaryRequest` (a method on `*Server`) reads `s.getConfig().GetSessionTitleMaxWords()` and passes it through. |
| 3 | `cmd/config_get.go` | Register the key in `supportedConfigKeys`; add a switch arm in `getConfigValue()` returning the resolved value via the getter; update help text. |
| 4 | `cmd/config_set.go` | Add a switch arm: `strconv.Atoi`, then call the shared validator from (1), then persist. Update help text. |
| 5 | `docs/configuration.md` | Add a commented example to the global config.yml block (between `paletteTmuxKeybinding` and `tmuxWindowTitle`). The README itself has no inline example block — it points at `docs/configuration.md`. |
| 6 | Tests | See Phase 2. |

### Components NOT touched

- `docs/system-architecture.md` — no new concept introduced.
- `cmd/genprime/main.go` (prime template) — auto-generated from the Cobra tree; no manual change needed.
- `internal/claudeconfig/adjutant_claude.md` — Adjutant doesn't need to know about this knob; it's not a concept users will ask Adjutant about.

Edge Case Discovery
-------------------

### 1. Inputs and Boundaries

| # | Case | Severity | Handling |
|---|------|----------|----------|
| 1.1 | Key missing from `config.yml` | Low | Default to 15 in `ValidateAndPopulateDefaults()`. |
| 1.2 | Value = 0 or negative | High | Reject in validation. |
| 1.3 | Value > 50 | Medium | Reject in validation. |
| 1.4 | Value = 1 or 2 (below the prompt's hardcoded lower bound of 3) | High | Validation floor set at 3 — keeps the field-level invariant aligned with the prompt's literal `"3-"` prefix. |
| 1.5 | Value = 3 (renders `"3-3 word title"`) | Low | Awkward but valid. Allow. |
| 1.6 | YAML type mismatch (string, float) | Medium | Existing yaml unmarshal error path. No new code. |
| 1.7 | `config set <key> <non-int>` | Medium | `strconv.Atoi` error → clean error message. |
| 1.8 | `config set <key> <out-of-range int>` | Medium | After Atoi, run shared validator before writing. |
| 1.9 | `config get <key>` when unset | Low | Return resolved default via the getter, not empty. |

### 2. State and Transitions

| # | Case | Severity | Handling |
|---|------|----------|----------|
| 2.1 | Config hot-reload during in-flight summarization | Low | Existing `atomic.Pointer` already guarantees atomic reads. In-flight requests use the value they grabbed at start. No new code. |

### 3. External Dependencies

Skipped — no new external dependency. Summarizer still calls Claude Haiku via CLI exactly as before; only the prompt's text content changes.

### 4. Timing and Ordering

Skipped — covered by 2.1.

### 5. Resources and Limits

| # | Case | Severity | Handling |
|---|------|----------|----------|
| 5.1 | Larger value → longer Haiku output → more tokens/latency | Low | Existing `summarizerMaxOutputLen` cap bounds output; validation cap of 50 keeps things sane. No new code. |

### 6. Domain-Specific (CLI config-key)

Covered above in 1.7–1.9.

**Adversarial.** The value is parsed as `int` and rendered with `%d`. No string interpolation of user-controlled text into the prompt. No prompt-injection surface introduced.

Phase 2: Error Handling and Testing
------------------------------------

### Error handling

| Case | Site | Behavior |
|------|------|----------|
| 1.1 missing key | `ValidateAndPopulateDefaults()` | Zero-value → set to 15. |
| 1.2 / 1.3 / 1.4 out-of-range | `ValidateAndPopulateDefaults()` via shared `validateSessionTitleMaxWords()` | Return error: `session_title_max_words must be between 3 and 50, got %d`. Bubbles up through `ReadAgencConfig()`. Server fails to start on invalid config — matches existing pattern. |
| 1.6 YAML type mismatch | yaml unmarshal | Existing error path. No new code. |
| 1.7 non-int via CLI | `cmd/config_set.go` switch arm | `strconv.Atoi` → `fmt.Errorf("session_title_max_words must be an integer, got %q", value)`. |
| 1.8 out-of-range via CLI | `cmd/config_set.go` switch arm | After Atoi, run `validateSessionTitleMaxWords()`; reject before writing to disk. |
| 1.9 get when unset | `cmd/config_get.go` switch arm | Print `GetSessionTitleMaxWords()` (returns default if zero). |

### Testing

**Unit tests in `internal/config/agenc_config_test.go`:**

1. `TestSessionTitleMaxWords_DefaultWhenMissing` — write a YAML without the key, read it, assert value is 15. *(1.1)*
2. `TestSessionTitleMaxWords_RejectsOutOfRange` — table-driven: 0, -1, 2, 51, 1000. Each must return a validation error. *(1.2, 1.3, 1.4)*
3. `TestSessionTitleMaxWords_AcceptsBoundaries` — write 3, write 50, write 15. All round-trip cleanly.

**Unit test in `internal/server/session_summarizer_test.go` (new file):**

4. `TestBuildSummarizerSystemPrompt` — assert that `buildSummarizerSystemPrompt(10)` contains `"3-10 word"` and `buildSummarizerSystemPrompt(15)` contains `"3-15 word"`.

**E2E tests in `scripts/e2e-test.sh`:**

5. `config get session_title_max_words` returns `15` on a fresh test env. *(1.9)*
6. `config set session_title_max_words 10` succeeds; subsequent `get` returns `10`.
7. `config set session_title_max_words 100` exits non-zero. *(1.8)*
8. `config set session_title_max_words abc` exits non-zero. *(1.7)*

**Not tested:** the actual Haiku call with a custom value. Cost/flakiness too high for CI; the prompt-rendering unit test (#4) gives the wiring guarantee, and Haiku's job is to honor whatever number we pass.
