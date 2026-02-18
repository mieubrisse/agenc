trusted-mcp-servers Design
==========================

**Date**: 2026-02-16
**Status**: Approved
**Related**: `internal/config/agenc_config.go`, `internal/claudeconfig/build.go`, `cmd/config_repo_config_set.go`

Overview
--------

When AgenC creates a mission, Claude Code prompts the user to approve MCP servers from `.mcp.json` every time — even if they were approved in a previous mission. This design adds a `trustedMcpServers` field to `repoConfig` in `config.yml` so users can pre-approve MCP servers on a per-repo basis and skip the prompt.

Root Cause
----------

Claude Code stores MCP consent in `~/.claude.json` under the `projects` map. Each project entry (keyed by directory path) can include:

```json
{
    "hasTrustDialogAccepted": true,
    "enabledMcpjsonServers": [],
    "disabledMcpjsonServers": []
}
```

When `enabledMcpjsonServers` and `disabledMcpjsonServers` are both present (even as empty arrays), Claude Code treats this as "consent recorded." When these fields are absent, Claude Code prompts.

AgenC's `copyAndPatchClaudeJSON` currently writes only `hasTrustDialogAccepted: true` — the MCP consent fields are missing, so Claude Code prompts on every mission.

Configuration Schema
--------------------

Add `trustedMcpServers` to `RepoConfig` in `config.yml`. Supports two formats:

```yaml
repoConfig:
  # Trust all servers in .mcp.json
  github.com/owner/repo1:
    trustedMcpServers: all

  # Trust only specific named servers
  github.com/owner/repo2:
    trustedMcpServers:
      - github
      - sentry
```

No config (field absent) preserves current behavior: Claude Code prompts for consent.

Data Model
----------

Add to `internal/config/agenc_config.go`:

```go
// TrustedMcpServers configures MCP server trust for a repository.
// Supports two formats: "all" (trust every server in .mcp.json) or
// a list of named servers to trust.
type TrustedMcpServers struct {
    All  bool     // true when "all" is specified
    List []string // populated when a list of server names is specified
}
```

`TrustedMcpServers` implements `yaml.Unmarshaler` to handle both formats:
- String `"all"` → `All: true`
- YAML sequence → `List: [...]`
- Empty sequence → validation error (use `all` instead)
- Any other string → validation error

Update `RepoConfig`:

```go
type RepoConfig struct {
    AlwaysSynced      bool               `yaml:"alwaysSynced,omitempty"`
    WindowTitle       string             `yaml:"windowTitle,omitempty"`
    TrustedMcpServers *TrustedMcpServers `yaml:"trustedMcpServers,omitempty"`
}
```

Mission Application
-------------------

### Data flow

```
config.yml RepoConfig.TrustedMcpServers
    ↓ (looked up by caller using mission's git_repo name)
CreateMissionDir / mission_update_config.go
    ↓ (passed as new parameter)
BuildMissionConfigDir(agencDirpath, missionID, trustedMcpServers)
    ↓ (threaded down)
copyAndPatchClaudeJSON(claudeConfigDirpath, agentDirpath, trustedMcpServers)
    ↓ (conditionally adds MCP consent fields)
.claude.json projects entry
```

### Trust entry transformation

| `trustedMcpServers` config | `enabledMcpjsonServers` | `disabledMcpjsonServers` |
|---------------------------|------------------------|--------------------------|
| nil (absent)              | *(field not written)*  | *(field not written)*    |
| `all`                     | `[]`                   | `[]`                     |
| `[github, sentry]`        | `["github", "sentry"]` | `[]`                     |

The nil case preserves current behavior — Claude Code prompts.

### Signature changes

```go
func BuildMissionConfigDir(agencDirpath string, missionID string, trustedMcpServers *config.TrustedMcpServers) error

func copyAndPatchClaudeJSON(claudeConfigDirpath string, missionAgentDirpath string, trustedMcpServers *config.TrustedMcpServers) error
```

### Callers

- `CreateMissionDir` already receives `gitRepoName` — reads AgencConfig, looks up `RepoConfig[gitRepoName].TrustedMcpServers`, passes it to `BuildMissionConfigDir`.
- `mission_update_config.go` fetches the mission from the DB (which has `git_repo`), looks up the same way.

CLI Support
-----------

Add `--trusted-mcp-servers` flag to `agenc config repoConfig set`:

```bash
# Trust all servers
agenc config repoConfig set github.com/owner/repo --trusted-mcp-servers all

# Trust specific servers (comma-separated)
agenc config repoConfig set github.com/owner/repo --trusted-mcp-servers "github,sentry"

# Clear the setting
agenc config repoConfig set github.com/owner/repo --trusted-mcp-servers ""
```

Parsing rules for the flag value:
- `"all"` → `TrustedMcpServers{All: true}`
- `""` → clears the field (sets to nil in the config)
- anything else → split on `,`, trim whitespace → `TrustedMcpServers{List: [...]}`

`agenc config repoConfig ls` shows the current value in the existing table output.

Validation
----------

In `ReadAgencConfig`:
- `"all"` → valid
- Non-empty list of strings → valid (server names are free-form; Claude Code defines their format)
- Empty list `[]` → error: "use `all` to trust all servers, or list at least one server name"
- Any other scalar string → error: "trustedMcpServers must be 'all' or a list of server names"

Testing
-------

- **Unit**: `TrustedMcpServers.UnmarshalYAML` — `"all"`, list, empty list, invalid string
- **Unit**: `copyAndPatchClaudeJSON` — verify `.claude.json` output for nil trust, `all`, and list modes
- **Unit**: `ReadAgencConfig` — valid configs parse cleanly; invalid values return descriptive errors

Files to Modify
---------------

- `internal/config/agenc_config.go` — `TrustedMcpServers` type, `RepoConfig` field, validation
- `internal/claudeconfig/build.go` — `BuildMissionConfigDir`, `copyAndPatchClaudeJSON` signatures and logic
- `internal/mission/mission.go` — `CreateMissionDir` reads AgencConfig, passes trust to `BuildMissionConfigDir`
- `cmd/mission_update_config.go` — reads AgencConfig, passes trust to `BuildMissionConfigDir`
- `cmd/config_repo_config_set.go` — new `--trusted-mcp-servers` flag
- `cmd/config_repo_config.go` — shared flag name constant
- `docs/system-architecture.md` — update `internal/config/` package description
- `README.md` — document `trustedMcpServers` under the Configuration section
