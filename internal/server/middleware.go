package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/odyssey/agenc/internal/sleep"
)

// loggingResponseWriter wraps http.ResponseWriter to capture the status code
// written by handlers that return success responses directly via writeJSON.
type loggingResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	if lrw.wroteHeader {
		return
	}
	lrw.status = code
	lrw.wroteHeader = true
	lrw.ResponseWriter.WriteHeader(code)
}

// appHandlerFunc is an HTTP handler that returns an error. Returning a non-nil
// error causes the middleware to write the appropriate HTTP error response and
// log the error message. Handlers should return newHTTPError for known error
// conditions; returning a plain error results in a 500 response.
type appHandlerFunc func(http.ResponseWriter, *http.Request) error

// stashGuard wraps a handler to reject requests while a stash operation is in
// progress. Returns 503 Service Unavailable with a descriptive message.
func (s *Server) stashGuard(fn appHandlerFunc) appHandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		if s.stashInProgress.Load() {
			return newHTTPError(http.StatusServiceUnavailable, "stash operation in progress — try again shortly")
		}
		return fn(w, r)
	}
}

// sleepGuard wraps a handler to reject requests during sleep mode windows.
// Returns 403 Forbidden with a friendly message. Exempts cron-triggered
// mission creation by peeking at the request body's "source" field.
func (s *Server) sleepGuard(fn appHandlerFunc) appHandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		cfg := s.getConfig()
		if cfg.SleepMode == nil || len(cfg.SleepMode.Windows) == 0 {
			return fn(w, r)
		}

		now := systemNow()
		if !sleep.IsActive(cfg.SleepMode.Windows, now) {
			return fn(w, r)
		}

		// Check if this is a cron-triggered mission (exempt from sleep guard).
		// Peek at the body without consuming it — buffer and restore.
		if r.Body != nil && r.Method == http.MethodPost {
			bodyBytes, err := io.ReadAll(r.Body)
			r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			if err == nil {
				var peek struct {
					Source string `json:"source"`
				}
				if json.Unmarshal(bodyBytes, &peek) == nil && peek.Source == "cron" {
					return fn(w, r)
				}
			}
		}

		endTime, _ := sleep.FindActiveWindowEnd(cfg.SleepMode.Windows, now)
		msg := "Sleep mode active — go to bed!"
		if endTime != "" {
			msg = fmt.Sprintf("Sleep mode active until %s — go to bed!", endTime)
		}
		return newHTTPError(http.StatusForbidden, msg)
	}
}

// systemNow returns the current time in the system's actual timezone.
// Go caches time.Local at process startup and never refreshes it, so
// time.Now() uses a stale timezone if the system timezone changes while
// the server is running (e.g., the user travels across timezones).
// This function re-reads /etc/localtime on each call to get the current
// timezone. Falls back to time.Now() if the timezone cannot be determined.
func systemNow() time.Time {
	target, err := os.Readlink("/etc/localtime")
	if err != nil {
		return time.Now()
	}
	const zoneinfoPrefix = "zoneinfo/"
	idx := strings.Index(target, zoneinfoPrefix)
	if idx < 0 {
		return time.Now()
	}
	tzName := target[idx+len(zoneinfoPrefix):]
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return time.Now()
	}
	return time.Now().In(loc)
}

// appHandler wraps an appHandlerFunc into an http.Handler that logs every
// request and automatically writes error responses.
func appHandler(logger *slog.Logger, fn appHandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}

		err := fn(lrw, r)

		duration := time.Since(start)
		status := lrw.status

		if err != nil {
			status = httpStatusFromError(err)
			lrw.ResponseWriter.Header().Set("Content-Type", "application/json")
			lrw.ResponseWriter.WriteHeader(status)
			_ = json.NewEncoder(lrw.ResponseWriter).Encode(errorResponse{Message: err.Error()}) // response already started; encode error cannot be propagated
		}

		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", status),
			slog.Int64("duration_ms", duration.Milliseconds()),
		}
		if err != nil {
			attrs = append(attrs, slog.String("error", err.Error()))
		}

		level := slog.LevelInfo
		if status >= 400 {
			level = slog.LevelError
		}

		logger.LogAttrs(r.Context(), level, "request", attrs...)
	})
}
