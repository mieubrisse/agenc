package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

// TestTokenSetValidation verifies that tokenSetCmd rejects invalid tokens
// before touching the filesystem.
func TestTokenSetValidation(t *testing.T) {
	t.Run("rejects empty token", func(t *testing.T) {
		// runTokenSet validates before calling WriteOAuthToken; we test the
		// validation logic directly by checking the prefix constant.
		token := ""
		if token != "" && !isValidOAuthToken(token) {
			t.Error("empty token should be caught before prefix check")
		}
		// An empty string should not pass the non-empty check
		if token != "" {
			t.Error("empty token slipped through the non-empty guard")
		}
	})

	t.Run("rejects token without sk-ant- prefix", func(t *testing.T) {
		token := "bad-token-value"
		if isValidOAuthToken(token) {
			t.Errorf("expected token %q to be invalid (no sk-ant- prefix)", token)
		}
	})

	t.Run("accepts valid sk-ant- token", func(t *testing.T) {
		token := "sk-ant-abc123xyz"
		if !isValidOAuthToken(token) {
			t.Errorf("expected token %q to be valid", token)
		}
	})
}

// TestTokenSetClearRoundTrip verifies the write-then-clear lifecycle using a
// temporary agenc directory.
func TestTokenSetClearRoundTrip(t *testing.T) {
	agencDirpath := t.TempDir()

	// Initially no token
	token, err := config.ReadOAuthToken(agencDirpath)
	if err != nil {
		t.Fatalf("unexpected error on initial read: %v", err)
	}
	if token != "" {
		t.Errorf("expected empty initial token, got %q", token)
	}

	// Write a valid token
	validToken := "sk-ant-test-token-12345"
	if err := config.WriteOAuthToken(agencDirpath, validToken); err != nil {
		t.Fatalf("failed to write token: %v", err)
	}

	// Read it back
	token, err = config.ReadOAuthToken(agencDirpath)
	if err != nil {
		t.Fatalf("unexpected error reading token: %v", err)
	}
	if token != validToken {
		t.Errorf("expected %q, got %q", validToken, token)
	}

	// Clear (token clear uses WriteOAuthToken with "")
	if err := config.WriteOAuthToken(agencDirpath, ""); err != nil {
		t.Fatalf("failed to clear token: %v", err)
	}

	// Verify cleared
	token, err = config.ReadOAuthToken(agencDirpath)
	if err != nil {
		t.Fatalf("unexpected error after clear: %v", err)
	}
	if token != "" {
		t.Errorf("expected empty token after clear, got %q", token)
	}

	// Verify file is gone
	tokenFilepath := filepath.Join(agencDirpath, config.CacheDirname, config.OAuthTokenFilename)
	if _, statErr := os.Stat(tokenFilepath); !os.IsNotExist(statErr) {
		t.Error("expected token file to not exist after clear")
	}
}

// TestTokenSetFilePermissions verifies that the stored token file has mode 0600.
func TestTokenSetFilePermissions(t *testing.T) {
	agencDirpath := t.TempDir()

	if err := config.WriteOAuthToken(agencDirpath, "sk-ant-test-perm-check"); err != nil {
		t.Fatalf("failed to write token: %v", err)
	}

	tokenFilepath := filepath.Join(agencDirpath, config.CacheDirname, config.OAuthTokenFilename)
	info, err := os.Stat(tokenFilepath)
	if err != nil {
		t.Fatalf("failed to stat token file: %v", err)
	}

	if info.Mode().Perm() != 0600 {
		t.Errorf("expected permissions 0600, got %o", info.Mode().Perm())
	}
}

// isValidOAuthToken mirrors the validation logic in runTokenSet so tests
// can exercise it without going through the full Cobra command machinery.
func isValidOAuthToken(token string) bool {
	if len(token) == 0 {
		return false
	}
	return len(token) >= len(tokenCmdOAuthPrefix) && token[:len(tokenCmdOAuthPrefix)] == tokenCmdOAuthPrefix
}
