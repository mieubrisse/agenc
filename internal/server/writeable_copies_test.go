package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
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

	// onFetch runs inside Fetch(), so a test can model the remote moving
	// between one git call and the next — the race this package's retry logic
	// has to tell apart from a genuine divergence. Static fields cannot express
	// "origin/main was X when we pushed and Y once we fetched".
	onFetch func(f *fakeGit)

	addAllErr error

	commitErr error

	// commitMovesHeadTo, when set, makes Commit behave like a real one: the
	// working tree goes clean and HEAD advances to this value. Needed to test
	// the resume path, whose whole condition is "the tree is clean and HEAD has
	// moved" — a static fake can express the before or the after, not the act.
	commitMovesHeadTo string

	pullConflicted []string
	pullErr        error

	// rebaseMovesHeadTo, when set, makes a successful PullRebaseAutostash
	// advance HEAD the way a real rebase does. Without it a post-rebase test
	// would assert against a HEAD the rebase should have replaced.
	rebaseMovesHeadTo string

	rebaseAbortErr error

	mergeFFErr error

	pushResult pushResult
	pushErr    error

	// pushOutcomes, when non-empty, is consumed one entry per Push call so a
	// test can script "first push rejected, second accepted". Once drained,
	// Push falls back to the static pushResult/pushErr pair above.
	pushOutcomes []fakePushOutcome

	cloneErr error

	rebaseInProgress bool
	mergeInProgress  bool
	indexLockExists  bool

	revParseValues map[string]string
	revParseErr    error

	mergeBaseValue string
	mergeBaseErr   error

	// mergeBaseValueByRef overrides mergeBaseValue per local-side ref, for
	// tests where HEAD moves mid-tick and the two states need different
	// answers.
	mergeBaseValueByRef map[string]string

	// Call tracking
	addAllCalls   int
	commitCalls   int
	pushCalls     int
	fetchCalls    int
	mergeFFCalls  int
	pullCalls     int
	abortedRebase bool
}

// fakePushOutcome is one scripted answer from fakeGit.Push.
type fakePushOutcome struct {
	result pushResult
	err    error
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
	f.fetchCalls++
	if f.onFetch != nil {
		f.onFetch(f)
	}
	return f.fetchErr
}

func (f *fakeGit) AddAll(string) error {
	f.addAllCalls++
	return f.addAllErr
}

func (f *fakeGit) Commit(string, string) error {
	f.commitCalls++
	if f.commitErr != nil {
		return f.commitErr
	}
	if f.commitMovesHeadTo != "" {
		f.statusClean = true
		f.headValue = f.commitMovesHeadTo
	}
	return nil
}

func (f *fakeGit) PullRebaseAutostash(string) ([]string, error) {
	f.pullCalls++
	if f.pullErr != nil {
		return f.pullConflicted, f.pullErr
	}
	if f.rebaseMovesHeadTo != "" {
		f.headValue = f.rebaseMovesHeadTo
	}
	return nil, nil
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
	if len(f.pushOutcomes) > 0 {
		outcome := f.pushOutcomes[0]
		f.pushOutcomes = f.pushOutcomes[1:]
		return outcome.result, outcome.err
	}
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

func (f *fakeGit) MergeBase(_ string, a string, _ string) (string, error) {
	if f.mergeBaseErr != nil {
		return "", f.mergeBaseErr
	}
	if v, ok := f.mergeBaseValueByRef[a]; ok {
		return v, nil
	}
	return f.mergeBaseValue, nil
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
	committed, err := commitIfDirty(gc, "/tmp/foo")
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
	committed, err := commitIfDirty(gc, "/tmp/foo")
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

// gitFetchFirstRejectOutput is what `git push` actually prints when the remote
// has moved since the last fetch. Captured from git 2.x against a local bare
// remote on 2026-11-13 — note that it says "fetch first", not
// "non-fast-forward", which is why the classifier matches both.
const gitFetchFirstRejectOutput = `To /tmp/remote.git
 ! [rejected]        main -> main (fetch first)
error: failed to push some refs to '/tmp/remote.git'
hint: Updates were rejected because the remote contains work that you do not
hint: have locally. This is usually caused by another repository pushing to
hint: the same ref. If you want to integrate the remote changes, use
hint: 'git pull' before pushing again.`

// gitRefLockRejectOutput is the shape of the rejection that most likely caused
// the three KEV-230 pauses: a remote ref-lock collision, which git reports as
// a flat refusal with no hint that trying again would work. Captured on
// 2026-11-13 by pushing to a bare repo holding a stale `refs/heads/main.lock`.
//
// This is the fixture that matters most in this file. Nothing in this text
// says "retryable", so any classifier that decides from the message pauses on
// it — which is exactly what happened.
const gitRefLockRejectOutput = `remote: error: cannot lock ref 'refs/heads/main': Unable to create '/tmp/remote.git/./refs/heads/main.lock': File exists.
remote:
remote: Another git process seems to be running in this repository, e.g.
remote: an editor opened by 'git commit'. Please make sure all processes
remote: are terminated then try again.
To /tmp/remote.git
 ! [remote rejected] main -> main (failed to update ref)
error: failed to push some refs to '/tmp/remote.git'`

// gitHookDeclineOutput is a server-side decline — a repository rule or
// push-protection block. Captured from the same probe, with a pre-receive hook
// standing in for GitHub's rule engine.
const gitHookDeclineOutput = `remote: error: GH013: Repository rule violations found for refs/heads/main.
To /tmp/remote.git
 ! [remote rejected] main -> main (pre-receive hook declined)
error: failed to push some refs to '/tmp/remote.git'`

// A rejected push whose remote tip turns out to be an ancestor of local HEAD is
// the race KEV-230 was filed for: the daemon's own previous push had moved the
// ref, and a second attempt lands. Retrying is lossless, so no pause is posted.
func TestReconcile_RejectedPushRetriesWhenRemoteIsAncestorOfLocal(t *testing.T) {
	gc := &fakeGit{
		headValue:      "local",
		revParseValues: map[string]string{"origin/main": "remote"},
		mergeBaseValue: "remote", // ahead path: remote is contained in local
		pushOutcomes: []fakePushOutcome{
			{result: pushResult{Rejected: true, Output: gitFetchFirstRejectOutput}, err: errors.New("git push failed")},
			{},
		},
	}
	got, err := reconcileWithRemote(gc, "/tmp", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.PauseReason != "" {
		t.Errorf("expected no pause when the retry succeeds, got %+v", got)
	}
	if got.Outcome != "ahead" || got.Action != "push" {
		t.Errorf("expected ahead/push, got %+v", got)
	}
	if gc.pushCalls != 2 {
		t.Errorf("expected exactly 2 push attempts (original + one retry), got %d", gc.pushCalls)
	}
	if gc.fetchCalls != 2 {
		t.Errorf("expected a second fetch before the retry, got %d fetch calls", gc.fetchCalls)
	}
}

// The rejection whose wording gives a retry no encouragement at all — a remote
// ref-lock collision — must still be retried, because ancestry says it is
// safe. This is the case that caused the three KEV-230 pauses, and the one a
// message-based classifier gets wrong.
func TestReconcile_RefLockRejectionRetriesDespiteItsWording(t *testing.T) {
	gc := &fakeGit{
		headValue:      "local",
		revParseValues: map[string]string{"origin/main": "remote"},
		mergeBaseValue: "remote",
		pushOutcomes: []fakePushOutcome{
			{result: classifyPushOutput(gitRefLockRejectOutput), err: errors.New("git push failed")},
			{},
		},
	}
	got, err := reconcileWithRemote(gc, "/tmp", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.PauseReason != "" {
		t.Errorf("expected no pause for a ref-lock rejection the retry cleared, got %+v", got)
	}
	if gc.pushCalls != 2 {
		t.Errorf("expected the ref-lock rejection to be retried, got %d push attempts", gc.pushCalls)
	}
}

// A server-side decline — repository rule, protected branch, secret scanning —
// is retried on the same ancestry test as any other refusal. The retry costs
// one push and keeps the decision out of the business of reading git's prose.
func TestReconcile_HookDeclinedPushRetriesOnAncestryNotWording(t *testing.T) {
	gc := &fakeGit{
		headValue:      "local",
		revParseValues: map[string]string{"origin/main": "remote"},
		mergeBaseValue: "remote",
		pushOutcomes: []fakePushOutcome{
			{result: classifyPushOutput(gitHookDeclineOutput), err: errors.New("git push failed")},
			{},
		},
	}
	got, err := reconcileWithRemote(gc, "/tmp", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.PauseReason != "" {
		t.Errorf("expected no pause when the retry succeeded, got %+v", got)
	}
	if gc.pushCalls != 2 {
		t.Errorf("expected the decline to be retried, got %d push attempts", gc.pushCalls)
	}
}

// When the fetch that follows a rejection shows the remote holding commits the
// local branch does not have, there is nothing for a human to do: the next tick
// fast-forwards or rebases onto it. So this reports a transient failure rather
// than pausing — and the follow-up tick is asserted to actually take the
// rebase path, since the whole point is that the situation resolves itself.
func TestReconcile_RejectedPushWithMovedRemoteDefersToTheRebasePath(t *testing.T) {
	gc := &fakeGit{
		headValue:      "local",
		revParseValues: map[string]string{"origin/main": "remote"},
		mergeBaseValue: "remote",
		pushResult:     pushResult{Rejected: true, Output: gitFetchFirstRejectOutput},
		pushErr:        errors.New("git push failed"),
	}
	// The post-rejection fetch is what reveals that the remote moved.
	gc.onFetch = func(f *fakeGit) {
		if f.fetchCalls < 2 {
			return
		}
		f.revParseValues["origin/main"] = "remote2"
		f.mergeBaseValue = "ancestor"
	}

	got, err := reconcileWithRemote(gc, "/tmp", "main")
	if err == nil {
		t.Fatalf("expected a transient error when the remote moved, got result %+v", got)
	}
	if got.PauseReason != "" {
		t.Errorf("expected no pause when the remote merely moved, got %+v", got)
	}
	if gc.fetchCalls != 2 {
		t.Errorf("expected the rejection to be re-checked against a fresh fetch, got %d fetch calls", gc.fetchCalls)
	}
	if gc.pushCalls != 1 {
		t.Errorf("expected no retry when the remote has moved, got %d push attempts", gc.pushCalls)
	}
	if !strings.Contains(err.Error(), "fetch first") {
		t.Errorf("expected the error to quote git's own output, got: %v", err)
	}

	// The next tick: still diverged, so it rebases and pushes. Let the push
	// succeed this time to prove the path completes.
	gc.onFetch = nil
	gc.pushResult = pushResult{}
	gc.pushErr = nil
	next, err := reconcileWithRemote(gc, "/tmp", "main")
	if err != nil {
		t.Fatalf("expected the follow-up tick to resolve the divergence, got %v", err)
	}
	if next.Action != "rebase-and-push" {
		t.Errorf("expected the follow-up tick to rebase and push, got %+v", next)
	}
	if gc.pullCalls != 1 {
		t.Errorf("expected exactly one rebase on the follow-up tick, got %d", gc.pullCalls)
	}
}

// A retry that is itself rejected while the remote is still an ancestor stays
// transient. The sweep brings the repo back within the minute, which is what
// makes "come back next tick" a real outcome rather than a stall.
func TestReconcile_RejectedRetryStaysTransientRatherThanPausing(t *testing.T) {
	gc := &fakeGit{
		headValue:      "local",
		revParseValues: map[string]string{"origin/main": "remote"},
		mergeBaseValue: "remote",
		pushResult:     pushResult{Rejected: true, Output: gitRefLockRejectOutput},
		pushErr:        errors.New("git push failed"),
	}
	got, err := reconcileWithRemote(gc, "/tmp", "main")
	if err == nil {
		t.Fatalf("expected a transient error when both pushes are rejected, got result %+v", got)
	}
	if got.PauseReason != "" {
		t.Errorf("expected no pause for a repeatedly-rejected push against an ancestor remote, got %+v", got)
	}
	if gc.pushCalls != 2 {
		t.Errorf("expected exactly 2 push attempts, got %d", gc.pushCalls)
	}
}

// A refusal of the push that follows a rebase is the same situation as one on
// the plain-ahead path, so it gets the same ancestry check and the same retry.
//
// The rebase is modelled as actually moving HEAD, because the ancestry check
// reads HEAD after it: a fixture that left HEAD at its pre-rebase value would
// pass on the merge-base alone and prove less than it looks like it does.
func TestReconcile_RejectedPushAfterRebaseAlsoRetries(t *testing.T) {
	gc := &fakeGit{
		headValue:         "local",
		revParseValues:    map[string]string{"origin/main": "remote"},
		mergeBaseValue:    "ancestor", // diverged → rebase-and-push path
		rebaseMovesHeadTo: "localRebasedOntoRemote",
		// Pre-rebase local and the remote share only an older ancestor; the
		// rebased HEAD contains the remote tip, which is what makes the retry
		// safe.
		mergeBaseValueByRef: map[string]string{"localRebasedOntoRemote": "remote"},
		pushOutcomes: []fakePushOutcome{
			{result: pushResult{Rejected: true, Output: gitRefLockRejectOutput}, err: errors.New("git push failed")},
			{},
		},
	}
	got, err := reconcileWithRemote(gc, "/tmp", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.PauseReason != "" {
		t.Errorf("expected no pause when the post-rebase retry succeeds, got %+v", got)
	}
	if got.Action != "rebase-and-push" {
		t.Errorf("expected rebase-and-push, got %+v", got)
	}
	if gc.pullCalls != 1 {
		t.Errorf("expected exactly one rebase, got %d", gc.pullCalls)
	}
	if gc.pushCalls != 2 {
		t.Errorf("expected exactly 2 push attempts, got %d", gc.pushCalls)
	}
}

// A rejected push whose follow-up fetch shows the remote already at local HEAD
// means the push landed and the client only saw the error. There is nothing
// left to push and nothing to pause over.
func TestReconcile_RejectedPushThatAlreadyLandedIsNotAPause(t *testing.T) {
	gc := &fakeGit{
		headValue:      "local",
		revParseValues: map[string]string{"origin/main": "remote"},
		mergeBaseValue: "remote",
		pushResult:     pushResult{Rejected: true, Output: gitRefLockRejectOutput},
		pushErr:        errors.New("git push failed"),
	}
	gc.onFetch = func(f *fakeGit) {
		if f.fetchCalls < 2 {
			return
		}
		f.revParseValues["origin/main"] = "local"
	}
	got, err := reconcileWithRemote(gc, "/tmp", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.PauseReason != "" {
		t.Errorf("expected no pause when the remote already holds local HEAD, got %+v", got)
	}
	if gc.pushCalls != 1 {
		t.Errorf("expected no retry when there is nothing left to push, got %d push attempts", gc.pushCalls)
	}
}

func TestClassifyPushOutput(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want pushResult
	}{
		{
			name: "remote moved since the last fetch",
			out:  gitFetchFirstRejectOutput,
			want: pushResult{Rejected: true},
		},
		{
			name: "local branch behind its remote counterpart",
			out:  " ! [rejected]        main -> main (non-fast-forward)",
			want: pushResult{Rejected: true},
		},
		{
			name: "remote ref-lock collision",
			out:  gitRefLockRejectOutput,
			want: pushResult{Rejected: true},
		},
		{
			name: "repository rule violation",
			out:  gitHookDeclineOutput,
			want: pushResult{Rejected: true},
		},
		{
			name: "authentication failure",
			out:  "remote: Invalid username or password.\nfatal: Authentication failed for 'https://github.com/o/r.git/'",
			want: pushResult{AuthFailure: true},
		},
		{
			name: "network failure is neither a rejection nor an auth problem",
			out:  "fatal: unable to access 'https://github.com/o/r.git/': Could not resolve host: github.com",
			want: pushResult{},
		},
	}
	for _, c := range cases {
		got := classifyPushOutput(c.out)
		got.Output = "" // compared separately below
		if got != c.want {
			t.Errorf("%s: classifyPushOutput() = %+v, want %+v", c.name, got, c.want)
		}
		if classifyPushOutput(c.out).Output != c.out {
			t.Errorf("%s: expected the raw git output to be carried through", c.name)
		}
	}
}

func TestTrimGitOutputTail(t *testing.T) {
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	got := trimGitOutputTail(strings.Join(lines, "\n"))
	if strings.Contains(got, "line 9\n") {
		t.Errorf("expected the oldest lines to be dropped, got:\n%s", got)
	}
	if !strings.Contains(got, "line 29") {
		t.Errorf("expected the newest line to be kept, got:\n%s", got)
	}
	if n := len(strings.Split(got, "\n")); n > maxGitOutputLinesInPauseDetail {
		t.Errorf("expected at most %d lines, got %d", maxGitOutputLinesInPauseDetail, n)
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
	if gc.pushCalls != 1 {
		t.Errorf("expected no retry for an auth failure, got %d push attempts", gc.pushCalls)
	}
	if !strings.Contains(got.PauseDetail, "keeps committing") {
		t.Errorf("expected the pause notification to say that committing continues, got:\n%s", got.PauseDetail)
	}
}

// ----- Sweep and resume tests -----

// newWriteableCopyTestServer builds a server wired for one writeable copy, with a
// real database and the supplied fake git.
func newWriteableCopyTestServer(t *testing.T, repoName, repoDirpath string, gc GitCommander) *Server {
	t.Helper()

	return newWriteableCopyTestServerForRepos(t, map[string]string{repoName: repoDirpath}, gc)
}

// newWriteableCopyTestServerForRepos is the same for more than one writeable
// copy, which is what proves per-repo state really is per repo.
func newWriteableCopyTestServerForRepos(t *testing.T, repos map[string]string, gc GitCommander) *Server {
	t.Helper()

	tmpDir := t.TempDir()
	db, err := database.Open(filepath.Join(tmpDir, "database.sqlite"))
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	srv := &Server{
		agencDirpath:             tmpDir,
		logger:                   log.New(os.Stderr, "", 0),
		db:                       db,
		gitCommander:             gc,
		writeableCopyReconcileCh: make(chan writeableCopyReconcileRequest, writeableCopyReconcileChannelSize),
	}
	repoConfigs := make(map[string]config.RepoConfig, len(repos))
	for repoName, repoDirpath := range repos {
		repoConfigs[repoName] = config.RepoConfig{WriteableCopy: repoDirpath}
	}
	srv.cachedConfig.Store(&config.AgencConfig{RepoConfigs: repoConfigs})
	return srv
}

// takeReconcileRequest pops one reconcile request without blocking, so a test
// that expects one and finds none fails rather than hanging.
func takeReconcileRequest(srv *Server) (writeableCopyReconcileRequest, bool) {
	select {
	case request := <-srv.writeableCopyReconcileCh:
		return request, true
	default:
		return writeableCopyReconcileRequest{}, false
	}
}

// pauseRepo records a pause for a repo exactly as the sync loop would.
func pauseRepo(t *testing.T, srv *Server, repoName, headAtPause string) {
	t.Helper()

	notification := &database.Notification{
		ID:           uuid.New().String(),
		Kind:         notificationKindForPauseReason(pauseReasonNonFFReject),
		SourceRepo:   repoName,
		Title:        titleForPauseReason(pauseReasonNonFFReject, repoName),
		BodyMarkdown: "paused",
	}
	pause := &database.WriteableCopyPause{
		RepoName:         repoName,
		PausedReason:     pauseReasonNonFFReject,
		LocalHeadAtPause: headAtPause,
		NotificationID:   notification.ID,
	}
	if _, err := srv.db.UpsertPauseAndNotification(pause, notification); err != nil {
		t.Fatalf("failed to record the pause: %v", err)
	}
}

// runSweepAndTick runs one sweep pass and then the tick it enqueued for the
// named repo, which is what the timer does in production.
func runSweepAndTick(t *testing.T, srv *Server, repoName string) {
	t.Helper()

	srv.runWriteableCopySweepCycle()
	request, enqueued := takeReconcileRequest(srv)
	if !enqueued {
		t.Fatal("expected the sweep to enqueue a reconcile for the configured writeable copy")
	}
	if request.repoName != repoName {
		t.Fatalf("expected a reconcile for '%s', got '%s'", repoName, request.repoName)
	}
	if err := srv.runWriteableCopyTick(context.Background(), request.repoName); err != nil {
		t.Fatalf("tick failed: %v", err)
	}
}

// A pause whose tree has since gone clean and whose HEAD has moved cannot
// resume on watcher events alone, because the commit that satisfies both
// conditions writes only to .git/, which the watcher skips. The sweep is what
// breaks that, so the repo recovers with nobody touching a file.
func TestSweep_ResumesAPauseWithoutAWorkingTreeEvent(t *testing.T) {
	const repoName = "github.com/o/r"
	repoDirpath := t.TempDir()
	gc := &fakeGit{
		statusClean:    true,
		headValue:      "headAfterCommit",
		defaultBranch:  "main",
		currentBranch:  "main",
		revParseValues: map[string]string{"origin/main": "headAfterCommit"},
	}
	srv := newWriteableCopyTestServer(t, repoName, repoDirpath, gc)
	pauseRepo(t, srv, repoName, "headAtPause")

	runSweepAndTick(t, srv, repoName)

	if pause := currentPause(t, srv, repoName); pause != nil {
		t.Errorf("expected the pause to have cleared, still paused with reason '%s'", pause.PausedReason)
	}
}

// The deadlock KEV-230 names. A pause taken while missions are writing to the
// checkout used to be permanent: the resume probe wants a clean tree, the only
// thing that cleans it is the loop's own commit, and the commit sat behind the
// pause. Committing before the probe is what resolves it — in one tick, with
// nobody intervening.
func TestSweep_CommitsADirtyTreeWhilePausedAndThenResumes(t *testing.T) {
	const repoName = "github.com/o/r"
	repoDirpath := t.TempDir()
	gc := &fakeGit{
		statusClean:         false, // a mission is mid-write, as in all three incidents
		headValue:           "headAtPause",
		commitMovesHeadTo:   "headAfterAutoCommit",
		defaultBranch:       "main",
		currentBranch:       "main",
		revParseValues:      map[string]string{"origin/main": "headAtPause"},
		mergeBaseValueByRef: map[string]string{"headAfterAutoCommit": "headAtPause"},
	}
	srv := newWriteableCopyTestServer(t, repoName, repoDirpath, gc)
	pauseRepo(t, srv, repoName, "headAtPause")

	runSweepAndTick(t, srv, repoName)

	if gc.commitCalls != 1 {
		t.Errorf("expected the dirty tree to be committed while paused, got %d commits", gc.commitCalls)
	}
	if pause := currentPause(t, srv, repoName); pause != nil {
		t.Errorf("expected the pause to clear once the commit moved HEAD, still paused with reason '%s'", pause.PausedReason)
	}
	if gc.pushCalls != 1 {
		t.Errorf("expected the resumed tick to push the committed work, got %d push attempts", gc.pushCalls)
	}
}

// The sweep must not resume a repo whose state still warrants the pause.
func TestSweep_LeavesAPauseWhoseConditionStillHolds(t *testing.T) {
	const repoName = "github.com/o/r"
	repoDirpath := t.TempDir()
	gc := &fakeGit{
		statusClean:    true,
		headValue:      "headAtPause", // nothing has changed since the pause
		defaultBranch:  "main",
		currentBranch:  "main",
		revParseValues: map[string]string{"origin/main": "headAtPause"},
	}
	srv := newWriteableCopyTestServer(t, repoName, repoDirpath, gc)
	pauseRepo(t, srv, repoName, "headAtPause")

	runSweepAndTick(t, srv, repoName)

	if pause := currentPause(t, srv, repoName); pause == nil {
		t.Error("expected the repo to stay paused while HEAD has not moved")
	}
	if gc.pushCalls != 0 {
		t.Errorf("expected no push while still paused, got %d push attempts", gc.pushCalls)
	}
}

// The stall the sweep exists for on the unpaused side: a repo left with local
// commits unpushed after a transient failure. No pause row means the old
// paused-only timer would have skipped it, the watcher only fires on
// working-tree events, and the library fan-out only on a library HEAD change —
// so a quiet repo could sit unpushed indefinitely. The sweep pushes it.
func TestSweep_PushesARepoLeftAheadByATransientFailure(t *testing.T) {
	const repoName = "github.com/o/r"
	repoDirpath := t.TempDir()
	gc := &fakeGit{
		statusClean:    true,
		headValue:      "local",
		defaultBranch:  "main",
		currentBranch:  "main",
		revParseValues: map[string]string{"origin/main": "remote"},
		mergeBaseValue: "remote", // local is ahead: the earlier push never landed
	}
	srv := newWriteableCopyTestServer(t, repoName, repoDirpath, gc)

	runSweepAndTick(t, srv, repoName)

	if gc.pushCalls != 1 {
		t.Errorf("expected the sweep to push the unpushed commits, got %d push attempts", gc.pushCalls)
	}
	if pause := currentPause(t, srv, repoName); pause != nil {
		t.Errorf("expected no pause to be posted, got '%s'", pause.PausedReason)
	}
}

// ----- Standing-refusal tests -----

// newRefusingRemoteFake builds a repo that is one commit ahead with a remote
// that refuses the push — and keeps refusing it on the retry, since the retry
// is what makes a refusal countable.
func newRefusingRemoteFake() *fakeGit {
	return &fakeGit{
		statusClean:    true,
		headValue:      "local",
		defaultBranch:  "main",
		currentBranch:  "main",
		revParseValues: map[string]string{"origin/main": "remote"},
		mergeBaseValue: "remote", // the remote is an ancestor: the push is lossless
		pushResult:     pushResult{Rejected: true, Output: gitHookDeclineOutput},
		pushErr:        errors.New("git push failed"),
	}
}

// A refusal the retry cannot clear is transient only for as long as it might
// still be a collision. A remote that refuses a lossless push over and over is
// refusing it for a reason of its own — a repository rule, branch protection,
// secret scanning — and retrying that forever once a minute is the silent
// failure this whole change set exists to remove. The fifth refusal says so.
func TestStandingRefusal_PausesOnTheFifthConsecutiveRefusal(t *testing.T) {
	const repoName = "github.com/o/r"
	repoDirpath := t.TempDir()
	gc := newRefusingRemoteFake()
	srv := newWriteableCopyTestServer(t, repoName, repoDirpath, gc)

	for refusal := 1; refusal < maxConsecutivePushRefusals; refusal++ {
		runSweepAndTick(t, srv, repoName)
		if pause := currentPause(t, srv, repoName); pause != nil {
			t.Fatalf("paused after only %d refusals, with reason '%s'", refusal, pause.PausedReason)
		}
	}

	runSweepAndTick(t, srv, repoName)

	pause := currentPause(t, srv, repoName)
	if pause == nil {
		t.Fatalf("expected a pause on refusal %d", maxConsecutivePushRefusals)
	}
	if pause.PausedReason != pauseReasonPushRefused {
		t.Errorf("expected reason '%s', got '%s'", pauseReasonPushRefused, pause.PausedReason)
	}

	body := notificationBody(t, srv, pause.NotificationID)
	if !strings.Contains(body, "GH013") {
		t.Errorf("expected the notification to quote git's own output, got:\n%s", body)
	}
	if !strings.Contains(body, fmt.Sprintf("%d times in a row", maxConsecutivePushRefusals)) {
		t.Errorf("expected the notification to say how many refusals there were, got:\n%s", body)
	}
	if !strings.Contains(body, "keeps committing") {
		t.Errorf("expected the notification to say that committing continues, got:\n%s", body)
	}
}

// The counter means "in a row", so anything the remote accepts ends the run.
// Without the reset, a repo that hits an occasional collision would eventually
// accumulate five of them and pause for a condition that was never standing.
func TestStandingRefusal_ASuccessfulPushResetsTheCount(t *testing.T) {
	const repoName = "github.com/o/r"
	repoDirpath := t.TempDir()
	gc := newRefusingRemoteFake()
	srv := newWriteableCopyTestServer(t, repoName, repoDirpath, gc)

	for refusal := 1; refusal < maxConsecutivePushRefusals; refusal++ {
		runSweepAndTick(t, srv, repoName)
	}
	if pause := currentPause(t, srv, repoName); pause != nil {
		t.Fatalf("paused before the threshold, with reason '%s'", pause.PausedReason)
	}

	// The remote accepts one push, which ends the run.
	gc.pushResult = pushResult{}
	gc.pushErr = nil
	runSweepAndTick(t, srv, repoName)

	// Four more refusals must not be enough, because the count started over.
	gc.pushResult = pushResult{Rejected: true, Output: gitHookDeclineOutput}
	gc.pushErr = errors.New("git push failed")
	for refusal := 1; refusal < maxConsecutivePushRefusals; refusal++ {
		runSweepAndTick(t, srv, repoName)
	}

	if pause := currentPause(t, srv, repoName); pause != nil {
		t.Errorf("expected the successful push to reset the count, but it paused with '%s'", pause.PausedReason)
	}
}

// Each writeable copy has its own remote and its own rules, so refusals are
// counted per repo. A shared counter would pause both repos here — after five
// ticks between them — while neither had been refused five times.
func TestStandingRefusal_IsCountedPerRepo(t *testing.T) {
	const firstRepo = "github.com/o/first"
	const secondRepo = "github.com/o/second"
	gc := newRefusingRemoteFake()
	srv := newWriteableCopyTestServerForRepos(t, map[string]string{
		firstRepo:  t.TempDir(),
		secondRepo: t.TempDir(),
	}, gc)

	for refusal := 1; refusal < maxConsecutivePushRefusals; refusal++ {
		tickRepo(t, srv, firstRepo)
		tickRepo(t, srv, secondRepo)
	}

	for _, repoName := range []string{firstRepo, secondRepo} {
		if pause := currentPause(t, srv, repoName); pause != nil {
			t.Errorf("'%s' paused after %d of its own refusals, with reason '%s'",
				repoName, maxConsecutivePushRefusals-1, pause.PausedReason)
		}
	}
}

// tickRepo runs one tick for a named repo, bypassing the sweep so a test can
// interleave repos deterministically.
func tickRepo(t *testing.T, srv *Server, repoName string) {
	t.Helper()

	if err := srv.runWriteableCopyTick(context.Background(), repoName); err != nil {
		t.Fatalf("tick for '%s' failed: %v", repoName, err)
	}
}

func notificationBody(t *testing.T, srv *Server, notificationID string) string {
	t.Helper()

	notification, err := srv.db.GetNotification(notificationID)
	if err != nil {
		t.Fatalf("failed to read notification '%s': %v", notificationID, err)
	}
	if notification == nil {
		t.Fatalf("notification '%s' does not exist", notificationID)
	}
	return notification.BodyMarkdown
}

func currentPause(t *testing.T, srv *Server, repoName string) *database.WriteableCopyPause {
	t.Helper()

	pause, err := srv.db.GetPause(repoName)
	if err != nil {
		t.Fatalf("failed to read the pause: %v", err)
	}
	return pause
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
