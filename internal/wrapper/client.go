package wrapper

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"time"

	"github.com/mieubrisse/stacktrace"
)

const (
	clientDialTimeout = 5 * time.Second
)

// ErrWrapperNotRunning is returned by SendCommand when the wrapper socket
// does not exist or the connection is refused (stale socket from a crashed wrapper).
var ErrWrapperNotRunning = errors.New("wrapper is not running")

// SendCommand dials the wrapper unix socket, sends a Command, and returns
// the Response. Returns ErrWrapperNotRunning if the socket file doesn't exist
// or the connection is refused.
func SendCommand(socketFilepath string, cmd Command) (*Response, error) {
	// Check if socket file exists before attempting to connect
	if _, err := os.Stat(socketFilepath); os.IsNotExist(err) {
		return nil, ErrWrapperNotRunning
	}

	conn, err := net.DialTimeout("unix", socketFilepath, clientDialTimeout)
	if err != nil {
		// Connection refused means the socket file is stale (wrapper crashed)
		if isConnectionRefused(err) {
			return nil, ErrWrapperNotRunning
		}
		return nil, stacktrace.Propagate(err, "failed to connect to wrapper socket")
	}
	defer conn.Close()

	// Send the command
	if err := json.NewEncoder(conn).Encode(cmd); err != nil {
		return nil, stacktrace.Propagate(err, "failed to send command to wrapper")
	}

	// Read the response
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, stacktrace.Propagate(err, "failed to read response from wrapper")
	}

	return &resp, nil
}

// SendCommandWithTimeout is like SendCommand but uses a custom dial timeout.
// Useful for hook commands that need a shorter timeout to avoid blocking Claude.
func SendCommandWithTimeout(socketFilepath string, cmd Command, timeout time.Duration) (*Response, error) {
	if _, err := os.Stat(socketFilepath); os.IsNotExist(err) {
		return nil, ErrWrapperNotRunning
	}

	conn, err := net.DialTimeout("unix", socketFilepath, timeout)
	if err != nil {
		if isConnectionRefused(err) {
			return nil, ErrWrapperNotRunning
		}
		return nil, stacktrace.Propagate(err, "failed to connect to wrapper socket")
	}
	defer conn.Close()

	// Set read/write deadlines based on the timeout
	deadline := time.Now().Add(timeout)
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, stacktrace.Propagate(err, "failed to set socket deadline")
	}

	if err := json.NewEncoder(conn).Encode(cmd); err != nil {
		return nil, stacktrace.Propagate(err, "failed to send command to wrapper")
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, stacktrace.Propagate(err, "failed to read response from wrapper")
	}

	return &resp, nil
}

// isConnectionRefused checks if an error is a "connection refused" error,
// which indicates a stale socket file.
func isConnectionRefused(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var sysErr *os.SyscallError
		if errors.As(opErr.Err, &sysErr) {
			return sysErr.Syscall == "connect"
		}
	}
	return false
}
