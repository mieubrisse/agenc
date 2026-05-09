package cmd

import (
	"testing"
	"time"

	"github.com/odyssey/agenc/internal/database"
)

func timePtr(t time.Time) *time.Time { return &t }
func strPtr(s string) *string        { return &s }

func TestSortMissionsForPicker(t *testing.T) {
	now := time.Now().UTC()

	needsAttention := strPtr("needs_attention")
	busy := strPtr("busy")
	idle := strPtr("idle")

	tests := []struct {
		name     string
		missions []*database.Mission
		wantIDs  []string // expected order of ShortIDs
	}{
		{
			name: "needs_attention sorts first",
			missions: []*database.Mission{
				{ShortID: "busy1", ClaudeState: busy, LastHeartbeat: timePtr(now)},
				{ShortID: "attn1", ClaudeState: needsAttention, LastHeartbeat: timePtr(now)},
				{ShortID: "idle1", ClaudeState: idle, LastHeartbeat: timePtr(now)},
			},
			wantIDs: []string{"attn1", "busy1", "idle1"},
		},
		{
			name: "within same tier, sort by last_user_prompt_at DESC",
			missions: []*database.Mission{
				{ShortID: "old", ClaudeState: busy, LastUserPromptAt: timePtr(now.Add(-1 * time.Hour))},
				{ShortID: "new", ClaudeState: busy, LastUserPromptAt: timePtr(now)},
			},
			wantIDs: []string{"new", "old"},
		},
		{
			name: "fresh unprompted mission outranks older prompted mission",
			missions: []*database.Mission{
				{ShortID: "older_prompted", ClaudeState: busy, CreatedAt: now.Add(-3 * time.Hour), LastUserPromptAt: timePtr(now.Add(-1 * time.Hour))},
				{ShortID: "fresh_unprompted", ClaudeState: busy, CreatedAt: now, LastUserPromptAt: nil},
			},
			wantIDs: []string{"fresh_unprompted", "older_prompted"},
		},
		{
			name: "unprompted missions order by created_at DESC",
			missions: []*database.Mission{
				{ShortID: "older", CreatedAt: now.Add(-2 * time.Hour)},
				{ShortID: "newer", CreatedAt: now.Add(-1 * time.Hour)},
			},
			wantIDs: []string{"newer", "older"},
		},
		{
			name: "multiple needs_attention sorted by last_user_prompt_at",
			missions: []*database.Mission{
				{ShortID: "attn_old", ClaudeState: needsAttention, LastUserPromptAt: timePtr(now.Add(-2 * time.Hour))},
				{ShortID: "attn_new", ClaudeState: needsAttention, LastUserPromptAt: timePtr(now)},
				{ShortID: "busy1", ClaudeState: busy, LastUserPromptAt: timePtr(now)},
			},
			wantIDs: []string{"attn_new", "attn_old", "busy1"},
		},
		{
			name: "nil claude_state treated as non-attention",
			missions: []*database.Mission{
				{ShortID: "stopped", ClaudeState: nil, LastUserPromptAt: timePtr(now)},
				{ShortID: "attn", ClaudeState: needsAttention, LastUserPromptAt: timePtr(now.Add(-1 * time.Hour))},
			},
			wantIDs: []string{"attn", "stopped"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortMissionsForPicker(tt.missions)
			for i, want := range tt.wantIDs {
				if tt.missions[i].ShortID != want {
					t.Errorf("position %d: want %s, got %s", i, want, tt.missions[i].ShortID)
				}
			}
		})
	}
}
