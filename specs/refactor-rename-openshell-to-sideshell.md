Rename openShell to sideShell
===============================

**Created**: 2026-02-14
**Type**: refactor
**Status**: Open
**Related**: N/A

---

Description
-----------

Rename the `openShell` builtin palette command to `sideShell` to better reflect its behavior of opening a shell in a side pane rather than replacing the current view. This is a clean, breaking rename with no backward compatibility layer.

Context
-------

The current name "openShell" doesn't clearly communicate that the shell opens in a new tmux pane alongside the current view. Users may expect it to replace their current view or launch a new window. The new name "sideShell" explicitly conveys that a shell pane appears to the side.

Based on codebase exploration:
- Primary definition: `internal/config/agenc_config.go:134-139`
- Current config key: `openShell`
- Current display title: `🐚  Open Shell`
- Referenced in 10 files across config, tests, README, tmux, cmd, and daemon packages
- Current display order: 8th of 14 builtin commands
- Current keybinding: `-n C-p` (will remain unchanged)
- Test coverage in: `agenc_config_test.go`, `keybindings_test.go`

Design Decision
---------------

**Selected Option:** Option F — Clean Full Rename

**Quality Score:** 9.5/10

**Rationale:**

This is the cleanest technical solution that:
1. Eliminates confusion between old and new terminology
2. Keeps the codebase simple and maintainable
3. Requires minimal code changes (pure rename)
4. Has clear migration path (users rename one key in config.yml)
5. Avoids technical debt from compatibility shims

The breaking change is acceptable because:
- Only affects users who have customized `paletteCommands.openShell` in their config
- Error message will be clear and actionable
- One-line fix in user's config file
- Better to break cleanly now than maintain dual naming forever

User Stories
------------

### As a user with default config, I see "Side Shell" in the command palette

**As a** AgenC user with no config overrides,
**I want** the command palette to display "🐚  Side Shell" instead of "Open Shell",
**so that** the terminology is consistent with the new naming convention.

**Test Steps:**
1. **Setup**: Fresh AgenC install, no config.yml customizations
2. **Action**: Open command palette (prefix+a → p)
3. **Assert**: Palette shows "🐚  Side Shell" (not "Open Shell")

### As a user who customized openShell config, I get a clear error

**As a** AgenC user who has `paletteCommands.openShell` in my config.yml,
**I want** to see a clear validation error explaining the rename,
**so that** I know exactly how to fix my configuration.

**Test Steps:**
1. **Setup**: config.yml contains `paletteCommands: { openShell: { title: "My Shell" } }`
2. **Action**: Run `agenc mission ls` (or any command that loads config)
3. **Assert**: Error message clearly states "openShell" has been renamed to "sideShell"
4. **Action**: Rename `openShell:` to `sideShell:` in config.yml
5. **Assert**: Command executes successfully

### As a user, the Ctrl+P keybinding still opens a shell pane

**As a** AgenC user,
**I want** the Ctrl+P keybinding to continue opening a shell in a side pane,
**so that** my workflow is uninterrupted by the rename.

**Test Steps:**
1. **Setup**: AgenC running with tmux session
2. **Action**: Press Ctrl+P
3. **Assert**: New shell pane appears to the side (behavior unchanged)

### As a developer, I see no references to "openShell" in the codebase

**As a** developer reading the codebase,
**I want** all code, tests, and documentation to consistently use "sideShell",
**so that** there's no confusion about naming.

**Test Steps:**
1. **Action**: Run `git grep -i "openshell"` (case-insensitive)
2. **Assert**: Only matches in git history, CHANGELOG, or migration notes
3. **Assert**: No matches in active code, tests, or user-facing docs

Implementation Plan
-------------------

### Phase 1: Core Rename

- [ ] Update `internal/config/agenc_config.go:134-139`
  - Rename map key from `"openShell"` to `"sideShell"`
  - Change `Title: "🐚  Open Shell"` to `Title: "🐚  Side Shell"`
- [ ] Update `internal/config/agenc_config.go:183`
  - Change `builtinPaletteCommandOrder` array entry from `"openShell"` to `"sideShell"`
- [ ] Update `internal/config/agenc_config_test.go`
  - Search for all occurrences of `"openShell"` string literal
  - Replace with `"sideShell"` in test assertions and setup
- [ ] Update `internal/tmux/keybindings_test.go`
  - Search for all occurrences of `"openShell"` string literal
  - Replace with `"sideShell"` in test assertions

### Phase 2: Documentation and References

- [ ] Update `README.md`
  - Line 144: Change "Open Shell" to "Side Shell" in builtin commands table
  - Search for any other references to "open shell" terminology
- [ ] Search and update code comments
  - Run `git grep -i "open shell"` to find comment references
  - Update to "side shell" for consistency
- [ ] Check `docs/system-architecture.md`
  - Search for "openShell" or "open shell" references
  - Update if found (unlikely based on exploration, but verify)

### Phase 3: Validation and Testing

- [ ] Run `make test`
  - Verify all unit tests pass
  - Confirm test output shows "sideShell" not "openShell"
- [ ] Run `make build`
  - Confirm binary builds without errors
  - Verify version string includes git hash
- [ ] Manual test: Command palette display
  - Run `./agenc tmux palette`
  - Verify "🐚  Side Shell" appears in output
  - Verify it's in the correct position (8th in list)
- [ ] Manual test: Keybinding functionality
  - Start agenc session: `./agenc mission new "test rename"`
  - Press Ctrl+P within tmux
  - Verify shell pane opens to the side
- [ ] Manual test: Custom config migration
  - Create test config.yml with `paletteCommands: { openShell: { title: "Custom" } }`
  - Run `./agenc mission ls`
  - Verify error message is clear and actionable
  - Rename to `sideShell:` in config
  - Verify command succeeds

### Phase 4: Release Documentation

- [ ] Add CHANGELOG.md entry
  - Section: "Breaking Changes"
  - Document the rename with before/after example
  - Include migration instructions for users with custom configs
- [ ] Prepare release notes snippet
  - Highlight the breaking change
  - Provide exact migration steps
  - Link to relevant documentation

Technical Details
-----------------

**Modules to modify:**

1. `internal/config/agenc_config.go` (primary definition)
   - Builtin command map key: `"openShell"` → `"sideShell"`
   - Title field: `"🐚  Open Shell"` → `"🐚  Side Shell"`
   - Display order array: update entry to `"sideShell"`

2. `internal/config/agenc_config_test.go` (unit tests)
   - Test assertions checking for `"openShell"` key
   - Expected values in test tables

3. `internal/tmux/keybindings_test.go` (keybinding tests)
   - Test assertions validating openShell command presence

4. `README.md` (user documentation)
   - Line 144 and any other references to "Open Shell"

5. `docs/system-architecture.md` (if applicable)
   - Any architectural references to openShell

**Key changes:**

```go
// Before (agenc_config.go:134-139)
"openShell": {
    Title:       "🐚  Open Shell",
    Command:     tmux.OpenShellCommand,
    Keybinding:  "-n C-p",
    Category:    CategoryNavigation,
},

// After
"sideShell": {
    Title:       "🐚  Side Shell",
    Command:     tmux.OpenShellCommand,
    Keybinding:  "-n C-p",
    Category:    CategoryNavigation,
},
```

```go
// Before (agenc_config.go:183)
builtinPaletteCommandOrder = []string{
    // ...
    "openShell",
    // ...
}

// After
builtinPaletteCommandOrder = []string{
    // ...
    "sideShell",
    // ...
}
```

**Files NOT modified:**
- `internal/tmux/tmux.go` — `OpenShellCommand` constant name unchanged (internal implementation detail)
- All keybinding values (`-n C-p`) — unchanged
- All command execution logic — unchanged

**Dependencies:** None — pure rename, no new libraries or external changes

**Breaking Change Impact:**

Users who have customized the palette command in their `~/.agenc/config.yml`:

```yaml
# Before (breaks after upgrade)
paletteCommands:
  openShell:
    title: "My Custom Shell"
    keybinding: "-n C-s"

# After (required fix)
paletteCommands:
  sideShell:
    title: "My Custom Shell"
    keybinding: "-n C-s"
```

Config validation will fail with error message pointing to the unknown key `openShell`.

Testing Strategy
----------------

**Unit tests:**

1. Verify `sideShell` key exists in builtin map
   - Test: `agenc_config_test.go` — assert builtin map contains `"sideShell"`
   - Test: `agenc_config_test.go` — assert builtin map does NOT contain `"openShell"`

2. Verify display order includes `sideShell`
   - Test: `agenc_config_test.go` — assert `builtinPaletteCommandOrder[7] == "sideShell"`

3. Verify title is correct
   - Test: `agenc_config_test.go` — assert `builtins["sideShell"].Title == "🐚  Side Shell"`

4. Verify keybinding is preserved
   - Test: `keybindings_test.go` — assert `sideShell` command has `-n C-p` keybinding

**Integration tests:**

1. Command palette includes "Side Shell"
   - Run `./agenc tmux palette` and capture output
   - Assert output contains "🐚  Side Shell"
   - Assert output does NOT contain "🐚  Open Shell"

2. Keybinding executes correct command
   - Start tmux session
   - Simulate Ctrl+P keypress
   - Verify `OpenShellCommand` is executed

**Manual testing checklist:**

- [ ] Fresh install: palette shows "Side Shell"
- [ ] Ctrl+P opens shell pane (functionality unchanged)
- [ ] Invalid config (with `openShell:`) shows clear error
- [ ] Valid migrated config (with `sideShell:`) works correctly
- [ ] `make test` passes all tests
- [ ] `make build` produces working binary
- [ ] README.md displays correct command name
- [ ] No grep matches for "openShell" in active code (only in history/changelog)

Acceptance Criteria
-------------------

- [ ] **Config key renamed:** `internal/config/agenc_config.go` uses `"sideShell"` as map key
- [ ] **Display title updated:** Builtin definition shows `Title: "🐚  Side Shell"`
- [ ] **Display order updated:** `builtinPaletteCommandOrder` contains `"sideShell"` at position 7
- [ ] **All tests pass:** `make test` exits with code 0
- [ ] **Binary builds:** `make build` completes successfully
- [ ] **README updated:** `README.md` shows "Side Shell" in builtin commands table
- [ ] **No legacy references:** `git grep "openShell"` returns no matches in active code (only in git history, CHANGELOG, or migration docs)
- [ ] **Keybinding works:** Ctrl+P keybinding still executes shell pane command
- [ ] **Command palette displays correctly:** Running palette shows "🐚  Side Shell" in 8th position
- [ ] **Clear error on old config:** Invalid config with `openShell:` produces actionable error message
- [ ] **Documentation complete:** CHANGELOG and release notes include breaking change with migration steps

Risks and Considerations
-------------------------

**Risk:** Users with custom configs experience breakage

**Likelihood:** Medium — some users may have customized this command

**Impact:** Low — one-line config fix

**Mitigation:**
- Config validation produces clear error message: "Unknown palette command 'openShell'. Did you mean 'sideShell'?"
- CHANGELOG entry with before/after example
- Release notes prominently feature the breaking change
- Migration instructions in upgrade documentation

**Risk:** Tests rely on "openShell" string literals

**Likelihood:** High — confirmed in exploration (2 test files reference it)

**Impact:** High — tests will fail if not updated

**Mitigation:**
- Comprehensive grep for all test references: `git grep -n "openShell" '*.go'`
- Update all test assertions in same commit as config change
- Run full test suite (`make test`) before committing
- Include test output verification in implementation checklist

**Risk:** Third-party integrations reference "openShell"

**Likelihood:** Very Low — this is an internal command name, not a public API

**Impact:** Low — would only affect users who wrote custom scripts parsing palette output

**Mitigation:**
- Note in CHANGELOG that this affects internal command naming
- Public behavior (keybinding, shell opening) unchanged

**Risk:** Incomplete grep misses edge case references

**Likelihood:** Low — using multiple search strategies

**Impact:** Medium — leaves inconsistent naming in codebase

**Mitigation:**
- Use multiple search patterns:
  - `git grep "openShell"` (exact match)
  - `git grep -i "open shell"` (case-insensitive phrase)
  - `git grep "open.*shell"` (regex pattern)
- Search in comments as well as code
- Manual review of search results

Implementation Notes
--------------------

**Search commands for implementation:**

```bash
# Find all code references
git grep -n "openShell"

# Find all case-insensitive references
git grep -in "open shell"

# Find in comments and strings
git grep -n "open.*shell"

# Verify no references remain (run after changes)
git grep "openShell" -- '*.go' '*.md'
```

**Expected file modifications (exact line numbers may shift):**

1. `internal/config/agenc_config.go:134` — map key
2. `internal/config/agenc_config.go:135` — title string
3. `internal/config/agenc_config.go:183` — display order array
4. `internal/config/agenc_config_test.go` — multiple test assertions
5. `internal/tmux/keybindings_test.go` — test assertions
6. `README.md:144` — user documentation table

**Test execution order:**

1. Make code changes
2. Run `make test` to catch test failures early
3. Update failing tests
4. Re-run `make test` until all pass
5. Run `make build` to verify compilation
6. Manual testing with `./agenc tmux palette`
7. Final verification: `git grep "openShell"` returns no active code matches

**Commit strategy:**

Single atomic commit containing:
- All code changes (config, tests)
- All documentation changes (README)
- CHANGELOG entry

Commit message:
```
Rename openShell to sideShell palette command

Breaking change: the builtin palette command "openShell" has been
renamed to "sideShell" to better reflect its behavior of opening
a shell in a side pane.

Users who have customized paletteCommands.openShell in their config
must rename it to paletteCommands.sideShell.

- Update builtin definition in agenc_config.go
- Update display order array
- Update all test references
- Update README.md documentation
- Add CHANGELOG entry with migration instructions
```
