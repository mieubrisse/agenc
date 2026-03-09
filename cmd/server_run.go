package cmd

import (
	"context"
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

var serverRunCmd = &cobra.Command{
	Use:    runCmdStr,
	Short:  "Run the AgenC server in the foreground",
	Hidden: true,
	RunE:   runServerRun,
}

func init() {
	serverCmd.AddCommand(serverRunCmd)
}

// runServerRun is the actual server process. It is invoked by ForkServer as a
// detached child — not intended for direct user invocation.
func runServerRun(cmd *cobra.Command, args []string) error {
	if _, err := ensureConfigured(); err != nil {
		return err
	}

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
