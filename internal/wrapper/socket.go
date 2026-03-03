package wrapper

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"
)

// StatusResponse is the JSON response for GET /status.
type StatusResponse struct {
	ClaudeState     string `json:"claude_state"`
	WrapperState    string `json:"wrapper_state"`
	HasConversation bool   `json:"has_conversation"`
}

// RestartRequest is the JSON body for POST /restart.
type RestartRequest struct {
	Mode   string `json:"mode"`
	Reason string `json:"reason"`
}

// ClaudeUpdateRequest is the JSON body for POST /claude-update.
type ClaudeUpdateRequest struct {
	Event            string `json:"event"`
	NotificationType string `json:"notification_type"`
}

// CommandResponse is the JSON response for POST /restart and POST /claude-update.
type CommandResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Command is an internal representation of a command sent through the event
// loop channel. Not serialized to JSON — the HTTP handlers construct it from
// typed request structs.
type Command struct {
	Command          string
	Mode             string
	Reason           string
	Event            string
	NotificationType string
}

// commandWithResponse pairs a Command with a channel for sending back the CommandResponse.
type commandWithResponse struct {
	cmd        Command
	responseCh chan<- CommandResponse
}

const (
	httpReadTimeout  = 5 * time.Second
	httpWriteTimeout = 5 * time.Second
)

// startHTTPServer creates an HTTP server listening on a unix socket at
// socketFilepath. It serves three endpoints: GET /status, POST /restart,
// and POST /claude-update. The server shuts down when ctx is cancelled.
func startHTTPServer(ctx context.Context, socketFilepath string, w *Wrapper, logger *slog.Logger) {
	// Remove stale socket file from a previous run
	os.Remove(socketFilepath)

	listener, err := net.Listen("unix", socketFilepath)
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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", handleStatus(w))
	mux.HandleFunc("POST /restart", handleRestart(w, logger))
	mux.HandleFunc("POST /claude-update", handleClaudeUpdateHTTP(w, logger))
	mux.HandleFunc("POST /command", handleLegacyCommand(w, logger))

	server := &http.Server{
		Handler:      mux,
		ReadTimeout:  httpReadTimeout,
		WriteTimeout: httpWriteTimeout,
	}

	// Wrap the listener with protocol detection so that raw-JSON connections
	// (from legacy clients or tests) are handled inline while HTTP connections
	// are forwarded to the HTTP server.
	pdl := &protocolDetectingListener{
		Listener:  listener,
		commandCh: w.commandCh,
		logger:    logger,
		ctx:       ctx,
	}

	// Shut down the server when the context is cancelled
	go func() {
		<-ctx.Done()
		server.Close()
		os.Remove(socketFilepath)
	}()

	if err := server.Serve(pdl); err != nil && err != http.ErrServerClosed {
		logger.Warn("HTTP server exited with error", "error", err)
	}
}

// handleStatus returns the current wrapper and Claude state. This handler
// reads state directly with stateMu — it does NOT go through the command
// channel because it is a read-only operation.
func handleStatus(w *Wrapper) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		w.stateMu.RLock()
		resp := StatusResponse{
			ClaudeState:     w.getClaudeStateString(),
			WrapperState:    w.getWrapperStateString(),
			HasConversation: w.hasConversation,
		}
		w.stateMu.RUnlock()

		rw.Header().Set("Content-Type", "application/json")
		json.NewEncoder(rw).Encode(resp)
	}
}

// handleRestart sends a restart command through the event loop channel and
// waits for the response.
func handleRestart(w *Wrapper, logger *slog.Logger) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		var req RestartRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeCommandResponse(rw, http.StatusBadRequest, CommandResponse{
				Status: "error",
				Error:  "invalid JSON",
			})
			return
		}

		logger.Info("Received restart request", "mode", req.Mode, "reason", req.Reason)

		cmd := Command{
			Command: "restart",
			Mode:    req.Mode,
			Reason:  req.Reason,
		}

		resp := sendCommandAndWait(w.commandCh, cmd)
		writeCommandResponse(rw, http.StatusOK, resp)
	}
}

// handleClaudeUpdateHTTP sends a claude_update command through the event loop
// channel and waits for the response.
func handleClaudeUpdateHTTP(w *Wrapper, logger *slog.Logger) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		var req ClaudeUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeCommandResponse(rw, http.StatusBadRequest, CommandResponse{
				Status: "error",
				Error:  "invalid JSON",
			})
			return
		}

		logger.Info("Received claude_update request", "event", req.Event, "notification_type", req.NotificationType)

		cmd := Command{
			Command:          "claude_update",
			Event:            req.Event,
			NotificationType: req.NotificationType,
		}

		resp := sendCommandAndWait(w.commandCh, cmd)
		writeCommandResponse(rw, http.StatusOK, resp)
	}
}

// legacyCommandRequest mirrors the old Command struct with JSON tags.
// Used by the /command endpoint for backward compatibility with the old protocol.
type legacyCommandRequest struct {
	Command          string `json:"command"`
	Mode             string `json:"mode,omitempty"`
	Reason           string `json:"reason,omitempty"`
	Event            string `json:"event,omitempty"`
	NotificationType string `json:"notification_type,omitempty"`
}

// handleLegacyCommand provides backward compatibility with the old raw-socket
// protocol. It accepts a JSON body matching the old Command format and routes
// it through the same command channel as the typed endpoints.
func handleLegacyCommand(w *Wrapper, logger *slog.Logger) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		var req legacyCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeCommandResponse(rw, http.StatusBadRequest, CommandResponse{
				Status: "error",
				Error:  "invalid JSON",
			})
			return
		}

		logger.Info("Received legacy command", "command", req.Command, "mode", req.Mode, "reason", req.Reason)

		cmd := Command{
			Command:          req.Command,
			Mode:             req.Mode,
			Reason:           req.Reason,
			Event:            req.Event,
			NotificationType: req.NotificationType,
		}

		resp := sendCommandAndWait(w.commandCh, cmd)
		writeCommandResponse(rw, http.StatusOK, resp)
	}
}

// sendCommandAndWait sends a command through the event loop channel and blocks
// until the main loop processes it and returns a response.
func sendCommandAndWait(commandCh chan<- commandWithResponse, cmd Command) CommandResponse {
	responseCh := make(chan CommandResponse, 1)
	commandCh <- commandWithResponse{cmd: cmd, responseCh: responseCh}
	return <-responseCh
}

// writeCommandResponse writes a CommandResponse as JSON to the HTTP response writer.
func writeCommandResponse(rw http.ResponseWriter, statusCode int, resp CommandResponse) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(statusCode)
	json.NewEncoder(rw).Encode(resp)
}

// protocolDetectingListener wraps a net.Listener to detect whether incoming
// connections speak HTTP or the legacy raw-JSON protocol. HTTP connections
// are forwarded to the HTTP server. Legacy connections are handled inline
// using the old protocol (read JSON Command, send through channel, write
// JSON Response). This provides backward compatibility during the migration.
type protocolDetectingListener struct {
	net.Listener
	commandCh chan<- commandWithResponse
	logger    *slog.Logger
	httpConns chan net.Conn
	ctx       context.Context
}

// Accept peeks at the first byte of each connection to determine whether
// it is an HTTP request or a legacy raw-JSON command.
func (pdl *protocolDetectingListener) Accept() (net.Conn, error) {
	for {
		conn, err := pdl.Listener.Accept()
		if err != nil {
			return nil, err
		}

		br := bufio.NewReader(conn)
		firstByte, err := br.Peek(1)
		if err != nil {
			// Connection closed or errored before sending data
			conn.Close()
			continue
		}

		// HTTP methods start with uppercase letters: G(ET), P(OST/UT/ATCH),
		// D(ELETE), H(EAD), O(PTIONS), C(ONNECT), T(RACE).
		// The old protocol sends JSON objects starting with '{'.
		if firstByte[0] == '{' {
			// Legacy raw-JSON protocol — handle inline
			go pdl.handleLegacyConnection(&prefixedConn{Conn: conn, reader: br}, pdl.logger)
			continue
		}

		// HTTP connection — return it to the HTTP server
		return &prefixedConn{Conn: conn, reader: br}, nil
	}
}

// prefixedConn wraps a net.Conn with a buffered reader that may have already
// consumed bytes via Peek. Read operations go through the buffered reader so
// the peeked byte is not lost.
type prefixedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (pc *prefixedConn) Read(b []byte) (int, error) {
	return pc.reader.Read(b)
}

// handleLegacyConnection implements the old raw-JSON protocol: read a single
// JSON Command, send it through the command channel, wait for a response, and
// write the JSON Response back.
func (pdl *protocolDetectingListener) handleLegacyConnection(conn net.Conn, logger *slog.Logger) {
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(httpReadTimeout))

	var req legacyCommandRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		conn.SetWriteDeadline(time.Now().Add(httpWriteTimeout))
		json.NewEncoder(conn).Encode(CommandResponse{Status: "error", Error: "invalid JSON"})
		return
	}

	cmd := Command{
		Command:          req.Command,
		Mode:             req.Mode,
		Reason:           req.Reason,
		Event:            req.Event,
		NotificationType: req.NotificationType,
	}

	responseCh := make(chan CommandResponse, 1)
	pdl.commandCh <- commandWithResponse{cmd: cmd, responseCh: responseCh}
	resp := <-responseCh

	conn.SetWriteDeadline(time.Now().Add(httpWriteTimeout))
	if err := json.NewEncoder(conn).Encode(resp); err != nil {
		logger.Warn("Failed to write legacy socket response", "error", err)
	}
}

// listenSocket is a backward-compatible stub that preserves the old function
// signature for tests that have not yet been updated. It starts the HTTP server
// using a minimal Wrapper with an internal channel, and bridges commands to the
// provided send-only channel.
// Deprecated: tests should be updated to use startHTTPServer directly.
func listenSocket(ctx context.Context, socketFilepath string, commandCh chan<- commandWithResponse, logger *slog.Logger) {
	// Create a bidirectional channel for the Wrapper, then bridge it to the
	// caller's send-only channel. This is needed because the Wrapper struct
	// uses a bidirectional channel but this function signature accepts send-only.
	internalCh := make(chan commandWithResponse, 1)
	w := &Wrapper{
		commandCh: internalCh,
	}

	// Bridge: forward commands from the internal channel to the caller's channel
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case cmd := <-internalCh:
				commandCh <- cmd
			}
		}
	}()

	startHTTPServer(ctx, socketFilepath, w, logger)
}
