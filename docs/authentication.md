Authentication
==============

Each mission runs Claude with its own isolated config directory (`CLAUDE_CONFIG_DIR`). This means each mission gets its own macOS Keychain entry for credentials, separate from the global `Claude Code-credentials` entry that Claude Code uses by default.

AgenC handles the plumbing so you rarely need to think about this — but it helps to understand what's happening.

How credentials flow
--------------------

When you create a mission, AgenC clones the global Keychain credentials into a per-mission Keychain entry (named `Claude Code-credentials-<hash>`, where the hash is derived from the mission's config directory path). Claude inside the mission reads from that per-mission entry instead of the global one.

When the mission's Claude process exits, AgenC merges any new credentials back into the global entry. This means if Claude acquires an OAuth token during a mission (e.g. authenticating with an MCP server like Todoist), that token propagates to the global entry and becomes available to every future mission — no re-authentication needed.

The merge is per-server and timestamp-aware: for each MCP server, the token with the newest `expiresAt` wins. Tokens that exist only on one side are always preserved.

```
Global Keychain ──clone──▶ Per-mission Keychain
       ▲                          │
       │                     Claude runs,
       │                     may acquire
       │                     MCP OAuth tokens
       │                          │
       └────merge back────────────┘
            (on exit)
```

`agenc login`
-------------

Run `agenc login` when:

- **First-time setup** — you haven't run `claude login` yet, or you're setting up a new machine
- **Credentials expired** — Claude sessions fail to authenticate
- **After re-authenticating with Claude** — so existing missions pick up the new credentials

The command opens an interactive Claude shell where you run `/login`, authorize in the browser, and exit. AgenC then:

1. Preserves any MCP OAuth tokens from the previous credentials (since `claude login` overwrites the global Keychain entry with a fresh blob that only contains the Claude auth token)
2. Propagates the updated credentials — with MCP tokens intact — to all existing missions

```
agenc login
```

You do **not** need to run `agenc login` for MCP OAuth tokens — those propagate automatically when you authenticate inside any mission. And if you do run `agenc login`, your existing MCP tokens are preserved.

MCP OAuth tokens
----------------

MCP servers that use OAuth (like Todoist) prompt for authentication the first time you use them. Once you authenticate inside any mission:

1. The OAuth token is stored in that mission's Keychain entry
2. When the Claude process exits, AgenC merges the token back to the global entry
3. The next mission you create inherits the token — no re-auth prompt

If a token expires and Claude refreshes it during a mission, the refreshed token (with a newer `expiresAt`) replaces the stale one in the global entry on exit.
