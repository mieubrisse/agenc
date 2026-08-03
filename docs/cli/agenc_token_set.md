## agenc token set

Store a long-lived Claude Code OAuth token

### Synopsis

Store a long-lived Claude Code OAuth token for use by AgenC missions.

The token must start with "sk-ant-". It is stored at
$AGENC_DIRPATH/cache/oauth-token with mode 600 (owner-only read/write)
and is never committed to Git.

When a token is set, all new missions receive it via CLAUDE_CODE_OAUTH_TOKEN.
Running missions pick it up on their next restart.

To obtain a long-lived token interactively, run: agenc token setup

You can also manage the token via the config alias:
  agenc config set claudeCodeOAuthToken <token>
  agenc config get claudeCodeOAuthToken

```
agenc token set <token> [flags]
```

### Options

```
  -h, --help   help for set
```

### SEE ALSO

* [agenc token](agenc_token.md)	 - Manage the long-lived OAuth token (opt-in fallback for headless or multi-session use)

