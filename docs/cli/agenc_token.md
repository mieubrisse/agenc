## agenc token

Manage the AgenC OAuth token (State-X fallback for headless/multi-session use)

### Synopsis

Manage the AgenC OAuth token.

AgenC missions default to using your native Claude Code authentication
(via 'claude auth login'). The OAuth token is an opt-in fallback for
headless or multi-session workflows where native auth is unavailable or
causes refresh thrashing.

When a token is configured, AgenC passes it to Claude via the
CLAUDE_CODE_OAUTH_TOKEN environment variable. When no token is set,
Claude uses its own native authentication.

Subcommands:
  agenc token set <token>   Store a long-lived OAuth token
  agenc token clear         Remove the stored token (revert to native auth)
  agenc token setup         Interactive wizard to obtain and store a token

### Options

```
  -h, --help   help for token
```

### SEE ALSO

* [agenc](agenc.md)	 - The AgenC — agent mission management CLI
* [agenc token clear](agenc_token_clear.md)	 - Remove the stored OAuth token (revert to native Claude authentication)
* [agenc token set](agenc_token_set.md)	 - Store a long-lived Claude Code OAuth token
* [agenc token setup](agenc_token_setup.md)	 - Interactive wizard to obtain and store a long-lived OAuth token

