# Session Print & Mission Print Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `agenc session print <session-uuid>` and `agenc mission print [mission-uuid]` commands that output raw JSONL session transcripts (last 20 lines by default) for agent-to-agent communication.

**Architecture:** Two new CLI commands share a common JSONL tail-printing function. `session print` finds a JSONL file by session UUID directly. `mission print` resolves a mission UUID to its current session UUID (via `GetLastSessionID`) then delegates to the same print logic. The `mission inspect` command is enhanced to list all session UUIDs.

**Tech Stack:** Go, Cobra CLI, existing `internal/session`, `internal/claudeconfig`, `internal/config` packages.

---

### Task 1: Add command string constants

**Files:**
- Modify: `cmd/command_str_consts.go`

**Step 1: Add new constants**

In `cmd/command_str_consts.go`, add `sessionCmdStr` to the top-level commands block (after line 26) and `printCmdStr` to the shared subcommands block (after line 37). Add flag constants to the flags block (after line 93).

```go
// In top-level commands section (after feedbackCmdStr line 26):
sessionCmdStr = "session"

// In shared subcommands section (after paneCmdStr line 37):
printCmdStr = "print"

// In flags section, add a new comment group after mission inspect flags:
// session/mission print flags
tailFlagName = "tail"
```

Note: `allFlagName` already exists at line 91 — reuse it for `--all`.

**Step 2: Commit**

```bash
git add cmd/command_str_consts.go
git commit -m "Add session and print command string constants"
```

---

### Task 2: Add session helpers to `internal/session`

**Files:**
- Modify: `internal/session/session.go`

**Step 1: Add `FindSessionJSONLPath` function**

Append to `internal/session/session.go` after the existing `findNamesInJSONL` function (after line 203):

```go
// FindSessionJSONLPath locates the JSONL transcript file for a given session UUID.
// It searches all project directories under ~/.claude/projects/ for a file named
// <sessionID>.jsonl. Returns the full path or an error if not found.
func FindSessionJSONLPath(sessionID string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	projectsDirpath := filepath.Join(homeDir, ".claude", "projects")
	entries, err := os.ReadDir(projectsDirpath)
	if err != nil {
		return "", fmt.Errorf("failed to read projects directory '%s': %w", projectsDirpath, err)
	}

	targetFilename := sessionID + ".jsonl"
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidateFilepath := filepath.Join(projectsDirpath, entry.Name(), targetFilename)
		if _, err := os.Stat(candidateFilepath); err == nil {
			return candidateFilepath, nil
		}
	}

	return "", fmt.Errorf("session transcript not found for session ID: %s", sessionID)
}
```

Note: Add `"fmt"` to the imports at the top of the file (it currently only has `"bufio"`, `"encoding/json"`, `"os"`, `"path/filepath"`, `"strings"`).

**Step 2: Add `ListSessionIDs` function**

Append after `FindSessionJSONLPath`:

```go
// ListSessionIDs returns all session UUIDs for a given mission by scanning
// the mission's project directory for .jsonl files. Returns session IDs
// (filenames without the .jsonl extension) sorted by modification time
// (most recent first). Returns an empty slice if no sessions are found.
func ListSessionIDs(claudeConfigDirpath string, missionID string) []string {
	projectDirpath := findProjectDirpath(claudeConfigDirpath, missionID)
	if projectDirpath == "" {
		return nil
	}

	entries, err := os.ReadDir(projectDirpath)
	if err != nil {
		return nil
	}

	// Collect JSONL files with their mod times for sorting
	type sessionEntry struct {
		id      string
		modTime int64
	}
	var sessions []sessionEntry

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
		sessions = append(sessions, sessionEntry{
			id:      sessionID,
			modTime: info.ModTime().UnixMilli(),
		})
	}

	// Sort by modification time, most recent first
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].modTime > sessions[j].modTime
	})

	result := make([]string, len(sessions))
	for i, s := range sessions {
		result[i] = s.id
	}
	return result
}
```

Note: Add `"sort"` to the imports.

**Step 3: Add `TailJSONLFile` function**

This is the shared printing logic used by both commands. Append after `ListSessionIDs`:

```go
// TailJSONLFile reads the last N lines from a JSONL file and writes them to
// the given writer. If n <= 0, writes the entire file. Returns the number
// of lines written.
func TailJSONLFile(filepath string, n int, w io.Writer) (int, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return 0, fmt.Errorf("failed to open session file '%s': %w", filepath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	if n <= 0 {
		// Print all lines
		count := 0
		for scanner.Scan() {
			fmt.Fprintln(w, scanner.Text())
			count++
		}
		return count, scanner.Err()
	}

	// Collect lines in a ring buffer for tail behavior
	ring := make([]string, n)
	total := 0
	for scanner.Scan() {
		ring[total%n] = scanner.Text()
		total++
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("error reading session file: %w", err)
	}

	// Output the last min(total, n) lines in order
	count := total
	if count > n {
		count = n
	}
	startIdx := total - count
	for i := 0; i < count; i++ {
		fmt.Fprintln(w, ring[(startIdx+i)%n])
	}
	return count, nil
}
```

Note: Add `"io"` to the imports.

**Step 4: Verify it compiles**

Run: `go build ./internal/session/`
Expected: No errors.

**Step 5: Commit**

```bash
git add internal/session/session.go
git commit -m "Add session JSONL lookup, listing, and tail helpers"
```

---

### Task 3: Create `session` parent command

**Files:**
- Create: `cmd/session.go`

**Step 1: Create the file**

Create `cmd/session.go`:

```go
package cmd

import (
	"github.com/spf13/cobra"
)

var sessionCmd = &cobra.Command{
	Use:   sessionCmdStr,
	Short: "Manage Claude Code sessions",
}

func init() {
	rootCmd.AddCommand(sessionCmd)
}
```

**Step 2: Verify it compiles**

Run: `go build ./cmd/...`
Expected: No errors. `agenc session` shows help with no subcommands yet.

**Step 3: Commit**

```bash
git add cmd/session.go
git commit -m "Add session parent command"
```

---

### Task 4: Create `session print` command

**Files:**
- Create: `cmd/session_print.go`

**Step 1: Create the file**

Create `cmd/session_print.go`:

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/session"
)

const defaultTailLines = 20

var sessionPrintTailFlag int
var sessionPrintAllFlag bool

var sessionPrintCmd = &cobra.Command{
	Use:   printCmdStr + " <session-uuid>",
	Short: "Print the JSONL transcript for a Claude session",
	Long: `Print the JSONL transcript for a Claude session.

Outputs the last 20 lines by default. Use --tail to change the line count,
or --all to print the entire session.

Example:
  agenc session print 18749fb5-02ba-4b19-b989-4e18fbf8ea92
  agenc session print 18749fb5-02ba-4b19-b989-4e18fbf8ea92 --tail 50
  agenc session print 18749fb5-02ba-4b19-b989-4e18fbf8ea92 --all`,
	Args: cobra.ExactArgs(1),
	RunE: runSessionPrint,
}

func init() {
	sessionPrintCmd.Flags().IntVar(&sessionPrintTailFlag, tailFlagName, defaultTailLines, "number of lines to print from end of session")
	sessionPrintCmd.Flags().BoolVar(&sessionPrintAllFlag, allFlagName, false, "print entire session")
	sessionCmd.AddCommand(sessionPrintCmd)
}

func runSessionPrint(cmd *cobra.Command, args []string) error {
	sessionID := args[0]

	if !sessionPrintAllFlag && sessionPrintTailFlag <= 0 {
		return stacktrace.NewError("--tail value must be positive")
	}

	jsonlFilepath, err := session.FindSessionJSONLPath(sessionID)
	if err != nil {
		return stacktrace.Propagate(err, "")
	}

	return printSessionJSONL(jsonlFilepath, sessionPrintTailFlag, sessionPrintAllFlag)
}

// printSessionJSONL is the shared JSONL printing logic used by both
// session print and mission print commands.
func printSessionJSONL(jsonlFilepath string, tailLines int, all bool) error {
	n := tailLines
	if all {
		n = 0
	}

	_, err := session.TailJSONLFile(jsonlFilepath, n, os.Stdout)
	if err != nil {
		return stacktrace.Propagate(err, "")
	}
	return nil
}
```

**Step 2: Smoke test**

Run: `make build`
Then: `./agenc session print --help`
Expected: Shows usage with `--tail` and `--all` flags documented.

**Step 3: Commit**

```bash
git add cmd/session_print.go
git commit -m "Add session print command"
```

---

### Task 5: Create `mission print` command

**Files:**
- Create: `cmd/mission_print.go`

**Step 1: Create the file**

Create `cmd/mission_print.go`:

```go
package cmd

import (
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/session"
)

var missionPrintTailFlag int
var missionPrintAllFlag bool

var missionPrintCmd = &cobra.Command{
	Use:   printCmdStr + " [mission-id]",
	Short: "Print the JSONL transcript for a mission's current session",
	Long: `Print the JSONL transcript for a mission's current session.

Without arguments, opens an interactive fzf picker to select a mission.
With arguments, accepts a mission ID (short 8-char hex or full UUID).

Outputs the last 20 lines by default. Use --tail to change the line count,
or --all to print the entire session.

Example:
  agenc mission print
  agenc mission print 2571d5d8
  agenc mission print 2571d5d8 --tail 50
  agenc mission print 2571d5d8 --all`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionPrint,
}

func init() {
	missionPrintCmd.Flags().IntVar(&missionPrintTailFlag, tailFlagName, defaultTailLines, "number of lines to print from end of session")
	missionPrintCmd.Flags().BoolVar(&missionPrintAllFlag, allFlagName, false, "print entire session")
	missionCmd.AddCommand(missionPrintCmd)
}

func runMissionPrint(cmd *cobra.Command, args []string) error {
	if !missionPrintAllFlag && missionPrintTailFlag <= 0 {
		return stacktrace.NewError("--tail value must be positive")
	}

	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	missions, err := db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		return stacktrace.NewError("no missions found")
	}

	entries, err := buildMissionPickerEntries(db, missions, defaultPromptMaxLen)
	if err != nil {
		return err
	}

	input := strings.Join(args, " ")
	result, err := Resolve(input, Resolver[missionPickerEntry]{
		TryCanonical: func(input string) (missionPickerEntry, bool, error) {
			if !looksLikeMissionID(input) {
				return missionPickerEntry{}, false, nil
			}
			missionID, err := db.ResolveMissionID(input)
			if err != nil {
				return missionPickerEntry{}, false, stacktrace.Propagate(err, "failed to resolve mission ID")
			}
			for _, e := range entries {
				if e.MissionID == missionID {
					return e, true, nil
				}
			}
			return missionPickerEntry{}, false, stacktrace.NewError("mission %s not found", input)
		},
		GetItems: func() ([]missionPickerEntry, error) { return entries, nil },
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Status, e.Session, e.Repo}
		},
		FzfPrompt:         "Select mission to print session: ",
		FzfHeaders:        []string{"LAST ACTIVE", "ID", "STATUS", "SESSION", "REPO"},
		MultiSelect:       false,
		NotCanonicalError: "not a valid mission ID",
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	missionID := result.Items[0].MissionID

	// Resolve mission's current session ID
	sessionID := claudeconfig.GetLastSessionID(agencDirpath, missionID)
	if sessionID == "" {
		return stacktrace.NewError("no current session found for mission %s", missionID)
	}

	// Find and print the session JSONL
	jsonlFilepath, err := session.FindSessionJSONLPath(sessionID)
	if err != nil {
		return stacktrace.Propagate(err, "")
	}

	return printSessionJSONL(jsonlFilepath, missionPrintTailFlag, missionPrintAllFlag)
}
```

**Step 2: Smoke test**

Run: `make build`
Then: `./agenc mission print --help`
Expected: Shows usage with fzf picker, `--tail`, and `--all` flags.

**Step 3: Commit**

```bash
git add cmd/mission_print.go
git commit -m "Add mission print command"
```

---

### Task 6: Enhance `mission inspect` with session listing

**Files:**
- Modify: `cmd/mission_inspect.go`

**Step 1: Add session listing to `inspectMission`**

In `cmd/mission_inspect.go`, add imports for `"github.com/odyssey/agenc/internal/claudeconfig"` and `"github.com/odyssey/agenc/internal/session"` to the import block.

Then, in the `inspectMission` function, add after the `fmt.Printf("Updated:...")` line (after line 128, before `return nil`):

```go
	// List session UUIDs
	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(agencDirpath, missionID)
	sessionIDs := session.ListSessionIDs(claudeConfigDirpath, missionID)
	currentSessionID := claudeconfig.GetLastSessionID(agencDirpath, missionID)

	if len(sessionIDs) == 0 {
		fmt.Printf("Sessions:    --\n")
	} else {
		fmt.Printf("Sessions:    %d total\n", len(sessionIDs))
		for _, sid := range sessionIDs {
			marker := "  "
			suffix := ""
			if sid == currentSessionID {
				marker = "* "
				suffix = "  (current)"
			}
			fmt.Printf("             %s%s%s\n", marker, sid, suffix)
		}
	}
```

**Step 2: Smoke test**

Run: `make build`
Then: `./agenc mission inspect <some-mission-id>`
Expected: Shows "Sessions:" section with UUIDs listed and `*` on current.

**Step 3: Commit**

```bash
git add cmd/mission_inspect.go
git commit -m "Add session UUID listing to mission inspect"
```

---

### Task 7: Update architecture doc

**Files:**
- Modify: `docs/system-architecture.md`

**Step 1: Update the architecture doc**

Find the `internal/session/` section in the architecture doc and add the new functions. Find the CLI commands section and add the new `session` command group and `mission print` subcommand.

The specific additions depend on the current doc structure — read the file and add entries in the appropriate locations following existing patterns.

**Step 2: Commit**

```bash
git add docs/system-architecture.md
git commit -m "Update architecture doc with session print commands"
```

---

### Task 8: Manual smoke test

**Step 1: Build**

Run: `make build`

**Step 2: Test `session print` with a known session**

Run `./agenc mission inspect` on any mission to find a session UUID, then:

```bash
./agenc session print <session-uuid>
```

Expected: Last 20 JSONL lines printed to stdout.

```bash
./agenc session print <session-uuid> --tail 5
```

Expected: Last 5 lines.

```bash
./agenc session print <session-uuid> --all
```

Expected: Entire file contents.

**Step 3: Test `mission print`**

```bash
./agenc mission print <mission-short-id>
```

Expected: Last 20 JSONL lines from that mission's current session.

```bash
./agenc mission print
```

Expected: fzf picker opens.

**Step 4: Test error cases**

```bash
./agenc session print nonexistent-uuid-here
```

Expected: Error message to stderr, exit code 1.

```bash
./agenc session print <session-uuid> --tail 0
```

Expected: Error about invalid value.

**Step 5: Commit any fixes**

If any fixes were needed during testing, commit them.
