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

This project uses **semantic versioning** (semver) and **GoReleaser**. Releases are triggered by pushing a Git tag — CI handles the build, binary compilation, and Homebrew tap update automatically.

Workflow
--------

### 1. Determine the current version

```bash
git fetch --tags origin
git tag --sort=-v:refname | head -5
```

Identify the latest tag. This is the baseline for the next version.

### 2. Analyze changes since the last tag

```bash
git log <latest-tag>..HEAD --oneline
```

Review the commits to classify the change scope:

| Change type | Version bump | Examples |
|---|---|---|
| Breaking / incompatible API changes | **Major** (X.0.0) | Removed a CLI command, changed config schema incompatibly, renamed a public concept |
| New features, capabilities, or commands | **Minor** (0.X.0) | Added a new subcommand, new config option, new integration |
| Bug fixes, docs, internal improvements | **Patch** (0.0.X) | Fixed a crash, corrected a typo, refactored internals with no behavior change |

While the project is pre-1.0, treat minor as the de facto "feature release" and patch as "fix release." Breaking changes during pre-1.0 bump minor, not major.

### 3. Suggest a version — then wait

Based on the change analysis, suggest a version number to the user. Explain your reasoning briefly (e.g., "These are all bug fixes, so I'd suggest a patch bump to v0.7.5").

<confirmation-gate>
Present the suggested version and STOP. Wait for the user to confirm or override the version number. Do not proceed to tagging until the user explicitly approves a specific version.

The user decides the version — your suggestion is advisory. If the user picks a different version, use theirs without pushback.
</confirmation-gate>

### 4. Tag and push

Once the user confirms the version:

```bash
git tag v<confirmed-version>
```

```bash
git push origin v<confirmed-version>
```

### 5. Verify the release

The tag push triggers the GitHub Actions release workflow (`.github/workflows/release.yml`), which runs GoReleaser to build binaries and update the Homebrew tap.

Monitor the workflow until it completes:

```bash
gh run list --workflow=release.yml --limit=1
```

If the run is still in progress, use `gh run watch` to stream logs. The release is not finished until the workflow reports success. If it fails, investigate and resolve the issue before telling the user the release is done.

Do not run GoReleaser locally — CI is the only release path.
