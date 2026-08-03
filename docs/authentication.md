Authentication
==============

AgenC missions authenticate with Claude Code using one of two mechanisms:

1. **Native authentication (default)** — Claude's own credentials, set up once
   with `claude auth login`. AgenC passes these through automatically; no token
   file is needed.

2. **Explicit OAuth token (opt-in fallback)** — a long-lived token you store via
   `agenc token set`. AgenC passes it as `CLAUDE_CODE_OAUTH_TOKEN`. Useful for
   headless or multi-session workflows where native auth refresh thrashing is a
   problem ([GitHub issue](https://github.com/anthropics/claude-code/issues/24317)).

Most users only need to run `claude auth login` once and never think about tokens.

Setting up native authentication
---------------------------------

```
claude auth login
```

This stores credentials in your macOS Keychain. All AgenC missions pick them up
automatically — no extra configuration required.

Managing the explicit OAuth token (optional)
---------------------------------------------

The `agenc token` command manages the State-X fallback token:

```
agenc token set <token>   Store a long-lived token (must start with sk-ant-)
agenc token clear         Remove the token (revert to native auth)
agenc token setup         Interactive wizard: runs 'claude setup-token' for you
```

The token is stored at `$AGENC_DIRPATH/cache/oauth-token` with mode 600 (never
committed to Git). When set, it overrides native auth for all missions. When
cleared, missions fall back to native Claude authentication.

The legacy config alias still works:

```
agenc config set claudeCodeOAuthToken <token>
agenc config get claudeCodeOAuthToken
agenc config set claudeCodeOAuthToken ""
```

How token passthrough works
----------------------------

When a token file is present and non-empty, the wrapper reads it and passes it
to Claude via the `CLAUDE_CODE_OAUTH_TOKEN` environment variable:

```
Token file (cache/oauth-token)
        │
        ▼
   Wrapper reads file
        │
        ▼
   CLAUDE_CODE_OAUTH_TOKEN env var
        │
        ▼
   Claude Code authenticates
```

When no token file exists, the wrapper omits the environment variable and Claude
uses its native Keychain credentials.

Headless missions
-----------------

Headless missions (spawned by cron or `--headless` flags) work the same way:
if a token is configured it is injected; otherwise native auth is used. If no
credentials are available at all, Claude itself will surface a clear auth error.

Token expiry
------------

Explicit OAuth tokens expire. When yours expires, Claude sessions will fail to
authenticate. To fix this:

1. Obtain a fresh token: `agenc token setup`
2. Or set it directly: `agenc token set <new-token>`

Running missions pick up the new token on their next restart.

MCP OAuth tokens
----------------

MCP servers that use OAuth (like Todoist) store their tokens in Claude's Keychain
independently of the main authentication token. These are managed by Claude Code
itself and are not affected by `agenc token` or `CLAUDE_CODE_OAUTH_TOKEN`.
