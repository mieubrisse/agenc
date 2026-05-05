## agenc repo writeable-copy

Manage writeable copies of repos

### Synopsis

A writeable copy is an additional clone of a repo at a user-chosen path
(e.g. ~/app/dotfiles) that AgenC keeps continuously synced with the repo's
git remote: local edits are auto-committed and pushed, remote changes are
pulled and rebased. Setting a writeable copy implies that the repo is
always-synced; the implication is enforced by AgenC.

### Options

```
  -h, --help   help for writeable-copy
```

### SEE ALSO

* [agenc repo](agenc_repo.md)	 - Manage the repo library
* [agenc repo writeable-copy ls](agenc_repo_writeable-copy_ls.md)	 - List configured writeable copies and their sync status
* [agenc repo writeable-copy set](agenc_repo_writeable-copy_set.md)	 - Configure a writeable copy for a repo
* [agenc repo writeable-copy unset](agenc_repo_writeable-copy_unset.md)	 - Remove a repo's writeable-copy configuration

