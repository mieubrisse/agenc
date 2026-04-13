## agenc mission rebuild

Rebuild the devcontainer for a containerized mission

### Synopsis

Rebuild the devcontainer for a containerized mission.

Stops the current Claude process, tears down and rebuilds the container from
the latest devcontainer.json, then restarts Claude. Only works for missions
whose repository has a devcontainer.json.

Accepts a mission ID (short 8-char hex or full UUID).

```
agenc mission rebuild <mission-id> [flags]
```

### Options

```
  -h, --help   help for rebuild
```

### SEE ALSO

* [agenc mission](agenc_mission.md)	 - Manage agent missions

