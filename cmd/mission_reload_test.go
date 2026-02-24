package cmd

import (
	"testing"
)

func TestLooksLikeMissionID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid short ID - lowercase",
			input:    "a1b2c3d4",
			expected: true,
		},
		{
			name:     "valid short ID - uppercase",
			input:    "A1B2C3D4",
			expected: true,
		},
		{
			name:     "valid short ID - mixed case",
			input:    "aB1c2D3e",
			expected: true,
		},
		{
			name:     "valid full UUID",
			input:    "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d",
			expected: true,
		},
		{
			name:     "too short",
			input:    "a1b2c3",
			expected: false,
		},
		{
			name:     "too long",
			input:    "a1b2c3d4e5",
			expected: false,
		},
		{
			name:     "contains non-hex characters",
			input:    "g1h2i3j4",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "search terms",
			input:    "my mission",
			expected: false,
		},
		{
			name:     "invalid UUID format",
			input:    "a1b2c3d4-e5f6-g7h8-i9j0-k1l2m3n4o5p6",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := looksLikeMissionID(tt.input)
			if result != tt.expected {
				t.Errorf("looksLikeMissionID(%q) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestAllLookLikeMissionIDs(t *testing.T) {
	tests := []struct {
		name     string
		inputs   []string
		expected bool
	}{
		{
			name:     "all valid short IDs",
			inputs:   []string{"a1b2c3d4", "e5f6a7b8"},
			expected: true,
		},
		{
			name:     "all valid UUIDs",
			inputs:   []string{"a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d", "f1e2d3c4-b5a6-4978-8675-309aabbccdd1"},
			expected: true,
		},
		{
			name:     "mixed valid and invalid",
			inputs:   []string{"a1b2c3d4", "invalid"},
			expected: false,
		},
		{
			name:     "single invalid",
			inputs:   []string{"not-a-mission-id"},
			expected: false,
		},
		{
			name:     "empty slice",
			inputs:   []string{},
			expected: false,
		},
		{
			name:     "single valid",
			inputs:   []string{"a1b2c3d4"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := allLookLikeMissionIDs(tt.inputs)
			if result != tt.expected {
				t.Errorf("allLookLikeMissionIDs(%v) = %v, expected %v", tt.inputs, result, tt.expected)
			}
		})
	}
}

// Note: Integration tests that actually interact with tmux, the database, and
// running wrappers should be added in a separate integration test suite.
// These unit tests focus on the helper functions and input validation logic.
