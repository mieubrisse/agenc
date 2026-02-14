package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsMissionAdjutant(t *testing.T) {
	t.Run("returns false without marker file", func(t *testing.T) {
		agencDirpath := t.TempDir()
		missionID := "test-mission-id"

		// Create mission directory but no marker file
		missionDirpath := filepath.Join(agencDirpath, MissionsDirname, missionID)
		if err := os.MkdirAll(missionDirpath, 0755); err != nil {
			t.Fatalf("failed to create mission dir: %v", err)
		}

		if IsMissionAdjutant(agencDirpath, missionID) {
			t.Error("expected false when marker file does not exist")
		}
	})

	t.Run("returns true with marker file", func(t *testing.T) {
		agencDirpath := t.TempDir()
		missionID := "test-mission-id"

		// Create mission directory with marker file
		missionDirpath := filepath.Join(agencDirpath, MissionsDirname, missionID)
		if err := os.MkdirAll(missionDirpath, 0755); err != nil {
			t.Fatalf("failed to create mission dir: %v", err)
		}

		markerFilepath := filepath.Join(missionDirpath, AdjutantMarkerFilename)
		if err := os.WriteFile(markerFilepath, []byte{}, 0644); err != nil {
			t.Fatalf("failed to write marker file: %v", err)
		}

		if !IsMissionAdjutant(agencDirpath, missionID) {
			t.Error("expected true when marker file exists")
		}
	})

	t.Run("returns false when mission dir does not exist", func(t *testing.T) {
		agencDirpath := t.TempDir()
		missionID := "nonexistent-mission"

		if IsMissionAdjutant(agencDirpath, missionID) {
			t.Error("expected false when mission directory does not exist")
		}
	})
}

func TestReadOAuthToken(t *testing.T) {
	t.Run("returns empty string when file does not exist", func(t *testing.T) {
		agencDirpath := t.TempDir()

		token, err := ReadOAuthToken(agencDirpath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "" {
			t.Errorf("expected empty string, got %q", token)
		}
	})

	t.Run("reads token from file", func(t *testing.T) {
		agencDirpath := t.TempDir()
		cacheDirpath := filepath.Join(agencDirpath, CacheDirname)
		if err := os.MkdirAll(cacheDirpath, 0755); err != nil {
			t.Fatalf("failed to create cache dir: %v", err)
		}

		tokenFilepath := filepath.Join(cacheDirpath, OAuthTokenFilename)
		if err := os.WriteFile(tokenFilepath, []byte("my-test-token\n"), 0600); err != nil {
			t.Fatalf("failed to write token file: %v", err)
		}

		token, err := ReadOAuthToken(agencDirpath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "my-test-token" {
			t.Errorf("expected %q, got %q", "my-test-token", token)
		}
	})

	t.Run("trims whitespace from token", func(t *testing.T) {
		agencDirpath := t.TempDir()
		cacheDirpath := filepath.Join(agencDirpath, CacheDirname)
		if err := os.MkdirAll(cacheDirpath, 0755); err != nil {
			t.Fatalf("failed to create cache dir: %v", err)
		}

		tokenFilepath := filepath.Join(cacheDirpath, OAuthTokenFilename)
		if err := os.WriteFile(tokenFilepath, []byte("  spaced-token  \n"), 0600); err != nil {
			t.Fatalf("failed to write token file: %v", err)
		}

		token, err := ReadOAuthToken(agencDirpath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "spaced-token" {
			t.Errorf("expected %q, got %q", "spaced-token", token)
		}
	})
}

func TestWriteOAuthToken(t *testing.T) {
	t.Run("creates file with 600 permissions", func(t *testing.T) {
		agencDirpath := t.TempDir()
		cacheDirpath := filepath.Join(agencDirpath, CacheDirname)
		if err := os.MkdirAll(cacheDirpath, 0755); err != nil {
			t.Fatalf("failed to create cache dir: %v", err)
		}

		if err := WriteOAuthToken(agencDirpath, "test-token"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		tokenFilepath := filepath.Join(cacheDirpath, OAuthTokenFilename)
		info, err := os.Stat(tokenFilepath)
		if err != nil {
			t.Fatalf("token file not found: %v", err)
		}

		if info.Mode().Perm() != 0600 {
			t.Errorf("expected permissions 0600, got %o", info.Mode().Perm())
		}

		data, err := os.ReadFile(tokenFilepath)
		if err != nil {
			t.Fatalf("failed to read token file: %v", err)
		}
		if string(data) != "test-token\n" {
			t.Errorf("expected %q, got %q", "test-token\n", string(data))
		}
	})

	t.Run("creates cache directory if missing", func(t *testing.T) {
		agencDirpath := t.TempDir()

		if err := WriteOAuthToken(agencDirpath, "test-token"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		tokenFilepath := filepath.Join(agencDirpath, CacheDirname, OAuthTokenFilename)
		if _, err := os.Stat(tokenFilepath); os.IsNotExist(err) {
			t.Error("token file was not created")
		}
	})

	t.Run("deletes file when token is empty", func(t *testing.T) {
		agencDirpath := t.TempDir()
		cacheDirpath := filepath.Join(agencDirpath, CacheDirname)
		if err := os.MkdirAll(cacheDirpath, 0755); err != nil {
			t.Fatalf("failed to create cache dir: %v", err)
		}

		tokenFilepath := filepath.Join(cacheDirpath, OAuthTokenFilename)
		if err := os.WriteFile(tokenFilepath, []byte("old-token\n"), 0600); err != nil {
			t.Fatalf("failed to write token file: %v", err)
		}

		if err := WriteOAuthToken(agencDirpath, ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := os.Stat(tokenFilepath); !os.IsNotExist(err) {
			t.Error("expected token file to be deleted")
		}
	})

	t.Run("no error when deleting nonexistent file", func(t *testing.T) {
		agencDirpath := t.TempDir()

		if err := WriteOAuthToken(agencDirpath, ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestReadWriteOAuthTokenRoundTrip(t *testing.T) {
	agencDirpath := t.TempDir()

	// Initially empty
	token, err := ReadOAuthToken(agencDirpath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}

	// Write token
	if err := WriteOAuthToken(agencDirpath, "round-trip-token"); err != nil {
		t.Fatalf("unexpected error writing: %v", err)
	}

	// Read back
	token, err = ReadOAuthToken(agencDirpath)
	if err != nil {
		t.Fatalf("unexpected error reading: %v", err)
	}
	if token != "round-trip-token" {
		t.Errorf("expected %q, got %q", "round-trip-token", token)
	}

	// Clear
	if err := WriteOAuthToken(agencDirpath, ""); err != nil {
		t.Fatalf("unexpected error clearing: %v", err)
	}

	// Read back empty
	token, err = ReadOAuthToken(agencDirpath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "" {
		t.Errorf("expected empty token after clear, got %q", token)
	}
}

func TestGetMissionAdjutantMarkerFilepath(t *testing.T) {
	agencDirpath := "/home/user/.agenc"
	missionID := "abc-123"

	result := GetMissionAdjutantMarkerFilepath(agencDirpath, missionID)
	expected := filepath.Join(agencDirpath, MissionsDirname, missionID, AdjutantMarkerFilename)

	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}
