package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/kurtosis-tech/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/daemon"
	"github.com/odyssey/agenc/internal/database"
)

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the background daemon",
	RunE:  runDaemonStart,
}

func init() {
	daemonCmd.AddCommand(daemonStartCmd)
}

func runDaemonStart(cmd *cobra.Command, args []string) error {
	if daemon.IsDaemonProcess() {
		return runDaemonLoop()
	}
	return forkDaemon()
}

// runDaemonLoop is the actual daemon child process. It opens the DB, sets up
// signal handling, and runs the daemon loop until SIGTERM/SIGINT.
func runDaemonLoop() error {
	pidFilepath := config.GetDaemonPIDFilepath(agencDirpath)

	// Write our own PID (the forked child PID file is written by the parent,
	// but this ensures accuracy if started directly)
	if err := os.WriteFile(pidFilepath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write PID file")
	}

	logFilepath := config.GetDaemonLogFilepath(agencDirpath)
	logFile, err := os.OpenFile(logFilepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open log file")
	}
	defer logFile.Close()

	logger := log.New(logFile, "", log.LstdFlags)

	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open database")
	}
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		logger.Printf("Received signal: %v", sig)
		cancel()
	}()

	d := daemon.NewDaemon(db, logger)
	d.Run(ctx)

	// Clean up PID file on graceful shutdown
	os.Remove(pidFilepath)
	logger.Println("Daemon exited")

	return nil
}

// forkDaemon is the parent process path — it forks off a background daemon.
func forkDaemon() error {
	pidFilepath := config.GetDaemonPIDFilepath(agencDirpath)
	logFilepath := config.GetDaemonLogFilepath(agencDirpath)

	pid, err := daemon.ReadPID(pidFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to check existing daemon")
	}

	if pid > 0 && daemon.IsProcessRunning(pid) {
		fmt.Printf("Daemon is already running (PID %d).\n", pid)
		return nil
	}

	if err := daemon.ForkDaemon(logFilepath, pidFilepath); err != nil {
		return stacktrace.Propagate(err, "failed to fork daemon")
	}

	newPID, _ := daemon.ReadPID(pidFilepath)
	fmt.Printf("Daemon started (PID %d).\n", newPID)

	return nil
}

// ensureDaemonRunning idempotently starts the daemon if it's not already running.
// This is safe to call from any command (e.g., mission new).
func ensureDaemonRunning(agencDirpath string) {
	pidFilepath := config.GetDaemonPIDFilepath(agencDirpath)
	logFilepath := config.GetDaemonLogFilepath(agencDirpath)

	pid, err := daemon.ReadPID(pidFilepath)
	if err != nil {
		return
	}

	if pid > 0 && daemon.IsProcessRunning(pid) {
		return
	}

	_ = daemon.ForkDaemon(logFilepath, pidFilepath)
}
