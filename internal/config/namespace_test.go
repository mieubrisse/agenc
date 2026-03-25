package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetNamespaceSuffix(t *testing.T) {
	t.Run("returns empty string for default agenc path", func(t *testing.T) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("failed to get home dir: %v", err)
		}
		defaultPath := filepath.Join(homeDir, ".agenc")
		suffix := GetNamespaceSuffix(defaultPath)
		if suffix != "" {
			t.Errorf("expected empty suffix for default path, got %q", suffix)
		}
	})

	t.Run("returns hash suffix for non-default path", func(t *testing.T) {
		suffix := GetNamespaceSuffix("/tmp/test-agenc-12345")
		if suffix == "" {
			t.Error("expected non-empty suffix for non-default path")
		}
		if suffix[0] != '-' {
			t.Errorf("expected suffix to start with '-', got %q", suffix)
		}
		// "-" + 8 hex chars = 9 chars total
		if len(suffix) != 9 {
			t.Errorf("expected suffix length 9, got %d: %q", len(suffix), suffix)
		}
	})

	t.Run("is deterministic", func(t *testing.T) {
		path := "/tmp/test-agenc-deterministic"
		s1 := GetNamespaceSuffix(path)
		s2 := GetNamespaceSuffix(path)
		if s1 != s2 {
			t.Errorf("non-deterministic: %q != %q", s1, s2)
		}
	})

	t.Run("different paths produce different suffixes", func(t *testing.T) {
		s1 := GetNamespaceSuffix("/tmp/agenc-aaa")
		s2 := GetNamespaceSuffix("/tmp/agenc-bbb")
		if s1 == s2 {
			t.Errorf("different paths produced same suffix: %q", s1)
		}
	})
}

func TestGetTmuxSessionName(t *testing.T) {
	t.Run("default path returns agenc", func(t *testing.T) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("failed to get home dir: %v", err)
		}
		got := GetTmuxSessionName(filepath.Join(homeDir, ".agenc"))
		if got != "agenc" {
			t.Errorf("expected 'agenc', got %q", got)
		}
	})

	t.Run("custom path returns agenc-HASH", func(t *testing.T) {
		got := GetTmuxSessionName("/tmp/test-agenc")
		if got == "agenc" {
			t.Error("expected namespaced session name, got plain 'agenc'")
		}
		// "agenc" + "-" + 8 hex chars = 14 chars
		if len(got) != 14 {
			t.Errorf("unexpected length %d: %q", len(got), got)
		}
	})
}

func TestGetPoolSessionName(t *testing.T) {
	t.Run("default path returns agenc-pool", func(t *testing.T) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("failed to get home dir: %v", err)
		}
		got := GetPoolSessionName(filepath.Join(homeDir, ".agenc"))
		if got != "agenc-pool" {
			t.Errorf("expected 'agenc-pool', got %q", got)
		}
	})

	t.Run("custom path returns agenc-HASH-pool", func(t *testing.T) {
		got := GetPoolSessionName("/tmp/test-agenc")
		if got == "agenc-pool" {
			t.Error("expected namespaced pool name, got plain 'agenc-pool'")
		}
		if got[len(got)-5:] != "-pool" {
			t.Errorf("expected suffix '-pool', got %q", got)
		}
	})
}

func TestGetCronPlistPrefix(t *testing.T) {
	t.Run("default path returns agenc-cron.", func(t *testing.T) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("failed to get home dir: %v", err)
		}
		got := GetCronPlistPrefix(filepath.Join(homeDir, ".agenc"))
		if got != "agenc-cron." {
			t.Errorf("expected 'agenc-cron.', got %q", got)
		}
	})

	t.Run("custom path returns agenc-HASH-cron.", func(t *testing.T) {
		got := GetCronPlistPrefix("/tmp/test-agenc")
		if got == "agenc-cron." {
			t.Error("expected namespaced cron prefix")
		}
		if got[len(got)-6:] != "-cron." {
			t.Errorf("expected suffix '-cron.', got %q", got)
		}
	})
}

func TestIsTestEnv(t *testing.T) {
	t.Run("returns false when unset", func(t *testing.T) {
		t.Setenv("AGENC_TEST_ENV", "")
		if IsTestEnv() {
			t.Error("expected false when AGENC_TEST_ENV is empty")
		}
	})

	t.Run("returns true when set to 1", func(t *testing.T) {
		t.Setenv("AGENC_TEST_ENV", "1")
		if !IsTestEnv() {
			t.Error("expected true when AGENC_TEST_ENV=1")
		}
	})
}
