package wrapper

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"time"
)

// Command represents a request sent to the wrapper over the unix socket.
type Command struct {
	Command          string `json:"command"`
	Mode             string `json:"mode,omitempty"`              // "graceful" or "hard" (restart)
	Reason           string `json:"reason,omitempty"`            // e.g. "credentials_changed"
	Event            string `json:"event,omitempty"`             // hook event name (claude_update)
	NotificationType string `json:"notification_type,omitempty"` // for Notification events
}

// Response represents the wrapper's reply to a socket command.
type Response struct {
	Status string `json:"status"`          // "ok" or "error"
	Error  string `json:"error,omitempty"` // set when status is "error"
}

const (
	socketReadTimeout  = 5 * time.Second
	socketWriteTimeout = 5 * time.Second
)

// listenSocket creates a unix socket at socketFilepath, accepts connections,
// decodes a single JSON Command per connection, sends it on commandCh, and
// writes the JSON Response back. The listener is cleaned up when ctx is cancelled.
func listenSocket(ctx context.Context, socketFilepath string, commandCh chan<- commandWithResponse, logger *slog.Logger) {
	// Remove stale socket file from a previous run
	os.Remove(socketFilepath)

	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketFilepath, Net: "unix"})
	if err != nil {
		logger.Warn("Failed to create wrapper socket", "path", socketFilepath, "error", err)
		return
	}

	// Set restrictive permissions (mode 0600) on the socket to prevent
	// unauthorized access. The socket provides control over the wrapper
	// process (restart commands, state updates), so it should only be
	// accessible to the owner.
	if err := os.Chmod(socketFilepath, 0600); err != nil {
		logger.Warn("Failed to set socket permissions", "path", socketFilepath, "error", err)
		listener.Close()
		os.Remove(socketFilepath)
		return
	}
	defer func() {
		listener.Close()
		os.Remove(socketFilepath)
	}()

	// Allow cleanup on context cancellation by setting a deadline loop
	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			// Check if we're shutting down
			select {
			case <-ctx.Done():
				return
			default:
			}
			logger.Warn("Failed to accept socket connection", "error", err)
			continue
		}

		handleSocketConnection(conn, commandCh, logger)
	}
}

// commandWithResponse pairs a Command with a channel for sending back the Response.
type commandWithResponse struct {
	cmd        Command
	responseCh chan<- Response
}

// handleSocketConnection reads a single JSON Command from the connection,
// sends it to the command channel, waits for a response, and writes it back.
func handleSocketConnection(conn net.Conn, commandCh chan<- commandWithResponse, logger *slog.Logger) {
	defer conn.Close()

	// Set read deadline
	if err := conn.SetReadDeadline(time.Now().Add(socketReadTimeout)); err != nil {
		logger.Warn("Failed to set socket read deadline", "error", err)
		return
	}

	var cmd Command
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&cmd); err != nil {
		logger.Warn("Failed to decode socket command", "error", err)
		writeResponse(conn, Response{Status: "error", Error: "invalid JSON"}, logger)
		return
	}

	logger.Info("Received socket command", "command", cmd.Command, "mode", cmd.Mode, "reason", cmd.Reason)

	// Send command to the main event loop and wait for response
	responseCh := make(chan Response, 1)
	commandCh <- commandWithResponse{cmd: cmd, responseCh: responseCh}

	// Wait for the main loop to process the command
	resp := <-responseCh

	writeResponse(conn, resp, logger)
}

// writeResponse encodes and writes a Response to the connection.
func writeResponse(conn net.Conn, resp Response, logger *slog.Logger) {
	if err := conn.SetWriteDeadline(time.Now().Add(socketWriteTimeout)); err != nil {
		logger.Warn("Failed to set socket write deadline", "error", err)
		return
	}

	if err := json.NewEncoder(conn).Encode(resp); err != nil {
		logger.Warn("Failed to write socket response", "error", err)
	}
}
