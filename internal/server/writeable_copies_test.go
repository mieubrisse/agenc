package server

import (
	"errors"
	"testing"
)

// fakeGit is a hand-rolled mock for GitCommander used by writeable-copy
// tests. Each method consults the fields below; tests configure these as
// needed.
type fakeGit struct {
	statusClean      bool
	statusConflicted []string
	statusErr        error

	headValue string
	headErr   error

	defaultBranch    string
	defaultBranchErr error

	currentBranch    string
	currentBranchErr error

	originURL    string
	originURLErr error

	fetchErr error

	addAllErr error

	commitErr error

	pullConflicted []string
	pullErr        error

	rebaseAbortErr error

	mergeFFErr error

	pushResult pushResult
	pushErr    error

	cloneErr error

	rebaseInProgress bool
	mergeInProgress  bool
	indexLockExists  bool

	revParseValues map[string]string
	revParseErr    error

	mergeBaseValue string
	mergeBaseErr   error

	// Call tracking
	addAllCalls   int
	commitCalls   int
	pushCalls     int
	mergeFFCalls  int
	pullCalls     int
	abortedRebase bool
}

func (f *fakeGit) Status(string) (bool, []string, error) {
	return f.statusClean, f.statusConflicted, f.statusErr
}

func (f *fakeGit) HEAD(string) (string, error) {
	return f.headValue, f.headErr
}

func (f *fakeGit) DefaultBranch(string) (string, error) {
	return f.defaultBranch, f.defaultBranchErr
}

func (f *fakeGit) CurrentBranch(string) (string, error) {
	return f.currentBranch, f.currentBranchErr
}

func (f *fakeGit) OriginURL(string) (string, error) {
	return f.originURL, f.originURLErr
}

func (f *fakeGit) Fetch(string) error {
	return f.fetchErr
}

func (f *fakeGit) AddAll(string) error {
	f.addAllCalls++
	return f.addAllErr
}

func (f *fakeGit) Commit(string, string) error {
	f.commitCalls++
	return f.commitErr
}

func (f *fakeGit) PullRebaseAutostash(string) ([]string, error) {
	f.pullCalls++
	return f.pullConflicted, f.pullErr
}

func (f *fakeGit) RebaseAbort(string) error {
	f.abortedRebase = true
	return f.rebaseAbortErr
}

func (f *fakeGit) MergeFFOnly(string, string) error {
	f.mergeFFCalls++
	return f.mergeFFErr
}

func (f *fakeGit) Push(string) (pushResult, error) {
	f.pushCalls++
	return f.pushResult, f.pushErr
}

func (f *fakeGit) Clone(string, string) error {
	return f.cloneErr
}

func (f *fakeGit) IsRebaseInProgress(string) bool {
	return f.rebaseInProgress
}

func (f *fakeGit) IsMergeInProgress(string) bool {
	return f.mergeInProgress
}

func (f *fakeGit) IndexLockExists(string) bool {
	return f.indexLockExists
}

func (f *fakeGit) RevParse(_ string, ref string) (string, error) {
	if f.revParseErr != nil {
		return "", f.revParseErr
	}
	if v, ok := f.revParseValues[ref]; ok {
		return v, nil
	}
	if ref == "HEAD" {
		return f.headValue, nil
	}
	return "", errors.New("rev-parse: ref not found in fakeGit: " + ref)
}

func (f *fakeGit) MergeBase(string, string, string) (string, error) {
	return f.mergeBaseValue, f.mergeBaseErr
}

// ----- Sanity check tests -----

func TestSanityCheck_PathMissing(t *testing.T) {
	gc := &fakeGit{}
	got := runWriteableCopySanityCheck(gc, "/no/such/path/exists/123456789", "")
	if got.PauseReason != pauseReasonPathMissing {
		t.Errorf("expected pause reason '%s', got '%s'", pauseReasonPathMissing, got.PauseReason)
	}
}

func TestSanityCheck_IndexLockSilent(t *testing.T) {
	gc := &fakeGit{indexLockExists: true}
	got := runWriteableCopySanityCheck(gc, t.TempDir(), "")
	if !got.SkipSilently {
		t.Errorf("expected skip-silently for index lock, got %+v", got)
	}
}

func TestSanityCheck_RebaseInProgressSilent(t *testing.T) {
	gc := &fakeGit{rebaseInProgress: true}
	got := runWriteableCopySanityCheck(gc, t.TempDir(), "")
	if !got.SkipSilently {
		t.Errorf("expected skip-silently for rebase-in-progress, got %+v", got)
	}
}

func TestSanityCheck_WrongBranchPauses(t *testing.T) {
	gc := &fakeGit{
		defaultBranch:    "main",
		currentBranch:    "feature/foo",
		statusClean:      true,
		statusConflicted: nil,
	}
	got := runWriteableCopySanityCheck(gc, t.TempDir(), "")
	if got.PauseReason != pauseReasonWrongBranch {
		t.Errorf("expected wrong-branch pause, got %+v", got)
	}
}

func TestSanityCheck_OriginDriftPauses(t *testing.T) {
	gc := &fakeGit{
		defaultBranch: "main",
		currentBranch: "main",
		statusClean:   true,
		originURL:     "https://github.com/o/different",
	}
	got := runWriteableCopySanityCheck(gc, t.TempDir(), "https://github.com/o/expected")
	if got.PauseReason != pauseReasonOriginDrift {
		t.Errorf("expected origin-drift pause, got %+v", got)
	}
}

func TestSanityCheck_OriginURLNormalizationAcceptsSSHvsHTTPS(t *testing.T) {
	gc := &fakeGit{
		defaultBranch: "main",
		currentBranch: "main",
		statusClean:   true,
		originURL:     "git@github.com:o/r.git",
	}
	got := runWriteableCopySanityCheck(gc, t.TempDir(), "https://github.com/o/r")
	if got.PauseReason != "" || got.SkipSilently {
		t.Errorf("expected sanity to pass for ssh ↔ https equivalence, got %+v", got)
	}
}

func TestSanityCheck_ConflictMarkersInTreeSilent(t *testing.T) {
	gc := &fakeGit{
		defaultBranch:    "main",
		currentBranch:    "main",
		statusClean:      false,
		statusConflicted: []string{"foo.txt"},
	}
	got := runWriteableCopySanityCheck(gc, t.TempDir(), "")
	if !got.SkipSilently {
		t.Errorf("expected skip-silently for tree with conflict markers, got %+v", got)
	}
}

func TestSanityCheck_HappyPath(t *testing.T) {
	gc := &fakeGit{
		defaultBranch: "main",
		currentBranch: "main",
		statusClean:   true,
		originURL:     "https://github.com/o/r",
	}
	got := runWriteableCopySanityCheck(gc, t.TempDir(), "https://github.com/o/r")
	if got.PauseReason != "" || got.SkipSilently {
		t.Errorf("expected sanity pass, got %+v", got)
	}
}

// ----- commitIfDirty tests -----

func TestCommitIfDirty_CleanTreeNoOp(t *testing.T) {
	gc := &fakeGit{statusClean: true}
	committed, err := commitIfDirty(gc, "/tmp/foo", "host")
	if err != nil {
		t.Fatal(err)
	}
	if committed {
		t.Error("expected no commit when tree is clean")
	}
	if gc.addAllCalls != 0 || gc.commitCalls != 0 {
		t.Errorf("expected no add/commit calls; got add=%d commit=%d", gc.addAllCalls, gc.commitCalls)
	}
}

func TestCommitIfDirty_DirtyTreeCommits(t *testing.T) {
	gc := &fakeGit{statusClean: false}
	committed, err := commitIfDirty(gc, "/tmp/foo", "host")
	if err != nil {
		t.Fatal(err)
	}
	if !committed {
		t.Error("expected commit when tree is dirty")
	}
	if gc.addAllCalls != 1 || gc.commitCalls != 1 {
		t.Errorf("expected 1 add + 1 commit; got add=%d commit=%d", gc.addAllCalls, gc.commitCalls)
	}
}

// ----- reconcileWithRemote tests -----

func TestReconcile_Equal(t *testing.T) {
	gc := &fakeGit{
		headValue:      "abc",
		revParseValues: map[string]string{"origin/main": "abc"},
	}
	got, err := reconcileWithRemote(gc, "/tmp", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != "equal" || got.Action != "noop" {
		t.Errorf("expected equal/noop, got %+v", got)
	}
}

func TestReconcile_AheadPushes(t *testing.T) {
	gc := &fakeGit{
		headValue:      "local",
		revParseValues: map[string]string{"origin/main": "remote"},
		mergeBaseValue: "remote", // local has the remote tip in its history → ahead
	}
	got, err := reconcileWithRemote(gc, "/tmp", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != "ahead" || got.Action != "push" || gc.pushCalls != 1 {
		t.Errorf("expected ahead/push with 1 push call, got %+v pushCalls=%d", got, gc.pushCalls)
	}
}

func TestReconcile_BehindFastForwards(t *testing.T) {
	gc := &fakeGit{
		headValue:      "local",
		revParseValues: map[string]string{"origin/main": "remote"},
		mergeBaseValue: "local", // remote contains local → behind
	}
	got, err := reconcileWithRemote(gc, "/tmp", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != "behind" || got.Action != "ff" || gc.mergeFFCalls != 1 {
		t.Errorf("expected behind/ff, got %+v mergeFF=%d", got, gc.mergeFFCalls)
	}
}

func TestReconcile_DivergedRebaseSucceeds(t *testing.T) {
	gc := &fakeGit{
		headValue:      "local",
		revParseValues: map[string]string{"origin/main": "remote"},
		mergeBaseValue: "ancestor", // diverged
	}
	got, err := reconcileWithRemote(gc, "/tmp", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != "diverged" || got.Action != "rebase-and-push" || gc.pullCalls != 1 || gc.pushCalls != 1 {
		t.Errorf("expected diverged/rebase-and-push, got %+v pull=%d push=%d", got, gc.pullCalls, gc.pushCalls)
	}
}

func TestReconcile_RebaseConflictPauses(t *testing.T) {
	gc := &fakeGit{
		headValue:      "local",
		revParseValues: map[string]string{"origin/main": "remote"},
		mergeBaseValue: "ancestor",
		pullConflicted: []string{"a.txt", "b.txt"},
		pullErr:        errors.New("CONFLICT (content): Merge conflict in a.txt"),
	}
	got, err := reconcileWithRemote(gc, "/tmp", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.PauseReason != pauseReasonRebaseConflict {
		t.Errorf("expected rebase_conflict pause, got %+v", got)
	}
	if !gc.abortedRebase {
		t.Error("expected RebaseAbort to be called")
	}
	if gc.pushCalls != 0 {
		t.Errorf("expected no push after rebase conflict; got %d push calls", gc.pushCalls)
	}
}

func TestReconcile_NonFFRejectPauses(t *testing.T) {
	gc := &fakeGit{
		headValue:      "local",
		revParseValues: map[string]string{"origin/main": "remote"},
		mergeBaseValue: "remote", // ahead path
		pushResult:     pushResult{NonFFRejected: true},
		pushErr:        errors.New("rejected: non-fast-forward"),
	}
	got, err := reconcileWithRemote(gc, "/tmp", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.PauseReason != pauseReasonNonFFReject {
		t.Errorf("expected non_ff_reject pause, got %+v", got)
	}
}

func TestReconcile_AuthFailurePauses(t *testing.T) {
	gc := &fakeGit{
		headValue:      "local",
		revParseValues: map[string]string{"origin/main": "remote"},
		mergeBaseValue: "remote",
		pushResult:     pushResult{AuthFailure: true},
		pushErr:        errors.New("authentication failed"),
	}
	got, err := reconcileWithRemote(gc, "/tmp", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.PauseReason != pauseReasonAuthFailure {
		t.Errorf("expected auth_failure pause, got %+v", got)
	}
}

// ----- ANSI strip is unrelated; covered in notifications_strip_test.go -----

// ----- Helper / boundary -----

func TestNormalizeOriginURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"git@github.com:o/r.git", "github.com/o/r"},
		{"https://github.com/o/r", "github.com/o/r"},
		{"https://github.com/o/r.git", "github.com/o/r"},
		{"https://github.com/o/r/", "github.com/o/r"},
		{"ssh://git@github.com/o/r.git", "github.com/o/r"},
	}
	for _, c := range cases {
		if got := normalizeOriginURL(c.in); got != c.want {
			t.Errorf("normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
