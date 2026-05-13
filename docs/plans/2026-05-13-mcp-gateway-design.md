AgenC MCP Gateway: Design
=========================

Design for a Go-native AgenC subsystem that runs MCP servers as persistent singletons inside the AgenC daemon and exposes them to missions via stdio shims over the AgenC Unix socket. Successor to the 2026-03-18 supergateway research; follow-up to bead `agenc-tpde`. Design discussion in mission `207a2561`, session `400f310e`.

Problem
-------

Every new AgenC mission cold-starts the MCP servers it depends on. When those servers need secrets (typical case), each cold start invokes `op` for biometric resolution, producing a 1Password Touch ID prompt per mission per server. Across a day of mission spawning, this is dozens of prompts and seconds of latency each. The persistence problem and the secrets-prompt problem are the same problem: any architecture that keeps the MCP server process alive across missions solves both.

The 2026-03-18 plan covered the off-the-shelf landscape (supergateway, mcp-proxy variants). This document covers the design AgenC will build natively.

Architecture summary
--------------------

- One **singleton stdio backend process per named MCP server**, supervised by the AgenC daemon. All missions share these singletons via a routing layer.
- **stdio shim over Unix socket**, not exposed HTTP. AgenC injects `{"command": "agenc", "args": ["mcp", "client", "<name>"]}` entries into each mission's `.mcp.json`. The `agenc mcp client` subcommand pipes bytes between the mission's stdin/stdout and a connection to the AgenC daemon over its existing Unix socket. The mission sees an ordinary stdio MCP server; the actual backend is a singleton in the daemon.
- **Per-backend mutex** by default. Many MCP servers aren't tested for concurrent JSON-RPC calls (e.g., supergateway issue #35). AgenC owns the gateway — exposes a per-backend `serialize` knob, default-on. Required correctness for multi-subagent workflows where parallel agents make concurrent tool calls against the same backend.
- **Daemon never holds plaintext secrets.** Each backend spawn invokes `op run --` (or `op inject` for batches). Plaintext flows: `op` -> backend env via `execve` -> backend process memory. AgenC's Go code holds only the `!op` reference. Trade-off: backend crash respawn re-invokes `op run --`, possibly another Touch ID prompt if outside the 10-minute idle window. Accept the edge case.
- **Daemon stays detached** (`Setsid: true` in `internal/server/process.go:ForkServer` unchanged). Daemon survives terminal close; supervised backends survive daemon crashes via the daemon's existing supervisor pattern.
- **`OP_CACHE=false` on all `op` invocations** to dodge the macOS Tahoe (26.3.1+) `op daemon --background` TCC hang ([reported issue](https://github.com/openclaw/openclaw/issues/55459)).

Config schema (illustrative; UX pass needed)
--------------------------------------------

```yaml
mcp_servers:
  notion:
    type: stdio
    command: ["node", "/path/to/notion-mcp-server"]
    env:
      NOTION_TOKEN: !op "op://Personal/Notion/integration-token"
    serialize: true   # default; per-backend mutex

  todoist:
    type: stdio
    command: ["uvx", "mcp-server-todoist"]
    env:
      TODOIST_API_TOKEN: !op "op://Personal/Todoist/api-token"
```

Per-mission opt-out (probably needed): `.agenc/mission.yml` with `mcp.exclude: [grain]` or similar. Default = all configured backends available to all missions.

Mission `.mcp.json` injection (AgenC writes this on mission spawn):

```json
{
  "mcpServers": {
    "notion":  { "command": "agenc", "args": ["mcp", "client", "notion"] },
    "todoist": { "command": "agenc", "args": ["mcp", "client", "todoist"] }
  }
}
```

Components
----------

| Component | Responsibility | Approximate size |
|-----------|---------------|------------------|
| Config loader | Parse `mcp_servers` section, resolve `!op` references at backend-spawn time | Small |
| Backend supervisor | Spawn each backend via `op run --`, hold stdin/stdout pipes, restart on crash with exponential backoff | Medium |
| Per-backend mutex | Serialize calls per backend; opt-out per-server | Small |
| Unix-socket gateway | Accept connections from `agenc mcp client` shims; route to the named backend; multiplex JSON-RPC by request ID | Medium-large |
| `agenc mcp client <name>` subcommand | Bidirectional pipe between mission stdin/stdout and a Unix-socket connection to the daemon | Small (100-200 LOC) |
| Mission-spawn config injection | Write the `.mcp.json` entries into the mission's claude-config at spawn | Small |
| `agenc mcp` CLI surface | `add`/`remove`/`status`/`restart`/`reload`/`shim` | Medium (UX pass scopes this) |

Rough total: 1500-2500 LOC Go for Phase 1. Estimate is approximate.

1Password TTY-session constraint
---------------------------------

`op` keys its biometric session by `TTY + start_time` on macOS/Linux ([1Password app integration security docs](https://developer.1password.com/docs/cli/app-integration-security/)). Authorization extends to sub-shell processes "in that window."

The AgenC daemon calls `Setsid: true` when forking — it has no controlling TTY. Three possible behaviors when the daemon invokes `op run --`:

| Outcome | Implication |
|---------|-------------|
| Prompts cleanly per invocation | Design works as scoped — N Touch ID prompts at daemon startup, batched if `op inject` |
| Hangs | Confirm `OP_CACHE=false` resolves; otherwise design needs revision |
| Errors with "no TTY" | Biometric is incompatible with detached daemons; service-account-token becomes the only path forward, revisit |

This is the **load-bearing empirical question for Phase 0**. The chosen design accepts N prompts at startup; rejected alternatives were:

- Foreground-resolves-and-ships-to-daemon: too much coordination complexity.
- Drop `Setsid: true`: daemon dies on terminal close, incompatible with all-day persistence.
- 1Password service-account token: bypasses biometric but makes AgenC the steward of a long-lived token — the thing we wanted to avoid.
- 1Password Connect server: adds a second daemon for negligible improvement.

Composio integration (hybrid model)
-----------------------------------

[Composio](https://composio.dev/) is a cloud-hosted MCP gateway with 1000+ pre-built tool integrations, OAuth-based credential management, SOC2 certification. Free tier 20K tool calls/mo, Starter $29/mo (200K), Pro $229/mo (2M), Enterprise self-host option.

**Composio is a fit for the cloud-SaaS slice; AgenC gateway is a fit for the custom-local slice.** Don't pick one — run both.

| Tool category | Backend |
|---------------|---------|
| Off-the-shelf cloud SaaS with OAuth (Todoist, GitHub, Linear, Slack, Gmail, possibly Notion) | Composio |
| Custom personal MCPs (`mieubrisse/notion-mcp-server` fork, `mieubrisse/anki-mcp-server`, future custom builds) | AgenC gateway |
| Local desktop apps (Anki) | AgenC gateway |
| Anything requiring offline / sovereignty | AgenC gateway |

The mission's `.mcp.json` gets both kinds of entries: `agenc` stdio shims for local backends, direct `transport: http` entries for Composio's hosted endpoints. Or — more cleanly — AgenC supports a "remote-MCP" backend type that proxies Composio, so missions see a uniform `agenc mcp client X` interface and don't care whether the backend is local or cloud.

Composio benefits for its slice:
- Zero 1Password prompts for Composio-handled tools (OAuth replaces token-on-disk).
- Zero local supervision / restart overhead for that slice.
- Eliminates Kevin maintaining personal forks of off-the-shelf MCP servers (notably the Notion fork).
- OAuth has better security properties than integration tokens (scope, revocation, rotation).

Trust trade-off: Composio holds OAuth tokens for the SaaS subset. SOC2-certified; comparable trust threshold to "GitHub holds my code" or "1Password holds my passwords."

Other competitive options in this space (not selected): Docker MCP Gateway, Lunar.dev MCPX, MintMCP, Bifrost.

Phase 0: Empirical validation
------------------------------

Must answer before committing to Phase 1:

1. **`op` behavior from a `setsid`'d daemon.** Spawn an existing-style detached AgenC daemon; have it `exec.Command("op", "run", "-e", "OP_CACHE=false", "--", "echo", "test")`. Observe: prompts cleanly, hangs, or errors with no-TTY. Three branches per the table above.
2. **Sandbox accessibility of the `agenc` binary from inside a mission.** Verify `agenc` is in `PATH` inside the mission sandbox (or that the shim entry can use an absolute path). The Unix socket itself is already in the sandbox allowlist.
3. **Composio API compatibility.** For each cloud SaaS we'd offload, verify their HTTP MCP endpoint works with Claude Code's `claude mcp add --transport http` flow, and verify their tool surface covers what we actually call.

Phase 1: Go-native MVP
-----------------------

Scope: AgenC config schema for MCP servers with `!op` references, `agenc mcp client <name>` shim subcommand, in-daemon backend supervisor using `op run`-based spawn, per-backend mutex, Unix-socket gateway routing, mission-spawn `.mcp.json` injection. Skip: HTTP transport, health probes, on-demand starting, sparfenyuk wrapping.

Rough estimate: 1-2 weeks of focused build, possibly more depending on JSON-RPC framing complexity.

Phase 2: Polish
---------------

- Health probes via `tools/list` ping (distinguish "HTTP up but child dead" from "everything healthy")
- Identity-scoped routing (different secrets per named backend instance, e.g., `todoist-personal` and `todoist-work`)
- Per-mission opt-out via `.agenc/mission.yml`
- UX polish on `agenc mcp add` / `configure` / `display` / `restart`
- Composio "remote-MCP" backend type if hybrid is adopted

Future direction: MCPs as CLIs
-------------------------------

Once the gateway exists, an inverted direction becomes possible: expose every gatewayed MCP server as a CLI on the user's system. The gateway already has the schema (from `tools/list`), the auth, and the supervised backend. Auto-generating a CLI dispatcher per server is mechanical.

```bash
agenc mcp call notion search '{"query": "Reading List"}'   # primitive
agenc mcp notion search --query "Reading List"             # dispatch
agenc mcp install-shim notion                              # creates ~/.local/bin/notion
notion search "Reading List"                                # works from any shell
```

Why this matters:

- **Shell composability.** `notion search Q3 | jq '.results[].title' | xargs -I {} bd create --title={}` — Unix philosophy applied to the MCP corpus.
- **Cron and scripts** can use MCP-managed services without AI in the loop.
- **Mobile-via-SSH.** SSH into the Mac, run `todoist add-task "..."` from a phone, skip the mission spawn.
- **Other agents and tools** that don't speak MCP get curated access via these CLIs.
- **Debugging.** Poke at a misbehaving backend directly from the shell.
- **Compounding with the Information Architecture.** Today many entries in `/master-information-architecture` say "via MCP" — only useful inside AI clients. As CLIs, they become universally accessible.

The deepest implication: MCP becomes a universal service-definition format, CLIs become the universal access layer, and the gateway converts between them. A service defined once in MCP shape is automatically available to AIs (via MCP) and humans/scripts (via CLI). Same persistent backend, same single auth handshake, two consumer surfaces.

Not scoped into Phase 1, but the Phase 1 backend abstraction should be designed with this in mind so it slots in cleanly later. Worth its own bead.

Open UX questions (separate design pass needed)
------------------------------------------------

- How does a user add a new MCP server to AgenC? (`agenc mcp add`? Edit YAML directly? Wizard?)
- How are secrets configured for it? (Inline `!op` reference in the same command? Separate `agenc mcp secret set`?)
- Status display: how does the user see "which backends are running / healthy / stale"?
- Per-server lifecycle: `agenc mcp restart notion`, `agenc mcp logs notion`, etc.
- Per-mission opt-out: where does it live, what's the syntax?
- Configuration storage: where does `mcp_servers` config live? Per-user (`~/.config/agenc/`)? Per-machine? Synced via dotfiles?

Tradeoffs / Constraints carried forward
----------------------------------------

- **Singleton model means single identity per backend.** Two missions wanting different Todoist accounts on the same backend can't be served by one shared singleton. Multi-identity = multiple named backends (`todoist-personal`, `todoist-work`).
- **Daemon restart briefly breaks all live missions** (singleton model). Acceptable for personal use.
- **Backend-side concurrency** is still the unsolved-by-anyone problem at the MCP-server level. Mutex serializes inbound calls; doesn't help if the backend itself has bad state machines. Per-server question.
- **Related bead `dotfiles-cnd`** ("Todoist MCP -> Todoist CLI") is the per-server alternative — replace MCP entirely with CLI shell-out. Decide per-server, not globally. The MCPs-as-CLIs future direction subsumes this for free.

References
----------

- Bead `agenc-tpde` (this doc supersedes its long body)
- [2026-03-18 supergateway research](2026-03-18-supergateway-research.md) — landscape sweep, still canonical for off-the-shelf comparison
- [Sparfenyuk mcp-proxy source `mcp_server.py`](https://github.com/sparfenyuk/mcp-proxy/blob/main/src/mcp_proxy/mcp_server.py) — singleton-child model confirmed
- [1Password app integration security](https://developer.1password.com/docs/cli/app-integration-security/) — TTY-keyed session
- [op daemon Tahoe TCC hang](https://github.com/openclaw/openclaw/issues/55459)
- [Composio](https://composio.dev/) — competitive hybrid integration
