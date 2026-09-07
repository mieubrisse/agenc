# AgenC cron outage — diagnosis and remediation record

Mission `10a97e0b-7234-4a6c-b938-d1ce40c64fb8`, surveyed and repaired 2026-09-06.
Filed by coordinator mission `5da13a09`.

## TL;DR

Six of seven crons had been failing **at launchd spawn time**, every scheduled
slot, since 2026-09-01 — producing no log line, no database row, no notification.
They were not "unscheduled": launchd fired them on time and the spawn aborted
before `agenc` ever executed. All seven have been repaired and each was verified
to fire.

## What was actually broken (proven, not inferred)

### The mechanism

`launchctl print gui/501/agenc-cron.<uuid>` reported, for the two daily crons:

```
last exit code = 78: EX_CONFIG
```

and for the three weekly crons:

```
last exit reason = OS_REASON_CODESIGNING
```

The unified log gives the precise cause. Kickstarting `daily-state-summary` in
its broken state produced:

```
xpcproxy[14659]  Unable to update LWCR with smd: 3
launchd[1] [gui/501/agenc-cron.04af416d-...[14659]:] Service could not initialize:
  Unable to get updated LWCR for (E3156307-..., /Users/odyssey/Library/LaunchAgents/
  agenc-cron.04af416d-....plist, 501), error 0x3 - No such process
```

LWCR = Lightweight Code Requirement, macOS's per-job code-identity record.
`xpcproxy` (the stub launchd execs before handing off to the real program) could
not resolve the job's code requirement, so the job died before `exec`.

### Why nothing surfaced

Because the failure happens *before* `agenc` runs:

- `StandardOutPath`/`StandardErrorPath` were never written — the cron log files'
  mtimes froze at each cron's last good run.
- No mission row was created, so `agenc cron history` simply showed nothing new.
- No notification, because nothing ran to post one.

The only place the failure was visible was `launchctl print`, which nothing
watches.

### Blast radius and the surviving cron

| cron | schedule | last good run | state found |
|---|---|---|---|
| daily-state-summary | `30 23 * * *` | 2026-08-31 | EX_CONFIG 78 |
| hn-daily-pull | `0 2 * * *` | 2026-09-01 | EX_CONFIG 78 |
| arpan-claude-optimization-suggestions | `0 13 * * 5` | 2026-08-28 | OS_REASON_CODESIGNING |
| claude-news-processor | `0 9 * * 5` | 2026-08-28 | OS_REASON_CODESIGNING |
| exobrain-update | `0 23 * * 4` | 2026-08-27 | OS_REASON_CODESIGNING |
| verify-workspace-mcp-denylist | `11 5 17 * *` | 2026-07-16 | `runs = 0`, missed 2026-08-17 |
| flight-watcher | `0 7 * * *` | 2026-09-06 | healthy, `runs = 5` |

`runs` resets to 0 when a launchd job is (re)bootstrapped. flight-watcher's
`runs = 5` corresponds exactly to 2026-09-02 through 09-06, i.e. it was
bootstrapped around 2026-09-01 while the other six carried registrations from
before then. That is the only property separating the survivor from the casualties.

The correlated event: `/Users/odyssey/app/agenc/_build/agenc` and `_build/` both
have mtime **2026-09-01 13:55** — the binary every cron plist points at was
rebuilt then.

### What I could NOT prove

I tried to reproduce the LWCR breakage by replacing a binary underneath a loaded
launchd job. It did **not** reproduce:

- built a small ad-hoc-signed Go binary, bootstrapped two jobs against it
  (one pointing at the binary directly, one at a bash wrapper that execs it)
- rebuilt the binary so its CDHash changed
  (`a401e06c…` → `e8a9dbe3…`), touching neither job
- kickstarted both — **both ran fine**

So "rebuilding the binary bricks the launchd job" is refuted as a sufficient
condition. The rebuild is a strong temporal correlate, not a proven cause. The
machine has been up since 2026-08-21, so a reboot is not the trigger either.

**Consequence for the fix:** since the trigger is not identified, the durable fix
must be *detection and repair*, which works regardless of cause — not an attempt
to eliminate a trigger I cannot name.

## Remediation applied

For all seven jobs:

```
launchctl bootout   gui/501/agenc-cron.<uuid>
launchctl bootstrap gui/501 ~/Library/LaunchAgents/agenc-cron.<uuid>.plist
```

Verified on `daily-state-summary` as a controlled before/after:

- before reload — kickstart → `runs` 16→17, `last exit code = 78`, log file
  unchanged (0 bytes appended)
- after reload — kickstart → `runs = 1`, `last exit code = 0`, log appended
  `Created mission: bddbc19a`

## The deeper defect: cron missions that die mid-run

Beads `agenc-inrf` and `exobrain-6g1`. A cron mission that dies partway leaves a
mission row indistinguishable from a healthy one — status is computed at read
time, and `prompt_count`/`ai_summary` are identical across dead and healthy runs.

**Finding the discriminator took two attempts, and the first was wrong.**

Each mission's `~/.agenc/missions/<uuid>/wrapper.log` records Claude hook events.
The obvious candidate was the `Stop` hook — Claude fires it when it finishes a
turn. Checked against the runs `exobrain-6g1` had independently identified, it
looked perfect: both confirmed-dead runs had zero `Stop` events and every
healthy run had two.

That sample was too small. Widened to every recent mission, `Stop` falls apart:
several missions that plainly did substantial work logged zero `Stop` events,
including this mission itself while actively running. Keying on `Stop` reports
healthy crons as dead.

`PostToolUse` separates them cleanly. Measured:

| mission | bytes | Stop | idle_prompt | PostToolUse | truth |
|---|---|---|---|---|---|
| 3052f4f1 (08-30) | 546 | 0 | 0 | **0** | died (bead: model unavailable) |
| 90e21a58 (08-26) | 848 | 0 | 2 | **0** | died (bead: ENOTFOUND) |
| a604036d (08-24) | 848 | 0 | 2 | **0** | died |
| a4540fb5 (09-06) | 847 | 0 | 2 | **0** | died |
| d90d1228 (09-06) | 846 | 0 | 2 | **0** | died |
| 787a113c (09-01) | 11396 | 2 | 2 | **74** | worked |
| bddbc19a (09-06) | 8053 | 2 | 2 | **50** | worked |
| b02f5ce5 (09-06) | 7500 | 2 | 2 | **46** | worked |
| 3628249f (09-06) | 12238 | **0** | 2 | **82** | worked |
| 9eb25b0c (09-06) | 5057 | **0** | 2 | **30** | worked |
| 3c92d1ba (09-06) | 7808 | **0** | 2 | **46** | worked |
| 10a97e0b (live) | 23864 | **0** | 2 | **160** | running fine |

The three bolded zero-`Stop` rows with 30-82 tool calls are what killed the
first discriminator. `idle_prompt` is no good either — dead runs have it too.

So the signal is: **a finished run that made no tool calls did no work.** That is
also right semantically — a cron skill that reads nothing, calls nothing and
writes nothing delivered nothing, whatever its exit status looked like.

## Fix shape

A deterministic auditor (`agenc cron doctor`) that, per enabled cron, checks:

1. launchd job loaded, and its last exit status — catches the outage class at the
   spawn layer, the thing nothing was watching
2. a mission actually created since the previous expected fire time — catches
   non-delivery whatever the cause, including causes not yet identified
3. the latest finished run made at least one tool call — catches mid-run death

…reports findings, repairs bricked launchd jobs, and posts an AgenC notification.
Scheduled independently of the crons it audits.

## Verification performed

Every claim below was produced by running the thing, not by reasoning about it.

**The auditor is scheduled and independent.** Installed as launchd job
`agenc-doctor`, every 15 minutes, running
`agenc cron doctor --repair --notify`. It is deliberately not named with the
`agenc-cron` prefix, because AgenC's own cron syncer deletes plists matching that
prefix that have no config entry — it would have removed its own watchdog.

**Induced failure 1 — a cron that can never fire.**
Booted out `verify-workspace-mcp-denylist`'s launchd job, then fired the auditor:

```
Reloaded 1 launchd job(s): verify-workspace-mcp-denylist
  [critical] verify-workspace-mcp-denylist: launchd has no job registered for
             this cron, so it can never fire
Posted an AgenC notification with these findings.
```

launchd confirmed the job present again afterwards. Detected, repaired, reported.

**Induced failure 2 — a run that dies before doing any work.**
Spawned a real `daily-state-summary` run and stopped it before it made a tool
call. Its wrapper log came out at 546 bytes with 0 `PostToolUse` — byte-for-byte
the shape of `3052f4f1`, the run `exobrain-6g1` recorded as dying on a
model-unavailable error. The auditor then reported:

```
  [warning] daily-state-summary: its run at 2026-09-06 22:17 made no tool calls
            at all — the mission started and died before doing any work,
            delivering nothing
```

**Unplanned true positive.** On its first live run the auditor flagged two real
failures nobody asked it to look for: the `flight-watcher` and
`verify-workspace-mcp-denylist` runs kickstarted at 21:25 and 21:27 had both
died at spawn without doing any work. Under the old system those would have
passed silently, exactly as the nine dead `hn-daily-pull` runs did.

## Known limitation, and the recommendation behind it

The auditor runs on the same machine, from the same binary family, and reports
through the same AgenC server as the crons it watches. A failure that takes out
all of AgenC takes out its watchdog too. This is the shared-fate problem, and it
is real — it is why a self-reporting cron could never have caught this outage.

The ops-world answer is an **external dead-man's-switch**: an outside service
that alerts on the *absence* of a success ping inside an expected window.
The ordering is the whole mechanism — the ping must fire only after the job has
verified its own deliverable, because a naive "the process ran" ping would have
reported success on every one of the nine `hn-daily-pull` runs that delivered
nothing.

`agenc cron doctor` is already shaped to be that ping's source: it exits 0 only
when every cron has actually delivered, and nonzero otherwise. Wiring it to an
external monitor is a one-line change to the scheduled command.

**This needs Kevin's decision, not an agent's** — it means signing up for a
third-party service (Healthchecks.io was suggested by a parallel research
mission; free tier covers seven crons). Deliberately not provisioned.
