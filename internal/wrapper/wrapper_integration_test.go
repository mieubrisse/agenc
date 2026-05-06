package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/server"
)

// testSetup creates a temporary agenc directory structure and database for testing.
type testSetup struct {
	agencDirpath   string
	missionID      string
	db             *database.DB
	cleanup        func()
	socketFilepath string
}

// setupTest creates a complete test environment with database and directory structure.
func setupTest(t *testing.T) *testSetup {
	t.Helper()

	// Create temporary agenc directory with short path to avoid unix socket path limit (104 chars on macOS).
	// Use os.TempDir() ($TMPDIR) so tests work inside the Claude Code sandbox.
	tempDir, err := os.MkdirTemp(os.TempDir(), "wt-")
	if err != nil {
		t.Fatalf("failed to create temp directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })

	agencDirpath := filepath.Join(tempDir, "a")
	if err := os.MkdirAll(agencDirpath, 0755); err != nil {
		t.Fatalf("failed to create agenc directory: %v", err)
	}

	// Create required subdirectories
	for _, dir := range []string{"cache", "missions"} {
		if err := os.MkdirAll(filepath.Join(agencDirpath, dir), 0755); err != nil {
			t.Fatalf("failed to create %s directory: %v", dir, err)
		}
	}

	// Create and initialize database
	dbFilepath := filepath.Join(agencDirpath, "database.sqlite")
	db, err := database.Open(dbFilepath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	// Create a test mission
	mission, err := db.CreateMission("github.com/test/repo", nil)
	if err != nil {
		db.Close()
		t.Fatalf("failed to create test mission: %v", err)
	}

	// Use a short mission ID to keep socket path under 104 chars
	shortMissionID := mission.ID[:8]

	// Create mission directory structure with short path
	missionDirpath := filepath.Join(agencDirpath, "m", shortMissionID)
	agentDirpath := filepath.Join(missionDirpath, "agent")
	for _, dir := range []string{missionDirpath, agentDirpath} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			db.Close()
			t.Fatalf("failed to create mission directory: %v", err)
		}
	}

	// Create a dummy OAuth token file
	tokenFilepath := config.GetOAuthTokenFilepath(agencDirpath)
	if err := os.WriteFile(tokenFilepath, []byte("test-token"), 0600); err != nil {
		db.Close()
		t.Fatalf("failed to create OAuth token file: %v", err)
	}

	// Use short socket path
	socketFilepath := filepath.Join(missionDirpath, "w.sock")

	cleanup := func() {
		db.Close()
	}

	return &testSetup{
		agencDirpath:   agencDirpath,
		missionID:      mission.ID,
		db:             db,
		cleanup:        cleanup,
		socketFilepath: socketFilepath,
	}
}

// mockClaudeProcess spawns a sleep command that acts as a mock Claude process.
// It can be sent signals and will respond appropriately.
func mockClaudeProcess(agentDirpath, missionID string, duration time.Duration) (*exec.Cmd, error) {

	// Use a sleep command as a mock Claude process
	cmd := exec.Command("sleep", fmt.Sprintf("%.0f", duration.Seconds()))
	cmd.Dir = agentDirpath
	cmd.Env = append(os.Environ(),
		"AGENC_MISSION_UUID="+missionID,
		"CLAUDE_CODE_OAUTH_TOKEN=test-token",
	)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return cmd, nil
}

// waitForSocket waits for the wrapper HTTP server to be ready by polling GET /status.
func waitForSocket(socketFilepath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := getStatusRaw(socketFilepath, 500*time.Millisecond); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("socket not ready after %v", timeout)
}

// getStatusRaw issues a GET /status against the wrapper socket using a raw
// HTTP client. Used by tests that previously relied on the removed
// WrapperClient.GetStatus method.
func getStatusRaw(socketFilepath string, timeout time.Duration) (*StatusResponse, error) {
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.DialTimeout("unix", socketFilepath, timeout)
			},
		},
		Timeout: timeout,
	}
	resp, err := httpClient.Get("http://wrapper/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var status StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}
	return &status, nil
}

// createTestWrapper creates a wrapper with a logger initialized for testing.
func createTestWrapper(agencDirpath, missionID, gitRepoName string) *Wrapper {
	w := NewWrapper(agencDirpath, missionID, gitRepoName, "")
	w.logger = slog.Default()
	return w
}

// getTestAgentDirpath returns the agent directory path for tests (using short paths).
func getTestAgentDirpath(agencDirpath, missionID string) string {
	return filepath.Join(agencDirpath, "m", missionID[:8], "agent")
}

// startTestEventLoop starts a goroutine that reads commands from the wrapper's
// commandCh and processes them via handleCommand, mimicking the main event loop.
// The goroutine exits when ctx is cancelled or after processing maxCommands
// commands (0 means unlimited). Returns a channel that receives nil on clean
// exit or an error on timeout.
func startTestEventLoop(ctx context.Context, w *Wrapper, maxCommands int) <-chan error {
	done := make(chan error, 1)
	go func() {
		count := 0
		for {
			select {
			case cmdResp := <-w.commandCh:
				resp := w.handleCommand(cmdResp.cmd)
				cmdResp.responseCh <- resp
				count++
				if maxCommands > 0 && count >= maxCommands {
					done <- nil
					return
				}
			case <-ctx.Done():
				done <- nil
				return
			}
		}
	}()
	return done
}

// startHTTPServerAndWait starts the HTTP server and waits for it to become ready.
func startHTTPServerAndWait(t *testing.T, ctx context.Context, socketFilepath string, w *Wrapper) {
	t.Helper()
	go startHTTPServer(ctx, socketFilepath, w, w.logger)
	time.Sleep(100 * time.Millisecond)
	if err := waitForSocket(socketFilepath, 2*time.Second); err != nil {
		t.Fatalf("HTTP server not ready: %v", err)
	}
}

// TestSocketProtocol tests the wrapper HTTP API communication.
func TestSocketProtocol(t *testing.T) {
	setup := setupTest(t)
	defer setup.cleanup()

	w := createTestWrapper(setup.agencDirpath, setup.missionID, "github.com/test/repo")
	w.claudeIdle = true

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.commandCh = make(chan commandWithResponse, 1)

	// Start HTTP server
	go startHTTPServer(ctx, setup.socketFilepath, w, w.logger)
	time.Sleep(100 * time.Millisecond)

	// Wait for HTTP server to be ready
	if err := waitForSocket(setup.socketFilepath, 2*time.Second); err != nil {
		t.Fatalf("HTTP server not ready: %v", err)
	}

	client := NewWrapperClient(setup.socketFilepath, 1*time.Second)

	tests := []struct {
		name       string
		action     func() error
		wantErr    bool
		errSubstr  string
		setup      func()
		checkState func(*testing.T, *Wrapper)
	}{
		{
			name: "claude_update Stop event",
			action: func() error {
				return client.SendClaudeUpdate("Stop", "")
			},
			checkState: func(t *testing.T, w *Wrapper) {
				if !w.claudeIdle {
					t.Error("expected claudeIdle=true after Stop event")
				}
				if !w.hasConversation {
					t.Error("expected hasConversation=true after Stop event")
				}
			},
		},
		{
			name: "claude_update UserPromptSubmit event",
			action: func() error {
				return client.SendClaudeUpdate("UserPromptSubmit", "")
			},
			checkState: func(t *testing.T, w *Wrapper) {
				if w.claudeIdle {
					t.Error("expected claudeIdle=false after UserPromptSubmit event")
				}
				if !w.hasConversation {
					t.Error("expected hasConversation=true after UserPromptSubmit event")
				}
			},
		},
		{
			name: "claude_update Notification event",
			action: func() error {
				return client.SendClaudeUpdate("Notification", "permission_prompt")
			},
		},
		{
			name: "claude_update PostToolUse event",
			action: func() error {
				return client.SendClaudeUpdate("PostToolUse", "")
			},
		},
		{
			name: "claude_update PostToolUseFailure event",
			action: func() error {
				return client.SendClaudeUpdate("PostToolUseFailure", "")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset state for each test
			w.claudeIdle = true
			w.hasConversation = false

			if tt.setup != nil {
				tt.setup()
			}

			// Start event loop (processes 1 command)
			loopDone := startTestEventLoop(ctx, w, 1)

			// Execute the action
			err := tt.action()

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got nil")
				} else if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}

			<-loopDone

			if tt.checkState != nil {
				tt.checkState(t, w)
			}
		})
	}
}

// TestSignalHandling tests SIGINT/SIGTERM forwarding to Claude.
func TestSignalHandling(t *testing.T) {
	setup := setupTest(t)
	defer setup.cleanup()

	w := createTestWrapper(setup.agencDirpath, setup.missionID, "github.com/test/repo")

	mockCmd, err := mockClaudeProcess(getTestAgentDirpath(setup.agencDirpath, setup.missionID), setup.missionID, 30*time.Second)
	if err != nil {
		t.Fatalf("failed to create mock Claude process: %v", err)
	}
	defer func() {
		if mockCmd.Process != nil {
			_ = mockCmd.Process.Kill() // best-effort test cleanup
			_ = mockCmd.Wait()         // reap process
		}
	}()

	w.claudeCmd = mockCmd
	initialPID := mockCmd.Process.Pid

	// Send SIGINT to the mock process
	if err := w.claudeCmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("failed to send SIGINT: %v", err)
	}

	// Wait for process to exit
	done := make(chan error, 1)
	go func() {
		done <- mockCmd.Wait()
	}()

	select {
	case err := <-done:
		// Process should have exited due to SIGINT
		if err == nil {
			t.Error("expected process to exit with error after SIGINT")
		}

		// Verify process is truly dead
		if err := syscall.Kill(initialPID, syscall.Signal(0)); err == nil {
			t.Error("process should be dead after SIGINT")
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for process to exit after SIGINT")
	}
}

// TestClaudeUpdateEventsStateTracking tests that claude_update events properly track state.
func TestClaudeUpdateEventsStateTracking(t *testing.T) {
	setup := setupTest(t)
	defer setup.cleanup()

	w := createTestWrapper(setup.agencDirpath, setup.missionID, "github.com/test/repo")
	w.claudeIdle = false
	w.hasConversation = false

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.commandCh = make(chan commandWithResponse, 1)

	// Start HTTP server and event loop
	go startHTTPServer(ctx, setup.socketFilepath, w, w.logger)
	time.Sleep(100 * time.Millisecond)

	// Start event loop (unlimited commands)
	startTestEventLoop(ctx, w, 0)

	// Wait for HTTP server to be ready
	if err := waitForSocket(setup.socketFilepath, 2*time.Second); err != nil {
		t.Fatalf("HTTP server not ready: %v", err)
	}

	client := NewWrapperClient(setup.socketFilepath, 1*time.Second)

	// Test Stop event: sets idle=true, hasConversation=true
	if err := client.SendClaudeUpdate("Stop", ""); err != nil {
		t.Fatalf("Stop event failed: %v", err)
	}
	if !w.claudeIdle {
		t.Error("expected claudeIdle=true after Stop")
	}
	if !w.hasConversation {
		t.Error("expected hasConversation=true after Stop")
	}

	// Test UserPromptSubmit event: sets idle=false, hasConversation=true
	if err := client.SendClaudeUpdate("UserPromptSubmit", ""); err != nil {
		t.Fatalf("UserPromptSubmit event failed: %v", err)
	}
	if w.claudeIdle {
		t.Error("expected claudeIdle=false after UserPromptSubmit")
	}
	if !w.hasConversation {
		t.Error("expected hasConversation=true after UserPromptSubmit")
	}
}

// TestJSONProtocol verifies the HTTP API returns valid JSON responses.
func TestJSONProtocol(t *testing.T) {
	setup := setupTest(t)
	defer setup.cleanup()

	w := createTestWrapper(setup.agencDirpath, setup.missionID, "github.com/test/repo")
	w.claudeIdle = true

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.commandCh = make(chan commandWithResponse, 1)

	// Start HTTP server (GET /status doesn't need the event loop)
	startHTTPServerAndWait(t, ctx, setup.socketFilepath, w)

	// Verify that GET /status returns valid JSON with expected fields
	w.stateMu.Lock()
	w.claudeIdle = true
	w.stateMu.Unlock()

	status, err := getStatusRaw(setup.socketFilepath, 1*time.Second)
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	if status.ClaudeState != "idle" {
		t.Errorf("expected claude_state 'idle', got %q", status.ClaudeState)
	}
}

// TestInvalidJSON tests that invalid JSON is handled gracefully.
func TestInvalidJSON(t *testing.T) {
	setup := setupTest(t)
	defer setup.cleanup()

	w := createTestWrapper(setup.agencDirpath, setup.missionID, "github.com/test/repo")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.commandCh = make(chan commandWithResponse, 1)

	// Start HTTP server (invalid JSON is rejected at the HTTP handler level, no event loop needed)
	startHTTPServerAndWait(t, ctx, setup.socketFilepath, w)

	// Send raw HTTP POST with malformed JSON body to /claude-update
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return net.DialTimeout("unix", setup.socketFilepath, 1*time.Second)
		},
	}
	httpClient := &http.Client{Transport: transport, Timeout: 1 * time.Second}

	resp, err := httpClient.Post("http://wrapper/claude-update", "application/json", bytes.NewBufferString("{invalid json}"))
	if err != nil {
		t.Fatalf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	// Should get 400 Bad Request
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

// TestGetStatus verifies the status endpoint returns correct state.
func TestGetStatus(t *testing.T) {
	setup := setupTest(t)
	defer setup.cleanup()

	w := createTestWrapper(setup.agencDirpath, setup.missionID, "github.com/test/repo")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.commandCh = make(chan commandWithResponse, 1)

	// Start HTTP server (GET /status doesn't need the event loop)
	startHTTPServerAndWait(t, ctx, setup.socketFilepath, w)

	// Test idle state
	w.stateMu.Lock()
	w.claudeIdle = true
	w.needsAttention = false
	w.stateMu.Unlock()

	status, err := getStatusRaw(setup.socketFilepath, 1*time.Second)
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}
	if status.ClaudeState != "idle" {
		t.Errorf("expected claude_state 'idle', got %q", status.ClaudeState)
	}

	// Test busy state
	w.stateMu.Lock()
	w.claudeIdle = false
	w.needsAttention = false
	w.stateMu.Unlock()

	status, err = getStatusRaw(setup.socketFilepath, 1*time.Second)
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}
	if status.ClaudeState != "busy" {
		t.Errorf("expected claude_state 'busy', got %q", status.ClaudeState)
	}

	// Test needs_attention state (takes priority over idle/busy)
	w.stateMu.Lock()
	w.needsAttention = true
	w.stateMu.Unlock()

	status, err = getStatusRaw(setup.socketFilepath, 1*time.Second)
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}
	if status.ClaudeState != "needs_attention" {
		t.Errorf("expected claude_state 'needs_attention', got %q", status.ClaudeState)
	}
}

// patchCall captures a recorded PATCH /missions/{id} request body.
type patchCall struct {
	id   string
	body server.UpdateMissionRequest
}

// startStubServer launches a minimal HTTP server on a unix socket that records
// PATCH /missions/{id} requests into the returned slice. Returns a cleanup
// function the test must call.
func startStubServer(t *testing.T, socketFilepath string) (*[]patchCall, *sync.Mutex, func()) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(socketFilepath), 0755); err != nil {
		t.Fatalf("failed to create server dir: %v", err)
	}

	calls := &[]patchCall{}
	mu := &sync.Mutex{}

	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /missions/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body server.UpdateMissionRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		*calls = append(*calls, patchCall{id: r.PathValue("id"), body: body})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"updated"}`))
	})

	listener, err := net.Listen("unix", socketFilepath)
	if err != nil {
		t.Fatalf("failed to listen on stub socket: %v", err)
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(listener) }()

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		_ = listener.Close()
	}
	return calls, mu, cleanup
}

// TestSpawnClaude_RebuildsConfigAndLogsCommit verifies that the wrapper's
// per-spawn preamble (1) regenerates the per-mission claude-config from the
// shadow repo, (2) updates the mission's config_commit on the server via
// PATCH, and (3) logs the shadow commit hash. This locks in the behavior that
// every Claude spawn — not just containerized ones — picks up the latest
// shadow state.
func TestSpawnClaude_RebuildsConfigAndLogsCommit(t *testing.T) {
	setup := setupTest(t)
	defer setup.cleanup()

	// Override HOME so claudeconfig.* helpers (UserHomeDir, ~/.claude lookups)
	// resolve to a controlled fake home, not the real user's $HOME.
	fakeHome := filepath.Join(setup.agencDirpath, "fake-home")
	if err := os.MkdirAll(fakeHome, 0755); err != nil {
		t.Fatalf("failed to create fake home: %v", err)
	}
	t.Setenv("HOME", fakeHome)

	// Seed ~/.claude with a CLAUDE.md whose contents we can later assert on,
	// plus a minimal .claude.json (required by copyAndPatchClaudeJSON).
	userClaudeDir := filepath.Join(fakeHome, ".claude")
	if err := os.MkdirAll(userClaudeDir, 0755); err != nil {
		t.Fatalf("failed to create fake ~/.claude: %v", err)
	}
	const userClaudeMdContent = "# User CLAUDE.md\n\nSentinel-content-for-rebuild-test\n"
	if err := os.WriteFile(filepath.Join(userClaudeDir, "CLAUDE.md"), []byte(userClaudeMdContent), 0644); err != nil {
		t.Fatalf("failed to write fake ~/.claude/CLAUDE.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userClaudeDir, ".claude.json"), []byte(`{"projects":{}}`), 0644); err != nil {
		t.Fatalf("failed to write fake ~/.claude/.claude.json: %v", err)
	}

	// Bootstrap the shadow repo so it has the commit our wrapper preamble
	// will read via GetShadowRepoCommitHash.
	if err := claudeconfig.EnsureShadowRepo(setup.agencDirpath); err != nil {
		t.Fatalf("failed to bootstrap shadow repo: %v", err)
	}
	expectedCommit := claudeconfig.GetShadowRepoCommitHash(setup.agencDirpath)
	if expectedCommit == "" {
		t.Fatal("shadow repo has no commit after EnsureShadowRepo — test setup is wrong")
	}

	// The mission directory was created by setupTest under <agenc>/m/<short-id>/agent,
	// but BuildMissionConfigDir resolves the mission dir via GetMissionDirpath
	// (<agenc>/missions/<full-id>). Create that directory tree so BuildMissionConfigDir
	// can write claude-config into it.
	missionDirpath := config.GetMissionDirpath(setup.agencDirpath, setup.missionID)
	missionAgentDirpath := config.GetMissionAgentDirpath(setup.agencDirpath, setup.missionID)
	if err := os.MkdirAll(missionAgentDirpath, 0755); err != nil {
		t.Fatalf("failed to create mission agent dir: %v", err)
	}

	// Stand up a stub server on the unix socket that the wrapper's client
	// connects to. The stub records PATCH /missions/{id} so we can verify
	// rebuildClaudeConfig calls UpdateMission with the right commit hash.
	stubSocketFilepath := config.GetServerSocketFilepath(setup.agencDirpath)
	calls, callsMu, stopStub := startStubServer(t, stubSocketFilepath)
	defer stopStub()

	// Build the wrapper. Use a buffered slog handler so we can assert on
	// the structured log line emitted after a successful rebuild.
	var logBuf bytes.Buffer
	logHandler := slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})
	w := NewWrapper(setup.agencDirpath, setup.missionID, "github.com/test/repo", "")
	w.logger = slog.New(logHandler)
	// Re-point the client at the stub socket. NewWrapper already constructed one
	// pointed at the same path, but we re-initialize for clarity.
	w.client = server.NewClient(stubSocketFilepath)

	// Sanity: confirm devcontainer is nil so we exercise the non-containerized
	// path — that's the gap this test closes.
	if w.devcontainer != nil {
		t.Fatal("test requires non-containerized wrapper")
	}

	if err := w.rebuildClaudeConfig(false); err != nil {
		t.Fatalf("rebuildClaudeConfig returned error: %v", err)
	}

	// Assertion 1: claude-config/CLAUDE.md exists and contains the user content.
	builtClaudeMdPath := filepath.Join(missionDirpath, claudeconfig.MissionClaudeConfigDirname, "CLAUDE.md")
	builtData, err := os.ReadFile(builtClaudeMdPath)
	if err != nil {
		t.Fatalf("expected claude-config/CLAUDE.md to exist at %s: %v", builtClaudeMdPath, err)
	}
	if !strings.Contains(string(builtData), "Sentinel-content-for-rebuild-test") {
		t.Errorf("expected built CLAUDE.md to contain user sentinel content, got:\n%s", string(builtData))
	}

	// Assertion 2: stub server saw exactly one PATCH for our mission with the
	// shadow commit hash in ConfigCommit.
	callsMu.Lock()
	recorded := append([]patchCall(nil), (*calls)...)
	callsMu.Unlock()
	if len(recorded) != 1 {
		t.Fatalf("expected exactly 1 PATCH /missions/{id} call, got %d: %+v", len(recorded), recorded)
	}
	if recorded[0].id != setup.missionID {
		t.Errorf("expected PATCH for mission %q, got %q", setup.missionID, recorded[0].id)
	}
	if recorded[0].body.ConfigCommit == nil {
		t.Fatal("expected ConfigCommit pointer to be non-nil")
	}
	if *recorded[0].body.ConfigCommit != expectedCommit {
		t.Errorf("expected ConfigCommit=%q, got %q", expectedCommit, *recorded[0].body.ConfigCommit)
	}

	// Assertion 3: an Info-level log entry was emitted carrying the shadow
	// commit. We assert on the JSON structure to avoid coupling to format.
	logOut := logBuf.String()
	if !strings.Contains(logOut, `"shadow_commit"`) {
		t.Errorf("expected log output to contain shadow_commit key, got:\n%s", logOut)
	}
	// The hash is logged short (first 12 chars) — verify by prefix.
	shortHash := expectedCommit
	if len(shortHash) > 12 {
		shortHash = shortHash[:12]
	}
	if !strings.Contains(logOut, shortHash) {
		t.Errorf("expected log output to contain shadow commit prefix %q, got:\n%s", shortHash, logOut)
	}
}
