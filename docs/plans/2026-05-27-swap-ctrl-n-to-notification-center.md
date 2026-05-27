# Swap Ctrl+N hotkey from New Mission to Notification Center

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Move the `-n C-n` (root-table Ctrl+N) tmux keybinding from the `newMission` builtin palette command to the `showNotifications` builtin. New Mission becomes palette-only (no default hotkey).

**Architecture:** Two-field move inside the `BuiltinPaletteCommands` map in `internal/config/agenc_config.go`. Tests that assert the old default keybinding on `newMission` are re-targeted to `showNotifications`. Docs and the README are updated to match. A new E2E test asserts the new default surfaces through `agenc config paletteCommand ls`.

**Tech Stack:** Go (config + tests), Bash (E2E test script), Markdown (README + docs).

**Provenance:** Designed in AgenC mission `29abbf65-eb3d-4265-9fa0-4fc0cdec2f8d` via a Double Diamond exploration. Selected approach: Option 1 (pure swap, New Mission demoted to palette-only).

**Manual verification flagged at end:** Per `agent/CLAUDE.md`, changes to keybinding generation require a live tmux session test. Listed in Task 8.

---

## Task 1: Re-target the default-keybinding assertion to showNotifications

**Files:**
- Modify: `internal/config/agenc_config_test.go:511-524`

**Step 1: Update the test to assert the new defaults**

Replace the "Check specific defaults" block in `TestPaletteCommands_BuiltinDefaults` (around lines 511-524) with:

```go
	// Check specific defaults
	foundShowNotifications := false
	foundNewMission := false
	for _, cmd := range resolved {
		if cmd.Name == "showNotifications" {
			foundShowNotifications = true
			if cmd.Title != "🔔  Notification Center" {
				t.Errorf("expected showNotifications title '🔔  Notification Center', got '%s'", cmd.Title)
			}
			if cmd.TmuxKeybinding != "-n C-n" {
				t.Errorf("expected showNotifications keybinding '-n C-n', got '%s'", cmd.TmuxKeybinding)
			}
			if !cmd.IsBuiltin {
				t.Error("expected showNotifications to be marked as builtin")
			}
		}
		if cmd.Name == "newMission" {
			foundNewMission = true
			if cmd.Title != "🚀  New Mission" {
				t.Errorf("expected newMission title '🚀  New Mission', got '%s'", cmd.Title)
			}
			if cmd.TmuxKeybinding != "" {
				t.Errorf("expected newMission to have no default keybinding, got '%s'", cmd.TmuxKeybinding)
			}
			if !cmd.IsBuiltin {
				t.Error("expected newMission to be marked as builtin")
			}
		}
	}
	if !foundShowNotifications {
		t.Error("expected showNotifications to appear in resolved commands")
	}
	if !foundNewMission {
		t.Error("expected newMission to appear in resolved commands")
	}
```

**Step 2: Run the test to verify it fails**

```bash
go test ./internal/config/ -run TestPaletteCommands_BuiltinDefaults -v
```

Expected: FAIL with messages about `showNotifications keybinding ''` (not yet `-n C-n`) and `newMission to have no default keybinding, got '-n C-n'`.

**Step 3: Do not commit yet** — Task 2 contains the implementation that makes this pass.

---

## Task 2: Swap the default keybinding in BuiltinPaletteCommands

**Files:**
- Modify: `internal/config/agenc_config.go:102-112`

**Step 1: Move the TmuxKeybinding field**

Edit `internal/config/agenc_config.go`. Change the `showNotifications` entry from:

```go
	"showNotifications": {
		Title:       StringPtr("🔔  Notification Center"),
		Description: StringPtr("Browse notifications and ENTER to attach to the linked mission"),
		Command:     StringPtr(`tmux display-popup -E -w 95% -h 90% "agenc notification manage"`),
	},
```

to:

```go
	"showNotifications": {
		Title:          StringPtr("🔔  Notification Center"),
		Description:    StringPtr("Browse notifications and ENTER to attach to the linked mission"),
		Command:        StringPtr(`tmux display-popup -E -w 95% -h 90% "agenc notification manage"`),
		TmuxKeybinding: StringPtr("-n C-n"),
	},
```

And change the `newMission` entry from:

```go
	"newMission": {
		Title:          StringPtr("🚀  New Mission"),
		Description:    StringPtr("Create a new mission and launch Claude"),
		Command:        StringPtr(`tmux display-popup -E -w 68% -h 63% "agenc mission new"`),
		TmuxKeybinding: StringPtr("-n C-n"),
	},
```

to:

```go
	"newMission": {
		Title:       StringPtr("🚀  New Mission"),
		Description: StringPtr("Create a new mission and launch Claude"),
		Command:     StringPtr(`tmux display-popup -E -w 68% -h 63% "agenc mission new"`),
	},
```

**Step 2: Run the Task 1 test to verify it now passes**

```bash
go test ./internal/config/ -run TestPaletteCommands_BuiltinDefaults -v
```

Expected: PASS.

**Step 3: Run the full config package test to spot collateral damage**

```bash
go test ./internal/config/ -v
```

Expected: failures in `TestPaletteCommands_BuiltinOverride` and `TestPaletteCommands_BuiltinClearKeybinding` (they target `newMission`'s old default). `TestPaletteCommands_KeybindingUniqueness` should still pass — it only asserts the *existence* of a duplicate error, and a custom `-n C-n` now collides with `showNotifications` instead of `newMission`. Task 3 fixes the two failing tests.

**Step 4: Do not commit yet** — Task 3 cleans up the remaining test breakage.

---

## Task 3: Re-target the override and clear-keybinding tests to showNotifications

**Files:**
- Modify: `internal/config/agenc_config_test.go:527-601`

The two affected tests demonstrate "user overrides a builtin's default keybinding" and "user clears a builtin's default keybinding." Both need a builtin that *has* a default keybinding. `newMission` no longer qualifies; `showNotifications` does.

**Step 1: Update `TestPaletteCommands_BuiltinOverride`**

Replace the body of `TestPaletteCommands_BuiltinOverride` (around lines 527-557):

```go
func TestPaletteCommands_BuiltinOverride(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, `
paletteCommands:
  showNotifications:
    tmuxKeybinding: "C-j"
`)

	cfg, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	resolved := cfg.GetResolvedPaletteCommands()
	for _, cmd := range resolved {
		if cmd.Name == "showNotifications" {
			if cmd.TmuxKeybinding != "C-j" {
				t.Errorf("expected overridden keybinding 'C-j', got '%s'", cmd.TmuxKeybinding)
			}
			// Title should keep the default
			if cmd.Title != "🔔  Notification Center" {
				t.Errorf("expected default title '🔔  Notification Center', got '%s'", cmd.Title)
			}
			if !cmd.IsBuiltin {
				t.Error("expected showNotifications to be marked as builtin")
			}
			return
		}
	}
	t.Error("showNotifications not found in resolved commands")
}
```

(Note: override value is `"C-j"` rather than `"C-n"` — keeps the test asserting that *a* keybinding override works without re-asserting the default value the test is supposed to be replacing.)

**Step 2: Update `TestPaletteCommands_BuiltinClearKeybinding`**

Replace the body of `TestPaletteCommands_BuiltinClearKeybinding` (around lines 559-601):

```go
func TestPaletteCommands_BuiltinClearKeybinding(t *testing.T) {
	tmpDir := t.TempDir()
	// showNotifications has a default keybinding of "-n C-n"; clearing it should
	// produce an empty keybinding in the resolved output.
	writeConfigYAML(t, tmpDir, `
paletteCommands:
  showNotifications:
    tmuxKeybinding: ""
`)

	cfg, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	// The override entry should persist (not be cleaned up as empty)
	override, exists := cfg.PaletteCommands["showNotifications"]
	if !exists {
		t.Fatal("expected showNotifications override to exist in config")
	}
	if override.TmuxKeybinding == nil {
		t.Fatal("expected TmuxKeybinding to be non-nil (explicitly set to empty)")
	}
	if *override.TmuxKeybinding != "" {
		t.Errorf("expected TmuxKeybinding to be empty string, got '%s'", *override.TmuxKeybinding)
	}

	// The resolved command should have no keybinding
	resolved := cfg.GetResolvedPaletteCommands()
	for _, cmd := range resolved {
		if cmd.Name == "showNotifications" {
			if cmd.TmuxKeybinding != "" {
				t.Errorf("expected cleared keybinding (empty string), got '%s'", cmd.TmuxKeybinding)
			}
			// Title should still have the builtin default
			if cmd.Title != "🔔  Notification Center" {
				t.Errorf("expected default title, got '%s'", cmd.Title)
			}
			return
		}
	}
	t.Error("showNotifications not found in resolved commands")
}
```

**Step 3: Run the full config package test to verify all green**

```bash
go test ./internal/config/ -v
```

Expected: all PASS.

**Step 4: Commit the code+test changes together**

```bash
git add internal/config/agenc_config.go internal/config/agenc_config_test.go
git commit -m "Swap Ctrl+N default keybinding from New Mission to Notification Center" -m "AgenC mission: 29abbf65-eb3d-4265-9fa0-4fc0cdec2f8d"
```

---

## Task 4: Update README hotkey list

**Files:**
- Modify: `README.md:164-168`

**Step 1: Rewrite the hotkey bullets**

The current block (around lines 164-168) is:

```markdown
- 🐚 Open a side shell in your current mission's workspace ("Side Shell" or `ctrl-p`)
  > 💡 [cmdk](https://github.com/mieubrisse/cmdk) is amazing in the side shell
- 🚀 Launch a side mission ("New Mission" or `ctrl-n`, or "Side Claude" or "Quick Claude")
- 🔀 Switch between your running missions ("Switch Mission" or `ctrl-m`)
- 💬 Send me feedback about AgenC!
```

Change to:

```markdown
- 🐚 Open a side shell in your current mission's workspace ("Side Shell" or `ctrl-p`)
  > 💡 [cmdk](https://github.com/mieubrisse/cmdk) is amazing in the side shell
- 🔔 Check your notifications ("Notification Center" or `ctrl-n`)
- 🚀 Launch a side mission ("New Mission", "Side Claude", or "Quick Claude")
- 🔀 Switch between your running missions ("Switch Mission" or `ctrl-m`)
- 💬 Send me feedback about AgenC!
```

(The Notification Center bullet is added immediately above New Mission so the hotkey list stays grouped; New Mission loses the inline `ctrl-n` reference.)

**Step 2: Skim the rest of the README for stale `ctrl-n` mentions**

```bash
grep -n "ctrl-n\|Ctrl-N\|C-n\b" README.md
```

Expected: zero remaining matches after the edit. If any remain, fix them.

**Step 3: Commit**

```bash
git add README.md
git commit -m "README: ctrl-n now opens Notification Center" -m "AgenC mission: 29abbf65-eb3d-4265-9fa0-4fc0cdec2f8d"
```

---

## Task 5: Update docs/configuration.md examples

**Files:**
- Modify: `docs/configuration.md:44-49, 178, 180`

**Step 1: Re-point the YAML override example**

The current snippet (around lines 44-49) uses `newMission` + `C-n` as the "override a builtin's keybinding" example, which is misleading now that `newMission` no longer has a default to override. Change:

```yaml
# Palette commands — customize the tmux command palette and keybindings
paletteCommands:
  # Override a builtin's keybinding
  newMission:
    tmuxKeybinding: "C-n"
```

to:

```yaml
# Palette commands — customize the tmux command palette and keybindings
paletteCommands:
  # Override a builtin's keybinding
  showNotifications:
    tmuxKeybinding: "C-j"
```

**Step 2: Re-point the CLI examples**

Around lines 178 and 180, change:

```
agenc config paletteCommand update newMission --keybinding="C-n"  # override builtin
agenc config paletteCommand rm myCmd                              # remove custom
agenc config paletteCommand rm newMission                         # restore builtin defaults
```

to:

```
agenc config paletteCommand update showNotifications --keybinding="C-j"  # override builtin
agenc config paletteCommand rm myCmd                                     # remove custom
agenc config paletteCommand rm showNotifications                         # restore builtin defaults
```

**Step 3: Skim the rest of docs/configuration.md for stale references**

```bash
grep -n "newMission\|C-n\b" docs/configuration.md
```

Expected: zero remaining matches. If any remain, fix them.

**Step 4: Commit**

```bash
git add docs/configuration.md
git commit -m "docs: re-point keybinding examples to showNotifications" -m "AgenC mission: 29abbf65-eb3d-4265-9fa0-4fc0cdec2f8d"
```

---

## Task 6: Update the Adjutant prompt's keybinding examples

**Files:**
- Modify: `internal/claudeconfig/adjutant_claude.md:56, 60`

**Step 1: Read the surrounding context**

The two references are pure syntax illustrations, not load-bearing defaults. They should still be updated so the Adjutant doesn't teach users an example that now collides with the default.

```bash
grep -n -B1 -A1 "C-n" internal/claudeconfig/adjutant_claude.md
```

**Step 2: Replace `C-n` with a non-defaulted example key**

Replace both occurrences of `C-n` with `C-j` so the illustration no longer overlaps with the new default. Use the Edit tool with `replace_all=false` per occurrence — verify the surrounding context matches before replacing.

The bare-key example on line 60 (`Bare key like \`"f"\` or \`"C-n"\``) — change the second example to `"C-j"` for consistency.

**Step 3: Commit**

```bash
git add internal/claudeconfig/adjutant_claude.md
git commit -m "adjutant prompt: avoid C-n in keybinding examples" -m "AgenC mission: 29abbf65-eb3d-4265-9fa0-4fc0cdec2f8d"
```

---

## Task 7: Add an E2E test asserting the new default surfaces through the CLI

**Files:**
- Modify: `scripts/e2e-test.sh`

**Step 1: Find the palette command test section (or the best insertion point)**

```bash
grep -n "paletteCommand\|--- " scripts/e2e-test.sh | head -40
```

Identify whichever section header is most appropriate (palette-related, or general config). If none exists, add a new section.

**Step 2: Add tests for the new defaults**

Append (under an appropriate `echo "--- ... ---"` header) the following block:

```bash
echo "--- Palette command default keybindings ---"

run_test_output_contains "paletteCommand ls shows showNotifications with C-n keybinding" \
    "showNotifications.*C-n" \
    "${agenc_test}" config paletteCommand ls

run_test "paletteCommand ls does not bind newMission to C-n" \
    1 \
    bash -c "'${agenc_test}' config paletteCommand ls | grep -E 'newMission.*C-n'"
```

The first test asserts `showNotifications` is shown alongside `C-n` in the listing. The second uses `bash -c` + `grep -E` and expects exit 1 (no match) to assert that `newMission` is NOT shown with `C-n`. Adjust the regex to whatever actual `paletteCommand ls` output format renders — run it once manually first to confirm:

```bash
make build && ./_build/agenc-test config paletteCommand ls
```

**Step 3: Run the E2E suite**

```bash
make e2e
```

Expected: all tests pass, including the two new ones. If the regex needs tuning to match the actual `paletteCommand ls` output, adjust and re-run.

**Step 4: Commit**

```bash
git add scripts/e2e-test.sh
git commit -m "E2E: assert Ctrl+N default keybinding is bound to showNotifications" -m "AgenC mission: 29abbf65-eb3d-4265-9fa0-4fc0cdec2f8d"
```

---

## Task 8: Full verification, push, manual-test handoff

**Step 1: Run the full quality gate**

```bash
make check
```

Expected: PASS. This runs gofmt/vet/lint/vulncheck/deadcode + the full test suite with race detection. Sandbox must be disabled for this command per `agent/CLAUDE.md`.

**Step 2: Re-run the E2E suite**

```bash
make e2e
```

Expected: PASS.

**Step 3: Pull-rebase and push**

```bash
git pull --rebase
git push
```

If the rebase surfaces conflicts (other missions may have pushed in the interim), resolve manually before pushing.

**Step 4: Flag manual tmux verification to the user**

Per `agent/CLAUDE.md`, changes to keybinding generation cannot be fully verified by unit or E2E tests. Tell the user:

> 🚨 **MANUAL TEST REQUIRED** 🚨
>
> 1. Run `agenc tmux inject` (or wait up to 5 minutes for the keybindings-writer loop) so the new defaults reach your live tmux config.
> 2. Inside an AgenC tmux session, press `Ctrl+N`. The Notification Center popup should open.
> 3. Press `Ctrl+Y` to open the palette, arrow to "🚀 New Mission", and confirm it still launches a new-mission popup. There should be no global hotkey for it.
> 4. If anything looks off, `agenc tmux inject` to regenerate, then re-test.

---

## Out of scope (deliberately not in this plan)

- Re-evaluating the Notification Center popup geometry (95%×90%). Flagged during design but the user confirmed it's not a concern.
- Picking a new root-table key for `newMission`. User chose palette-only.
- Updating any external docs / blog posts / Discord messages that mention `ctrl-n`. Not in this repo's surface area.
