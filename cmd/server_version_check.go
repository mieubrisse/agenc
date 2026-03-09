package cmd

import (
	"fmt"
	"os"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/server"
	"github.com/odyssey/agenc/internal/version"
)

// checkServerVersion compares the running server's version against the CLI
// version. If the versions differ, it prints a warning. Also stops any stale
// daemon process from a pre-server version. All errors are silently ignored —
// this check must never block CLI commands.
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

	serverVersion := healthResp.Version
	cliVersion := version.Version
	if serverVersion == cliVersion || serverVersion == "" {
		return
	}

	fmt.Fprintf(os.Stderr, "⚠ Server is running %s but CLI is %s. Run 'agenc server restart' to upgrade.\n", serverVersion, cliVersion)
}

// stopStaleDaemon stops any leftover daemon process from a pre-server version
// of agenc. After a Homebrew upgrade, the old daemon PID file may still exist
// with a running process. This function sends SIGTERM and cleans up. All errors
// are silently ignored.
func stopStaleDaemon(agencDirpath string) {
	daemonPIDFilepath := config.GetDaemonPIDFilepath(agencDirpath)

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
	daemonDirpath := config.GetDaemonDirpath(agencDirpath)
	_ = os.RemoveAll(daemonDirpath)
}
