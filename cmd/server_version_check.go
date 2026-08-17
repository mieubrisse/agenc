package cmd

import (
	"fmt"
	"os"
	"sync"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/server"
	"github.com/odyssey/agenc/internal/version"
)

// serverVersionWarnMu guards serverVersionWarned so the stale-server warning is
// printed at most once per CLI invocation, even though checkServerVersion runs
// on every server-touching command (via serverClient) and every config read.
var (
	serverVersionWarnMu sync.Mutex
	serverVersionWarned bool
)

// checkServerVersion compares the running server's version against the CLI
// version and, when they differ, prints a warning (at most once per process).
// Also stops any stale daemon process from a pre-server version. All errors are
// silently ignored — this check must never block CLI commands.
func checkServerVersion(agencDirpath string) {
	// Clean up any leftover daemon directory from a pre-server version of agenc.
	cleanupDaemonDir(agencDirpath)

	pidFilepath := config.GetServerPIDFilepath(agencDirpath)

	if !server.IsRunning(pidFilepath) {
		return
	}

	// Use the health endpoint to get the server version
	socketFilepath := config.GetServerSocketFilepath(agencDirpath)
	client := server.NewClient(socketFilepath)

	var healthResp struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := client.Get("/health", &healthResp); err != nil {
		return
	}

	warning := serverVersionWarning(healthResp.Version, version.Version)
	if warning == "" {
		return
	}

	serverVersionWarnMu.Lock()
	defer serverVersionWarnMu.Unlock()
	if serverVersionWarned {
		return
	}
	serverVersionWarned = true
	fmt.Fprintln(os.Stderr, warning)
}

// serverVersionWarning returns the stale-server warning to show the user, or an
// empty string when the running server matches the CLI and no warning is needed.
// An empty serverVersion means the server is too old to report one at all (it
// predates the /health version field) — itself a stale signal worth warning on,
// and exactly the case a naive equality check silently skips.
func serverVersionWarning(serverVersion, cliVersion string) string {
	if serverVersion == cliVersion {
		return ""
	}
	if serverVersion == "" {
		return fmt.Sprintf("⚠ A stale agenc server is running (it predates version reporting; CLI is %s). Run 'agenc server restart' to upgrade.", cliVersion)
	}
	return fmt.Sprintf("⚠ Server is running %s but CLI is %s. Run 'agenc server restart' to upgrade.", serverVersion, cliVersion)
}

// stopStaleDaemon stops any leftover daemon process from a pre-server version
// of agenc. After a Homebrew upgrade, the old daemon PID file may still exist
// with a running process. This function sends SIGTERM and cleans up. All errors
// are silently ignored.
func stopStaleDaemon(agencDirpath string) {
	daemonPIDFilepath := config.GetDaemonPIDFilepath(agencDirpath) //nolint:staticcheck // intentional: cleaning up deprecated daemon artifacts

	if !server.IsRunning(daemonPIDFilepath) {
		return
	}

	// Reuse StopServer — it works with any PID file (SIGTERM → poll → SIGKILL).
	_ = server.StopServer(daemonPIDFilepath)
}

// cleanupDaemonDir removes the legacy daemon/ directory from the agenc root.
// This cleans up files left behind by pre-server versions of agenc. All errors
// are silently ignored — cleanup must never block server start.
func cleanupDaemonDir(agencDirpath string) {
	stopStaleDaemon(agencDirpath)
	daemonDirpath := config.GetDaemonDirpath(agencDirpath) //nolint:staticcheck // intentional: cleaning up deprecated daemon artifacts
	_ = os.RemoveAll(daemonDirpath)
}
