package mission

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
)

const (
	// Number of retry attempts when 1Password CLI fails to connect to the
	// desktop app. The IPC socket between op and the desktop app is
	// intermittently flaky.
	opConnectMaxRetries = 3
	opConnectRetryDelay = 1 * time.Second
)

// CreateMissionDir sets up the mission directory structure. When gitRepoSource
// is non-empty, the repository is copied directly as the agent/ directory
// (agent/ IS the repo). When gitRepoSource is empty, an empty agent/ directory
// is created.
//
// The per-mission claude config directory is always built from the shadow repo.
//
// Returns the mission root directory path (not the agent/ subdirectory).
func CreateMissionDir(agencDirpath string, missionID string, gitRepoName string, gitRepoSource string) (string, error) {
	missionDirpath := config.GetMissionDirpath(agencDirpath, missionID)
	agentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionID)

	if err := os.MkdirAll(missionDirpath, 0755); err != nil {
		return "", stacktrace.Propagate(err, "failed to create directory '%s'", missionDirpath)
	}

	if gitRepoSource != "" {
		// Copy the repo directly as agent/ (CopyRepo creates the destination)
		if err := CopyRepo(gitRepoSource, agentDirpath); err != nil {
			return "", stacktrace.Propagate(err, "failed to copy git repo into agent directory")
		}
	} else {
		if err := os.MkdirAll(agentDirpath, 0755); err != nil {
			return "", stacktrace.Propagate(err, "failed to create directory '%s'", agentDirpath)
		}
	}

	// Look up MCP trust config for this repo
	var trustedMcpServers *config.TrustedMcpServers
	if gitRepoName != "" {
		cfg, _, err := config.ReadAgencConfig(agencDirpath)
		if err == nil {
			if rc, ok := cfg.GetRepoConfig(gitRepoName); ok {
				trustedMcpServers = rc.TrustedMcpServers
			}
		}
	}

	// Build per-mission claude config directory from shadow repo
	if err := claudeconfig.BuildMissionConfigDir(agencDirpath, missionID, trustedMcpServers); err != nil {
		return "", stacktrace.Propagate(err, "failed to build per-mission claude config directory")
	}

	return missionDirpath, nil
}

// BuildClaudeCmd constructs an exec.Cmd for running Claude in the given agent
// directory. If a secrets.env file exists at .claude/secrets.env within the
// agent directory, secret references are resolved via `op inject` and added
// as environment variables. Otherwise, Claude is invoked directly.
//
// The secrets are resolved before starting Claude (not via `op run` wrapping)
// because `op run` does not support TUI applications — it interferes with
// terminal (TTY) passthrough, causing interactive apps like Claude to hang.
//
// The returned command has its working directory, environment variables
// (CLAUDE_CONFIG_DIR, AGENC_MISSION_UUID, CLAUDE_CODE_OAUTH_TOKEN), set but
// does NOT set stdin/stdout/stderr — callers should wire those as needed
// (e.g. interactive mode connects to the terminal, headless mode uses pipes).
func BuildClaudeCmd(agencDirpath string, missionID string, agentDirpath string, model string, claudeArgs []string) (*exec.Cmd, error) {
	// Prepend --model flag if a default model is configured
	if model != "" {
		claudeArgs = append([]string{"--model", model}, claudeArgs...)
	}

	claudeBinary, err := exec.LookPath("claude")
	if err != nil {
		return nil, stacktrace.Propagate(err, "'claude' binary not found in PATH")
	}

	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(agencDirpath, missionID)

	cmd := exec.Command(claudeBinary, claudeArgs...)
	cmd.Dir = agentDirpath
	cmd.Env = append(os.Environ(),
		"CLAUDE_CONFIG_DIR="+claudeConfigDirpath,
		config.MissionUUIDEnvVar+"="+missionID,
	)

	// Resolve 1Password secrets if secrets.env exists
	secretsEnvFilepath := filepath.Join(agentDirpath, config.UserClaudeDirname, config.SecretsEnvFilename)
	if _, statErr := os.Stat(secretsEnvFilepath); statErr == nil {
		resolvedSecrets, err := resolveOnePasswordSecrets(secretsEnvFilepath)
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to resolve 1Password secrets from '%s'", secretsEnvFilepath)
		}
		cmd.Env = append(cmd.Env, resolvedSecrets...)
	}

	// Read the OAuth token — callers must ensure it exists (via
	// config.SetupOAuthToken) before entering the wrapper.
	oauthToken, err := config.ReadOAuthToken(agencDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to read OAuth token")
	}
	if oauthToken == "" {
		return nil, stacktrace.NewError(
			"no OAuth token configured\n\n" +
				"To set up authentication:\n" +
				"  1. Run: claude setup-token\n" +
				"  2. Copy the token (starts with sk-ant-)\n" +
				"  3. Run: agenc config set claudeCodeOAuthToken <token>",
		)
	}
	cmd.Env = append(cmd.Env, "CLAUDE_CODE_OAUTH_TOKEN="+oauthToken)

	return cmd, nil
}

// resolveOnePasswordSecrets reads a secrets.env file and resolves all 1Password
// secret references using `op inject`. Each line should be KEY=op://... format.
// Returns resolved environment variables as KEY=value strings.
//
// Uses `op inject` rather than `op run` because `op run` does not support TUI
// applications — it breaks terminal (TTY) passthrough.
func resolveOnePasswordSecrets(secretsEnvFilepath string) ([]string, error) {
	opBinary, err := exec.LookPath("op")
	if err != nil {
		return nil, stacktrace.Propagate(err, "'op' (1Password CLI) not found in PATH; required because '%s' exists", secretsEnvFilepath)
	}

	// Verify 1Password desktop app connectivity before resolving secrets.
	// The IPC socket is intermittently flaky, so retry on failure.
	if err := ensureOpConnectivity(opBinary); err != nil {
		return nil, err
	}

	// Read the secrets.env file to build an op inject template.
	// Each line is KEY=op://vault/item/field — we convert to KEY={{ op://vault/item/field }}
	// so op inject can resolve them.
	data, err := os.ReadFile(secretsEnvFilepath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to read secrets.env file")
	}

	var templateLines []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eqIdx := strings.IndexByte(line, '=')
		if eqIdx < 0 {
			continue
		}
		key := line[:eqIdx]
		value := line[eqIdx+1:]
		// Wrap the value in {{ }} for op inject template syntax
		templateLines = append(templateLines, fmt.Sprintf("%s={{ %s }}", key, value))
	}
	if err := scanner.Err(); err != nil {
		return nil, stacktrace.Propagate(err, "failed to parse secrets.env file")
	}

	if len(templateLines) == 0 {
		return nil, nil
	}

	template := strings.Join(templateLines, "\n")

	// Run op inject to resolve all secret references at once
	cmd := exec.Command(opBinary, "inject")
	cmd.Stdin = strings.NewReader(template)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, stacktrace.NewError("'op inject' failed: %s\n%s", exitErr.Error(), string(exitErr.Stderr))
		}
		return nil, stacktrace.Propagate(err, "'op inject' failed")
	}

	// Parse resolved output back into KEY=value pairs
	var resolved []string
	scanner = bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		resolved = append(resolved, line)
	}

	return resolved, nil
}

// SpawnClaude starts claude as a child process in the given agent directory.
// Returns the running command. The caller is responsible for calling cmd.Wait().
func SpawnClaude(agencDirpath string, missionID string, agentDirpath string, model string) (*exec.Cmd, error) {
	return SpawnClaudeWithPrompt(agencDirpath, missionID, agentDirpath, model, "")
}

// SpawnClaudeWithPrompt starts claude with an initial prompt as a child process
// in the given agent directory. Claude always starts in interactive mode; if
// initialPrompt is non-empty, it is passed as a positional argument to pre-fill
// the first message. Returns the running command. The caller is responsible for
// calling cmd.Wait().
func SpawnClaudeWithPrompt(agencDirpath string, missionID string, agentDirpath string, model string, initialPrompt string) (*exec.Cmd, error) {
	var args []string
	if initialPrompt != "" {
		// Pass prompt as positional argument for interactive mode with pre-filled message
		args = []string{initialPrompt}
	}

	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, model, args)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to build claude command")
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, stacktrace.Propagate(err, "failed to start claude")
	}

	return cmd, nil
}

// SpawnClaudeResume starts claude -c as a child process in the given agent
// directory. Returns the running command. The caller is responsible for
// calling cmd.Wait().
func SpawnClaudeResume(agencDirpath string, missionID string, agentDirpath string, model string) (*exec.Cmd, error) {
	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, model, []string{"-c"})
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to build claude resume command")
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, stacktrace.Propagate(err, "failed to start claude -c")
	}

	return cmd, nil
}

// SpawnClaudeResumeWithSession starts claude with session-based resumption
// using claude -r <session-id>. If sessionID is empty, falls back to
// claude -c. Returns the running command. The caller is responsible for
// calling cmd.Wait().
func SpawnClaudeResumeWithSession(agencDirpath string, missionID string, agentDirpath string, model string, sessionID string) (*exec.Cmd, error) {
	var args []string
	if sessionID != "" {
		args = []string{"-r", sessionID}
	} else {
		args = []string{"-c"}
	}

	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, model, args)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to build claude resume command")
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, stacktrace.Propagate(err, "failed to start claude resume")
	}

	return cmd, nil
}

// ensureOpConnectivity verifies that the 1Password CLI can connect to the
// desktop app by running `op whoami`. The IPC socket between the CLI and the
// desktop app is intermittently flaky, so this retries on failure to warm the
// connection before using it for secret injection.
func ensureOpConnectivity(opBinary string) error {
	var lastErr error
	for attempt := 0; attempt <= opConnectMaxRetries; attempt++ {
		if attempt > 0 {
			fmt.Fprintf(os.Stderr, "1Password connection failed, retrying (%d/%d)...\n", attempt, opConnectMaxRetries)
			time.Sleep(opConnectRetryDelay)
		}
		cmd := exec.Command(opBinary, "whoami")
		output, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		lastErr = stacktrace.NewError("%s: %s", err, strings.TrimSpace(string(output)))
	}
	return stacktrace.Propagate(lastErr, "1Password CLI failed to connect to desktop app after %d attempts", opConnectMaxRetries+1)
}
