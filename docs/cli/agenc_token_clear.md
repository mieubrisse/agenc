## agenc token clear

Remove the stored OAuth token (revert to native Claude authentication)

### Synopsis

Remove the stored OAuth token file.

After clearing, AgenC missions will use Claude's native authentication
(set up via 'claude auth login') rather than an explicit token.

Equivalent to: agenc config set claudeCodeOAuthToken ""

```
agenc token clear [flags]
```

### Options

```
  -h, --help   help for clear
```

### SEE ALSO

* [agenc token](agenc_token.md)	 - Manage the AgenC OAuth token (State-X fallback for headless/multi-session use)

