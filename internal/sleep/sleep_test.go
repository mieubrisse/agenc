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
