package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateWriteableCopyPath_Empty(t *testing.T) {
	tmp := t.TempDir()
	_, err := ValidateWriteableCopyPath("", tmp, nil)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected empty-path error, got %v", err)
	}
}

func TestValidateWriteableCopyPath_RelativeReversibleToHome(t *testing.T) {
	tmp := t.TempDir()
	// "~" expands to home — a subdirectory under home is valid as long as it's
	// not under the agenc dir we pass.
	_, err := ValidateWriteableCopyPath("~/Library/AgenCWriteableCopyTestSubdirThatShouldNeverExist/x", tmp, nil)
	if err == nil {
		t.Fatal("expected error for non-existent parent")
	}
	if !strings.Contains(err.Error(), "parent directory") {
		t.Errorf("expected parent-directory error, got %v", err)
	}
}

func TestValidateWriteableCopyPath_UnderAgencDir(t *testing.T) {
	tmp := t.TempDir()
	candidate := filepath.Join(tmp, "subdir", "wc")
	_, err := ValidateWriteableCopyPath(candidate, tmp, nil)
	if err == nil {
		t.Fatal("expected error for path under agenc dir")
	}
	if !strings.Contains(err.Error(), "AgenC directory") {
		t.Errorf("expected agenc-dir error, got %v", err)
	}
}

func TestValidateWriteableCopyPath_AgencDirItself(t *testing.T) {
	tmp := t.TempDir()
	_, err := ValidateWriteableCopyPath(tmp, tmp, nil)
	if err == nil {
		t.Fatal("expected error for path equal to agenc dir")
	}
}

func TestValidateWriteableCopyPath_NestedInAnotherWriteableCopy(t *testing.T) {
	parent := t.TempDir() // serves as base; agenc dir will be unrelated
	agencDir := t.TempDir()
	otherCopyPath := filepath.Join(parent, "other")
	if err := os.MkdirAll(otherCopyPath, 0755); err != nil {
		t.Fatal(err)
	}

	candidate := filepath.Join(otherCopyPath, "nested")
	_, err := ValidateWriteableCopyPath(candidate, agencDir, map[string]string{
		"github.com/o/other": otherCopyPath,
	})
	if err == nil {
		t.Fatal("expected error for path nested inside another writeable copy")
	}
	if !strings.Contains(err.Error(), "overlaps") {
		t.Errorf("expected overlap error, got %v", err)
	}
}

func TestValidateWriteableCopyPath_ContainsAnotherWriteableCopy(t *testing.T) {
	agencDir := t.TempDir()
	wantPath := t.TempDir() // candidate path (parent dir exists)
	otherCopyPath := filepath.Join(wantPath, "inner")

	_, err := ValidateWriteableCopyPath(wantPath, agencDir, map[string]string{
		"github.com/o/other": otherCopyPath,
	})
	if err == nil {
		t.Fatal("expected error for candidate that contains another writeable copy")
	}
	if !strings.Contains(err.Error(), "overlaps") {
		t.Errorf("expected overlap error, got %v", err)
	}
}

func TestValidateWriteableCopyPath_ParentMissing(t *testing.T) {
	agencDir := t.TempDir()
	candidate := filepath.Join(t.TempDir(), "nonexistent_parent", "wc")
	_, err := ValidateWriteableCopyPath(candidate, agencDir, nil)
	if err == nil {
		t.Fatal("expected error for missing parent")
	}
	if !strings.Contains(err.Error(), "parent directory") {
		t.Errorf("expected parent-directory error, got %v", err)
	}
}

func TestValidateWriteableCopyPath_HappyPath(t *testing.T) {
	agencDir := t.TempDir()
	parent := t.TempDir()
	candidate := filepath.Join(parent, "wc")

	got, err := ValidateWriteableCopyPath(candidate, agencDir, nil)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if got != filepath.Clean(candidate) {
		t.Errorf("expected %q, got %q", candidate, got)
	}
}

func TestValidateWriteableCopyPath_SymlinkIntoAgencDir(t *testing.T) {
	agencDir := t.TempDir()
	parent := t.TempDir()
	candidate := filepath.Join(parent, "symlinked_wc")

	if err := os.Symlink(agencDir, candidate); err != nil {
		t.Skipf("cannot create symlink in test environment: %v", err)
	}

	_, err := ValidateWriteableCopyPath(candidate, agencDir, nil)
	if err == nil {
		t.Fatal("expected error for symlink into agenc dir")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("expected symlink error, got %v", err)
	}
}
