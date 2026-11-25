package server

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

const (
	// watcherTickTimeout bounds how long a test waits for a tick it expects.
	watcherTickTimeout = 60 * time.Second

	// watcherQuietPeriod is how long a test waits before concluding that no
	// tick is coming: the debounce, plus room for the OS to deliver the event.
	watcherQuietPeriod = writeableCopyWorkingTreeDebounce + 2*time.Second

	// watcherProbeInterval must exceed the debounce, or each probe write would
	// push the tick the test is waiting for further away.
	watcherProbeInterval = writeableCopyWorkingTreeDebounce + time.Second
)

// The notify library reports event paths with the watch root's symlinks
// resolved, whatever spelling the copy was registered under. A watcher that compares them against the unresolved
// spelling recognises neither .git/ nor gitignored paths, and its own ticks'
// index writes then re-tick it forever (KEV-366). The symlink is created here
// rather than inherited from macOS's /var -> /private/var, so the test means
// the same thing on every platform.
func TestWorkingTreeWatcher_FiltersEventsForACopyRegisteredThroughASymlink(t *testing.T) {
	cases := []struct {
		name          string
		quietRelPaths []string
	}{
		{"events inside .git", []string{".git/index", ".git/refs/heads/probe"}},
		{"gitignored paths", []string{"ignored/output.bin"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			const repoName = "github.com/o/r"
			// The link is an ancestor of the copy, as /var is of macOS's temp
			// dir: the copy's own last path element is an ordinary directory.
			resolvedRepoDirpath := newRepoForTest(t)
			linkDirpath := filepath.Join(t.TempDir(), "link")
			if err := os.Symlink(filepath.Dir(resolvedRepoDirpath), linkDirpath); err != nil {
				t.Fatal(err)
			}
			repoDirpath := filepath.Join(linkDirpath, filepath.Base(resolvedRepoDirpath))
			srv := newWriteableCopyTestServer(t, repoName, repoDirpath, &fakeGit{})
			startWorkingTreeWatcherForTest(t, srv, repoName, repoDirpath)

			// The control: an ordinary edit ticks, so the silence asserted
			// below is the filter's and not a watcher that never started.
			awaitTickFromOrdinaryEdits(t, srv, repoDirpath)
			awaitWatcherQuiet(t, srv)

			for _, relPath := range testCase.quietRelPaths {
				writeFileForTest(t, filepath.Join(repoDirpath, relPath), "written by the tick itself")
			}
			select {
			case <-srv.writeableCopyReconcileCh:
				t.Errorf("expected no tick from %v", testCase.quietRelPaths)
			case <-time.After(watcherQuietPeriod):
			}
		})
	}
}

// startWorkingTreeWatcherForTest runs the watcher until the test ends, waiting
// for it to return so it cannot outlive the directories it watches.
func startWorkingTreeWatcherForTest(t *testing.T, srv *Server, repoName string, repoDirpath string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.runWriteableCopyWorkingTreeWatcher(ctx, repoName, repoDirpath)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

// awaitTickFromOrdinaryEdits edits a tracked file until the watcher ticks. It
// repeats the edit because the watcher gives no signal that its stream is live,
// and an edit made before that is never reported.
func awaitTickFromOrdinaryEdits(t *testing.T, srv *Server, repoDirpath string) {
	t.Helper()
	deadline := time.Now().Add(watcherTickTimeout)
	for edit := 0; time.Now().Before(deadline); edit++ {
		writeFileForTest(t, filepath.Join(repoDirpath, "tracked.md"), "edit "+strconv.Itoa(edit))
		select {
		case <-srv.writeableCopyReconcileCh:
			return
		case <-time.After(watcherProbeInterval):
		}
	}
	t.Fatalf("expected an ordinary edit to tick within %v", watcherTickTimeout)
}

// awaitWatcherQuiet drains ticks until a whole quiet period passes without one,
// so an event from an earlier edit that the OS delivers late is not blamed on
// the write under test.
func awaitWatcherQuiet(t *testing.T, srv *Server) {
	t.Helper()
	deadline := time.Now().Add(watcherTickTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-srv.writeableCopyReconcileCh:
		case <-time.After(watcherQuietPeriod):
			return
		}
	}
	t.Fatalf("expected the watcher to go quiet within %v", watcherTickTimeout)
}
