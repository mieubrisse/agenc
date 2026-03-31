package server

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/sleep"
)

func TestHandleListSleepWindows_Empty(t *testing.T) {
	srv := &Server{logger: log.New(os.Stderr, "", 0)}
	srv.cachedConfig.Store(&config.AgencConfig{})

	req := httptest.NewRequest("GET", "/config/sleep/windows", nil)
	w := httptest.NewRecorder()

	if err := srv.handleListSleepWindows(w, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result []sleep.WindowDef
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 windows, got %d", len(result))
	}
}

func TestHandleListSleepWindows_WithWindows(t *testing.T) {
	cfg := &config.AgencConfig{
		SleepMode: &config.SleepModeConfig{
			Windows: []sleep.WindowDef{
				{Days: []string{"mon", "tue"}, Start: "22:00", End: "06:00"},
				{Days: []string{"sat"}, Start: "23:00", End: "08:00"},
			},
		},
	}

	srv := &Server{logger: log.New(os.Stderr, "", 0)}
	srv.cachedConfig.Store(cfg)

	req := httptest.NewRequest("GET", "/config/sleep/windows", nil)
	w := httptest.NewRecorder()

	if err := srv.handleListSleepWindows(w, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result []sleep.WindowDef
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(result))
	}
	if result[0].Start != "22:00" {
		t.Errorf("expected first window start 22:00, got %s", result[0].Start)
	}
	if result[1].Days[0] != "sat" {
		t.Errorf("expected second window day sat, got %s", result[1].Days[0])
	}
}

func newTestServerWithConfig(t *testing.T) (*Server, string) {
	t.Helper()
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(configDirpath, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDirpath, "config.yml"), []byte(""), 0o644); err != nil {
		t.Fatalf("failed to write config.yml: %v", err)
	}

	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	srv.cachedConfig.Store(&config.AgencConfig{})
	return srv, tmpDir
}

func TestHandleAddSleepWindow_Valid(t *testing.T) {
	srv, _ := newTestServerWithConfig(t)

	body := `{"days":["mon","tue"],"start":"22:00","end":"06:00"}`
	req := httptest.NewRequest("POST", "/config/sleep/windows", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	if err := srv.handleAddSleepWindow(w, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}

	var result []sleep.WindowDef
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 window, got %d", len(result))
	}
	if result[0].Start != "22:00" || result[0].End != "06:00" {
		t.Errorf("unexpected window: %+v", result[0])
	}
}

func TestHandleAddSleepWindow_InvalidDay(t *testing.T) {
	srv := &Server{logger: log.New(os.Stderr, "", 0)}
	srv.cachedConfig.Store(&config.AgencConfig{})

	body := `{"days":["monday"],"start":"22:00","end":"06:00"}`
	req := httptest.NewRequest("POST", "/config/sleep/windows", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	err := srv.handleAddSleepWindow(w, req)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestHandleAddSleepWindow_StartEqualsEnd(t *testing.T) {
	srv := &Server{logger: log.New(os.Stderr, "", 0)}
	srv.cachedConfig.Store(&config.AgencConfig{})

	body := `{"days":["mon"],"start":"22:00","end":"22:00"}`
	req := httptest.NewRequest("POST", "/config/sleep/windows", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	err := srv.handleAddSleepWindow(w, req)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestHandleAddSleepWindow_InvalidBody(t *testing.T) {
	srv := &Server{logger: log.New(os.Stderr, "", 0)}
	srv.cachedConfig.Store(&config.AgencConfig{})

	body := `not json`
	req := httptest.NewRequest("POST", "/config/sleep/windows", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	err := srv.handleAddSleepWindow(w, req)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestHandleRemoveSleepWindow_Valid(t *testing.T) {
	srv, _ := newTestServerWithConfig(t)

	cfg := &config.AgencConfig{
		SleepMode: &config.SleepModeConfig{
			Windows: []sleep.WindowDef{
				{Days: []string{"mon"}, Start: "22:00", End: "06:00"},
				{Days: []string{"sat"}, Start: "23:00", End: "08:00"},
			},
		},
	}
	srv.cachedConfig.Store(cfg)
	// Write config to disk so ReadAgencConfig works
	if err := config.WriteAgencConfig(srv.agencDirpath, cfg, nil); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	req := httptest.NewRequest("DELETE", "/config/sleep/windows/0", nil)
	req.SetPathValue("index", "0")
	w := httptest.NewRecorder()

	if err := srv.handleRemoveSleepWindow(w, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result []sleep.WindowDef
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 window remaining, got %d", len(result))
	}
	if result[0].Days[0] != "sat" {
		t.Errorf("expected remaining window day sat, got %s", result[0].Days[0])
	}
}

func TestHandleRemoveSleepWindow_OutOfBounds(t *testing.T) {
	srv, _ := newTestServerWithConfig(t)

	cfg := &config.AgencConfig{
		SleepMode: &config.SleepModeConfig{
			Windows: []sleep.WindowDef{
				{Days: []string{"mon"}, Start: "22:00", End: "06:00"},
			},
		},
	}
	srv.cachedConfig.Store(cfg)
	if err := config.WriteAgencConfig(srv.agencDirpath, cfg, nil); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	req := httptest.NewRequest("DELETE", "/config/sleep/windows/5", nil)
	req.SetPathValue("index", "5")
	w := httptest.NewRecorder()

	err := srv.handleRemoveSleepWindow(w, req)
	assertHTTPError(t, err, http.StatusNotFound)
}

func TestHandleRemoveSleepWindow_InvalidIndex(t *testing.T) {
	srv := &Server{logger: log.New(os.Stderr, "", 0)}
	srv.cachedConfig.Store(&config.AgencConfig{})

	req := httptest.NewRequest("DELETE", "/config/sleep/windows/abc", nil)
	req.SetPathValue("index", "abc")
	w := httptest.NewRecorder()

	err := srv.handleRemoveSleepWindow(w, req)
	assertHTTPError(t, err, http.StatusBadRequest)
}

func TestHandleRemoveSleepWindow_CleansUpNilSleepMode(t *testing.T) {
	srv, _ := newTestServerWithConfig(t)

	cfg := &config.AgencConfig{
		SleepMode: &config.SleepModeConfig{
			Windows: []sleep.WindowDef{
				{Days: []string{"mon"}, Start: "22:00", End: "06:00"},
			},
		},
	}
	srv.cachedConfig.Store(cfg)
	if err := config.WriteAgencConfig(srv.agencDirpath, cfg, nil); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	req := httptest.NewRequest("DELETE", "/config/sleep/windows/0", nil)
	req.SetPathValue("index", "0")
	w := httptest.NewRecorder()

	if err := srv.handleRemoveSleepWindow(w, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify SleepMode is nil in the cached config
	storedCfg := srv.cachedConfig.Load()
	if storedCfg.SleepMode != nil {
		t.Errorf("expected SleepMode to be nil after removing last window, got %+v", storedCfg.SleepMode)
	}

	// Verify response is an empty array
	var result []sleep.WindowDef
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 windows, got %d", len(result))
	}
}

// assertHTTPError checks that err is an *httpError with the expected status code.
func assertHTTPError(t *testing.T, err error, expectedStatus int) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	he, ok := err.(*httpError)
	if !ok {
		t.Fatalf("expected *httpError, got %T: %v", err, err)
	}
	if he.status != expectedStatus {
		t.Errorf("expected status %d, got %d (message: %s)", expectedStatus, he.status, he.message)
	}
}
