package server

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newShutdownTestServer returns a Server serving a fresh agenc directory, with
// the startup identity of that directory, and its last request stamped at
// lastRequestAt.
func newShutdownTestServer(t *testing.T, lastRequestAt time.Time) (*Server, os.FileInfo) {
	t.Helper()
	agencDirpath := filepath.Join(t.TempDir(), "agenc")
	if err := os.MkdirAll(agencDirpath, 0755); err != nil {
		t.Fatalf("failed to create agenc directory: %v", err)
	}
	info, err := os.Stat(agencDirpath)
	if err != nil {
		t.Fatalf("failed to stat agenc directory: %v", err)
	}
	srv := &Server{agencDirpath: agencDirpath, logger: log.New(os.Stderr, "", 0)}
	srv.lastRequestAt.Store(lastRequestAt.UnixNano())
	return srv, info
}

func TestTestEnvShutdownReason(t *testing.T) {
	now := time.Now()
	quietPeriod := 10 * time.Minute

	t.Run("recent request keeps the server running", func(t *testing.T) {
		srv, info := newShutdownTestServer(t, now.Add(-quietPeriod/2))
		if reason := srv.testEnvShutdownReason(info, now, quietPeriod); reason != "" {
			t.Fatalf("expected no shutdown, got %q", reason)
		}
	})

	t.Run("quiet past the threshold shuts down", func(t *testing.T) {
		srv, info := newShutdownTestServer(t, now.Add(-quietPeriod-time.Second))
		reason := srv.testEnvShutdownReason(info, now, quietPeriod)
		if !strings.Contains(reason, "no requests") {
			t.Fatalf("expected a quiet shutdown reason, got %q", reason)
		}
	})

	t.Run("an in-flight request keeps a quiet server running", func(t *testing.T) {
		srv, info := newShutdownTestServer(t, now.Add(-2*quietPeriod))
		srv.inFlightRequests.Add(1)
		if reason := srv.testEnvShutdownReason(info, now, quietPeriod); reason != "" {
			t.Fatalf("expected no shutdown while a request is in flight, got %q", reason)
		}
	})

	t.Run("removed agenc directory shuts down despite recent requests", func(t *testing.T) {
		srv, info := newShutdownTestServer(t, now)
		if err := os.RemoveAll(srv.agencDirpath); err != nil {
			t.Fatalf("failed to remove agenc directory: %v", err)
		}
		reason := srv.testEnvShutdownReason(info, now, quietPeriod)
		if !strings.Contains(reason, "was removed") {
			t.Fatalf("expected a removed-directory reason, got %q", reason)
		}
	})

	// `make test-env-clean && make test-env` recreates the same path; the old
	// server must not adopt the new directory as its own.
	t.Run("agenc directory recreated at the same path shuts down", func(t *testing.T) {
		srv, info := newShutdownTestServer(t, now)
		if err := os.RemoveAll(srv.agencDirpath); err != nil {
			t.Fatalf("failed to remove agenc directory: %v", err)
		}
		if err := os.MkdirAll(srv.agencDirpath, 0755); err != nil {
			t.Fatalf("failed to recreate agenc directory: %v", err)
		}
		reason := srv.testEnvShutdownReason(info, now, quietPeriod)
		if !strings.Contains(reason, "replaced") {
			t.Fatalf("expected a replaced-directory reason, got %q", reason)
		}
	})
}

func TestTrackRequestActivity(t *testing.T) {
	srv, _ := newShutdownTestServer(t, time.Time{})

	var inFlightDuringRequest int64
	handler := srv.trackRequestActivity(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inFlightDuringRequest = srv.inFlightRequests.Load()
	}))

	before := time.Now()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	if inFlightDuringRequest != 1 {
		t.Fatalf("expected 1 request in flight while serving, got %d", inFlightDuringRequest)
	}
	if got := srv.inFlightRequests.Load(); got != 0 {
		t.Fatalf("expected 0 requests in flight after serving, got %d", got)
	}
	if last := time.Unix(0, srv.lastRequestAt.Load()); last.Before(before) {
		t.Fatalf("lastRequestAt %v was not updated by a request served after %v", last, before)
	}
}

func TestRunTestEnvShutdownLoopShutsDownWhenDirectoryRemoved(t *testing.T) {
	srv, _ := newShutdownTestServer(t, time.Now())
	ctx, shutdown := context.WithCancel(t.Context())
	defer shutdown()

	loopDone := make(chan struct{})
	go func() {
		srv.runTestEnvShutdownLoop(ctx, shutdown)
		close(loopDone)
	}()
	// The loop records the directory's identity on entry; removing it first
	// would test the "cannot stat at startup" path instead.
	time.Sleep(200 * time.Millisecond)

	if err := os.RemoveAll(srv.agencDirpath); err != nil {
		t.Fatalf("failed to remove agenc directory: %v", err)
	}

	select {
	case <-ctx.Done():
	case <-time.After(testEnvShutdownCheckInterval + 5*time.Second):
		t.Fatal("server did not shut itself down after its agenc directory was removed")
	}
	<-loopDone
}
