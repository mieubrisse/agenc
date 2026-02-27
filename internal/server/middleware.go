package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
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
			json.NewEncoder(lrw.ResponseWriter).Encode(errorResponse{Message: err.Error()})
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
