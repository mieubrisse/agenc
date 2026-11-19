package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/odyssey/agenc/internal/config"
)

const (
	// testEnvQuietShutdownPeriod is how long a test-environment server may go
	// without serving a single request before it exits. Every live wrapper
	// heartbeats every 10 seconds, so a server with any running mission is
	// never quiet by this measure; quiet means nothing is left to serve.
	//
	// Sized against `make e2e`. Measured across a full run (KEV-79, 2026-11-19:
	// 1,465 requests over 322 seconds), the longest gap between two requests
	// was 3.4 seconds — wrappers heartbeat through the suite's own sleeps, the
	// longest of which is 90 seconds. Ten minutes leaves more than six times
	// that sleep even if no wrapper were running. Exiting mid-suite would not
	// lose data — the next CLI call auto-starts a fresh server, and cron monitor
	// state lives in the database — but it would drop in-memory state such as
	// queued async reloads, and the fresh server inherits the environment of
	// whichever command forked it, which some sections control deliberately.
	testEnvQuietShutdownPeriod = 10 * time.Minute

	// testEnvShutdownCheckInterval is how often a test-environment server checks
	// whether it should exit. It bounds how long a server outlives the removal
	// of its agenc directory.
	testEnvShutdownCheckInterval = 15 * time.Second
)

// trackRequestActivity wraps the server's handler to record when the server
// last served a request and how many are still in flight. The timestamp is
// stamped at both start and end, so a long-running request (a mission create
// cloning a repo) never makes the server look quiet while it is being served.
func (s *Server) trackRequestActivity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.inFlightRequests.Add(1)
		s.lastRequestAt.Store(time.Now().UnixNano())
		defer func() {
			s.lastRequestAt.Store(time.Now().UnixNano())
			s.inFlightRequests.Add(-1)
		}()
		next.ServeHTTP(w, r)
	})
}

// runTestEnvShutdownLoop exits a test-environment server once it has nothing
// left to serve, by calling shutdown. A throwaway AGENC_DIRPATH starts a server
// on the first CLI call that needs one, and before KEV-79 nothing ever stopped
// it, so every abandoned test environment left a permanent daemon behind.
//
// Scoped to the test environment. The production server owns cron syncing,
// config watching, writeable-copy reconciliation and pane reconciliation, none
// of which shows up as request traffic; exiting it on quiet would silently
// stop all of them.
func (s *Server) runTestEnvShutdownLoop(ctx context.Context, shutdown context.CancelFunc) {
	ticker := time.NewTicker(testEnvShutdownCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reason := s.testEnvShutdownReason(time.Now(), testEnvQuietShutdownPeriod)
			if reason != "" {
				s.logger.Printf("Test-env server shutting itself down: %s", reason)
				shutdown()
				return
			}
		}
	}
}

// testEnvShutdownReason returns why a test-environment server should exit now,
// or the empty string if it should keep running.
//
// The directory is compared by identity, not only by existence: `make
// test-env-clean && make test-env` recreates the same path, and a server still
// serving the deleted directory must not mistake the new one for its own.
func (s *Server) testEnvShutdownReason(now time.Time, quietPeriod time.Duration) string {
	if _, err := os.Stat(s.agencDirpath); os.IsNotExist(err) {
		return fmt.Sprintf("agenc directory '%s' was removed", s.agencDirpath)
	}
	if !s.OwnsAgencDir() {
		return fmt.Sprintf("agenc directory '%s' was replaced by a different directory", s.agencDirpath)
	}

	if s.inFlightRequests.Load() > 0 {
		return ""
	}
	quietFor := now.Sub(time.Unix(0, s.lastRequestAt.Load()))
	if quietFor >= quietPeriod {
		return fmt.Sprintf("no requests for %s (threshold %s)", quietFor.Round(time.Second), quietPeriod)
	}
	return ""
}

// startTestEnvShutdownLoopIfApplicable starts runTestEnvShutdownLoop when this
// server serves a test environment, and does nothing otherwise.
func (s *Server) startTestEnvShutdownLoopIfApplicable(ctx context.Context, wg *sync.WaitGroup, shutdown context.CancelFunc) {
	if !config.IsTestEnv() {
		return
	}
	go s.runLoop("test-env-shutdown", wg, ctx, func(ctx context.Context) {
		s.runTestEnvShutdownLoop(ctx, shutdown)
	})
}
