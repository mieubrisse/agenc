# Claude Modifications Config API Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add server endpoints and CLI commands for reading/writing the claude-modifications CLAUDE.md and settings.json files with optimistic concurrency control.

**Architecture:** Server endpoints handle file I/O and git commits. CLI commands are thin clients. Content hash (SHA-256 of file bytes) prevents concurrent write conflicts. Each file has independent concurrency — changes to one don't affect the other.

**Tech Stack:** Go, Cobra CLI, HTTP server (existing unix socket pattern), crypto/sha256

---

### Task 1: Add Put method to server client

**Files:**
- Modify: `internal/server/client.go`

**Step 1: Write the Put method**

Add after the existing `Patch` method (around line 145):

```go
// Put sends a PUT request with a JSON body and decodes the response into result.
func (c *Client) Put(path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		pr, pw := io.Pipe()
		go func() {
			pw.CloseWithError(json.NewEncoder(pw).Encode(body))
		}()
		bodyReader = pr
	}

	req, err := http.NewRequest(http.MethodPut, c.baseURL+path, bodyReader)
	if err != nil {
		return stacktrace.Propagate(err, "failed to create request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return stacktrace.Propagate(err, "failed to connect to server")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.decodeError(resp)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return stacktrace.Propagate(err, "failed to decode server response")
		}
	}

	return nil
}
```

**Step 2: Build and verify it compiles**

Run: `make check`
Expected: PASS (no callers yet, just compiles)

**Step 3: Commit**

```
git add internal/server/client.go
git commit -m "Add Put method to server client"
```

---

### Task 2: Add server handlers for claude-md and settings-json

**Files:**
- Create: `internal/server/claude_modifications.go`
- Modify: `internal/server/server.go` (add routes in `registerRoutes`)

**Step 1: Create the handler file with request/response types and shared helpers**

Create `internal/server/claude_modifications.go`:

```go
package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/odyssey/agenc/internal/config"
)

const gitOperationTimeout = 30 * time.Second

// claudeModsFileResponse is the JSON response for GET /config/claude-md
// and GET /config/settings-json.
type claudeModsFileResponse struct {
	Content     string `json:"content"`
	ContentHash string `json:"contentHash"`
}

// claudeModsFileUpdateRequest is the JSON body for PUT /config/claude-md
// and PUT /config/settings-json.
type claudeModsFileUpdateRequest struct {
	Content      string `json:"content"`
	ExpectedHash string `json:"expectedHash"`
}

// claudeModsFileUpdateResponse is the JSON response for successful PUT operations.
type claudeModsFileUpdateResponse struct {
	ContentHash string `json:"contentHash"`
}

// computeContentHash returns the hex-encoded SHA-256 of the given bytes.
func computeContentHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// readClaudeModsFile reads a file from the claude-modifications directory and
// returns its content and SHA-256 content hash.
func (s *Server) readClaudeModsFile(filename string) (content []byte, contentHash string, err error) {
	modsDirpath := config.GetClaudeModificationsDirpath(s.agencDirpath)
	filepath := filepath.Join(modsDirpath, filename)

	data, err := os.ReadFile(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			// Empty file — return empty content with hash of empty bytes
			return []byte{}, computeContentHash([]byte{}), nil
		}
		return nil, "", fmt.Errorf("failed to read %s: %w", filename, err)
	}

	return data, computeContentHash(data), nil
}

// writeClaudeModsFile writes content to a file in the claude-modifications
// directory, validates the expected hash, and commits the change to the config
// repo. Returns the new content hash on success.
func (s *Server) writeClaudeModsFile(filename string, content []byte, expectedHash string) (string, error) {
	modsDirpath := config.GetClaudeModificationsDirpath(s.agencDirpath)
	targetFilepath := filepath.Join(modsDirpath, filename)

	// Read current file and validate hash
	currentData, err := os.ReadFile(targetFilepath)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to read current %s: %w", filename, err)
	}
	if os.IsNotExist(err) {
		currentData = []byte{}
	}

	currentHash := computeContentHash(currentData)
	if currentHash != expectedHash {
		return "", newHTTPError(http.StatusConflict,
			fmt.Sprintf("file has been modified since last read; run 'agenc config %s get' to fetch the current version, then retry your update",
				filenameToCmdName(filename)))
	}

	// Ensure the directory exists
	if err := os.MkdirAll(modsDirpath, 0755); err != nil {
		return "", fmt.Errorf("failed to create claude-modifications directory: %w", err)
	}

	// Write the file
	if err := os.WriteFile(targetFilepath, content, 0644); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", filename, err)
	}

	// Git add + commit in the config repo
	configDirpath := config.GetConfigDirpath(s.agencDirpath)
	if isGitRepo(configDirpath) {
		relPath := filepath.Join(config.ClaudeModificationsDirname, filename)
		if err := s.commitConfigFile(configDirpath, relPath, filename); err != nil {
			s.logger.Printf("Warning: failed to git commit %s: %v", filename, err)
			// Don't fail the request — the file was written successfully
		}
	}

	newHash := computeContentHash(content)
	return newHash, nil
}

// commitConfigFile stages and commits a single file in the config repo.
func (s *Server) commitConfigFile(configDirpath string, relFilepath string, displayName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), gitOperationTimeout)
	defer cancel()

	addCmd := exec.CommandContext(ctx, "git", "add", relFilepath)
	addCmd.Dir = configDirpath
	if output, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add failed: %w\n%s", err, strings.TrimSpace(string(output)))
	}

	commitMsg := fmt.Sprintf("Update claude-modifications/%s", displayName)
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", commitMsg)
	commitCmd.Dir = configDirpath
	if output, err := commitCmd.CombinedOutput(); err != nil {
		// "nothing to commit" is not an error
		if strings.Contains(string(output), "nothing to commit") {
			return nil
		}
		return fmt.Errorf("git commit failed: %w\n%s", err, strings.TrimSpace(string(output)))
	}

	s.logger.Printf("Committed config change: %s", commitMsg)
	return nil
}

// filenameToCmdName converts a filename to the CLI command name.
func filenameToCmdName(filename string) string {
	switch filename {
	case "CLAUDE.md":
		return "claude-md"
	case "settings.json":
		return "settings-json"
	default:
		return filename
	}
}
```

**Step 2: Add the GET handler for claude-md**

Append to `internal/server/claude_modifications.go`:

```go
// handleGetClaudeMd handles GET /config/claude-md.
func (s *Server) handleGetClaudeMd(w http.ResponseWriter, r *http.Request) error {
	content, contentHash, err := s.readClaudeModsFile("CLAUDE.md")
	if err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "%s", err.Error())
	}

	writeJSON(w, http.StatusOK, claudeModsFileResponse{
		Content:     string(content),
		ContentHash: contentHash,
	})
	return nil
}
```

**Step 3: Add the PUT handler for claude-md**

Append to `internal/server/claude_modifications.go`:

```go
// handleUpdateClaudeMd handles PUT /config/claude-md.
func (s *Server) handleUpdateClaudeMd(w http.ResponseWriter, r *http.Request) error {
	var req claudeModsFileUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
	}

	if req.ExpectedHash == "" {
		return newHTTPError(http.StatusBadRequest, "expectedHash is required")
	}

	newHash, err := s.writeClaudeModsFile("CLAUDE.md", []byte(req.Content), req.ExpectedHash)
	if err != nil {
		return err // writeClaudeModsFile returns httpError for conflicts
	}

	writeJSON(w, http.StatusOK, claudeModsFileUpdateResponse{
		ContentHash: newHash,
	})
	return nil
}
```

**Step 4: Add the GET and PUT handlers for settings-json**

Append to `internal/server/claude_modifications.go`:

```go
// handleGetSettingsJson handles GET /config/settings-json.
func (s *Server) handleGetSettingsJson(w http.ResponseWriter, r *http.Request) error {
	content, contentHash, err := s.readClaudeModsFile("settings.json")
	if err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "%s", err.Error())
	}

	writeJSON(w, http.StatusOK, claudeModsFileResponse{
		Content:     string(content),
		ContentHash: contentHash,
	})
	return nil
}

// handleUpdateSettingsJson handles PUT /config/settings-json.
func (s *Server) handleUpdateSettingsJson(w http.ResponseWriter, r *http.Request) error {
	var req claudeModsFileUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
	}

	if req.ExpectedHash == "" {
		return newHTTPError(http.StatusBadRequest, "expectedHash is required")
	}

	// Validate JSON before writing
	if !json.Valid([]byte(req.Content)) {
		return newHTTPError(http.StatusBadRequest, "content is not valid JSON")
	}

	newHash, err := s.writeClaudeModsFile("settings.json", []byte(req.Content), req.ExpectedHash)
	if err != nil {
		return err
	}

	writeJSON(w, http.StatusOK, claudeModsFileUpdateResponse{
		ContentHash: newHash,
	})
	return nil
}
```

**Step 5: Register routes in server.go**

Add these four lines at the end of `registerRoutes()` in `internal/server/server.go`:

```go
mux.Handle("GET /config/claude-md", appHandler(s.requestLogger, s.handleGetClaudeMd))
mux.Handle("PUT /config/claude-md", appHandler(s.requestLogger, s.handleUpdateClaudeMd))
mux.Handle("GET /config/settings-json", appHandler(s.requestLogger, s.handleGetSettingsJson))
mux.Handle("PUT /config/settings-json", appHandler(s.requestLogger, s.handleUpdateSettingsJson))
```

**Step 6: Build and verify**

Run: `make check`
Expected: PASS

**Step 7: Commit**

```
git add internal/server/claude_modifications.go internal/server/server.go
git commit -m "Add server endpoints for claude-modifications config files"
```

---

### Task 3: Add client methods for claude-modifications endpoints

**Files:**
- Modify: `internal/server/client.go`

**Step 1: Add high-level client methods**

Add after existing client methods:

```go
// GetClaudeMd reads the AgenC-specific CLAUDE.md content and its content hash.
func (c *Client) GetClaudeMd() (*ClaudeModsFileResponse, error) {
	var resp ClaudeModsFileResponse
	if err := c.Get("/config/claude-md", &resp); err != nil {
		return nil, stacktrace.Propagate(err, "failed to get claude-md")
	}
	return &resp, nil
}

// UpdateClaudeMd writes new content to the AgenC-specific CLAUDE.md.
// Returns the new content hash on success.
func (c *Client) UpdateClaudeMd(content string, expectedHash string) (*ClaudeModsFileUpdateResponse, error) {
	var resp ClaudeModsFileUpdateResponse
	req := ClaudeModsFileUpdateRequest{
		Content:      content,
		ExpectedHash: expectedHash,
	}
	if err := c.Put("/config/claude-md", req, &resp); err != nil {
		return nil, err // Preserve HTTP error for conflict detection
	}
	return &resp, nil
}

// GetSettingsJson reads the AgenC-specific settings.json content and its content hash.
func (c *Client) GetSettingsJson() (*ClaudeModsFileResponse, error) {
	var resp ClaudeModsFileResponse
	if err := c.Get("/config/settings-json", &resp); err != nil {
		return nil, stacktrace.Propagate(err, "failed to get settings-json")
	}
	return &resp, nil
}

// UpdateSettingsJson writes new content to the AgenC-specific settings.json.
// Returns the new content hash on success.
func (c *Client) UpdateSettingsJson(content string, expectedHash string) (*ClaudeModsFileUpdateResponse, error) {
	var resp ClaudeModsFileUpdateResponse
	req := ClaudeModsFileUpdateRequest{
		Content:      content,
		ExpectedHash: expectedHash,
	}
	if err := c.Put("/config/settings-json", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
```

**Step 2: Export the request/response types from client.go**

Add at the top of `client.go` (or in `claude_modifications.go` — these types are shared):

Actually, the types are already defined in `claude_modifications.go` with lowercase names (server-internal). For the client, export capitalized versions in `client.go`:

```go
// ClaudeModsFileResponse is the response from GET /config/claude-md
// and GET /config/settings-json.
type ClaudeModsFileResponse = claudeModsFileResponse

// ClaudeModsFileUpdateRequest is the request body for PUT /config/claude-md
// and PUT /config/settings-json.
type ClaudeModsFileUpdateRequest = claudeModsFileUpdateRequest

// ClaudeModsFileUpdateResponse is the response from successful PUT operations.
type ClaudeModsFileUpdateResponse = claudeModsFileUpdateResponse
```

**Step 3: Build and verify**

Run: `make check`
Expected: PASS

**Step 4: Commit**

```
git add internal/server/client.go
git commit -m "Add client methods for claude-modifications endpoints"
```

---

### Task 4: Add CLI commands for claude-md get/set

**Files:**
- Create: `cmd/config_claude_md.go`
- Create: `cmd/config_claude_md_get.go`
- Create: `cmd/config_claude_md_set.go`
- Modify: `cmd/command_str_consts.go`

**Step 1: Add command name and flag constants**

In `cmd/command_str_consts.go`, add to the config subcommands section:

```go
claudeMdCmdStr     = "claude-md"
settingsJsonCmdStr = "settings-json"
```

And add to the flag name constants section:

```go
contentHashFlagName = "content-hash"
```

**Step 2: Create the parent command**

Create `cmd/config_claude_md.go`:

```go
package cmd

import (
	"github.com/spf13/cobra"
)

var configClaudeMdCmd = &cobra.Command{
	Use:   claudeMdCmdStr,
	Short: "Manage AgenC-specific CLAUDE.md instructions",
	Long: `Read and write the AgenC-specific CLAUDE.md that gets merged into every mission's config.

This file contains instructions that apply to all AgenC missions but not to
Claude Code sessions outside of AgenC. Content is appended after the user's
~/.claude/CLAUDE.md when building per-mission config.

Changes take effect for new missions automatically. Use 'agenc mission reconfig'
to propagate changes to existing missions.`,
}

func init() {
	configCmd.AddCommand(configClaudeMdCmd)
}
```

**Step 3: Create the get command**

Create `cmd/config_claude_md_get.go`:

```go
package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var configClaudeMdGetCmd = &cobra.Command{
	Use:   getCmdStr,
	Short: "Print the AgenC-specific CLAUDE.md content",
	RunE:  runConfigClaudeMdGet,
}

func init() {
	configClaudeMdCmd.AddCommand(configClaudeMdGetCmd)
}

func runConfigClaudeMdGet(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	resp, err := client.GetClaudeMd()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get CLAUDE.md")
	}

	fmt.Printf("Content-Hash: %s\n\n--- Content ---\n%s", resp.ContentHash, resp.Content)
	return nil
}
```

**Step 4: Create the set command**

Create `cmd/config_claude_md_set.go`:

```go
package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var configClaudeMdSetCmd = &cobra.Command{
	Use:   setCmdStr,
	Short: "Update the AgenC-specific CLAUDE.md content",
	Long: `Update the AgenC-specific CLAUDE.md content. Reads new content from stdin.

Requires --content-hash from a previous 'get' to prevent overwriting concurrent
changes. If the file was modified since your last read, the update is rejected
and you must re-read before retrying.

Example:
  agenc config claude-md get                                    # note the Content-Hash
  echo "New instructions" | agenc config claude-md set --content-hash=abc123`,
	RunE: runConfigClaudeMdSet,
}

func init() {
	configClaudeMdCmd.AddCommand(configClaudeMdSetCmd)
	configClaudeMdSetCmd.Flags().String(contentHashFlagName, "", "content hash from the last get (required)")
	_ = configClaudeMdSetCmd.MarkFlagRequired(contentHashFlagName)
}

func runConfigClaudeMdSet(cmd *cobra.Command, args []string) error {
	contentHash, err := cmd.Flags().GetString(contentHashFlagName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read --%s flag", contentHashFlagName)
	}

	content, err := io.ReadAll(os.Stdin)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read content from stdin")
	}

	client, err := serverClient()
	if err != nil {
		return err
	}

	resp, err := client.UpdateClaudeMd(string(content), contentHash)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "modified since last read") {
			fmt.Fprintln(os.Stderr, "Error: CLAUDE.md has been modified since last read.")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "To resolve:")
			fmt.Fprintf(os.Stderr, "  1. agenc config %s %s    (fetch current content and hash)\n", claudeMdCmdStr, getCmdStr)
			fmt.Fprintln(os.Stderr, "  2. Re-apply your changes to the new content")
			fmt.Fprintf(os.Stderr, "  3. agenc config %s %s --content-hash=<new-hash>\n", claudeMdCmdStr, setCmdStr)
			return stacktrace.NewError("CLAUDE.md has been modified since last read")
		}
		return stacktrace.Propagate(err, "failed to update CLAUDE.md")
	}

	fmt.Printf("Updated CLAUDE.md (content hash: %s)\n", resp.ContentHash)
	return nil
}
```

**Step 5: Build and verify**

Run: `make check`
Expected: PASS

**Step 6: Commit**

```
git add cmd/config_claude_md.go cmd/config_claude_md_get.go cmd/config_claude_md_set.go cmd/command_str_consts.go
git commit -m "Add CLI commands for agenc config claude-md get/set"
```

---

### Task 5: Add CLI commands for settings-json get/set

**Files:**
- Create: `cmd/config_settings_json.go`
- Create: `cmd/config_settings_json_get.go`
- Create: `cmd/config_settings_json_set.go`

**Step 1: Create the parent command**

Create `cmd/config_settings_json.go`:

```go
package cmd

import (
	"github.com/spf13/cobra"
)

var configSettingsJsonCmd = &cobra.Command{
	Use:   settingsJsonCmdStr,
	Short: "Manage AgenC-specific settings.json overrides",
	Long: `Read and write the AgenC-specific settings.json that gets merged into every mission's config.

This file contains settings overrides that apply to all AgenC missions but not
to Claude Code sessions outside of AgenC. Settings are deep-merged over the
user's ~/.claude/settings.json when building per-mission config (objects merge
recursively, arrays are concatenated, scalars from this file win).

Changes take effect for new missions automatically. Use 'agenc mission reconfig'
to propagate changes to existing missions.`,
}

func init() {
	configCmd.AddCommand(configSettingsJsonCmd)
}
```

**Step 2: Create the get command**

Create `cmd/config_settings_json_get.go`:

```go
package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var configSettingsJsonGetCmd = &cobra.Command{
	Use:   getCmdStr,
	Short: "Print the AgenC-specific settings.json content",
	RunE:  runConfigSettingsJsonGet,
}

func init() {
	configSettingsJsonCmd.AddCommand(configSettingsJsonGetCmd)
}

func runConfigSettingsJsonGet(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	resp, err := client.GetSettingsJson()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get settings.json")
	}

	fmt.Printf("Content-Hash: %s\n\n--- Content ---\n%s", resp.ContentHash, resp.Content)
	return nil
}
```

**Step 3: Create the set command**

Create `cmd/config_settings_json_set.go`:

```go
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var configSettingsJsonSetCmd = &cobra.Command{
	Use:   setCmdStr,
	Short: "Update the AgenC-specific settings.json content",
	Long: `Update the AgenC-specific settings.json content. Reads new content from stdin.

Content must be valid JSON. Requires --content-hash from a previous 'get' to
prevent overwriting concurrent changes.

Example:
  agenc config settings-json get                                         # note the Content-Hash
  echo '{"permissions":{"allow":["Bash(npm:*)"]}}' | agenc config settings-json set --content-hash=abc123`,
	RunE: runConfigSettingsJsonSet,
}

func init() {
	configSettingsJsonCmd.AddCommand(configSettingsJsonSetCmd)
	configSettingsJsonSetCmd.Flags().String(contentHashFlagName, "", "content hash from the last get (required)")
	_ = configSettingsJsonSetCmd.MarkFlagRequired(contentHashFlagName)
}

func runConfigSettingsJsonSet(cmd *cobra.Command, args []string) error {
	contentHash, err := cmd.Flags().GetString(contentHashFlagName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read --%s flag", contentHashFlagName)
	}

	content, err := io.ReadAll(os.Stdin)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read content from stdin")
	}

	// Validate JSON client-side for fast feedback
	if !json.Valid(content) {
		return stacktrace.NewError("content is not valid JSON")
	}

	client, err := serverClient()
	if err != nil {
		return err
	}

	resp, err := client.UpdateSettingsJson(string(content), contentHash)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "modified since last read") {
			fmt.Fprintln(os.Stderr, "Error: settings.json has been modified since last read.")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "To resolve:")
			fmt.Fprintf(os.Stderr, "  1. agenc config %s %s    (fetch current content and hash)\n", settingsJsonCmdStr, getCmdStr)
			fmt.Fprintln(os.Stderr, "  2. Re-apply your changes to the new content")
			fmt.Fprintf(os.Stderr, "  3. agenc config %s %s --content-hash=<new-hash>\n", settingsJsonCmdStr, setCmdStr)
			return stacktrace.NewError("settings.json has been modified since last read")
		}
		return stacktrace.Propagate(err, "failed to update settings.json")
	}

	fmt.Printf("Updated settings.json (content hash: %s)\n", resp.ContentHash)
	return nil
}
```

**Step 4: Build and verify**

Run: `make check`
Expected: PASS

**Step 5: Commit**

```
git add cmd/config_settings_json.go cmd/config_settings_json_get.go cmd/config_settings_json_set.go
git commit -m "Add CLI commands for agenc config settings-json get/set"
```

---

### Task 6: Update Adjutant prompt

**Files:**
- Modify: `internal/claudeconfig/adjutant_claude.md`

**Step 1: Replace the "Claude Modifications Directory" section**

Replace lines 141-167 (the entire "Claude Modifications Directory" section) with:

```markdown
AgenC-Specific Claude Instructions and Settings
------------------------------------------------

AgenC maintains its own CLAUDE.md and settings.json that get merged into every mission's Claude config. These are separate from the user's `~/.claude/` config — they apply only within AgenC missions.

- **CLAUDE.md** — instructions appended after the user's `~/.claude/CLAUDE.md`
- **settings.json** — settings deep-merged over the user's `~/.claude/settings.json` (objects merge recursively, arrays concatenate, scalars from this file win)

**Reading and writing these files:**

```bash
# Read the current CLAUDE.md (prints content hash + content)
agenc config claude-md get

# Update CLAUDE.md (reads new content from stdin, requires content hash)
echo "New instructions here" | agenc config claude-md set --content-hash=<hash-from-get>

# Read the current settings.json
agenc config settings-json get

# Update settings.json (must be valid JSON)
echo '{"permissions":{"allow":["Bash(npm:*)"]}}' | agenc config settings-json set --content-hash=<hash-from-get>
```

**Content hash flow:** The `get` command returns a `Content-Hash` header. The `set` command requires `--content-hash` matching the version you last read. If the file was modified by another agent since your read, the update is rejected and you must re-read before retrying.

**When changes take effect:** New missions pick up changes automatically. Existing missions keep their config snapshot from creation time. To propagate changes to existing missions, run `agenc mission reconfig`. Running missions must be restarted after reconfig.

**Do NOT edit the underlying files directly** — always use the `agenc config claude-md` and `agenc config settings-json` commands.
```

**Step 2: Update the "What You Help With" section**

Add this line to the bullet list (after the "Configuring AgenC" line):

```markdown
- Managing AgenC-specific Claude instructions and settings (`agenc config claude-md`, `agenc config settings-json`)
```

**Step 3: Build and verify**

Run: `make check`
Expected: PASS (genprime regenerates with new commands)

**Step 4: Commit**

```
git add internal/claudeconfig/adjutant_claude.md
git commit -m "Update Adjutant prompt to use claude-md and settings-json commands"
```

---

### Task 7: Build, restart server, and end-to-end test

**Step 1: Build the full binary**

Run: `make build`
Expected: PASS

**Step 2: Restart the server**

```
agenc server stop
agenc server start
```

**Step 3: Test claude-md get**

Run: `agenc config claude-md get`
Expected: Output with `Content-Hash:` line and `--- Content ---` separator, followed by the file content (may be empty).

**Step 4: Test claude-md set**

Run: `echo "Test instruction" | agenc config claude-md set --content-hash=<hash-from-step-3>`
Expected: `Updated CLAUDE.md (content hash: <new-hash>)`

**Step 5: Verify the write persisted**

Run: `agenc config claude-md get`
Expected: Content contains "Test instruction", hash matches what was returned in step 4.

**Step 6: Test conflict detection**

Run: `echo "Conflicting change" | agenc config claude-md set --content-hash=stale-hash-here`
Expected: Error message with resolution steps.

**Step 7: Test settings-json get/set**

Run: `agenc config settings-json get`
Then: `echo '{}' | agenc config settings-json set --content-hash=<hash>`
Expected: Both succeed.

**Step 8: Test settings-json JSON validation**

Run: `echo "not json" | agenc config settings-json set --content-hash=<hash>`
Expected: Error about invalid JSON.

**Step 9: Restore original claude-md content**

Re-read and restore whatever the original CLAUDE.md content was before testing.

**Step 10: Commit and push**

```
git add .
git commit -m "Build with claude-modifications config API"
git push
```
