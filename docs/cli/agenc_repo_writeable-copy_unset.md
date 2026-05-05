## agenc repo writeable-copy unset

Remove a repo's writeable-copy configuration

### Synopsis

Remove the writeable-copy configuration for a repo. The on-disk clone is
NOT deleted; the user can remove it manually if desired.

The repo can be in any of the formats accepted by 'agenc repo add' — shorthand
('owner/repo'), canonical name ('github.com/owner/repo'), or full URL.

```
agenc repo writeable-copy unset <repo> [flags]
```

### Options

```
  -h, --help   help for unset
```

### SEE ALSO

* [agenc repo writeable-copy](agenc_repo_writeable-copy.md)	 - Manage writeable copies of repos

