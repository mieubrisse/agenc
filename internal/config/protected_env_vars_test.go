package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// agencEnvVarLiteralRegexp finds an AGENC_-prefixed environment variable name
// written as a Go string literal.
var agencEnvVarLiteralRegexp = regexp.MustCompile(`"(AGENC_[A-Z0-9_]+)"`)

// TestProtectedEnvVarsCoversEveryAgencEnvVar is the reason ProtectedEnvVars can
// be trusted as a single source of truth rather than a list someone has to
// remember. It scans the WHOLE REPOSITORY for AGENC_-prefixed names written as
// Go string literals and fails if one is not in the list.
//
// Without it, the list is a convention: a new AGENC_ variable would be settable
// through `agenc mission new --env`, a caller's value would win over AgenC's,
// and nothing would say so. That is precisely the "exclusion list that goes
// stale" failure the per-mission environment channel was designed to avoid, so
// the list earns its place only while this test holds it to the code.
//
// It scans the repository rather than this package because a package-scoped
// scan already missed one: AGENC_CALLING_PANE_ID was written as a bare literal
// in cmd/tmux_palette.go with no constant anywhere, and a scan of this package
// reported clean while that variable was overridable through `--env`. A
// convention saying "define AgenC's variables in internal/config" would not
// have caught it either, because the whole failure was that nobody did.
func TestProtectedEnvVarsCoversEveryAgencEnvVar(t *testing.T) {
	repoRootDirpath := "../.."

	declaredNames := map[string]string{}
	err := filepath.WalkDir(repoRootDirpath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if skippedScanDirnames[entry.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		// Test files are excluded because a test may legitimately name a
		// variable to probe it — this file does.
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range agencEnvVarLiteralRegexp.FindAllStringSubmatch(string(contents), -1) {
			declaredNames[match[1]] = filepath.ToSlash(path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to scan the repository for AgenC environment variable names: %v", err)
	}

	if len(declaredNames) == 0 {
		t.Fatal("found no AGENC_-prefixed environment variable names in the repository; the scan is broken, not the code")
	}

	var missing []string
	for name := range declaredNames {
		if !IsProtectedEnvVar(name) {
			missing = append(missing, name+" (written in "+declaredNames[name]+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf(
			"these AgenC environment variables are not in ProtectedEnvVars, so a mission could override them via `--env`:\n  %s\n"+
				"Add each to ProtectedEnvVars in protected_env_vars.go. If AgenC genuinely does not set one into a mission's "+
				"Claude process, say so in that list's doc comment alongside the names already excluded there.",
			strings.Join(missing, "\n  "),
		)
	}
}

// skippedScanDirnames are directories the repository scan does not descend
// into: build output, the throwaway test installation, dependencies, and git's
// own storage. None contains AgenC source.
var skippedScanDirnames = map[string]bool{
	".git":         true,
	"_build":       true,
	"_test-env":    true,
	"vendor":       true,
	"node_modules": true,
}

// Every entry must name something, and the list must have no duplicates — a
// duplicate would mean two people added the same variable and one of them
// probably meant a different one.
func TestProtectedEnvVarsEntriesAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range ProtectedEnvVars {
		if name == "" {
			t.Error("ProtectedEnvVars contains an empty name, which would match nothing")
			continue
		}
		if seen[name] {
			t.Errorf("ProtectedEnvVars lists '%s' more than once", name)
		}
		seen[name] = true
	}
}

func TestIsProtectedEnvVar(t *testing.T) {
	if !IsProtectedEnvVar(MissionUUIDEnvVar) {
		t.Errorf("expected '%s' to be protected", MissionUUIDEnvVar)
	}
	if !IsProtectedEnvVar(OAuthTokenEnvVar) {
		t.Errorf("expected '%s' to be protected", OAuthTokenEnvVar)
	}
	// The marker KEV-89 actually uses. If this ever becomes protected, the
	// reflection-session channel stops working.
	if IsProtectedEnvVar("REFLECTION_SESSION") {
		t.Error("expected an ordinary caller-chosen name not to be protected")
	}
}
