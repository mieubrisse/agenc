Remove `agenc tmux window new` — Implementation Plan
=====================================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove the `agenc tmux window new` command, the `AGENC_WINDOW_NAME` env var, and migrate all callers to vanilla tmux with UUID-based path construction.

**Architecture:** All palette commands that need a working directory derive it deterministically from `$AGENC_CALLING_MISSION_UUID` instead of relying on `#{pane_current_path}`. The `agenc tmux window new` wrapper and its `AGENC_WINDOW_NAME` env var are deleted entirely. `buildShellCommand()` moves to `cmd/tmux_pane_new.go` since that's the only remaining consumer.

**Tech Stack:** Go (Cobra CLI), tmux, shell commands

**Design doc:** `docs/plans/2026-02-28-remove-agenc-tmux-window-new-design.md`

---

### Task 1: Migrate palette commands in `agenc_config.go`

**Files:**
- Modify: `internal/config/agenc_config.go:123-164`

**Step 1: Update the four palette command strings**

In `internal/config/agenc_config.go`, change the `defaultPaletteCommands` map entries:

```go
"sideShell": {
    Title:          "🐚  Side Shell",
    Description:    "Split pane and open a shell in the current mission's workspace",
    Command:        `tmux split-window -h -c "${AGENC_DIRPATH:-$HOME/.agenc}/missions/$AGENC_CALLING_MISSION_UUID/agent" $SHELL`,
    TmuxKeybinding: "-n C-p",
},
"shell": {
    Title:       "🐚  Shell",
    Description: "Open a shell in a new window",
    Command:     `tmux new-window -a -c "${AGENC_DIRPATH:-$HOME/.agenc}/missions/$AGENC_CALLING_MISSION_UUID/agent" $SHELL`,
},
"removeMission": {
    Title:       "❌  Remove Mission",
    Description: "Remove a mission and its directory",
    Command:     "tmux new-window -a agenc mission rm",
},
"nukeMissions": {
    Title:       "💥  Nuke Missions",
    Description: "Remove all archived missions",
    Command:     "tmux new-window -a agenc mission nuke",
},
```

**Step 2: Run tests**

Run: `make check`

Expected: All tests pass. The `TestIsMissionScoped` tests should still pass — `shell` and `sideShell` now contain `AGENC_CALLING_MISSION_UUID` so `IsMissionScoped()` returns true for them.

**Step 3: Commit**

```
git add internal/config/agenc_config.go
git commit -m "Migrate palette commands to vanilla tmux with UUID-based paths"
```

---

### Task 2: Update `agenc_config_test.go`

**Files:**
- Modify: `internal/config/agenc_config_test.go:480-602`

**Step 1: Update test fixtures that reference `agenc tmux window new`**

The `TestPaletteCommands_CustomWithKeybinding` test (line 487) uses `agenc tmux window new` in its YAML fixture. This is a user-defined custom command, not a builtin — the string is just test data and doesn't need to match builtins. Leave it as-is since users can put any command string they want.

The `TestPaletteCommands_RoundTrip` test (lines 562-602) also uses `agenc tmux window new` in its test fixture data. Same reasoning — leave as-is since this tests round-trip serialization of arbitrary command strings.

Verify: read both tests to confirm they are testing user-defined commands, not builtin defaults.

**Step 2: Run tests**

Run: `make check`

Expected: PASS. No test changes needed since these tests use arbitrary user-defined command strings, not builtin defaults.

**Step 3: Commit (skip if no changes)**

---

### Task 3: Update `agenc feedback` command

**Files:**
- Modify: `cmd/feedback.go:16-35`

**Step 1: Change feedback to use vanilla tmux**

Replace the entire `RunE` function body and update the `Long` description:

```go
var feedbackCmd = &cobra.Command{
	Use:   feedbackCmdStr,
	Short: "Launch a feedback mission with Adjutant",
	Long: `Launches a new tmux window with an Adjutant mission for sending feedback about AgenC.
This is a shorthand for:
  tmux new-window -a agenc mission new --adjutant --prompt "I'd like to send feedback about AgenC"`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		feedbackCmd := exec.Command("tmux", "new-window", "-a",
			"agenc", "mission", "new",
			"--adjutant",
			"--prompt", feedbackPrompt,
		)
		feedbackCmd.Stdout = os.Stdout
		feedbackCmd.Stderr = os.Stderr

		if err := feedbackCmd.Run(); err != nil {
			return stacktrace.Propagate(err, "failed to launch feedback mission")
		}
		return nil
	},
}
```

Note: This removes the dependency on `tmuxCmdStr`, `windowCmdStr`, `newCmdStr`, `agencCmdStr`, and `missionCmdStr` constants from `feedback.go`. Verify these constants are still used elsewhere before removing them — they likely are (they're cmd string constants used across the package).

**Step 2: Run tests**

Run: `make check`

Expected: PASS.

**Step 3: Commit**

```
git add cmd/feedback.go
git commit -m "Use vanilla tmux in agenc feedback command"
```

---

### Task 4: Move `buildShellCommand()` to `tmux_pane_new.go`

**Files:**
- Modify: `cmd/tmux_pane_new.go`
- Delete: (not yet — just moving the function in this task)

**Step 1: Copy `buildShellCommand` and `shellQuote` to `cmd/tmux_pane_new.go`**

Add at the bottom of `cmd/tmux_pane_new.go`:

```go
// buildShellCommand joins command arguments into a single shell command string,
// quoting arguments that contain spaces or special characters.
func buildShellCommand(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		if strings.ContainsAny(arg, " \t\n\"'\\$`|&;(){}[]<>?*~!#") {
			// Use single quotes, escaping any existing single quotes
			quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
		} else {
			quoted[i] = arg
		}
	}
	return strings.Join(quoted, " ")
}
```

Add `"strings"` to the import block in `tmux_pane_new.go`.

Also check if `shellQuote` (used in `tmux_window_new.go:75`) is needed anywhere else. If not, don't move it.

**Step 2: Update the comment in `tmux_pane_new.go:53-54`**

Remove the cross-reference:

```go
	// Pass the user's command directly (no shell wrapping) so the shell can
	// exec into it.
```

(Remove the "See tmux_window_new.go for the rationale." part.)

**Step 3: Run tests (should fail — duplicate function)**

Run: `make check`

Expected: Build failure due to `buildShellCommand` being defined in both files. This confirms the function exists in both places. (If it passes, the old file was already removed — skip to Task 5.)

**Step 4: Commit (don't commit yet — wait for Task 5 to delete the old file)**

---

### Task 5: Delete `agenc tmux window new` command files

**Files:**
- Delete: `cmd/tmux_window_new.go`
- Delete: `cmd/tmux_window.go`
- Delete: `docs/cli/agenc_tmux_window_new.md`
- Delete: `docs/cli/agenc_tmux_window.md`

**Step 1: Verify `tmuxWindowCmd` has no other subcommands**

Search for `tmuxWindowCmd.AddCommand` — should only appear in `tmux_window_new.go:47`. After deleting that file, the parent command has no subcommands.

**Step 2: Check if `shellQuote` is used outside `tmux_window_new.go`**

Search for `shellQuote` in the `cmd/` package. If it's only in `tmux_window_new.go`, it's safe to delete with the file. If used elsewhere, move it.

**Step 3: Delete the files**

```
rm cmd/tmux_window_new.go
rm cmd/tmux_window.go
rm docs/cli/agenc_tmux_window_new.md
rm docs/cli/agenc_tmux_window.md
```

**Step 4: Run tests**

Run: `make check`

Expected: PASS. The build should succeed because `buildShellCommand` now lives in `tmux_pane_new.go` (from Task 4), and `tmuxWindowCmd` is no longer referenced.

If build fails because something still references `tmuxWindowCmd` or `tmuxWindowNewCmd`, investigate and fix.

**Step 5: Commit (combined with Task 4)**

```
git add cmd/tmux_pane_new.go
git add -u cmd/tmux_window_new.go cmd/tmux_window.go docs/cli/agenc_tmux_window_new.md docs/cli/agenc_tmux_window.md
git commit -m "Remove agenc tmux window new command and parent group"
```

---

### Task 6: Remove `AGENC_WINDOW_NAME` from wrapper

**Files:**
- Modify: `internal/wrapper/tmux.go:24-57,170-204`

**Step 1: Update `renameWindowForTmux` doc comment and body**

Replace lines 24-57 with:

```go
// renameWindowForTmux renames the current tmux window when running inside
// any tmux session. Priority order (highest to lowest):
//  1. windowTitle from config.yml
//  2. repo short name
//  3. mission ID
//
// Only renames the window if this pane is the sole pane in the window and the
// user has not manually renamed the window since the last AgenC-managed rename.
// In regular tmux sessions or outside tmux, this is a no-op.
func (w *Wrapper) renameWindowForTmux() {
	if os.Getenv("TMUX") == "" {
		return
	}

	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		return
	}

	title := w.missionID
	if w.gitRepoName != "" {
		repoName := extractRepoName(w.gitRepoName)
		if repoName != "" {
			title = repoName
		}
	}
	if w.windowTitle != "" {
		title = w.windowTitle
	}

	w.applyWindowTitle(paneID, title)
}
```

**Step 2: Update `updateWindowTitleFromSession` doc comment and body**

Replace the doc comment (lines 170-178) with:

```go
// updateWindowTitleFromSession updates the tmux window title based on the best
// available name for this mission. Priority order (highest to lowest):
//  1. Custom title from Claude's /rename command
//  2. AI-generated summary from daemon (updated every ~10 user prompts)
//  3. Auto-generated session name from Claude's session metadata
//
// Only runs inside a tmux session. Called on each Stop event so the title
// stays in sync as the session evolves.
```

Remove the `AGENC_WINDOW_NAME` early-return block (lines 200-204):

```go
	// If an explicit --name was provided at launch, treat it as a fixed title.
	// Don't update it with AI summaries or session names (only /rename can override).
	if os.Getenv("AGENC_WINDOW_NAME") != "" {
		return
	}
```

**Step 3: Run tests**

Run: `make check`

Expected: PASS.

**Step 4: Commit**

```
git add internal/wrapper/tmux.go
git commit -m "Remove AGENC_WINDOW_NAME env var from wrapper title system"
```

---

### Task 7: Update documentation

**Files:**
- Modify: `docs/system-architecture.md:413`
- Modify: `internal/claudeconfig/adjutant_claude.md:31`
- Modify: `docs/cli/agenc_feedback.md:9`
- Modify: `docs/configuration.md:53,65,144`
- Modify: `README.md:162`
- Modify: `internal/claudeconfig/prime_content.md` (if it references `agenc tmux window new`)
- Modify: `internal/claudeconfig/agenc_usage_skill.md` (if it references `agenc tmux window new`)

**Step 1: Update `docs/system-architecture.md:413`**

Change the `tmux.go` description to remove `AGENC_WINDOW_NAME`:

```
- `tmux.go` — tmux window renaming when inside any tmux session (`$TMUX` set) (startup: config.yml `windowTitle` > repo short name > mission ID; dynamic on Stop events: custom title from /rename > AI summary from server > auto-generated session name), pane color management (`setWindowBusy`, `setWindowNeedsAttention`, `resetWindowTabStyle`) for visual mission status feedback, pane registration/clearing via server client for mission resolution
```

**Step 2: Update `internal/claudeconfig/adjutant_claude.md:31`**

Remove the line about not wrapping commands with `agenc tmux window new`:

```
Use `agenc mission new` and `agenc mission resume` directly. The server handles creating tmux windows automatically.
```

**Step 3: Update `docs/cli/agenc_feedback.md:9`**

Change the example from `agenc tmux window new -a -- agenc mission new ...` to `tmux new-window -a agenc mission new ...`.

**Step 4: Update `docs/configuration.md`**

Update the palette command examples (lines 53, 65) to use vanilla tmux commands instead of `agenc tmux window new`. Update line 144 to use a different example command.

**Step 5: Update `README.md:162`**

Change the palette command example to use a vanilla tmux or `agenc mission new` command instead of `agenc tmux window new`.

**Step 6: Check and update `prime_content.md` and `agenc_usage_skill.md`**

Search these files for `agenc tmux window new` references and update if found.

**Step 7: Run tests**

Run: `make check`

Expected: PASS.

**Step 8: Regenerate CLI docs**

Run: `make build` (which runs gendocs as part of the build).

Check if `docs/cli/agenc_tmux_window.md` or `docs/cli/agenc_tmux_window_new.md` got regenerated. If so, delete them again. The gendocs tool should no longer generate them since the commands are gone.

**Step 9: Commit**

```
git add docs/ internal/claudeconfig/ README.md
git commit -m "Update docs to remove agenc tmux window new references"
```

---

### Task 8: Final verification

**Step 1: Full grep for lingering references**

Search the entire codebase (excluding `specs/ARCHIVE/`, `docs/plans/`, and `.beads/`) for:
- `agenc tmux window new`
- `AGENC_WINDOW_NAME`
- `tmuxWindowNewCmd`
- `tmuxWindowCmd`
- `tmux_window_new`

Any remaining references in active code should be updated or removed.

**Step 2: Full build and test**

Run: `make build`

Expected: Binary builds, all tests pass, CLI docs regenerate cleanly.

**Step 3: Smoke test the binary**

Run: `./agenc tmux window new --help`

Expected: Error — unknown command (confirms the command is gone).

Run: `./agenc tmux pane new --help`

Expected: Shows help (confirms pane new still works and `buildShellCommand` moved correctly).

**Step 4: Commit any stragglers, then push**

```
git push
```
