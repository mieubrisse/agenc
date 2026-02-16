package launchd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratePlistXML(t *testing.T) {
	tests := []struct {
		name     string
		plist    *Plist
		wantKeys []string
	}{
		{
			name: "basic plist with minute and hour",
			plist: &Plist{
				Label:            "agenc-cron-test",
				ProgramArguments: []string{"/usr/local/bin/agenc", "mission", "new", "--headless", "test prompt"},
				StartCalendarInterval: &CalendarInterval{
					Minute: intPtr(0),
					Hour:   intPtr(9),
				},
				StandardOutPath:   "/dev/null",
				StandardErrorPath: "/dev/null",
			},
			wantKeys: []string{"Label", "ProgramArguments", "StartCalendarInterval", "Minute", "Hour", "StandardOutPath", "StandardErrorPath"},
		},
		{
			name: "plist with all calendar fields",
			plist: &Plist{
				Label:            "agenc-cron-complex",
				ProgramArguments: []string{"/usr/local/bin/agenc", "mission", "new"},
				StartCalendarInterval: &CalendarInterval{
					Minute:  intPtr(30),
					Hour:    intPtr(14),
					Day:     intPtr(15),
					Month:   intPtr(6),
					Weekday: intPtr(2),
				},
				StandardOutPath:   "/dev/null",
				StandardErrorPath: "/dev/null",
			},
			wantKeys: []string{"Minute", "Hour", "Day", "Month", "Weekday"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			xml, err := tt.plist.GeneratePlistXML()
			if err != nil {
				t.Fatalf("GeneratePlistXML failed: %v", err)
			}

			xmlStr := string(xml)

			// Check for required XML structure
			if !strings.Contains(xmlStr, `<?xml version="1.0" encoding="UTF-8"?>`) {
				t.Errorf("missing XML declaration")
			}
			if !strings.Contains(xmlStr, `<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"`) {
				t.Errorf("missing DOCTYPE declaration")
			}
			if !strings.Contains(xmlStr, `<plist version="1.0">`) {
				t.Errorf("missing plist version")
			}

			// Check for expected keys
			for _, key := range tt.wantKeys {
				if !strings.Contains(xmlStr, "<key>"+key+"</key>") {
					t.Errorf("missing expected key: %s", key)
				}
			}
		})
	}
}

func TestParseCronExpression(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		want    *CalendarInterval
		wantErr bool
	}{
		{
			name: "simple hourly cron",
			expr: "0 9 * * *",
			want: &CalendarInterval{
				Minute: intPtr(0),
				Hour:   intPtr(9),
			},
		},
		{
			name: "specific minute and hour",
			expr: "30 14 * * *",
			want: &CalendarInterval{
				Minute: intPtr(30),
				Hour:   intPtr(14),
			},
		},
		{
			name: "all wildcards",
			expr: "* * * * *",
			want: &CalendarInterval{},
		},
		{
			name: "with day and month",
			expr: "0 12 15 6 *",
			want: &CalendarInterval{
				Minute: intPtr(0),
				Hour:   intPtr(12),
				Day:    intPtr(15),
				Month:  intPtr(6),
			},
		},
		{
			name: "with weekday",
			expr: "0 9 * * 1",
			want: &CalendarInterval{
				Minute:  intPtr(0),
				Hour:    intPtr(9),
				Weekday: intPtr(1),
			},
		},
		{
			name:    "invalid - too few fields",
			expr:    "0 9 *",
			wantErr: true,
		},
		{
			name:    "invalid - non-numeric minute",
			expr:    "abc 9 * * *",
			wantErr: true,
		},
		{
			name:    "invalid - minute out of range",
			expr:    "60 9 * * *",
			wantErr: true,
		},
		{
			name:    "invalid - hour out of range",
			expr:    "0 24 * * *",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCronExpression(tt.expr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseCronExpression() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			// Compare interval fields
			if !compareIntPtr(got.Minute, tt.want.Minute) {
				t.Errorf("Minute = %v, want %v", ptrToString(got.Minute), ptrToString(tt.want.Minute))
			}
			if !compareIntPtr(got.Hour, tt.want.Hour) {
				t.Errorf("Hour = %v, want %v", ptrToString(got.Hour), ptrToString(tt.want.Hour))
			}
			if !compareIntPtr(got.Day, tt.want.Day) {
				t.Errorf("Day = %v, want %v", ptrToString(got.Day), ptrToString(tt.want.Day))
			}
			if !compareIntPtr(got.Month, tt.want.Month) {
				t.Errorf("Month = %v, want %v", ptrToString(got.Month), ptrToString(tt.want.Month))
			}
			if !compareIntPtr(got.Weekday, tt.want.Weekday) {
				t.Errorf("Weekday = %v, want %v", ptrToString(got.Weekday), ptrToString(tt.want.Weekday))
			}
		})
	}
}

func TestCronToPlistFilename(t *testing.T) {
	tests := []struct {
		name     string
		cronName string
		want     string
	}{
		{
			name:     "simple name",
			cronName: "my-cron",
			want:     "agenc-cron-my-cron.plist",
		},
		{
			name:     "name with spaces",
			cronName: "my cron job",
			want:     "agenc-cron-my-cron-job.plist",
		},
		{
			name:     "name with special characters",
			cronName: "my@cron#job!",
			want:     "agenc-cron-mycronjob.plist",
		},
		{
			name:     "name with underscores",
			cronName: "my_cron_job",
			want:     "agenc-cron-my_cron_job.plist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CronToPlistFilename(tt.cronName)
			if got != tt.want {
				t.Errorf("CronToPlistFilename() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWriteToDisk(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	plistPath := filepath.Join(tempDir, "test.plist")

	plist := &Plist{
		Label:            "test",
		ProgramArguments: []string{"/bin/echo", "test"},
		StartCalendarInterval: &CalendarInterval{
			Minute: intPtr(0),
			Hour:   intPtr(9),
		},
		StandardOutPath:   "/dev/null",
		StandardErrorPath: "/dev/null",
	}

	err := plist.WriteToDisk(plistPath)
	if err != nil {
		t.Fatalf("WriteToDisk failed: %v", err)
	}

	// Check file exists
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		t.Errorf("plist file was not created")
	}

	// Check file permissions
	info, err := os.Stat(plistPath)
	if err != nil {
		t.Fatalf("failed to stat plist file: %v", err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("file permissions = %o, want 0644", info.Mode().Perm())
	}

	// Check file contents
	content, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatalf("failed to read plist file: %v", err)
	}
	contentStr := string(content)
	if !strings.Contains(contentStr, "<key>Label</key>") {
		t.Errorf("plist file missing Label key")
	}
}

// Helper functions

func intPtr(i int) *int {
	return &i
}

func compareIntPtr(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func ptrToString(p *int) string {
	if p == nil {
		return "nil"
	}
	return string(rune(*p + '0'))
}
