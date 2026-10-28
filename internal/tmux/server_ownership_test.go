package tmux

import (
	"go/ast"
	"go/parser"
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

// serverCreatingTmuxCommandRegexp finds a tmux command that starts the tmux
// server. It is applied to the CONTENTS of Go string literals rather than to
// raw source text, which is what lets it look for the command as a word
// anywhere inside a string -- `"new-session"` passed as its own argument, and
// equally `"tmux new-session -d"` embedded in a shell command string, which is
// the form tmux keybinding and palette commands are built in. Matching raw
// source would instead flag every comment that names the command, including
// the ones in server_ownership.go explaining why it is forbidden.
var serverCreatingTmuxCommandRegexp = regexp.MustCompile(`\b(new-session|start-server|attach-session)\b`)

// serverOwnershipSourceFilename is the one file allowed to name a
// server-creating tmux command in a string. Everything else goes through
// NewSessionCommand.
const serverOwnershipSourceFilename = "server_ownership.go"

// serverOwnershipTestFilename is this file, which names both commands in its
// own regex and in its probes, and so must exempt itself from its own scan.
const serverOwnershipTestFilename = "server_ownership_test.go"

// skippedScanDirnames are directories the repository scan does not descend
// into: build output, the isolated test environment, and vendored code.
var skippedScanDirnames = map[string]bool{
	".git":         true,
	"_build":       true,
	"_test-env":    true,
	"vendor":       true,
	"node_modules": true,
}

// TestOnlyServerOwnershipBuildsServerCreatingCommands is what makes "AgenC
// never creates the tmux server" an enforced invariant rather than a comment
// someone has to find and believe. It parses every Go file in the WHOLE
// REPOSITORY and fails if any string literal outside server_ownership.go names
// a tmux command that starts the server.
//
// The invariant needs mechanical enforcement specifically because violating it
// is silent. Creating the tmux server makes the creating process the macOS TCC
// "responsible process" for every pane in it -- including the user's own
// shells -- for the life of the server. Nothing errors at that moment. The
// damage surfaces weeks later as unrelated permission failures in the user's
// own terminal, under AgenC's name. See server_ownership.go for the full
// rationale.
//
// The three commands scanned for are the three that start a server, measured
// across every tmux subcommand AgenC invokes: `new-session`, `start-server`
// and `attach-session`. Measuring this needs the right condition, and the
// obvious one is wrong: under tmux's default `exit-empty on`, the server
// `attach-session` starts exits again before a check can see it, so a sweep
// that looks afterwards reports it as harmless. Re-run under `exit-empty off`
// it is plainly server-creating, and the server keeps the attach command's
// own argv. If you extend this list, use that condition.
//
// The fix for a failure here is to call NewSessionCommand (which refuses with
// an explanation when no server is running) or AttachSessionCommand.
//
// One honest limit: this catches a command named in a string, not one assembled
// from pieces -- `"new-"+"session"` passes. Deliberate evasion is not the threat
// model; a future caller writing the command the ordinary way is.
func TestOnlyServerOwnershipBuildsServerCreatingCommands(t *testing.T) {
	repoRootDirpath := "../.."

	offendingLiterals := map[string][]string{}
	allowedFileMatchCount := 0
	scannedFileCount := 0

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
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		filename := filepath.Base(path)
		if filename == serverOwnershipTestFilename {
			return nil
		}
		scannedFileCount++

		matchedLiterals, err := findServerCreatingTmuxCommandLiterals(path)
		if err != nil {
			return err
		}
		if len(matchedLiterals) == 0 {
			return nil
		}
		if filename == serverOwnershipSourceFilename {
			allowedFileMatchCount += len(matchedLiterals)
			return nil
		}
		offendingLiterals[filepath.ToSlash(path)] = matchedLiterals
		return nil
	})
	if err != nil {
		t.Fatalf("failed to scan the repository for server-creating tmux commands: %v", err)
	}

	// A scan that parsed almost nothing, or that found no match in the one
	// file that is supposed to contain these commands, is a broken scan
	// reporting green rather than a clean repository. This repo has been
	// bitten by exactly that: an e2e check whose command printed an
	// empty-state string inspected nothing and passed while colour was
	// injected right past it.
	if scannedFileCount < 50 {
		t.Fatalf("only %d Go files were scanned; the walk is broken, not the code", scannedFileCount)
	}
	if allowedFileMatchCount == 0 {
		t.Fatalf(
			"found no server-creating tmux command in %v; the scan is broken, not the code",
			serverOwnershipSourceFilename,
		)
	}

	if len(offendingLiterals) == 0 {
		return
	}

	var sortedFilepaths []string
	for offendingFilepath := range offendingLiterals {
		sortedFilepaths = append(sortedFilepaths, offendingFilepath)
	}
	sort.Strings(sortedFilepaths)

	for _, offendingFilepath := range sortedFilepaths {
		t.Errorf(
			"%v names a server-creating tmux command in the string(s) %v. AgenC must never create the tmux server: whichever process creates it becomes the macOS privacy identity for every pane in it, including the user's own shells, silently and for weeks. Call tmux.NewSessionCommand instead, which refuses with an explanation when no server is running. See internal/tmux/server_ownership.go.",
			offendingFilepath,
			strings.Join(offendingLiterals[offendingFilepath], ", "),
		)
	}
}

// findServerCreatingTmuxCommandLiterals returns the string literals in a Go
// file whose contents name a tmux command that starts the server. Parsing the
// file rather than grepping it is what separates a real command from a comment
// describing one.
func findServerCreatingTmuxCommandLiterals(goFilepath string) ([]string, error) {
	fileSet := token.NewFileSet()
	parsedFile, err := parser.ParseFile(fileSet, goFilepath, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}

	var matchedLiterals []string
	ast.Inspect(parsedFile, func(node ast.Node) bool {
		literal, isLiteral := node.(*ast.BasicLit)
		if !isLiteral || literal.Kind != token.STRING {
			return true
		}
		literalValue, err := strconv.Unquote(literal.Value)
		if err != nil {
			// A literal that will not unquote is not something this scan can
			// reason about; the compiler will have its own opinion.
			return true
		}
		if serverCreatingTmuxCommandRegexp.MatchString(literalValue) {
			matchedLiterals = append(matchedLiterals, strconv.Quote(literalValue))
		}
		return true
	})
	return matchedLiterals, nil
}

// TestIsServerRunningDoesNotCreateAServer pins the half of the guard that is
// easy to get wrong by accident: the probe itself must not start the thing it
// is probing for. It runs against an isolated, empty tmux socket directory, so
// it neither sees nor touches any real tmux server on the machine.
func TestIsServerRunningDoesNotCreateAServer(t *testing.T) {
	// A tmux socket path lives in a unix-domain address, which is limited to
	// around 104 bytes on macOS, so the directory has to be short. t.TempDir()
	// under the default Go test temp root is comfortably too long and fails
	// with "File name too long" rather than reporting no server -- which would
	// look like a pass while testing nothing.
	isolatedSocketDirpath, err := os.MkdirTemp("/tmp", "agenc-tmuxprobe-")
	if err != nil {
		t.Fatalf("failed to create an isolated tmux socket directory: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(isolatedSocketDirpath)
	})

	// TMUX must be cleared as well as TMUX_TMPDIR set: inside a tmux pane,
	// tmux finds its server through TMUX and ignores the socket directory.
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", isolatedSocketDirpath)

	if IsServerRunning() {
		t.Fatalf("IsServerRunning reported a server in the empty socket directory %v", isolatedSocketDirpath)
	}

	if _, err := NewSessionCommand("-d", "-s", "should-never-be-created"); err == nil {
		t.Fatal("NewSessionCommand returned a command instead of refusing when no tmux server was running")
	}

	// The real assertion: after probing and after a refused creation, there is
	// still no server. A socket file would mean one of the two started it.
	socketDirEntries, err := os.ReadDir(filepath.Join(isolatedSocketDirpath, tmuxSocketSubdirname()))
	if err != nil {
		// tmux creates the per-user subdirectory as a side effect of looking
		// for its socket; its absence is also a pass.
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("failed to read the isolated tmux socket directory: %v", err)
	}
	for _, socketDirEntry := range socketDirEntries {
		t.Errorf(
			"probing for a tmux server left %v behind in the isolated socket directory; something started a server",
			socketDirEntry.Name(),
		)
	}
}

// tmuxSocketSubdirname is the per-user subdirectory tmux creates under
// TMUX_TMPDIR to hold its sockets.
func tmuxSocketSubdirname() string {
	return "tmux-" + strconv.Itoa(os.Getuid())
}

// TestNoStartServerFlagSupportIsNotCachedAcrossAnUpgrade pins the absence of a
// cache in checkNoStartServerFlagSupported, which is the kind of absence that
// gets optimised away by a well-meaning future change.
//
// Caching looks obviously right — the installed tmux cannot change inside one
// process — and is wrong here, because the process asking is a daemon that
// runs for days. A cached "too old" answer means a user who does exactly what
// the error tells them to do, upgrade tmux, meets the identical error
// afterwards, forever. That is a confident error whose stated remedy leads
// nowhere, which is the failure this file exists to prevent.
func TestNoStartServerFlagSupportIsNotCachedAcrossAnUpgrade(t *testing.T) {
	fakeTmuxDirpath := t.TempDir()
	fakeTmuxVersionFilepath := filepath.Join(fakeTmuxDirpath, "version")
	fakeTmuxFilepath := filepath.Join(fakeTmuxDirpath, "tmux")

	fakeTmuxScript := "#!/bin/sh\ncat " + fakeTmuxVersionFilepath + "\n"
	if err := os.WriteFile(fakeTmuxFilepath, []byte(fakeTmuxScript), 0o755); err != nil {
		t.Fatalf("failed to write the fake tmux: %v", err)
	}
	t.Setenv("PATH", fakeTmuxDirpath+string(os.PathListSeparator)+os.Getenv("PATH"))

	writeFakeTmuxVersion := func(version string) {
		if err := os.WriteFile(fakeTmuxVersionFilepath, []byte(version+"\n"), 0o644); err != nil {
			t.Fatalf("failed to set the fake tmux version to %v: %v", version, err)
		}
	}

	// A point release below the floor, deliberately: 3.1c parses as 3.1, and
	// the installed-version half of the message must not claim otherwise.
	writeFakeTmuxVersion("tmux 3.1c")
	tooOldErr := checkNoStartServerFlagSupported()
	if tooOldErr == nil {
		t.Fatal("tmux 3.1c was accepted, but it does not have the no-start flag")
	}
	if strings.Contains(tooOldErr.Error(), "tmux 3.1 is installed") {
		t.Errorf("the error claims to echo the installed version but drops the point release, which will not match the reader's own 'tmux -V': %v", tooOldErr)
	}

	// The upgrade the error asks for, in the same process.
	writeFakeTmuxVersion("tmux 3.6a")
	if err := checkNoStartServerFlagSupported(); err != nil {
		t.Errorf("after upgrading tmux in the same process the check still refuses, so the answer is cached: %v", err)
	}
}
