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
// pick up the new binary. All errors are silently ignored — this check must
// never block CLI commands.
func checkServerVersion(agencDirpath string) {
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
