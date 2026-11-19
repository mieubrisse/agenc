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
	// Sized against `make e2e`: the longest deliberate gap in the suite is a
	// 90-second sleep in the cron-monitoring section, and that section asserts
	// on the server's in-memory state across the sleep, so a server that exited
	// mid-gap would fail the suite rather than merely restart. Ten minutes is
	// more than six times that gap. Exiting early is otherwise cheap — the next
	// CLI call auto-starts a fresh server — so the period only has to be long
	// enough not to drop in-memory state a caller is relying on.
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
	agencDirInfo, err := os.Stat(s.agencDirpath)
	if err != nil {
		s.logger.Printf("Test-env shutdown: cannot stat agenc directory '%s', self-shutdown disabled: %v", s.agencDirpath, err)
		return
	}

	ticker := time.NewTicker(testEnvShutdownCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reason := s.testEnvShutdownReason(agencDirInfo, time.Now(), testEnvQuietShutdownPeriod)
			if reason != "" {
				s.logger.Printf("Test-env server shutting itself down: %s", reason)
				shutdown()
				return
			}
		}
	}
}

// testEnvShutdownReason returns why a test-environment server should exit now,
// or the empty string if it should keep running. agencDirInfo is the agenc
// directory as it was when the server started.
//
// The directory is compared by identity, not only by existence: `make
// test-env-clean && make test-env` recreates the same path, and a server still
// serving the deleted directory must not mistake the new one for its own.
func (s *Server) testEnvShutdownReason(agencDirInfo os.FileInfo, now time.Time, quietPeriod time.Duration) string {
	currentInfo, err := os.Stat(s.agencDirpath)
	if os.IsNotExist(err) {
		return fmt.Sprintf("agenc directory '%s' was removed", s.agencDirpath)
	}
	if err == nil && !os.SameFile(agencDirInfo, currentInfo) {
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
