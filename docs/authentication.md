Authentication
==============

AgenC authenticates Claude Code missions using an OAuth token stored in a secure local file. You provide the token once, and every mission receives it automatically via the `CLAUDE_CODE_OAUTH_TOKEN` environment variable.

Setting your token
------------------

AgenC automatically obtains a token when needed. If no token is configured, AgenC runs `claude setup-token` to walk you through the authentication flow interactively. This happens during first-time setup (`agenc config init`) and whenever you create or resume a mission without a token.

You can also manage the token manually:

```
agenc config set claudeCodeOAuthToken <your-token>
agenc config get claudeCodeOAuthToken
agenc config set claudeCodeOAuthToken ""
```

How it works
------------

The token is stored at `$AGENC_DIRPATH/cache/oauth-token` with restrictive file permissions (owner-only read/write, mode 600). This file lives outside the config directory and is never committed to Git.

When AgenC spawns a Claude process (interactive or headless), it reads the token file and passes the value as the `CLAUDE_CODE_OAUTH_TOKEN` environment variable. Claude Code uses this token directly for authentication without any Keychain interaction.

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

Token expiry
------------

OAuth tokens expire. When your token expires, Claude sessions will fail to authenticate. To fix this:

1. Obtain a fresh token (see "Where to get your token" above)
2. Run `agenc config set claudeCodeOAuthToken <new-token>`

New missions will use the updated token immediately. Running missions will pick up the new token when they next restart.

MCP OAuth tokens
----------------

MCP servers that use OAuth (like Todoist) store their tokens in Claude's Keychain independently of the main authentication token. These tokens are managed by Claude Code itself and are not affected by the `CLAUDE_CODE_OAUTH_TOKEN` setting.
