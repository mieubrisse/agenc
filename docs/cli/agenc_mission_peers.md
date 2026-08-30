## agenc mission peers

List missions reachable by Claude Code's SendMessage tool

### Synopsis

List missions reachable by Claude Code's SendMessage tool.

Claude Code's ListAgents tool prints one row per live session, addressed by a
peer name like 'agent-da' that carries no mission, repo, or task identity. This
command prints the same peer names alongside the mission each one belongs to,
so a row in ListAgents can be matched to the work it is doing.

The PEER and PANE columns are exactly the strings ListAgents prints. Match on
PEER; if two live sessions share a peer name, match on PANE instead. Then
address the peer with SendMessage, appending the '[ref]' that ListAgents shows
for that row.

A peer in ListAgents with no row here is not an active AgenC mission — another
Claude session on this machine, or a session on another machine.

```
agenc mission peers [flags]
```

### Options

```
  -h, --help   help for peers
```

### SEE ALSO

* [agenc mission](agenc_mission.md)	 - Manage agent missions

