package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/kurtosis-tech/stacktrace"
)

const (
	daemonEnvVar    = "AGENC_DAEMON_PROCESS"
	stopPollTimeout = 3 * time.Second
	stopPollTick    = 100 * time.Millisecond
)

// IsDaemonProcess returns true if this process was launched as the daemon child.
func IsDaemonProcess() bool {
	return os.Getenv(daemonEnvVar) == "1"
}

// ForkDaemon re-executes the current binary as a background daemon process.
// The child's stdout/stderr are redirected to logFilepath, and its PID is
// written to pidFilepath.
func ForkDaemon(logFilepath string, pidFilepath string) error {
	executableFilepath, err := os.Executable()
	if err != nil {
		return stacktrace.Propagate(err, "failed to determine executable path")
	}

	logFile, err := os.OpenFile(logFilepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open daemon log file")
	}

	cmd := exec.Command(executableFilepath, "daemon", "start")
	cmd.Env = append(os.Environ(), fmt.Sprintf("%s=1", daemonEnvVar))
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return stacktrace.Propagate(err, "failed to start daemon process")
	}

	pid := cmd.Process.Pid
	logFile.Close()

	if err := os.WriteFile(pidFilepath, []byte(strconv.Itoa(pid)), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write PID file")
	}

	// Detach — we don't wait for the child
	if err := cmd.Process.Release(); err != nil {
		return stacktrace.Propagate(err, "failed to release daemon process")
	}

	return nil
}

// ReadPID reads the daemon PID from the PID file. Returns 0 if the file
// does not exist or is empty.
func ReadPID(pidFilepath string) (int, error) {
	data, err := os.ReadFile(pidFilepath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, stacktrace.Propagate(err, "failed to read PID file")
	}

	pidStr := strings.TrimSpace(string(data))
	if pidStr == "" {
		return 0, nil
	}

	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, stacktrace.Propagate(err, "invalid PID in file: '%s'", pidStr)
	}

	return pid, nil
}

// IsProcessRunning checks if a process with the given PID is running.
func IsProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks if process exists without actually sending a signal
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// StopDaemon sends SIGTERM to the daemon process and waits for it to exit.
// Cleans up the PID file afterward.
func StopDaemon(pidFilepath string) error {
	pid, err := ReadPID(pidFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read daemon PID")
	}

	if pid == 0 || !IsProcessRunning(pid) {
		// Clean up stale PID file if it exists
		os.Remove(pidFilepath)
		return stacktrace.NewError("daemon is not running")
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return stacktrace.Propagate(err, "failed to find daemon process")
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return stacktrace.Propagate(err, "failed to send SIGTERM to daemon (PID %d)", pid)
	}

	// Poll until the process exits or we time out
	deadline := time.Now().Add(stopPollTimeout)
	for time.Now().Before(deadline) {
		if !IsProcessRunning(pid) {
			os.Remove(pidFilepath)
			return nil
		}
		time.Sleep(stopPollTick)
	}

	// Force kill if still running
	_ = process.Signal(syscall.SIGKILL)
	os.Remove(pidFilepath)

	return nil
}
