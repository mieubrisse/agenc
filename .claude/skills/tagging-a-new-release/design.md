Purpose
=======

Cutting a release in this repo is mostly mechanical and partly not. The mechanical half — pre-flight, semver classification, tag, push, watch the workflow — is easy to get right and easy to write down. The part that keeps going wrong is everything after the workflow turns green, because nothing in the environment signals that work remains: GoReleaser publishes a release that looks finished, the Homebrew tap updates, CI is green, and the release notes are a commit list nobody wrote.

This skill exists so that an agent cutting a release produces what the repo's published releases have always actually contained — written notes, and a `CHANGELOG.md` entry when something breaks — without having to rediscover either by comparing a previous release body against `.goreleaser.yaml`.

Provenance
==========

### Created

- **Skill body:** predates this sidecar. The original covered steps 1 through 6 (pre-flight through verifying the workflow) and stopped there.
- **Sidecar:** created 2026-11-05 as part of the edit below, because `/cc-skill-management` requires one and the skill had none.

### Edits

- **2026-11-05** — Added the post-workflow half of the release: `CHANGELOG.md` before tagging (step 4), replacing GoReleaser's generated body (step 8), the fact-sourcing rule and fresh-eyes gate inside it, and the unverified-claims line in the final report. Renumbered the old steps 4–6 to 5–7 and moved the user-facing confirmation out of the workflow-verification step into its own step 9.
  - Session: `2bc56a0d-9f76-46e1-894c-d2afe2144554`
  - Mission: `beaf3b88-17c6-4707-aa17-e6504ee42ddd`
  - Tracked as: Linear KEV-219
  - Sources: the v0.15.0 release (`https://github.com/mieubrisse/agenc/releases/tag/v0.15.0`), cut during that mission; `.goreleaser.yaml`; `cmd/tmux_attach.go`, `cmd/mission_ls.go`, `internal/config/protected_env_vars.go` for the four miscalled facts; `brew info --json=v2 tmux`.

Design Decisions
================

- **`CHANGELOG.md` goes before the tag, not after.** The v0.15.0 release did it afterward, which is how the ordering problem surfaced: the tagged commit is the released source, so an entry added after tagging is absent from what users download. The step sits between change analysis and the confirmation gate so the entry is written while the breaking changes are freshly in hand, and so the user sees it at the gate.

- **The fact-sourcing rule is anchored in four named incidents rather than stated as a principle.** "Verify your claims" is advice every agent already believes it follows, which is exactly why it failed here: all four wrong claims were drafted faithfully from long, well-evidenced PR bodies, and none of them felt like guessing. The incidents carry the discriminator a bare principle cannot — that plausibility, specificity and traceability to a real document are all present in the failure case, so the check has to be reading the code rather than assessing how sourced a claim feels.

- **The fresh-eyes gate on the notes is specified as a subagent, not as self-review.** Every one of those four was caught by an independent reviewer and none by the author re-reading. The stated reason is in the body: the author knows which PR each claim came from, and that knowledge is what makes a wrong claim look checked.

- **The disclosure line is not quoted in the skill.** It is governed by the author's global guidance, which a clone of this repo would not carry, so the skill points at the previous release's body as the model instead. That keeps the step correct for any agent in any environment and avoids a second copy of a string that lives elsewhere.

- **`gh release view` and `gh release edit` are written with explicit `--repo`.** Agents run this from AgenC mission clones and from worktrees, where the inferred repo is not always what they expect.

- **Rejected: a `release:` section in `.goreleaser.yaml` to stop generating the body.** That would remove the clobber hazard the skill now warns about, and it is a real option worth taking. It was left alone because it changes release machinery rather than guidance, and bundling it into a documentation edit would have put an untested release-pipeline change in a PR nobody would think to review as one. Worth filing separately.

- **Rejected: a verifier that checks the notes exist.** There is no cheap deterministic check for "the body is written prose rather than a commit list" that would not also fail on a legitimately terse release, and a check that can be satisfied by pasting anything into the body protects nothing. The gate stays human and procedural.
