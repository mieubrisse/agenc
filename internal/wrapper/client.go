package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/mieubrisse/stacktrace"
)

// ErrWrapperNotRunning is returned when the wrapper socket does not exist or
// the connection is refused (stale socket from a crashed wrapper).
var ErrWrapperNotRunning = errors.New("wrapper is not running")

// WrapperClient is an HTTP client that connects to a wrapper via its unix socket.
type WrapperClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewWrapperClient creates a new client that connects to the wrapper at the
// given socket path. The timeout controls both the dial timeout and the overall
// HTTP request timeout.
func NewWrapperClient(socketFilepath string, timeout time.Duration) *WrapperClient {
	return &WrapperClient{
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return net.DialTimeout("unix", socketFilepath, timeout)
				},
			},
			Timeout: timeout,
		},
		// The host doesn't matter for unix sockets, but HTTP requires one
		baseURL: "http://wrapper",
	}
}

// GetStatus fetches the current wrapper and Claude state.
func (c *WrapperClient) GetStatus() (*StatusResponse, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/status")
	if err != nil {
		if isConnectionError(err) {
			return nil, ErrWrapperNotRunning
		}
		return nil, stacktrace.Propagate(err, "failed to connect to wrapper")
	}
	defer resp.Body.Close()

	var status StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, stacktrace.Propagate(err, "failed to decode wrapper status response")
	}

	return &status, nil
}

// Restart sends a restart command to the wrapper.
func (c *WrapperClient) Restart(mode, reason string) error {
	req := RestartRequest{Mode: mode, Reason: reason}
	cmdResp, err := c.postCommand("/restart", req)
	if err != nil {
		return err
	}
	if cmdResp.Status == "error" {
		return stacktrace.NewError("wrapper restart failed: %s", cmdResp.Error)
	}
	return nil
}

// SendClaudeUpdate sends a claude_update event to the wrapper.
func (c *WrapperClient) SendClaudeUpdate(event, notificationType string) error {
	req := ClaudeUpdateRequest{Event: event, NotificationType: notificationType}
	cmdResp, err := c.postCommand("/claude-update", req)
	if err != nil {
		return err
	}
	if cmdResp.Status == "error" {
		return stacktrace.NewError("wrapper claude-update failed: %s", cmdResp.Error)
	}
	return nil
}

// postCommand sends a POST request with a JSON body and decodes the
// CommandResponse. Returns ErrWrapperNotRunning on connection errors.
func (c *WrapperClient) postCommand(path string, body any) (*CommandResponse, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal request body")
	}

	resp, err := c.httpClient.Post(c.baseURL+path, "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		if isConnectionError(err) {
			return nil, ErrWrapperNotRunning
		}
		return nil, stacktrace.Propagate(err, "failed to connect to wrapper")
	}
	defer resp.Body.Close()

	var cmdResp CommandResponse
	if err := json.NewDecoder(resp.Body).Decode(&cmdResp); err != nil {
		return nil, stacktrace.Propagate(err, "failed to decode wrapper response")
	}

	return &cmdResp, nil
}

// isConnectionError checks if an error indicates the wrapper is not running.
// This covers both "socket file not found" (os.IsNotExist) and "connection
// refused" (stale socket from a crashed wrapper).
func isConnectionError(err error) bool {
	if os.IsNotExist(err) {
		return true
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		// Connection refused or socket not found at the syscall level
		var sysErr *os.SyscallError
		if errors.As(opErr.Err, &sysErr) {
			return sysErr.Syscall == "connect"
		}
		// Also catch "no such file or directory" wrapped inside OpError
		if os.IsNotExist(opErr.Err) {
			return true
		}
	}

	return false
}
