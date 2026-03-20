package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/server"
)

var serverStartCmd = &cobra.Command{
	Use:   startCmdStr,
	Short: "Start the AgenC server",
	RunE:  runServerStart,
}

func init() {
	serverCmd.AddCommand(serverStartCmd)
}

func runServerStart(cmd *cobra.Command, args []string) error {
	agencDirpath, err := ensureConfigured()
	if err != nil {
		return err
	}
	return forkServer(agencDirpath)
}

func forkServer(agencDirpath string) error {
	pidFilepath := config.GetServerPIDFilepath(agencDirpath)
	logFilepath := config.GetServerLogFilepath(agencDirpath)

	if server.IsRunning(pidFilepath) {
		pid, _ := server.ReadPID(pidFilepath)
		fmt.Printf("Server is already running (PID %d).\n", pid)
		return nil
	}

	if err := server.ForkServer(logFilepath, pidFilepath); err != nil {
		return stacktrace.Propagate(err, "failed to fork server")
	}

	socketFilepath := config.GetServerSocketFilepath(agencDirpath)
	if err := server.WaitForReady(socketFilepath); err != nil {
		return stacktrace.Propagate(err, "server process started but failed to become ready")
	}

	newPID, _ := server.ReadPID(pidFilepath)
	fmt.Printf("Server started (PID %d).\n", newPID)

	return nil
}

// ensureServerRunning idempotently starts the server if not already running,
// and waits for it to be ready to accept connections. Resolves the agenc
// directory path internally via config.GetAgencDirpath().
func ensureServerRunning() {
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return
	}
	cleanupDaemonDir(agencDirpath)
	pidFilepath := config.GetServerPIDFilepath(agencDirpath)
	if server.IsRunning(pidFilepath) {
		return
	}
	logFilepath := config.GetServerLogFilepath(agencDirpath)
	if err := server.ForkServer(logFilepath, pidFilepath); err != nil {
		return
	}
	socketFilepath := config.GetServerSocketFilepath(agencDirpath)
	_ = server.WaitForReady(socketFilepath)
}
