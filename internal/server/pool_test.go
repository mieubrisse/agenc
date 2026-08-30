package server

import "testing"

func TestComputeMissionAttached(t *testing.T) {
	attachedPane := "42"
	unlinkedPane := "99"
	linked := map[string]bool{"42": true}

	if !computeMissionAttached(&attachedPane, linked) {
		t.Errorf("pane in linked set should report attached")
	}
	if computeMissionAttached(&unlinkedPane, linked) {
		t.Errorf("pane absent from linked set should report not attached")
	}
	if computeMissionAttached(nil, linked) {
		t.Errorf("nil pane should report not attached")
	}
}

func TestResolveUnlinkTargets(t *testing.T) {
	tests := []struct {
		name           string
		callerSession  string
		linkedSessions []string
		expected       []string
	}{
		{
			name:           "caller session holds the window: unlink from it alone",
			callerSession:  "cockpit",
			linkedSessions: []string{"cockpit"},
			expected:       []string{"cockpit"},
		},
		{
			name:           "window in several sessions including the caller's: unlink from the caller's only",
			callerSession:  "cockpit",
			linkedSessions: []string{"cockpit", "deepwork", "hyperspace"},
			expected:       []string{"cockpit"},
		},
		{
			name:           "window in one other session: unlink from that session",
			callerSession:  "cockpit",
			linkedSessions: []string{"hyperspace"},
			expected:       []string{"hyperspace"},
		},
		{
			name:           "window in several other sessions: unlink from all of them",
			callerSession:  "cockpit",
			linkedSessions: []string{"deepwork", "hyperspace"},
			expected:       []string{"deepwork", "hyperspace"},
		},
		{
			name:           "window is pool-only: nothing to unlink",
			callerSession:  "cockpit",
			linkedSessions: nil,
			expected:       nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := resolveUnlinkTargets(test.callerSession, test.linkedSessions)
			if len(actual) != len(test.expected) {
				t.Fatalf("expected unlink targets %v, got %v", test.expected, actual)
			}
			for i := range test.expected {
				if actual[i] != test.expected[i] {
					t.Errorf("expected unlink targets %v, got %v", test.expected, actual)
					break
				}
			}
		})
	}
}
