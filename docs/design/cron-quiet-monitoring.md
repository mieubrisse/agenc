Cron quiet monitoring — design record
=====================================

Why this exists
---------------

Between 2026-09-01 and 2026-09-06, six of Kevin's seven cron jobs stopped running
and nothing said so. launchd fired each one on schedule, but the spawn aborted
before `agenc` ever executed: macOS could not resolve the job's Lightweight Code
Requirement (`Unable to update LWCR with smd: 3`), producing `EX_CONFIG` 78 or an
`OS_REASON_CODESIGNING` kill. That happens *before* the process has a stdout, so
there was nothing to log, no mission row, no notification. The only place the
failure was visible was `launchctl print`, which nothing watches.

The diagnosis of that outage is recorded separately in
[`docs/incidents/2026-09-06-silent-cron-outage.md`](../incidents/2026-09-06-silent-cron-outage.md)
on branch `mieubrisse/agenc-cron-doctor`.

The design consequence is the whole point:

> **You cannot detect this by catching a failure, only by noticing an absence.**

The server already knows every cron's schedule, already owns the missions table,
and already owns the notification system. Nothing was comparing the first against
the second.

What this ships
---------------

A background loop in the AgenC server that notices when a cron job has stopped
producing missions, and posts one informational notification when it does.

That is the whole check: **a cron that was due should have produced a mission.**
Kevin confirmed this framing directly — "'did a mission appear' is in fact the
right thing to test" — and the tone he asked for is informational, not an alarm:
"the server itself can just verify that the launchd's are firing occasionally,
and if they're not fire a notification saying 'hey btw...'"

Design decisions
----------------

These are the questions the brief left open. Each is answered here rather than
picked silently.

### Staleness, not per-fire accounting

The loop does not reconcile every scheduled fire against every mission. It asks a
coarser question: *has this cron produced anything lately?*

That is deliberate. Per-slot accounting forces the monitor to reason about
whether the server was awake for one specific 07:00 window, which is exactly the
reasoning that goes wrong when a laptop sleeps. Staleness sidesteps it: the
answer is derived from persistent state (the config, the missions table, and the
monitor's own state table), so a server that was down simply catches up on its
next tick. There is no per-slot bookkeeping to lose.

### How long is "quiet": two of the cron's own cycles

A cron is quiet when it has produced nothing for longer than **twice its own
scheduled cadence** — that is, it has skipped two consecutive cycles.

This scales the way Kevin asked for: a daily job going two days quiet is a
signal; an hourly job going two days quiet is much louder, and gets reported
after two hours rather than two days. It is also one sentence long, which matters
for something whose output a human reads.

Why two cycles and not one: a single skipped fire has innocent explanations. In
particular, macOS defers `StartCalendarInterval` jobs across *sleep* and replays
one on wake, but does **not** replay fires missed across a full *shutdown* — so a
machine that is off overnight genuinely misses a fire through no fault of the
cron system. Alarming on that would produce a false note after every such reboot,
and a channel that cries wolf stops being read. Two consecutive misses is a
pattern, not an accident.

**The tradeoff, recorded rather than hidden:** this makes weekly crons slow to
report — roughly 14 days before a note. In the September outage the three weekly
crons had been dead 9–10 days when Kevin found them by hand, so the monitor would
have told him at day 14: later than ideal, versus never. The alternative
considered was capping the horizon (e.g. "never wait more than 8 days regardless
of cadence"), which reports weekly crons faster at the cost of a second rule to
explain. One explainable rule won. If Kevin wants the cap, it is a small change
to `quietThreshold`.

### The reference point: last mission, or first sight

Quiet time is measured from `max(last mission this cron created, the first time
the server saw this cron enabled)`.

The second half of that is what stops a brand-new cron from being reported the
moment it is created. A cron added at 15:00 with a `0 2 * * *` schedule has its
most recent expected fire in the past and no run history; without a first-seen
reference it reads as long overdue. It also handles re-enabling: when a cron
disappears from the enabled set its state row is deleted, so re-enabling starts
the clock fresh.

The prior attempt at this feature left exactly this false positive unfixed and
flagged it for a reviewer. This is the fix.

### Sleep and restart

Covered by the two decisions above, but stated plainly because the brief asked:

- **The server being down does not blind the monitor.** Nothing is computed
  incrementally. Every cycle re-derives the answer from the config, the missions
  table, and the state table, all of which survive a restart.
- **Sleep produces no false note.** launchd replays one deferred fire on wake, so
  the mission appears and the cron is not quiet.
- **A full shutdown across a fire is a true positive, not a false one.** The fire
  genuinely never happens and the cron genuinely did not deliver. It takes two
  such misses to be reported, which a single overnight shutdown will not reach.
- A three-minute settle delay after server startup keeps the monitor from judging
  during the seconds between "machine woke up" and "launchd replayed the missed
  run."

### Alarm storms: one note, naming everyone

Six crons going quiet at once — which is what actually happened — produces **one**
notification listing all six, not six notifications. The loop collects every
newly-quiet cron in a cycle and posts a single note.

### Repeats: mentioned once per episode

A cron is mentioned once when it goes quiet. It is not mentioned again while it
stays quiet — a daily cron broken for a month produces one note, not thirty. The
state clears when the cron produces a mission again, so a cron that recovers and
later goes quiet a second time does get a fresh note.

### What the note says

Informational, in the register of a colleague mentioning something: which crons,
when each last ran, what its schedule is, and how to look closer. No urgency
language, no capitalised severity, no implication that Kevin must act now.

Two additions beyond the letter of the brief
--------------------------------------------

Both are small, both serve the single check rather than adding a second one, and
both are called out here so Kevin can strike either in review.

1. **launchd's last exit status is included in the note.** Read-only, best-effort,
   and computed *only* for a cron that has already been found quiet — a
   `launchctl print` failure never suppresses or delays the finding. It is here
   because it is the single most useful fact when a cron stops: discovering
   `last exit code = 78: EX_CONFIG` is what took an entire mission last time, and
   putting it in the note turns an investigation into a sentence.

2. **`agenc cron health` reports the current state on demand.** Read-only —
   it evaluates and prints, it does not notify, repair, or mutate. It exists so
   the monitor is inspectable: without it, the only signal the monitor produces is
   silence, and silence is ambiguous between "everything is fine" and "the monitor
   is broken."

What this deliberately does not do
----------------------------------

- **No repair.** The monitor observes and reports. The previous attempt at this
  feature autonomously repaired launchd registrations every fifteen minutes from
  an unreviewed binary, and Kevin's verdict on it was "that doctor stuff was super
  wrong; glad we got rid of it." The server already has the authority to reload a
  plist — the cron syncer does it — so adding repair later is a small change, but
  it is Kevin's call to make, not a side effect of shipping detection.

- **No new launchd job, no pinned binary.** This is server code, shipped through
  the normal release path. It reaches Kevin's machine when he installs a released
  `agenc` and not before.

- **No check on whether a run *delivered*.** An earlier version of this brief
  asked for a monitor that distinguished "a mission appeared" from "the mission
  did any work," on the evidence that nine of ten `hn-daily-pull` runs produced
  nothing. Kevin overruled that on 2026-09-07: several of his cron skills are
  designed to stay silent on uneventful days, so silence is correct behaviour
  rather than failure. A mission that spawns and then makes zero tool calls is
  arguably still a failure, and is a genuinely different signal from "no mission
  appeared" — recorded here as a possible later addition, deliberately not built.

- **No watch on the monitor itself.** A server-side monitor cannot detect its own
  death: if the AgenC server is down, nothing notices. That is the external
  dead-man's-switch question, filed as bead `agenc-8tss`, and it is Kevin's
  decision because it means signing up for a third-party service. Nothing here
  covers it.

The monitor's own failures
--------------------------

The previous attempt failed in exactly the way it existed to catch: it wrote its
own errors to `~/.agenc/logs/cron-doctor.log`, which nothing reads, and a
version-skew warning accumulated there unread on every run. This version does not
get a log of its own:

- A cron whose schedule AgenC cannot parse **is itself a finding**, reported in
  the same note. The cron syncer silently skips those today, which means a
  hand-edited `config.yml` can contain a cron that never fires and never will —
  the same silent hole in a different place.
- Transient errors (a database read that fails this cycle) go to the server log
  and are retried on the next tick, because that is what they are.
- `agenc cron health` surfaces the monitor's current judgment on demand, so its
  state is never only visible to itself.

Provenance
----------

- Beads `agenc-inrf` (cron missions that die produce no signal) and `exobrain-6g1`
  (hn-daily-pull: five consecutive runs died before posting).
- Prior art mined, not resurrected: branch `mieubrisse/agenc-cron-doctor`
  (commits 32cc218, a115b0a, e97bcdd). The schedule arithmetic and the launchd
  status parsing come from there; its delivery mechanism — a standalone launchd
  job running an unreviewed binary that repaired system state unattended — is
  deliberately not reproduced.
- Kevin's framing, via coordinator mission `5da13a09`, 2026-09-07.
