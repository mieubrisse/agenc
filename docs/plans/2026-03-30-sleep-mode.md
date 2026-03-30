# Sleep Mode Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Block non-cron mission creation and cron creation during configured time windows to encourage the user to sleep.

**Architecture:** New `internal/sleep/` package with time-window matching logic (ported from YappBlocker). New `SleepModeConfig` field on `AgencConfig`. Server-side `sleepGuard` middleware wrapping `POST /missions`. Server endpoints for managing sleep windows (`POST/DELETE/GET /config/sleep/windows`). CLI commands as thin HTTP clients (`agenc config sleep add/rm/ls`).

**Tech Stack:** Go, Cobra CLI, net/http, YAML config

**Design doc:** `docs/plans/2026-03-26-sleep-mode-design.md`

---

### Task 1: Create `internal/sleep/` package — types and validation

**Files:**
- Create: `internal/sleep/sleep.go`
- Create: `internal/sleep/sleep_test.go`

**Step 1: Write the failing tests for validation**

```go
// internal/sleep/sleep_test.go
package sleep

import (
	"testing"
)

func TestValidateDays(t *testing.T) {
	tests := []struct {
		name    string
		days    []string
		wantErr bool
	}{
		{"valid weekdays", []string{"mon", "tue", "wed"}, false},
		{"all days", []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}, false},
		{"invalid day", []string{"monday"}, true},
		{"empty list", []string{}, true},
		{"duplicate day", []string{"mon", "mon"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDays(tt.days)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDays() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateTime(t *testing.T) {
	tests := []struct {
		name    string
		time    string
		wantErr bool
	}{
		{"valid time", "22:00", false},
		{"midnight", "00:00", false},
		{"end of day", "23:59", false},
		{"invalid hour", "24:00", true},
		{"invalid minute", "12:60", true},
		{"bad format no colon", "2200", true},
		{"empty string", "", true},
		{"letters", "ab:cd", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTime(tt.time)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTime(%q) error = %v, wantErr %v", tt.time, err, tt.wantErr)
			}
		})
	}
}

func TestValidateWindow(t *testing.T) {
	tests := []struct {
		name    string
		window  WindowDef
		wantErr bool
	}{
		{"valid overnight", WindowDef{Days: []string{"mon"}, Start: "22:00", End: "06:00"}, false},
		{"valid same-day", WindowDef{Days: []string{"mon"}, Start: "09:00", End: "17:00"}, false},
		{"start equals end rejected", WindowDef{Days: []string{"mon"}, Start: "22:00", End: "22:00"}, true},
		{"invalid day", WindowDef{Days: []string{"monday"}, Start: "22:00", End: "06:00"}, true},
		{"invalid start", WindowDef{Days: []string{"mon"}, Start: "25:00", End: "06:00"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWindow(tt.window)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWindow() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/sleep/ -v`
Expected: Compilation failure — package does not exist yet.

**Step 3: Write the types and validation functions**

```go
// internal/sleep/sleep.go
package sleep

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// WindowDef defines a time window with days and start/end times in HH:MM format.
type WindowDef struct {
	Days  []string `json:"days"  yaml:"days"`
	Start string   `json:"start" yaml:"start"`
	End   string   `json:"end"   yaml:"end"`
}

// validDays is the set of accepted day name abbreviations.
var validDays = map[string]bool{
	"mon": true, "tue": true, "wed": true, "thu": true,
	"fri": true, "sat": true, "sun": true,
}

// dayNames maps the adjusted weekday index to the short day name.
var dayNames = [7]string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// ValidateDays checks that all day names are valid and non-empty with no duplicates.
func ValidateDays(days []string) error {
	if len(days) == 0 {
		return fmt.Errorf("days list cannot be empty")
	}
	seen := make(map[string]bool, len(days))
	for _, d := range days {
		if !validDays[d] {
			return fmt.Errorf("invalid day %q: must be one of mon, tue, wed, thu, fri, sat, sun", d)
		}
		if seen[d] {
			return fmt.Errorf("duplicate day %q", d)
		}
		seen[d] = true
	}
	return nil
}

// ValidateTime checks that a string is in HH:MM format with valid ranges.
func ValidateTime(s string) error {
	if s == "" {
		return fmt.Errorf("time cannot be empty")
	}
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid time format %q: expected HH:MM", s)
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return fmt.Errorf("invalid hour in %q: %v", s, err)
	}
	if hour < 0 || hour > 23 {
		return fmt.Errorf("hour %d out of range 0-23 in %q", hour, s)
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("invalid minute in %q: %v", s, err)
	}
	if minute < 0 || minute > 59 {
		return fmt.Errorf("minute %d out of range 0-59 in %q", minute, s)
	}
	return nil
}

// ValidateWindow validates a complete window definition.
func ValidateWindow(w WindowDef) error {
	if err := ValidateDays(w.Days); err != nil {
		return err
	}
	if err := ValidateTime(w.Start); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	if err := ValidateTime(w.End); err != nil {
		return fmt.Errorf("end: %w", err)
	}
	if w.Start == w.End {
		return fmt.Errorf("start and end cannot be equal (%s) — a 24-hour block is not supported", w.Start)
	}
	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/sleep/ -v -run 'TestValidate'`
Expected: All pass.

**Step 5: Commit**

```
git add internal/sleep/sleep.go internal/sleep/sleep_test.go
git commit -m "Add internal/sleep package with types and validation"
```

---

### Task 2: Add `IsActive` and `FindActiveWindowEnd` to `internal/sleep/`

**Files:**
- Modify: `internal/sleep/sleep.go`
- Modify: `internal/sleep/sleep_test.go`

**Step 1: Write the failing tests for IsActive**

Append to `internal/sleep/sleep_test.go`:

```go
func TestIsActive(t *testing.T) {
	tests := []struct {
		name    string
		windows []WindowDef
		now     time.Time
		want    bool
	}{
		{
			name:    "nil windows returns false",
			windows: nil,
			now:     time.Date(2026, 3, 25, 22, 0, 0, 0, time.Local),
			want:    false,
		},
		{
			name:    "empty windows returns false",
			windows: []WindowDef{},
			now:     time.Date(2026, 3, 25, 22, 0, 0, 0, time.Local),
			want:    false,
		},
		{
			name:    "same-day window active within range",
			windows: []WindowDef{{Days: []string{"mon", "tue", "wed", "thu"}, Start: "20:45", End: "23:00"}},
			now:     time.Date(2026, 3, 25, 21, 0, 0, 0, time.Local), // Wednesday
			want:    true,
		},
		{
			name:    "same-day window inactive before start",
			windows: []WindowDef{{Days: []string{"mon", "tue", "wed", "thu"}, Start: "20:45", End: "23:00"}},
			now:     time.Date(2026, 3, 25, 20, 30, 0, 0, time.Local),
			want:    false,
		},
		{
			name:    "same-day window inactive wrong day",
			windows: []WindowDef{{Days: []string{"mon", "tue", "wed", "thu"}, Start: "20:45", End: "23:00"}},
			now:     time.Date(2026, 3, 28, 21, 0, 0, 0, time.Local), // Saturday
			want:    false,
		},
		{
			name:    "overnight window active before midnight",
			windows: []WindowDef{{Days: []string{"mon", "tue", "wed", "thu"}, Start: "20:45", End: "06:00"}},
			now:     time.Date(2026, 3, 25, 23, 0, 0, 0, time.Local), // Wednesday night
			want:    true,
		},
		{
			name:    "overnight window active after midnight (yesterday started)",
			windows: []WindowDef{{Days: []string{"wed"}, Start: "20:45", End: "06:00"}},
			now:     time.Date(2026, 3, 26, 2, 0, 0, 0, time.Local), // Thursday 2am
			want:    true,
		},
		{
			name:    "overnight window inactive after end",
			windows: []WindowDef{{Days: []string{"wed"}, Start: "20:45", End: "06:00"}},
			now:     time.Date(2026, 3, 26, 7, 0, 0, 0, time.Local), // Thursday 7am
			want:    false,
		},
		{
			name:    "overnight window inactive wrong start day",
			windows: []WindowDef{{Days: []string{"mon"}, Start: "20:45", End: "06:00"}},
			now:     time.Date(2026, 3, 26, 2, 0, 0, 0, time.Local), // Thursday 2am
			want:    false,
		},
		{
			name:    "sunday keyword works",
			windows: []WindowDef{{Days: []string{"sun"}, Start: "10:00", End: "22:00"}},
			now:     time.Date(2026, 3, 29, 15, 0, 0, 0, time.Local), // Sunday
			want:    true,
		},
		{
			name:    "exactly at start time is active",
			windows: []WindowDef{{Days: []string{"wed"}, Start: "22:00", End: "06:00"}},
			now:     time.Date(2026, 3, 25, 22, 0, 0, 0, time.Local),
			want:    true,
		},
		{
			name:    "exactly at end time is not active",
			windows: []WindowDef{{Days: []string{"wed"}, Start: "22:00", End: "06:00"}},
			now:     time.Date(2026, 3, 26, 6, 0, 0, 0, time.Local),
			want:    false,
		},
		{
			name: "multiple windows first matches",
			windows: []WindowDef{
				{Days: []string{"mon", "tue", "wed", "thu"}, Start: "22:00", End: "06:00"},
				{Days: []string{"fri", "sat"}, Start: "23:00", End: "07:00"},
			},
			now:  time.Date(2026, 3, 25, 23, 0, 0, 0, time.Local), // Wednesday 11pm
			want: true,
		},
		{
			name: "multiple windows second matches",
			windows: []WindowDef{
				{Days: []string{"mon", "tue", "wed", "thu"}, Start: "22:00", End: "06:00"},
				{Days: []string{"fri", "sat"}, Start: "23:00", End: "07:00"},
			},
			now:  time.Date(2026, 3, 28, 0, 30, 0, 0, time.Local), // Saturday 12:30am (fri window)
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsActive(tt.windows, tt.now)
			if got != tt.want {
				t.Errorf("IsActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindActiveWindowEnd(t *testing.T) {
	tests := []struct {
		name    string
		windows []WindowDef
		now     time.Time
		want    string
		wantOk  bool
	}{
		{
			name:    "returns end time of active window",
			windows: []WindowDef{{Days: []string{"wed"}, Start: "22:00", End: "06:00"}},
			now:     time.Date(2026, 3, 25, 23, 0, 0, 0, time.Local),
			want:    "06:00",
			wantOk:  true,
		},
		{
			name:    "no active window returns empty",
			windows: []WindowDef{{Days: []string{"wed"}, Start: "22:00", End: "06:00"}},
			now:     time.Date(2026, 3, 25, 12, 0, 0, 0, time.Local),
			want:    "",
			wantOk:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := FindActiveWindowEnd(tt.windows, tt.now)
			if ok != tt.wantOk || got != tt.want {
				t.Errorf("FindActiveWindowEnd() = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.wantOk)
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/sleep/ -v -run 'TestIsActive|TestFindActiveWindowEnd'`
Expected: Compilation failure — `IsActive` and `FindActiveWindowEnd` not defined.

**Step 3: Write the implementation**

Append to `internal/sleep/sleep.go`:

```go
// IsActive returns true if the given time falls within any of the provided windows.
func IsActive(windows []WindowDef, now time.Time) bool {
	for _, w := range windows {
		if isWindowActive(w, now) {
			return true
		}
	}
	return false
}

// FindActiveWindowEnd returns the end time of the first active window, or ("", false)
// if no window is active. Used to include "until HH:MM" in the sleep mode message.
func FindActiveWindowEnd(windows []WindowDef, now time.Time) (string, bool) {
	for _, w := range windows {
		if isWindowActive(w, now) {
			return w.End, true
		}
	}
	return "", false
}

// isWindowActive reports whether the given time falls within a single schedule window.
// Ported from github.com/mieubrisse/yappblocker/internal/schedule.
func isWindowActive(w WindowDef, now time.Time) bool {
	startHour, startMinute, err := parseTime(w.Start)
	if err != nil {
		return false
	}
	endHour, endMinute, err := parseTime(w.End)
	if err != nil {
		return false
	}

	todayDay := dayName(now)
	nowMinutes := now.Hour()*60 + now.Minute()
	startMinutes := startHour*60 + startMinute
	endMinutes := endHour*60 + endMinute

	if startMinutes < endMinutes {
		// Same-day window: active if today is a listed day and time is within [start, end).
		return containsDay(w.Days, todayDay) &&
			nowMinutes >= startMinutes &&
			nowMinutes < endMinutes
	}

	// Overnight window (start >= end): spans midnight.
	if containsDay(w.Days, todayDay) && nowMinutes >= startMinutes {
		return true
	}

	yesterdayDay := dayName(now.AddDate(0, 0, -1))
	if containsDay(w.Days, yesterdayDay) && nowMinutes < endMinutes {
		return true
	}

	return false
}

// dayName returns the short lowercase day name for the given time.
func dayName(t time.Time) string {
	idx := (int(t.Weekday()) + 6) % 7
	return dayNames[idx]
}

// parseTime parses an "HH:MM" string into hour and minute components.
func parseTime(s string) (hour, minute int, err error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid time format %q: expected HH:MM", s)
	}
	hour, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid hour in %q: %v", s, err)
	}
	minute, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid minute in %q: %v", s, err)
	}
	return hour, minute, nil
}

// containsDay checks whether the given day name appears in the list.
func containsDay(days []string, target string) bool {
	for _, d := range days {
		if d == target {
			return true
		}
	}
	return false
}
```

**Step 4: Run all sleep tests**

Run: `go test ./internal/sleep/ -v`
Expected: All pass.

**Step 5: Commit**

```
git add internal/sleep/sleep.go internal/sleep/sleep_test.go
git commit -m "Add IsActive and FindActiveWindowEnd to sleep package"
```

---

### Task 3: Add `SleepModeConfig` to `AgencConfig`

**Files:**
- Modify: `internal/config/agenc_config.go` (add `SleepModeConfig` type and field at line ~387, add `validateSleepMode` function)
- Modify: `internal/config/agenc_config_test.go` (add validation tests)

**Step 1: Write failing test for sleep mode config validation**

Add a test to `internal/config/agenc_config_test.go`:

```go
func TestValidateSleepMode(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name:    "nil sleep mode is valid",
			yaml:    "",
			wantErr: false,
		},
		{
			name: "valid sleep mode",
			yaml: `
sleepMode:
  windows:
    - days: [mon, tue]
      start: "22:00"
      end: "06:00"
`,
			wantErr: false,
		},
		{
			name: "invalid day name",
			yaml: `
sleepMode:
  windows:
    - days: [monday]
      start: "22:00"
      end: "06:00"
`,
			wantErr: true,
		},
		{
			name: "start equals end",
			yaml: `
sleepMode:
  windows:
    - days: [mon]
      start: "22:00"
      end: "22:00"
`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write a minimal config.yml with the sleep mode section, parse and validate.
			// Use a temp dir with a config/ subdir and config.yml file.
			tmpDir := t.TempDir()
			configDirpath := filepath.Join(tmpDir, "config")
			os.MkdirAll(configDirpath, 0o755)
			os.WriteFile(filepath.Join(configDirpath, "config.yml"), []byte(tt.yaml), 0o644)
			_, _, err := ReadAgencConfig(tmpDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadAgencConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -v -run TestValidateSleepMode`
Expected: Fails — `SleepModeConfig` not defined, no validation happens.

**Step 3: Add SleepModeConfig type and validation**

In `internal/config/agenc_config.go`:

1. Add import for `"github.com/odyssey/agenc/internal/sleep"`

2. Add the type (before `AgencConfig` struct, around line 379):
```go
// SleepModeConfig defines time windows during which mission and cron creation is blocked.
type SleepModeConfig struct {
	Windows []sleep.WindowDef `yaml:"windows"`
}
```

3. Add field to `AgencConfig` struct (line ~387):
```go
SleepMode *SleepModeConfig `yaml:"sleepMode,omitempty"`
```

4. Add validation function:
```go
func validateSleepMode(cfg *AgencConfig) error {
	if cfg.SleepMode == nil {
		return nil
	}
	for i, w := range cfg.SleepMode.Windows {
		if err := sleep.ValidateWindow(w); err != nil {
			return fmt.Errorf("sleepMode.windows[%d]: %w", i, err)
		}
	}
	return nil
}
```

5. Call `validateSleepMode` from `ValidateAndPopulateDefaults` (or from `ReadAgencConfig` where other validation functions are called — find the validation call chain and add it there).

**Step 4: Run tests**

Run: `go test ./internal/config/ -v -run TestValidateSleepMode`
Expected: All pass.

**Step 5: Run full test suite**

Run: `go test ./internal/config/ -v`
Expected: All pass (no regressions).

**Step 6: Commit**

```
git add internal/config/agenc_config.go internal/config/agenc_config_test.go
git commit -m "Add SleepModeConfig to AgencConfig with validation"
```

---

### Task 4: Add server endpoints for sleep window management

**Files:**
- Create: `internal/server/handle_sleep.go`
- Create: `internal/server/handle_sleep_test.go`
- Modify: `internal/server/server.go` (register routes, line ~266)

**Step 1: Write failing tests**

```go
// internal/server/handle_sleep_test.go
package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/sleep"
)

func TestHandleListSleepWindows_Empty(t *testing.T) {
	srv := newTestServer(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/config/sleep/windows", nil)
	err := srv.handleListSleepWindows(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result []sleep.WindowDef
	json.NewDecoder(w.Body).Decode(&result)
	if len(result) != 0 {
		t.Fatalf("expected empty list, got %d windows", len(result))
	}
}

func TestHandleAddSleepWindow_Valid(t *testing.T) {
	srv := newTestServer(t)
	body := `{"days":["mon","tue"],"start":"22:00","end":"06:00"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/config/sleep/windows", bytes.NewBufferString(body))
	err := srv.handleAddSleepWindow(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}

func TestHandleAddSleepWindow_InvalidDay(t *testing.T) {
	srv := newTestServer(t)
	body := `{"days":["monday"],"start":"22:00","end":"06:00"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/config/sleep/windows", bytes.NewBufferString(body))
	err := srv.handleAddSleepWindow(w, req)
	if err == nil {
		t.Fatal("expected error for invalid day")
	}
	if httpStatusFromError(err) != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", httpStatusFromError(err))
	}
}

func TestHandleRemoveSleepWindow_OutOfBounds(t *testing.T) {
	srv := newTestServer(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/config/sleep/windows/0", nil)
	req.SetPathValue("index", "0")
	err := srv.handleRemoveSleepWindow(w, req)
	if err == nil {
		t.Fatal("expected error for out of bounds")
	}
	if httpStatusFromError(err) != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", httpStatusFromError(err))
	}
}
```

Note: `newTestServer` is an existing test helper. Check `internal/server/*_test.go` for its signature and adapt if needed. The test server must have a writable config dir with a config.yml file so the add/remove handlers can write to it.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -v -run TestHandleSleep`
Expected: Compilation failure — handlers not defined.

**Step 3: Write the handlers**

```go
// internal/server/handle_sleep.go
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/sleep"
)

// handleListSleepWindows handles GET /config/sleep/windows.
func (s *Server) handleListSleepWindows(w http.ResponseWriter, r *http.Request) error {
	cfg := s.getConfig()
	windows := []sleep.WindowDef{}
	if cfg.SleepMode != nil {
		windows = cfg.SleepMode.Windows
	}
	if windows == nil {
		windows = []sleep.WindowDef{}
	}
	writeJSON(w, http.StatusOK, windows)
	return nil
}

// handleAddSleepWindow handles POST /config/sleep/windows.
func (s *Server) handleAddSleepWindow(w http.ResponseWriter, r *http.Request) error {
	var window sleep.WindowDef
	if err := json.NewDecoder(r.Body).Decode(&window); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
	}

	if err := sleep.ValidateWindow(window); err != nil {
		return newHTTPError(http.StatusBadRequest, err.Error())
	}

	// Acquire config lock, read, modify, write
	release, err := config.AcquireConfigLock(s.agencDirpath)
	if err != nil {
		return fmt.Errorf("failed to acquire config lock: %w", err)
	}
	defer release()

	cfg, cm, err := config.ReadAgencConfig(s.agencDirpath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	if cfg.SleepMode == nil {
		cfg.SleepMode = &config.SleepModeConfig{}
	}
	cfg.SleepMode.Windows = append(cfg.SleepMode.Windows, window)

	if err := config.WriteAgencConfig(s.agencDirpath, cfg, cm); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Update cached config so the guard picks it up immediately
	s.cachedConfig.Store(cfg)

	writeJSON(w, http.StatusCreated, cfg.SleepMode.Windows)
	return nil
}

// handleRemoveSleepWindow handles DELETE /config/sleep/windows/{index}.
func (s *Server) handleRemoveSleepWindow(w http.ResponseWriter, r *http.Request) error {
	indexStr := r.PathValue("index")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid index: "+indexStr)
	}

	release, err := config.AcquireConfigLock(s.agencDirpath)
	if err != nil {
		return fmt.Errorf("failed to acquire config lock: %w", err)
	}
	defer release()

	cfg, cm, err := config.ReadAgencConfig(s.agencDirpath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	var windows []sleep.WindowDef
	if cfg.SleepMode != nil {
		windows = cfg.SleepMode.Windows
	}

	if index < 0 || index >= len(windows) {
		return newHTTPError(http.StatusNotFound,
			fmt.Sprintf("window index %d out of range (have %d windows)", index, len(windows)))
	}

	windows = append(windows[:index], windows[index+1:]...)
	if cfg.SleepMode == nil {
		cfg.SleepMode = &config.SleepModeConfig{}
	}
	cfg.SleepMode.Windows = windows

	// Clean up: remove sleepMode entirely if no windows remain
	if len(windows) == 0 {
		cfg.SleepMode = nil
	}

	if err := config.WriteAgencConfig(s.agencDirpath, cfg, cm); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	s.cachedConfig.Store(cfg)

	result := windows
	if result == nil {
		result = []sleep.WindowDef{}
	}
	writeJSON(w, http.StatusOK, result)
	return nil
}
```

**Step 4: Register routes in `server.go`**

Add to `registerRoutes()` after the cron endpoints (around line 264):

```go
// Sleep mode config endpoints
mux.Handle("GET /config/sleep/windows", appHandler(s.requestLogger, s.handleListSleepWindows))
mux.Handle("POST /config/sleep/windows", appHandler(s.requestLogger, s.handleAddSleepWindow))
mux.Handle("DELETE /config/sleep/windows/{index}", appHandler(s.requestLogger, s.handleRemoveSleepWindow))
```

**Step 5: Run tests**

Run: `go test ./internal/server/ -v -run TestHandleSleep`
Expected: All pass. Adapt test setup if `newTestServer` needs config dir adjustments.

**Step 6: Commit**

```
git add internal/server/handle_sleep.go internal/server/handle_sleep_test.go internal/server/server.go
git commit -m "Add server endpoints for sleep window management"
```

---

### Task 5: Add `sleepGuard` middleware

**Files:**
- Modify: `internal/server/middleware.go` (add `sleepGuard` method)
- Modify: `internal/server/middleware_test.go` or create it (add sleep guard tests)
- Modify: `internal/server/server.go` (wrap `POST /missions` with `sleepGuard`)

**Step 1: Write failing tests**

```go
// Add to internal/server/middleware_test.go (create if needed)
package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/sleep"
)

func TestSleepGuard_NoConfig(t *testing.T) {
	srv := newTestServer(t)
	// No sleep mode configured — should pass through
	called := false
	inner := func(w http.ResponseWriter, r *http.Request) error {
		called = true
		return nil
	}
	guarded := srv.sleepGuard(inner)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/missions", nil)
	err := guarded(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("inner handler not called")
	}
}

func TestSleepGuard_ActiveWindow_BlocksNonCron(t *testing.T) {
	srv := newTestServer(t)
	// Configure a window that covers "now"
	now := time.Now()
	startTime := fmt.Sprintf("%02d:%02d", now.Hour(), now.Minute())
	endHour := (now.Hour() + 2) % 24
	endTime := fmt.Sprintf("%02d:%02d", endHour, now.Minute())
	todayDay := []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}[int(now.Weekday()+6)%7]

	cfg := srv.getConfig()
	cfg.SleepMode = &config.SleepModeConfig{
		Windows: []sleep.WindowDef{{Days: []string{todayDay}, Start: startTime, End: endTime}},
	}
	srv.cachedConfig.Store(cfg)

	called := false
	inner := func(w http.ResponseWriter, r *http.Request) error {
		called = true
		return nil
	}
	guarded := srv.sleepGuard(inner)
	body := `{"repo":"github.com/test/test","prompt":"test"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/missions", bytes.NewBufferString(body))
	err := guarded(w, req)
	if err == nil {
		t.Fatal("expected error from sleep guard")
	}
	if httpStatusFromError(err) != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", httpStatusFromError(err))
	}
	if called {
		t.Fatal("inner handler should not be called during sleep mode")
	}
}

func TestSleepGuard_ActiveWindow_AllowsCron(t *testing.T) {
	srv := newTestServer(t)
	// Same window setup as above
	now := time.Now()
	startTime := fmt.Sprintf("%02d:%02d", now.Hour(), now.Minute())
	endHour := (now.Hour() + 2) % 24
	endTime := fmt.Sprintf("%02d:%02d", endHour, now.Minute())
	todayDay := []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}[int(now.Weekday()+6)%7]

	cfg := srv.getConfig()
	cfg.SleepMode = &config.SleepModeConfig{
		Windows: []sleep.WindowDef{{Days: []string{todayDay}, Start: startTime, End: endTime}},
	}
	srv.cachedConfig.Store(cfg)

	called := false
	inner := func(w http.ResponseWriter, r *http.Request) error {
		called = true
		return nil
	}
	guarded := srv.sleepGuard(inner)
	body := `{"source":"cron","prompt":"scheduled job"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/missions", bytes.NewBufferString(body))
	err := guarded(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v — cron missions should be exempt", err)
	}
	if !called {
		t.Fatal("inner handler should be called for cron missions")
	}
}
```

Note: The cron exemption requires the guard to peek at the request body. Since the body is consumed, the guard must buffer and restore it so the inner handler can also read it. Use `io.ReadAll` + `bytes.NewReader` to replay the body.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -v -run TestSleepGuard`
Expected: Compilation failure — `sleepGuard` not defined.

**Step 3: Write the middleware**

Add to `internal/server/middleware.go`:

```go
// sleepGuard wraps a handler to reject requests during sleep mode windows.
// Returns 403 Forbidden with a friendly message. Exempts cron-triggered
// mission creation by peeking at the request body's "source" field.
func (s *Server) sleepGuard(fn appHandlerFunc) appHandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		cfg := s.getConfig()
		if cfg.SleepMode == nil || len(cfg.SleepMode.Windows) == 0 {
			return fn(w, r)
		}

		now := time.Now()
		if !sleep.IsActive(cfg.SleepMode.Windows, now) {
			return fn(w, r)
		}

		// Check if this is a cron-triggered mission (exempt from sleep guard).
		// We need to peek at the body without consuming it.
		if r.Body != nil && r.Method == http.MethodPost {
			bodyBytes, err := io.ReadAll(r.Body)
			r.Body.Close()
			// Restore body for the inner handler
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
```

Add imports for `"bytes"`, `"io"`, and `"github.com/odyssey/agenc/internal/sleep"` to middleware.go.

**Step 4: Wrap `POST /missions` with `sleepGuard` in `server.go`**

Change line 233 from:
```go
mux.Handle("POST /missions", appHandler(s.requestLogger, s.stashGuard(s.handleCreateMission)))
```
to:
```go
mux.Handle("POST /missions", appHandler(s.requestLogger, s.sleepGuard(s.stashGuard(s.handleCreateMission))))
```

Sleep guard runs first (outermost), then stash guard, then the handler.

**Step 5: Run tests**

Run: `go test ./internal/server/ -v -run TestSleepGuard`
Expected: All pass.

**Step 6: Run full server tests to check for regressions**

Run: `go test ./internal/server/ -v`
Expected: All pass.

**Step 7: Commit**

```
git add internal/server/middleware.go internal/server/middleware_test.go internal/server/server.go
git commit -m "Add sleepGuard middleware blocking mission creation during sleep windows"
```

---

### Task 6: Add client methods for sleep endpoints

**Files:**
- Modify: `internal/server/client.go` (add `ListSleepWindows`, `AddSleepWindow`, `RemoveSleepWindow`)

**Step 1: Add client methods**

```go
// ListSleepWindows returns the current sleep mode windows.
func (c *Client) ListSleepWindows() ([]sleep.WindowDef, error) {
	var result []sleep.WindowDef
	if err := c.Get("/config/sleep/windows", &result); err != nil {
		return nil, err
	}
	return result, nil
}

// AddSleepWindow adds a new sleep window and returns the updated list.
func (c *Client) AddSleepWindow(window sleep.WindowDef) ([]sleep.WindowDef, error) {
	var result []sleep.WindowDef
	if err := c.Post("/config/sleep/windows", window, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// RemoveSleepWindow removes a sleep window by index and returns the updated list.
func (c *Client) RemoveSleepWindow(index int) ([]sleep.WindowDef, error) {
	var result []sleep.WindowDef
	path := fmt.Sprintf("/config/sleep/windows/%d", index)
	if err := c.Delete(path); err != nil {
		return nil, err
	}
	return result, nil
}
```

Note: The `Delete` method on `Client` currently does not decode a response body (line 92-109 in client.go). `RemoveSleepWindow` may need to use a custom `Do` call to decode the response, or the `Delete` method can be extended. Check the existing `Delete` signature and adapt — possibly use `c.Do(req, &result)` or add a `DeleteWithResult` variant.

**Step 2: Commit**

```
git add internal/server/client.go
git commit -m "Add client methods for sleep window endpoints"
```

---

### Task 7: Add CLI commands (`agenc config sleep add/rm/ls`)

**Files:**
- Create: `cmd/config_sleep.go` (parent command)
- Create: `cmd/config_sleep_add.go`
- Create: `cmd/config_sleep_rm.go`
- Create: `cmd/config_sleep_ls.go`

**Step 1: Create the parent command**

```go
// cmd/config_sleep.go
package cmd

import "github.com/spf13/cobra"

var configSleepCmd = &cobra.Command{
	Use:   "sleep",
	Short: "Manage sleep mode windows",
	Long: `Manage sleep mode time windows that block mission and cron creation.

When sleep mode is active, the server rejects new mission creation (except
cron-triggered missions) and new cron creation. This encourages you to
stop working and go to sleep.

Configure one or more time windows with day-of-week and HH:MM start/end
times. Overnight windows (e.g., 22:00 to 06:00) are supported.

Example config.yml:

  sleepMode:
    windows:
      - days: [mon, tue, wed, thu]
        start: "22:00"
        end: "06:00"
      - days: [fri, sat]
        start: "23:00"
        end: "07:00"
`,
}

func init() {
	configCmd.AddCommand(configSleepCmd)
}
```

**Step 2: Create `config sleep ls`**

```go
// cmd/config_sleep_ls.go
package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var configSleepLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List sleep mode windows",
	RunE:  runConfigSleepLs,
}

func init() {
	configSleepCmd.AddCommand(configSleepLsCmd)
}

func runConfigSleepLs(cmd *cobra.Command, args []string) error {
	client, err := newServerClient()
	if err != nil {
		return err
	}
	windows, err := client.ListSleepWindows()
	if err != nil {
		return stacktrace.Propagate(err, "failed to list sleep windows")
	}
	if len(windows) == 0 {
		fmt.Println("No sleep windows configured")
		return nil
	}
	for i, w := range windows {
		fmt.Printf("  %d: %s %s–%s\n", i, strings.Join(w.Days, ","), w.Start, w.End)
	}
	return nil
}
```

**Step 3: Create `config sleep add`**

```go
// cmd/config_sleep_add.go
package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/sleep"
)

var configSleepAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a sleep mode window",
	Long: `Add a time window during which mission and cron creation is blocked.

Examples:
  agenc config sleep add --days mon,tue,wed,thu --start 22:00 --end 06:00
  agenc config sleep add --days fri,sat --start 23:00 --end 07:00`,
	RunE: runConfigSleepAdd,
}

func init() {
	configSleepCmd.AddCommand(configSleepAddCmd)
	configSleepAddCmd.Flags().String("days", "", "comma-separated day names (mon,tue,wed,thu,fri,sat,sun) (required)")
	configSleepAddCmd.Flags().String("start", "", "start time in HH:MM format (required)")
	configSleepAddCmd.Flags().String("end", "", "end time in HH:MM format (required)")
	_ = configSleepAddCmd.MarkFlagRequired("days")
	_ = configSleepAddCmd.MarkFlagRequired("start")
	_ = configSleepAddCmd.MarkFlagRequired("end")
}

func runConfigSleepAdd(cmd *cobra.Command, args []string) error {
	daysStr, _ := cmd.Flags().GetString("days")
	start, _ := cmd.Flags().GetString("start")
	end, _ := cmd.Flags().GetString("end")

	days := strings.Split(daysStr, ",")

	client, err := newServerClient()
	if err != nil {
		return err
	}
	windows, err := client.AddSleepWindow(sleep.WindowDef{
		Days:  days,
		Start: start,
		End:   end,
	})
	if err != nil {
		return stacktrace.Propagate(err, "failed to add sleep window")
	}

	fmt.Printf("Added sleep window: %s %s–%s (window %d)\n", daysStr, start, end, len(windows)-1)
	return nil
}
```

**Step 4: Create `config sleep rm`**

```go
// cmd/config_sleep_rm.go
package cmd

import (
	"fmt"
	"strconv"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var configSleepRmCmd = &cobra.Command{
	Use:   "rm <index>",
	Short: "Remove a sleep mode window",
	Long:  "Remove a sleep window by its index (shown in 'agenc config sleep ls').",
	Args:  cobra.ExactArgs(1),
	RunE:  runConfigSleepRm,
}

func init() {
	configSleepCmd.AddCommand(configSleepRmCmd)
}

func runConfigSleepRm(cmd *cobra.Command, args []string) error {
	index, err := strconv.Atoi(args[0])
	if err != nil {
		return stacktrace.NewError("invalid index %q: must be a number", args[0])
	}

	client, err := newServerClient()
	if err != nil {
		return err
	}
	if _, err := client.RemoveSleepWindow(index); err != nil {
		return stacktrace.Propagate(err, "failed to remove sleep window")
	}

	fmt.Printf("Removed sleep window %d\n", index)
	return nil
}
```

**Step 5: Find `newServerClient` helper**

Check existing CLI commands (e.g., `cmd/mission_ls.go` or similar) for the `newServerClient` pattern. The CLI commands that talk to the server use this helper. Match the existing pattern.

**Step 6: Run build**

Run: `make build` (with `dangerouslyDisableSandbox: true`)
Expected: Compiles successfully.

**Step 7: Manual smoke test with test environment**

```bash
make test-env
./_build/agenc-test server start
./_build/agenc-test config sleep ls
# Expected: "No sleep windows configured"

./_build/agenc-test config sleep add --days mon,tue,wed,thu --start 22:00 --end 06:00
# Expected: "Added sleep window: mon,tue,wed,thu 22:00–06:00 (window 0)"

./_build/agenc-test config sleep ls
# Expected: shows the window

./_build/agenc-test config sleep rm 0
# Expected: "Removed sleep window 0"

./_build/agenc-test config sleep ls
# Expected: "No sleep windows configured"
```

**Step 8: Commit**

```
git add cmd/config_sleep.go cmd/config_sleep_add.go cmd/config_sleep_rm.go cmd/config_sleep_ls.go
git commit -m "Add CLI commands for sleep mode window management"
```

---

### Task 8: Update docs and architecture

**Files:**
- Modify: `docs/system-architecture.md` (add `internal/sleep/` package description)
- Regenerate CLI docs if auto-generated (check if `make build` regenerates `docs/cli/`)

**Step 1: Add `internal/sleep/` to architecture doc**

Find the package listing section in `docs/system-architecture.md` and add:

```
internal/sleep/      — Sleep mode time-window matching and validation (pure library, no server dependency)
```

**Step 2: Run `make build` to regenerate CLI docs**

Run: `make build` (with `dangerouslyDisableSandbox: true`)
Check if `docs/cli/agenc_config_sleep*.md` files were generated.

**Step 3: Commit**

```
git add docs/system-architecture.md docs/cli/
git commit -m "Add sleep mode to architecture docs and regenerate CLI help"
```

---

### Task 9: E2E test

**Files:**
- Check existing E2E test structure and add a sleep mode E2E test

**Step 1: Find existing E2E test pattern**

Look at `make e2e` target and the E2E test files to understand the structure. Add a test that:

1. Starts server with test environment
2. Adds a sleep window covering "now" via `agenc-test config sleep add`
3. Attempts `agenc-test mission new` — expects failure with 403 / "Sleep mode active"
4. Removes the window via `agenc-test config sleep rm 0`
5. Attempts `agenc-test mission new` — expects success (or at least no sleep guard rejection)

**Step 2: Write the E2E test**

Follow the existing E2E test patterns in the project. Use `_build/agenc-test` as the binary.

**Step 3: Run E2E**

Run: `make e2e` (with `dangerouslyDisableSandbox: true`)
Expected: All pass including the new sleep mode test.

**Step 4: Commit**

```
git add <e2e test files>
git commit -m "Add E2E test for sleep mode guard"
```

---

### Task 10: Final quality check

**Step 1: Run full quality pipeline**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: All checks pass (tidy, fmt, vet, lint, vulncheck, deadcode, tests).

**Step 2: Run E2E**

Run: `make e2e` (with `dangerouslyDisableSandbox: true`)
Expected: All pass.

**Step 3: Push**

```
git push
```
