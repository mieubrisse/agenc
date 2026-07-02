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

func TestSystemNow_MatchesSystemTimezone(t *testing.T) {
	now := systemNow()

	// Read the system timezone the same way systemNow does
	target, err := os.Readlink("/etc/localtime")
	if err != nil {
		t.Skip("cannot read /etc/localtime symlink — skipping timezone test")
	}

	const zoneinfoPrefix = "zoneinfo/"
	idx := strings.Index(target, zoneinfoPrefix)
	if idx < 0 {
		t.Skip("cannot parse timezone from /etc/localtime target")
	}
	expectedTZName := target[idx+len(zoneinfoPrefix):]

	if now.Location().String() != expectedTZName {
		t.Errorf("systemNow() timezone = %q, want %q", now.Location().String(), expectedTZName)
	}

	// Verify it's within 1 second of time.Now() (same absolute time, different zone)
	reference := time.Now()
	diff := now.Sub(reference)
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Second {
		t.Errorf("systemNow() differs from time.Now() by %v — expected same absolute time", diff)
	}
}

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

func intPtr(v int) *int { return &v }

func TestCheckAttachedMissionLimit_NoConfig(t *testing.T) {
	if err := checkAttachedMissionLimit(nil, 10, true); err != nil {
		t.Fatalf("nil config should passthrough, got: %v", err)
	}
}

func TestCheckAttachedMissionLimit_NilLimit(t *testing.T) {
	cfg := &config.AgencConfig{}
	if err := checkAttachedMissionLimit(cfg, 10, true); err != nil {
		t.Fatalf("nil AttachedMissionLimit should passthrough, got: %v", err)
	}
}

func TestCheckAttachedMissionLimit_WillNotAttach(t *testing.T) {
	cfg := &config.AgencConfig{AttachedMissionLimit: intPtr(5)}
	// Even at the cap, a request that won't attach passes.
	if err := checkAttachedMissionLimit(cfg, 5, false); err != nil {
		t.Fatalf("willAttach=false should passthrough, got: %v", err)
	}
}

func TestCheckAttachedMissionLimit_UnderCap(t *testing.T) {
	cfg := &config.AgencConfig{AttachedMissionLimit: intPtr(5)}
	if err := checkAttachedMissionLimit(cfg, 4, true); err != nil {
		t.Fatalf("under-cap should passthrough, got: %v", err)
	}
}

func TestCheckAttachedMissionLimit_AtCap(t *testing.T) {
	cfg := &config.AgencConfig{AttachedMissionLimit: intPtr(5)}
	err := checkAttachedMissionLimit(cfg, 5, true)
	if err == nil {
		t.Fatal("at-cap should error, got nil")
	}
	httpErr, ok := err.(*httpError)
	if !ok {
		t.Fatalf("expected *httpError, got %T", err)
	}
	if httpErr.status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", httpErr.status)
	}
	if !strings.Contains(err.Error(), "Attached mission limit reached (5)") {
		t.Fatalf("expected message to name the limit, got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "detach") {
		t.Fatalf("expected message to mention detach, got: %s", err.Error())
	}
}

func TestCheckAttachedMissionLimit_OverCap(t *testing.T) {
	cfg := &config.AgencConfig{AttachedMissionLimit: intPtr(5)}
	err := checkAttachedMissionLimit(cfg, 10, true)
	if err == nil {
		t.Fatal("over-cap should error, got nil")
	}
}

func TestCheckAttachedMissionLimit_ZeroCap_BlocksAllAttach(t *testing.T) {
	// Zero in the config file is a literal "no attachments" cap. Hand-edited
	// only — CLI `config set` rejects zero.
	cfg := &config.AgencConfig{AttachedMissionLimit: intPtr(0)}
	err := checkAttachedMissionLimit(cfg, 0, true)
	if err == nil {
		t.Fatal("zero cap should block any willAttach, got nil")
	}
	if !strings.Contains(err.Error(), "(0)") {
		t.Fatalf("expected message to name the zero limit, got: %s", err.Error())
	}
}

func TestCheckAttachedMissionLimit_ZeroCap_AllowsPoolOnly(t *testing.T) {
	cfg := &config.AgencConfig{AttachedMissionLimit: intPtr(0)}
	if err := checkAttachedMissionLimit(cfg, 0, false); err != nil {
		t.Fatalf("zero cap with willAttach=false should passthrough, got: %v", err)
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
