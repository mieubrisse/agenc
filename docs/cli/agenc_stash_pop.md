## agenc stash pop

Restore missions from a stash

### Synopsis

Restore all missions from a previously saved stash. Missions are
re-started and their windows are linked back into the tmux sessions
they were in at the time of the stash.

If there is only one stash, it is restored automatically. If multiple
stashes exist, an interactive picker is shown.

```
agenc stash pop [flags]
```

### Options

```
  -h, --help   help for pop
```

### SEE ALSO

* [agenc stash](agenc_stash.md)	 - Snapshot and restore running missions

