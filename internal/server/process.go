package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mieubrisse/stacktrace"
)

// ErrServerLocked is returned when another server process holds the lock.
var ErrServerLocked = errors.New("another server is already running")

const (
	stopPollTimeout  = 3 * time.Second
	stopPollTick     = 100 * time.Millisecond
	readyPollTimeout = 5 * time.Second
	readyPollTick    = 100 * time.Millisecond
)

// tryAcquireServerLock attempts to acquire an exclusive flock on the given lock
// file. Returns the open file handle (caller must defer Close) on success, or
// ErrServerLocked if another process holds the lock. Any other error indicates
// a filesystem problem.
func tryAcquireServerLock(lockFilepath string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(lockFilepath), 0755); err != nil {
		return nil, stacktrace.Propagate(err, "failed to create lock file directory")
	}

	f, err := os.OpenFile(lockFilepath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to open lock file")
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) //nolint:gosec // G115: file descriptor fits in int
	if err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrServerLocked
		}
		return nil, stacktrace.Propagate(err, "failed to acquire lock")
	}

	return f, nil
}

// ForkServer re-executes the current binary as a background server process.
// The child's stdout/stderr are redirected to logFilepath, and its PID is
// written to pidFilepath.
func ForkServer(logFilepath string, pidFilepath string) error {
	executableFilepath, err := os.Executable()
	if err != nil {
		return stacktrace.Propagate(err, "failed to determine executable path")
	}

	logFile, err := os.OpenFile(logFilepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open server log file")
	}

	cmd := exec.Command(executableFilepath, "server", "run")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return stacktrace.Propagate(err, "failed to start server process")
	}

	pid := cmd.Process.Pid
	logFile.Close()

	if err := os.WriteFile(pidFilepath, []byte(strconv.Itoa(pid)), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write PID file")
	}

	// Detach — we don't wait for the child
	if err := cmd.Process.Release(); err != nil {
		return stacktrace.Propagate(err, "failed to release server process")
	}

	return nil
}

// ReadPID reads the server PID from the PID file. Returns 0 if the file
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
	return process.Signal(syscall.Signal(0)) == nil
}

// IsRunning checks if the server process is running.
func IsRunning(pidFilepath string) bool {
	pid, err := ReadPID(pidFilepath)
	if err != nil || pid == 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

// StopServer sends SIGTERM to the server process from the PID file, then sweeps
// for any orphaned server processes and kills those too. Tolerant of missing or
// stale PID files — the orphan sweep runs regardless.
func StopServer(pidFilepath string) error {
	killedPIDs := map[int]bool{os.Getpid(): true}

	pid, _ := ReadPID(pidFilepath)
	if pid > 0 && IsProcessRunning(pid) {
		killProcess(pid)
		killedPIDs[pid] = true
	}

	_ = os.Remove(pidFilepath)

	// Sweep for orphaned server processes
	orphans := findOrphanServerPIDs(killedPIDs)
	for _, orphanPID := range orphans {
		killProcess(orphanPID)
	}

	return nil
}

// killProcess sends SIGTERM to a process and waits for it to exit, falling
// back to SIGKILL if it doesn't exit within the timeout.
func killProcess(pid int) {
	process, err := os.FindProcess(pid)
	if err != nil {
		return
	}

	_ = process.Signal(syscall.SIGTERM)

	deadline := time.Now().Add(stopPollTimeout)
	for time.Now().Before(deadline) {
		if !IsProcessRunning(pid) {
			return
		}
		time.Sleep(stopPollTick)
	}

	_ = process.Signal(syscall.SIGKILL)
}

// findOrphanServerPIDs finds all running `agenc server run` processes,
// excluding the given set of PIDs.
// Returns nil (not an error) if pgrep is unavailable or finds nothing.
func findOrphanServerPIDs(excludePIDs map[int]bool) []int {
	cmd := exec.Command("pgrep", "-f", "agenc server run")
	output, err := cmd.Output()
	if err != nil {
		return nil // pgrep returns exit 1 when no matches
	}

	var result []int
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil || pid <= 0 || excludePIDs[pid] {
			continue
		}
		result = append(result, pid)
	}

	return result
}

// WaitForReady polls the server's /health endpoint on the given unix socket
// until it responds successfully or the timeout expires.
func WaitForReady(socketFilepath string) error {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.DialTimeout("unix", socketFilepath, 1*time.Second)
			},
		},
		Timeout: 2 * time.Second,
	}

	deadline := time.Now().Add(readyPollTimeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://agenc/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(readyPollTick)
	}

	return stacktrace.NewError("server did not become ready within %s", readyPollTimeout)
}
