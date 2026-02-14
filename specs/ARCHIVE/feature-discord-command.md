Discord Command Implementation Specification
=============================================

**Created**: 2026-02-14
**Type**: feature
**Status**: Open

---

Description
-----------

Implement a new `agenc discord` CLI command that opens the AgenC Discord community server (https://discord.gg/x9Y8Se4XF3) in the user's browser, and add a corresponding "👾 Join the Discord" entry to the command palette for discoverability.

This feature provides users with quick access to the AgenC community for support, discussions, and collaboration.

Context
-------

The user requested an easy way for AgenC users to join the Discord community. After evaluating multiple design options:

1. **Palette-only entry** (Quality: 7.5/10) - Minimal implementation, matches sendFeedback pattern, but limited CLI discoverability
2. **CLI Command + Palette Entry** (Quality: 9/10) - Complete solution with dual access paths
3. **Unified Outreach Command** (Quality: 6.5/10) - Over-engineered with parent command structure
4. **Hybrid CLI with Browser Abstraction** (Quality: 8/10) - Cross-platform but unnecessary for macOS-focused codebase

Option 2 was selected as it provides the best user experience by offering both direct CLI access for power users and palette discoverability for all users, while maintaining simplicity and following existing codebase patterns.

Design Decision
---------------

**Selected: Option 2 - CLI Command + Palette Entry** (Quality Score: 9/10)

**Quality Score Breakdown:**
- Design alignment (2/2): Perfect match with version.go pattern
- Best practices (2/2): Follows Cobra conventions, discoverable
- Maintainability (2/2): Clear separation of concerns
- Security (2/2): No security concerns
- Test coverage (1/1): Easily testable via command execution
- Sustainability (0/1): Slight overhead for very simple functionality

**Why this beats alternatives:**
- Palette-only: Hidden from `agenc --help`, not scriptable, harder to test
- Outreach command: Over-engineered for a single Discord link
- Browser abstraction: Unnecessary complexity for macOS-only current scope

User Stories
------------

### CLI Discord Access

**As a** command-line power user, **I want** to run `agenc discord` from my terminal, **so that** I can quickly join the Discord community without leaving my workflow.

**Test Steps:**

1. **Setup**: Have `agenc` binary built and in PATH, Chrome browser installed
2. **Action**: Run `agenc discord` from the terminal
3. **Assert**:
   - Command executes without error
   - Chrome browser opens automatically
   - Discord invite link (https://discord.gg/x9Y8Se4XF3) loads in a new tab/window

### Palette Discord Access

**As a** tmux user, **I want** to find "👾 Join the Discord" in the command palette, **so that** I can discover and join the community while browsing available commands.

**Test Steps:**

1. **Setup**: Have AgenC tmux integration active with palette injected
2. **Action**: Open the command palette (default keybinding or `agenc tmux palette`)
3. **Assert**:
   - "👾 Join the Discord" appears in the palette list
   - Entry shows description "Join the AgenC Discord community"
   - Selecting the entry opens Discord invite in Chrome

### Help Text Discoverability

**As a** new AgenC user, **I want** to see the discord command in `agenc --help`, **so that** I know the community resource exists.

**Test Steps:**

1. **Setup**: Fresh AgenC installation
2. **Action**: Run `agenc --help`
3. **Assert**:
   - `discord` appears in the available commands list
   - Short description indicates it opens the Discord community

Implementation Plan
-------------------

### Phase 1: Add Command Constant

- [ ] Add `discordCmdStr = "discord"` constant to `cmd/command_str_consts.go`
  - Insert in alphabetical order in the "Top-level commands" section (after `daemonCmdStr`, before `doctorCmdStr`)

### Phase 2: Create CLI Command

- [ ] Create `cmd/discord.go` file
  - Define `discordCmd` as a `*cobra.Command`
  - Set `Use: discordCmdStr`
  - Set `Short: "Open the AgenC Discord community in your browser"`
  - Set `Long:` with full description and URL
  - Implement `RunE` function to execute browser open command using `exec.Command`
  - Add command to rootCmd in `init()` function

### Phase 3: Command Palette Integration

- [ ] Modify `internal/config/agenc_config.go`
  - Add `"joinDiscord"` entry to `BuiltinPaletteCommands` map (lines 93-171)
    - Title: `"👾  Join the Discord"`
    - Description: `"Join the AgenC Discord community"`
    - Command: `"agenc discord"`
  - Add `"joinDiscord"` to `builtinPaletteCommandOrder` array (lines 173-190)
    - Insert after `"sendFeedback"` at the end of the list

### Phase 4: Verification

- [ ] Build the binary with `make build`
- [ ] Test `./agenc discord` command execution
- [ ] Test `./agenc --help` includes discord command
- [ ] Test palette entry appears with `./agenc config paletteCommand ls`
- [ ] Verify browser opens to correct Discord URL

Technical Details
-----------------

**Files to create:**
- `cmd/discord.go`

**Files to modify:**
- `cmd/command_str_consts.go`
  - Line ~21 (between `daemonCmdStr` and `doctorCmdStr`)
- `internal/config/agenc_config.go`
  - Lines 95-171: Add new entry to `BuiltinPaletteCommands` map
  - Lines 173-190: Add `"joinDiscord"` to `builtinPaletteCommandOrder` array

**Key implementation notes:**
- Browser command: `open -a 'Google Chrome' https://discord.gg/x9Y8Se4XF3`
- Use `exec.Command("open", "-a", "Google Chrome", url)` in Go
- No need for user confirmation - command intent is explicit
- Command should execute synchronously and exit after launching browser
- Discord URL: https://discord.gg/x9Y8Se4XF3 (hardcoded as stable invite link)

**Code pattern reference:**
The implementation should closely mirror `cmd/version.go`:
- Simple cobra command with no subcommands or flags
- Direct execution in RunE function
- Minimal error handling (browser launch failures are non-critical)

**Emoji selection rationale:**
`👾` (space invader) chosen for Discord to:
- Align with gaming/community theme
- Distinguish from existing emojis (🦀🤖🚀🐚🛑💬🔧🔄❌💥🔀🟢)
- Provide visual interest in palette

Testing Strategy
----------------

**Manual testing:**
1. Build: `make build`
2. CLI test: `./agenc discord` → verify Chrome opens to Discord invite
3. Help test: `./agenc --help` → verify discord command listed
4. Palette test: `./agenc config paletteCommand ls` → verify "joinDiscord" appears
5. Tmux test (if tmux integration active): Open palette → verify "👾 Join the Discord" entry

**Unit tests:**
- Not required for this simple command
- Could test command registration exists
- Browser launch is external - testing would require mocking

**Integration testing:**
- Verify command appears in `agenc --help` output
- Verify palette command references valid CLI command
- Verify palette ordering places joinDiscord after sendFeedback

Acceptance Criteria
-------------------

- [ ] Running `./agenc discord` opens Chrome browser to https://discord.gg/x9Y8Se4XF3
- [ ] `./agenc --help` lists the discord command with appropriate short description
- [ ] `./agenc config paletteCommand ls` shows "👾 Join the Discord" entry
- [ ] Palette entry executes `agenc discord` when selected
- [ ] No errors or warnings during command execution
- [ ] Code follows existing patterns (uses command constants, proper cobra structure)
- [ ] Changes committed and pushed to repository

Risks & Considerations
----------------------

**Browser dependency:**
- Command assumes Chrome is installed and available
- CLAUDE.md indicates `open -a 'Google Chrome'` is the standard pattern
- If Chrome is not installed, macOS will show an error - acceptable failure mode
- Future: Could fall back to default browser with `open <url>` if Chrome fails

**Discord URL stability:**
- Invite link https://discord.gg/x9Y8Se4XF3 is assumed permanent
- If URL changes, command will need code update
- Alternative: Make URL configurable in config.yml (likely overkill for single static URL)

**Command namespace:**
- Adding `discord` to top-level commands is permanent API surface
- Low risk - straightforward naming, unlikely to conflict with future features
- Consistent with other top-level utility commands (version, login, doctor)

**Palette growth:**
- Adding one more entry increases palette length
- Currently 14 entries, adding one more (15 total) is reasonable
- Placement at end (after sendFeedback) is logical grouping with other meta-commands

**Future enhancements:**
- Could add `--browser` flag to specify browser (Chrome, Firefox, Safari, default)
- Could add similar commands for other community resources (GitHub, docs site, etc.)
- Could track analytics on Discord command usage (requires telemetry infrastructure)
