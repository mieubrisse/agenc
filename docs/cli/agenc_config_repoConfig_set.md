## agenc config repoConfig set

Set per-repo configuration

### Synopsis

Set or update configuration for a repository.

The repo must be specified in canonical format (github.com/owner/repo).
At least one flag must be provided.

Examples:
  agenc config repoConfig set github.com/owner/repo --always-synced=true
  agenc config repoConfig set github.com/owner/repo --emoji="🔥"
  agenc config repoConfig set github.com/owner/repo --always-synced=true --emoji="🔥"
  agenc config repoConfig set github.com/owner/repo --post-update-hook="make setup"


```
agenc config repoConfig set <repo> [flags]
```

### Options

```
      --always-synced                keep this repo continuously synced by the server
      --claude-args string           extra Claude CLI args: comma-separated (e.g., "--chrome,--verbose"); empty to clear
      --default-model string         default Claude model for missions using this repo (e.g., "opus", "sonnet")
      --emoji string                 emoji to display for missions using this repo
  -h, --help                         help for set
      --post-update-hook string      shell command to run after repo updates (e.g., "make setup"); empty to clear
      --title string                 friendly title for the repo (e.g., "Dotfiles")
      --trusted-mcp-servers string   MCP server trust: "all", comma-separated server names, or "" to clear
```

### SEE ALSO

* [agenc config repoConfig](agenc_config_repoConfig.md)	 - Manage per-repo configuration

