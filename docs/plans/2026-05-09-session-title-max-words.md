Configurable Session Title Word Count — Implementation Plan
=============================================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the auto-summarizer's word-count target configurable via a new global config field `sessionTitleMaxWords` (default 15, range 3-50), wired through `agenc config get`/`set` and the existing summarizer prompt.

**Architecture:** Add a single int field to `AgencConfig`. The summarizer (currently a package-level function) gets a new `maxWords int` parameter; its caller (a `*Server` method) reads the value via the existing lock-free atomic config pointer. The literal `"3-15 word"` substring in the system prompt becomes `fmt.Sprintf("3-%d word", maxWords)` inside a small extracted helper, which makes the rendering unit-testable without booting the server.

**Tech Stack:** Go, Cobra, `goccy/go-yaml`, existing test helpers in `agenc_config_test.go`, and the bash-based E2E harness at `scripts/e2e-test.sh`.

**Design doc:** `docs/plans/2026-05-09-session-title-max-words-design.md` (read first if any task feels under-specified).

**Beads:** `agenc-1pbf`

---

Pre-flight notes
----------------

- This is the AgenC repo. Treat as **solo workflow** per repo CLAUDE.md (commit directly to `main`).
- Run `make build` and `make check` with `dangerouslyDisableSandbox: true` — they need access to `~/.cache/go-build` and bind unix sockets in `/tmp/claude-501/...` outside the default sandbox.
- The pre-commit hook runs `make check` automatically. **Do NOT pass `--no-verify`** at any point.
- The push step is `git add → git commit → git pull --rebase → git push` per repo CLAUDE.md.
- Commit messages: single line, no `Co-Authored-By` footer.

---

Task 1: Add config field, getter, validator, default
----------------------------------------------------

**Files:**

- Modify: `internal/config/agenc_config.go`
  - Struct definition: lines 397-407 (`AgencConfig`)
  - Existing getter style: lines 409-425 (`GetPaletteTmuxKeybinding`, `GetTmuxWindowTitleConfig`)
  - `ValidateAndPopulateDefaults`: lines 1037-1079
- Test: `internal/config/agenc_config_test.go` (append new tests at end of file)

**Step 1: Write the failing tests**

Append to `internal/config/agenc_config_test.go`:

```go
func TestSessionTitleMaxWords_DefaultWhenMissing(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	// Write a config without sessionTitleMaxWords
	cfg := &AgencConfig{}
	if err := WriteAgencConfig(tmpDir, cfg, nil); err != nil {
		t.Fatalf("WriteAgencConfig failed: %v", err)
	}

	got, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	if v := got.GetSessionTitleMaxWords(); v != DefaultSessionTitleMaxWords {
		t.Errorf("expected default %d, got %d", DefaultSessionTitleMaxWords, v)
	}
}

func TestSessionTitleMaxWords_AcceptsBoundaries(t *testing.T) {
	for _, v := range []int{3, 15, 50} {
		t.Run(fmt.Sprintf("value=%d", v), func(t *testing.T) {
			tmpDir := t.TempDir()
			configDirpath := filepath.Join(tmpDir, ConfigDirname)
			if err := os.MkdirAll(configDirpath, 0755); err != nil {
				t.Fatal(err)
			}

			cfg := &AgencConfig{SessionTitleMaxWords: v}
			if err := WriteAgencConfig(tmpDir, cfg, nil); err != nil {
				t.Fatalf("WriteAgencConfig failed: %v", err)
			}

			got, _, err := ReadAgencConfig(tmpDir)
			if err != nil {
				t.Fatalf("ReadAgencConfig failed for value %d: %v", v, err)
			}
			if got.SessionTitleMaxWords != v {
				t.Errorf("expected %d, got %d", v, got.SessionTitleMaxWords)
			}
		})
	}
}

func TestSessionTitleMaxWords_RejectsOutOfRange(t *testing.T) {
	for _, v := range []int{-1, 0, 1, 2, 51, 1000} {
		t.Run(fmt.Sprintf("value=%d", v), func(t *testing.T) {
			tmpDir := t.TempDir()
			configDirpath := filepath.Join(tmpDir, ConfigDirname)
			if err := os.MkdirAll(configDirpath, 0755); err != nil {
				t.Fatal(err)
			}

			cfg := &AgencConfig{SessionTitleMaxWords: v}
			if err := WriteAgencConfig(tmpDir, cfg, nil); err != nil {
				t.Fatalf("WriteAgencConfig failed: %v", err)
			}

			_, _, err := ReadAgencConfig(tmpDir)
			if err == nil {
				t.Errorf("expected ReadAgencConfig to reject %d, got nil error", v)
			}
		})
	}
}
```

If `fmt` is not yet imported in the test file, add it.

**Step 2: Run tests to verify they fail**

```
cd /Users/odyssey/.agenc/missions/656966a3-6805-41ec-9121-e1d6d5a3dff6/agent
go test ./internal/config/ -run TestSessionTitleMaxWords -v
```

Expected: compile error (`undefined: DefaultSessionTitleMaxWords`, `unknown field SessionTitleMaxWords`).

**Step 3: Implement the field, getter, default, validator**

In `internal/config/agenc_config.go`:

(a) Find an existing constants block near the top of the file (search for `const` near `DefaultPaletteTmuxKeybinding` if present, else add a new block right above `type AgencConfig struct`). Add:

```go
const (
	// DefaultSessionTitleMaxWords is the default upper bound for words in the
	// auto-generated session title.
	DefaultSessionTitleMaxWords = 15

	// MinSessionTitleMaxWords is the lower bound for the configurable upper bound.
	// It matches the lower bound baked into the summarizer system prompt.
	MinSessionTitleMaxWords = 3

	// MaxSessionTitleMaxWords is a sanity cap to prevent nonsense values.
	MaxSessionTitleMaxWords = 50
)
```

(b) In the `AgencConfig` struct (lines 397-407), add the field at the end:

```go
SessionTitleMaxWords  int                             `yaml:"sessionTitleMaxWords,omitempty"`
```

(c) Below the existing `Get*` methods (after `GetTmuxWindowTitleConfig`, around line 425), add:

```go
// GetSessionTitleMaxWords returns the configured upper bound for words in the
// auto-generated session title, falling back to DefaultSessionTitleMaxWords
// when unset (zero value).
func (c *AgencConfig) GetSessionTitleMaxWords() int {
	if c.SessionTitleMaxWords == 0 {
		return DefaultSessionTitleMaxWords
	}
	return c.SessionTitleMaxWords
}
```

(d) Add the shared validator near the other `validate*` helpers (e.g., right after `validateSleepMode` around line 692):

```go
// ValidateSessionTitleMaxWords returns an error if v is outside the supported
// range. Zero is allowed (it means "unset, use default") — callers that read
// the field directly from a freshly-loaded config should treat zero as default.
func ValidateSessionTitleMaxWords(v int) error {
	if v == 0 {
		return nil
	}
	if v < MinSessionTitleMaxWords || v > MaxSessionTitleMaxWords {
		return stacktrace.NewError(
			"sessionTitleMaxWords must be between %d and %d, got %d",
			MinSessionTitleMaxWords, MaxSessionTitleMaxWords, v,
		)
	}
	return nil
}
```

(e) In `ValidateAndPopulateDefaults` (line 1037), add a call before `return nil`:

```go
	if err := ValidateSessionTitleMaxWords(cfg.SessionTitleMaxWords); err != nil {
		return err
	}

	return nil
}
```

**Step 4: Run tests to verify they pass**

```
go test ./internal/config/ -run TestSessionTitleMaxWords -v
```

Expected: all three tests PASS.

Then run the full config package and make sure nothing else regressed:

```
go test ./internal/config/...
```

Expected: PASS.

**Step 5: Commit**

```
git add internal/config/agenc_config.go internal/config/agenc_config_test.go
git commit -m "Add SessionTitleMaxWords config field with default 15, range 3-50 (agenc-1pbf)"
```

---

Task 2: Extract summarizer system prompt into a testable helper
----------------------------------------------------------------

**Files:**

- Modify: `internal/server/session_summarizer.go` (lines 121-124)
- Create: `internal/server/session_summarizer_test.go`

**Step 1: Write the failing test**

Create `internal/server/session_summarizer_test.go`:

```go
package server

import (
	"strings"
	"testing"
)

func TestBuildSummarizerSystemPrompt(t *testing.T) {
	tests := []struct {
		maxWords int
		wantSub  string
	}{
		{maxWords: 15, wantSub: "3-15 word"},
		{maxWords: 10, wantSub: "3-10 word"},
		{maxWords: 50, wantSub: "3-50 word"},
		{maxWords: 3, wantSub: "3-3 word"},
	}
	for _, tt := range tests {
		got := buildSummarizerSystemPrompt(tt.maxWords)
		if !strings.Contains(got, tt.wantSub) {
			t.Errorf("buildSummarizerSystemPrompt(%d) = %q; want substring %q",
				tt.maxWords, got, tt.wantSub)
		}
		// Sanity: the prompt should still mention "title generator" — guards
		// against accidental gutting of the rest of the prompt body.
		if !strings.Contains(got, "title generator") {
			t.Errorf("buildSummarizerSystemPrompt(%d) lost the 'title generator' phrase", tt.maxWords)
		}
	}
}
```

**Step 2: Run test to verify it fails**

```
go test ./internal/server/ -run TestBuildSummarizerSystemPrompt -v
```

Expected: compile error (`undefined: buildSummarizerSystemPrompt`).

**Step 3: Extract the helper**

In `internal/server/session_summarizer.go`, replace lines 121-124:

```go
	systemPrompt := "You are a title generator. You will receive the text of a user's request to an AI coding assistant. " +
		"Your job: output a 3-15 word terminal window title summarizing what the user is working on. " +
		"Rules: output ONLY the title. No quotes. No punctuation at the end. No markdown. No explanation. " +
		"Do NOT answer the user's request. Do NOT ask questions. Do NOT offer help. Just the title."
```

with:

```go
	systemPrompt := buildSummarizerSystemPrompt(15)
```

(The hardcoded `15` is temporary — Task 3 wires it through from config.)

Then, near the bottom of the file (after `generateSessionSummary` returns), add:

```go
// buildSummarizerSystemPrompt renders the system prompt used by the auto-
// summarizer. The rendered "3-N word" range is the only knob; all other
// instructions are fixed.
func buildSummarizerSystemPrompt(maxWords int) string {
	return fmt.Sprintf(
		"You are a title generator. You will receive the text of a user's request to an AI coding assistant. "+
			"Your job: output a 3-%d word terminal window title summarizing what the user is working on. "+
			"Rules: output ONLY the title. No quotes. No punctuation at the end. No markdown. No explanation. "+
			"Do NOT answer the user's request. Do NOT ask questions. Do NOT offer help. Just the title.",
		maxWords,
	)
}
```

`fmt` is already imported at line 5.

**Step 4: Run tests to verify**

```
go test ./internal/server/ -run TestBuildSummarizerSystemPrompt -v
```

Expected: PASS.

Then the full server package:

```
go test ./internal/server/...
```

Expected: PASS (no regressions).

**Step 5: Commit**

```
git add internal/server/session_summarizer.go internal/server/session_summarizer_test.go
git commit -m "Extract summarizer system prompt into testable helper buildSummarizerSystemPrompt (agenc-1pbf)"
```

---

Task 3: Wire the summarizer to read the config value
-----------------------------------------------------

**Files:**

- Modify: `internal/server/session_summarizer.go` — change `generateSessionSummary` signature; update `handleSummaryRequest` caller (line 94).

**Step 1: Update `generateSessionSummary` signature**

Change the function declaration (line 114):

```go
func generateSessionSummary(ctx context.Context, agencDirpath string, firstUserMessage string) (string, error) {
```

to:

```go
func generateSessionSummary(ctx context.Context, agencDirpath string, firstUserMessage string, maxWords int) (string, error) {
```

Inside the function body, replace:

```go
	systemPrompt := buildSummarizerSystemPrompt(15)
```

with:

```go
	systemPrompt := buildSummarizerSystemPrompt(maxWords)
```

**Step 2: Update the caller**

In `handleSummaryRequest` (line 94), change:

```go
	summary, err := generateSessionSummary(ctx, s.agencDirpath, req.firstUserMessage)
```

to:

```go
	maxWords := s.getConfig().GetSessionTitleMaxWords()
	summary, err := generateSessionSummary(ctx, s.agencDirpath, req.firstUserMessage, maxWords)
```

**Step 3: Verify compilation and existing tests still pass**

```
go build ./...
go test ./internal/server/...
```

Expected: build succeeds; tests PASS.

**Step 4: Commit**

```
git add internal/server/session_summarizer.go
git commit -m "Pass configured sessionTitleMaxWords through to summarizer prompt (agenc-1pbf)"
```

---

Task 4: Wire `agenc config get sessionTitleMaxWords`
-----------------------------------------------------

**Files:**

- Modify: `cmd/config_get.go`

**Step 1: Register the key**

In `cmd/config_get.go`, append to `supportedConfigKeys` (lines 14-23):

```go
	"sessionTitleMaxWords",
```

**Step 2: Add help text**

In the `Long:` field of `configGetCmd` (lines 28-40), append the line (preserve column alignment with surrounding entries):

```
  sessionTitleMaxWords                       Max words in auto-generated session titles (default: 15; range: 3-50)
```

**Step 3: Add the switch arm**

In `getConfigValue` (around line 105), add a new `case` before `default`:

```go
	case "sessionTitleMaxWords":
		return strconv.Itoa(cfg.GetSessionTitleMaxWords()), nil
```

Add the import for `strconv` at the top of the file (between `fmt` and `strings`):

```go
	"strconv"
```

**Step 4: Build and run unit tests in cmd/**

```
go build ./...
go test ./cmd/...
```

Expected: PASS.

**Step 5: Commit**

```
git add cmd/config_get.go
git commit -m "Register sessionTitleMaxWords key in config get (agenc-1pbf)"
```

---

Task 5: Wire `agenc config set sessionTitleMaxWords`
-----------------------------------------------------

**Files:**

- Modify: `cmd/config_set.go`

**Step 1: Add help text**

In the `Long:` field (lines 17-53), append within the supported-keys block:

```
  sessionTitleMaxWords                       Max words in auto-generated session titles (default: 15; range: 3-50)
```

**Step 2: Add the switch arm**

In `setConfigValue` (around line 149), add a new `case` before `default`:

```go
	case "sessionTitleMaxWords":
		n, err := strconv.Atoi(value)
		if err != nil {
			return stacktrace.NewError(
				"sessionTitleMaxWords must be an integer, got %q", value,
			)
		}
		if err := config.ValidateSessionTitleMaxWords(n); err != nil {
			return err
		}
		cfg.SessionTitleMaxWords = n
		return nil
```

Add the import for `strconv` at the top of the file (alphabetically between `fmt` and `strings`):

```go
	"strconv"
```

**Step 3: Build and verify**

```
go build ./...
go test ./cmd/...
```

Expected: PASS.

Manual sanity check (not required for commit, but useful):

```
make test-env
./_build/agenc-test config get sessionTitleMaxWords     # → 15
./_build/agenc-test config set sessionTitleMaxWords 8
./_build/agenc-test config get sessionTitleMaxWords     # → 8
./_build/agenc-test config set sessionTitleMaxWords 100  # → error, exit non-zero
./_build/agenc-test config set sessionTitleMaxWords abc  # → error, exit non-zero
make test-env-clean
```

**Step 4: Commit**

```
git add cmd/config_set.go
git commit -m "Register sessionTitleMaxWords key in config set with int parsing + range validation (agenc-1pbf)"
```

---

Task 6: Add E2E tests
---------------------

**Files:**

- Modify: `scripts/e2e-test.sh` — append to the `--- Config commands ---` section (starts at line 139).

**Step 1: Add E2E tests**

Locate the block that ends at line 150 (after `config sleep --help succeeds`). Insert the following tests just **before** the trailing `echo ""` at line 152:

```bash
run_test_output_contains "config get sessionTitleMaxWords returns default" \
    "^15$" \
    "${agenc_test}" config get sessionTitleMaxWords

run_test "config set sessionTitleMaxWords accepts valid int" \
    0 \
    "${agenc_test}" config set sessionTitleMaxWords 10

run_test_output_contains "config get reflects the new value" \
    "^10$" \
    "${agenc_test}" config get sessionTitleMaxWords

run_test "config set sessionTitleMaxWords rejects out-of-range" \
    1 \
    "${agenc_test}" config set sessionTitleMaxWords 100

run_test "config set sessionTitleMaxWords rejects non-integer" \
    1 \
    "${agenc_test}" config set sessionTitleMaxWords abc

# Reset to default for downstream tests
run_test "config set sessionTitleMaxWords reset" \
    0 \
    "${agenc_test}" config set sessionTitleMaxWords 15
```

Note: `run_test` checks for an exact exit code, so 1 is the expected exit on error from `setConfigValue` returning an error (cobra surfaces `RunE` errors as exit 1).

If the regex anchors `^15$` / `^10$` don't match because output has trailing whitespace from the `Println`, drop the `^` and `$` and use `"^15"` / `"^10"` instead. The simplest alternative is:

```bash
run_test_output_contains "config get sessionTitleMaxWords returns default" \
    "15" \
    "${agenc_test}" config get sessionTitleMaxWords
```

— but that may match other config values containing "15". Try the anchored form first.

**Step 2: Run E2E**

```
make e2e
```

This requires `dangerouslyDisableSandbox: true` (it builds the binary, which needs the Go cache).

Expected: all new tests PASS, plus all pre-existing tests PASS, exit code 0.

**Step 3: Commit**

```
git add scripts/e2e-test.sh
git commit -m "Add E2E tests for sessionTitleMaxWords config get/set (agenc-1pbf)"
```

---

Task 7: Document the new config key in `docs/configuration.md`
---------------------------------------------------------------

**Files:**

- Modify: `docs/configuration.md`

**Step 1: Add a commented example in the global config block**

In `docs/configuration.md`, find the line:

```
# Tmux window tab coloring — visual feedback for Claude state
```

(around line 78). Insert above it:

```
# Max words in auto-generated session titles (default: 15; range: 3-50).
# Lower bound stays at 3; this configures only the upper bound.
# sessionTitleMaxWords: 10
```

Leave a blank line above and below the new block to match surrounding spacing.

**Step 2: No build needed — docs only**

Verify the file renders:

```
grep -A 3 sessionTitleMaxWords docs/configuration.md
```

Expected: shows the new block.

**Step 3: Commit and push everything**

```
git add docs/configuration.md
git commit -m "Document sessionTitleMaxWords config key in configuration.md (agenc-1pbf)"
git pull --rebase
git push
```

---

Task 8: Close the beads issue
------------------------------

**Step 1: Verify the full check passes**

```
make check
```

(Use `dangerouslyDisableSandbox: true`.)

Expected: all green.

**Step 2: Close the bead**

```
bd close agenc-1pbf --reason="Implemented per docs/plans/2026-05-09-session-title-max-words-design.md and -impl plan"
```

**Step 3: Final push (the bead close auto-commits the dolt state)**

```
git status
git add .beads/issues.jsonl .beads/export-state.json 2>/dev/null || true
git commit -m "Close agenc-1pbf" 2>/dev/null || true
git pull --rebase
git push
```

If there's nothing to commit (the bead push to dolt may not produce a local file change), the no-op commit will fail harmlessly — the `|| true` handles it.

---

Verification Checklist
----------------------

Before declaring done:

- [ ] `make build` passes (with `dangerouslyDisableSandbox: true`)
- [ ] `make check` passes (with `dangerouslyDisableSandbox: true`)
- [ ] `make e2e` passes (with `dangerouslyDisableSandbox: true`)
- [ ] `agenc config get sessionTitleMaxWords` returns `15` on a fresh test env
- [ ] `agenc config set sessionTitleMaxWords 10` succeeds, get returns `10`
- [ ] `agenc config set sessionTitleMaxWords 100` and `... abc` both fail cleanly
- [ ] `docs/configuration.md` mentions `sessionTitleMaxWords`
- [ ] All commits pushed to `origin/main`
- [ ] Beads issue `agenc-1pbf` closed
