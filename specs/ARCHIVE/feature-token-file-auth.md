Token File Authentication
=========================

**Created**: 2026-02-12  **Type**: feature  **Status**: Open
**Related**: `docs/authentication.md`, `specs/credential-sync-loop.md`

---

Description
-----------

Replace the macOS Keychain-based authentication flow with a simple token file approach. Users provide their `CLAUDE_CODE_OAUTH_TOKEN` once (during onboarding or via `agenc config set`), and AgenC writes it to a secure file. The wrapper reads this file and passes the token as an environment variable to Claude Code, bypassing all Keychain machinery.

Context
-------

The current authentication system uses macOS Keychain to store and synchronize OAuth tokens between a global entry and per-mission entries. This involves:

- Cloning credentials from global → per-mission Keychain on mission create
- Upward sync (per-mission → global) via 60-second polling
- Downward sync (global → per-mission) via fsnotify
- Write-back on mission exit
- Token expiry monitoring with Keychain re-reads
- `agenc login` command that opens an interactive Claude shell for `/login`

This machinery is complex, macOS-specific, and fragile (token races between concurrent missions, OAuth refresh conflicts). The new approach simplifies everything: one token file, one env var, no Keychain.

Design Decision
---------------

**Selected: Token file with env var passthrough (Quality: 8/10)**

The user provides their OAuth token via `agenc config set claudeCodeOAuthToken <token>` or during `agenc config init`. The token is written to `$AGENC_DIRPATH/cache/oauth-token` with 600 permissions. The wrapper reads this file at spawn time and sets `CLAUDE_CODE_OAUTH_TOKEN` in Claude's environment. When this file exists, all Keychain operations are skipped.

**Rationale:** This is the simplest approach that satisfies the core requirement (authenticate once, skip Keychain). No daemon involvement needed — the wrapper just reads a file. The token file has restricted permissions (600) and lives outside the config repo (so it's never committed or pushed). The existing Keychain code is commented out (not deleted) to preserve the option of re-enabling it later.

User Stories
------------

### Set token via config command

**As a** user, **I want** to run `agenc config set claudeCodeOAuthToken <token>`, **so that** all future missions use this token without Keychain interaction.

**Test Steps:**

1. **Setup**: Fresh AgenC installation with no token file
2. **Action**: Run `agenc config set claudeCodeOAuthToken sk-ant-xxx`
3. **Assert**: File `$AGENC_DIRPATH/cache/oauth-token` exists with permissions 600, contains `sk-ant-xxx`

### Set token during onboarding

**As a** new user, **I want** to be prompted for my Claude Code OAuth token during `agenc config init`, **so that** I can set up authentication as part of the initial setup flow.

**Test Steps:**

1. **Setup**: Fresh AgenC installation, no prior config
2. **Action**: Run `agenc config init`, enter token when prompted
3. **Assert**: Token file created at `$AGENC_DIRPATH/cache/oauth-token` with 600 permissions

### Token passed to Claude missions

**As a** user with a configured token, **I want** every mission's Claude process to receive `CLAUDE_CODE_OAUTH_TOKEN` as an environment variable, **so that** Claude authenticates without touching the Keychain.

**Test Steps:**

1. **Setup**: Token file exists with valid token
2. **Action**: Create a new mission (`agenc mission new`)
3. **Assert**: Claude process's environment includes `CLAUDE_CODE_OAUTH_TOKEN=<token>`; no Keychain clone/sync/writeback occurs

### Clear token

**As a** user, **I want** to run `agenc config set claudeCodeOAuthToken ""`, **so that** I can remove the token and (in the future) fall back to another auth method.

**Test Steps:**

1. **Setup**: Token file exists
2. **Action**: Run `agenc config set claudeCodeOAuthToken ""`
3. **Assert**: Token file is deleted

### Headless/cron missions use token

**As a** user running cron missions, **I want** headless missions to also use the token file, **so that** they authenticate without user interaction.

**Test Steps:**

1. **Setup**: Token file exists, cron job configured
2. **Action**: Daemon spawns headless mission
3. **Assert**: Headless Claude process receives `CLAUDE_CODE_OAUTH_TOKEN`; no Keychain operations

Implementation Plan
-------------------

### Phase 1: Token file infrastructure

- [ ] Add `GetCacheDirpath()` helper to `internal/config/config.go` — returns `$AGENC_DIRPATH/cache/`
- [ ] Add `GetOAuthTokenFilepath()` helper to `internal/config/config.go` — returns `$AGENC_DIRPATH/cache/oauth-token`
- [ ] Add `EnsureCacheDirStructure()` to `internal/config/config.go` — creates `cache/` dir if missing
- [ ] Call `EnsureCacheDirStructure()` from `EnsureDirStructure()` in `internal/config/config.go`
- [ ] Add `ReadOAuthToken(agencDirpath string) (string, error)` to `internal/config/config.go` — reads token file, returns empty string if file doesn't exist
- [ ] Add `WriteOAuthToken(agencDirpath string, token string) error` to `internal/config/config.go` — writes token with 600 perms; if token is empty, deletes the file

### Phase 2: Config CLI support

- [ ] Add `claudeCodeOAuthToken` to `supportedConfigKeys` in `cmd/config_get.go`
- [ ] Add `claudeCodeOAuthToken` case to `getConfigValue()` — reads from token file via `ReadOAuthToken()`, returns "unset" if empty
- [ ] Add `claudeCodeOAuthToken` case to `setConfigValue()` in `cmd/config_set.go` — calls `WriteOAuthToken()`
- [ ] Note: This config key is NOT stored in `config.yml` — it uses the dedicated token file. The `config get`/`set` commands serve as the user-facing interface but route to the file, not YAML.

### Phase 3: Onboarding integration

- [ ] Add `setupOAuthToken(reader)` function to `cmd/config_init.go` — prompts user for token with guidance text about where to obtain it
- [ ] Call `setupOAuthToken()` from `ensureConfigured()` after config repo setup, but only if token file doesn't already exist
- [ ] Print token status in `printConfigSummary()` — "OAuth token: configured" or "OAuth token: not set"

### Phase 4: Wrapper token passthrough

- [ ] Modify `buildClaudeCmd()` in `internal/mission/mission.go` — read token file; if non-empty, append `CLAUDE_CODE_OAUTH_TOKEN=<token>` to `cmd.Env`
- [ ] Modify `buildHeadlessClaudeCmd()` in `internal/wrapper/wrapper.go` — same token file read and env var injection
- [ ] Comment out `cloneCredentials()` call in `wrapper.Run()` (line 192) with explanation: `// Keychain auth disabled: using token file auth via CLAUDE_CODE_OAUTH_TOKEN`
- [ ] Comment out `initCredentialHash()` call in `wrapper.Run()` (line 193) with same explanation
- [ ] Comment out `watchTokenExpiry()` goroutine launch (line 196)
- [ ] Comment out `watchCredentialUpwardSync()` goroutine launch (line 201)
- [ ] Comment out `watchCredentialDownwardSync()` goroutine launch (line 202)
- [ ] Comment out `writeBackCredentials()` calls in the main event loop (lines 232, 243, 270)
- [ ] Comment out `cloneCredentials()` call in `RunHeadless()` (line 600)
- [ ] Comment out `writeBackCredentials()` calls in `RunHeadless()` (lines 642, 652, 658, 661)
- [ ] Add a block comment at the top of `credential_sync.go` explaining: these goroutines are disabled because token file auth replaces Keychain-based auth
- [ ] Add a block comment at the top of `token_expiry.go` explaining: this watcher is disabled because token file auth replaces Keychain-based auth

### Phase 5: Remove `agenc login`

- [ ] Comment out the body of `cmd/login.go` — replace RunE with a function that prints an error message: `"agenc login is no longer needed. Set your token with: agenc config set claudeCodeOAuthToken <token>"`
- [ ] Update `docs/authentication.md` to describe the new token file approach and remove Keychain references

### Phase 6: Clean up mission removal

- [ ] Comment out `DeleteKeychainCredentials()` call in `cmd/mission_rm.go` (line 121) with explanation

### Phase 7: Documentation

- [ ] Update `docs/authentication.md` — rewrite to describe token file auth flow
- [ ] Update `docs/system-architecture.md` — remove credential sync goroutines from wrapper section, update IPC table, note token file in directory layout

Technical Details
-----------------

- **Token file location**: `$AGENC_DIRPATH/cache/oauth-token`
- **Token file permissions**: `0600` (owner read/write only)
- **Cache directory**: `$AGENC_DIRPATH/cache/` (new directory, created by `EnsureDirStructure`)
- **Config key**: `claudeCodeOAuthToken` — routed to token file, NOT stored in `config.yml`
- **Env var**: `CLAUDE_CODE_OAUTH_TOKEN` — set by `buildClaudeCmd()` and `buildHeadlessClaudeCmd()`
- **Modules to modify**: `internal/config/config.go`, `internal/mission/mission.go`, `internal/wrapper/wrapper.go`, `cmd/config_get.go`, `cmd/config_set.go`, `cmd/config_init.go`, `cmd/login.go`, `cmd/mission_rm.go`
- **Documentation to update**: `docs/authentication.md`, `docs/system-architecture.md`
- **No new dependencies** required

Testing Strategy
----------------

- **Unit tests**: `ReadOAuthToken` / `WriteOAuthToken` — test file creation, 600 perms, empty-string deletion, non-existent file returns empty string
- **Integration tests**: Verify `buildClaudeCmd` includes `CLAUDE_CODE_OAUTH_TOKEN` in env when token file exists, omits it when absent
- **Manual tests**: Run `agenc config set claudeCodeOAuthToken <token>`, create a mission, verify Claude starts without Keychain prompts

Acceptance Criteria
-------------------

- [ ] `agenc config set claudeCodeOAuthToken <token>` writes token to `$AGENC_DIRPATH/cache/oauth-token` with 600 perms
- [ ] `agenc config get claudeCodeOAuthToken` returns the stored token (or "unset")
- [ ] `agenc config set claudeCodeOAuthToken ""` deletes the token file
- [ ] `agenc config init` prompts for token during interactive setup
- [ ] All missions (interactive and headless) pass `CLAUDE_CODE_OAUTH_TOKEN` to Claude when token file exists
- [ ] All Keychain operations (clone, sync, writeback, expiry watch) are commented out
- [ ] `agenc login` prints a deprecation message pointing to `config set`
- [ ] `docs/authentication.md` and `docs/system-architecture.md` are updated
- [ ] Token file is never committed to the config repo (lives in `cache/`, not `config/`)

Risks & Considerations
----------------------

- **Token expiry**: The token file approach has no automatic refresh mechanism. When the token expires, the user must manually obtain a new one and run `config set` again. This is an acceptable tradeoff for simplicity — the user explicitly opted into managing their own token.
- **No fallback**: With Keychain code commented out, there's no fallback if the token file is missing. Missions will still launch but Claude Code may fail to authenticate. The `agenc config init` flow mitigates this by prompting during first setup.
- **Future re-enablement**: The Keychain code is commented out, not deleted. If a use case arises that requires Keychain support (e.g., Linux port using a different credential store), the code can be uncommented and adapted.
- **Security**: The token lives in a file with 600 perms. This is less secure than macOS Keychain (which encrypts at rest and requires user consent for access) but is adequate for a single-user development tool.
