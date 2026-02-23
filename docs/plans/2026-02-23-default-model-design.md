defaultModel Config Key
======================

Summary
-------

Add a `defaultModel` config key at both the top-level `AgencConfig` and per-repo `RepoConfig` level. When set, AgenC passes `--model <value>` to the Claude CLI when spawning missions. When unset, Claude uses its own default.

Precedence chain: **repoConfig.defaultModel > config.defaultModel > (unset, Claude decides)**

Data Model
----------

- `AgencConfig.DefaultModel string` (yaml: `defaultModel`) — top-level default, empty means unset
- `RepoConfig.DefaultModel string` (yaml: `defaultModel`) — per-repo override, empty means unset
- No validation on the value — pass through whatever the user provides so it stays compatible with future Claude model names

Resolution Logic
-----------------

New method on `AgencConfig`:

```go
func (c *AgencConfig) GetDefaultModel(repoName string) string
```

1. If `repoName` is non-empty, check `RepoConfigs[repoName].DefaultModel` — return if non-empty
2. Fall back to `c.DefaultModel` — return if non-empty
3. Return `""` (caller treats as "don't pass --model")

In `BuildClaudeCmd()`: if resolved model is non-empty, append `--model`, `<value>` to the args slice.

CLI Commands
------------

**Top-level:**

- `agenc config get defaultModel` — returns value or "unset"
- `agenc config set defaultModel <value>` — sets top-level default
- `agenc config unset defaultModel` — clears to empty

**Per-repo:**

- `agenc config repo-config set <repo> --default-model <value>` — sets per-repo override
- `agenc config repo-config set <repo> --default-model ""` — clears per-repo override
- `agenc config repo-config ls` — adds a `DEFAULT MODEL` column to the table

Out of Scope
------------

- Mission summarizer — keeps its hardcoded haiku model (different purpose: cost/speed)
- Database schema — model preference is config, not per-mission state
- `BuildMissionConfigDir()` — model goes via CLI flag, not config merging
