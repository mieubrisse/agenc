package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/daemon"
	"github.com/odyssey/agenc/internal/version"
)

// checkDaemonVersion compares the running daemon's version against the CLI
// version. If the CLI is newer by semver, it gracefully restarts the daemon.
// If versions differ but can't be compared by semver, it prints a warning.
// All errors are silently ignored — this check must never block CLI commands.
func checkDaemonVersion(agencDirpath string) {
	pidFilepath := config.GetDaemonPIDFilepath(agencDirpath)

	pid, err := daemon.ReadPID(pidFilepath)
	if err != nil || pid == 0 || !daemon.IsProcessRunning(pid) {
		return
	}

	versionFilepath := config.GetDaemonVersionFilepath(agencDirpath)
	raw, err := os.ReadFile(versionFilepath)
	if err != nil {
		return
	}
	daemonVersion := strings.TrimSpace(string(raw))

	cliVersion := version.Version
	if daemonVersion == cliVersion {
		return
	}

	if semver.IsValid(cliVersion) && semver.IsValid(daemonVersion) {
		if semver.Compare(cliVersion, daemonVersion) > 0 {
			restartDaemon(agencDirpath, daemonVersion, cliVersion)
		}
		return
	}

	fmt.Fprintf(os.Stderr, "Warning: daemon version (%s) does not match CLI version (%s). "+
		"Run 'agenc daemon restart' to restart the daemon.\n", daemonVersion, cliVersion)
}

// restartDaemon stops the running daemon and starts a new one, printing a
// notice to stderr.
func restartDaemon(agencDirpath string, oldVersion string, newVersion string) {
	pidFilepath := config.GetDaemonPIDFilepath(agencDirpath)
	logFilepath := config.GetDaemonLogFilepath(agencDirpath)

	if err := daemon.StopDaemon(pidFilepath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to stop daemon for upgrade: %v\n", err)
		return
	}

	if err := daemon.ForkDaemon(logFilepath, pidFilepath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to start daemon after upgrade: %v\n", err)
		return
	}

	fmt.Fprintf(os.Stderr, "Daemon restarted: %s → %s\n", oldVersion, newVersion)
}

// isUnderDaemonCmd returns true if cmd is the daemon command or any of its
// subcommands.
func isUnderDaemonCmd(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c == daemonCmd {
			return true
		}
	}
	return false
}
