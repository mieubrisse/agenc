Distributing Go Binaries via apt Using GoReleaser
==================================================

Research date: 2026-03-30

This document covers how to produce `.deb` packages with GoReleaser and host them
in an apt repository that Ubuntu/Debian users can install from.

Step 1: Producing .deb Packages with nFPM
------------------------------------------

GoReleaser uses nFPM to produce `.deb` (and `.rpm`, `.apk`) packages. This is
configured in the `nfpms` section of `.goreleaser.yaml`. nFPM is included in
GoReleaser OSS -- no Pro license needed for package generation.

### Minimal .deb Configuration

```yaml
version: 2

nfpms:
  - id: packages
    package_name: myapp
    file_name_template: "{{ .ConventionalFileName }}"
    vendor: My Company
    homepage: https://github.com/myorg/myapp
    maintainer: Your Name <you@example.com>
    description: A short description of what myapp does.
    license: MIT
    formats:
      - deb
    bindir: /usr/bin
    section: utils
    priority: optional

    # Dependencies
    dependencies:
      - libc6

    # Additional files to include in the package
    contents:
      - src: ./completions/myapp.bash
        dst: /usr/share/bash-completion/completions/myapp
        file_info:
          mode: 0644
      - src: ./completions/myapp.zsh
        dst: /usr/share/zsh/site-functions/_myapp
        file_info:
          mode: 0644
      - src: ./completions/myapp.fish
        dst: /usr/share/fish/vendor_completions.d/myapp.fish
        file_info:
          mode: 0644
      - src: ./manpages/myapp.1.gz
        dst: /usr/share/man/man1/myapp.1.gz
        file_info:
          mode: 0644
      - src: ./LICENSE
        dst: /usr/share/doc/myapp/copyright
        file_info:
          mode: 0644

    # Install/remove scripts
    scripts:
      postinstall: scripts/postinstall.sh
      preremove: scripts/preremove.sh

    # Debian-specific settings
    deb:
      lintian_overrides:
        - statically-linked-binary
        - changelog-file-missing-in-native-package
      compression: zstd  # options: gzip (default), xz, zstd, none
```

### Debian-Specific Options

The `deb` subsection supports:

- `lintian_overrides` -- suppress specific Debian linting warnings
- `compression` -- `gzip` (default), `xz`, `zstd`, or `none`
- `scripts.rules` -- custom Debian rules script
- `scripts.templates` -- debconf templates
- `triggers.interest` / `triggers.activate` -- dpkg trigger support
- `breaks` -- packages this one breaks
- `predepends` -- pre-dependencies
- `signature.key_file` / `signature.type` -- GPG signing
- `fields` -- arbitrary control file fields (e.g., `Bugs:`)

### Per-Format Dependency Overrides

```yaml
nfpms:
  - dependencies:
      - git
    overrides:
      deb:
        dependencies:
          - git (>= 2.0)
          - libc6
      rpm:
        dependencies:
          - git >= 2.0
```

### GPG Signing

```yaml
nfpms:
  - deb:
      signature:
        key_file: "{{ .Env.GPG_KEY_PATH }}"
        type: origin  # origin, maint, or archive
```

Passphrase resolution order:
1. `$NFPM_<ID>_DEB_PASSPHRASE`
2. `$NFPM_<ID>_PASSPHRASE`
3. `$NFPM_PASSPHRASE`


Step 2: Hosting the apt Repository
-----------------------------------

Producing .deb files is only half the problem. Users need an apt repository they
can add to their system. There are several approaches, each with different
tradeoffs.

### Approach 1: GitHub Releases Only (No apt repo)

**Complexity:** Lowest
**Cost:** Free
**GoReleaser edition:** OSS

The simplest approach: attach .deb files to GitHub Releases. Users download and
install manually. No apt repository involved.

**GoReleaser config:**

```yaml
version: 2

nfpms:
  - id: packages
    formats:
      - deb
    # ... (nfpm config as above)

release:
  github:
    owner: myorg
    name: myapp
```

nFPM artifacts are automatically attached to the GitHub Release.

**User install experience:**

```bash
# One-time install (no auto-updates)
curl -LO https://github.com/myorg/myapp/releases/latest/download/myapp_1.0.0_amd64.deb
sudo dpkg -i myapp_1.0.0_amd64.deb
```

**Pros:** Zero infrastructure, free, works immediately.
**Cons:** No `apt update` / auto-upgrade support. Users must manually download
new versions.


### Approach 2: Gemfury (Fury.io)

**Complexity:** Low
**Cost:** Free tier available (1 private repo, unlimited public)
**GoReleaser edition:** Pro (for built-in `furies` publisher); OSS with custom publisher

This is what GoReleaser itself uses (via GoReleaser Pro) and what Charmbracelet
uses for their tools (gum, bubbletea CLI tools, etc.).

**GoReleaser Pro config:**

```yaml
version: 2

nfpms:
  - id: packages
    formats:
      - deb
    # ... (nfpm config as above)

furies:
  - account: myaccount
    secret_name: FURY_TOKEN  # env var containing your push token
    ids:
      - packages
    formats:
      - deb
```

**GoReleaser OSS alternative (custom publisher):**

```yaml
version: 2

nfpms:
  - id: packages
    formats:
      - deb

publishers:
  - name: fury
    ids:
      - packages
    cmd: >-
      curl -sf -F package=@{{ .ArtifactPath }}
      https://{{ .Env.FURY_PUSH_TOKEN }}@push.fury.io/myaccount/
    env:
      - FURY_PUSH_TOKEN={{ .Env.FURY_PUSH_TOKEN }}
```

**User install experience (public repo):**

```bash
# Add GPG key
curl -fsSL https://apt.fury.io/myaccount/gpg.key \
  | gpg --dearmor \
  | sudo tee /usr/share/keyrings/myaccount-fury.gpg > /dev/null

# Add apt source
echo "deb [signed-by=/usr/share/keyrings/myaccount-fury.gpg] https://apt.fury.io/myaccount/ * *" \
  | sudo tee /etc/apt/sources.list.d/myaccount.list

# Install
sudo apt update
sudo apt install myapp
```

**User install experience (simplified, trusting Fury):**

```bash
echo "deb [trusted=yes] https://apt.fury.io/myaccount/ * *" \
  | sudo tee /etc/apt/sources.list.d/myaccount.list
sudo apt update
sudo apt install myapp
```

**Pros:** Minimal setup, GoReleaser has built-in support (Pro), Gemfury handles
repo metadata generation, supports versioning and upgrades via `apt update`.
**Cons:** Pro feature for native integration (though OSS can use curl publisher).
Free tier is limited. Fury is a third-party dependency.


### Approach 3: Packagecloud

**Complexity:** Low-Medium
**Cost:** Free tier (1 repo, limited bandwidth)
**GoReleaser edition:** OSS (via custom publisher)

Packagecloud is a hosted package repository service. GoReleaser does not have a
built-in Packagecloud publisher, but you can use a custom publisher.

**GoReleaser config:**

```yaml
version: 2

nfpms:
  - id: packages
    formats:
      - deb

publishers:
  - name: packagecloud
    ids:
      - packages
    cmd: >-
      package_cloud push myorg/myrepo/any/any {{ .ArtifactPath }}
    env:
      - PACKAGECLOUD_TOKEN={{ .Env.PACKAGECLOUD_TOKEN }}
```

Requires the `package_cloud` CLI gem to be installed in your CI environment.

**User install experience:**

```bash
# Using Packagecloud's install script
curl -s https://packagecloud.io/install/repositories/myorg/myrepo/script.deb.sh | sudo bash
sudo apt install myapp
```

Or manually:

```bash
# Add GPG key
curl -fsSL https://packagecloud.io/myorg/myrepo/gpgkey \
  | gpg --dearmor \
  | sudo tee /usr/share/keyrings/myorg-myrepo.gpg > /dev/null

# Add apt source
echo "deb [signed-by=/usr/share/keyrings/myorg-myrepo.gpg] https://packagecloud.io/myorg/myrepo/any/ any main" \
  | sudo tee /etc/apt/sources.list.d/myorg-myrepo.list

sudo apt update
sudo apt install myapp
```

**Pros:** Well-known service, handles all repo metadata, provides install scripts.
**Cons:** Requires `package_cloud` CLI. Free tier limits apply.


### Approach 4: Cloudsmith

**Complexity:** Low
**Cost:** Free tier (1 repo, 1GB storage)
**GoReleaser edition:** Pro (for built-in `cloudsmiths` publisher)

**GoReleaser Pro config:**

```yaml
version: 2

nfpms:
  - id: packages
    formats:
      - deb

cloudsmiths:
  - organization: myorg
    repository: myrepo
    distributions:
      deb:
        - "ubuntu/focal"
        - "ubuntu/jammy"
        - "ubuntu/noble"
        - "any-distro/any-version"
```

Requires `CLOUDSMITH_TOKEN` environment variable.

**User install experience:**

```bash
# Cloudsmith provides a setup script
curl -1sLf 'https://dl.cloudsmith.io/public/myorg/myrepo/setup.deb.sh' | sudo bash
sudo apt install myapp
```

**Pros:** Built-in GoReleaser Pro support. Multi-distro targeting. Handles
all repository metadata. Good free tier.
**Cons:** Pro only for native integration. Third-party dependency.


### Approach 5: Self-Hosted apt Repo on GitHub Pages

**Complexity:** Medium
**Cost:** Free
**GoReleaser edition:** OSS

Host the apt repository as a static site on GitHub Pages. You generate the repo
metadata yourself (using `dpkg-scanpackages` and `apt-ftparchive`) and publish
the result.

**GoReleaser config (generate packages):**

```yaml
version: 2

nfpms:
  - id: packages
    formats:
      - deb
```

**GitHub Actions workflow (post-release, publish to apt repo):**

```yaml
# In a separate repository (e.g., myorg/apt-repo)
name: Update apt repository
on:
  workflow_dispatch:
  repository_dispatch:
    types: [new-release]

jobs:
  update-repo:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Download .deb from latest release
        run: |
          gh release download --repo myorg/myapp --pattern "*.deb" -D debs/
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}

      - name: Generate repo metadata
        run: |
          cd debs/
          dpkg-scanpackages --multiversion . > Packages
          gzip -k -f Packages
          apt-ftparchive release . > Release

      - name: Sign release
        run: |
          echo "${{ secrets.GPG_PRIVATE_KEY }}" | gpg --import
          cd debs/
          gpg --default-key "you@example.com" -abs -o Release.gpg Release
          gpg --default-key "you@example.com" --clearsign -o InRelease Release

      - name: Push to GitHub Pages
        uses: peaceiris/actions-gh-pages@v3
        with:
          github_token: ${{ secrets.GITHUB_TOKEN }}
          publish_dir: ./debs
```

**User install experience:**

```bash
# Add GPG key
curl -fsSL https://myorg.github.io/apt-repo/KEY.gpg \
  | gpg --dearmor \
  | sudo tee /usr/share/keyrings/myorg.gpg > /dev/null

# Add apt source
echo "deb [signed-by=/usr/share/keyrings/myorg.gpg] https://myorg.github.io/apt-repo ./" \
  | sudo tee /etc/apt/sources.list.d/myorg.list

sudo apt update
sudo apt install myapp
```

**Pros:** Completely free. No third-party dependencies. Full control.
**Cons:** More setup work. Must manage GPG keys. Must rebuild repo metadata on
each release. Not a standard GoReleaser feature -- requires external automation.


### Approach 6: Self-Hosted with aptly

**Complexity:** Medium-High
**Cost:** Free (you provide the hosting)
**GoReleaser edition:** OSS (via custom publisher)

[aptly](https://github.com/aptly-dev/aptly) is a full-featured Debian
repository manager. You run it on your own server (or in CI) and serve the
resulting repo via any static file host (S3, nginx, etc.).

**GoReleaser config:**

```yaml
version: 2

nfpms:
  - id: packages
    formats:
      - deb

publishers:
  - name: aptly
    ids:
      - packages
    cmd: >-
      aptly repo add myrepo {{ .ArtifactPath }}
```

Then separately run `aptly publish` to generate and serve the repo.

**Pros:** Full control. Supports complex repo layouts (components, distributions,
snapshots). Well-established tool.
**Cons:** Significant operational overhead. You manage the server, GPG keys,
and publishing pipeline.


### Approach 7: S3/GCS + GoReleaser Blob Publisher

**Complexity:** Medium
**Cost:** Low (cloud storage costs)
**GoReleaser edition:** OSS (blob publisher is OSS)

Upload .deb files and repo metadata to cloud object storage. Serve via
CloudFront/Cloud CDN or directly from the bucket.

This is likely how GoReleaser serves `repo.goreleaser.com/apt/` -- they use
Gemfury for package hosting but the pattern of blob + CDN is common for
high-traffic repos.

**GoReleaser config:**

```yaml
version: 2

nfpms:
  - id: packages
    formats:
      - deb

blobs:
  - provider: s3
    bucket: my-apt-repo
    region: us-east-1
    ids:
      - packages
```

This uploads the .deb files. Generating the apt repo metadata (Packages,
Release, InRelease) requires a separate step -- either aptly, reprepro, or
custom scripting in CI.


Comparison Summary
------------------

| Approach | Cost | GoReleaser Edition | Setup Effort | Auto-Updates | Maintenance |
|---|---|---|---|---|---|
| GitHub Releases only | Free | OSS | None | No | None |
| Gemfury | Free tier / Paid | Pro (OSS via curl) | Low | Yes | Low |
| Packagecloud | Free tier / Paid | OSS | Low | Yes | Low |
| Cloudsmith | Free tier / Paid | Pro | Low | Yes | Low |
| GitHub Pages apt repo | Free | OSS | Medium | Yes | Medium |
| aptly self-hosted | Free + hosting | OSS | High | Yes | High |
| S3/GCS blob | Low | OSS | Medium | Yes | Medium |


Recommendation
--------------

For most Go projects using GoReleaser:

1. **If you have GoReleaser Pro:** Use the built-in `furies` publisher with
   Gemfury. This is what GoReleaser and Charmbracelet both use. Minimal config,
   Gemfury handles all apt repo metadata.

2. **If you use GoReleaser OSS and want simplicity:** Use Gemfury with a custom
   `publishers` entry (curl-based upload). Same end result, slightly more config.

3. **If you want zero cost and full control:** Host a static apt repo on GitHub
   Pages. More CI work upfront but no third-party dependencies.

4. **If you just need one-off installs:** Attach .deb files to GitHub Releases.
   Users can `curl` + `dpkg -i`. No apt repo overhead.


Real-World Examples
-------------------

**GoReleaser itself:**
- Uses Gemfury via GoReleaser Pro's `furies` publisher
- User install: `echo 'deb [trusted=yes] https://repo.goreleaser.com/apt/ /' | sudo tee /etc/apt/sources.list.d/goreleaser.list`
- Note: `repo.goreleaser.com` likely fronts Gemfury or a custom CDN

**Charmbracelet (gum, bubbletea tools):**
- Uses Gemfury via `furies` publisher with `FURY_TOKEN`
- Account: `charmcli`

Both of these are GoReleaser Pro users. For OSS, the GitHub Pages approach or
the curl-to-Gemfury custom publisher are the most common patterns.
