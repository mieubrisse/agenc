package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

// TestValidateOAuthToken exercises the production validateOAuthToken function
// directly (the same function runTokenSet calls). Because both the production
// path and these tests call the one function, the tests cannot silently
// diverge from production validation.
func TestValidateOAuthToken(t *testing.T) {
	t.Run("rejects empty token with the empty-specific message", func(t *testing.T) {
		// The empty-token guard produces a distinct, more helpful message than
		// the prefix check (which would ALSO reject "" since HasPrefix("", …) is
		// false). Asserting on the empty-specific message means this test fails
		// if the empty-token guard is removed — not silently absorbed by the
		// prefix branch.
		err := validateOAuthToken("")
		if err == nil {
			t.Fatal("expected empty token to be rejected, got nil error")
		}
		if !strings.Contains(err.Error(), "cannot be empty") {
			t.Errorf("expected empty-specific error message, got: %v", err)
		}
	})

	t.Run("rejects whitespace-only token (trimmed to empty)", func(t *testing.T) {
		// runTokenSet trims before validating, so a whitespace-only arg reaches
		// validateOAuthToken as "". Assert the empty-token guard fires.
		err := validateOAuthToken(strings.TrimSpace("   "))
		if err == nil {
			t.Fatal("expected whitespace-only token to be rejected, got nil error")
		}
		if !strings.Contains(err.Error(), "cannot be empty") {
			t.Errorf("expected empty-specific error message, got: %v", err)
		}
	})

	t.Run("rejects token without sk-ant- prefix", func(t *testing.T) {
		if err := validateOAuthToken("bad-token-value"); err == nil {
			t.Error("expected token without sk-ant- prefix to be rejected, got nil error")
		}
	})

	t.Run("accepts valid sk-ant- token", func(t *testing.T) {
		if err := validateOAuthToken("sk-ant-abc123xyz"); err != nil {
			t.Errorf("expected valid token to pass, got error: %v", err)
		}
	})
}

// TestRunTokenSetRejectsInvalid drives runTokenSet through its real cobra
// command with invalid args, so the empty-token and bad-prefix guards are
// genuinely exercised end-to-end (not just the extracted validator). Uses an
// isolated AGENC_DIRPATH so no real config is touched.
func TestRunTokenSetRejectsInvalid(t *testing.T) {
	t.Setenv("AGENC_DIRPATH", t.TempDir())

	t.Run("empty-string arg is rejected", func(t *testing.T) {
		err := runTokenSet(tokenSetCmd, []string{""})
		if err == nil {
			t.Error("expected runTokenSet to reject empty-string token, got nil error")
		}
	})

	t.Run("whitespace-only arg is rejected", func(t *testing.T) {
		err := runTokenSet(tokenSetCmd, []string{"   "})
		if err == nil {
			t.Error("expected runTokenSet to reject whitespace-only token, got nil error")
		}
	})

	t.Run("bad-prefix arg is rejected", func(t *testing.T) {
		err := runTokenSet(tokenSetCmd, []string{"not-a-real-token"})
		if err == nil {
			t.Error("expected runTokenSet to reject bad-prefix token, got nil error")
		}
	})
}

// TestRunTokenSetAcceptsValid drives runTokenSet with a valid token through its
// real cobra command and confirms the token lands on disk. This proves the
// happy path all the way through validation + write.
func TestRunTokenSetAcceptsValid(t *testing.T) {
	agencDirpath := t.TempDir()
	t.Setenv("AGENC_DIRPATH", agencDirpath)

	validToken := "sk-ant-run-token-set-happy-path"
	if err := runTokenSet(tokenSetCmd, []string{validToken}); err != nil {
		t.Fatalf("expected runTokenSet to accept valid token, got error: %v", err)
	}

	stored, err := config.ReadOAuthToken(agencDirpath)
	if err != nil {
		t.Fatalf("unexpected error reading stored token: %v", err)
	}
	if stored != validToken {
		t.Errorf("expected stored token %q, got %q", validToken, stored)
	}
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
