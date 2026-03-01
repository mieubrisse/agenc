## agenc repo add

Add a repository to the repo library

### Synopsis

Add a repository to the repo library by cloning it into $AGENC_DIRPATH/repos/.

Accepts any of these formats:
  repo                                 - shorthand (requires gh auth login)
  owner/repo                           - shorthand (e.g., mieubrisse/agenc)
  github.com/owner/repo                - canonical name
  https://github.com/owner/repo        - HTTPS URL
  git@github.com:owner/repo.git        - SSH URL
  /path/to/local/clone                 - local filesystem path

Tip: Single-word shorthand works automatically if you're logged into gh (gh auth login)

For shorthand formats, the clone protocol (SSH vs HTTPS) is auto-detected
from existing repos in your library. If no repos exist, you'll be prompted
to choose.

Use --always-synced to keep the repo continuously synced by the daemon.
Use --emoji to set an emoji for the repo.

```
agenc repo add <repo> [flags]
```

### Options

```
      --always-synced   keep this repo continuously synced by the daemon
      --emoji string    emoji to display for missions using this repo
  -h, --help            help for add
```

### SEE ALSO

* [agenc repo](agenc_repo.md)	 - Manage the repo library

