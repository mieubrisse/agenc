package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mieubrisse/stacktrace"
	"github.com/rjeczalik/notify"
)

const (
	// writeableCopyWorkingTreeDebounce is the quiet period between filesystem
	// activity in the working tree and the auto-commit-and-push tick. Long
	// enough to coalesce a multi-file edit burst from an agent or editor.
	writeableCopyWorkingTreeDebounce = 3 * time.Second

	// writeableCopyRefDebounce is the quiet period between filesystem events on
	// .git/refs/remotes/origin/<branch> and the library push-event POST.
	writeableCopyRefDebounce = 1 * time.Second
)

// writeableCopyWatchers manages the active watchers for each configured
// writeable copy. Watchers are registered when a writeable copy is added
// (config watcher integration) and deregistered when removed.
type writeableCopyWatchers struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc // repoName → cancel for the watcher pair
}

func newWriteableCopyWatchers() *writeableCopyWatchers {
	return &writeableCopyWatchers{cancels: make(map[string]context.CancelFunc)}
}

// start launches working-tree and origin-ref watchers for a writeable copy
// already known to exist on disk.
func (w *writeableCopyWatchers) start(ctx context.Context, s *Server, repoName, repoDirpath string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.cancels[repoName]; exists {
		return // already watching
	}
	wctx, cancel := context.WithCancel(ctx) //nolint:gosec // cancel is stored and invoked from stop()
	w.cancels[repoName] = cancel
	go s.runWriteableCopyWorkingTreeWatcher(wctx, repoName, repoDirpath)
	go s.runWriteableCopyRefWatcher(wctx, repoName, repoDirpath)
}

// stop cancels both watchers for a writeable copy. Safe to call when no
// watchers are running.
func (w *writeableCopyWatchers) stop(repoName string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if cancel, ok := w.cancels[repoName]; ok {
		cancel()
		delete(w.cancels, repoName)
	}
}

// activeRepos returns the list of repos currently being watched.
func (w *writeableCopyWatchers) activeRepos() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, 0, len(w.cancels))
	for repo := range w.cancels {
		out = append(out, repo)
	}
	return out
}

// runWriteableCopyWorkingTreeWatcher installs a recursive notify.Watch on the
// writeable copy's working tree (excluding .git/ and gitignored paths) and
// enqueues a reconcile request after a quiet period following any filesystem
// event. On macOS this opens a single FSEvents stream per repo, avoiding the
// per-directory FD explosion of the old fsnotify walk.
func (s *Server) runWriteableCopyWorkingTreeWatcher(ctx context.Context, repoName, repoDirpath string) {
	// Load the gitignore matcher BEFORE starting the watcher to avoid the
	// load-order race surfaced in edge-case discovery (4.1).
	filter, err := newGitignoreFilter(repoDirpath)
	if err != nil {
		s.logger.Printf("Writeable-copy watcher: failed to load gitignore for '%s': %v", repoName, err)
		return
	}

	eventCh := make(chan notify.EventInfo, 256)
	if err := notify.Watch(repoDirpath+"/...", eventCh, notify.All); err != nil {
		s.logger.Printf("Writeable-copy watcher: notify.Watch failed for '%s': %v", repoName, err)
		return
	}
	defer notify.Stop(eventCh)

	debounce := time.NewTimer(0)
	if !debounce.Stop() {
		<-debounce.C
	}
	timerActive := false

	for {
		select {
		case <-ctx.Done():
			if !debounce.Stop() && timerActive {
				<-debounce.C
			}
			return
		case event := <-eventCh:
			eventPath := event.Path()
			// Skip events inside .git/.
			if isInsideRepoGitDir(eventPath, repoDirpath) {
				continue
			}
			// Skip events matching the repo's gitignore.
			if filter.shouldIgnore(eventPath, eventIsDir(event)) {
				continue
			}
			if !timerActive {
				timerActive = true
			} else if !debounce.Stop() {
				<-debounce.C
			}
			debounce.Reset(writeableCopyWorkingTreeDebounce)
		case <-debounce.C:
			timerActive = false
			s.enqueueReconcileForRepo(repoName)
		}
	}
}

// runWriteableCopyRefWatcher installs a notify watch on
// <repoDirpath>/.git/refs/remotes/origin/<default-branch> and triggers a
// library push-event update when the ref advances (i.e., after a successful
// push from this writeable copy).
func (s *Server) runWriteableCopyRefWatcher(ctx context.Context, repoName, repoDirpath string) {
	gc := s.gitCommander
	if gc == nil {
		gc = newRealGit()
	}
	defaultBranch, err := gc.DefaultBranch(repoDirpath)
	if err != nil {
		s.logger.Printf("Writeable-copy ref watcher: cannot determine default branch for '%s': %v", repoName, err)
		return
	}

	refsDirpath := filepath.Join(repoDirpath, ".git", "refs", "remotes", "origin")
	eventCh := make(chan notify.EventInfo, 256)
	if err := notify.Watch(refsDirpath, eventCh, notify.Create|notify.Write); err != nil {
		s.logger.Printf("Writeable-copy ref watcher: cannot watch '%s': %v", refsDirpath, err)
		return
	}
	defer notify.Stop(eventCh)

	debounce := time.NewTimer(0)
	if !debounce.Stop() {
		<-debounce.C
	}
	timerActive := false

	for {
		select {
		case <-ctx.Done():
			if !debounce.Stop() && timerActive {
				<-debounce.C
			}
			return
		case event := <-eventCh:
			if filepath.Base(event.Path()) != defaultBranch {
				continue
			}
			if event.Event()&(notify.Create|notify.Write) == 0 {
				continue
			}
			if !timerActive {
				timerActive = true
			} else if !debounce.Stop() {
				<-debounce.C
			}
			debounce.Reset(writeableCopyRefDebounce)
		case <-debounce.C:
			timerActive = false
			// Enqueue a library refresh through the existing push-event flow.
			select {
			case s.repoUpdateCh <- repoUpdateRequest{repoName: repoName}:
			default:
				s.logger.Printf("Writeable-copy ref watcher: repoUpdateCh full for '%s'", repoName)
			}
		}
	}
}

// isInsideRepoGitDir replaces the old isInsideGitDir helper. FSEvents will
// emit paths under .git/ since we now watch recursively without per-dir Add
// filtering; this check restores the previous "skip .git/" semantic.
func isInsideRepoGitDir(eventPath, repoDirpath string) bool {
	gitDir := filepath.Join(repoDirpath, ".git")
	rel, err := filepath.Rel(gitDir, eventPath)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

// eventIsDir returns true if the event path is a directory at evaluation time.
// Returns false if the path does not exist (event for a deleted item) — that's
// safe because gitignore directory-only patterns won't match a non-directory.
func eventIsDir(event notify.EventInfo) bool {
	info, err := os.Stat(event.Path())
	if err != nil {
		return false
	}
	return info.IsDir()
}

// enqueueReconcileForRepo is a small wrapper that adds to the channel without
// blocking. Used by the watcher and by external callers (config watcher).
func (s *Server) enqueueReconcileForRepo(repoName string) {
	select {
	case s.writeableCopyReconcileCh <- writeableCopyReconcileRequest{repoName: repoName}:
	default:
		s.logger.Printf("Writeable-copy reconcile: channel full, skipping '%s'", repoName)
	}
}

// ensureWriteableCopyExists makes sure a writeable copy exists on disk and is
// a clone of the matching repo. Behaviors:
//   - path missing → clone from origin (using the library copy's origin URL)
//   - path exists and origin matches → adopt
//   - path exists and origin does not match → return error (caller decides whether to surface)
func (s *Server) ensureWriteableCopyExists(repoName, repoDirpath string) error {
	gc := s.gitCommander
	if gc == nil {
		gc = newRealGit()
	}

	if _, err := os.Stat(repoDirpath); err != nil {
		if !os.IsNotExist(err) {
			return stacktrace.Propagate(err, "failed to stat '%v'", repoDirpath)
		}
		// Path absent → clone.
		expectedURL := s.expectedOriginURLForRepo(repoName)
		if expectedURL == "" {
			return stacktrace.NewError("cannot clone writeable copy for '%s': library copy missing or has no origin", repoName)
		}
		parent := filepath.Dir(repoDirpath)
		if err := os.MkdirAll(parent, 0755); err != nil {
			return stacktrace.Propagate(err, "failed to create parent directory '%v'", parent)
		}
		if err := gc.Clone(expectedURL, repoDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to clone '%v' for repo '%v'", repoDirpath, repoName)
		}
		s.logger.Printf("Writeable-copy: cloned '%s' from %s into %s", repoName, expectedURL, repoDirpath)
		return nil
	}

	// Path exists. Verify it is a git repo with the expected origin.
	if _, err := os.Stat(filepath.Join(repoDirpath, ".git")); err != nil {
		return stacktrace.NewError("writeable-copy path '%v' exists but is not a git repository", repoDirpath)
	}
	expectedURL := s.expectedOriginURLForRepo(repoName)
	if expectedURL == "" {
		return nil // library missing — assume the existing checkout is fine
	}
	actual, err := gc.OriginURL(repoDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read origin URL of writeable copy '%v'", repoDirpath)
	}
	if !originURLMatches(actual, expectedURL) {
		return stacktrace.NewError(
			"writeable-copy path '%v' is a clone of a different repo (origin '%v', expected to match '%v' from the library)",
			repoDirpath, actual, expectedURL,
		)
	}
	return nil
}

// reconcileWriteableCopiesFromConfig diffs the current set of writeable copies
// against the active watcher set, starting watchers for newly-added entries
// and stopping watchers for removed entries. Called at startup and on every
// config-change debounce.
func (s *Server) reconcileWriteableCopiesFromConfig(ctx context.Context) {
	cfg := s.getConfig()
	desired := cfg.GetAllWriteableCopies()

	if s.writeableCopyWatchers == nil {
		s.writeableCopyWatchers = newWriteableCopyWatchers()
	}

	active := make(map[string]bool)
	for _, repo := range s.writeableCopyWatchers.activeRepos() {
		active[repo] = true
	}

	for repoName, path := range desired {
		if active[repoName] {
			delete(active, repoName)
			continue
		}
		if err := s.ensureWriteableCopyExists(repoName, path); err != nil {
			s.logger.Printf("Writeable-copy: ensureExists failed for '%s' at '%s': %v", repoName, path, err)
			s.postWriteableCopyEnsureFailureNotification(repoName, path, err)
			continue
		}
		s.writeableCopyWatchers.start(ctx, s, repoName, path)
		s.enqueueReconcileForRepo(repoName)
		s.logger.Printf("Writeable-copy: started watchers for '%s' at '%s'", repoName, path)
	}

	// Anything left in `active` was removed from config.
	for repoName := range active {
		s.writeableCopyWatchers.stop(repoName)
		s.logger.Printf("Writeable-copy: stopped watchers for '%s' (removed from config)", repoName)
	}
}

func (s *Server) postWriteableCopyEnsureFailureNotification(repoName, path string, err error) {
	gc := s.gitCommander
	if gc == nil {
		gc = newRealGit()
	}
	body := fmt.Sprintf("# Cannot start writeable copy\n\n**Repo:** %s\n\n**Path:** %s\n\n**Error:** %v\n\nFix the underlying issue and run `agenc repo writeable-copy set %s %s` to retry.", repoName, path, err, repoName, path)
	_ = s.postWriteableCopyPause(gc, repoName, path, pauseReasonPathMissing, body)
}
