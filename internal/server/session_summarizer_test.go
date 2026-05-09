package server

import (
	"strings"
	"testing"
)

func TestBuildSummarizerSystemPrompt(t *testing.T) {
	tests := []struct {
		maxWords int
		wantSub  string
	}{
		{maxWords: 15, wantSub: "3-15 word"},
		{maxWords: 10, wantSub: "3-10 word"},
		{maxWords: 50, wantSub: "3-50 word"},
		{maxWords: 3, wantSub: "3-3 word"},
	}
	for _, tt := range tests {
		got := buildSummarizerSystemPrompt(tt.maxWords)
		if !strings.Contains(got, tt.wantSub) {
			t.Errorf("buildSummarizerSystemPrompt(%d) = %q; want substring %q",
				tt.maxWords, got, tt.wantSub)
		}
		// Sanity: the prompt should still mention "title generator" — guards
		// against accidental gutting of the rest of the prompt body.
		if !strings.Contains(got, "title generator") {
			t.Errorf("buildSummarizerSystemPrompt(%d) lost the 'title generator' phrase", tt.maxWords)
		}
	}
}
