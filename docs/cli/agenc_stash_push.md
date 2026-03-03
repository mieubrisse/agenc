## agenc stash push

Snapshot running missions and stop them

### Synopsis

Snapshot all running missions — recording which tmux sessions they are
linked into — then stop them. The snapshot is saved to a stash file that
can be restored later with 'agenc stash pop'.

If any missions are actively busy (not idle), you will be warned before
proceeding. Use --force to skip the warning.

```
agenc stash push [flags]
```

### Options

```
      --force   skip warning for non-idle missions
  -h, --help    help for push
```

### SEE ALSO

* [agenc stash](agenc_stash.md)	 - Snapshot and restore running missions

