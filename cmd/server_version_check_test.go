package cmd

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

// TestServerVersionWarning_MatchIsSilent verifies that a running server whose
// version matches the CLI produces no warning.
func TestServerVersionWarning_MatchIsSilent(t *testing.T) {
	if warning := serverVersionWarning("0.13.1", "0.13.1"); warning != "" {
		t.Errorf("expected no warning when versions match, got: %q", warning)
	}
}

// TestServerVersionWarning_MismatchWarns verifies that a version mismatch warns
// and names both versions plus the restart remedy.
func TestServerVersionWarning_MismatchWarns(t *testing.T) {
	warning := serverVersionWarning("0.13.0", "0.13.1")
	if warning == "" {
		t.Fatal("expected a warning when server and CLI versions differ, got none")
	}
	for _, want := range []string{"0.13.0", "0.13.1", "agenc server restart"} {
		if !strings.Contains(warning, want) {
			t.Errorf("warning %q does not contain %q", warning, want)
		}
	}
}

// TestServerVersionWarning_EmptyServerVersionWarns is the regression guard for
// GH #30: a running server too old to report a version (empty string) is the
// exact stale case the previous naive equality check silently skipped. It must
// still warn.
func TestServerVersionWarning_EmptyServerVersionWarns(t *testing.T) {
	warning := serverVersionWarning("", "0.13.1")
	if warning == "" {
		t.Fatal("expected a warning when the running server reports no version, got none")
	}
	for _, want := range []string{"0.13.1", "agenc server restart"} {
		if !strings.Contains(warning, want) {
			t.Errorf("warning %q does not contain %q", warning, want)
		}
	}
}

// TestCheckServerVersion_WarnsOncePerProcessAgainstStaleServer is the wiring +
// warn-once regression guard for GH #30. The original bug was that the warning
// function existed but the mission-create call site did not — a defect no
// pure-helper test can catch. This drives checkServerVersion end-to-end against
// a fake server reporting a mismatched version, and asserts the warning fires
// exactly once across repeated calls (mission creation calls serverClient
// several times per command).
func TestCheckServerVersion_WarnsOncePerProcessAgainstStaleServer(t *testing.T) {
	// Short temp dir (not t.TempDir, whose long test-name path can exceed the
	// ~104-byte unix socket limit on macOS).
	agencDirpath, err := os.MkdirTemp("", "av")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(agencDirpath) })

	// Make IsRunning() report a live server by pointing the PID file at this
	// test process (signal-0 to self succeeds).
	pidFilepath := config.GetServerPIDFilepath(agencDirpath)
	if err := os.MkdirAll(filepath.Dir(pidFilepath), 0755); err != nil {
		t.Fatalf("mkdir pid dir: %v", err)
	}
	if err := os.WriteFile(pidFilepath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	// Serve a fake /health reporting a version different from the CLI's, over the
	// exact unix socket the client will dial.
	socketFilepath := config.GetServerSocketFilepath(agencDirpath)
	if err := os.MkdirAll(filepath.Dir(socketFilepath), 0755); err != nil {
		t.Fatalf("mkdir socket dir: %v", err)
	}
	listener, err := net.Listen("unix", socketFilepath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": "0.0.1-test-stale"})
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close() })

	// Reset the process-global warn-once guard so the test is deterministic and
	// does not leak state to other tests.
	resetServerVersionWarned := func() {
		serverVersionWarnMu.Lock()
		serverVersionWarned = false
		serverVersionWarnMu.Unlock()
	}
	resetServerVersionWarned()
	t.Cleanup(resetServerVersionWarned)

	// Capture stderr for the duration of the calls.
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	checkServerVersion(agencDirpath)
	checkServerVersion(agencDirpath)

	os.Stderr = origStderr
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}

	got := string(out)
	if n := strings.Count(got, "agenc server restart"); n != 1 {
		t.Errorf("expected exactly one stale-server warning across two calls, got %d; stderr: %q", n, got)
	}
	if !strings.Contains(got, "0.0.1-test-stale") {
		t.Errorf("warning should name the stale server version; stderr: %q", got)
	}
}
