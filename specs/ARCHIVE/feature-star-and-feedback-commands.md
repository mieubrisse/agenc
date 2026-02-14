# Star and Feedback Commands

**Created**: 2026-02-14
**Type**: feature
**Status**: Open
**Related**: N/A

---

## Description

Add two new CLI commands and one palette command to improve user engagement and feedback collection:

1. `agenc star` - Opens browser to AgenC GitHub repository for easy starring
2. `agenc feedback` - Shorthand for launching Adjutant feedback mission in new tmux window
3. "⭐ Star AgenC on Github" - Command palette entry that invokes `agenc star`

All implementations must use cmdStr constants from `cmd/command_str_consts.go` rather than hardcoded command strings.

## Context

Users currently need to remember long command sequences to perform common actions:

- Starring the repo requires manually navigating to GitHub
- Sending feedback requires typing the full `agenc tmux window new -a -- agenc mission new --adjutant --prompt "I'd like to send feedback about AgenC"` command

These new commands provide simple shortcuts that improve discoverability and reduce friction for common user actions.

## Design Decision

**Selected: Option 4 - Recursive Exec** (Quality Score: 8/10)

This option was chosen because:

1. **Follows existing patterns** - Matches the recursive exec pattern already used in `cron_run.go` for launching tmux sessions
2. **Proper separation of concerns** - `agenc star` handles browser opening directly; `agenc feedback` delegates to existing tmux/mission infrastructure via recursive exec
3. **Maintainable** - Reuses existing command constants and doesn't require new shared libraries
4. **Testable** - Simple command implementations with clear success/failure modes
5. **User requirement satisfied** - Uses cmdStr string constants throughout, avoiding hardcoded command names

**How it works:**

- `agenc star`: Direct execution using `exec.Command("open", url)` to open browser
- `agenc feedback`: Recursive exec using `exec.Command(os.Args[0], tmuxCmdStr, windowCmdStr, newCmdStr, ...)` to build the full tmux/mission command
- Palette command: Shell invocation of `agenc star` as a simple wrapper

## User Stories

### Story 1: User wants to star the GitHub repo

**As a** user,
**I want** to quickly open the AgenC GitHub repository,
**so that** I can star it or browse the code.

**Test Steps:**
1. **Setup**: Terminal with agenc installed
2. **Action**: Run `agenc star`
3. **Assert**: Browser opens to https://github.com/mieubrisse/agenc

### Story 2: User wants to star via command palette

**As a** user,
**I want** a palette command to star the repo,
**so that** I can access it from my tmux workflow without leaving my session.

**Test Steps:**
1. **Setup**: Inside an agenc tmux session with command palette available
2. **Action**: Open command palette, select "⭐ Star AgenC on Github"
3. **Assert**: Browser opens to GitHub repo

### Story 3: User wants to send feedback

**As a** user,
**I want** a quick shorthand command to send feedback,
**so that** I don't have to remember the full tmux/mission incantation.

**Test Steps:**
1. **Setup**: Terminal with agenc installed, inside a tmux session
2. **Action**: Run `agenc feedback`
3. **Assert**: New tmux window opens with Adjutant mission prompting for feedback

## Implementation Plan

### Phase 1: Add Command String Constants

**File**: `cmd/command_str_consts.go`

- [ ] Add `starCmdStr = "star"` constant
- [ ] Add `feedbackCmdStr = "feedback"` constant

### Phase 2: Implement `agenc star`

**File**: `cmd/star.go` (new file)

- [ ] Create `starCmd` using `&cobra.Command{}`
- [ ] Set `Use: starCmdStr`
- [ ] Set `Short: "Open the AgenC GitHub repository in your browser"`
- [ ] Set `Long:` with detailed description
- [ ] Set `Args: cobra.NoArgs` to reject unexpected arguments
- [ ] Implement `RunE` function that:
  - Calls `exec.Command("open", "https://github.com/mieubrisse/agenc").Run()`
  - Returns errors via `stacktrace.Propagate(err, "failed to open browser to GitHub repository")`
- [ ] Add `init()` function that calls `rootCmd.AddCommand(starCmd)`

### Phase 3: Implement `agenc feedback`

**File**: `cmd/feedback.go` (new file)

- [ ] Create `feedbackCmd` using `&cobra.Command{}`
- [ ] Set `Use: feedbackCmdStr`
- [ ] Set `Short: "Launch a feedback mission with Adjutant"`
- [ ] Set `Long:` with detailed description
- [ ] Set `Args: cobra.NoArgs` to reject unexpected arguments
- [ ] Implement `RunE` function that:
  - Builds command args slice: `[]string{tmuxCmdStr, windowCmdStr, newCmdStr, "-a", "--", agencCmdStr, missionCmdStr, newCmdStr, "--adjutant", "--prompt", "I'd like to send feedback about AgenC"}`
  - Note: `agencCmdStr` is the first arg after `os.Args[0]`, so the full command becomes: `agenc tmux window new -a -- agenc mission new --adjutant --prompt "..."`
  - Calls `exec.Command(os.Args[0], args...).Run()`
  - Returns errors via `stacktrace.Propagate(err, "failed to launch feedback mission")`
- [ ] Add `init()` function that calls `rootCmd.AddCommand(feedbackCmd)`

### Phase 4: Add Palette Command

**File**: `internal/config/agenc_config.go`

- [ ] Add new entry to `BuiltinPaletteCommands` map:
  ```go
  "starAgenc": {
      Title:       "⭐ Star AgenC on Github",
      Description: "Open the AgenC GitHub repository in your browser",
      Command:     "agenc star",
  }
  ```

### Phase 5: Testing

- [ ] Test `agenc star` manually - verify browser opens to correct URL
- [ ] Test `agenc feedback` manually - verify tmux window opens with Adjutant mission
- [ ] Test palette "⭐ Star AgenC on Github" - verify browser opens
- [ ] Test error case: `agenc star extraarg` - should reject with "unknown command" error
- [ ] Test error case: `agenc feedback extraarg` - should reject with "unknown command" error
- [ ] Test error case: Run `agenc star` when `open` command unavailable - should propagate clear error

## Technical Details

### Files to Create

**`cmd/star.go`**
```go
package cmd

import (
    "os/exec"
    "github.com/mieubrisse/stacktrace"
    "github.com/spf13/cobra"
)

const githubRepoURL = "https://github.com/mieubrisse/agenc"

var starCmd = &cobra.Command{
    Use:   starCmdStr,
    Short: "Open the AgenC GitHub repository in your browser",
    Long: `Opens the AgenC GitHub repository in your default browser.
This makes it easy to star the project, browse the code, or file issues.`,
    Args: cobra.NoArgs,
    RunE: func(cmd *cobra.Command, args []string) error {
        if err := exec.Command("open", githubRepoURL).Run(); err != nil {
            return stacktrace.Propagate(err, "failed to open browser to GitHub repository")
        }
        return nil
    },
}

func init() {
    rootCmd.AddCommand(starCmd)
}
```

**`cmd/feedback.go`**
```go
package cmd

import (
    "os"
    "os/exec"
    "github.com/mieubrisse/stacktrace"
    "github.com/spf13/cobra"
)

const feedbackPrompt = "I'd like to send feedback about AgenC"

var feedbackCmd = &cobra.Command{
    Use:   feedbackCmdStr,
    Short: "Launch a feedback mission with Adjutant",
    Long: `Launches a new tmux window with an Adjutant mission for sending feedback about AgenC.
This is a shorthand for:
  agenc tmux window new -a -- agenc mission new --adjutant --prompt "I'd like to send feedback about AgenC"`,
    Args: cobra.NoArgs,
    RunE: func(cmd *cobra.Command, args []string) error {
        cmdArgs := []string{
            tmuxCmdStr,
            windowCmdStr,
            newCmdStr,
            "-a",
            "--",
            agencCmdStr,
            missionCmdStr,
            newCmdStr,
            "--adjutant",
            "--prompt",
            feedbackPrompt,
        }

        if err := exec.Command(os.Args[0], cmdArgs...).Run(); err != nil {
            return stacktrace.Propagate(err, "failed to launch feedback mission")
        }
        return nil
    },
}

func init() {
    rootCmd.AddCommand(feedbackCmd)
}
```

### Files to Modify

**`cmd/command_str_consts.go`**

Add these constants to the existing file:
```go
const (
    // ... existing constants ...
    starCmdStr     = "star"
    feedbackCmdStr = "feedback"
)
```

**`internal/config/agenc_config.go`**

Add this entry to `BuiltinPaletteCommands`:
```go
"starAgenc": {
    Title:       "⭐ Star AgenC on Github",
    Description: "Open the AgenC GitHub repository in your browser",
    Command:     "agenc star",
},
```

### Key Implementation Notes

1. **Use cmdStr constants throughout** - Never hardcode command names
   ```go
   // Good
   exec.Command(os.Args[0], tmuxCmdStr, windowCmdStr, newCmdStr, ...)

   // Bad
   exec.Command(os.Args[0], "tmux", "window", "new", ...)
   ```

2. **Platform-specific browser opening** - Current implementation uses macOS `open` command
   - Linux would need `xdg-open`
   - Windows would need `start` or `cmd /c start`
   - Future enhancement could use `runtime.GOOS` to detect platform
   - Document this as a known limitation for now

3. **Error messages** - Use descriptive context in `stacktrace.Propagate()`:
   - "failed to open browser to GitHub repository" - for star command
   - "failed to launch feedback mission" - for feedback command

4. **Command validation** - Both commands use `cobra.NoArgs` since they accept no arguments

5. **Recursive exec pattern** - `agenc feedback` uses `os.Args[0]` to invoke itself, matching the pattern in `cron_run.go`

6. **Constant extraction** - The feedback prompt and GitHub URL are extracted as package-level constants for easy maintenance

### Dependencies

All dependencies are already present in the codebase:
- `os/exec` - for executing commands
- `github.com/spf13/cobra` - command framework (already used)
- `github.com/mieubrisse/stacktrace` - error handling (already used)

## Testing Strategy

### Manual Testing (Primary Approach)

These commands are simple wrappers around system operations, so manual testing is the primary validation approach:

**`agenc star` command:**
1. Run `agenc star` from terminal
2. Verify browser opens to https://github.com/mieubrisse/agenc
3. Run `agenc star extraarg`
4. Verify error message about unexpected argument
5. Run `agenc star --help`
6. Verify help text displays correctly

**`agenc feedback` command:**
1. Start agenc tmux session
2. Run `agenc feedback` from terminal
3. Verify new tmux window opens
4. Verify Adjutant mission starts with feedback prompt
5. Run `agenc feedback extraarg`
6. Verify error message about unexpected argument
7. Run `agenc feedback --help`
8. Verify help text displays correctly

**Palette command:**
1. Open command palette in tmux session
2. Find "⭐ Star AgenC on Github" command
3. Execute it
4. Verify browser opens to GitHub repo

### Unit Testing (Optional)

Unit tests are not strictly necessary for these simple commands, but could be added by:
- Mocking `exec.Command` to verify correct arguments are passed
- Would require refactoring to inject command executor interface
- Complexity may not justify the test coverage gain for these simple wrappers

### Integration Testing

Not needed - these commands are thin wrappers around system commands that are better validated through manual testing.

## Acceptance Criteria

- [ ] `agenc star` opens https://github.com/mieubrisse/agenc in default browser
- [ ] `agenc feedback` launches new tmux window with Adjutant feedback mission containing correct prompt
- [ ] "⭐ Star AgenC on Github" palette command opens browser to GitHub repo
- [ ] Both CLI commands reject unexpected arguments with clear error message
- [ ] All command names use cmdStr constants (no hardcoded strings like "tmux", "mission", etc.)
- [ ] Error handling uses `stacktrace.Propagate()` with descriptive context
- [ ] Commands follow existing Cobra patterns:
  - Registered in `init()` function
  - Use `RunE` for error returns
  - Use `cobra.NoArgs` validator
  - Include `Short` and `Long` descriptions
- [ ] Help text is clear and informative for both commands
- [ ] Palette command has emoji, title, and description

## Risks & Considerations

### Platform Compatibility

**Risk**: Current implementation uses macOS `open` command, which doesn't work on Linux or Windows.

**Mitigation**:
- Document this limitation in command help text
- Future enhancement can add platform detection using `runtime.GOOS`:
  ```go
  var browserCmd string
  switch runtime.GOOS {
  case "darwin":
      browserCmd = "open"
  case "linux":
      browserCmd = "xdg-open"
  case "windows":
      browserCmd = "cmd"
      args = []string{"/c", "start", url}
  }
  ```
- For initial release, target macOS users (primary development platform)
- Add GitHub issue for cross-platform support if users request it

### Browser Opening Failures

**Risk**: `open` command might fail if no browser is installed or configured.

**Mitigation**:
- Error is properly propagated via `stacktrace.Propagate()`
- User sees clear error message: "failed to open browser to GitHub repository"
- User can manually navigate to GitHub if automatic opening fails

### Recursive Exec Overhead

**Risk**: Spawning subprocess has some performance overhead.

**Mitigation**:
- Overhead is minimal (milliseconds) for interactive commands
- Matches existing pattern in `cron_run.go`, which is proven in production
- User won't notice latency for this type of operation

### String Constant Management

**Risk**: Missing or mistyped cmdStr constants will cause runtime errors.

**Mitigation**:
- Go compiler catches undefined constants at build time
- All constants are defined in single file (`command_str_consts.go`)
- Easy to verify all command strings are present
- Manual testing will catch any runtime issues before release

### Feedback Command Assumptions

**Risk**: `agenc feedback` assumes user is inside a tmux session.

**Mitigation**:
- Error from tmux command will be propagated clearly
- User will see error message if not in tmux session
- This matches existing behavior of other tmux commands
- Could add future enhancement to detect tmux session and provide helpful error message
