Codebase Cleanup — 2026-09-06 Disposition
=========================================

This records what happened to the last five open beads of `agenc-351`, the epic generated from `codebase-cleanup.md`. Read it before acting on anything in that report: the report describes commit `9f69e0d` as of 2026-02-13, and seven months of changes have moved most of what it measured.

**Recommendation: close all five.** None was implemented. The recommendation is Kevin's to accept or reject; nothing here has been closed unilaterally.

Full evidence lives in each bead's notes (`bd show <id>`) — file paths, line numbers, commit hashes, measured timings. This page carries only the verdict and the one fact that drives it, so a reader can decide what to re-open without re-running the audit.

| Bead | Verdict | The fact that decides it |
|---|---|---|
| `agenc-337` — ctx on database methods | Close | Its only stated justification is cancelling long-running queries. There are none: the heaviest query the code can issue takes 200 ms on the real 60 MB database, and lock contention is capped at 5 s by `busy_timeout` and returns `SQLITE_BUSY` rather than hanging. |
| `agenc-350` — `MissionStore` interface | Close | Zero consumers. `*database.DB` appears in one production location; tests use real SQLite in `t.TempDir()` and pass. The repo's two existing test-double interfaces are narrow and declared at the consumer — the opposite of the broad producer-side interface this bead specifies. |
| `agenc-342` — split `wrapper.go` | Close | Two of its four extraction targets were already deleted by other work. The state machine went in commit `bff7f87`; the commented-out Keychain code went in `7016008`, one day *after* the epic was written. |
| `agenc-343` — split config files | Close | The plan's buckets no longer map to the file: 129 lines are claimed by two buckets at once, `cron_config.go` would hold 21 lines, and the residual `agenc_config.go` misses the ticket's own 400-line target. |
| `agenc-344` — reorganize `cmd/` | Close | Every number is stale (76 files then, 125 now), `cmd/daemon/` maps to zero files, and the naive split is an import cycle rather than a file move. |

Three findings from the audit that outlived the beads that produced them
------------------------------------------------------------------------

**A likely data race, filed as `agenc-way9`.** `internal/wrapper/wrapper.go:214` writes `hasConversation` without holding `stateMu`, after the HTTP server goroutine that reads it under `RLock` has already started. This matters more than any of the five closed beads: `hasConversation` feeds the status AgenC uses to decide whether a mission is idle, and a bad read there misreports mission state silently. `setupRun` has 0.0 % coverage, so `make check`'s race detector never reaches it. Indicated by inspection, not yet confirmed under `-race`.

**Reorganizing `cmd/` would weaken the ANSI guard, not merely disturb it.** Several `ForPicker` functions already cross the proposed package boundaries, so the split would force them to be exported — and unexported-ness is precisely what bounds their blast radius today. Recorded on `agenc-344` in case that ticket is ever revived.

**The `deadcode` gate is informational only.** `Makefile` prints `⚠ Dead code found (informational — will become a hard error after cleanup)` and then reports `✓ Deadcode OK`; it never fails the build. There are currently 24 unreachable functions. Worth a look: `ValidatePathNoTraversal` (`internal/config/agenc_config.go:1079`) is unreachable, which is either a leftover to delete or a defense that was meant to be wired in and never was. This belongs to the epic's P0-6 tooling item, and "temporary" has now lasted some months.

The gate this touches
---------------------

`agenc-9jgp` binds `agenc-337`, `agenc-350` and `agenc-342` as a Phase 0 gate that must close before autonomy ships. Closing all three is therefore a decision about the gate, not just about the beads, and it is flagged on `agenc-9jgp` rather than resolved here.

One correction while you weigh it. That gate's 2026-07-05 note applies the cut "does this failure class corrupt unattended work silently?" and answers **yes** for the config cache (`agenc-347`, since closed), never-restarted background loops, and undetectable git hangs (`agenc-336`, since closed); it answers **no** for `wrapper.go` decomposition. It does not classify `337` or `350` either way. Any summary calling those two "the silent-failure cut" is inferring from the BINDS list rather than quoting the note.

Measured against that cut on its own terms, `337` scores no: the contention failure is bounded, returns an error, gets logged, and retries on the next tick. If the recommendations are accepted, the note's own suggestion applies — re-scope the gate to the silent-failure subset so it closes sooner — leaving the loop health-recovery policy and the raised test floor as what still binds.

---

Audit performed 2026-09-06 in AgenC mission `7e071462-4819-4684-b12d-0183477cdb61`, prompted by Kevin's challenge to the epic: "do we even need to do these? they are from long, long ago, and I bet agenc has shifted quite a bit since then."
