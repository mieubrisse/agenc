package server

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/sleep"
)

// dayNames maps Go's time.Weekday (Sunday=0) to our abbreviated day names.
var dayNames = [7]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// buildActiveWindow returns a WindowDef guaranteed to contain "now", using today's day name
// with a window spanning from 1 hour ago to 1 hour from now.
func buildActiveWindow(now time.Time) sleep.WindowDef {
	todayDay := dayNames[now.Weekday()]
	startHour := (now.Hour() + 23) % 24
	endHour := (now.Hour() + 1) % 24
	startTime := fmt.Sprintf("%02d:00", startHour)
	endTime := fmt.Sprintf("%02d:00", endHour)
	return sleep.WindowDef{Days: []string{todayDay}, Start: startTime, End: endTime}
}

// buildInactiveWindow returns a WindowDef guaranteed to NOT contain "now",
// by using a day that is not today.
func buildInactiveWindow(now time.Time) sleep.WindowDef {
	// Pick a day that is NOT today (tomorrow works)
	tomorrow := now.AddDate(0, 0, 1)
	tomorrowDay := dayNames[tomorrow.Weekday()]
	return sleep.WindowDef{Days: []string{tomorrowDay}, Start: "02:00", End: "04:00"}
}

func newTestServer() *Server {
	srv := &Server{logger: log.New(os.Stderr, "", 0)}
	return srv
}

// okHandler is a simple handler that writes 200 OK with a known body.
func okHandler(w http.ResponseWriter, _ *http.Request) error {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
	return nil
}

func TestSleepGuard_NoConfig(t *testing.T) {
	srv := newTestServer()
	cfg := &config.AgencConfig{} // No SleepMode configured
	srv.cachedConfig.Store(cfg)

	handler := srv.sleepGuard(okHandler)
	req := httptest.NewRequest(http.MethodPost, "/missions", nil)
	rr := httptest.NewRecorder()

	err := handler(rr, req)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestSleepGuard_OutsideWindow(t *testing.T) {
	srv := newTestServer()
	now := time.Now()
	cfg := &config.AgencConfig{
		SleepMode: &config.SleepModeConfig{
			Windows: []sleep.WindowDef{buildInactiveWindow(now)},
		},
	}
	srv.cachedConfig.Store(cfg)

	handler := srv.sleepGuard(okHandler)
	req := httptest.NewRequest(http.MethodPost, "/missions", nil)
	rr := httptest.NewRecorder()

	err := handler(rr, req)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestSleepGuard_ActiveWindow_BlocksNonCron(t *testing.T) {
	srv := newTestServer()
	now := time.Now()
	cfg := &config.AgencConfig{
		SleepMode: &config.SleepModeConfig{
			Windows: []sleep.WindowDef{buildActiveWindow(now)},
		},
	}
	srv.cachedConfig.Store(cfg)

	handler := srv.sleepGuard(okHandler)
	body := bytes.NewBufferString(`{"repo":"some/repo"}`)
	req := httptest.NewRequest(http.MethodPost, "/missions", body)
	rr := httptest.NewRecorder()

	err := handler(rr, req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	httpErr, ok := err.(*httpError)
	if !ok {
		t.Fatalf("expected *httpError, got %T", err)
	}
	if httpErr.status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", httpErr.status)
	}
	if !strings.Contains(err.Error(), "Sleep mode active") {
		t.Fatalf("expected message to contain 'Sleep mode active', got: %s", err.Error())
	}
}

func TestSleepGuard_ActiveWindow_AllowsCron(t *testing.T) {
	srv := newTestServer()
	now := time.Now()
	cfg := &config.AgencConfig{
		SleepMode: &config.SleepModeConfig{
			Windows: []sleep.WindowDef{buildActiveWindow(now)},
		},
	}
	srv.cachedConfig.Store(cfg)

	handler := srv.sleepGuard(okHandler)
	body := bytes.NewBufferString(`{"repo":"some/repo","source":"cron"}`)
	req := httptest.NewRequest(http.MethodPost, "/missions", body)
	rr := httptest.NewRecorder()

	err := handler(rr, req)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestSleepGuard_ActiveWindow_MessageContainsEndTime(t *testing.T) {
	srv := newTestServer()
	now := time.Now()
	window := buildActiveWindow(now)
	cfg := &config.AgencConfig{
		SleepMode: &config.SleepModeConfig{
			Windows: []sleep.WindowDef{window},
		},
	}
	srv.cachedConfig.Store(cfg)

	handler := srv.sleepGuard(okHandler)
	req := httptest.NewRequest(http.MethodPost, "/missions", nil)
	rr := httptest.NewRecorder()

	err := handler(rr, req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), window.End) {
		t.Fatalf("expected message to contain end time %q, got: %s", window.End, err.Error())
	}
}
