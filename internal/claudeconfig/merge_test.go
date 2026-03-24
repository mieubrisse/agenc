package claudeconfig

import (
	"encoding/json"
	"testing"
)

func TestMergeCredentialJSON(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }

	tests := []struct {
		name         string
		base         string
		overlay      string
		expectError  bool
		expectChange *bool
		expectTokens map[string]string
		customCheck  func(t *testing.T, merged []byte)
	}{
		{
			name:         "overlay adds new mcpOAuth server",
			base:         `{"claudeAiOauth":{"accessToken":"base-token"}}`,
			overlay:      `{"claudeAiOauth":{"accessToken":"base-token"},"mcpOAuth":{"todoist|abc":{"accessToken":"tok1","expiresAt":1000}}}`,
			expectChange: boolPtr(true),
			customCheck: func(t *testing.T, merged []byte) {
				var result map[string]json.RawMessage
				if err := json.Unmarshal(merged, &result); err != nil {
					t.Fatalf("failed to parse merged result: %v", err)
				}
				if _, ok := result["mcpOAuth"]; !ok {
					t.Fatal("expected mcpOAuth in merged result")
				}
			},
		},
		{
			name:         "overlay wins for non-mcpOAuth top-level keys",
			base:         `{"claudeAiOauth":{"accessToken":"old-token"}}`,
			overlay:      `{"claudeAiOauth":{"accessToken":"new-token"}}`,
			expectChange: boolPtr(true),
			customCheck: func(t *testing.T, merged []byte) {
				var result map[string]json.RawMessage
				if err := json.Unmarshal(merged, &result); err != nil {
					t.Fatalf("failed to parse merged result: %v", err)
				}

				var oauth map[string]json.RawMessage
				if err := json.Unmarshal(result["claudeAiOauth"], &oauth); err != nil {
					t.Fatalf("failed to parse claudeAiOauth: %v", err)
				}

				var token string
				if err := json.Unmarshal(oauth["accessToken"], &token); err != nil {
					t.Fatalf("failed to parse accessToken: %v", err)
				}
				if token != "new-token" {
					t.Errorf("expected overlay token 'new-token', got %q", token)
				}
			},
		},
		{
			name:         "mcpOAuth keeps newer expiresAt from overlay",
			base:         `{"mcpOAuth":{"todoist|abc":{"accessToken":"old","expiresAt":1000}}}`,
			overlay:      `{"mcpOAuth":{"todoist|abc":{"accessToken":"new","expiresAt":2000}}}`,
			expectChange: boolPtr(true),
			expectTokens: map[string]string{"todoist|abc": "new"},
		},
		{
			name:         "mcpOAuth keeps newer expiresAt from base",
			base:         `{"mcpOAuth":{"todoist|abc":{"accessToken":"base","expiresAt":3000}}}`,
			overlay:      `{"mcpOAuth":{"todoist|abc":{"accessToken":"overlay","expiresAt":1000}}}`,
			expectTokens: map[string]string{"todoist|abc": "base"},
		},
		{
			name:         "mcpOAuth merges servers from both sides",
			base:         `{"mcpOAuth":{"server-a|111":{"accessToken":"a","expiresAt":1000}}}`,
			overlay:      `{"mcpOAuth":{"server-b|222":{"accessToken":"b","expiresAt":2000}}}`,
			expectChange: boolPtr(true),
			expectTokens: map[string]string{
				"server-a|111": "a",
				"server-b|222": "b",
			},
		},
		{
			name:         "no change when overlay equals base",
			base:         `{"claudeAiOauth":{"accessToken":"same"}}`,
			overlay:      `{"claudeAiOauth":{"accessToken":"same"}}`,
			expectChange: boolPtr(false),
		},
		{
			name:         "missing expiresAt defaults to zero",
			base:         `{"mcpOAuth":{"s|1":{"accessToken":"base"}}}`,
			overlay:      `{"mcpOAuth":{"s|1":{"accessToken":"overlay","expiresAt":100}}}`,
			expectTokens: map[string]string{"s|1": "overlay"},
		},
		{
			name:        "invalid base JSON returns error",
			base:        "not json",
			overlay:     `{}`,
			expectError: true,
		},
		{
			name:        "invalid overlay JSON returns error",
			base:        `{}`,
			overlay:     "not json",
			expectError: true,
		},
		{
			name:         "base-only mcpOAuth preserved when overlay has no mcpOAuth",
			base:         `{"mcpOAuth":{"s|1":{"accessToken":"base","expiresAt":1000}}}`,
			overlay:      `{"claudeAiOauth":{"accessToken":"new"}}`,
			expectChange: boolPtr(true),
			expectTokens: map[string]string{"s|1": "base"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			merged, changed, err := MergeCredentialJSON([]byte(tc.base), []byte(tc.overlay))

			if tc.expectError {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.expectChange != nil {
				if changed != *tc.expectChange {
					t.Fatalf("expected changed=%v, got %v", *tc.expectChange, changed)
				}
			}

			for serverKey, wantToken := range tc.expectTokens {
				gotToken := extractMcpOAuthToken(t, merged, serverKey)
				if gotToken != wantToken {
					t.Errorf("server %q: expected token %q, got %q", serverKey, wantToken, gotToken)
				}
			}

			if tc.customCheck != nil {
				tc.customCheck(t, merged)
			}
		})
	}
}

// extractMcpOAuthToken is a test helper that extracts the accessToken for a
// given server key from a merged credential JSON blob.
func extractMcpOAuthToken(t *testing.T, merged []byte, serverKey string) string {
	t.Helper()

	var result map[string]json.RawMessage
	if err := json.Unmarshal(merged, &result); err != nil {
		t.Fatalf("failed to parse merged result: %v", err)
	}

	var mcpOAuth map[string]json.RawMessage
	if err := json.Unmarshal(result["mcpOAuth"], &mcpOAuth); err != nil {
		t.Fatalf("failed to parse mcpOAuth: %v", err)
	}

	serverData, ok := mcpOAuth[serverKey]
	if !ok {
		t.Fatalf("server key %q not found in mcpOAuth", serverKey)
	}

	var server map[string]json.RawMessage
	if err := json.Unmarshal(serverData, &server); err != nil {
		t.Fatalf("failed to parse server entry: %v", err)
	}

	var token string
	if err := json.Unmarshal(server["accessToken"], &token); err != nil {
		t.Fatalf("failed to parse accessToken: %v", err)
	}

	return token
}
