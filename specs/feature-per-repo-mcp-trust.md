Per-Repo MCP Server Trust Configuration
========================================

**Created**: 2026-02-12  **Type**: feature  **Status**: Open
**Related**: `internal/claudeconfig/build.go`, `internal/config/agenc_config.go`

---

Description
-----------

When AgenC creates a mission, Claude Code prompts the user to accept MCP servers from `.mcp.json` every time — even though the user has already approved them in previous missions. Users should be able to configure per-repo MCP server trust so missions start without prompts.

Context
-------

### Root Cause

Claude Code stores per-project MCP consent in `~/.claude.json` under the `projects` map. Each project entry (keyed by directory path) contains:

```json
{
    "enabledMcpjsonServers": [],
    "disabledMcpjsonServers": [],
    "hasTrustDialogAccepted": true,
    ...
}
```

When `enabledMcpjsonServers` and `disabledMcpjsonServers` are both present as empty arrays, Claude Code interprets this as "consent recorded — trust all `.mcp.json` servers." When these fields are **absent**, Claude Code treats it as "consent not yet given" and prompts.

AgenC's `copyAndPatchClaudeJSON()` creates a trust entry for each mission's agent directory with only:

```go
trustEntry := map[string]bool{
    "hasTrustDialogAccepted": true,
}
```

This is missing the MCP consent fields, so Claude Code prompts every time.

### Solution

1. Always include `enabledMcpjsonServers` and `disabledMcpjsonServers` in the trust entry
2. Allow users to configure per-repo MCP trust via `config.yml` to control which servers are enabled/disabled

Design Decision
---------------

**Selected: Per-repo configurable MCP trust** (Quality score: 8/10)

Extend `RepoConfig` in `config.yml` with `mcpTrust` settings. When building a mission's `.claude.json`, populate the MCP consent fields based on the repo's configuration. This scopes trust per-repository (least privilege) and follows the existing `RepoConfig` pattern.

Alternative approaches considered:
- **Global trust-all**: Simpler but coarse-grained — security risk if user trusts a server for one repo but not others
- **Auto-parse .mcp.json**: Zero config but auto-trust is a security risk for cloned repos
- **Copy existing trust from user's .claude.json**: Fragile path matching; doesn't help first-time users

User Stories
------------

### Trust all MCP servers for a repo by default

**As a** user running missions against a repo with `.mcp.json`, **I want** MCP servers to be auto-trusted without prompts, **so that** I can start working immediately.

**Test Steps:**

1. **Setup**: Configure a repo with `mcpTrust: all` in `config.yml`. Ensure the repo has a `.mcp.json` with at least one server.
2. **Action**: Create a new mission for this repo.
3. **Assert**: Claude Code starts without prompting for MCP server approval. The mission's `.claude.json` contains `enabledMcpjsonServers: []` and `disabledMcpjsonServers: []` for the agent directory path.

### Selectively enable/disable MCP servers per repo

**As a** security-conscious user, **I want** to enable specific MCP servers and disable others for a given repo, **so that** only approved servers are available.

**Test Steps:**

1. **Setup**: Configure a repo with `mcpTrust: {enabled: ["github"], disabled: ["notion"]}` in `config.yml`.
2. **Action**: Create a new mission for this repo.
3. **Assert**: The mission's `.claude.json` contains `enabledMcpjsonServers: ["github"]` and `disabledMcpjsonServers: ["notion"]` for the agent directory path.

### Default behavior without config

**As a** user who hasn't configured `mcpTrust`, **I want** the current behavior preserved (Claude Code prompts for consent), **so that** nothing changes unexpectedly.

**Test Steps:**

1. **Setup**: A repo with no `mcpTrust` in `config.yml` and a `.mcp.json` present.
2. **Action**: Create a new mission for this repo.
3. **Assert**: The mission's `.claude.json` trust entry does NOT include `enabledMcpjsonServers` or `disabledMcpjsonServers`. Claude Code prompts as before.

Implementation Plan
-------------------

### Phase 1: Add MCP trust to RepoConfig

- [ ] Add `McpTrust` field to `RepoConfig` struct in `internal/config/agenc_config.go`
  - String field: `"all"` (trust everything), `""` (default, don't set), or struct for selective trust
  - Use a flexible type that supports both the simple `all` shorthand and the `{enabled: [...], disabled: [...]}` form
- [ ] Add validation in `ReadAgencConfig` for the new field
- [ ] Add `mcpTrust` to `config get/set` support in `cmd/config_get.go` and `cmd/config_set.go`

### Phase 2: Inject MCP trust into mission .claude.json

- [ ] Modify `copyAndPatchClaudeJSON()` in `internal/claudeconfig/build.go`:
  - Accept the repo's MCP trust config
  - When trust is configured, include `enabledMcpjsonServers` and `disabledMcpjsonServers` in the project entry
  - When trust is `"all"`, set both to empty arrays `[]`
  - When trust specifies enabled/disabled lists, populate accordingly
- [ ] Thread the MCP trust config from `BuildMissionConfigDir` down to `copyAndPatchClaudeJSON`
  - This requires knowing which repo the mission is for — use the mission's git remote or the repo config lookup

### Phase 3: Wire up repo identification

- [ ] In `BuildMissionConfigDir`, determine which repo the mission targets (from the mission's agent directory git remote or mission metadata)
- [ ] Look up the repo's `McpTrust` from `AgencConfig`
- [ ] Pass it to `copyAndPatchClaudeJSON`

Technical Details
-----------------

### Config schema

```yaml
repoConfig:
  github.com/owner/repo:
    alwaysSynced: true
    windowTitle: "my-repo"
    mcpTrust: all           # Trust all .mcp.json servers

  github.com/other/repo:
    mcpTrust:               # Selective trust
      enabled:
        - github
        - sentry
      disabled:
        - notion
```

### Modules to modify

- `internal/config/agenc_config.go` — `RepoConfig` struct, validation
- `internal/claudeconfig/build.go` — `copyAndPatchClaudeJSON()`, `BuildMissionConfigDir()`
- `cmd/config_get.go` / `cmd/config_set.go` — CLI support for the new key

### Key changes

The trust entry in `copyAndPatchClaudeJSON` changes from:

```go
trustEntry := map[string]bool{
    "hasTrustDialogAccepted": true,
}
```

To (when mcpTrust is configured):

```go
trustEntry := map[string]interface{}{
    "hasTrustDialogAccepted":  true,
    "enabledMcpjsonServers":   enabledServers,  // []string
    "disabledMcpjsonServers":  disabledServers, // []string
}
```

### Repo identification

The mission's agent directory is a git repo clone. To look up the `RepoConfig`, resolve the git remote to a canonical repo name (e.g., `github.com/owner/repo`). This logic already exists in the codebase for repo library management.

Testing Strategy
----------------

- **Unit tests**: Test `copyAndPatchClaudeJSON` with different MCP trust configs (nil, "all", selective). Verify the output `.claude.json` contains the expected fields.
- **Unit tests**: Test `RepoConfig` validation — valid `mcpTrust` values, invalid values rejected.
- **Integration tests**: Test `BuildMissionConfigDir` end-to-end with a mock repo config and verify the `.claude.json` output.

Acceptance Criteria
-------------------

- [ ] `mcpTrust: all` in `repoConfig` results in missions that don't prompt for MCP server approval
- [ ] Selective `mcpTrust` with `enabled`/`disabled` lists correctly populates the `.claude.json` fields
- [ ] No `mcpTrust` config preserves current behavior (Claude Code prompts)
- [ ] `agenc config get/set` can read and write the `mcpTrust` value for repos
- [ ] Architecture doc updated if package responsibilities change

Risks & Considerations
----------------------

- **Claude Code schema stability**: The `enabledMcpjsonServers` / `disabledMcpjsonServers` fields are observed from Claude Code's runtime behavior, not from official documentation. If Claude Code changes this schema, the feature will break silently (missions will prompt again). The impact is graceful degradation — the feature stops working but nothing breaks.
- **Repo identification**: The mission must be able to resolve its git remote to a canonical repo name to look up the config. If the mission isn't backed by a known repo, MCP trust config won't apply.
- **Security**: Users explicitly opt in per repo. There is no global "trust everything" setting, and the default (no config) preserves the current prompting behavior.
