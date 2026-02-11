1Password Secret Injection
==========================

Repos often need secrets — API tokens, database credentials, etc. AgenC integrates with [1Password CLI](https://developer.1password.com/docs/cli/) (`op`) to inject secrets at runtime without storing them on disk.

Setup
-----

Create a `.claude/secrets.env` file in your repo. Each line maps an environment variable to a [1Password secret reference](https://developer.1password.com/docs/cli/secret-references/):

```
NOTION_TOKEN=op://Personal/Notion API Token/credential
TODOIST_API_KEY=op://Personal/Todoist/api_key
```

When AgenC detects this file, it automatically wraps the Claude invocation with `op run`, which resolves secret references and injects the values as environment variables.

Example: MCP servers with secrets
----------------------------------

This is particularly useful for MCP servers that need API tokens. For example, a `.mcp.json` that connects to Todoist and a custom Notion server:

```json
{
    "mcpServers": {
        "todoist": {
            "type": "http",
            "url": "https://ai.todoist.net/mcp"
        },
        "notion": {
            "command": "npx",
            "args": [
                "-y",
                "@mieubrisse/notion-mcp-server"
            ],
            "env": {
                "NOTION_TOKEN": "${NOTION_TOKEN}"
            }
        }
    }
}
```

The `${NOTION_TOKEN}` reference is resolved from the environment, which `op run` populates from your `.claude/secrets.env`. The secret never touches disk — it flows from 1Password to environment to MCP server process.

Requirements
------------

- [1Password CLI](https://developer.1password.com/docs/cli/) (`op`) must be installed and in your PATH
- You must be signed in to 1Password (`op signin`)
- The `.claude/secrets.env` file is only needed in the repo; AgenC handles the rest

If `.claude/secrets.env` does not exist, AgenC launches Claude directly with no `op` dependency.
