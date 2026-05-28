AgenC → Autonomous, High-Quality Claude Orchestrator
====================================================

A strategic plan, structured with the Double Diamond.

> **Provenance.** Authored by an AgenC agent in mission `e68829d7-fade-49b7-bc60-a3b82f1c492d` on 2026-05-28. Run `agenc session print` on that mission for the full research transcript (five parallel investigations: backlog mining, autonomy/quality-loop code audit, external best-practice research, AgenC maintainability/UX-risk audit, and a fresh-eyes request review). Tracking epic: `agenc-g7nk`. Phase beads: `agenc-9jgp` (Phase 0), `agenc-zwot` (Phase 1), `agenc-wh9p` (Phase 2), `agenc-n6jh` (Phase 3), `agenc-fs5i` (Phase 4), `agenc-hkvw` (cross-cutting), `agenc-8616` (Define artifact), `agenc-s9w3` (build-vs-adopt spike).


The one-paragraph version
-------------------------

AgenC today is a *reliable human-in-the-loop* orchestrator. What blocks fully-autonomous, high-quality execution is **not** model intelligence — Opus 4.8 writes excellent first drafts — it is that **AgenC has no way to know whether a mission's output is good without a human looking at it.** There is no verifier, no quality gate, no feedback loop anywhere in the harness; output quality rests entirely on the Claude config (skills/CLAUDE.md) with no backstop. The transformation, therefore, is not "add autonomy" — it is "**make trust transferable from human to machine**": build the verification, escalation, and memory infrastructure that lets AgenC accept or reject mission output on the user's behalf, and let autonomy expand exactly as far as that verification reaches. This must be built on a *stabilized* orchestrator, because autonomy amplifies orchestrator bugs into silent, ephemeral data loss.


The reframe, and why it is not a hedge
--------------------------------------

The request asks for "fully autonomous" and "high-quality output," with quality paramount. Held literally and together, those two goals are in tension, and the resolution is the central design decision of this entire program:

> **Autonomy bounded by verifiability.** Full autonomy is the goal *for the work we can verify*; everything else stays gated by a human until verification catches up.

This is not a retreat from ambition. "Fully autonomous AND high-quality" *without an oracle that distinguishes good from bad* collapses into "fully autonomous and high-quality-**looking**" — confident, plausible, and wrong at a rate no human is watching for. That is the single failure mode that destroys trust in an autonomous system, and a smarter model does not fix it: a model still cannot certify its own work (authorship bias is architectural, not a capability gap). So making the **verifier the first-class design object** is precisely what *serves* the "quality is paramount" requirement. The autonomy you get is real and full — it is simply gated by an oracle so that it stays high-quality.

Confidence: high. This conclusion converged independently across the external best-practice research ("ground autonomy in execution signals, not model self-judgment"), the code audit (the quality gap is total and exact), and the fresh-eyes review. I would update if the user's real mission mix turns out to be dominated by work with no possible verifier *and* where errors are cheap — but "quality is paramount" argues the opposite.


DISCOVER — what is actually true today
======================================

(First diamond, divergent. Findings from the five-stream investigation; every claim is grounded in the codebase or the backlog.)

What AgenC already is
---------------------

A mature, well-architected, human-in-the-loop system: per-mission isolation (own repo clone, own Claude config, own tmux window), a thin-CLI/thick-server split, a shadow-repo config pipeline, a tmux attach/detach model, visual pane-color feedback, a Notification Center, an idle picker, FTS search over transcripts, cron scheduling, and mission-spawns-mission provenance. The entire design is oriented around *a human steering*.

The autonomy primitives that exist (thin)
------------------------------------------

- **Headless missions** (`internal/wrapper/wrapper.go` `RunHeadless`): run `claude --print -p <prompt>`, capture output to `claude-output.log`. **And that is the end of the pipeline** — nothing reads, evaluates, or acts on the result. No restart machine, no hook socket.
- **Cron** (`internal/server/cron_syncer.go` → launchd → `agenc mission new --headless`): fire-and-forget. No completion callback, no success check, no retry.
- **Mission-spawns-mission** (`cmd/mission_new.go`): the CLI auto-stamps `source=mission, source_id=<parent>`. But a parent has **no primitive to await, poll, or receive a child's result.**

The quality gap (total)
-----------------------

A targeted search of the entire codebase for any mechanism that evaluates, reviews, verifies, scores, or gates output quality returns **nothing**. Specifically:

- After a headless mission exits, `claude-output.log` is never read by the harness.
- After a cron mission completes, the only harness action is a user-facing "a mission ran" notification — not "the mission succeeded."
- There is **no DB column, no endpoint, no data type** for a quality verdict, an acceptance status, or a retry-with-context.
- Output quality depends *entirely* on the CLAUDE.md/skill chain the agent carries. There is no harness backstop.

The backlog (`bd`, ~430 issues) confirms this is the gap: it is rich in autonomy *primitives* (the "CEO managing fleets of agents" vision, `agenc-uui` P1; inter-agent messaging `agenc-qoj`; provenance `agenc-1ckj`) and rich in *post-hoc* quality ideas (adjutant-as-coach `agenc-317`; stuck-detection `agenc-n79`; auto-retrospective `agenc-3ms2`; friction capture `agenc-82i`) — but has **zero issues for an in-flight quality gate.** Every quality idea is offline improvement of the *configuration*, never an in-flight gate on *output*.

The integration point already exists
-------------------------------------

The hook surface is the seam a quality loop plugs into. The **Stop hook** fires at harness level on every turn-end → wrapper marks idle → the server's `fireQueuedReload` async-reload machine (`internal/server/missions.go`) can already restart Claude with a queued follow-up prompt. That is exactly the shape of "when Claude says it's done, run a review pass and feed critique back." The reusable primitives: `POST /missions/{id}/reload?async=true&prompt=`, child-mission spawn, `mission send-keys`, the FTS search endpoint, and `source/source_id` lineage.

The foundation is not yet load-bearing (the binding constraint)
----------------------------------------------------------------

The maintainability audit found three **HIGH** risks that form a dependency chain, plus several more:

| Risk | Severity | Evidence |
|------|----------|----------|
| Config reads bypass the server cache (26 callers re-parse YAML on hot paths) | HIGH | `agenc-347`; `handle_crons.go`, `repos.go`, `wrapper.go` |
| git operations have no timeout — a hung remote silently blocks a loop goroutine forever; `/health` still reports "running" | HIGH | `agenc-336` (P0); 79 `exec.Command` vs 38 `exec.CommandContext` |
| DB layer has no `context` propagation and inconsistent not-found semantics; not testable without a real SQLite file | HIGH | `agenc-337`, `agenc-346`, `agenc-350` (all blocked) |
| 90+ flat files in `cmd/`; **two parallel cron verb-trees** (`agenc cron` vs `agenc config cron`) | MED | `agenc-344` |
| 11 server background loops; crashed loops are marked "degraded" but **never restarted** | MED | `server.go` `runLoop` |
| ~5% test-coverage floor; **zero** `cmd/` unit tests; tmux paths manual-only | MED | `Makefile`; `specs/remaining-codebase-cleanup.md` |
| `wrapper.go` still ~992 lines mixing event loop + headless + process mgmt | LOW-MED | `agenc-342` (P0) |

The two P0 beads (`agenc-351` master tech-debt epic, `agenc-342` wrapper refactor) are the most load-bearing items in the entire backlog; the epic itself states fixing failing tests "BLOCKS EVERYTHING." This matters acutely here because **autonomy amplifies these bugs into silent, ephemeral data loss** — missions are throwaway, only pushed work survives, and an unattended agent hitting an orchestrator bug loses work with no human watching. Every new autonomous feature wants its own background loop, config access, DB write, and git op — i.e., it hits all three HIGH risks at once.


DEFINE — the problem, sharply
=============================

(First diamond, convergent. → tracked as `agenc-8616`.)

The problem is **not** "AgenC cannot run Claude unattended" (it can — headless + cron). The problem is:

> **AgenC cannot tell whether unattended work is good, cannot feed a correction back when it isn't, cannot decide when to escalate to a human, and cannot carry what it learns into the next run — and its own foundation is not yet stable enough to do any of this without losing work.**

Four sub-problems fall out, each mapping to a pillar the user named:

1. **No verifier / no quality gate.** (Quality is paramount.) → Phases 1–2.
2. **No structured feedback/result store, and no compounding memory.** (Feedback, store work, reference info.) → Phases 1 & 4.
3. **No graduated trust model** — autonomy is conceived as a global switch, not earned per work-type. → Phase 3.
4. **The orchestrator itself is a source of entropy and silent failure.** (A buggy/entropy-filled orchestrator is no good.) → Phase 0 + cross-cutting.

Two design objects must be defined before building (the Define deliverable, `agenc-8616`):

**(a) The (value × verifiability) mission taxonomy.** Catalogue AgenC's mission types and score each on *value* and *verifiability*. Code changes are highly verifiable (tests / build / lint / CI are the oracle). Prose, strategy, and design often have *no* deterministic oracle. Autonomy can only safely reach the verifiable quadrant; the rest stays human-reviewed. This taxonomy *is* the scope-setting instrument — it prevents the program from promising autonomy where no oracle can exist.

**(b) The autonomy trust ladder** (promotes mission *types*, never a global flip):

| Level | Behaviour |
|-------|-----------|
| **L0 Attended** | Today's default — human steers in tmux. |
| **L1 Gated** | Runs autonomously but pauses at defined gates for human approval. |
| **L2 Reviewed** | Runs to completion, produces a *graded artifact*, human reviews before accept. |
| **L3 Verified-autonomous** | Auto-accepts when the objective verifier is green and the judge passes; escalates otherwise. |


DEVELOP — the solution architecture
===================================

(Second diamond, divergent. The mechanisms, grounded in what the codebase already offers and what external practice validates.)

The quality loop (the heart of the system)
------------------------------------------

On mission completion (Stop hook → idle), instead of returning straight to the human, the mission enters a **quality gate** with three layers:

1. **Objective verifier (deterministic — the hard gate).** The harness runs the mission's *own* project checks — build, tests, lint, E2E. Green is a *necessary precondition* for "done." This is the reward-hack-resistant ground truth, because it is not the agent grading itself. External research is unambiguous here: execution-based verification (tests/CI as the reward signal) beats subjective LLM judgment. AgenC's own `make check` + mandatory E2E culture is already the right shape — lean into it as the primary oracle.

2. **Judge / critic (LLM-as-judge, fresh context — the soft gate).** For what tests cannot capture — scope adherence, design quality, "did it do what was actually asked," readability — a reviewer subagent with a *fresh context window and no authorship bias* grades the diff against a rubric and emits a structured verdict + critique. (~80–85% human agreement at a tiny fraction of human cost, per the research.) Critically: the judge *cannot grade its own authored work*, and it is advisory relative to the objective verifier, never a substitute for it.

3. **Loop or escalate.** On failure → feed the structured critique back via the existing `reload?async=true&prompt=<critique>` machine and retry, **bounded by a budget/retry ceiling** (no unbounded loops, no cost runaway, termination must be provable). On pass → accept per the mission's ladder level. On retry-exhaustion, an *irreversible* action, a denylisted path, or a novel error class → **escalate to a human** under reversibility-tiered rules (reversible + green may proceed; irreversible requires approval, regardless of the agent's self-rated confidence — "confidence is one signal, not the signal").

The result record (the missing substrate)
------------------------------------------

An append-only, `mission_id`-linked store — architecturally analogous to the existing `notifications` table — holding the verdict, the critique, the acceptance status, and the retry lineage. This is the **single source of truth for "how did this mission do."** It is the substrate the in-flight loop writes, *and* the data layer the already-conceived post-hoc loops (`agenc-317`, `-n79`, `-3ms2`, `-82i`) consume. Build it once; both halves of the quality story (in-flight gating and offline improvement) read from it.

Memory & compounding quality (SST-respecting)
----------------------------------------------

Do **not** build a new, vendor-locked memory store — that recreates the drifting-duplicate-context problem the user's own SST principle warns against. AgenC *already has* portable, queryable memory: **git** (work product + history), **beads** (tasks / decisions / provenance), **skills + CLAUDE.md** (procedural memory), **session JSONL + FTS index** (transcript memory). The net-new work is connective tissue: (a) feed the result record into the retrospective/self-improvement loops so failures become *skill/CLAUDE.md improvements* (quality compounds run-over-run); and (b) assemble a "reference pack" of prior accepted work + learnings at mission spawn so agents start grounded rather than cold.

An explicit build-vs-adopt fork (`agenc-s9w3`)
----------------------------------------------

AgenC is built around the Claude Code *CLI* (spawn `claude`, hooks over a unix socket, OAuth passthrough). Anthropic now ships autonomous-run primitives (headless `-p`, hooks, subagents, the Agent SDK). Before committing the Phase 2 architecture, run a spike: should the quality loop *wrap/adopt* SDK supervision primitives, or *build custom* loops on the existing CLI+hook+async-reload machine? Resolve the integration-cost vs. lock-in tradeoff deliberately rather than by default.


DELIVER — the phased program
============================

(Second diamond, convergent. Each phase is a gate; the order is load-bearing. The solo-builder constraint is real — stabilization and autonomy compete for the same hands — so the sequencing is strict and Phase 0 is scoped *narrowly*, not "fix everything.")

**Phase 0 — Stabilize the foundation (GATE). `agenc-9jgp` (P0).**
Pay down *only* the dependency chain and the lifecycle/loop code autonomy will touch — not all 196 open beads. Binds: config-cache enforcement (`agenc-347`) → DB context propagation (`agenc-337`) → store interface (`agenc-350`); git-op timeouts (`agenc-336`); a loop **health-recovery** policy (restart crashed loops, don't just flag them); `wrapper.go` decomposition (`agenc-342`); the master tech-debt epic (`agenc-351`); a raised test floor on mission lifecycle + background loops; and an ephemeral-data-loss / checkpoint audit. **This phase must close before Phase 2 ships to production.**

**Phase 1 — Verifier + result record. `agenc-zwot` (P1).** (Depends on Phase 0 + the Define taxonomy.)
Build the objective verifier (harness runs project checks, captures structured pass/fail) and the append-only mission-result record. This is the quality substrate everything else stands on.

**Phase 2 — The in-flight quality loop. `agenc-wh9p` (P1).** (Depends on Phase 1 + the build-vs-adopt spike.)
Wire Stop-hook → verify → judge → critique-feedback-retry → accept/escalate on the existing async-reload machine. Reversibility-tiered escalation; budget/retry ceilings; reward-hack resistance.

**Phase 3 — Trust ladder. `agenc-n6jh` (P2).** (Depends on Phase 2.)
Operationalize per-mission-type autonomy levels; promote a type to L3 only as its harness-measured accepted-rate (from the result record) justifies it.

**Phase 4 — Compounding memory. `agenc-fs5i` (P2).** (Depends on Phase 2.)
Feed the result record into the self-improvement loops; assemble reference packs at spawn. Reference, don't duplicate.

**Cross-cutting (continuous). `agenc-hkvw` (P1).**
UX coherence (resolve claude-config/reconfig confusion `agenc-tcoh`; collapse the dual cron verb-trees `agenc-344`; onboarding `agenc-1p7z`) **and** a standing maintainability rule: *every new background loop / autonomous feature must use the config cache, take a `context` for cancellation, and have a health-recovery policy* — so the autonomy expansion does not re-grow the very entropy Phase 0 is paying down.


Risks to hold in view
=====================

- **Reward-hacking the gate.** Once an automated check exists, agents optimize to pass *it*. Mitigation: the objective verifier (real tests) is the hard gate; the judge is advisory and cannot grade its own work.
- **Cost / loop runaway.** Unattended retry loops burn tokens. Mitigation: budget ceilings + provable termination, enforced in the harness.
- **Stabilization that never ends.** Phase 0 could swallow the program. Mitigation: scope it to the dependency chain + autonomy-adjacent code only; it is a gate, not a destination.
- **Verifier coverage gaps.** Where no oracle exists (prose/strategy), autonomy must *not* extend — those types stay at L1/L2 by design, not by omission.
- **Building the second story on a cracked foundation.** The whole reason Phase 0 precedes everything.


What I would want from you next
==============================

These are the inputs that would most sharpen execution (none block starting Phase 0):

1. **The verification oracle per mission type** — for each kind of work you run, what is the objective check? (This populates `agenc-8616` and sets the autonomy ceiling.)
2. **What "done" should mean** — auto-merge behind green CI? PR for review? Notify-and-hold? (Sets the top of the ladder.)
3. **Your mission mix** — roughly what fraction is verifiable code vs. unverifiable prose/strategy? (Determines how far L3 can reach.)
4. **Cost tolerance & failure blast-radius** — informs the budget ceilings and the reversibility tiers.
