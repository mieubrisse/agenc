package config

import (
	"fmt"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// agencEnvVarNameRegexp finds an AGENC_-prefixed environment variable name
// anywhere in a piece of text, not only when the name is the whole of it. The
// leading word boundary keeps a longer name such as MY_AGENC_THING from
// matching, while still matching after a `$`, a `{` or a space.
var agencEnvVarNameRegexp = regexp.MustCompile(`\bAGENC_[A-Z0-9_]+`)

// TestProtectedEnvVarsCoversEveryAgencEnvVar is the reason ProtectedEnvVars can
// be trusted as a single source of truth rather than a list someone has to
// remember. It scans the WHOLE REPOSITORY for AGENC_-prefixed names and fails
// if one is not in the list. It looks in two places:
//
//   - Every string literal in a non-test Go file, matching the name ANYWHERE
//     inside the literal. Matching only a literal that is exactly the name
//     missed the spelling the hook commands actually use —
//     "agenc mission send claude-update $AGENC_MISSION_UUID Stop" — which was
//     covered only because the same name also happened to appear bare
//     elsewhere (KEV-124).
//   - Every line of a .sh file except a full-line comment. Shell scripts read
//     these names directly, as repo_library_guard.sh reads AGENC_DIRPATH.
//
// What it still does not see is listed in ProtectedEnvVars' doc comment.
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
	goNameCount := 0
	shellNameCount := 0
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
		isGoSource := strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")
		isShellScript := strings.HasSuffix(path, ".sh")
		if !isGoSource && !isShellScript {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var names []string
		if isGoSource {
			names, err = findAgencEnvVarNamesInGoStringLiterals(path, contents)
			if err != nil {
				return err
			}
			goNameCount += len(names)
		} else {
			names = findAgencEnvVarNamesInShellScript(contents)
			shellNameCount += len(names)
		}
		for _, name := range names {
			if _, found := declaredNames[name]; !found {
				declaredNames[name] = filepath.ToSlash(path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to scan the repository for AgenC environment variable names: %v", err)
	}

	// Each half is checked on its own, because a broken shell scan would
	// otherwise hide behind a working Go scan and report green.
	if goNameCount == 0 {
		t.Fatal("found no AGENC_-prefixed environment variable names in any Go file; the scan is broken, not the code")
	}
	if shellNameCount == 0 {
		t.Fatal("found no AGENC_-prefixed environment variable names in any .sh file, though repo_library_guard.sh reads AGENC_DIRPATH; the scan is broken, not the code")
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
			"these AgenC environment variables are not in ProtectedEnvVars, so a mission could set them via `--env`:\n  %s\n"+
				"Add each to ProtectedEnvVars in protected_env_vars.go. If AgenC genuinely does not set one into a mission's "+
				"Claude process, say so in that list's doc comment alongside the names already excluded there.",
			strings.Join(missing, "\n  "),
		)
	}
}

// findAgencEnvVarNamesInGoStringLiterals returns every AGENC_-prefixed name
// inside a string literal in this Go source, wherever in the literal it sits.
// Comments are not tokens, so a comment's "AGENC_CALLING_*" shorthand does not
// register as a name.
//
// Each literal is unquoted before matching, because matching the quoted source
// would miss a name directly after an escape: in "\nAGENC_X" the `n` of the
// escape would sit against the `A` with no word boundary between them.
func findAgencEnvVarNamesInGoStringLiterals(goFilepath string, contents []byte) ([]string, error) {
	fileSet := token.NewFileSet()
	file := fileSet.AddFile(goFilepath, -1, len(contents))
	var scanErrs []string
	var goScanner scanner.Scanner
	goScanner.Init(file, contents, func(pos token.Position, msg string) {
		scanErrs = append(scanErrs, fmt.Sprintf("%v: %v", pos, msg))
	}, 0)

	var names []string
	for {
		_, tok, literal := goScanner.Scan()
		if tok == token.EOF {
			break
		}
		if tok != token.STRING {
			continue
		}
		value, err := strconv.Unquote(literal)
		if err != nil {
			return nil, fmt.Errorf("failed to unquote string literal '%v' in '%v': %w", literal, goFilepath, err)
		}
		names = append(names, agencEnvVarNameRegexp.FindAllString(value, -1)...)
	}
	if len(scanErrs) > 0 {
		return nil, fmt.Errorf("failed to tokenize Go file '%v':\n%v", goFilepath, strings.Join(scanErrs, "\n"))
	}
	return names, nil
}

// findAgencEnvVarNamesInShellScript returns every AGENC_-prefixed name in a
// shell script, skipping lines that are wholly a comment so prose such as
// "the AGENC_CALLING_* names" is not read as a variable.
func findAgencEnvVarNamesInShellScript(contents []byte) []string {
	var names []string
	for _, line := range strings.Split(string(contents), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		names = append(names, agencEnvVarNameRegexp.FindAllString(line, -1)...)
	}
	return names
}

// The shapes the KEV-96 review planted, each pinned to whether the scan sees
// it, so that the coverage ProtectedEnvVars' doc comment claims is checked
// rather than asserted. A shape moving from unseen to seen should update that
// doc comment along with this table.
func TestAgencEnvVarNameScanCoverage(t *testing.T) {
	goTests := []struct {
		name   string
		source string
		want   []string
	}{
		{"bare literal", `package p; var _ = os.Getenv("AGENC_PROBE")`, []string{"AGENC_PROBE"}},
		{"embedded in a hook command", `package p; var _ = "agenc mission send claude-update $AGENC_PROBE Stop"`, []string{"AGENC_PROBE"}},
		{"embedded in a raw string", "package p; var _ = `export AGENC_PROBE=1`", []string{"AGENC_PROBE"}},
		{"directly after an escape", `package p; var _ = "x\nAGENC_PROBE=1"`, []string{"AGENC_PROBE"}},
		{"inside a longer name", `package p; var _ = "MY_AGENC_PROBE"`, nil},
		{"in a comment", "package p\n// AGENC_PROBE and the AGENC_CALLING_* names\nvar _ = 1", nil},
		{"built by concatenation (documented gap)", `package p; var _ = os.Getenv("AGENC_" + "PROBE")`, nil},
	}
	for _, tt := range goTests {
		t.Run("go/"+tt.name, func(t *testing.T) {
			got, err := findAgencEnvVarNamesInGoStringLiterals("probe.go", []byte(tt.source))
			if err != nil {
				t.Fatalf("scan failed: %v", err)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("found %v, want %v", got, tt.want)
			}
		})
	}

	shellTests := []struct {
		name   string
		source string
		want   []string
	}{
		{"defaulted expansion", `x="${AGENC_PROBE:-}"`, []string{"AGENC_PROBE"}},
		{"bare expansion", `echo $AGENC_PROBE`, []string{"AGENC_PROBE"}},
		{"export", `export AGENC_PROBE=1`, []string{"AGENC_PROBE"}},
		{"full-line comment", `    # reads $AGENC_PROBE`, nil},
	}
	for _, tt := range shellTests {
		t.Run("sh/"+tt.name, func(t *testing.T) {
			got := findAgencEnvVarNamesInShellScript([]byte(tt.source))
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("found %v, want %v", got, tt.want)
			}
		})
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
