---
name: tagging-a-new-release
description: >-
  Guides the release workflow for this project, from version selection through
  the published release notes. Invoke when the user asks to tag, release, cut a
  release, bump the version, or publish a new version. Determines the next
  semver version, presents it for user approval, tags and pushes, then replaces
  GoReleaser's auto-generated release body with hand-written notes.
---

Tagging a New Release
=====================

This project uses **semantic versioning** (semver) and **GoReleaser**. Releases are triggered by pushing a Git tag — CI handles the build, binary compilation, and Homebrew tap update automatically. Do not run GoReleaser locally.

A release is not finished when the workflow goes green. GoReleaser publishes a body nobody wrote, and steps 8 and 9 below replace it. Stopping at green ships a release whose notes are a commit list.

Three invariants, before the steps
----------------------------------

These hold at every step, and two of them are one-way doors.

**Never delete a tag from the remote.** Tags are immutable release markers; deleting one breaks downstream references — changelogs, dependency locks, Homebrew formula URLs, audit trails. If a tag is pushed and the workflow then fails, the tag stays. Fix the problem and cut a new patch version, or re-run the workflow if the failure was transient and the commit is genuinely fine.

**Approval for one version is approval for that version only.** The user confirming v0.16.0 is not approval for v0.16.1, however obviously it follows. If a workflow failure sends you back for another release, you need a fresh confirmation — and go back to step 1, because fixing the failure added commits the approved changelog never covered.

**A failed workflow means the release did not happen, and anything claiming otherwise is now wrong.** That includes the `CHANGELOG.md` entry from step 4, which says a version shipped. Correct it in the follow-up release rather than leaving it asserting a version that does not exist.

Workflow
--------

### 1. Pre-flight checks

The default branch is `main`. Confirm all of the following before proceeding.

```bash
git branch --show-current     # must print: main
git status --porcelain        # must print nothing
git fetch origin
git log --oneline HEAD..origin/main   # must print nothing
```

If the working tree is dirty, commit or stash first.

If the last command prints anything, the remote is ahead — run `git pull --rebase`. Expect this: multiple agents and missions commit to this repo concurrently, so the remote is frequently ahead (see the repo's `CLAUDE.md`, "Always run `git pull --rebase` before pushing"). Resolve any conflicts the rebase surfaces before continuing.

### 2. Determine the current version

```bash
git fetch --tags origin
git tag --sort=-v:refname
```

Do not pipe this through `head` or another command. A piped command that fails inside the sandbox can produce empty output with no error, and an empty tag list reads as "no releases yet" — which would make the next version v0.1.0 over the top of a live release series. Run `git tag` alone and read the output.

The highest tag is the baseline for the next version.

### 3. Analyze what changed and summarize it for the user

```bash
git log <latest-tag>..HEAD --oneline
```

Group the commits into a summary for the user — this is the prose you will present at the gate in step 5, not a file:

- **Features:** new capabilities, commands, integrations
- **Fixes:** bug fixes, crash fixes, correctness improvements
- **Internal:** refactoring, docs, CI changes, dependency updates

Then classify the overall change scope to determine the version bump:

| Change type | Version bump | Examples |
|---|---|---|
| Breaking / incompatible API changes | **Major** (X.0.0), or **Minor** while pre-1.0 | Removed a CLI command, changed config schema incompatibly, renamed a public concept |
| New features, capabilities, or commands | **Minor** (0.X.0) | Added a new subcommand, new config option, new integration |
| Bug fixes, docs, internal improvements | **Patch** (0.0.X) | Fixed a crash, corrected a typo, refactored internals with no behavior change |

While the project is pre-1.0, breaking changes bump minor rather than major.

### 4. Draft the CHANGELOG.md entry for anything breaking

`CHANGELOG.md` is a different artifact from the step 3 summary. It is this repo's breaking-changes-and-migration document: it carries an entry only for changes that can break someone who upgrades, each with a **Migration:** section telling them what to do.

If step 3 found anything of that kind — a removed or renamed command, an incompatible config change, a new minimum version of a dependency, a behavior an existing setup relies on — draft the entry now and present it at the gate. If the release breaks nothing, skip this and say so at the gate, so the user can disagree.

Follow the existing entries' shape: an `###` heading naming the change, prose explaining what and why, then **Migration:**. Leave existing entries alone — they record what shipped.

Each entry also states which version introduced it, so the entry cannot be finished until step 5 settles the number. Draft the body now; fill the version in and commit it in step 6, once there is a number to write.

### 5. Present the version — then wait

Present the user with:

1. The **change summary** from step 3
2. The **suggested version**, with brief reasoning
3. The **commit that will be tagged** (short SHA + first line)
4. The **drafted CHANGELOG entry** from step 4, or that the release breaks nothing

<confirmation-gate>
STOP here and wait for the user to confirm or override the version number. Do not proceed to tagging until the user explicitly approves a specific version.

The user decides the version — your suggestion is advisory. If the user picks a different version, use theirs without pushback.
</confirmation-gate>

Normalize whatever the user says to a bare semver triple: "v0.16.0" and "0.16.0" both mean `0.16.0`, and the tag is that with one `v` prefix. Before tagging, check the confirmed version is actually higher than the step 2 baseline — a tag that sorts below a published release passes the does-not-exist check in step 6 and still corrupts the series.

### 6. Commit the CHANGELOG entry, then tag and push

If step 4 produced an entry, it lands **before** the tag, and this ordering is the whole point of the step: the tagged commit is the released source, so an entry committed afterward is absent from what users download.

```bash
# Only if step 4 produced an entry:
git add CHANGELOG.md
git commit -m "Record v<version>'s breaking changes"
git pull --rebase
git push origin main
```

Then confirm the tag is free and create it on the commit you are about to release:

```bash
git tag -l v<version>        # must print nothing
git tag v<version>
git push origin v<version>
```

If the tag already exists, stop and tell the user — do not delete it.

### 7. Verify the workflow for *this* tag

The tag push triggers `.github/workflows/release.yml`, which runs GoReleaser to build binaries and update the Homebrew tap.

**Identify the run by its tag, not by recency.** For a tag-triggered run, `headBranch` holds the tag name, so filter on it:

```bash
gh run list --repo mieubrisse/agenc --workflow=release.yml --branch v<version> \
  --json headBranch,status,conclusion,createdAt
```

Do not use `--limit=1` without `--branch`. GitHub takes a few seconds to register a new run, so the most recent row immediately after a push is the *previous* release's — `completed` and `success` — and reading it as this release's result sends you into step 8 while GoReleaser is still running, which is exactly the clobber that step warns about.

If the run is in progress, stream it with `gh run watch <run-id> --repo mieubrisse/agenc`.

The release is not finished until the run for this tag reports success. If it fails, investigate, and then re-read the three invariants above before doing anything about the tag.

### 8. Write the release notes

`.goreleaser.yaml` has no `release:` section, so GoReleaser generates the body itself: a filtered commit list. The two most recent releases, v0.14.0 and v0.15.0, replaced that with hand-written prose; the 33 releases before them shipped the generated list. Prose is the current standard and the generated body is a placeholder to replace — but do not treat an older release as the model for what good looks like, because most of them are the thing you are replacing.

Those two carry these sections, and not in a consistent order, so choose the order that fits the release:

- **New** — capabilities a user gains
- **Changed** — behavior that differs for an existing setup, including anything from step 4
- **Fixed** — defects a user may have hit
- **Upgrade** — the `brew upgrade agenc` instruction

Lead with what a user upgrading needs to know. Something they can trip over on upgrade outranks a larger change they will never notice.

Write the notes to a file **outside the repository** — a temp path or your scratchpad — so the worktree stays clean while a tag is in flight.

Both prose releases open with a disclosure line identifying the release notes as agent-authored. Read v0.15.0's body for the exact text and carry it if your own guidance requires one:

```bash
gh release view v0.15.0 --repo mieubrisse/agenc --json body -q .body
```

#### Where the facts come from

**Check every factual claim against the code or the live system before publishing. A merged PR body is a lead, not a source.**

This repo's PR bodies are unusually long and well-evidenced, which is exactly what makes them dangerous here: a PR body is a dated snapshot of what one author believed while writing, and the code has moved since. Release notes go to every user, and every claim in them is checkable.

All four of these were caught in review of the v0.15.0 notes, after being drafted faithfully from PR bodies:

- **A remedy that was wrong on the path users hit first.** The notes told users with no tmux server to run `tmux` and carry on. Bare `agenc` is the attach path, and having run `tmux` the caller is inside a session where attach declines. `cmd/tmux_attach.go` holds both the real remedy and a comment on why the generic one is wrong there.
- **A new user-visible state omitted entirely.** `QUOTA` (`cmd/mission_ls.go`) appears in `agenc mission ls` and went unmentioned, because no PR body framed it as a headline.
- **A set described by its most memorable members.** The protected environment variables were called `AGENC_*`; `internal/config/protected_env_vars.go` holds ten, two of which are not.
- **A dependency version taken from prose.** "Homebrew ships tmux 3.6a" came from a PR body where it was the author's own installed version. `brew info --json=v2 tmux` reported 3.8.

The pattern: each was plausible, specific, confidently worded, and traceable to a real document. Nothing about the drafting felt like guessing. So the check cannot be "does this feel sourced" — it is reading the code, or running the command, per claim.

Generalizing from one example is the same failure wearing a different coat. The first version of this skill claimed every published release carried hand-written prose, inferred from the one release its author had opened; the real count was two of thirty-five.

For a user-visible behavior change especially, read the remedy out of the code that produces the error rather than paraphrasing a PR's description of it.

#### Get fresh eyes on the notes before publishing

Spawn a subagent to review the drafted notes against the code, giving it the notes' path and the merged PRs in range. The author of the notes is the worst-placed reader of them: they know which PR each claim came from, and that knowledge is what makes a wrong claim look checked. Every failure listed above was caught this way; none was caught by re-reading.

### 9. Publish the notes and read them back

Only after step 7 reports success for this tag. GoReleaser writes the body when it publishes the release, so an edit issued while the run is still going is overwritten with no error and no sign it happened.

```bash
gh release edit v<version> --repo mieubrisse/agenc --notes-file <path-from-step-8>
gh release view v<version> --repo mieubrisse/agenc --json body -q .body
```

Read the output of the second command. It confirms the notes landed, though only as of that moment — it cannot catch a later overwrite, which is why the ordering above matters more than the read-back.

### 10. Confirm to the user

Report:

- The version released, and a link to the GitHub release page
- That `brew upgrade agenc` picks it up
- Whether the Homebrew tap formula actually updated, if you checked it
- Anything you could not verify, named as unverified

*Maintainer lineage for this skill — why each step exists, what it was built from, and its relationship to the global `new-semver-release` skill — lives in `design.md` beside this file.*
