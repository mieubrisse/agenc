package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

// maxTrustWriteAttempts caps the verify-retry loop in writeTrustEntry.
// A concurrent lock-free writer (Claude itself) may clobber our rename
// between the rename and the re-read; retrying a small number of times
// is sufficient in practice.
const maxTrustWriteAttempts = 3

// trustWriteRetryDelay is the pause between verify-retry attempts.
const trustWriteRetryDelay = 50 * time.Millisecond

// writeTrustEntry seeds projects["<agentDirpath>"] into the ~/.claude.json-shaped
// file at claudeJSONFilepath, preserving all other content. It performs a
// read-modify-write with an atomic temp-file+rename, then re-reads and verifies
// the entry landed; retrying a bounded number of times to survive a concurrent
// lock-free writer (Claude) that may clobber the file between our rename and
// the verify read.
func writeTrustEntry(claudeJSONFilepath string, agentDirpath string, trustedMcpServers *config.TrustedMcpServers) error {
	for attempt := 0; attempt < maxTrustWriteAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(trustWriteRetryDelay)
		}

		if err := writeTrustEntryOnce(claudeJSONFilepath, agentDirpath, trustedMcpServers); err != nil {
			return err
		}

		// Verify the entry landed — a concurrent writer (Claude) may have
		// clobbered our rename between the rename and this read.
		verified, err := verifyTrustEntry(claudeJSONFilepath, agentDirpath)
		if err != nil {
			return stacktrace.Propagate(err, "failed to verify trust entry for '%s' in '%s'", agentDirpath, claudeJSONFilepath)
		}
		if verified {
			return nil
		}
	}

	return stacktrace.NewError(
		"trust entry for '%s' not present in '%s' after %d attempts; a concurrent writer may be clobbering the file",
		agentDirpath, claudeJSONFilepath, maxTrustWriteAttempts,
	)
}

// writeTrustEntryOnce performs a single read-modify-write of claudeJSONFilepath,
// atomically writing the trust entry via a temp file + rename.
func writeTrustEntryOnce(claudeJSONFilepath string, agentDirpath string, trustedMcpServers *config.TrustedMcpServers) error {
	// Read existing file; treat a missing file as an empty JSON object.
	data, err := os.ReadFile(claudeJSONFilepath)
	if err != nil {
		if !os.IsNotExist(err) {
			return stacktrace.Propagate(err, "failed to read '%s'", claudeJSONFilepath)
		}
		data = []byte("{}")
	}

	// Parse into a top-level map, preserving all other keys byte-for-byte.
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return stacktrace.Propagate(err, "failed to parse JSON in '%s'", claudeJSONFilepath)
	}

	// Get or create the "projects" sub-map, preserving all other entries.
	var projects map[string]json.RawMessage
	if existingProjects, ok := root["projects"]; ok {
		if err := json.Unmarshal(existingProjects, &projects); err != nil {
			return stacktrace.Propagate(err, "failed to parse projects map in '%s'", claudeJSONFilepath)
		}
	} else {
		projects = make(map[string]json.RawMessage)
	}

	// Build the trust entry for agentDirpath; only this key is touched.
	trustEntry := buildTrustEntry(trustedMcpServers)
	trustEntryData, err := json.Marshal(trustEntry)
	if err != nil {
		return stacktrace.Propagate(err, "failed to marshal trust entry for '%s'", agentDirpath)
	}
	projects[agentDirpath] = json.RawMessage(trustEntryData)

	// Write projects back into the root map.
	projectsData, err := json.Marshal(projects)
	if err != nil {
		return stacktrace.Propagate(err, "failed to marshal projects map")
	}
	root["projects"] = json.RawMessage(projectsData)

	// Serialize the full file with indentation + trailing newline,
	// matching copyAndPatchClaudeJSON's output format.
	result, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return stacktrace.Propagate(err, "failed to marshal root JSON")
	}
	result = append(result, '\n')

	return atomicWriteFile(claudeJSONFilepath, result)
}

// buildTrustEntry constructs the trust entry map for a project, mirroring the
// logic in claudeconfig.copyAndPatchClaudeJSON.
func buildTrustEntry(trustedMcpServers *config.TrustedMcpServers) map[string]interface{} {
	entry := map[string]interface{}{
		"hasTrustDialogAccepted": true,
	}
	if trustedMcpServers != nil {
		if trustedMcpServers.All {
			entry["enabledMcpjsonServers"] = []string{}
			entry["disabledMcpjsonServers"] = []string{}
		} else {
			entry["enabledMcpjsonServers"] = trustedMcpServers.List
			entry["disabledMcpjsonServers"] = []string{}
		}
	}
	return entry
}

// verifyTrustEntry re-reads claudeJSONFilepath and returns true if
// projects[agentDirpath].hasTrustDialogAccepted == true.
func verifyTrustEntry(claudeJSONFilepath string, agentDirpath string) (bool, error) {
	data, err := os.ReadFile(claudeJSONFilepath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, stacktrace.Propagate(err, "failed to read '%s' during verify", claudeJSONFilepath)
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return false, stacktrace.Propagate(err, "failed to parse JSON in '%s' during verify", claudeJSONFilepath)
	}

	projectsRaw, ok := root["projects"]
	if !ok {
		return false, nil
	}

	var projects map[string]json.RawMessage
	if err := json.Unmarshal(projectsRaw, &projects); err != nil {
		return false, stacktrace.Propagate(err, "failed to parse projects map in '%s' during verify", claudeJSONFilepath)
	}

	entryRaw, ok := projects[agentDirpath]
	if !ok {
		return false, nil
	}

	var entry map[string]interface{}
	if err := json.Unmarshal(entryRaw, &entry); err != nil {
		return false, stacktrace.Propagate(err, "failed to parse trust entry for '%s' during verify", agentDirpath)
	}

	trusted, ok := entry["hasTrustDialogAccepted"]
	if !ok {
		return false, nil
	}
	trustedBool, ok := trusted.(bool)
	return ok && trustedBool, nil
}

// pruneTrustEntry removes projects["<agentDirpath>"] from the file at
// claudeJSONFilepath, preserving all other content, via the same atomic
// temp-file+rename as writeTrustEntry. No-op if the file or entry is absent.
func pruneTrustEntry(claudeJSONFilepath string, agentDirpath string) error {
	data, err := os.ReadFile(claudeJSONFilepath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return stacktrace.Propagate(err, "failed to read '%s'", claudeJSONFilepath)
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return stacktrace.Propagate(err, "failed to parse JSON in '%s'", claudeJSONFilepath)
	}

	projectsRaw, ok := root["projects"]
	if !ok {
		// No projects key — nothing to prune.
		return nil
	}

	var projects map[string]json.RawMessage
	if err := json.Unmarshal(projectsRaw, &projects); err != nil {
		return stacktrace.Propagate(err, "failed to parse projects map in '%s'", claudeJSONFilepath)
	}

	if _, ok := projects[agentDirpath]; !ok {
		// Entry already absent — nothing to do.
		return nil
	}

	delete(projects, agentDirpath)

	projectsData, err := json.Marshal(projects)
	if err != nil {
		return stacktrace.Propagate(err, "failed to marshal projects map after prune")
	}
	root["projects"] = json.RawMessage(projectsData)

	result, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return stacktrace.Propagate(err, "failed to marshal root JSON after prune")
	}
	result = append(result, '\n')

	return atomicWriteFile(claudeJSONFilepath, result)
}

// atomicWriteFile writes data to targetFilepath via a temp file + rename in the
// same directory, using file mode 0644. On any error, the temp file is removed.
func atomicWriteFile(targetFilepath string, data []byte) error {
	dir := filepath.Dir(targetFilepath)
	tmpFile, err := os.CreateTemp(dir, ".claude.json.tmp-*")
	if err != nil {
		return stacktrace.Propagate(err, "failed to create temp file in '%s'", dir)
	}
	shouldRemoveTmp := true
	defer func() {
		if shouldRemoveTmp {
			os.Remove(tmpFile.Name()) //nolint:errcheck // best-effort cleanup; nothing useful to do with the error
		}
	}()

	if err := os.Chmod(tmpFile.Name(), 0644); err != nil {
		tmpFile.Close()
		return stacktrace.Propagate(err, "failed to chmod temp file '%s'", tmpFile.Name())
	}

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return stacktrace.Propagate(err, "failed to write temp file '%s'", tmpFile.Name())
	}
	if err := tmpFile.Close(); err != nil {
		return stacktrace.Propagate(err, "failed to close temp file '%s'", tmpFile.Name())
	}

	if err := os.Rename(tmpFile.Name(), targetFilepath); err != nil {
		return stacktrace.Propagate(err, "failed to rename '%s' to '%s'", tmpFile.Name(), targetFilepath)
	}
	shouldRemoveTmp = false

	return nil
}

// seedMissionTrust writes the trust entry for a mission's agent dir into the
// Claude-config .claude.json under s.claudeJSONMu. This targets the file
// Claude reads — $CLAUDE_CONFIG_DIR/.claude.json when CLAUDE_CONFIG_DIR is
// set, else ~/.claude.json (State Y). (Unwired until the State Y flip.)
//
//nolint:unused // intentionally unwired; wired at the State Y flip
func (s *Server) seedMissionTrust(agentDirpath string, trustedMcpServers *config.TrustedMcpServers) error {
	claudeJSONPath, err := resolveClaudeJSONFilepath()
	if err != nil {
		return err
	}

	s.claudeJSONMu.Lock()
	defer s.claudeJSONMu.Unlock()

	return writeTrustEntry(claudeJSONPath, agentDirpath, trustedMcpServers)
}

// pruneMissionTrust removes a mission's trust entry from the Claude-config
// .claude.json under s.claudeJSONMu. (Unwired until the State Y flip.)
//
//nolint:unused // intentionally unwired; wired at the State Y flip
func (s *Server) pruneMissionTrust(agentDirpath string) error {
	claudeJSONPath, err := resolveClaudeJSONFilepath()
	if err != nil {
		return err
	}

	s.claudeJSONMu.Lock()
	defer s.claudeJSONMu.Unlock()

	return pruneTrustEntry(claudeJSONPath, agentDirpath)
}

// resolveClaudeJSONFilepath returns the path to the .claude.json file that
// Claude reads and writes — $CLAUDE_CONFIG_DIR/.claude.json when
// CLAUDE_CONFIG_DIR is set, else ~/.claude.json. This mirrors Claude Code's
// own config resolution, so AgenC writes trust to the file Claude actually
// reads regardless of whether CLAUDE_CONFIG_DIR is set (e.g. in e2e tests).
//
//nolint:unused // intentionally unwired; wired at the State Y flip
func resolveClaudeJSONFilepath() (string, error) {
	if configDir := os.Getenv("CLAUDE_CONFIG_DIR"); configDir != "" {
		return filepath.Join(configDir, ".claude.json"), nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to determine home directory")
	}
	return filepath.Join(homeDir, ".claude.json"), nil
}

// reconcileTrustEntries is the pure, unit-testable core of the boot-time trust
// migration pass. It performs a single read-modify-write of claudeJSONFilepath:
//
//   - Seeds projects[agentDir] with a bare hasTrustDialogAccepted=true entry
//     for every path in existingAgentDirs. Bare entries (no MCP-server lists)
//     are sufficient for the migration: the trust dialog is suppressed, and
//     per-server MCP consent is re-established on the next real mission create.
//
//   - Prunes any projects key whose path starts with missionsDirPrefix+"/" that
//     is NOT in existingAgentDirs. These are stale entries from archived or
//     deleted missions.
//
//   - Leaves all other projects keys byte-for-byte untouched. The user's own
//     repos outside the missions directory are never touched.
//
// If claudeJSONFilepath does not exist, it is created with only the seeded
// entries (and no other keys). If existingAgentDirs is empty the function is
// still correct: it only prunes stale missions-prefix keys and writes the
// result if the file changed.
func reconcileTrustEntries(claudeJSONFilepath string, existingAgentDirs []string, missionsDirPrefix string) error {
	// Build a set of the expected agent dirs for O(1) lookup.
	expected := make(map[string]struct{}, len(existingAgentDirs))
	for _, dir := range existingAgentDirs {
		expected[dir] = struct{}{}
	}

	// Read existing file; treat a missing file as an empty JSON object.
	data, err := os.ReadFile(claudeJSONFilepath)
	if err != nil {
		if !os.IsNotExist(err) {
			return stacktrace.Propagate(err, "failed to read '%s'", claudeJSONFilepath)
		}
		data = []byte("{}")
	}

	// Parse into a top-level map, preserving all other keys byte-for-byte.
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return stacktrace.Propagate(err, "failed to parse JSON in '%s'", claudeJSONFilepath)
	}

	// Get or create the "projects" sub-map.
	var projects map[string]json.RawMessage
	if existingProjects, ok := root["projects"]; ok {
		if err := json.Unmarshal(existingProjects, &projects); err != nil {
			return stacktrace.Propagate(err, "failed to parse projects map in '%s'", claudeJSONFilepath)
		}
	} else {
		projects = make(map[string]json.RawMessage)
	}

	// Prune stale missions entries: any key under missionsDirPrefix that is not
	// in the expected set. Use a prefix + separator check so we don't
	// accidentally match a directory that merely starts with the missions dir
	// name (e.g. ~/.agenc/missions-backup/).
	missionsPrefix := missionsDirPrefix + string(filepath.Separator)
	for key := range projects {
		if strings.HasPrefix(key, missionsPrefix) {
			if _, keep := expected[key]; !keep {
				delete(projects, key)
			}
		}
	}

	// Seed a bare trust entry for every expected agent dir.
	// Bare entry choice: hasTrustDialogAccepted=true with no MCP-server lists.
	// The migration only needs to suppress the trust dialog; per-server MCP
	// consent is re-established on the next real mission create via spawnClaude.
	bareEntry := buildTrustEntry(nil)
	bareEntryData, err := json.Marshal(bareEntry)
	if err != nil {
		return stacktrace.Propagate(err, "failed to marshal bare trust entry")
	}
	for _, dir := range existingAgentDirs {
		projects[dir] = json.RawMessage(bareEntryData)
	}

	// Write projects back into the root map.
	projectsData, err := json.Marshal(projects)
	if err != nil {
		return stacktrace.Propagate(err, "failed to marshal projects map")
	}
	root["projects"] = json.RawMessage(projectsData)

	// Serialize the full file with indentation + trailing newline.
	result, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return stacktrace.Propagate(err, "failed to marshal root JSON")
	}
	result = append(result, '\n')

	return atomicWriteFile(claudeJSONFilepath, result)
}

// reconcileMissionTrust performs the boot-time trust migration + reconcile pass
// for all non-archived missions. It seeds a trust entry in the Claude-config
// .claude.json ($CLAUDE_CONFIG_DIR/.claude.json when set, else ~/.claude.json)
// for every existing mission's agent directory (so in-flight missions can
// respawn without a trust dialog after the State Y flip) and prunes stale
// entries left by archived or deleted missions.
//
// The entire pass is a single read-modify-write under s.claudeJSONMu to avoid
// N atomic renames on boot. Inert until Task 4 wires it into server startup.
//
//nolint:unused // wired at the State Y flip (Task 4)
func (s *Server) reconcileMissionTrust() error {
	claudeJSONPath, err := resolveClaudeJSONFilepath()
	if err != nil {
		return err
	}

	missions, err := s.db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions for trust reconcile")
	}

	agentDirs := make([]string, 0, len(missions))
	for _, m := range missions {
		agentDirs = append(agentDirs, config.GetMissionAgentDirpath(s.agencDirpath, m.ID))
	}

	missionsDirPrefix := config.GetMissionsDirpath(s.agencDirpath)

	s.claudeJSONMu.Lock()
	defer s.claudeJSONMu.Unlock()

	return reconcileTrustEntries(claudeJSONPath, agentDirs, missionsDirPrefix)
}
