package server

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

func TestHandleServerLogs_DefaultTailsServerLog(t *testing.T) {
	tmpDir := t.TempDir()
	setupServerLogDir(t, tmpDir)

	// Write more than 200 lines
	var lines []string
	for i := 0; i < 250; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	writeLogFile(t, config.GetServerLogFilepath(tmpDir), strings.Join(lines, "\n")+"\n")

	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/server/logs", nil)
	w := httptest.NewRecorder()

	err := srv.handleServerLogs(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := w.Body.String()
	bodyLines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	if len(bodyLines) != 200 {
		t.Errorf("expected 200 lines, got %d", len(bodyLines))
	}
	if bodyLines[0] != "line 50" {
		t.Errorf("expected first line 'line 50', got %q", bodyLines[0])
	}
}

func TestHandleServerLogs_AllMode(t *testing.T) {
	tmpDir := t.TempDir()
	setupServerLogDir(t, tmpDir)

	content := "line 1\nline 2\nline 3\n"
	writeLogFile(t, config.GetServerLogFilepath(tmpDir), content)

	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/server/logs?mode=all", nil)
	w := httptest.NewRecorder()

	err := srv.handleServerLogs(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if w.Body.String() != content {
		t.Errorf("expected %q, got %q", content, w.Body.String())
	}
}

func TestHandleServerLogs_RequestsSource(t *testing.T) {
	tmpDir := t.TempDir()
	setupServerLogDir(t, tmpDir)

	content := `{"time":"2026-03-01","level":"INFO","msg":"request"}` + "\n"
	writeLogFile(t, config.GetServerRequestsLogFilepath(tmpDir), content)

	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/server/logs?source=requests&mode=all", nil)
	w := httptest.NewRecorder()

	err := srv.handleServerLogs(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if w.Body.String() != content {
		t.Errorf("expected %q, got %q", content, w.Body.String())
	}
}

func TestHandleServerLogs_InvalidSource(t *testing.T) {
	tmpDir := t.TempDir()
	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/server/logs?source=invalid", nil)
	w := httptest.NewRecorder()

	err := srv.handleServerLogs(w, req)
	if err == nil {
		t.Fatal("expected error for invalid source")
	}

	httpErr, ok := err.(*httpError)
	if !ok {
		t.Fatalf("expected *httpError, got %T", err)
	}
	if httpErr.status != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", httpErr.status)
	}
}

func TestHandleServerLogs_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/server/logs", nil)
	w := httptest.NewRecorder()

	err := srv.handleServerLogs(w, req)
	if err == nil {
		t.Fatal("expected error for missing log file")
	}

	httpErr, ok := err.(*httpError)
	if !ok {
		t.Fatalf("expected *httpError, got %T", err)
	}
	if httpErr.status != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", httpErr.status)
	}
}

func setupServerLogDir(t *testing.T, agencDirpath string) {
	t.Helper()
	serverDirpath := filepath.Join(agencDirpath, config.ServerDirname)
	if err := os.MkdirAll(serverDirpath, 0755); err != nil {
		t.Fatalf("failed to create server dir: %v", err)
	}
}

func writeLogFile(t *testing.T, filepath string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write log file: %v", err)
	}
}
