---
name: tagging-a-new-release
description: >-
  Guides the release tagging workflow for this project. Invoke when the user
  asks to tag, release, cut a release, bump the version, or publish a new
  version. Determines the next semver version, presents it for user approval,
  then tags and pushes.
---

Tagging a New Release
=====================

This project uses **semantic versioning** (semver) and **GoReleaser**. Releases are triggered by pushing a Git tag — CI handles the build, binary compilation, and Homebrew tap update automatically. Do not run GoReleaser locally.

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
git tag --sort=-v:refname | head -5
```

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

### 4. Suggest a version — then wait

Present the user with:

1. The **changelog summary** from step 3
2. The **suggested version** with brief reasoning
3. The **commit that will be tagged** (short SHA + first line)

<confirmation-gate>
STOP here and wait for the user to confirm or override the version number. Do not proceed to tagging until the user explicitly approves a specific version.

The user decides the version — your suggestion is advisory. If the user picks a different version, use theirs without pushback.
</confirmation-gate>

### 5. Tag and push

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

### 6. Verify the release

The tag push triggers the GitHub Actions release workflow (`.github/workflows/release.yml`), which runs GoReleaser to build binaries and update the Homebrew tap.

Monitor the workflow until it completes:

```bash
gh run list --workflow=release.yml --limit=1
```

If the run is still in progress, use `gh run watch <run-id>` to stream logs (get the run ID from the `gh run list` output).

The release is not finished until the workflow reports success. If it fails, investigate and resolve the issue before telling the user the release is done.

When the workflow succeeds, confirm to the user with:
- The version that was released
- A link to the GitHub release page
- A reminder that `brew upgrade agenc` will pick up the new version
