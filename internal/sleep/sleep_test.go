package sleep

import (
	"testing"
	"time"
)

func TestValidateDays(t *testing.T) {
	tests := []struct {
		name    string
		days    []string
		wantErr bool
	}{
		{
			name:    "valid weekdays",
			days:    []string{"mon", "tue", "wed", "thu", "fri"},
			wantErr: false,
		},
		{
			name:    "all days",
			days:    []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"},
			wantErr: false,
		},
		{
			name:    "invalid day name",
			days:    []string{"mon", "holiday"},
			wantErr: true,
		},
		{
			name:    "empty list",
			days:    []string{},
			wantErr: true,
		},
		{
			name:    "nil list",
			days:    nil,
			wantErr: true,
		},
		{
			name:    "duplicate day",
			days:    []string{"mon", "tue", "mon"},
			wantErr: true,
		},
		{
			name:    "single valid day",
			days:    []string{"sat"},
			wantErr: false,
		},
		{
			name:    "mixed case rejected",
			days:    []string{"Mon", "TUE"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDays(tt.days)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDays(%v) error = %v, wantErr %v", tt.days, err, tt.wantErr)
			}
		})
	}
}

func TestValidateTime(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid time",
			input:   "14:30",
			wantErr: false,
		},
		{
			name:    "midnight",
			input:   "00:00",
			wantErr: false,
		},
		{
			name:    "end of day",
			input:   "23:59",
			wantErr: false,
		},
		{
			name:    "invalid hour 24",
			input:   "24:00",
			wantErr: true,
		},
		{
			name:    "invalid minute 60",
			input:   "12:60",
			wantErr: true,
		},
		{
			name:    "bad format no colon",
			input:   "1430",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "letters",
			input:   "ab:cd",
			wantErr: true,
		},
		{
			name:    "single digit hour",
			input:   "9:30",
			wantErr: true,
		},
		{
			name:    "extra colon",
			input:   "12:30:00",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTime(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTime(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestIsActive(t *testing.T) {
	// March 25, 2026 is a Wednesday
	weekdayWindow := WindowDef{
		Days:  []string{"mon", "tue", "wed", "thu"},
		Start: "20:45",
		End:   "23:00",
	}
	overnightWindow := WindowDef{
		Days:  []string{"mon", "tue", "wed", "thu"},
		Start: "20:45",
		End:   "06:00",
	}

	tests := []struct {
		name    string
		windows []WindowDef
		now     time.Time
		want    bool
	}{
		{
			name:    "nil windows",
			windows: nil,
			now:     time.Date(2026, 3, 25, 21, 0, 0, 0, time.UTC),
			want:    false,
		},
		{
			name:    "empty windows",
			windows: []WindowDef{},
			now:     time.Date(2026, 3, 25, 21, 0, 0, 0, time.UTC),
			want:    false,
		},
		{
			name:    "same-day window active within range",
			windows: []WindowDef{weekdayWindow},
			now:     time.Date(2026, 3, 25, 21, 0, 0, 0, time.UTC), // Wed 21:00
			want:    true,
		},
		{
			name:    "same-day window inactive before start",
			windows: []WindowDef{weekdayWindow},
			now:     time.Date(2026, 3, 25, 20, 0, 0, 0, time.UTC), // Wed 20:00
			want:    false,
		},
		{
			name:    "same-day window inactive wrong day",
			windows: []WindowDef{weekdayWindow},
			now:     time.Date(2026, 3, 28, 21, 0, 0, 0, time.UTC), // Sat 21:00
			want:    false,
		},
		{
			name:    "overnight window active before midnight",
			windows: []WindowDef{overnightWindow},
			now:     time.Date(2026, 3, 25, 23, 0, 0, 0, time.UTC), // Wed 23:00
			want:    true,
		},
		{
			name:    "overnight window active after midnight",
			windows: []WindowDef{overnightWindow},
			now:     time.Date(2026, 3, 26, 2, 0, 0, 0, time.UTC), // Thu 2am, window started Wed
			want:    true,
		},
		{
			name:    "overnight window inactive after end",
			windows: []WindowDef{overnightWindow},
			now:     time.Date(2026, 3, 26, 7, 0, 0, 0, time.UTC), // Thu 7am
			want:    false,
		},
		{
			name: "overnight window inactive wrong start day",
			windows: []WindowDef{{
				Days:  []string{"mon"},
				Start: "20:45",
				End:   "06:00",
			}},
			now:  time.Date(2026, 3, 26, 2, 0, 0, 0, time.UTC), // Thu 2am, window is mon only
			want: false,
		},
		{
			name: "sunday keyword works",
			windows: []WindowDef{{
				Days:  []string{"sun"},
				Start: "10:00",
				End:   "18:00",
			}},
			now:  time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC), // Sun 12:00
			want: true,
		},
		{
			name:    "exactly at start time is active",
			windows: []WindowDef{weekdayWindow},
			now:     time.Date(2026, 3, 25, 20, 45, 0, 0, time.UTC), // Wed 20:45
			want:    true,
		},
		{
			name:    "exactly at end time is not active",
			windows: []WindowDef{weekdayWindow},
			now:     time.Date(2026, 3, 25, 23, 0, 0, 0, time.UTC), // Wed 23:00
			want:    false,
		},
		{
			name: "multiple windows first matches",
			windows: []WindowDef{
				weekdayWindow,
				{Days: []string{"sat"}, Start: "10:00", End: "18:00"},
			},
			now:  time.Date(2026, 3, 25, 21, 0, 0, 0, time.UTC), // Wed 21:00
			want: true,
		},
		{
			name: "multiple windows second matches",
			windows: []WindowDef{
				{Days: []string{"sat"}, Start: "10:00", End: "18:00"},
				weekdayWindow,
			},
			now:  time.Date(2026, 3, 25, 21, 0, 0, 0, time.UTC), // Wed 21:00
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
		wantEnd string
		wantOk  bool
	}{
		{
			name: "returns end time of active window",
			windows: []WindowDef{{
				Days:  []string{"wed"},
				Start: "20:00",
				End:   "23:00",
			}},
			now:     time.Date(2026, 3, 25, 21, 0, 0, 0, time.UTC), // Wed 21:00
			wantEnd: "23:00",
			wantOk:  true,
		},
		{
			name: "no active window returns empty and false",
			windows: []WindowDef{{
				Days:  []string{"sat"},
				Start: "10:00",
				End:   "18:00",
			}},
			now:     time.Date(2026, 3, 25, 21, 0, 0, 0, time.UTC), // Wed 21:00
			wantEnd: "",
			wantOk:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotEnd, gotOk := FindActiveWindowEnd(tt.windows, tt.now)
			if gotEnd != tt.wantEnd || gotOk != tt.wantOk {
				t.Errorf("FindActiveWindowEnd() = (%q, %v), want (%q, %v)", gotEnd, gotOk, tt.wantEnd, tt.wantOk)
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
		{
			name: "valid overnight window",
			window: WindowDef{
				Days:  []string{"mon", "tue", "wed", "thu", "fri"},
				Start: "22:00",
				End:   "06:00",
			},
			wantErr: false,
		},
		{
			name: "valid same-day window",
			window: WindowDef{
				Days:  []string{"sat", "sun"},
				Start: "09:00",
				End:   "17:00",
			},
			wantErr: false,
		},
		{
			name: "start equals end rejected",
			window: WindowDef{
				Days:  []string{"mon"},
				Start: "12:00",
				End:   "12:00",
			},
			wantErr: true,
		},
		{
			name: "invalid day",
			window: WindowDef{
				Days:  []string{"funday"},
				Start: "09:00",
				End:   "17:00",
			},
			wantErr: true,
		},
		{
			name: "invalid start time",
			window: WindowDef{
				Days:  []string{"mon"},
				Start: "25:00",
				End:   "06:00",
			},
			wantErr: true,
		},
		{
			name: "invalid end time",
			window: WindowDef{
				Days:  []string{"mon"},
				Start: "22:00",
				End:   "bad",
			},
			wantErr: true,
		},
		{
			name: "empty days",
			window: WindowDef{
				Days:  []string{},
				Start: "09:00",
				End:   "17:00",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWindow(tt.window)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWindow(%+v) error = %v, wantErr %v", tt.window, err, tt.wantErr)
			}
		})
	}
}
