package cmd

import (
	"fmt"
	"os"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/server"
	"github.com/odyssey/agenc/internal/version"
)

// checkServerVersion compares the running server's version against the CLI
// version. If the versions differ, it restarts the server so background loops
// pick up the new binary. Also stops any stale daemon process from a pre-server
// version. All errors are silently ignored — this check must never block CLI
// commands.
func checkServerVersion(agencDirpath string) {
	// Stop any leftover daemon from a pre-server version of agenc.
	// After upgrade, the daemon PID file may still exist with a running process.
	stopStaleDaemon(agencDirpath)

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

	// Versions differ — restart the server
	restartServer(agencDirpath, serverVersion, cliVersion)
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

// restartServer stops the running server and starts a new one, printing a
// notice to stderr.
func restartServer(agencDirpath string, oldVersion string, newVersion string) {
	pidFilepath := config.GetServerPIDFilepath(agencDirpath)
	logFilepath := config.GetServerLogFilepath(agencDirpath)

	if err := server.StopServer(pidFilepath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to stop server for upgrade: %v\n", err)
		return
	}

	if err := server.ForkServer(logFilepath, pidFilepath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to start server after upgrade: %v\n", err)
		return
	}

	fmt.Fprintf(os.Stderr, "Server restarted: %s → %s\n", oldVersion, newVersion)
}
