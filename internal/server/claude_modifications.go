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

// claudeModsFileResponse is the JSON response returned by GET handlers for
// claude-modifications files.
type claudeModsFileResponse struct {
	Content     string `json:"content"`
	ContentHash string `json:"contentHash"`
}

// claudeModsFileUpdateRequest is the JSON request body for PUT handlers that
// update claude-modifications files.
type claudeModsFileUpdateRequest struct {
	Content      string `json:"content"`
	ExpectedHash string `json:"expectedHash"`
}

// claudeModsFileUpdateResponse is the JSON response returned after a
// successful PUT to a claude-modifications file.
type claudeModsFileUpdateResponse struct {
	ContentHash string `json:"contentHash"`
}

// computeContentHash returns the hex-encoded SHA-256 hash of data.
func computeContentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// readClaudeModsFile reads a file from the claude-modifications directory and
// returns its content bytes and content hash. If the file does not exist, it
// returns empty bytes with the hash of empty bytes.
func (s *Server) readClaudeModsFile(filename string) ([]byte, string, error) {
	modsDirpath := config.GetClaudeModificationsDirpath(s.agencDirpath)
	filePath := filepath.Join(modsDirpath, filename)

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			emptyData := []byte{}
			return emptyData, computeContentHash(emptyData), nil
		}
		return nil, "", fmt.Errorf("failed to read %s: %w", filename, err)
	}

	return data, computeContentHash(data), nil
}

// writeClaudeModsFile validates that expectedHash matches the current file
// content, writes new content, and commits the change in the config repo.
// Returns the new content hash. If the hash does not match, returns a 409 error.
func (s *Server) writeClaudeModsFile(filename, content, expectedHash string) (string, error) {
	modsDirpath := config.GetClaudeModificationsDirpath(s.agencDirpath)
	filePath := filepath.Join(modsDirpath, filename)

	// Read current file to verify hash
	currentData, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			currentData = []byte{}
		} else {
			return "", fmt.Errorf("failed to read current %s: %w", filename, err)
		}
	}

	currentHash := computeContentHash(currentData)
	if currentHash != expectedHash {
		cmdName := filenameToCmdName(filename)
		return "", newHTTPErrorf(
			http.StatusConflict,
			"file has been modified since last read; run 'agenc config %s get' to fetch the current version, then retry your update",
			cmdName,
		)
	}

	// Ensure the directory exists
	if err := os.MkdirAll(modsDirpath, 0755); err != nil {
		return "", fmt.Errorf("failed to create claude-modifications directory: %w", err)
	}

	// Write the new content
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", filename, err)
	}

	newHash := computeContentHash([]byte(content))

	// Attempt to commit — non-fatal if it fails
	configDirpath := config.GetConfigDirpath(s.agencDirpath)
	relFilepath := filepath.Join(config.ClaudeModificationsDirname, filename)
	displayName := filename
	if err := commitConfigFile(configDirpath, relFilepath, displayName, s.logger); err != nil {
		s.logger.Printf("Warning: failed to commit %s: %v", filename, err)
	}

	return newHash, nil
}

// commitConfigFile stages and commits a single file within the config repo.
// It is a no-op if the config directory is not a git repo. "Nothing to commit"
// is handled gracefully.
func commitConfigFile(configDirpath, relFilepath, displayName string, logger interface{ Printf(string, ...any) }) error {
	if !isGitRepo(configDirpath) {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stage the file
	addCmd := exec.CommandContext(ctx, "git", "add", relFilepath)
	addCmd.Dir = configDirpath
	if output, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add failed: %v\n%s", err, strings.TrimSpace(string(output)))
	}

	// Commit
	commitMsg := fmt.Sprintf("Update claude-modifications/%s", displayName)
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", commitMsg)
	commitCmd.Dir = configDirpath
	output, err := commitCmd.CombinedOutput()
	if err != nil {
		outputStr := strings.TrimSpace(string(output))
		// "nothing to commit" is not a real error — the file was already
		// committed or content didn't change on disk.
		if strings.Contains(outputStr, "nothing to commit") {
			return nil
		}
		return fmt.Errorf("git commit failed: %v\n%s", err, outputStr)
	}

	logger.Printf("Committed config file: %s", commitMsg)
	return nil
}

// filenameToCmdName maps a claude-modifications filename to the corresponding
// CLI subcommand name.
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

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (s *Server) handleGetClaudeMd(w http.ResponseWriter, r *http.Request) error {
	data, hash, err := s.readClaudeModsFile("CLAUDE.md")
	if err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "%v", err)
	}
	writeJSON(w, http.StatusOK, claudeModsFileResponse{
		Content:     string(data),
		ContentHash: hash,
	})
	return nil
}

func (s *Server) handleUpdateClaudeMd(w http.ResponseWriter, r *http.Request) error {
	var req claudeModsFileUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid JSON request body")
	}

	if req.ExpectedHash == "" {
		return newHTTPError(http.StatusBadRequest, "expectedHash is required")
	}

	newHash, err := s.writeClaudeModsFile("CLAUDE.md", req.Content, req.ExpectedHash)
	if err != nil {
		return err
	}

	writeJSON(w, http.StatusOK, claudeModsFileUpdateResponse{ContentHash: newHash})
	return nil
}

func (s *Server) handleGetSettingsJson(w http.ResponseWriter, r *http.Request) error {
	data, hash, err := s.readClaudeModsFile("settings.json")
	if err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "%v", err)
	}
	writeJSON(w, http.StatusOK, claudeModsFileResponse{
		Content:     string(data),
		ContentHash: hash,
	})
	return nil
}

func (s *Server) handleUpdateSettingsJson(w http.ResponseWriter, r *http.Request) error {
	var req claudeModsFileUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid JSON request body")
	}

	if req.ExpectedHash == "" {
		return newHTTPError(http.StatusBadRequest, "expectedHash is required")
	}

	// Validate that the content is valid JSON
	if !json.Valid([]byte(req.Content)) {
		return newHTTPError(http.StatusBadRequest, "content is not valid JSON")
	}

	newHash, err := s.writeClaudeModsFile("settings.json", req.Content, req.ExpectedHash)
	if err != nil {
		return err
	}

	writeJSON(w, http.StatusOK, claudeModsFileUpdateResponse{ContentHash: newHash})
	return nil
}
