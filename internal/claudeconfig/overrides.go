package claudeconfig

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AgencHooksDirname is the per-mission claude-config subdirectory that holds
// AgenC-managed hook scripts (separate from the user's `hooks/` dir, which is
// copied verbatim from the shadow repo).
const AgencHooksDirname = "agenc-hooks"

// RepoLibraryGuardScriptName is the filename of the PreToolUse hook script
// inside AgencHooksDirname.
const RepoLibraryGuardScriptName = "repo-library-guard.sh"

// RepoLibraryGuardScript is the embedded body of the PreToolUse hook that
// blocks Write/Edit/NotebookEdit calls targeting the repo library and
// substitutes a guidance message in place of the bare permission denial.
//
//go:embed repo_library_guard.sh
var RepoLibraryGuardScript string

// claudeConfigProtectedItems lists the files and directories inside
// claude-config that agents must not read or modify. These are the
// AgenC-injected configuration files; everything else (symlinked
// runtime dirs like shell-snapshots, plugins, projects, etc.) is left
// accessible so Claude Code can operate normally.
var claudeConfigProtectedItems = append(
	[]string{AgencHooksDirname},
	TrackableItemNames...,
)

// agencHookEventNames lists the Claude hook events that agenc intercepts to
// track Claude state and update tmux pane colors.
var agencHookEventNames = []string{
	"Stop",
	"UserPromptSubmit",
	"Notification",
	"PostToolUse",
	"PostToolUseFailure",
}

// staticAgencHookEntries holds the host-mission state-tracking hook entries
// (Stop, UserPromptSubmit, Notification, PostToolUse, PostToolUseFailure) that
// don't depend on any per-mission path. The PreToolUse repo-library guard,
// which does depend on the per-mission claude-config path, is added by
// BuildAgencHookEntries.
var staticAgencHookEntries map[string]json.RawMessage

// staticContainerHookEntries is the container variant of staticAgencHookEntries.
var staticContainerHookEntries map[string]json.RawMessage

func init() {
	staticAgencHookEntries = make(map[string]json.RawMessage, len(agencHookEventNames))
	for _, eventName := range agencHookEventNames {
		// The Go command handler (runMissionSendClaudeUpdate) skips stdin for
		// non-Notification events and uses a timeout for Notification events,
		// so no shell-level stdin redirect is needed here. Shell redirects like
		// "< /dev/null" cannot be used because Claude Code may tokenize the
		// command string rather than passing it to sh -c, causing the redirect
		// tokens to be interpreted as extra positional arguments.
		entry := `[{"hooks":[{"type":"command","command":"agenc mission send claude-update $AGENC_MISSION_UUID ` + eventName + `"}]}]`
		staticAgencHookEntries[eventName] = json.RawMessage(entry)
	}

	staticContainerHookEntries = make(map[string]json.RawMessage, len(agencHookEventNames))
	for _, eventName := range agencHookEventNames {
		// Only Notification events pass stdin data (-d @-) to extract
		// notification_type. Other events use an empty body to avoid hanging
		// on stdin that Claude Code may not close.
		var stdinFlag string
		if eventName == "Notification" {
			stdinFlag = "-d @-"
		} else {
			stdinFlag = `-d "{}"`
		}
		cmd := fmt.Sprintf(
			`curl -s --unix-socket $AGENC_WRAPPER_SOCKET -X POST http://w/claude-update/%s -H "Content-Type: application/json" %s -o /dev/null || true`,
			eventName, stdinFlag,
		)
		entry := fmt.Sprintf(`[{"hooks":[{"type":"command","command":"%s"}]}]`, cmd)
		staticContainerHookEntries[eventName] = json.RawMessage(entry)
	}
}

// BuildAgencHookEntries returns the full hook entries map for non-containerized
// missions: the static state-tracking hooks plus the PreToolUse repo-library
// guard, which references the per-mission claude-config snapshot.
func BuildAgencHookEntries(claudeConfigDirpath string) map[string]json.RawMessage {
	entries := make(map[string]json.RawMessage, len(staticAgencHookEntries)+1)
	for eventName, entry := range staticAgencHookEntries {
		entries[eventName] = entry
	}
	entries["PreToolUse"] = buildRepoLibraryGuardHookEntry(claudeConfigDirpath)
	return entries
}

// BuildContainerHookEntries returns the full hook entries map for
// containerized missions. The repo library is host-only state and is not
// bind-mounted into containers, so the PreToolUse repo-library guard is
// omitted — there is no path inside the container that would match it.
func BuildContainerHookEntries() map[string]json.RawMessage {
	entries := make(map[string]json.RawMessage, len(staticContainerHookEntries))
	for eventName, entry := range staticContainerHookEntries {
		entries[eventName] = entry
	}
	return entries
}

// buildRepoLibraryGuardHookEntry constructs the PreToolUse hook entry that
// runs the embedded bash guard script. Matches Write, Edit, and NotebookEdit
// — the file-modifying tools whose permission-deny against the repo library
// produces the confusing "denied by your permission settings" message we want
// to replace with explicit guidance.
//
// The script lives at <claudeConfigDirpath>/agenc-hooks/repo-library-guard.sh
// (written by WriteAgencHookScripts at config-build time) and is invoked with
// an absolute path so no env var expansion or path-rewriting is required at
// hook-firing time.
func buildRepoLibraryGuardHookEntry(claudeConfigDirpath string) json.RawMessage {
	scriptFilepath := filepath.Join(claudeConfigDirpath, AgencHooksDirname, RepoLibraryGuardScriptName)

	command := fmt.Sprintf("bash %s", scriptFilepath)
	commandJSON, _ := json.Marshal(command)

	entry := fmt.Sprintf(
		`[{"matcher":"Write|Edit|NotebookEdit","hooks":[{"type":"command","command":%s}]}]`,
		string(commandJSON),
	)
	return json.RawMessage(entry)
}

// AgencFilePermissionTools lists the Claude Code file-access tools used to
// construct both allow and deny permission entries.
var AgencFilePermissionTools = []string{
	"Read",
	"Glob",
	"Search",
	"Grep",
	"Write",
	"Edit",
	"NotebookEdit",
}

// AgencDenyPermissionTools is an alias preserved for readability in deny-specific
// contexts. It references the same tool list as AgencFilePermissionTools.
var AgencDenyPermissionTools = AgencFilePermissionTools

// BuildAgentDirAllowEntries returns permission allow entries that grant agents
// full read/write access to their own working directory. Generates entries
// using relative paths (./* and ./**) and absolute path variants (// and ~)
// with both single-level (*) and recursive (**) globs. In gitignore syntax
// used by Claude Code permissions, * matches a single directory level and **
// matches recursively.
func BuildAgentDirAllowEntries(agentDirpath string) []string {
	// Relative entries cover tool-level access from the working directory
	relativePattern := "./**"

	// Absolute entries ensure the Bash sandbox filesystem allowlist includes
	// the agent directory by its full path
	absolutePatterns := buildPathVariants(agentDirpath)

	allPatterns := make([]string, 0, 2+2*len(absolutePatterns))
	allPatterns = append(allPatterns, relativePattern)
	allPatterns = append(allPatterns, "./*")
	for _, p := range absolutePatterns {
		allPatterns = append(allPatterns, p+"/**")
		allPatterns = append(allPatterns, p+"/*")
	}

	entries := make([]string, 0, len(AgencFilePermissionTools)*len(allPatterns))
	for _, tool := range AgencFilePermissionTools {
		for _, pattern := range allPatterns {
			entries = append(entries, tool+"("+pattern+")")
		}
	}
	return entries
}

// AgencRepoLibraryWriteTools lists the tools denied write access to the shared
// repo library. Read-only tools (Read, Glob, Grep) are intentionally omitted
// so agents can explore code in the repo library without spawning a new mission.
var AgencRepoLibraryWriteTools = []string{
	"Write",
	"Edit",
	"NotebookEdit",
}

// BuildRepoLibraryDenyEntries constructs permission deny entries that prevent
// agents from modifying the shared repo library under the given agenc dir.
// Read-only access (Read, Glob, Grep) is allowed so agents can explore code
// in other repos without needing to spawn a new mission.
func BuildRepoLibraryDenyEntries(agencDirpath string) []string {
	reposDirpath := filepath.Join(agencDirpath, "repos")
	baseVariants := buildPathVariants(reposDirpath)

	entries := make([]string, 0, len(AgencRepoLibraryWriteTools)*len(baseVariants))
	for _, tool := range AgencRepoLibraryWriteTools {
		for _, base := range baseVariants {
			entries = append(entries, tool+"("+base+"/**)")
		}
	}
	return entries
}

// BuildClaudeConfigDenyEntries constructs permission deny entries that prevent
// agents from reading or modifying the AgenC-injected configuration files
// inside their mission's claude-config directory (CLAUDE.md, settings.json,
// skills/, hooks/, commands/, agents/).
//
// Only the protected items are denied — symlinked runtime directories like
// shell-snapshots, plugins, and projects are left accessible so Claude Code
// can operate normally.
//
// Generates deny rules for both path formats (// absolute, tilde) to ensure
// agents cannot bypass the deny rules by using different path representations.
func BuildClaudeConfigDenyEntries(claudeConfigDirpath string) []string {
	baseVariants := buildPathVariants(claudeConfigDirpath)

	// Build the list of per-item path suffixes. Files get an exact match;
	// directories get both /* and /** globs to cover single-level and recursive access.
	var itemSuffixes []string
	for _, item := range claudeConfigProtectedItems {
		if isFileName(item) {
			itemSuffixes = append(itemSuffixes, "/"+item)
		} else {
			itemSuffixes = append(itemSuffixes, "/"+item+"/**")
			itemSuffixes = append(itemSuffixes, "/"+item+"/*")
		}
	}

	entries := make([]string, 0, len(AgencDenyPermissionTools)*len(baseVariants)*len(itemSuffixes))
	for _, tool := range AgencDenyPermissionTools {
		for _, base := range baseVariants {
			for _, suffix := range itemSuffixes {
				entries = append(entries, tool+"("+base+suffix+")")
			}
		}
	}
	return entries
}

// isFileName returns true if the name looks like a file (contains a dot
// indicating an extension) rather than a directory.
func isFileName(name string) bool {
	return strings.Contains(name, ".")
}

// buildPathVariants converts an absolute path to the Claude Code permission path
// formats documented at https://code.claude.com/docs/en/permissions:
//
//   - //path  — absolute from filesystem root (NOT /path, which is project-root-relative)
//   - ~/path  — relative to home directory
//
// The // prefix is required because Claude Code's permission system uses gitignore
// syntax, where a single leading / means "relative to project root", not "absolute".
func buildPathVariants(absolutePath string) []string {
	// "/" prefix marks an absolute filesystem path in gitignore syntax.
	// Absolute paths already start with /, so prepend just one more.
	variants := []string{"/" + absolutePath}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return variants
	}

	// Check if path is under home directory
	if strings.HasPrefix(absolutePath, homeDir+string(filepath.Separator)) {
		relPath, err := filepath.Rel(homeDir, absolutePath)
		if err == nil {
			// Add tilde variant: ~/.agenc/missions/...
			variants = append(variants, filepath.Join("~", relPath))
		}
	}

	return variants
}
