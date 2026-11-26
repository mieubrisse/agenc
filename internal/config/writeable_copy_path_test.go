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

// newSymlinkedDir returns base/real (an existing directory) and base/link, a
// symlink to it. Tests below spell one path through link and another through
// real, which name the same directory (KEV-435).
func newSymlinkedDir(t *testing.T) (realDirpath, linkDirpath string) {
	t.Helper()
	base := t.TempDir()
	realDirpath = filepath.Join(base, "real")
	if err := os.Mkdir(realDirpath, 0755); err != nil {
		t.Fatal(err)
	}
	linkDirpath = filepath.Join(base, "link")
	if err := os.Symlink(realDirpath, linkDirpath); err != nil {
		t.Fatalf("cannot create symlink: %v", err)
	}
	return realDirpath, linkDirpath
}

func TestValidateWriteableCopyPath_SymlinkedAncestorIntoAgencDir(t *testing.T) {
	realDirpath, linkDirpath := newSymlinkedDir(t)
	agencDir := filepath.Join(realDirpath, "agenc")
	if err := os.Mkdir(agencDir, 0755); err != nil {
		t.Fatal(err)
	}

	candidate := filepath.Join(linkDirpath, "agenc", "x")
	_, err := ValidateWriteableCopyPath(candidate, agencDir, nil)
	if err == nil {
		t.Fatal("expected error for path reaching the agenc dir through a symlinked ancestor")
	}
	if !strings.Contains(err.Error(), "AgenC directory") {
		t.Errorf("expected agenc-dir error, got %v", err)
	}
}

func TestValidateWriteableCopyPath_AgencDirSpelledThroughSymlink(t *testing.T) {
	realDirpath, linkDirpath := newSymlinkedDir(t)
	if err := os.Mkdir(filepath.Join(realDirpath, "agenc"), 0755); err != nil {
		t.Fatal(err)
	}

	candidate := filepath.Join(realDirpath, "agenc", "x")
	_, err := ValidateWriteableCopyPath(candidate, filepath.Join(linkDirpath, "agenc"), nil)
	if err == nil {
		t.Fatal("expected error for path inside an agenc dir that is spelled through a symlink")
	}
	if !strings.Contains(err.Error(), "AgenC directory") {
		t.Errorf("expected agenc-dir error, got %v", err)
	}
}

func TestValidateWriteableCopyPath_SymlinkedAncestorOverlapsAnotherCopy(t *testing.T) {
	agencDir := t.TempDir()
	realDirpath, linkDirpath := newSymlinkedDir(t)
	otherCopyPath := filepath.Join(realDirpath, "c")
	if err := os.Mkdir(otherCopyPath, 0755); err != nil {
		t.Fatal(err)
	}
	others := map[string]string{"github.com/o/other": otherCopyPath}

	for name, candidate := range map[string]string{
		"same directory":     filepath.Join(linkDirpath, "c"),
		"nested inside it":   filepath.Join(linkDirpath, "c", "nested"),
		"containing it":      linkDirpath,
		"nested, not cloned": filepath.Join(linkDirpath, "c", "missing"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ValidateWriteableCopyPath(candidate, agencDir, others)
			if err == nil {
				t.Fatalf("expected overlap error for '%v' against '%v'", candidate, otherCopyPath)
			}
			if !strings.Contains(err.Error(), "overlaps") {
				t.Errorf("expected overlap error, got %v", err)
			}
		})
	}
}

func TestValidateWriteableCopyPath_OtherCopyRegisteredThroughSymlink(t *testing.T) {
	agencDir := t.TempDir()
	realDirpath, linkDirpath := newSymlinkedDir(t)
	if err := os.Mkdir(filepath.Join(realDirpath, "c"), 0755); err != nil {
		t.Fatal(err)
	}

	_, err := ValidateWriteableCopyPath(filepath.Join(realDirpath, "c"), agencDir, map[string]string{
		"github.com/o/other": filepath.Join(linkDirpath, "c"),
	})
	if err == nil {
		t.Fatal("expected overlap error against a copy registered through a symlinked parent")
	}
	if !strings.Contains(err.Error(), "overlaps") {
		t.Errorf("expected overlap error, got %v", err)
	}
}

// `writeable-copy set` clones into a directory that need not exist yet, so a
// missing last element under a symlinked parent must still validate, and keep
// the spelling the user gave.
func TestValidateWriteableCopyPath_NotYetClonedUnderSymlinkedParent(t *testing.T) {
	agencDir := t.TempDir()
	realDirpath, linkDirpath := newSymlinkedDir(t)
	unrelatedCopy := filepath.Join(realDirpath, "c")
	if err := os.Mkdir(unrelatedCopy, 0755); err != nil {
		t.Fatal(err)
	}

	candidate := filepath.Join(linkDirpath, "new")
	got, err := ValidateWriteableCopyPath(candidate, agencDir, map[string]string{
		"github.com/o/other": unrelatedCopy,
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if got != candidate {
		t.Errorf("expected %q, got %q", candidate, got)
	}
}

// Re-running `writeable-copy set` for a repo whose copy is already registered
// under a symlinked path excludes that repo from the others map, so it must
// validate and return the registered spelling unchanged.
func TestValidateWriteableCopyPath_ExistingCopyUnderSymlinkedParent(t *testing.T) {
	agencDir := t.TempDir()
	realDirpath, linkDirpath := newSymlinkedDir(t)
	if err := os.Mkdir(filepath.Join(realDirpath, "c"), 0755); err != nil {
		t.Fatal(err)
	}

	registered := filepath.Join(linkDirpath, "c")
	got, err := ValidateWriteableCopyPath(registered, agencDir, map[string]string{
		"github.com/o/elsewhere": filepath.Join(t.TempDir(), "d"),
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if got != registered {
		t.Errorf("expected %q, got %q", registered, got)
	}
}

// A copy registered under a symlinked path must not stop another repo being
// configured somewhere unrelated, including when that registered link dangles.
func TestValidateWriteableCopyPath_UnrelatedPathBesideSymlinkedCopies(t *testing.T) {
	agencDir := t.TempDir()
	realDirpath, linkDirpath := newSymlinkedDir(t)
	if err := os.Mkdir(filepath.Join(realDirpath, "c"), 0755); err != nil {
		t.Fatal(err)
	}
	danglingLink := filepath.Join(t.TempDir(), "dangling")
	if err := os.Symlink(filepath.Join(t.TempDir(), "gone"), danglingLink); err != nil {
		t.Fatal(err)
	}

	candidate := filepath.Join(t.TempDir(), "wc")
	_, err := ValidateWriteableCopyPath(candidate, agencDir, map[string]string{
		"github.com/o/linked":   filepath.Join(linkDirpath, "c"),
		"github.com/o/dangling": filepath.Join(danglingLink, "c"),
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestResolveExistingPrefix(t *testing.T) {
	realDirpath, linkDirpath := newSymlinkedDir(t)
	wantReal, err := filepath.EvalSymlinks(realDirpath)
	if err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct{ input, want string }{
		"existing link":         {linkDirpath, wantReal},
		"missing tail":          {filepath.Join(linkDirpath, "a"), filepath.Join(wantReal, "a")},
		"several missing parts": {filepath.Join(linkDirpath, "a", "b"), filepath.Join(wantReal, "a", "b")},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := resolveExistingPrefix(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
