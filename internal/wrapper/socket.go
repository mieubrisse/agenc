package wrapper

import (
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
	if err := os.Remove(socketFilepath); err != nil && !os.IsNotExist(err) {
		logger.Warn("Failed to remove stale socket file", "path", socketFilepath, "error", err)
		return
	}

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
		_ = os.Remove(socketFilepath)
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", handleStatus(w))
	mux.HandleFunc("POST /restart", handleRestart(w, logger))
	mux.HandleFunc("POST /claude-update", handleClaudeUpdateHTTP(w, logger))
	mux.HandleFunc("POST /claude-update/{event}", handleClaudeUpdateWithPathEvent(w, logger))
	mux.HandleFunc("POST /rebuild", handleRebuild(w, logger))

	server := &http.Server{
		Handler:      mux,
		ReadTimeout:  httpReadTimeout,
		WriteTimeout: httpWriteTimeout,
	}

	// Shut down the server when the context is cancelled
	go func() {
		<-ctx.Done()
		server.Close()
		_ = os.Remove(socketFilepath)
	}()

	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
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
		_ = json.NewEncoder(rw).Encode(resp) // response already started; encode error cannot be propagated
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

// handleRebuild sends a rebuild command through the event loop channel and
// waits for the response.
func handleRebuild(w *Wrapper, logger *slog.Logger) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		logger.Info("Received rebuild request")

		cmd := Command{
			Command: "rebuild",
		}

		resp := sendCommandAndWait(w.commandCh, cmd)
		writeCommandResponse(rw, http.StatusOK, resp)
	}
}

// handleClaudeUpdateWithPathEvent handles POST /claude-update/{event} used by
// containerized missions. The event type is in the URL path instead of the JSON
// body, since container hooks use curl with the event in the URL.
func handleClaudeUpdateWithPathEvent(w *Wrapper, logger *slog.Logger) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		event := r.PathValue("event")
		if event == "" {
			writeCommandResponse(rw, http.StatusBadRequest, CommandResponse{
				Status: "error",
				Error:  "missing event in path",
			})
			return
		}

		// Read optional body (Claude hook stdin forwarded as-is)
		var req ClaudeUpdateRequest
		if r.ContentLength > 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				// Body might not be valid JSON — that's okay for some hooks
				logger.Debug("Could not parse hook body", "event", event, "error", err)
			}
		}

		logger.Info("Received claude_update request (path)", "event", event, "notification_type", req.NotificationType)

		cmd := Command{
			Command:          "claude_update",
			Event:            event,
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
	_ = json.NewEncoder(rw).Encode(resp) // response already started; encode error cannot be propagated
}
