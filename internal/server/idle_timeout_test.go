package server

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

// TestShouldReapMission pins the whole reaping policy. The rows that matter
// most are the two regressions: a mission whose wrapper says it is working must
// survive a long-stale conversation log (the bug this policy exists to fix),
// and a mission whose wrapper says it is idle must still be reaped (proving the
// veto did not turn the loop into a no-op).
func TestShouldReapMission(t *testing.T) {
	// The production values, not copies of them: a table built on local
	// constants would keep passing if someone retuned the real ones.
	const (
		timeout = defaultIdleTimeout
		ceiling = idleTimeoutHardCeiling
	)

	tests := []struct {
		name                string
		idleDuration        time.Duration
		isAttached          bool
		claudeState         string
		wrapperUnresponsive bool
		expected            bool
	}{
		{
			name:         "working inside a long tool call: stale log but wrapper says busy, so spared",
			idleDuration: 2 * time.Hour,
			claudeState:  claudeStateBusy,
			expected:     false,
		},
		{
			name:         "turn in flight behind a permission prompt: spared, because needs-attention never means finished",
			idleDuration: 2 * time.Hour,
			claudeState:  claudeStateNeedsAttention,
			expected:     false,
		},
		{
			// The case KEV-71 had to avoid creating: a quota pause observed on
			// a real mission ran 70 minutes before Claude Code resumed by
			// itself. Reaping at 30 minutes would have destroyed that turn.
			name:         "stopped on the usage quota: spared, because Claude Code may resume the turn itself",
			idleDuration: 71 * time.Minute,
			claudeState:  claudeStateQuotaBlocked,
			expected:     false,
		},
		{
			// The quota state is a veto, not an exemption — otherwise a
			// mission whose quota never resets leaks forever.
			name:         "stopped on the usage quota past the hard ceiling: reaped anyway",
			idleDuration: 7 * time.Hour,
			claudeState:  claudeStateQuotaBlocked,
			expected:     true,
		},
		{
			name:         "genuinely idle past the threshold: reaped",
			idleDuration: 31 * time.Minute,
			claudeState:  "idle",
			expected:     true,
		},
		{
			name:         "idle but inside the threshold: spared",
			idleDuration: 29 * time.Minute,
			claudeState:  "idle",
			expected:     false,
		},
		{
			name:         "busy past the hard ceiling: reaped anyway, because the hook-fed state can be wedged",
			idleDuration: 7 * time.Hour,
			claudeState:  claudeStateBusy,
			expected:     true,
		},
		{
			name:         "needs-attention past the hard ceiling: reaped anyway",
			idleDuration: 7 * time.Hour,
			claudeState:  claudeStateNeedsAttention,
			expected:     true,
		},
		{
			name:         "no wrapper state available: falls back to the log's verdict and reaps",
			idleDuration: 31 * time.Minute,
			claudeState:  "",
			expected:     true,
		},
		{
			name:                "live wrapper too slow to answer: decision deferred to the next cycle",
			idleDuration:        31 * time.Minute,
			claudeState:         "",
			wrapperUnresponsive: true,
			expected:            false,
		},
		{
			name:                "unresponsive wrapper past the hard ceiling: reaped anyway",
			idleDuration:        7 * time.Hour,
			claudeState:         "",
			wrapperUnresponsive: true,
			expected:            true,
		},
		{
			name:         "attached mission, idle past the threshold: never reaped while a human is watching",
			idleDuration: 2 * time.Hour,
			isAttached:   true,
			claudeState:  "idle",
			expected:     false,
		},
		{
			name:         "attached mission past the hard ceiling: still never reaped",
			idleDuration: 7 * time.Hour,
			isAttached:   true,
			claudeState:  "idle",
			expected:     false,
		},
		{
			name:         "unrecognized state from a newer wrapper: treated like no state and reaped on the log",
			idleDuration: 31 * time.Minute,
			claudeState:  "something_new",
			expected:     true,
		},
		// The two comparison boundaries. `<` on the threshold and `>=` on the
		// ceiling both live exactly here, so an off-by-one in either would
		// otherwise slip through every row above.
		{
			name:         "exactly at the threshold: reaped, the threshold is not exclusive",
			idleDuration: timeout,
			claudeState:  "idle",
			expected:     true,
		},
		{
			name:         "one nanosecond under the threshold: spared",
			idleDuration: timeout - 1,
			claudeState:  "idle",
			expected:     false,
		},
		{
			name:         "exactly at the ceiling while busy: reaped, the ceiling is not exclusive",
			idleDuration: ceiling,
			claudeState:  claudeStateBusy,
			expected:     true,
		},
		{
			name:         "one nanosecond under the ceiling while busy: still spared",
			idleDuration: ceiling - 1,
			claudeState:  claudeStateBusy,
			expected:     false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := shouldReapMission(
				test.idleDuration,
				timeout,
				ceiling,
				test.isAttached,
				test.claudeState,
				test.wrapperUnresponsive,
			)
			if actual != test.expected {
				t.Errorf("shouldReapMission() = %t, want %t", actual, test.expected)
			}
		})
	}
}

// newIdleTimeoutTestServer builds a server whose agenc directory sits at a
// deliberately short path, because these tests put a real unix socket inside
// the mission directory and macOS caps socket paths at 104 bytes.
//
// That cap rules out both t.TempDir() and os.TempDir(): on macOS $TMPDIR alone
// is around 49 bytes, and "missions/<uuid>/wrapper.sock" adds 58 more, so the
// path overruns the cap before the test has written anything. "/tmp" keeps the
// whole path near 75 bytes. If this is ever changed back, the socket listen in
// startFakeWrapper fails with "bind: invalid argument" and reports the length.
func newIdleTimeoutTestServer(t *testing.T) *Server {
	t.Helper()

	tmpDir, err := os.MkdirTemp("/tmp", "it-")
	if err != nil {
		t.Fatalf("failed to create temp directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	t.Setenv("HOME", tmpDir)

	db, err := database.Open(filepath.Join(tmpDir, "database.sqlite"))
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	srv := &Server{
		agencDirpath: tmpDir,
		logger:       log.New(os.Stderr, "", 0),
		db:           db,
	}
	srv.cachedConfig.Store(&config.AgencConfig{})
	return srv
}

// startFakeWrapper makes a mission look like it has a live wrapper reporting
// claudeState: a sacrificial child process whose PID goes in the mission's PID
// file, plus a unix socket serving the wrapper's /status endpoint. The returned
// channel closes when the child exits, which is how a test observes whether the
// reaper signalled it.
//
// The child must be waited on concurrently. Without that it lingers as a zombie
// after being signalled, which still answers signal 0 — so IsProcessRunning
// would report it alive, stopWrapper would spend its whole timeout waiting, and
// the test would see a reap that worked as a reap that did nothing.
func startFakeWrapper(t *testing.T, s *Server, missionID, claudeState string) <-chan struct{} {
	t.Helper()

	missionDirpath := config.GetMissionDirpath(s.agencDirpath, missionID)
	if err := os.MkdirAll(missionDirpath, 0o755); err != nil {
		t.Fatalf("failed to create mission dir: %v", err)
	}

	// A real process for the reaper to signal. `sleep` exits on SIGTERM, so
	// whether it is still alive after a cycle is a direct read on whether the
	// reaper fired.
	child := exec.Command("sleep", "300")
	if err := child.Start(); err != nil {
		t.Fatalf("failed to start sacrificial child: %v", err)
	}
	pid := child.Process.Pid
	exited := make(chan struct{})
	go func() {
		_ = child.Wait()
		close(exited)
	}()
	t.Cleanup(func() { _ = child.Process.Kill() })

	pidFilepath := config.GetMissionPIDFilepath(s.agencDirpath, missionID)
	if err := os.WriteFile(pidFilepath, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		t.Fatalf("failed to write pid file: %v", err)
	}

	socketFilepath := config.GetMissionSocketFilepath(s.agencDirpath, missionID)
	listener, err := net.Listen("unix", socketFilepath)
	if err != nil {
		t.Fatalf("failed to listen on wrapper socket %q (%d bytes): %v", socketFilepath, len(socketFilepath), err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(wrapperStatusResponse{ClaudeState: claudeState})
	})
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	return exited
}

// backdateConversationLog gives a mission a conversation log whose mtime is
// `age` old, which is what missionIdleDuration reads to compute idleness.
func backdateConversationLog(t *testing.T, s *Server, missionID string, age time.Duration) {
	t.Helper()

	agentDirpath := config.GetMissionAgentDirpath(s.agencDirpath, missionID)
	projectDirpath, err := claudeconfig.ComputeProjectDirpath(agentDirpath)
	if err != nil {
		t.Fatalf("ComputeProjectDirpath failed: %v", err)
	}
	if err := os.MkdirAll(projectDirpath, 0o755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}
	jsonlFilepath := filepath.Join(projectDirpath, "session.jsonl")
	if err := os.WriteFile(jsonlFilepath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("failed to write JSONL: %v", err)
	}
	stale := time.Now().Add(-age)
	if err := os.Chtimes(jsonlFilepath, stale, stale); err != nil {
		t.Fatalf("failed to backdate JSONL: %v", err)
	}
}

// TestRunIdleTimeoutCycle_SparesWorkingMission is the regression test for the
// reported bug, exercised through the real cycle rather than the predicate
// alone — so it fails if the cycle ever stops consulting the wrapper, which a
// predicate-only test cannot detect.
func TestRunIdleTimeoutCycle_SparesWorkingMission(t *testing.T) {
	s := newIdleTimeoutTestServer(t)

	m, err := s.db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	exited := startFakeWrapper(t, s, m.ID, claudeStateBusy)
	backdateConversationLog(t, s, m.ID, 2*time.Hour)

	s.runIdleTimeoutCycle()

	select {
	case <-exited:
		t.Error("reaped a mission whose wrapper reported busy; a long tool call must not look idle")
	default:
	}
}

// TestRunIdleTimeoutCycle_ReapsIdleMission is the guard that makes the test
// above mean something. If the setup silently failed to look idle, or the veto
// were applied unconditionally, this test fails instead of passing vacuously.
func TestRunIdleTimeoutCycle_ReapsIdleMission(t *testing.T) {
	s := newIdleTimeoutTestServer(t)

	m, err := s.db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	exited := startFakeWrapper(t, s, m.ID, "idle")
	backdateConversationLog(t, s, m.ID, 2*time.Hour)

	s.runIdleTimeoutCycle()

	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Error("did not reap a mission whose wrapper reported idle past the threshold")
	}
}
