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
	pauseReasonNonFFReject    = "non_ff_reject"
	pauseReasonPushRejected   = "push_rejected"
	pauseReasonAuthFailure    = "auth_failure"
	pauseReasonWrongBranch    = "wrong_branch"
	pauseReasonOriginDrift    = "origin_drift"
	pauseReasonPathMissing    = "path_missing"
	pauseReasonGitCorrupt     = "git_corrupt"
)

// maxGitOutputLinesInPauseDetail bounds how much of git's output a pause
// carries. The last lines are the ones that say what happened; the leading ones
// are progress chatter.
const maxGitOutputLinesInPauseDetail = 20

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
		failure, err := pushHandlingRejections(gc, repoDirpath, defaultBranch)
		if err != nil {
			return reconcileResult{}, err
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
		failure, err := pushHandlingRejections(gc, repoDirpath, defaultBranch)
		if err != nil {
			return reconcileResult{}, err
		}
		if failure.PauseReason != "" {
			failure.Outcome = "diverged"
			return failure, nil
		}
		return reconcileResult{Outcome: "diverged", Action: "rebase-and-push"}, nil
	}
}

// pushHandlingRejections pushes, and decides what a failure means.
//
// Its three answers: a zero result with a nil error means the push landed; a
// result carrying a PauseReason means the failure needs a human; a non-nil
// error means the failure is transient and the next tick should try again.
//
// The retry is why this exists. A `git push` can be rejected as
// non-fast-forward purely because the daemon is racing its own previous push —
// this happened three times in one day on an actively-edited checkout, and each
// time the identical commits pushed fine minutes later. So a rejection is
// re-examined against a fresh fetch rather than believed: if the remote tip is
// an ancestor of local HEAD, nothing on the remote would be lost by pushing
// again, and the push is retried once. A pause is posted only when the remote
// genuinely holds commits the local branch does not, which is the case where
// pushing past the rejection would destroy someone's work.
func pushHandlingRejections(gc GitCommander, repoDirpath, defaultBranch string) (reconcileResult, error) {
	result, pushErr := gc.Push(repoDirpath)
	if pushErr == nil {
		return reconcileResult{}, nil
	}
	if !result.NonFFRejected {
		return classifyPushFailure(result, pushErr, repoDirpath)
	}

	if err := gc.Fetch(repoDirpath); err != nil {
		// Without a fresh view of the remote there is no way to tell a race
		// from a rewrite, and guessing in either direction is worse than
		// coming back next tick.
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
		// The remote holds commits this checkout does not. That is the
		// rejection the pause exists for.
		return reconcileResult{
			PauseReason: pauseReasonNonFFReject,
			PauseDetail: buildNonFFRejectDetail(repoDirpath, defaultBranch, localHead, remoteHead, result.Output),
		}, nil
	}

	retryResult, retryErr := gc.Push(repoDirpath)
	if retryErr == nil {
		return reconcileResult{}, nil
	}
	if !retryResult.NonFFRejected {
		return classifyPushFailure(retryResult, retryErr, repoDirpath)
	}
	// Rejected again while the remote is still an ancestor: something is
	// flapping rather than diverging. Staying transient keeps the loop alive
	// instead of parking it until someone notices.
	return reconcileResult{}, stacktrace.Propagate(retryErr, "git push was rejected twice in '%v' even though %v is an ancestor of local HEAD; git said: %s", repoDirpath, remoteRef, trimGitOutputTail(retryResult.Output))
}

// classifyPushFailure decides what a push failure that is *not* a
// fast-forward rejection means. Fast-forward rejections never reach here:
// pushHandlingRejections re-checks those against a fresh fetch, because they
// are the only kind a retry can clear.
func classifyPushFailure(result pushResult, pushErr error, repoDirpath string) (reconcileResult, error) {
	switch {
	case result.OtherRejected:
		return reconcileResult{
			PauseReason: pauseReasonPushRejected,
			PauseDetail: fmt.Sprintf(
				"The remote refused the push, and not because of fast-forwarding — a repository rule, a protected branch, "+
					"or a secret-scanning block will all look like this, and no retry will clear any of them.\n\n"+
					"**Path:** %s\n\n"+
					"## What git said\n\n```\n%s\n```\n\n"+
					"Act on whatever git names above. The sync loop resumes once HEAD has moved and the working tree is clean.",
				repoDirpath, trimGitOutputTail(result.Output),
			),
		}, nil
	case result.AuthFailure:
		return reconcileResult{
			PauseReason: pauseReasonAuthFailure,
			PauseDetail: fmt.Sprintf(
				"git push failed authentication. Refresh your credentials (e.g. `gh auth login` or update your SSH key) and retry. "+
					"The pause clears once a successful push lands.\n\n"+
					"## What git said\n\n```\n%s\n```",
				trimGitOutputTail(result.Output),
			),
		}, nil
	default:
		// Generic push failure — propagate as a transient error so the caller
		// can retry on the next trigger without pausing.
		return reconcileResult{}, stacktrace.Propagate(pushErr, "git push failed in '%v'", repoDirpath)
	}
}

// buildNonFFRejectDetail reports what was measured rather than what it might
// mean. The previous wording asserted the remote "was likely rewritten under
// us (force-pushed)", which was wrong in all three KEV-230 incidents and sent
// the investigation after a force-push that had never happened.
func buildNonFFRejectDetail(repoDirpath, defaultBranch, localHead, remoteHead, pushOutput string) string {
	var sb strings.Builder
	sb.WriteString("# Push rejected in writeable copy\n\n")
	sb.WriteString(fmt.Sprintf("**Path:** %s\n\n", repoDirpath))
	sb.WriteString("The push was rejected, and the fetch that followed showed the remote holding commits this checkout does not have. ")
	sb.WriteString("Pushing past that would discard them, so the sync loop stopped.\n\n")
	sb.WriteString(fmt.Sprintf("**Local HEAD:** `%s`\n", abbrevSHA(localHead)))
	sb.WriteString(fmt.Sprintf("**origin/%s after fetching:** `%s`\n\n", defaultBranch, abbrevSHA(remoteHead)))
	sb.WriteString("## What git said\n\n```\n")
	sb.WriteString(trimGitOutputTail(pushOutput))
	sb.WriteString("\n```\n\n")
	sb.WriteString("## How to investigate\n\n")
	sb.WriteString("```sh\n")
	sb.WriteString(fmt.Sprintf("cd %s\n", repoDirpath))
	sb.WriteString("git fetch origin\n")
	sb.WriteString(fmt.Sprintf("git log HEAD..origin/%s --oneline   # what the remote has that you don't\n", defaultBranch))
	sb.WriteString("```\n\n")
	sb.WriteString("The sync loop is paused for this repo. It resumes on its own once `git status` is clean and HEAD has moved past where it was when the pause was taken — including when nothing touches a file, because paused repos are re-probed on a timer.\n")
	return sb.String()
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
	sb.WriteString("The sync loop is paused for this repo. It will auto-resume on the next tick once HEAD has moved and `git status` is clean. Marking this notification as read is purely cosmetic — it does not control the loop.\n")
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
	if pause != nil {
		resumed, err := s.tryResumeFromPause(gc, pause, repoDirpath)
		if err != nil {
			return stacktrace.Propagate(err, "resume probe failed for '%v'", repoName)
		}
		if !resumed {
			return nil // still paused
		}
	}

	expectedOriginURL := s.expectedOriginURLForRepo(repoName)
	sanity := runWriteableCopySanityCheck(gc, repoDirpath, expectedOriginURL)
	if sanity.SkipSilently {
		return nil
	}
	if sanity.PauseReason != "" {
		return s.postWriteableCopyPause(gc, repoName, repoDirpath, sanity.PauseReason, sanity.PauseDetail)
	}

	if _, err := commitIfDirty(gc, repoDirpath); err != nil {
		s.logger.Printf("Writeable-copy auto-commit failed for '%s': %v", repoName, err)
		return nil // transient; retry next trigger
	}

	defaultBranch, err := gc.DefaultBranch(repoDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read default branch for '%v'", repoName)
	}
	result, err := reconcileWithRemote(gc, repoDirpath, defaultBranch)
	if err != nil {
		s.logger.Printf("Writeable-copy reconcile transient error for '%s': %v", repoName, err)
		return nil // transient; retry
	}
	if result.PauseReason != "" {
		return s.postWriteableCopyPause(gc, repoName, repoDirpath, result.PauseReason, result.PauseDetail)
	}
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
		return "Push rejected (non-FF) on " + repoName
	case pauseReasonPushRejected:
		return "Push refused by the remote on " + repoName
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

// writeableCopyPauseResumeInterval is how often paused writeable copies are
// re-probed. Each pass is one small database query plus, for paused repos only,
// a tick that stops at the resume check — cheap enough that a short interval
// costs nothing and keeps a recovered repo's backlog from aging.
const writeableCopyPauseResumeInterval = 1 * time.Minute

// runWriteableCopyPauseResumeLoop re-probes paused writeable copies on a timer.
//
// Without it a pause can only clear on a filesystem event in the working tree,
// and the one act that satisfies the resume condition — a commit, which makes
// the tree clean and moves HEAD — touches only `.git/`, which the watcher
// ignores. So the pause outlived its own cause: the repo was pushable, the tree
// was clean, and nothing was left to wake the probe. Clearing it by hand meant
// rewriting a tracked file with identical bytes purely to produce an event.
func (s *Server) runWriteableCopyPauseResumeLoop(ctx context.Context) {
	ticker := time.NewTicker(writeableCopyPauseResumeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runWriteableCopyPauseResumeCycle()
		}
	}
}

// runWriteableCopyPauseResumeCycle enqueues one reconcile per paused repo.
//
// It enqueues rather than ticking directly: the reconcile worker is the only
// thing serializing ticks across all writeable copies, and a second path into
// the tick would make "two reconciles in flight" possible — one of the
// candidate causes of the rejections this whole change is about.
func (s *Server) runWriteableCopyPauseResumeCycle() {
	pauses, err := s.db.ListPauses()
	if err != nil {
		s.logger.Printf("Writeable-copy pause resume: failed to list pauses: %v", err)
		return
	}
	for _, pause := range pauses {
		s.enqueueReconcileForRepo(pause.RepoName)
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
