package claudeconfig

import (
	"regexp"
	"strings"
	"testing"
)

func TestComputeCredentialServiceName(t *testing.T) {
	t.Run("deterministic output", func(t *testing.T) {
		path := "/Users/test/.agenc/missions/abc123/claude-config"
		name1 := ComputeCredentialServiceName(path)
		name2 := ComputeCredentialServiceName(path)
		if name1 != name2 {
			t.Errorf("expected deterministic output, got %q and %q", name1, name2)
		}
	})

	t.Run("has correct prefix", func(t *testing.T) {
		path := "/Users/test/.agenc/missions/abc123/claude-config"
		name := ComputeCredentialServiceName(path)
		prefix := "Claude Code-credentials-"
		if !strings.HasPrefix(name, prefix) {
			t.Errorf("expected prefix %q, got %q", prefix, name)
		}
	})

	t.Run("hash suffix is exactly 8 hex characters", func(t *testing.T) {
		path := "/Users/test/.agenc/missions/abc123/claude-config"
		name := ComputeCredentialServiceName(path)
		prefix := "Claude Code-credentials-"
		suffix := strings.TrimPrefix(name, prefix)
		if len(suffix) != 8 {
			t.Errorf("expected 8-char hash suffix, got %d chars: %q", len(suffix), suffix)
		}
		matched, _ := regexp.MatchString(`^[0-9a-f]{8}$`, suffix)
		if !matched {
			t.Errorf("expected hex characters in suffix, got %q", suffix)
		}
	})

	t.Run("different paths produce different names", func(t *testing.T) {
		path1 := "/Users/test/.agenc/missions/abc123/claude-config"
		path2 := "/Users/test/.agenc/missions/def456/claude-config"
		name1 := ComputeCredentialServiceName(path1)
		name2 := ComputeCredentialServiceName(path2)
		if name1 == name2 {
			t.Errorf("expected different names for different paths, both got %q", name1)
		}
	})
}
