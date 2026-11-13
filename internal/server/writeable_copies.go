package server

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

// Pause-reason constants. Used both as the persisted "paused_reason" column
// value and as the kind tag on the linked notification.
const (
	pauseReasonRebaseConflict = "rebase_conflict"
	// pauseReasonNonFFReject is no longer posted by any code path. A rejected
	// push is now re-checked against a fresh fetch (pushWithRejectionRetry),
	// and the two states that used to land here are both handled without a
	// human: the remote being an ancestor is retried, and the remote having
	// moved is left to the next tick's fast-forward or rebase. The constant
	// stays so that pause rows and notifications written before that change
	// still render a title rather than falling through to the generic one.
	pauseReasonNonFFReject = "non_ff_reject"
	// pauseReasonPushRefused is posted when the remote has refused the same
	// lossless push maxConsecutivePushRefusals times running — a standing
	// refusal rather than the transient kind the retry exists for.
	pauseReasonPushRefused = "push_refused"
	pauseReasonAuthFailure = "auth_failure"
	pauseReasonWrongBranch = "wrong_branch"
	pauseReasonOriginDrift = "origin_drift"
	pauseReasonPathMissing = "path_missing"
	pauseReasonGitCorrupt  = "git_corrupt"
)

// maxGitOutputLinesInPauseDetail bounds how much of git's output a pause
// carries. The last lines are the ones that say what happened; the leading ones
// are progress chatter.
const maxGitOutputLinesInPauseDetail = 20

// maxConsecutivePushRefusals is how many refusals in a row of a push that a
// fetch proved lossless are tolerated before the loop pauses and says so.
//
// The retry exists because a single refusal is usually transient — a remote
// ref-lock collision, the daemon racing its own previous push. But a repository
// rule, branch protection or a secret-scanning block refuses the identical push
// every time, and retrying one of those once a minute forever would be the
// silent failure this whole change set exists to remove. Counting is the only
// way to tell the two apart, because at the moment of failure they look
// identical: git's wording is not a reliable discriminator, which is the
// lesson of the three KEV-230 incidents.
//
// Five, against the sweep's one-minute cadence, surfaces a standing refusal in
// roughly five minutes while leaving ample room for a flapping remote.
const maxConsecutivePushRefusals = 5

// notificationKindForPauseReason maps a pause reason to its notification kind.
// The kind is namespaced under "writeable_copy." for filtering.
func notificationKindForPauseReason(reason string) string {
	return "writeable_copy." + reason
}

// sanityResult communicates the outcome of pre-tick sanity checks.
type sanityResult struct {
	// SkipSilently means abort the tick without notifying — typical for
	// transient states (index lock held, in-progress rebase from manual git
	// command, etc.). Caller retries on next trigger.
	SkipSilently bool
	// PauseReason is set when the sanity check found a state that warrants
	// pausing the loop and posting a notification. Empty when SkipSilently is
	// true or when sanity passed.
	PauseReason string
	// PauseDetail is human-readable detail about the pause reason, included
	// in the notification body.
	PauseDetail string
}

// runWriteableCopySanityCheck examines the writeable copy's directory and git
// state. It returns one of three outcomes:
//   - all-zero result → sanity passed; proceed with the tick
//   - SkipSilently=true → transient state; retry next trigger, no notification
//   - PauseReason non-empty → pause + notify
func runWriteableCopySanityCheck(gc GitCommander, repoDirpath, expectedOriginURL string) sanityResult {
	if _, err := os.Stat(repoDirpath); err != nil {
		return sanityResult{
			PauseReason: pauseReasonPathMissing,
			PauseDetail: fmt.Sprintf("the writeable-copy path '%s' does not exist (was it deleted manually?)", repoDirpath),
		}
	}
	if gc.IndexLockExists(repoDirpath) {
		return sanityResult{SkipSilently: true}
	}
	if gc.IsRebaseInProgress(repoDirpath) || gc.IsMergeInProgress(repoDirpath) {
		return sanityResult{SkipSilently: true}
	}
	defaultBranch, err := gc.DefaultBranch(repoDirpath)
	if err != nil {
		return sanityResult{
			PauseReason: pauseReasonGitCorrupt,
			PauseDetail: fmt.Sprintf("could not read default branch from origin/HEAD in '%s': %v", repoDirpath, err),
		}
	}
	currentBranch, err := gc.CurrentBranch(repoDirpath)
	if err != nil {
		return sanityResult{
			PauseReason: pauseReasonGitCorrupt,
			PauseDetail: fmt.Sprintf("could not determine current branch in '%s': %v", repoDirpath, err),
		}
	}
	if currentBranch != defaultBranch {
		return sanityResult{
			PauseReason: pauseReasonWrongBranch,
			PauseDetail: fmt.Sprintf("the writeable copy is on branch '%s'; the sync loop only operates on the default branch '%s'. Switch back with: git -C %s checkout %s", currentBranch, defaultBranch, repoDirpath, defaultBranch),
		}
	}
	// Tree may have conflicts from a manual merge — refuse silently in that
	// case so we don't blow away the user's in-progress resolution.
	_, conflicted, err := gc.Status(repoDirpath)
	if err != nil {
		return sanityResult{
			PauseReason: pauseReasonGitCorrupt,
			PauseDetail: fmt.Sprintf("git status failed in '%s': %v", repoDirpath, err),
		}
	}
	if len(conflicted) > 0 {
		return sanityResult{SkipSilently: true}
	}
	if expectedOriginURL != "" {
		actual, err := gc.OriginURL(repoDirpath)
		if err != nil {
			return sanityResult{
				PauseReason: pauseReasonGitCorrupt,
				PauseDetail: fmt.Sprintf("could not read origin URL in '%s': %v", repoDirpath, err),
			}
		}
		if !originURLMatches(actual, expectedOriginURL) {
			return sanityResult{
				PauseReason: pauseReasonOriginDrift,
				PauseDetail: fmt.Sprintf("the writeable copy's origin URL '%s' does not match the expected URL '%s'. Restore it with: git -C %s remote set-url origin %s", actual, expectedOriginURL, repoDirpath, expectedOriginURL),
			}
		}
	}
	return sanityResult{}
}

// originURLMatches reports whether two origin URLs refer to the same remote.
// Tolerates ssh ↔ https variations and trailing ".git".
func originURLMatches(a, b string) bool {
	return normalizeOriginURL(a) == normalizeOriginURL(b)
}

func normalizeOriginURL(url string) string {
	u := strings.TrimSpace(url)
	u = strings.TrimSuffix(u, ".git")
	u = strings.TrimSuffix(u, "/")
	// git@github.com:owner/repo  → github.com/owner/repo
	if strings.HasPrefix(u, "git@") {
		u = strings.TrimPrefix(u, "git@")
		u = strings.Replace(u, ":", "/", 1)
	}
	// https://github.com/owner/repo
	for _, prefix := range []string{"https://", "http://", "ssh://git@"} {
		u = strings.TrimPrefix(u, prefix)
	}
	return u
}

// commitIfDirty stages and commits any working-tree changes. Returns whether
// a commit was made.
func commitIfDirty(gc GitCommander, repoDirpath string) (bool, error) {
	clean, _, err := gc.Status(repoDirpath)
	if err != nil {
		return false, stacktrace.Propagate(err, "failed to read status before auto-commit in '%v'", repoDirpath)
	}
	if clean {
		return false, nil
	}
	if err := gc.AddAll(repoDirpath); err != nil {
		return false, stacktrace.Propagate(err, "failed to git-add in '%v'", repoDirpath)
	}
	msg := fmt.Sprintf("auto-sync: %s", time.Now().UTC().Format(time.RFC3339))
	if err := gc.Commit(repoDirpath, msg); err != nil {
		return false, stacktrace.Propagate(err, "failed to commit in '%v'", repoDirpath)
	}
	return true, nil
}

// reconcileResult describes the disposition of a fetch+reconcile cycle.
type reconcileResult struct {
	// Outcome is one of: "equal", "ahead", "behind", "diverged".
	Outcome string
	// Action describes the action taken: "noop", "push", "ff", "rebase-and-push".
	Action string
	// PauseReason is set when reconcile failed in a way that requires user
	// intervention (rebase conflict, non-FF reject, auth failure).
	PauseReason string
	// PauseDetail is the human-readable detail accompanying PauseReason.
	PauseDetail string
	// RefusedPushOutput is set, alongside a transient error, when the remote
	// refused a push and refused it again after a fetch had shown the remote
	// to be an ancestor of local HEAD — a refusal of a push that cannot lose
	// anything. It carries git's output from the second refusal, and its
	// presence is what the caller counts towards maxConsecutivePushRefusals.
	RefusedPushOutput string
}

// reconcileWithRemote runs a single fetch + reconcile cycle. The repo is
// expected to already pass sanity checks.
func reconcileWithRemote(gc GitCommander, repoDirpath, defaultBranch string) (reconcileResult, error) {
	if err := gc.Fetch(repoDirpath); err != nil {
		// Treat fetch failure as transient at this layer; caller decides how
		// to track repeated failures over time.
		return reconcileResult{}, stacktrace.Propagate(err, "git fetch failed in '%v'", repoDirpath)
	}
	localHead, err := gc.HEAD(repoDirpath)
	if err != nil {
		return reconcileResult{}, stacktrace.Propagate(err, "failed to read local HEAD in '%v'", repoDirpath)
	}
	remoteRef := "origin/" + defaultBranch
	remoteHead, err := gc.RevParse(repoDirpath, remoteRef)
	if err != nil {
		return reconcileResult{}, stacktrace.Propagate(err, "failed to read %v in '%v'", remoteRef, repoDirpath)
	}

	if localHead == remoteHead {
		return reconcileResult{Outcome: "equal", Action: "noop"}, nil
	}

	// Determine ahead / behind / diverged via merge-base.
	mergeBase, err := gc.MergeBase(repoDirpath, localHead, remoteHead)
	if err != nil {
		return reconcileResult{}, stacktrace.Propagate(err, "failed to compute merge-base in '%v'", repoDirpath)
	}

	switch {
	case mergeBase == remoteHead:
		// Local is ahead of remote → push.
		failure, err := pushWithRejectionRetry(gc, repoDirpath, defaultBranch)
		if err != nil {
			// failure, not a zero result: it may carry RefusedPushOutput,
			// which the caller counts.
			return failure, err
		}
		if failure.PauseReason != "" {
			return failure, nil
		}
		return reconcileResult{Outcome: "ahead", Action: "push"}, nil
	case mergeBase == localHead:
		// Remote is ahead of local → fast-forward.
		if err := gc.MergeFFOnly(repoDirpath, remoteRef); err != nil {
			return reconcileResult{}, stacktrace.Propagate(err, "fast-forward to '%v' failed in '%v'", remoteRef, repoDirpath)
		}
		return reconcileResult{Outcome: "behind", Action: "ff"}, nil
	default:
		// Diverged → rebase-and-push.
		conflicted, rebaseErr := gc.PullRebaseAutostash(repoDirpath)
		if rebaseErr != nil {
			_ = gc.RebaseAbort(repoDirpath)
			return reconcileResult{
				Outcome:     "diverged",
				PauseReason: pauseReasonRebaseConflict,
				PauseDetail: buildRebaseConflictDetail(repoDirpath, conflicted, localHead, remoteHead),
			}, nil
		}
		failure, err := pushWithRejectionRetry(gc, repoDirpath, defaultBranch)
		if err != nil {
			// failure, not a zero result: it may carry RefusedPushOutput,
			// which the caller counts.
			return failure, err
		}
		if failure.PauseReason != "" {
			failure.Outcome = "diverged"
			return failure, nil
		}
		return reconcileResult{Outcome: "diverged", Action: "rebase-and-push"}, nil
	}
}

// pushWithRejectionRetry pushes, and decides what a failure means.
//
// Its three answers: a zero result with a nil error means the push landed; a
// result carrying a PauseReason means the failure needs a human; a non-nil
// error means the failure is transient and a later tick should try again.
//
// The rule it encodes is that ancestry, not git's wording, decides whether a
// retry is safe. Any refused push is re-examined against a fresh fetch, and
// when origin/<branch> turns out to be an ancestor of local HEAD, pushing
// again cannot discard anything the remote holds — so it is pushed again,
// whatever reason the remote gave for refusing. That is what the three KEV-230
// incidents needed: checked against the reflog afterwards, the last change to
// origin/main before each pause was this checkout's own push 9 to 17 seconds
// earlier, so the remote was an ancestor every time and a retry would have
// landed. Classifying from the message would have paused all three.
//
// Nothing here pauses for a remote that has moved. Behind and diverged are
// states reconcileWithRemote already resolves without a human, by
// fast-forwarding or by rebasing, and the exobrain remote demonstrably has a
// second writer — 13 `fetch origin: fast-forward` entries in its reflog — so
// that path is live rather than theoretical. A transient error hands the
// situation to the next tick, which takes the ordinary path.
func pushWithRejectionRetry(gc GitCommander, repoDirpath, defaultBranch string) (reconcileResult, error) {
	result, pushErr := gc.Push(repoDirpath)
	if pushErr == nil {
		return reconcileResult{}, nil
	}
	if !result.Rejected {
		return classifyPushFailure(result, pushErr, repoDirpath)
	}

	if err := gc.Fetch(repoDirpath); err != nil {
		// Without a fresh view of the remote there is no way to know whether a
		// retry is lossless, and guessing is worse than coming back next tick.
		return reconcileResult{}, stacktrace.Propagate(err, "git fetch after a rejected push failed in '%v'", repoDirpath)
	}
	localHead, err := gc.HEAD(repoDirpath)
	if err != nil {
		return reconcileResult{}, stacktrace.Propagate(err, "failed to read local HEAD after a rejected push in '%v'", repoDirpath)
	}
	remoteRef := "origin/" + defaultBranch
	remoteHead, err := gc.RevParse(repoDirpath, remoteRef)
	if err != nil {
		return reconcileResult{}, stacktrace.Propagate(err, "failed to read %v after a rejected push in '%v'", remoteRef, repoDirpath)
	}
	if localHead == remoteHead {
		// The push landed and only the client saw an error. Nothing to retry
		// and nothing to pause over.
		return reconcileResult{}, nil
	}
	mergeBase, err := gc.MergeBase(repoDirpath, localHead, remoteHead)
	if err != nil {
		return reconcileResult{}, stacktrace.Propagate(err, "failed to compute merge-base after a rejected push in '%v'", repoDirpath)
	}
	if mergeBase != remoteHead {
		// The remote moved. Fast-forwarding or rebasing onto it needs nobody,
		// so this is reported and handed to the next tick rather than paused.
		return reconcileResult{}, stacktrace.NewError(
			"git push was rejected in '%v' and %v (%v) now holds commits local HEAD (%v) does not; "+
				"leaving it for the next tick to fast-forward or rebase. git said: %s",
			repoDirpath, remoteRef, abbrevSHA(remoteHead), abbrevSHA(localHead), trimGitOutputTail(result.Output),
		)
	}

	retryResult, retryErr := gc.Push(repoDirpath)
	if retryErr == nil {
		return reconcileResult{}, nil
	}
	if !retryResult.Rejected {
		return classifyPushFailure(retryResult, retryErr, repoDirpath)
	}
	// Refused twice while the remote is still an ancestor: the remote is
	// refusing a push that cannot lose anything. Still transient on its own —
	// the sweep brings it back within the minute — but reported upward so the
	// caller can count it, because a refusal that never stops is a different
	// condition from one that does.
	return reconcileResult{RefusedPushOutput: retryResult.Output},
		stacktrace.Propagate(retryErr, "git push was rejected twice in '%v' even though %v is an ancestor of local HEAD; git said: %s", repoDirpath, remoteRef, trimGitOutputTail(retryResult.Output))
}

// classifyPushFailure decides what a push failure that was *not* a refusal of
// the ref update means. Refusals never reach here: pushWithRejectionRetry
// re-checks those against a fresh fetch, because ancestry rather than wording
// is what says whether they can be retried.
func classifyPushFailure(result pushResult, pushErr error, repoDirpath string) (reconcileResult, error) {
	if result.AuthFailure {
		return reconcileResult{
			PauseReason: pauseReasonAuthFailure,
			PauseDetail: fmt.Sprintf(
				"git push failed authentication. Refresh your credentials (e.g. `gh auth login` or update your SSH key); "+
					"the pause clears once a successful push lands.\n\n"+
					"## What git said\n\n```\n%s\n```\n\n%s",
				trimGitOutputTail(result.Output), pausedStillCommittingNote,
			),
		}, nil
	}
	// Generic push failure — propagate as a transient error so a later tick
	// retries without pausing.
	return reconcileResult{}, stacktrace.Propagate(pushErr, "git push failed in '%v'", repoDirpath)
}

// recordPushRefusal counts one refusal against a repo and returns the running
// total. Counts are per repo: a refusal on one writeable copy says nothing
// about any other, since each has its own remote and its own rules.
func (s *Server) recordPushRefusal(repoName string) int {
	s.consecutivePushRefusalsMu.Lock()
	defer s.consecutivePushRefusalsMu.Unlock()
	if s.consecutivePushRefusals == nil {
		s.consecutivePushRefusals = map[string]int{}
	}
	s.consecutivePushRefusals[repoName]++
	return s.consecutivePushRefusals[repoName]
}

// forgetPushRefusals clears a repo's refusal count. Called whenever the remote
// accepts what the tick had for it — the count means "in a row", so anything
// landing ends the run.
func (s *Server) forgetPushRefusals(repoName string) {
	s.consecutivePushRefusalsMu.Lock()
	defer s.consecutivePushRefusalsMu.Unlock()
	delete(s.consecutivePushRefusals, repoName)
}

// buildPushRefusedDetail describes a refusal that has stopped looking
// transient. It states the count plainly, because "the remote refused this
// five times running" is the fact that rules out the race the retry exists for
// and points at the remote's own configuration instead.
func buildPushRefusedDetail(repoDirpath, defaultBranch string, refusals int, pushOutput string) string {
	var sb strings.Builder
	sb.WriteString("# The remote keeps refusing this push\n\n")
	sb.WriteString(fmt.Sprintf("**Path:** %s\n\n", repoDirpath))
	sb.WriteString(fmt.Sprintf(
		"The remote has refused the same push %d times in a row, and each time a fetch first confirmed that `origin/%s` "+
			"was an ancestor of local HEAD — so the push would not have discarded anything, and nothing about the local "+
			"branch is the problem. That rules out the transient collision a retry clears. Something on the remote is "+
			"refusing this ref update: a repository rule, branch protection, or push protection such as secret scanning "+
			"are the usual causes.\n\n",
		refusals, defaultBranch,
	))
	sb.WriteString("## What git said\n\n```\n")
	sb.WriteString(trimGitOutputTail(pushOutput))
	sb.WriteString("\n```\n\n")
	sb.WriteString("Act on whatever git names above — it is the remote's own account of why. ")
	sb.WriteString("Once the remote accepts a push again, the pause clears on its next tick.\n\n")
	sb.WriteString(pausedStillCommittingNote)
	return sb.String()
}

// pausedStillCommittingNote closes every pause notification.
//
// A pause used to be read as "the daemon has stopped", which was half right
// and the dangerous half: it stops exchanging with the remote, and it keeps
// committing local work. Saying so is what makes a pause safe to leave alone
// for a while — and the commit is also what clears the pause, since it is what
// moves HEAD and cleans the tree.
const pausedStillCommittingNote = "While paused, the sync loop **keeps committing** local changes — only the exchange with the remote stops, so work on disk is still being captured in git. The pause clears on its own once the working tree is clean and HEAD has moved past where it was when the pause was taken, and every writeable copy is re-checked once a minute, so no file has to be touched to wake it.\n"

// trimGitOutputTail keeps the last few lines of a git invocation's output, which
// is where git puts the reason, and drops the progress chatter above them.
func trimGitOutputTail(out string) string {
	trimmed := strings.TrimRight(out, "\n")
	if trimmed == "" {
		return "(git produced no output)"
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) > maxGitOutputLinesInPauseDetail {
		lines = lines[len(lines)-maxGitOutputLinesInPauseDetail:]
	}
	return strings.Join(lines, "\n")
}

func buildRebaseConflictDetail(repoDirpath string, conflicted []string, localHead, remoteHead string) string {
	var sb strings.Builder
	sb.WriteString("# Rebase conflict in writeable copy\n\n")
	sb.WriteString(fmt.Sprintf("**Path:** %s\n\n", repoDirpath))
	sb.WriteString(fmt.Sprintf("**Local HEAD:** `%s`\n", abbrevSHA(localHead)))
	sb.WriteString(fmt.Sprintf("**Remote HEAD (origin):** `%s`\n\n", abbrevSHA(remoteHead)))
	if len(conflicted) > 0 {
		sb.WriteString("## Conflicted files\n\n")
		for _, p := range conflicted {
			sb.WriteString(fmt.Sprintf("- `%s`\n", p))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("## How to resolve\n\n")
	sb.WriteString("```sh\n")
	sb.WriteString(fmt.Sprintf("cd %s\n", repoDirpath))
	sb.WriteString("git fetch origin\n")
	sb.WriteString("git pull --rebase --autostash\n")
	sb.WriteString("# resolve the conflicts in the listed files\n")
	sb.WriteString("git rebase --continue\n")
	sb.WriteString("git push\n")
	sb.WriteString("```\n\n")
	sb.WriteString("Marking this notification as read is purely cosmetic — it does not control the loop.\n\n")
	sb.WriteString(pausedStillCommittingNote)
	return sb.String()
}

func abbrevSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// writeableCopyReconcileRequest is the unit of work for the writeable-copy
// reconcile worker.
type writeableCopyReconcileRequest struct {
	repoName string
}

const writeableCopyReconcileChannelSize = 16

// runWriteableCopyTick executes one full tick for a writeable copy: pause
// resume probe, sanity, commit-if-dirty, fetch+reconcile, atomic pause+notify
// on failure. This is the entry point used by the reconcile worker goroutine
// and by the boot sweep.
func (s *Server) runWriteableCopyTick(ctx context.Context, repoName string) error {
	gc := s.gitCommander
	if gc == nil {
		gc = newRealGit()
	}
	cfg := s.getConfig()
	repoDirpath, ok := cfg.GetWriteableCopy(repoName)
	if !ok {
		return nil // no longer configured; quietly exit
	}

	pause, err := s.db.GetPause(repoName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read pause for '%v'", repoName)
	}

	expectedOriginURL := s.expectedOriginURLForRepo(repoName)
	sanity := runWriteableCopySanityCheck(gc, repoDirpath, expectedOriginURL)
	if sanity.SkipSilently {
		return nil
	}
	if sanity.PauseReason != "" {
		return s.postWriteableCopyPause(gc, repoName, repoDirpath, sanity.PauseReason, sanity.PauseDetail)
	}

	// Committing runs even while paused, and before the resume probe. A pause
	// means "stop exchanging with the remote", never "stop capturing local
	// work" — a commit is safe whatever state the remote is in. Ordering it
	// first is also what breaks the deadlock KEV-230 names: the resume probe
	// wants a clean tree and a moved HEAD, and on a checkout that missions are
	// writing to, the only thing that produces either is this commit. With the
	// probe first, a paused repo in use could never satisfy its own resume
	// condition, because the work that would satisfy it was gated behind the
	// pause.
	if _, err := commitIfDirty(gc, repoDirpath); err != nil {
		s.logger.Printf("Writeable-copy auto-commit failed for '%s': %v", repoName, err)
		return nil // transient; retry next trigger
	}

	if pause != nil {
		resumed, err := s.tryResumeFromPause(gc, pause, repoDirpath)
		if err != nil {
			return stacktrace.Propagate(err, "resume probe failed for '%v'", repoName)
		}
		if !resumed {
			return nil // still paused; local work is committed either way
		}
	}

	defaultBranch, err := gc.DefaultBranch(repoDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read default branch for '%v'", repoName)
	}
	result, err := reconcileWithRemote(gc, repoDirpath, defaultBranch)
	if result.RefusedPushOutput != "" {
		refusals := s.recordPushRefusal(repoName)
		s.logger.Printf("Writeable-copy '%s': the remote refused a lossless push (%d in a row)", repoName, refusals)
		if refusals >= maxConsecutivePushRefusals {
			// Standing refusal, not a flapping one. Forget the count so that a
			// repo resuming after this is fixed starts over.
			s.forgetPushRefusals(repoName)
			return s.postWriteableCopyPause(gc, repoName, repoDirpath, pauseReasonPushRefused,
				buildPushRefusedDetail(repoDirpath, defaultBranch, refusals, result.RefusedPushOutput))
		}
	}
	if err != nil {
		s.logger.Printf("Writeable-copy reconcile transient error for '%s': %v", repoName, err)
		return nil // transient; retry
	}
	if result.PauseReason != "" {
		return s.postWriteableCopyPause(gc, repoName, repoDirpath, result.PauseReason, result.PauseDetail)
	}
	// The remote accepted whatever this tick had for it, so any earlier refusals
	// were transient after all.
	s.forgetPushRefusals(repoName)
	if result.Action != "" && result.Action != "noop" {
		s.logger.Printf("Writeable-copy '%s': %s (%s)", repoName, result.Action, result.Outcome)
	}
	return nil
}

func (s *Server) tryResumeFromPause(gc GitCommander, pause *database.WriteableCopyPause, repoDirpath string) (bool, error) {
	clean, _, err := gc.Status(repoDirpath)
	if err != nil {
		return false, err
	}
	if !clean {
		return false, nil
	}
	currentHead, err := gc.HEAD(repoDirpath)
	if err != nil {
		return false, err
	}
	if currentHead == pause.LocalHeadAtPause {
		return false, nil
	}
	if err := s.db.DeletePause(pause.RepoName); err != nil {
		return false, stacktrace.Propagate(err, "failed to clear pause for '%v'", pause.RepoName)
	}
	s.logger.Printf("Writeable-copy '%s': resumed (HEAD moved from %s to %s)", pause.RepoName, abbrevSHA(pause.LocalHeadAtPause), abbrevSHA(currentHead))
	return true, nil
}

// postWriteableCopyPause atomically inserts a pause row and a linked
// notification. If a pause already exists for the repo, this is a no-op
// (preserves append-only deduplication across server restarts).
func (s *Server) postWriteableCopyPause(gc GitCommander, repoName, repoDirpath, reason, detail string) error {
	localHead, err := gc.HEAD(repoDirpath)
	if err != nil {
		// HEAD might be unreadable (e.g. corrupt repo). Use a sentinel so the
		// pause row is still well-formed.
		localHead = "unknown"
	}
	notif := &database.Notification{
		ID:           uuid.New().String(),
		Kind:         notificationKindForPauseReason(reason),
		SourceRepo:   repoName,
		Title:        titleForPauseReason(reason, repoName),
		BodyMarkdown: detail,
	}
	pause := &database.WriteableCopyPause{
		RepoName:         repoName,
		PausedReason:     reason,
		LocalHeadAtPause: localHead,
		NotificationID:   notif.ID,
	}
	inserted, err := s.db.UpsertPauseAndNotification(pause, notif)
	if err != nil {
		return stacktrace.Propagate(err, "failed to upsert pause for '%v'", repoName)
	}
	if inserted {
		s.logger.Printf("Writeable-copy '%s': PAUSED (%s) — notification %s posted", repoName, reason, abbrevSHA(notif.ID))
	}
	return nil
}

func titleForPauseReason(reason, repoName string) string {
	switch reason {
	case pauseReasonRebaseConflict:
		return "Rebase conflict on " + repoName
	case pauseReasonNonFFReject:
		// No longer posted; kept so pause rows written before the retry
		// landed still read as something.
		return "Push rejected (non-FF) on " + repoName
	case pauseReasonPushRefused:
		return "Remote keeps refusing the push on " + repoName
	case pauseReasonAuthFailure:
		return "Push auth failure on " + repoName
	case pauseReasonWrongBranch:
		return "Wrong branch in writeable copy of " + repoName
	case pauseReasonOriginDrift:
		return "Origin URL changed on " + repoName
	case pauseReasonPathMissing:
		return "Writeable copy missing for " + repoName
	case pauseReasonGitCorrupt:
		return "Git error in writeable copy of " + repoName
	default:
		return "Writeable-copy issue on " + repoName
	}
}

// expectedOriginURLForRepo computes the canonical origin URL for a repo from
// the library copy (which is known-good because the library worker maintains
// it). Returns empty when the library clone is missing or origin is unset.
func (s *Server) expectedOriginURLForRepo(repoName string) string {
	libraryDir := config.GetRepoDirpath(s.agencDirpath, repoName)
	gc := s.gitCommander
	if gc == nil {
		gc = newRealGit()
	}
	url, err := gc.OriginURL(libraryDir)
	if err != nil {
		return ""
	}
	return url
}

// runWriteableCopyReconcileWorker drains writeableCopyReconcileCh and runs a
// tick per request. Single-goroutine: ticks for all writeable copies are
// serialized through this worker.
func (s *Server) runWriteableCopyReconcileWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-s.writeableCopyReconcileCh:
			if err := s.runWriteableCopyTick(ctx, req.repoName); err != nil {
				s.logger.Printf("Writeable-copy reconcile failed for '%s': %v", req.repoName, err)
			}
		}
	}
}

// writeableCopySweepInterval is how often every writeable copy is reconciled
// regardless of whether anything happened to it. A tick on a clean, in-sync
// repo is a status, a fetch and a merge-base, resolving to a noop, which is
// affordable once a minute.
const writeableCopySweepInterval = 1 * time.Minute

// runWriteableCopySweepLoop reconciles every configured writeable copy on a
// timer, as the backstop under all the event-driven triggers.
//
// Every other trigger is conditional, and between them they leave states that
// nothing re-enters:
//
//   - the working-tree watcher fires on filesystem events but skips `.git/`, so
//     a commit — the one act that both cleans the tree and moves HEAD, which is
//     exactly the resume condition — produces no tick at all;
//   - the library fan-out fires only when the library clone's HEAD changes;
//   - the boot sweep fires once, at startup.
//
// So a repo left with local commits unpushed after a transient failure, or a
// pause whose cause has since cleared, could sit indefinitely while every
// surface check looked healthy. Clearing one by hand meant rewriting a tracked
// file with identical bytes purely to manufacture a watcher event. An
// unconditional pass is the thing that makes every "come back next tick" in
// this package true.
func (s *Server) runWriteableCopySweepLoop(ctx context.Context) {
	ticker := time.NewTicker(writeableCopySweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runWriteableCopySweepCycle()
		}
	}
}

// runWriteableCopySweepCycle enqueues one reconcile per configured writeable
// copy.
//
// It enqueues rather than ticking directly: the reconcile worker is the only
// thing serializing ticks across all writeable copies, and a second path into
// the tick would make "two reconciles in flight" possible — one of the
// candidate causes of the rejections this whole change is about.
func (s *Server) runWriteableCopySweepCycle() {
	for repoName := range s.getConfig().GetAllWriteableCopies() {
		s.enqueueWriteableCopyReconcile(repoName)
	}
}

// enqueueWriteableCopyReconcile enqueues a reconcile request for a repo if it
// has a writeable copy configured. Used by the library update worker to
// fan out remote changes that the library just pulled.
func (s *Server) enqueueWriteableCopyReconcile(repoName string) {
	cfg := s.getConfig()
	if _, ok := cfg.GetWriteableCopy(repoName); !ok {
		return
	}
	select {
	case s.writeableCopyReconcileCh <- writeableCopyReconcileRequest{repoName: repoName}:
	default:
		s.logger.Printf("Writeable-copy fan-out: channel full, skipping '%s'", repoName)
	}
}
