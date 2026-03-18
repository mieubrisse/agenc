Mission ls server-side simplification
======================================

Status: approved
Date: 2026-03-18

Problem
-------

`mission ls` calls `getAgencContext()` which triggers the full onboarding/dir-creation
machinery (`ensureConfigured` → `EnsureDirStructure`). This is unnecessary for a read
command. It also reads local filesystem state (adjutant markers, history files, git
status) that should come from the server.

Design
------

### Server: add IsAdjutant to mission responses

Add `IsAdjutant bool` to `MissionResponse` and as a transient field on
`database.Mission`. The server populates it during enrichment by calling
`config.IsMissionAdjutant(s.agencDirpath, m.ID)` — a cheap stat check that
runs in the existing enrichment loop alongside `ResolvedSessionTitle` and
`ClaudeState`.

`MissionResponse.ToMission()` carries `IsAdjutant` through so client code can
read it off the struct.

### Client: slim down mission ls

- Replace `getAgencContext()` with `resolveAgencDirpath()` (read-only, no dir
  creation or onboarding wizard).
- Remove the redundant `ensureServerRunning()` call (already done inside
  `fetchMissions` → `serverClient()`).
- Remove `--git-status` flag and associated functions (`getMissionGitStatus`,
  `checkGitStatus`).
- Remove config staleness columns (`shadowHeadCommitHash`,
  `CountCommitsBehind`, `formatConfigCommit`).
- Remove `resolveMissionPrompt()` local history fallback — SESSION column uses
  server-provided `ResolvedSessionTitle` → `Prompt`.
- Switch adjutant badge to use `m.IsAdjutant` from server response instead of
  local `config.IsMissionAdjutant()` call.

### Unify picker and ls display

`buildMissionPickerEntries` also switches to `m.IsAdjutant` from the server
response, keeping ls and picker displays consistent. Both use the same
`resolveSessionName` function which simplifies to:
`ResolvedSessionTitle` → `Prompt` → empty.

What gets deleted
-----------------

- `resolveMissionPrompt()` function
- `getMissionGitStatus()` and `checkGitStatus()` functions
- `formatConfigCommit()` function
- `--git-status` flag and `lsGitStatusFlag` variable
- Config staleness code in `runMissionLs`
- `getAgencContext()` + `ensureServerRunning()` calls in `runMissionLs`
