# Session-Agnostic Tmux Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove `AGENC_TMUX` entirely and replace all "are we in tmux?" checks with the standard `$TMUX` env var, so AgenC tmux features work in any tmux session.

**Architecture:** `isInsideAgencTmux()` in `cmd/tmux_helpers.go` is the single guard function used by all `agenc tmux` subcommands. Two wrapper functions in `internal/wrapper/tmux.go` also gate on the env var. The change is mechanical: remove the `AGENC_TMUX` constant, rename the guard function, swap the condition, and update every call site and doc reference.

**Tech Stack:** Go, tmux, cobra

---

### Task 1: Remove `AGENC_TMUX` constant and rename guard in `cmd/tmux_helpers.go`

**Files:**
- Modify: `cmd/tmux_helpers.go`

**Step 1: Open the file and understand what's there**

Read `cmd/tmux_helpers.go`. You'll see:
- `agencTmuxEnvVar = "AGENC_TMUX"` constant (line 16)
- `isInsideAgencTmux()` function (line 128–130) that checks `os.Getenv(agencTmuxEnvVar) == "1"`

**Step 2: Remove the `agencTmuxEnvVar` constant**

Delete line 16 (`agencTmuxEnvVar    = "AGENC_TMUX"`). The `agencDirpathEnvVar` constant below it stays.

**Step 3: Rename and rewrite `isInsideAgencTmux()`**

Replace:
```go
// isInsideAgencTmux returns true if the current process is running inside
// the AgenC tmux session.
func isInsideAgencTmux() bool {
	return strings.TrimSpace(os.Getenv(agencTmuxEnvVar)) == "1"
}
```

With:
```go
// isInsideTmux returns true if the current process is running inside any
// tmux session (i.e. the $TMUX environment variable is set).
func isInsideTmux() bool {
	return os.Getenv("TMUX") != ""
}
```

**Step 4: Check whether the `strings` import is still needed**

After removing the `strings.TrimSpace` call, check if `strings` is still used elsewhere in `tmux_helpers.go`. It is not (the only other string op in the file is in `shellQuote`, which uses `strings.ReplaceAll` — wait, `shellQuote` is in `tmux_attach.go`). If `strings` is now unused, remove it from the import block.

Actually, check: `tmux_helpers.go` only imports `fmt`, `os`, `os/exec`, and the two internal packages. `strings` is not imported there — so no import change needed.

**Step 5: Build to confirm no compile errors**

```bash
make build
```
Expected: builds successfully (zero errors).

**Step 6: Commit**

```bash
git add cmd/tmux_helpers.go
git commit -m "Remove AGENC_TMUX constant, rename isInsideAgencTmux to isInsideTmux"
git pull --rebase
git push
```

---

### Task 2: Update `cmd/tmux_attach.go` — stop setting `AGENC_TMUX`

**Files:**
- Modify: `cmd/tmux_attach.go`

**Step 1: Update the nested-attach guard**

In `runTmuxAttach()`, line 30:
```go
if os.Getenv(agencTmuxEnvVar) == "1" {
```
Change to:
```go
if isInsideTmux() {
```

**Step 2: Update `printInsideSessionError()`**

In `tmux_helpers.go`, `printInsideSessionError()` currently says:
```
"Already inside the agenc tmux session. Use standard tmux commands to navigate."
```
Change it to:
```go
func printInsideSessionError() {
	fmt.Println("Already inside a tmux session. Use standard tmux commands to navigate.")
}
```

**Step 3: Update `createTmuxSession()` — stop setting `AGENC_TMUX=1`**

In `createTmuxSession()`:

Remove the `AGENC_TMUX=1` prefix from the initial command string. Change:
```go
initialCmd := agencTmuxEnvVar + "=1"
dirpathValue := os.Getenv(agencDirpathEnvVar)
if dirpathValue != "" {
    initialCmd += " " + agencDirpathEnvVar + "=" + shellQuote(dirpathValue)
}
initialCmd += " " + agencBinaryPath + " " + missionCmdStr + " " + newCmdStr
```
To:
```go
initialCmd := ""
dirpathValue := os.Getenv(agencDirpathEnvVar)
if dirpathValue != "" {
    initialCmd = agencDirpathEnvVar + "=" + shellQuote(dirpathValue) + " "
}
initialCmd += agencBinaryPath + " " + missionCmdStr + " " + newCmdStr
```

Then remove the `setTmuxSessionEnv(agencTmuxEnvVar, "1")` call and its error check. That block is:
```go
// Set session environment variables so that subsequent windows (created via
// tmux new-window) also inherit them.
if err := setTmuxSessionEnv(agencTmuxEnvVar, "1"); err != nil {
    return err
}
```
Delete it entirely. Keep only the `AGENC_DIRPATH` block.

**Step 4: Update the command docstring**

In `tmuxAttachCmd.Long`, remove the sentence "The session sets AGENC_TMUX=1 and propagates AGENC_DIRPATH so all windows share the same agenc configuration." Replace with: "The session propagates AGENC_DIRPATH so all windows share the same agenc configuration."

**Step 5: Build**

```bash
make build
```
Expected: builds successfully.

**Step 6: Commit**

```bash
git add cmd/tmux_attach.go cmd/tmux_helpers.go
git commit -m "Stop setting AGENC_TMUX in tmux attach; use isInsideTmux for nested-attach guard"
git pull --rebase
git push
```

---

### Task 3: Update the four `cmd/tmux_*.go` call sites

**Files:**
- Modify: `cmd/tmux_palette.go` (line 94)
- Modify: `cmd/tmux_window_new.go` (line 51)
- Modify: `cmd/tmux_pane_new.go` (line 45)
- Modify: `cmd/tmux_switch.go` (line 36)

Each file has exactly this pattern:
```go
if !isInsideAgencTmux() {
    return stacktrace.NewError("must be run inside the AgenC tmux session (AGENC_TMUX != 1)")
}
```

In all four files, change to:
```go
if !isInsideTmux() {
    return stacktrace.NewError("must be run inside a tmux session")
}
```

Also update the command `Long` docstring in `tmux_pane_new.go` and `tmux_window_new.go` — both say "Must be run from inside the AgenC tmux session." Change to "Must be run from inside a tmux session."

**Step 1: Make the changes in all four files**

Apply the guard change and docstring changes to each file.

**Step 2: Build**

```bash
make build
```
Expected: builds successfully.

**Step 3: Commit**

```bash
git add cmd/tmux_palette.go cmd/tmux_window_new.go cmd/tmux_pane_new.go cmd/tmux_switch.go
git commit -m "Update tmux subcommands to use isInsideTmux and session-agnostic error messages"
git pull --rebase
git push
```

---

### Task 4: Update `internal/wrapper/tmux.go`

**Files:**
- Modify: `internal/wrapper/tmux.go`

**Step 1: Remove the `agencTmuxEnvVar` constant**

Delete lines 12–14:
```go
const (
	agencTmuxEnvVar = "AGENC_TMUX"
)
```

**Step 2: Update `renameWindowForTmux()`**

Line 36 currently reads:
```go
if os.Getenv(agencTmuxEnvVar) != "1" {
    return
}
```
Change to:
```go
if os.Getenv("TMUX") == "" {
    return
}
```

**Step 3: Update `updateWindowTitleFromSession()`**

Line 187 currently reads:
```go
if os.Getenv(agencTmuxEnvVar) != "1" {
    return
}
```
Change to:
```go
if os.Getenv("TMUX") == "" {
    return
}
```

**Step 4: Update comments**

- Line 28: Change "AgenC tmux session (AGENC_TMUX == 1)" → "any tmux session"
- Line 184: Change "Only runs inside the AgenC tmux session (AGENC_TMUX == 1)." → "Only runs inside a tmux session."

**Step 5: Build**

```bash
make build
```
Expected: builds successfully.

**Step 6: Commit**

```bash
git add internal/wrapper/tmux.go
git commit -m "Remove AGENC_TMUX guard from wrapper; activate tmux features in any tmux session"
git pull --rebase
git push
```

---

### Task 5: Update `internal/claudeconfig/adjutant_claude.md`

**Files:**
- Modify: `internal/claudeconfig/adjutant_claude.md`

**Step 1: Find the section**

Lines 31–34 currently read:
```
Check the `$AGENC_TMUX` environment variable to determine whether you are running inside AgenC's tmux session.

- **`AGENC_TMUX` is set** — you are inside tmux. You can launch and resume missions...
- **`AGENC_TMUX` is not set** — you are outside tmux. You cannot launch missions...
```

**Step 2: Replace with `$TMUX` check**

Replace those lines with:
```markdown
Check the `$TMUX` environment variable to determine whether you are running inside a tmux session.

- **`$TMUX` is set** — you are inside tmux. You can launch and resume missions directly by running `agenc tmux window new -- agenc mission new <args>` or `agenc tmux window new -- agenc mission resume <args>`. This opens a new tmux window for the mission.
- **`$TMUX` is not set** — you are outside tmux. You cannot launch missions yourself because there is no tmux session to create windows in. Instead, give the user the command they need to run (e.g., `agenc mission new <args>` or `agenc mission resume <args>`) and let them execute it.
```

**Step 3: Commit**

```bash
git add internal/claudeconfig/adjutant_claude.md
git commit -m "Update Adjutant tmux detection to use \$TMUX instead of \$AGENC_TMUX"
git pull --rebase
git push
```

---

### Task 6: Update `docs/system-architecture.md`

**Files:**
- Modify: `docs/system-architecture.md`

**Step 1: Find the line**

Search for "AGENC_TMUX=1" in `docs/system-architecture.md`. It appears in the `wrapper/tmux.go` bullet (line ~327):
```
- `tmux.go` — tmux window renaming when `AGENC_TMUX=1` (startup: ...
```

**Step 2: Update**

Change `"when AGENC_TMUX=1"` to `"when inside any tmux session ($TMUX set)"`.

**Step 3: Commit**

```bash
git add docs/system-architecture.md
git commit -m "Update architecture doc: tmux features activate on \$TMUX, not AGENC_TMUX"
git pull --rebase
git push
```

---

### Task 7: Update `specs/tmux-mission-windows.md`

**Files:**
- Modify: `specs/tmux-mission-windows.md`

**Step 1: Find all `AGENC_TMUX` references**

Run: `grep -n "AGENC_TMUX" specs/tmux-mission-windows.md`

You'll find references at approximately lines 43, 57, 87, 91, 121, 151, 217.

**Step 2: Update each reference**

For each occurrence, apply the appropriate replacement:
- `AGENC_TMUX` env var table entry (line ~43): Remove the row entirely or change to note that `$TMUX` is the standard tmux env var (set automatically).
- `$AGENC_TMUX == 1` conditions (lines ~57, 91, 121, 151): Change to `$TMUX` is set.
- `AGENC_TMUX 1` in the `set-environment` command (line ~87): Remove that line from the sequence entirely — `createTmuxSession` no longer sets it.

**Step 3: Commit**

```bash
git add specs/tmux-mission-windows.md
git commit -m "Update tmux-mission-windows spec: remove AGENC_TMUX, use \$TMUX detection"
git pull --rebase
git push
```

---

### Task 8: Update `docs/cli/agenc_tmux_attach.md` and `docs/cli/agenc_attach.md`

**Files:**
- Modify: `docs/cli/agenc_tmux_attach.md`
- Modify: `docs/cli/agenc_attach.md`

**Step 1: Find the line in each file**

Both files have a line reading:
```
The session sets AGENC_TMUX=1 and propagates AGENC_DIRPATH so all windows
```

**Step 2: Update**

Change to:
```
The session propagates AGENC_DIRPATH so all windows
```

**Step 3: Build binary and regenerate docs (if auto-generated)**

Check whether these CLI docs are auto-generated from cobra docstrings:
```bash
grep -r "GenMarkdownTree\|GenMarkdown" cmd/ --include="*.go" -l
```

If docs are auto-generated, run `make docs` (or whatever the generation command is) after updating the cobra `Long` strings in Task 2, rather than editing these files manually. If they're manually maintained, edit them directly.

**Step 4: Commit**

```bash
git add docs/cli/agenc_tmux_attach.md docs/cli/agenc_attach.md
git commit -m "Update CLI docs: remove AGENC_TMUX=1 reference from tmux attach"
git pull --rebase
git push
```

---

### Task 9: Smoke test

**Step 1: Build the final binary**

```bash
make build
```

**Step 2: Confirm `AGENC_TMUX` is gone from the codebase**

```bash
grep -r "AGENC_TMUX" . --include="*.go"
```
Expected: no matches.

**Step 3: Verify `isInsideTmux` is used everywhere**

```bash
grep -r "isInsideAgencTmux" . --include="*.go"
```
Expected: no matches.

**Step 4: Manual smoke test (if inside tmux)**

If you are currently in a tmux session (any session — not necessarily `agenc`):
```bash
./agenc tmux window new -- echo "hello from any session"
```
Expected: a new window opens and runs the command, then closes. No "must be run inside the AgenC tmux session" error.

If you are outside tmux:
```bash
./agenc tmux window new -- echo "hello"
```
Expected: error "must be run inside a tmux session" (no mention of AGENC_TMUX).

**Step 5: Final commit (if any cleanup needed)**

If all good, no further commit needed. The work is done.
