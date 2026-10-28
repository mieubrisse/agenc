package tmux

import (
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
// server, written as a Go string literal. The quote characters are part of the
// pattern so prose that merely names the command does not match.
var serverCreatingTmuxCommandRegexp = regexp.MustCompile(`"(new-session|start-server)"`)

// serverOwnershipSourceFilename is the one file allowed to build a
// server-creating tmux command. Everything else goes through NewSessionCommand.
const serverOwnershipSourceFilename = "server_ownership.go"

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
// someone has to find and believe. It scans the WHOLE REPOSITORY for a tmux
// command that starts the server, written as a Go string literal, and fails if
// one appears outside server_ownership.go.
//
// The invariant needs mechanical enforcement specifically because violating it
// is silent. Creating the tmux server makes the creating process the macOS TCC
// "responsible process" for every pane in it — including the user's own shells
// — for the life of the server. Nothing errors at that moment. The damage
// surfaces weeks later as unrelated permission failures in the user's own
// terminal, under AgenC's name. See server_ownership.go for the full rationale.
//
// So a future `exec.Command("tmux", "new-session", ...)` added in good faith
// anywhere in this codebase has to fail a test, not merely contradict a
// comment. The fix for a failure here is to call NewSessionCommand, which
// refuses with an explanation when no server is running.
func TestOnlyServerOwnershipBuildsServerCreatingCommands(t *testing.T) {
	repoRootDirpath := "../.."

	offendingFilepaths := map[string][]string{}
	allowedFileMatchCount := 0

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

		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		matches := serverCreatingTmuxCommandRegexp.FindAllStringSubmatch(string(contents), -1)
		if len(matches) == 0 {
			return nil
		}

		// This test file names both commands in its own regex, and the helper
		// is the one place allowed to build them.
		filename := filepath.Base(path)
		if filename == serverOwnershipSourceFilename {
			allowedFileMatchCount += len(matches)
			return nil
		}
		if filename == serverOwnershipTestFilename {
			return nil
		}

		for _, match := range matches {
			offendingFilepaths[filepath.ToSlash(path)] = append(offendingFilepaths[filepath.ToSlash(path)], match[1])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to scan the repository for server-creating tmux commands: %v", err)
	}

	// A scan that finds nothing at all in the one file that is supposed to
	// contain these commands is a broken scan reporting green, not a clean
	// repository. The repo has been bitten by exactly that before: an e2e
	// check whose command printed an empty-state string inspected nothing and
	// passed while colour was injected right past it.
	if allowedFileMatchCount == 0 {
		t.Fatalf(
			"found no server-creating tmux command in %v; the scan is broken, not the code",
			serverOwnershipSourceFilename,
		)
	}

	if len(offendingFilepaths) == 0 {
		return
	}

	var sortedFilepaths []string
	for offendingFilepath := range offendingFilepaths {
		sortedFilepaths = append(sortedFilepaths, offendingFilepath)
	}
	sort.Strings(sortedFilepaths)

	for _, offendingFilepath := range sortedFilepaths {
		t.Errorf(
			"%v builds the server-creating tmux command(s) %v directly. AgenC must never create the tmux server: whichever process creates it becomes the macOS privacy identity for every pane in it, including the user's own shells, silently and for weeks. Call tmux.NewSessionCommand instead, which refuses with an explanation when no server is running. See internal/tmux/server_ownership.go.",
			offendingFilepath,
			strings.Join(offendingFilepaths[offendingFilepath], ", "),
		)
	}
}

// serverOwnershipTestFilename is this file, which names both commands in its
// own regex and so must exempt itself from its own scan.
const serverOwnershipTestFilename = "server_ownership_test.go"

// TestIsServerRunningDoesNotCreateAServer pins the half of the guard that is
// easy to get wrong by accident: the probe itself must not start the thing it
// is probing for. It runs against an isolated, empty tmux socket directory, so
// it neither sees nor touches any real tmux server on the machine.
func TestIsServerRunningDoesNotCreateAServer(t *testing.T) {
	// A tmux socket path lives in a unix-domain address, which is limited to
	// around 104 bytes on macOS, so the directory has to be short. t.TempDir()
	// under the default Go test temp root is comfortably too long and fails
	// with "File name too long" rather than reporting no server.
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
