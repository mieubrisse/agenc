package mission

import (
	"reflect"
	"testing"
)

func TestParseMissionEnvAssignments(t *testing.T) {
	tests := []struct {
		name        string
		assignments []string
		want        map[string]string
		wantErr     bool
	}{
		{
			name:        "no assignments yields nil",
			assignments: nil,
			want:        nil,
		},
		{
			name:        "single assignment",
			assignments: []string{"REFLECTION_SESSION=1"},
			want:        map[string]string{"REFLECTION_SESSION": "1"},
		},
		{
			name:        "several assignments",
			assignments: []string{"A=1", "B=2"},
			want:        map[string]string{"A": "1", "B": "2"},
		},
		{
			// The split is on the first separator, so a value carrying one of
			// its own survives. A JSON fragment or a URL query routinely does.
			name:        "value containing the separator",
			assignments: []string{"CONFIG={\"a\":1}", "QUERY=x=1&y=2"},
			want:        map[string]string{"CONFIG": "{\"a\":1}", "QUERY": "x=1&y=2"},
		},
		{
			// A comma is not a delimiter here. This is the regression guard for
			// StringArrayVar vs StringSliceVar: the slice variant would have cut
			// this value into two.
			name:        "value containing commas is one value",
			assignments: []string{"LIST=a,b,c"},
			want:        map[string]string{"LIST": "a,b,c"},
		},
		{
			name:        "empty value is kept",
			assignments: []string{"EMPTY="},
			want:        map[string]string{"EMPTY": ""},
		},
		{
			name:        "value with spaces and newlines is kept verbatim",
			assignments: []string{"NOTE=line one\nline two with 'quotes'"},
			want:        map[string]string{"NOTE": "line one\nline two with 'quotes'"},
		},
		{
			// One value per name is all a process environment can carry, so the
			// resolution is AgenC's to make rather than Go's exec to inherit.
			name:        "repeated name is last-wins",
			assignments: []string{"A=first", "A=second"},
			want:        map[string]string{"A": "second"},
		},
		{
			name:        "assignment without a separator is an error",
			assignments: []string{"NOTANASSIGNMENT"},
			wantErr:     true,
		},
		{
			name:        "empty name is an error",
			assignments: []string{"=value"},
			wantErr:     true,
		},
		{
			name:        "name starting with a digit is an error",
			assignments: []string{"1BAD=value"},
			wantErr:     true,
		},
		{
			name:        "name with a dash is an error",
			assignments: []string{"BAD-NAME=value"},
			wantErr:     true,
		},
		{
			name:        "name with a space is an error",
			assignments: []string{"BAD NAME=value"},
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMissionEnvAssignments(tt.assignments)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseMissionEnvAssignments(%v) returned no error, want one", tt.assignments)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMissionEnvAssignments(%v) returned error: %v", tt.assignments, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseMissionEnvAssignments(%v) = %v, want %v", tt.assignments, got, tt.want)
			}
		})
	}
}

// TestValidateMissionEnvRejectsNulByteValue covers the one case the CLI cannot
// reach: argv carries no NUL, but the server's JSON body can, and the exec
// syscall rejects the whole environment rather than truncating the one value.
func TestValidateMissionEnvRejectsNulByteValue(t *testing.T) {
	err := ValidateMissionEnv(map[string]string{"MARKER": "before\x00after"})
	if err == nil {
		t.Fatal("ValidateMissionEnv accepted a value containing a NUL byte, want an error")
	}
}

func TestMissionEnvToAssignments(t *testing.T) {
	tests := []struct {
		name       string
		missionEnv map[string]string
		want       []string
	}{
		{
			name:       "nil map yields no assignments",
			missionEnv: nil,
			want:       []string{},
		},
		{
			// Sorted, because the map's iteration order is random and this
			// output lands on a real process: an unsorted environment makes two
			// spawns of the same mission differ for no reason.
			name:       "assignments are sorted by name",
			missionEnv: map[string]string{"ZED": "z", "ALPHA": "a", "MID": "m"},
			want:       []string{"ALPHA=a", "MID=m", "ZED=z"},
		},
		{
			name:       "value is rendered verbatim",
			missionEnv: map[string]string{"NOTE": "a b\nc"},
			want:       []string{"NOTE=a b\nc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MissionEnvToAssignments(tt.missionEnv)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MissionEnvToAssignments(%v) = %v, want %v", tt.missionEnv, got, tt.want)
			}
		})
	}
}

func TestFormatMissionEnv(t *testing.T) {
	tests := []struct {
		name       string
		missionEnv map[string]string
		want       string
	}{
		{
			name:       "nil map renders nothing",
			missionEnv: nil,
			want:       "",
		},
		{
			name:       "plain assignments render unquoted and sorted",
			missionEnv: map[string]string{"B": "2", "A": "1"},
			want:       "A=1 B=2",
		},
		{
			// The line sits in an aligned block callers read line by line, so a
			// value's newline is escaped rather than emitted.
			name:       "a multi-line value is escaped onto one line",
			missionEnv: map[string]string{"NOTE": "one\ntwo"},
			want:       `"NOTE=one\ntwo"`,
		},
		{
			name:       "a value with a space is quoted",
			missionEnv: map[string]string{"NOTE": "two words"},
			want:       `"NOTE=two words"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatMissionEnv(tt.missionEnv)
			if got != tt.want {
				t.Errorf("FormatMissionEnv(%v) = %q, want %q", tt.missionEnv, got, tt.want)
			}
		})
	}
}
