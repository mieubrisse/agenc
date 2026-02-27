package server

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type errorResponse struct {
	Message string `json:"message"`
}

// httpError is an error that carries an HTTP status code. Handlers return these
// to signal both the error message and the appropriate HTTP response status.
type httpError struct {
	status  int
	message string
}

func (e *httpError) Error() string {
	return e.message
}

// newHTTPError creates an error that will be rendered as an HTTP error response
// with the given status code and message.
func newHTTPError(status int, message string) error {
	return &httpError{status: status, message: message}
}

// newHTTPErrorf is like newHTTPError but accepts a format string.
func newHTTPErrorf(status int, format string, args ...any) error {
	return &httpError{status: status, message: fmt.Sprintf(format, args...)}
}

// httpStatusFromError extracts the HTTP status code from an error. If the error
// is not an *httpError, defaults to 500 Internal Server Error.
func httpStatusFromError(err error) int {
	if he, ok := err.(*httpError); ok {
		return he.status
	}
	return http.StatusInternalServerError
}

func writeJSON(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
