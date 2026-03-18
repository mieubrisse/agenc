Supergateway Research: Persistent MCP Servers for AgenC
=======================================================

Research into using [supergateway](https://github.com/supercorp-ai/supergateway) to keep MCP servers running persistently on macOS, so that Claude Code sessions connect to already-running servers instead of cold-starting them each time. This document also evaluates what AgenC could build natively to achieve the same goal — potentially with better reliability and tighter integration.

How Supergateway Works
----------------------

Supergateway is a Node.js tool that wraps MCP servers using transport translation. Its primary use case: take an MCP server that communicates over stdio and expose it over HTTP (SSE, Streamable HTTP, or WebSocket) so that network-based clients can connect to it.

### Transport Modes

| Input Transport | Output Transport | Description |
|---|---|---|
| `--stdio "command"` | `--outputTransport sse` | Wrap a stdio server as SSE |
| `--stdio "command"` | `--outputTransport streamableHttp` | Wrap a stdio server as Streamable HTTP |
| `--stdio "command"` | `--outputTransport ws` | Wrap a stdio server as WebSocket |
| `--sse "url"` | (stdio) | Connect to remote SSE, expose as stdio |
| `--streamableHttp "url"` | (stdio) | Connect to remote Streamable HTTP, expose as stdio |

For persistent MCP servers, we want `--stdio "command" --outputTransport streamableHttp`.

### CLI Options

| Flag | Default | Description |
|---|---|---|
| `--stdio "command"` | (required) | The MCP server command to wrap |
| `--outputTransport` | `sse` | Output protocol: `sse`, `streamableHttp`, `ws` |
| `--port` | `8000` | HTTP listen port |
| `--baseUrl` | `http://localhost:8000` | Base URL for clients |
| `--streamableHttpPath` | `/mcp` | Endpoint path for Streamable HTTP |
| `--stateful` | (off) | Enable stateful mode for Streamable HTTP |
| `--sessionTimeout` | (none) | Session timeout in milliseconds |
| `--healthEndpoint` | (none) | Register health check endpoint(s) |
| `--logLevel` | `info` | `debug`, `info`, or `none` |
| `--cors` | (off) | Enable CORS |

### Stateless vs Stateful Streamable HTTP

**Stateless mode** (default): Each HTTP request spawns a fresh child process. No state persists between requests — every tool call triggers a full cold start.

**Stateful mode** (`--stateful`): A single child process is spawned per session, tracked via `Mcp-Session-Id` header. The child stays alive across requests within the same session.

### Process Supervision: The Critical Gap

Supergateway does **NOT** restart crashed child processes.

From the source code:

- **SSE mode** (`stdioToSse.ts`): Child crash kills the entire supergateway process.
- **Stateful Streamable HTTP** (`stdioToStatefulStreamableHttp.ts`): Child crash kills the session but HTTP server stays up. New sessions get fresh children.
- **Stateless Streamable HTTP**: Per-request children, not applicable.

| Mode | Child crashes | Supergateway stays alive? | Restart? |
|---|---|---|---|
| SSE | Supergateway exits too | No | No |
| Stateful HTTP | Session becomes defunct | Yes | No |
| Stateless HTTP | N/A | Yes | N/A |

### Claude Code Integration

Claude Code connects via:

```
claude mcp add --transport http <name> http://localhost:<port>/mcp
```

Known Issues (from GitHub)
--------------------------

- **#108**: Memory leak in stateless mode (child processes not cleaned up)
- **#112**: SSE reconnection crash
- **#35**: Child process sharing between sessions (state contamination)
- **#105/#106**: Multi-client concurrency issues
- **#98**: Claude MCP connector failures with streamable HTTP
- **#96**: No lifecycle hooks

Alternatives
------------

| Tool | Language | Key Difference |
|---|---|---|
| supergateway | TypeScript/Node | Most popular, most features, no child restart |
| mcp-proxy (sparfenyuk) | Python | Can proxy multiple stdio servers from one instance |
| mcp-proxy (punkpeye) | TypeScript | Persistent sessions for HTTP streamable |
| ContextForge MCP Gateway | Python | Smart proxy with JWT auth |

None provide built-in child process restart. Process supervision is delegated to the host system.

What AgenC Could Do Better
---------------------------

AgenC is already an orchestration system that manages agent lifecycles, missions, and configuration. Building persistent MCP server management directly into AgenC — rather than depending on supergateway — would let us address the gaps above and integrate tightly with the existing system.

### Built-in Child Process Restart on Crash

Supergateway's biggest weakness: when a wrapped MCP server crashes, it stays dead. AgenC already manages long-running processes (missions, tmux sessions). Extending this to MCP server processes is natural:

- Detect child process exit and automatically respawn with exponential backoff
- Track crash counts and alert the user if a server is in a crash loop
- Optionally drain in-flight requests before restart (graceful restart)

This eliminates the need for external process supervisors (launchd, systemd) that supergateway users must configure themselves.

### Centralized Port Management

Supergateway requires manually assigning ports per server (`--port 8001`, `--port 8002`, etc.). With multiple MCP servers, port conflicts become a real risk — especially across missions.

AgenC could:

- Maintain a port registry (in the SQLite database or a simple file) that tracks which ports are allocated to which MCP servers
- Auto-assign ports from a configurable range, avoiding conflicts
- Free ports when servers are stopped or missions end
- Expose the port mapping so agents can discover server endpoints without hardcoding

### Secrets Injection Without Config File Exposure

MCP servers often need API keys and tokens (e.g., Todoist, Grain, GitHub). With supergateway, these secrets must appear in launch commands or environment variables that end up in config files, shell history, or process listings.

AgenC could:

- Store secrets in a dedicated secrets store (encrypted at rest, separate from config files)
- Inject secrets into the MCP server process environment at launch time, without writing them to disk
- Rotate secrets without restarting agents — only the MCP server process needs a restart
- Avoid exposing secrets in `settings.json`, `claude_desktop_config.json`, or any checked-in file

### Health Monitoring and Automatic Recovery

Supergateway has an optional `--healthEndpoint` flag, but it only checks if the HTTP layer is alive — not whether the underlying MCP server is functional.

AgenC could:

- Periodically invoke a lightweight MCP method (e.g., `tools/list`) to verify the server is actually responsive
- Distinguish between "HTTP server is up but child is dead" and "everything is healthy"
- Automatically restart unhealthy servers with backoff
- Surface server health in `agenc status` or mission inspect output
- Log health history for debugging intermittent failures

### Integration with the AgenC Mission Lifecycle

This is the strongest argument for building it into AgenC rather than using supergateway as a dependency:

- **Mission-scoped servers**: Some MCP servers should only run while a mission is active. AgenC can start/stop them with the mission lifecycle automatically.
- **Global servers**: Others (Todoist, Grain) should be shared across all missions. AgenC can manage these as daemon-level processes that outlive individual missions.
- **Config injection**: AgenC already assembles `claude-config/` for each mission. It could rewrite MCP server entries from stdio to HTTP transport automatically, pointing agents at the persistent server endpoints.
- **Deduplication**: If multiple missions need the same MCP server, AgenC can share a single server process instead of spawning duplicates.
- **Shutdown coordination**: When AgenC shuts down (or the user logs out), it can gracefully stop all managed MCP servers.

### Implementation Considerations

- AgenC is written in Go, so the transport translation layer (stdio ↔ HTTP) would need to be implemented in Go or delegated to a subprocess. The MCP protocol is JSON-RPC over the chosen transport — straightforward to proxy.
- Could start simple: just process supervision + port management, without reimplementing transport translation. Use supergateway as the wrapped binary initially, then replace it later.
- The `agenc` daemon (if one exists or is planned) is the natural home for this — a long-running process that manages MCP server lifecycles alongside missions.

Sources
-------

- <https://github.com/supercorp-ai/supergateway>
- <https://github.com/supercorp-ai/supergateway/blob/main/src/gateways/stdioToSse.ts>
- <https://github.com/supercorp-ai/supergateway/blob/main/src/gateways/stdioToStatefulStreamableHttp.ts>
- <https://github.com/supercorp-ai/supergateway/issues>
- <https://code.claude.com/docs/en/mcp>
- <https://github.com/sparfenyuk/mcp-proxy>
- <https://github.com/punkpeye/mcp-proxy>
