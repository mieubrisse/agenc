package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

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
	// Use ensureConfigured directly — skip the version check that
	// getAgencContext performs, since we're starting/managing the server ourselves.
	if _, err := ensureConfigured(); err != nil {
		return err
	}
	if server.IsServerProcess() {
		return runServerLoop()
	}
	return forkServer()
}

func runServerLoop() error {
	pidFilepath := config.GetServerPIDFilepath(agencDirpath)

	if err := os.WriteFile(pidFilepath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write PID file")
	}

	logFilepath := config.GetServerLogFilepath(agencDirpath)
	logFile, err := os.OpenFile(logFilepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open log file")
	}
	defer logFile.Close()

	logger := log.New(logFile, "", log.LstdFlags)

	socketFilepath := config.GetServerSocketFilepath(agencDirpath)
	srv := server.NewServer(agencDirpath, socketFilepath, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		logger.Printf("Received signal: %v", sig)
		cancel()
	}()

	if err := srv.Run(ctx); err != nil {
		return err
	}

	os.Remove(pidFilepath)
	logger.Println("Server exited")

	return nil
}

func forkServer() error {
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
// and waits for it to be ready to accept connections.
func ensureServerRunning(agencDirpath string) {
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
