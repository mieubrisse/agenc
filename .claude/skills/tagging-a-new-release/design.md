Purpose
=======

Cutting a release in this repo is mostly mechanical and partly not. The mechanical half — pre-flight, semver classification, tag, push, watch the workflow — is easy to get right and easy to write down. The part that keeps going wrong is everything after the workflow turns green, because nothing in the environment signals that work remains: GoReleaser publishes a release that looks finished, the Homebrew tap updates, CI is green, and the release notes are a commit list nobody wrote.

Hand-written notes are the standard set by the two most recent releases, v0.14.0 and v0.15.0, not a long-standing repo practice — 33 of the 35 published releases shipped GoReleaser's generated body. This skill exists so an agent cutting the next one meets that standard rather than copying the majority that do not, and records a `CHANGELOG.md` entry when something breaks, without having to rediscover either by comparing a previous release body against `.goreleaser.yaml`.

Provenance
==========

### Created

- **Skill body:** predates this sidecar. The original covered six steps, pre-flight through verifying the workflow, and stopped there.
- **Sidecar:** created 2026-11-05 as part of the edit below, because `/cc-skill-management` requires one and the skill had none.

### Edits

- **2026-11-05** — Added the post-workflow half of the release: the `CHANGELOG.md` entry before tagging, replacing GoReleaser's generated body, the fact-sourcing rule and fresh-eyes gate inside it, and the unverified-claims line in the final report. Added the three tag-safety invariants above the steps. Grew from six steps to ten.
  - Session: `2bc56a0d-9f76-46e1-894c-d2afe2144554`
  - Mission: `beaf3b88-17c6-4707-aa17-e6504ee42ddd`
  - Tracked as: Linear KEV-219
  - Sources: the v0.15.0 release (`https://github.com/mieubrisse/agenc/releases/tag/v0.15.0`), cut during that mission; `.goreleaser.yaml`; `cmd/tmux_attach.go`, `cmd/mission_ls.go`, `internal/config/protected_env_vars.go` for the four miscalled facts; `brew info --json=v2 tmux`; the repo's `CLAUDE.md` ("Always run `git pull --rebase` before pushing", under Git Workflow) for the pull-rebase mandate; the global `new-semver-release` skill for the tag-safety invariants.
  - A fresh-context verifier returned OPERABLE WITH GAPS on the first draft of this edit, and five of its findings changed behaviour. All five were confirmed against the repo or the live GitHub API before being acted on:
    - **The workflow check read the wrong run.** `gh run list --workflow=release.yml --limit=1` returns the most recent run, and GitHub takes seconds to register a new one, so immediately after a push it returns the *previous* release's `success` row. The stated pass condition was satisfied by stale state, directly upstream of the step that warns against acting before the run finishes. Fixed by filtering on `--branch v<version>`; verified that `headBranch` carries the tag name for tag-triggered runs, and that `--branch v0.15.0` isolates exactly one run.
    - **The CHANGELOG step never committed anything.** Its entire rationale is that the tagged commit is the released source, and followed literally it produced an uncommitted working-tree change that the subsequent tag therefore excluded — invisibly. Split into drafting (step 4) and committing-then-tagging (step 6).
    - **The CHANGELOG step needed a version it could not have yet.** It said to match the existing entries' shape, and those entries name the version that introduced them, but the step ran before the version was confirmed. Resolved by making the draft/commit split explicit about when the number arrives.
    - **"Every published release in this repo carries hand-written prose" was false.** Measured across all 35 published releases: 2 carry prose and 33 are raw GoReleaser output. See "Prose is the standard without being the history" below.
    - **Nothing covered a workflow failure after the tag was pushed**, which is the boundary where the obvious move is the wrong one. Fixed by the invariants block.
  - It also flagged the clause "minor is the 'feature release,' and patch is the 'fix release'" as restating the bump table directly above it. That was accepted: the clause is gone, and step 3's summary sentence now reads only "While the project is pre-1.0, breaking changes bump minor rather than major."
  - One finding was declined. The verifier could not reproduce the sandbox pipe warning in step 2, but its test piped a *succeeding* command while the claim is about pipe failures, so it is not refuted. The warning stays, and the consequence it guards against — an empty tag list reading as "no releases yet", which would restart the version series over the top of a live release — is now stated so a reader can judge the risk rather than take the prohibition on faith.

Design Decisions
================

- **Prose is the standard without being the history, and the skill says both.** The first draft of this edit asserted that every published release carried hand-written prose, inferred from the single release its author had opened. The measured split is 2 of 35. The correction matters in two directions, which is why the body carries both halves: an agent told "every release has prose" and then reading the skill's own instruction to check claims finds the skill wrong about its own repo, and may discount the step; and an agent told to use "the previous release" as a model gets a commit list for any baseline at or before v0.13.1 — the very thing it was sent to replace. So the body names the two prose releases explicitly, says prose is the current standard, and warns against treating an older release as the model. It also enumerates the sections rather than pointing at a model release, because the two prose releases disagree on section order and "match the previous one" therefore has no stable referent.

- **`CHANGELOG.md` goes before the tag, not after.** The v0.15.0 release did it afterward, which is how the ordering problem surfaced: the tagged commit is the released source, so an entry added after tagging is absent from what users download. Drafting sits before the confirmation gate so the user sees the entry while approving the version; committing sits after it, because each entry names the version that introduced it and that number does not exist until the gate closes.

- **The fact-sourcing rule is anchored in named incidents rather than stated as a principle.** "Verify your claims" is advice every agent already believes it follows, which is exactly why it failed here: all four wrong claims were drafted faithfully from long, well-evidenced PR bodies, and none felt like guessing. The incidents carry the discriminator a bare principle cannot — that plausibility, specificity and traceability to a real document are all present in the failure case, so the check has to be reading the code rather than assessing how sourced a claim feels. The 2-of-35 error is included as a fifth incident precisely because the skill's own author committed it while writing that section.

- **The fresh-eyes gate on the notes is specified as a subagent, not self-review.** Every one of those failures was caught by an independent reviewer and none by the author re-reading. The stated reason is in the body: the author knows which PR each claim came from, and that knowledge is what makes a wrong claim look checked.

- **The tag-safety invariants are duplicated from the global `new-semver-release` skill, deliberately.** That skill carries the same triggers and near-identical framing, and this one reads as a fork of it that dropped three invariants: never delete a remote tag, approval does not transfer between versions, and restart from step 1 after a CI failure. Single Source of Truth would normally say to reference rather than restate — but this repo is public and its `.claude/skills/` travels to every clone, where the author's global skills do not exist. The canonical source is absent at the point of use, not merely slow, so the duplication is justified. A maintainer changing either copy should change both.

- **The disclosure line is not quoted in the skill.** It is governed by the author's global guidance, which a clone would not carry, so the skill points at v0.15.0's body for the exact text and conditions carrying it on "if your own guidance requires one." v0.15.0 is named specifically rather than "the previous release" so the pointer still resolves to a prose body after future releases.

- **`gh` invocations carry explicit `--repo mieubrisse/agenc`.** Agents run this from AgenC mission clones and from worktrees, where the inferred repo is not always what they expect. The first draft of this edit applied that to `gh release` but not to `gh run list`, which is exposed to the same condition; both carry it now.

- **Rejected: adding a `release:` section to `.goreleaser.yaml`.** That would stop GoReleaser generating a body at all and remove the clobber hazard the skill warns about. It is a real option and probably the right long-term fix. Left alone because it changes release machinery rather than guidance, and bundling an untested release-pipeline change into a documentation edit would put it in a PR nobody would think to review as one. Worth filing separately.

- **Rejected: a deterministic verifier that checks the notes exist.** There is no cheap check for "the body is written prose rather than a commit list" that would not also fail a legitimately terse release, and a check satisfiable by pasting anything into the body protects nothing. The gate stays procedural.
