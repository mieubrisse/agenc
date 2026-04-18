## agenc repo mv

Rename a repository in the repo library

### Synopsis

Rename a repository in the repo library after a GitHub rename or ownership transfer.

Moves the cloned repo directory, migrates per-repo config (emoji, title,
always-synced, etc.), and updates the clone's origin remote URL.

Does NOT update existing missions that reference the old name.

Accepts any of these formats for both arguments:
  owner/repo                           - shorthand (e.g., mieubrisse/agenc)
  github.com/owner/repo                - canonical name
  https://github.com/owner/repo        - URL
  git@github.com:owner/repo.git        - SSH URL

Example:
  agenc repo mv old-owner/my-repo new-owner/my-repo

```
agenc repo mv <old-name> <new-name> [flags]
```

### Options

```
  -h, --help   help for mv
```

### SEE ALSO

* [agenc repo](agenc_repo.md)	 - Manage the repo library

