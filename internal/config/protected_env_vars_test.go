package config

import (
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
// remember. It scans this package's own source for AGENC_-prefixed names and
// fails if one is not in the list.
//
// Without it, the list is a convention: a new AGENC_ variable would be settable
// through `agenc mission new --env`, a caller's value would win over AgenC's,
// and nothing would say so. That is precisely the "exclusion list that goes
// stale" failure the per-mission environment channel was designed to avoid, so
// the list earns its place only while this test holds it to the code.
//
// Scoped to this package because it is where AgenC's environment variable names
// are defined. A name introduced elsewhere would not be caught — if that ever
// happens, the right fix is to move the constant here, next to its siblings,
// rather than to widen this scan.
func TestProtectedEnvVarsCoversEveryAgencEnvVar(t *testing.T) {
	sourceFilepaths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("failed to list this package's source files: %v", err)
	}
	if len(sourceFilepaths) == 0 {
		t.Fatal("found no Go source files to scan; this test cannot inspect anything and would pass vacuously")
	}

	declaredNames := map[string]string{}
	for _, sourceFilepath := range sourceFilepaths {
		if strings.HasSuffix(sourceFilepath, "_test.go") {
			continue
		}
		contents, err := os.ReadFile(sourceFilepath)
		if err != nil {
			t.Fatalf("failed to read '%s': %v", sourceFilepath, err)
		}
		for _, match := range agencEnvVarLiteralRegexp.FindAllStringSubmatch(string(contents), -1) {
			declaredNames[match[1]] = sourceFilepath
		}
	}

	if len(declaredNames) == 0 {
		t.Fatal("found no AGENC_-prefixed environment variable names in this package; the scan is broken, not the code")
	}

	var missing []string
	for name := range declaredNames {
		if !IsProtectedEnvVar(name) {
			missing = append(missing, name+" (declared in "+declaredNames[name]+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf(
			"these AgenC environment variables are not in ProtectedEnvVars, so a mission could override them via `--env`:\n  %s\n"+
				"Add each to ProtectedEnvVars in protected_env_vars.go, or, if AgenC genuinely does not set it into a mission's Claude process, say so in that list's doc comment alongside CLAUDE_CONFIG_DIR.",
			strings.Join(missing, "\n  "),
		)
	}
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
