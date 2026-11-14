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

// newSanityPause builds a sanity-stage failure with its resolution note
// attached. Every sanity failure goes through here so that exactly one piece of
// text has to be right about what the loop does for this class: it stops before
// the auto-commit, so nothing is being captured until the condition is fixed.
func newSanityPause(reason, detail string) sanityResult {
	return sanityResult{
		PauseReason: reason,
		PauseDetail: detail + "\n\n" + pauseResolutionNote(reason),
	}
}

// runWriteableCopySanityCheck examines the writeable copy's directory and git
// state. It returns one of three outcomes:
//   - all-zero result → sanity passed; proceed with the tick
//   - SkipSilently=true → transient state; retry next trigger, no notification
//   - PauseReason non-empty → pause + notify
func runWriteableCopySanityCheck(gc GitCommander, repoDirpath, expectedOriginURL string) sanityResult {
	if _, err := os.Stat(repoDirpath); err != nil {
		return newSanityPause(pauseReasonPathMissing, fmt.Sprintf("the writeable-copy path '%s' does not exist (was it deleted manually?)", repoDirpath))
	}
	if gc.IndexLockExists(repoDirpath) {
		return sanityResult{SkipSilently: true}
	}
	if gc.IsRebaseInProgress(repoDirpath) || gc.IsMergeInProgress(repoDirpath) {
		return sanityResult{SkipSilently: true}
	}
	defaultBranch, err := gc.DefaultBranch(repoDirpath)
	if err != nil {
		return newSanityPause(pauseReasonGitCorrupt, fmt.Sprintf("could not read default branch from origin/HEAD in '%s': %v", repoDirpath, err))
	}
	currentBranch, err := gc.CurrentBranch(repoDirpath)
	if err != nil {
		return newSanityPause(pauseReasonGitCorrupt, fmt.Sprintf("could not determine current branch in '%s': %v", repoDirpath, err))
	}
	if currentBranch != defaultBranch {
		return newSanityPause(pauseReasonWrongBranch, fmt.Sprintf("the writeable copy is on branch '%s'; the sync loop only operates on the default branch '%s'. Switch back with: git -C %s checkout %s", currentBranch, defaultBranch, repoDirpath, defaultBranch))
	}
	// Tree may have conflicts from a manual merge — refuse silently in that
	// case so we don't blow away the user's in-progress resolution.
	_, conflicted, err := gc.Status(repoDirpath)
	if err != nil {
		return newSanityPause(pauseReasonGitCorrupt, fmt.Sprintf("git status failed in '%s': %v", repoDirpath, err))
	}
	if len(conflicted) > 0 {
		return sanityResult{SkipSilently: true}
	}
	if expectedOriginURL != "" {
		actual, err := gc.OriginURL(repoDirpath)
		if err != nil {
			return newSanityPause(pauseReasonGitCorrupt, fmt.Sprintf("could not read origin URL in '%s': %v", repoDirpath, err))
		}
		if !originURLMatches(actual, expectedOriginURL) {
			return newSanityPause(pauseReasonOriginDrift, fmt.Sprintf("the writeable copy's origin URL '%s' does not match the expected URL '%s'. Restore it with: git -C %s remote set-url origin %s", actual, expectedOriginURL, repoDirpath, expectedOriginURL))
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

// Actions a reconcile can take. The first four mean the writeable copy ended
// the tick in agreement with the remote; actionRebaseDeferred does not.
const (
	actionNoop           = "noop"
	actionPush           = "push"
	actionFastForward    = "ff"
	actionRebaseAndPush  = "rebase-and-push"
	actionRebaseDeferred = "rebase-deferred"
)

// reconcileResult describes the disposition of a fetch+reconcile cycle.
type reconcileResult struct {
	// Outcome is one of: "equal", "ahead", "behind", "diverged".
	Outcome string
	// Action describes what was done; see the action constants above.
	Action string
	// PauseReason is set when reconcile failed in a way that requires user
	// intervention: a rebase conflict, an auth failure, or a push the remote
	// has refused maxConsecutivePushRefusals times running.
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
//
// mayRebase is asked, with the freshly-fetched remote tip, whether a rebase
// should be attempted when the branches have diverged. It exists because the
// sweep ticks every minute and a rebase is the one operation here that is not
// cheap to repeat: it runs against a tree missions are writing to, and each
// attempt is another chance to land in the state where the rebase cannot be
// aborted and every later tick skips the repo in silence.
func reconcileWithRemote(gc GitCommander, repoDirpath, defaultBranch string, mayRebase func(remoteHead string) bool) (reconcileResult, error) {
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
		return reconcileResult{Outcome: "equal", Action: actionNoop}, nil
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
		return reconcileResult{Outcome: "ahead", Action: actionPush}, nil
	case mergeBase == localHead:
		// Remote is ahead of local → fast-forward.
		if err := gc.MergeFFOnly(repoDirpath, remoteRef); err != nil {
			return reconcileResult{}, stacktrace.Propagate(err, "fast-forward to '%v' failed in '%v'", remoteRef, repoDirpath)
		}
		return reconcileResult{Outcome: "behind", Action: actionFastForward}, nil
	default:
		// Diverged → rebase-and-push, if the caller's policy allows another
		// attempt right now.
		if !mayRebase(remoteHead) {
			return reconcileResult{Outcome: "diverged", Action: actionRebaseDeferred}, nil
		}
		conflicted, rebaseErr := gc.PullRebaseAutostash(repoDirpath)
		if rebaseErr != nil {
			// A failed abort is worth saying out loud: it leaves the rebase
			// in progress, which makes every later sanity check skip the repo
			// silently, so the sync would stop with nothing explaining why.
			abortErr := gc.RebaseAbort(repoDirpath)
			return reconcileResult{
				Outcome:     "diverged",
				PauseReason: pauseReasonRebaseConflict,
				PauseDetail: buildRebaseConflictDetail(repoDirpath, conflicted, localHead, remoteHead, abortErr),
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
		return reconcileResult{Outcome: "diverged", Action: actionRebaseAndPush}, nil
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
				"git push failed authentication. Refresh your credentials — `gh auth login`, or update the SSH key "+
					"this checkout's remote uses.\n\n"+
					"## What git said\n\n```\n%s\n```\n\n%s",
				trimGitOutputTail(result.Output), pauseResolutionNote(pauseReasonAuthFailure),
			),
		}, nil
	}
	// Generic push failure — propagate as a transient error so a later tick
	// retries without pausing.
	return reconcileResult{}, stacktrace.Propagate(pushErr, "git push failed in '%v'", repoDirpath)
}

// rebaseRetryInterval is the shortest gap between two rebase attempts on the
// same writeable copy.
//
// Every other operation in a tick is cheap to repeat once a minute. A rebase is
// not: it runs `pull --rebase --autostash` against a working tree that missions
// are actively writing to, and a conflict leaves an abort that can itself fail,
// after which the rebase stays in progress and every later tick skips the repo
// in silence. Fifteen minutes keeps that exposure to four attempts an hour
// instead of sixty.
const rebaseRetryInterval = 15 * time.Minute

// rebaseAttempt is what the loop remembers about its last rebase on a repo.
type rebaseAttempt struct {
	remoteHead string
	at         time.Time
}

// rebaseGateFor builds the policy reconcileWithRemote asks before rebasing.
//
// Two conditions, both required. The remote tip must have moved since the last
// attempt, because a rebase onto an unchanged tip is the same rebase and will
// conflict the same way. And rebaseRetryInterval must have passed, which bounds
// the damage when the remote is busy enough to keep producing new tips.
//
// Nothing here blocks the resolution path: a person who resolves the conflict
// leaves the branches no longer diverged, so the next tick takes the ordinary
// ahead-or-equal route, succeeds, and clears the recorded failure.
func (s *Server) rebaseGateFor(repoName string) func(remoteHead string) bool {
	return func(remoteHead string) bool {
		s.lastRebaseAttemptsMu.Lock()
		defer s.lastRebaseAttemptsMu.Unlock()

		last, attempted := s.lastRebaseAttempts[repoName]
		if attempted && !shouldAttemptRebase(last, remoteHead, time.Now()) {
			return false
		}
		if s.lastRebaseAttempts == nil {
			s.lastRebaseAttempts = map[string]rebaseAttempt{}
		}
		s.lastRebaseAttempts[repoName] = rebaseAttempt{remoteHead: remoteHead, at: time.Now()}
		return true
	}
}

// shouldAttemptRebase holds the policy as a pure function so the clock is an
// argument rather than a dependency.
func shouldAttemptRebase(last rebaseAttempt, remoteHead string, now time.Time) bool {
	if last.remoteHead == remoteHead {
		return false
	}
	return now.Sub(last.at) >= rebaseRetryInterval
}

// forgetRebaseAttempt clears a repo's rebase memory once it has reconciled
// fully, so a later divergence is judged fresh rather than against a situation
// that is over.
func (s *Server) forgetRebaseAttempt(repoName string) {
	s.lastRebaseAttemptsMu.Lock()
	defer s.lastRebaseAttemptsMu.Unlock()
	delete(s.lastRebaseAttempts, repoName)
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
	sb.WriteString("Act on whatever git names above — it is the remote's own account of why.\n\n")
	sb.WriteString(pauseResolutionNote(pauseReasonPushRefused))
	return sb.String()
}

// pauseResolutionNote closes every failure notification by saying what the
// loop is still doing and what will end the episode.
//
// It is per reason because the honest answer differs by reason, and the
// previous single note was wrong twice over: it promised that committing
// continues even for the failures found before the commit, and it explained
// resumption by a mechanism — "once the tree is clean and HEAD has moved" —
// that described the implementation rather than anything the reader can act
// on. Every branch below is checkable against the tick: what it does, how
// often, and what ends it.
func pauseResolutionNote(reason string) string {
	const oneNotificationPerEpisode = "This is the only notification you will get for this episode. " +
		"A new one arrives only if the failure turns into a different one; when it clears, it clears quietly.\n"

	switch reason {
	case pauseReasonAuthFailure, pauseReasonPushRefused, pauseReasonNonFFReject:
		return "The loop **keeps committing** local changes, so work on disk is still being captured in git — only the push is failing. " +
			"It retries roughly once a minute, so this resolves itself as soon as the remote accepts a push again: " +
			"fix the credentials or the remote-side rule and nothing further is needed here.\n\n" +
			oneNotificationPerEpisode

	case pauseReasonRebaseConflict:
		return "The loop **keeps committing** local changes, so nothing on disk is at risk — but it will not re-attempt the rebase " +
			"more than once every 15 minutes, and not at all until the remote moves, because retrying a conflicting rebase against " +
			"a working tree that is being edited is how a half-finished rebase gets left behind. **This one needs a person**: resolve it " +
			"as above, and once the branches are no longer diverged the next tick reconciles normally and clears this.\n\n" +
			oneNotificationPerEpisode

	default:
		// The sanity-stage failures: wrong branch, missing path, origin drift,
		// git corruption. The tick stops before the commit for these.
		return "**Nothing is being committed or pushed for this repo while this holds.** The loop stops before its auto-commit here, " +
			"deliberately — committing onto the wrong branch, or into a repo whose git state is broken, would make the situation worse — " +
			"so any uncommitted work is sitting on disk with nothing capturing it. Treat this as the urgent kind. " +
			"The loop re-checks about once a minute, so fixing the condition above clears this within a minute, with nothing else to do.\n\n" +
			oneNotificationPerEpisode
	}
}

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

func buildRebaseConflictDetail(repoDirpath string, conflicted []string, localHead, remoteHead string, abortErr error) string {
	var sb strings.Builder
	sb.WriteString("# Rebase conflict in writeable copy\n\n")
	if abortErr != nil {
		sb.WriteString(fmt.Sprintf(
			"**The rebase could not be aborted, so it is still in progress.** Until it is cleared by hand "+
				"(`git -C %s rebase --abort`), every later tick will skip this repo silently — nothing further will "+
				"be committed or pushed, and no new notification will say so.\n\n"+
				"## Why the abort failed\n\n```\n%s\n```\n\n",
			repoDirpath, trimGitOutputTail(abortErr.Error()),
		))
	}
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
	if abortErr == nil {
		sb.WriteString(pauseResolutionNote(pauseReasonRebaseConflict))
	}
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

// runWriteableCopyTick executes one full tick for a writeable copy: sanity,
// commit-if-dirty, fetch+reconcile, and then either recording a failure or
// clearing the one on record. This is the entry point used by the reconcile
// worker goroutine, the sweep and the boot pass.
//
// A recorded failure never short-circuits a tick. The loop keeps trying on
// every trigger; the row exists so that an unresolved failure is reported once
// rather than once a minute.
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

	// A pause row does not stop this tick. It records that the user has already
	// been told about a failure that is still unresolved; the loop goes on
	// trying, and the row is what keeps one episode to one notification.
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
		return s.recordWriteableCopyFailure(gc, repoName, repoDirpath, sanity.PauseReason, sanity.PauseDetail)
	}

	// Committing runs whether or not there is an unresolved failure. A pause
	// means "something about the remote is unresolved", never "stop capturing
	// local work" — a commit is safe whatever state the remote is in — and on
	// a checkout that missions are writing to, this commit is the only thing
	// that ever makes the tree clean. Gating it behind the pause is what made
	// the KEV-230 deadlock permanent.
	if _, err := commitIfDirty(gc, repoDirpath); err != nil {
		s.logger.Printf("Writeable-copy auto-commit failed for '%s': %v", repoName, err)
		return nil // transient; retry next trigger
	}

	defaultBranch, err := gc.DefaultBranch(repoDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read default branch for '%v'", repoName)
	}
	result, err := reconcileWithRemote(gc, repoDirpath, defaultBranch, s.rebaseGateFor(repoName))
	if result.RefusedPushOutput != "" {
		refusals := s.recordPushRefusal(repoName)
		s.logger.Printf("Writeable-copy '%s': the remote refused a lossless push (%d in a row)", repoName, refusals)
		if refusals >= maxConsecutivePushRefusals {
			// Standing refusal, not a flapping one. Forget the count so that a
			// repo that clears this starts over.
			s.forgetPushRefusals(repoName)
			return s.recordWriteableCopyFailure(gc, repoName, repoDirpath, pauseReasonPushRefused,
				buildPushRefusedDetail(repoDirpath, defaultBranch, refusals, result.RefusedPushOutput))
		}
	}
	if err != nil {
		s.logger.Printf("Writeable-copy reconcile transient error for '%s': %v", repoName, err)
		return nil // transient; retry
	}
	if result.PauseReason != "" {
		return s.recordWriteableCopyFailure(gc, repoName, repoDirpath, result.PauseReason, result.PauseDetail)
	}
	if !isFullyReconciled(result) {
		return nil // nothing failed, but nothing was settled either
	}

	// Everything this tick had for the remote is now on it. Anything recorded
	// against this repo described a situation that no longer exists.
	s.forgetPushRefusals(repoName)
	s.forgetRebaseAttempt(repoName)
	if pause != nil {
		if err := s.db.DeletePause(repoName); err != nil {
			return stacktrace.Propagate(err, "failed to clear the pause for '%v'", repoName)
		}
		s.logger.Printf("Writeable-copy '%s': resumed — a full reconcile succeeded, clearing the %s failure", repoName, pause.PausedReason)
	}
	if result.Action != "" && result.Action != actionNoop {
		s.logger.Printf("Writeable-copy '%s': %s (%s)", repoName, result.Action, result.Outcome)
	}
	return nil
}

// isFullyReconciled reports whether a tick got the writeable copy all the way
// into agreement with the remote — the only outcome that clears a recorded
// failure. Everything the tick had for the remote is on it: either a push was
// accepted, or there was nothing to push.
func isFullyReconciled(result reconcileResult) bool {
	switch result.Action {
	case actionNoop, actionPush, actionFastForward, actionRebaseAndPush:
		return true
	default:
		// Includes actionRebaseDeferred: a real divergence left unresolved.
		return false
	}
}

// recordWriteableCopyFailure records that a writeable copy has a failure the
// user should know about, and notifies — but only when the news is new.
//
// Three cases. No existing row: insert one and post a notification. An existing
// row for the same reason: write nothing at all. That is what keeps an episode
// to a single notification however long it lasts, and it is the whole reason
// the row outlives the tick, because the loop retries every minute and will
// meet the same failure sixty times an hour. An existing row for a *different*
// reason: repoint the row and post a fresh notification, so a situation that
// changes gets said once more rather than going quiet.
//
// local_head_at_pause is still written, as a record of where the repo stood
// when the episode opened. Nothing reads it to make a decision any more.
func (s *Server) recordWriteableCopyFailure(gc GitCommander, repoName, repoDirpath, reason, detail string) error {
	existing, err := s.db.GetPause(repoName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read the recorded failure for '%v'", repoName)
	}
	if existing != nil && existing.PausedReason == reason {
		return nil // already reported, and nothing about it has changed
	}

	localHead, err := gc.HEAD(repoDirpath)
	if err != nil {
		// HEAD might be unreadable (e.g. corrupt repo, missing path). Use a
		// sentinel so the row is still well-formed.
		localHead = "unknown"
	}
	notification := &database.Notification{
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
		NotificationID:   notification.ID,
	}

	if existing != nil {
		replaced, err := s.db.ReplacePauseAndNotification(pause, notification)
		if err != nil {
			return stacktrace.Propagate(err, "failed to re-record the failure for '%v'", repoName)
		}
		if replaced {
			s.logger.Printf("Writeable-copy '%s': failure changed from %s to %s — notification %s posted", repoName, existing.PausedReason, reason, abbrevSHA(notification.ID))
			return nil
		}
		// The row went away underneath us; fall through and insert.
	}

	inserted, err := s.db.UpsertPauseAndNotification(pause, notification)
	if err != nil {
		return stacktrace.Propagate(err, "failed to record the failure for '%v'", repoName)
	}
	if inserted {
		s.logger.Printf("Writeable-copy '%s': PAUSED (%s) — notification %s posted", repoName, reason, abbrevSHA(notification.ID))
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
//     nothing git does on its own produces a tick;
//   - the library fan-out fires only when the library clone's HEAD changes;
//   - the boot sweep fires once, at startup.
//
// So a repo left with local commits unpushed after a transient failure, or one
// whose recorded failure has since been fixed on the remote side, could sit
// indefinitely while every surface check looked healthy. Clearing one by hand
// meant rewriting a tracked file with identical bytes purely to manufacture a
// watcher event. An
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
