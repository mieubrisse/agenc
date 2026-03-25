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

func TestHandleCronLogs_DefaultTails(t *testing.T) {
	tmpDir := t.TempDir()
	setupCronLogDir(t, tmpDir)

	cronID := "abc-123"
	var lines []string
	for i := 0; i < 250; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	writeLogFile(t, config.GetCronLogFilepath(tmpDir, cronID), strings.Join(lines, "\n")+"\n")

	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/crons/"+cronID+"/logs", nil)
	req.SetPathValue("id", cronID)
	w := httptest.NewRecorder()

	err := srv.handleCronLogs(w, req)
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

func TestHandleCronLogs_AllMode(t *testing.T) {
	tmpDir := t.TempDir()
	setupCronLogDir(t, tmpDir)

	cronID := "abc-123"
	content := "line 1\nline 2\nline 3\n"
	writeLogFile(t, config.GetCronLogFilepath(tmpDir, cronID), content)

	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/crons/"+cronID+"/logs?mode=all", nil)
	req.SetPathValue("id", cronID)
	w := httptest.NewRecorder()

	err := srv.handleCronLogs(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if w.Body.String() != content {
		t.Errorf("expected %q, got %q", content, w.Body.String())
	}
}

func TestHandleCronLogs_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/crons/nonexistent/logs", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()

	err := srv.handleCronLogs(w, req)
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

func TestHandleCronLogs_InvalidMode(t *testing.T) {
	tmpDir := t.TempDir()
	setupCronLogDir(t, tmpDir)

	cronID := "abc-123"
	writeLogFile(t, config.GetCronLogFilepath(tmpDir, cronID), "content\n")

	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/crons/"+cronID+"/logs?mode=invalid", nil)
	req.SetPathValue("id", cronID)
	w := httptest.NewRecorder()

	err := srv.handleCronLogs(w, req)
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}

	httpErr, ok := err.(*httpError)
	if !ok {
		t.Fatalf("expected *httpError, got %T", err)
	}
	if httpErr.status != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", httpErr.status)
	}
}

func setupCronLogDir(t *testing.T, agencDirpath string) {
	t.Helper()
	cronLogDirpath := filepath.Join(agencDirpath, "logs", "crons")
	if err := os.MkdirAll(cronLogDirpath, 0755); err != nil {
		t.Fatalf("failed to create cron log dir: %v", err)
	}
}
