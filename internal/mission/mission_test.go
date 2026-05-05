package mission

import (
	"reflect"
	"testing"
)

func TestBuildResumeArgs(t *testing.T) {
	tests := []struct {
		name          string
		sessionID     string
		initialPrompt string
		want          []string
	}{
		{
			name:          "session id, no prompt",
			sessionID:     "sess-abc",
			initialPrompt: "",
			want:          []string{"-r", "sess-abc"},
		},
		{
			name:          "no session id, no prompt",
			sessionID:     "",
			initialPrompt: "",
			want:          []string{"-c"},
		},
		{
			name:          "session id with prompt",
			sessionID:     "sess-abc",
			initialPrompt: "follow up please",
			want:          []string{"-r", "sess-abc", "follow up please"},
		},
		{
			name:          "no session id with prompt",
			sessionID:     "",
			initialPrompt: "follow up please",
			want:          []string{"-c", "follow up please"},
		},
		{
			name:          "prompt with shell metachars preserved literally",
			sessionID:     "sess-abc",
			initialPrompt: "$(rm -rf /); echo 'hi'",
			want:          []string{"-r", "sess-abc", "$(rm -rf /); echo 'hi'"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildResumeArgs(tt.sessionID, tt.initialPrompt)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildResumeArgs(%q, %q) = %v, want %v", tt.sessionID, tt.initialPrompt, got, tt.want)
			}
		})
	}
}
