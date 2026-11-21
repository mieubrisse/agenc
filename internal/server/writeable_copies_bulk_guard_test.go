package server

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/database"
)

// These tests are the guard's check-loop: they run under `make check`, so the
// pre-commit hook and CI both fail if a change turns the guard into a no-op —
// the measurement stops seeing untracked files, the limits stop firing, or the
// tick goes back to committing what it measured. KEV-223 has the incident the
// guard exists for.

// nFiles builds n pending changes of the given size under one directory.
func nFiles(relDirpath string, n int, bytesEach int64) []pendingChange {
	changes := make([]pendingChange, 0, n)
	for i := 0; i < n; i++ {
		changes = append(changes, pendingChange{relPath: fmt.Sprintf("%s/f%05d.png", relDirpath, i), bytes: bytesEach})
	}
	return changes
}

func TestSummarizePendingChanges_GroupsByDirectoryMostFilesFirst(t *testing.T) {
	var changes []pendingChange
	changes = append(changes, nFiles("OUTPUT/Large", 30, 1000)...)
	changes = append(changes, nFiles("OUTPUT/Small", 50, 10)...)
	changes = append(changes, pendingChange{relPath: "README.md", bytes: 7})
	for i := 0; i < 6; i++ {
		changes = append(changes, pendingChange{relPath: fmt.Sprintf("notes/d%d/x.md", i), bytes: 1})
	}

	volume := summarizePendingChanges(changes)

	if volume.files != 87 {
		t.Errorf("expected 87 files, got %d", volume.files)
	}
	if want := int64(30*1000 + 50*10 + 7 + 6); volume.bytes != want {
		t.Errorf("expected %d bytes, got %d", want, volume.bytes)
	}
	if len(volume.largestDirs) != maxDirsInBulkChangeDetail {
		t.Fatalf("expected the breakdown capped at %d directories, got %d", maxDirsInBulkChangeDetail, len(volume.largestDirs))
	}
	if got := volume.largestDirs[0]; got.relDirpath != "OUTPUT/Small" || got.files != 50 {
		t.Errorf("expected OUTPUT/Small with 50 files first, got %+v", got)
	}
	if got := volume.largestDirs[1]; got.relDirpath != "OUTPUT/Large" || got.files != 30 || got.bytes != 30000 {
		t.Errorf("expected OUTPUT/Large with 30 files and 30000 bytes second, got %+v", got)
	}
	// Ties on files and bytes fall back to the name, so "." (the repo root,
	// 7 bytes) outranks the one-byte directories and the order is stable.
	if got := volume.largestDirs[2]; got.relDirpath != "." {
		t.Errorf("expected the repo root third, got %+v", got)
	}
}

func TestChangeVolume_ExceedsAutoCommitLimitsAtTheBoundaries(t *testing.T) {
	cases := []struct {
		name  string
		files int
		bytes int64
		want  bool
	}{
		{"ordinary edit", 3, 20_000, false},
		{"99.9th percentile pass", 17, 1_800_000, false},
		{"exactly the file limit", autoCommitMaxFiles, 0, false},
		{"one file over", autoCommitMaxFiles + 1, 0, true},
		{"exactly the byte limit", 1, autoCommitMaxBytes, false},
		{"one byte over", 1, autoCommitMaxBytes + 1, true},
		{"the incident's smallest large commit", 261, 5_160_000, true},
	}
	for _, c := range cases {
		volume := &changeVolume{files: c.files, bytes: c.bytes}
		if got := volume.exceedsAutoCommitLimits(); got != c.want {
			t.Errorf("%s (%d files, %d bytes): expected exceeds=%v, got %v", c.name, c.files, c.bytes, c.want, got)
		}
	}
}

func TestCommitIfDirty_HoldsABulkPassWithoutStagingAnything(t *testing.T) {
	gc := &fakeGit{statusClean: false, pendingChanges: nFiles("OUTPUT", autoCommitMaxFiles+1, 100)}

	result, err := commitIfDirty(gc, "/tmp/foo")
	if err != nil {
		t.Fatal(err)
	}
	if result.committed {
		t.Error("expected no commit for a pass over the file limit")
	}
	if result.heldVolume == nil || result.heldVolume.files != autoCommitMaxFiles+1 {
		t.Fatalf("expected the held volume to report %d files, got %+v", autoCommitMaxFiles+1, result.heldVolume)
	}
	if gc.addAllCalls != 0 || gc.commitCalls != 0 {
		t.Errorf("expected nothing staged or committed; got add=%d commit=%d", gc.addAllCalls, gc.commitCalls)
	}
}

func TestCommitIfDirty_HoldsOneHugeFile(t *testing.T) {
	gc := &fakeGit{statusClean: false, pendingChanges: []pendingChange{{relPath: "model.ckpt", bytes: autoCommitMaxBytes + 1}}}

	result, err := commitIfDirty(gc, "/tmp/foo")
	if err != nil {
		t.Fatal(err)
	}
	if result.heldVolume == nil || gc.commitCalls != 0 {
		t.Errorf("expected a single file over the byte limit to be held; held=%+v commits=%d", result.heldVolume, gc.commitCalls)
	}
}

func TestCommitIfDirty_OrdinaryEditingStillCommits(t *testing.T) {
	gc := &fakeGit{statusClean: false, pendingChanges: nFiles("notes", 17, 100_000)}

	result, err := commitIfDirty(gc, "/tmp/foo")
	if err != nil {
		t.Fatal(err)
	}
	if !result.committed || result.heldVolume != nil {
		t.Errorf("expected an ordinary pass to commit; got %+v", result)
	}
	if gc.addAllCalls != 1 || gc.commitCalls != 1 {
		t.Errorf("expected 1 add + 1 commit; got add=%d commit=%d", gc.addAllCalls, gc.commitCalls)
	}
}

// A measurement that fails must not fall through to committing: an unmeasured
// pass is exactly the one the guard cannot vouch for.
func TestCommitIfDirty_DoesNotCommitWhenTheMeasurementFails(t *testing.T) {
	gc := &fakeGit{statusClean: false, pendingChangesErr: fmt.Errorf("status failed")}

	if _, err := commitIfDirty(gc, "/tmp/foo"); err == nil {
		t.Error("expected the measurement failure to propagate")
	}
	if gc.addAllCalls != 0 || gc.commitCalls != 0 {
		t.Errorf("expected nothing staged or committed; got add=%d commit=%d", gc.addAllCalls, gc.commitCalls)
	}
}

// The whole episode, on a repo with nothing else to push — the case where
// carrying on to reconcile would have cleared the record and re-notified every
// minute. Held: not committed, not pushed, told once with the repo and volume
// named. Released: by the person's choice, after which it syncs and clears
// quietly.
func TestSweep_ABulkPassIsHeldNotifiedOnceAndReleasedByResolvingIt(t *testing.T) {
	const repoName = "github.com/o/r"
	const ticks = 3
	repoDirpath := t.TempDir()
	gc := &fakeGit{
		statusClean:    false,
		pendingChanges: nFiles("practice/image-pipeline-01/OUTPUT/Large", 9561, 3576),
		headValue:      "local",
		defaultBranch:  "main",
		currentBranch:  "main",
		revParseValues: map[string]string{"origin/main": "local"},
	}
	srv := newWriteableCopyTestServer(t, repoName, repoDirpath, gc)

	for tick := 1; tick <= ticks; tick++ {
		runSweepAndTick(t, srv, repoName)

		pause := currentPause(t, srv, repoName)
		if pause == nil || pause.PausedReason != pauseReasonBulkChange {
			t.Fatalf("tick %d: expected a bulk_change record, got %+v", tick, pause)
		}
	}
	if gc.addAllCalls != 0 || gc.commitCalls != 0 {
		t.Errorf("expected nothing staged or committed while held; got add=%d commit=%d", gc.addAllCalls, gc.commitCalls)
	}
	if gc.fetchCalls != 0 || gc.pushCalls != 0 {
		t.Errorf("expected the tick to stop before reconciling; got fetch=%d push=%d", gc.fetchCalls, gc.pushCalls)
	}

	notifications, err := srv.db.ListNotifications(database.ListNotificationsParams{SourceRepo: repoName})
	if err != nil {
		t.Fatalf("failed to list notifications: %v", err)
	}
	if len(notifications) != 1 {
		t.Fatalf("expected one notification for the episode, got %d across %d ticks", len(notifications), ticks)
	}
	notification := notifications[0]
	if notification.Kind != "writeable_copy.bulk_change" {
		t.Errorf("expected kind writeable_copy.bulk_change, got '%s'", notification.Kind)
	}
	for _, want := range []string{repoName, "9561 new or changed files", "34.2 MB"} {
		if !strings.Contains(notification.Title, want) {
			t.Errorf("expected the title to name '%s', got '%s'", want, notification.Title)
		}
	}
	for _, want := range []string{repoDirpath, "practice/image-pipeline-01/OUTPUT/Large", ".gitignore", "commit -m", "Nothing has been staged, committed or deleted"} {
		if !strings.Contains(notification.BodyMarkdown, want) {
			t.Errorf("expected the body to contain '%s', got:\n%s", want, notification.BodyMarkdown)
		}
	}

	// The person gitignores the output. What is left is an ordinary edit.
	gc.pendingChanges = nFiles("notes", 1, 2_000)
	gc.commitMovesHeadTo = "localAfterAutoCommit"
	gc.revParseValues = map[string]string{"origin/main": "local"}
	gc.mergeBaseValue = "local" // the new commit is ahead of the remote
	runSweepAndTick(t, srv, repoName)

	if gc.commitCalls != 1 || gc.pushCalls != 1 {
		t.Errorf("expected the released tick to commit and push once; got commit=%d push=%d", gc.commitCalls, gc.pushCalls)
	}
	if pause := currentPause(t, srv, repoName); pause != nil {
		t.Errorf("expected the record to clear once the tree was back within limits, still '%s'", pause.PausedReason)
	}
	if n := countNotifications(t, srv, repoName); n != 1 {
		t.Errorf("expected clearing to be quiet, got %d notifications", n)
	}
}

// ----- realGit.PendingChanges against a real repository -----

func runGitForTest(t *testing.T, repoDirpath string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repoDirpath
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeFileForTest(t *testing.T, filepathToWrite string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepathToWrite), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepathToWrite, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newRepoForTest(t *testing.T) string {
	t.Helper()
	repoDirpath := t.TempDir()
	runGitForTest(t, repoDirpath, "init", "-q")
	runGitForTest(t, repoDirpath, "config", "user.email", "test@example.com")
	runGitForTest(t, repoDirpath, "config", "user.name", "test")
	writeFileForTest(t, filepath.Join(repoDirpath, "tracked.md"), "v1")
	writeFileForTest(t, filepath.Join(repoDirpath, "doomed.md"), "bye")
	writeFileForTest(t, filepath.Join(repoDirpath, ".gitignore"), "ignored/\n")
	runGitForTest(t, repoDirpath, "add", "-A")
	runGitForTest(t, repoDirpath, "commit", "-q", "-m", "init")
	return repoDirpath
}

// Untracked files are counted one by one, not as their directory; modified
// files count at their new size; deletions and ignored files count for
// nothing. A path with a space survives, because -z output is unquoted.
func TestRealGitPendingChanges_CountsWhatAnAutoCommitWouldAdd(t *testing.T) {
	repoDirpath := newRepoForTest(t)
	writeFileForTest(t, filepath.Join(repoDirpath, "tracked.md"), "version two")
	if err := os.Remove(filepath.Join(repoDirpath, "doomed.md")); err != nil {
		t.Fatal(err)
	}
	writeFileForTest(t, filepath.Join(repoDirpath, "OUTPUT", "a.png"), "aaaa")
	writeFileForTest(t, filepath.Join(repoDirpath, "OUTPUT", "nested", "b c.png"), "bb")
	writeFileForTest(t, filepath.Join(repoDirpath, "ignored", "huge.bin"), strings.Repeat("x", 10_000))

	changes, err := (&realGit{}).PendingChanges(repoDirpath)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]int64{}
	for _, change := range changes {
		got[change.relPath] = change.bytes
	}
	want := map[string]int64{"tracked.md": 11, "OUTPUT/a.png": 4, "OUTPUT/nested/b c.png": 2}
	if len(got) != len(want) {
		t.Errorf("expected exactly %v, got %v", want, got)
	}
	for relPath, bytes := range want {
		if got[relPath] != bytes {
			t.Errorf("expected '%s' at %d bytes, got %v", relPath, bytes, got)
		}
	}
}

// One file over the byte limit is held on size alone. The file is sparse, so
// the test costs no disk: what is measured is the size git would store.
func TestCommitIfDirty_RealGitHoldsOneFileOverTheByteLimit(t *testing.T) {
	repoDirpath := newRepoForTest(t)
	checkpointFilepath := filepath.Join(repoDirpath, "checkpoints", "model.ckpt")
	writeFileForTest(t, checkpointFilepath, "")
	if err := os.Truncate(checkpointFilepath, autoCommitMaxBytes+1); err != nil {
		t.Fatal(err)
	}

	result, err := commitIfDirty(&realGit{}, repoDirpath)
	if err != nil {
		t.Fatal(err)
	}
	if result.heldVolume == nil || result.heldVolume.files != 1 || result.heldVolume.bytes != autoCommitMaxBytes+1 {
		t.Fatalf("expected one file of %d bytes held, got %+v", autoCommitMaxBytes+1, result.heldVolume)
	}
}

// The guard end to end against real git: a bulk write is held with every file
// still on disk and HEAD unmoved; once it is gitignored, the ordinary edit
// beside it commits.
func TestCommitIfDirty_RealGitHoldsABulkWriteAndKeepsEveryFile(t *testing.T) {
	repoDirpath := newRepoForTest(t)
	bulkFileCount := autoCommitMaxFiles + 50
	for i := 0; i < bulkFileCount; i++ {
		writeFileForTest(t, filepath.Join(repoDirpath, "OUTPUT", fmt.Sprintf("img%04d.png", i)), "png")
	}
	writeFileForTest(t, filepath.Join(repoDirpath, "tracked.md"), "an ordinary edit")
	gc := &realGit{}
	headBefore, err := gc.HEAD(repoDirpath)
	if err != nil {
		t.Fatal(err)
	}

	result, err := commitIfDirty(gc, repoDirpath)
	if err != nil {
		t.Fatal(err)
	}
	if result.heldVolume == nil || result.heldVolume.files != bulkFileCount+1 {
		t.Fatalf("expected %d pending files held, got %+v", bulkFileCount+1, result.heldVolume)
	}
	headAfter, err := gc.HEAD(repoDirpath)
	if err != nil {
		t.Fatal(err)
	}
	if headAfter != headBefore {
		t.Errorf("expected HEAD unmoved while held, went from %s to %s", headBefore, headAfter)
	}
	entries, err := os.ReadDir(filepath.Join(repoDirpath, "OUTPUT"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != bulkFileCount {
		t.Errorf("expected all %d bulk files still on disk, found %d", bulkFileCount, len(entries))
	}

	writeFileForTest(t, filepath.Join(repoDirpath, ".gitignore"), "ignored/\nOUTPUT/\n")
	result, err = commitIfDirty(gc, repoDirpath)
	if err != nil {
		t.Fatal(err)
	}
	if !result.committed {
		t.Fatalf("expected the ordinary edit to commit once the output was ignored, got %+v", result)
	}
	clean, _, err := gc.Status(repoDirpath)
	if err != nil {
		t.Fatal(err)
	}
	if !clean {
		t.Error("expected a clean tree after the release commit")
	}
}
