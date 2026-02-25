## agenc tmux rm

Destroy the AgenC tmux session, stopping all running missions

### Synopsis

Destroy the AgenC tmux session. Killing the session sends SIGHUP to all
wrapper processes, which triggers graceful shutdown (forwarding to Claude,
waiting for exit, cleaning up PID files).

```
agenc tmux rm [flags]
```

### Options

```
  -h, --help   help for rm
```

### SEE ALSO

* [agenc tmux](agenc_tmux.md)	 - Manage the AgenC tmux session

