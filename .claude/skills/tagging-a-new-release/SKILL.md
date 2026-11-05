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

A release is not finished when the workflow goes green. GoReleaser publishes a body nobody wrote, and the last two steps below replace it and record anything breaking. Skipping them ships a release whose notes are a commit list.

Workflow
--------

### 1. Pre-flight checks

Verify the environment is ready for a release. Do not proceed until all checks pass.

```bash
# Must be on the default branch
git branch --show-current

# Must be clean — no uncommitted changes
git status --porcelain

# Must be up to date with remote
git fetch origin
git status -uno
```

If the working tree is dirty, commit or stash first. If the branch is behind the remote, pull before proceeding.

### 2. Determine the current version

```bash
git fetch --tags origin
git tag --sort=-v:refname
```

Do not pipe through `head` or other commands — the sandbox silently swallows pipe failures, producing empty output with no error. Run `git tag` alone and read the output directly.

Identify the latest tag. This is the baseline for the next version.

### 3. Analyze changes and build a changelog

```bash
git log <latest-tag>..HEAD --oneline
```

Group the commits into a changelog summary for the user:

- **Features:** new capabilities, commands, integrations
- **Fixes:** bug fixes, crash fixes, correctness improvements
- **Internal:** refactoring, docs, CI changes, dependency updates

Then classify the overall change scope to determine the version bump:

| Change type | Version bump | Examples |
|---|---|---|
| Breaking / incompatible API changes | **Major** (X.0.0), or **Minor** while pre-1.0 | Removed a CLI command, changed config schema incompatibly, renamed a public concept |
| New features, capabilities, or commands | **Minor** (0.X.0) | Added a new subcommand, new config option, new integration |
| Bug fixes, docs, internal improvements | **Patch** (0.0.X) | Fixed a crash, corrected a typo, refactored internals with no behavior change |

While the project is pre-1.0, breaking changes bump minor (not major), minor is the "feature release," and patch is the "fix release."

### 4. Record breaking changes in CHANGELOG.md, before tagging

`CHANGELOG.md` is this repo's breaking-changes-and-migration document, not a per-release log — it carries an entry only for changes that can break a user who upgrades, each with a **Migration:** section telling them what to do about it.

If step 3 found anything of that kind — a removed or renamed command, an incompatible config change, a new minimum version of a dependency, a behavior an existing setup relies on — it gets an entry before the tag exists. Two reasons it has to be before rather than after: the tagged commit is the released source, so an entry added afterward is absent from the thing users download; and the release body is a dated announcement, while `CHANGELOG.md` is where someone upgrading across several versions actually looks.

Match the shape of the entries already in the file. Leave existing entries alone — they are the record of what shipped, and editing them rewrites history rather than adding to it.

If the release has no breaking changes, skip this step and say so when presenting the version, so the user can disagree.

### 5. Suggest a version — then wait

Present the user with:

1. The **changelog summary** from step 3
2. The **suggested version** with brief reasoning
3. The **commit that will be tagged** (short SHA + first line)
4. Whether step 4 found breaking changes, and what you wrote for them

<confirmation-gate>
STOP here and wait for the user to confirm or override the version number. Do not proceed to tagging until the user explicitly approves a specific version.

The user decides the version — your suggestion is advisory. If the user picks a different version, use theirs without pushback.
</confirmation-gate>

### 6. Tag and push

Once the user confirms the version, verify the tag does not already exist:

```bash
git tag -l v<confirmed-version>
```

If the tag already exists, tell the user and ask for a different version. Otherwise:

```bash
git tag v<confirmed-version>
```

```bash
git push origin v<confirmed-version>
```

### 7. Verify the workflow

The tag push triggers the GitHub Actions release workflow (`.github/workflows/release.yml`), which runs GoReleaser to build binaries and update the Homebrew tap.

Monitor the workflow until it completes:

```bash
gh run list --workflow=release.yml --limit=1
```

If the run is still in progress, use `gh run watch <run-id>` to stream logs (get the run ID from the `gh run list` output).

The release is not finished until the workflow reports success. If it fails, investigate and resolve the issue before telling the user the release is done.

### 8. Replace the release body with written notes

`.goreleaser.yaml` has no `release:` section, so GoReleaser generates the release body itself: a filtered commit list. Every published release in this repo carries hand-written prose instead, so the generated body is a placeholder to replace, not output to accept.

**Wait for the workflow to succeed first.** GoReleaser writes the body when it publishes the release, so an edit issued while the run is still going is overwritten with no error and no sign it happened.

Read the previous release's body as the model for structure, section order, and any disclosure line the repo's releases carry:

```bash
gh release view <previous-tag> --repo mieubrisse/agenc --json body -q .body
```

Then publish the notes and read back what landed:

```bash
gh release edit v<version> --repo mieubrisse/agenc --notes-file <path>
gh release view v<version> --repo mieubrisse/agenc --json body -q .body
```

Lead with what a user upgrading needs to know. A change they can trip over on upgrade outranks a larger change they will never notice, and anything from step 4 belongs here too, in the notes a reader sees first.

#### Where the facts come from

**Check every factual claim against the code or the live system before publishing. A merged PR body is a lead, not a source.**

This repo's PR bodies are unusually long and well-evidenced, which is exactly what makes them dangerous here: they read like primary evidence while being a dated snapshot of what one author believed while writing, and the code has moved since. Release notes go to every user, who can check them.

All four of these were caught in review of the v0.15.0 notes, after being drafted faithfully from PR bodies:

- **A remedy that was wrong on the path users hit first.** The notes told users with no tmux server to run `tmux` and carry on. Bare `agenc` is the attach path, and having run `tmux` the caller is inside a session where attach declines. `cmd/tmux_attach.go` carries both the real remedy and a comment explaining why the generic one is wrong there.
- **A new user-visible state omitted entirely.** `QUOTA` (`cmd/mission_ls.go`) appears in `agenc mission ls` and went unmentioned, because no PR body framed it as a headline.
- **A set described by its most memorable members.** The protected environment variables were called `AGENC_*`; `internal/config/protected_env_vars.go` holds the real list, two entries of which are not.
- **A dependency version taken from prose.** "Homebrew ships tmux 3.6a" came from a PR body where it was the author's own installed version. `brew info --json=v2 tmux` reported 3.8.

The pattern: each was plausible, specific, confidently worded, and traceable to a real document. Nothing about the drafting felt like guessing. So the check cannot be "does this feel sourced" — it has to be reading the code, or running the command, for each claim.

For a user-visible behavior change especially, read the remedy out of the code that produces the error rather than paraphrasing a PR's description of it.

#### Get fresh eyes on the notes before publishing

Spawn a subagent to review the drafted notes against the code, and give it the file path plus the merged PRs in range. The author of the notes is the worst-placed reader of them: they know which PR each claim came from, which is what makes a wrong claim look checked. Every failure listed above was caught this way and none was caught by re-reading.

### 9. Confirm to the user

Report:

- The version released, and a link to the GitHub release page
- That `brew upgrade agenc` picks it up
- Anything you could not verify, named as unverified

*Maintainer lineage for this skill — why each step exists and what it was built from — lives in `design.md` beside this file.*
