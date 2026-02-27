package claudeconfig

import (
	"encoding/json"
	"testing"
)

func TestMergeCredentialJSON(t *testing.T) {
	t.Run("overlay adds new mcpOAuth server", func(t *testing.T) {
		base := `{"claudeAiOauth":{"accessToken":"base-token"}}`
		overlay := `{"claudeAiOauth":{"accessToken":"base-token"},"mcpOAuth":{"todoist|abc":{"accessToken":"tok1","expiresAt":1000}}}`

		merged, changed, err := MergeCredentialJSON([]byte(base), []byte(overlay))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !changed {
			t.Fatal("expected changed=true")
		}

		var result map[string]json.RawMessage
		if err := json.Unmarshal(merged, &result); err != nil {
			t.Fatalf("failed to parse merged result: %v", err)
		}
		if _, ok := result["mcpOAuth"]; !ok {
			t.Fatal("expected mcpOAuth in merged result")
		}
	})

	t.Run("overlay wins for non-mcpOAuth top-level keys", func(t *testing.T) {
		base := `{"claudeAiOauth":{"accessToken":"old-token"}}`
		overlay := `{"claudeAiOauth":{"accessToken":"new-token"}}`

		merged, changed, err := MergeCredentialJSON([]byte(base), []byte(overlay))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !changed {
			t.Fatal("expected changed=true")
		}

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
	})

	t.Run("mcpOAuth keeps newer expiresAt from overlay", func(t *testing.T) {
		base := `{"mcpOAuth":{"todoist|abc":{"accessToken":"old","expiresAt":1000}}}`
		overlay := `{"mcpOAuth":{"todoist|abc":{"accessToken":"new","expiresAt":2000}}}`

		merged, changed, err := MergeCredentialJSON([]byte(base), []byte(overlay))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !changed {
			t.Fatal("expected changed=true")
		}

		token := extractMcpOAuthToken(t, merged, "todoist|abc")
		if token != "new" {
			t.Errorf("expected overlay token 'new', got %q", token)
		}
	})

	t.Run("mcpOAuth keeps newer expiresAt from base", func(t *testing.T) {
		base := `{"mcpOAuth":{"todoist|abc":{"accessToken":"base","expiresAt":3000}}}`
		overlay := `{"mcpOAuth":{"todoist|abc":{"accessToken":"overlay","expiresAt":1000}}}`

		merged, _, err := MergeCredentialJSON([]byte(base), []byte(overlay))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		token := extractMcpOAuthToken(t, merged, "todoist|abc")
		if token != "base" {
			t.Errorf("expected base token 'base', got %q", token)
		}
	})

	t.Run("mcpOAuth merges servers from both sides", func(t *testing.T) {
		base := `{"mcpOAuth":{"server-a|111":{"accessToken":"a","expiresAt":1000}}}`
		overlay := `{"mcpOAuth":{"server-b|222":{"accessToken":"b","expiresAt":2000}}}`

		merged, changed, err := MergeCredentialJSON([]byte(base), []byte(overlay))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !changed {
			t.Fatal("expected changed=true")
		}

		tokenA := extractMcpOAuthToken(t, merged, "server-a|111")
		tokenB := extractMcpOAuthToken(t, merged, "server-b|222")
		if tokenA != "a" {
			t.Errorf("expected server-a token 'a', got %q", tokenA)
		}
		if tokenB != "b" {
			t.Errorf("expected server-b token 'b', got %q", tokenB)
		}
	})

	t.Run("no change when overlay equals base", func(t *testing.T) {
		base := `{"claudeAiOauth":{"accessToken":"same"}}`
		overlay := `{"claudeAiOauth":{"accessToken":"same"}}`

		_, changed, err := MergeCredentialJSON([]byte(base), []byte(overlay))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if changed {
			t.Fatal("expected changed=false when overlay equals base")
		}
	})

	t.Run("missing expiresAt defaults to zero", func(t *testing.T) {
		base := `{"mcpOAuth":{"s|1":{"accessToken":"base"}}}`
		overlay := `{"mcpOAuth":{"s|1":{"accessToken":"overlay","expiresAt":100}}}`

		merged, _, err := MergeCredentialJSON([]byte(base), []byte(overlay))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		token := extractMcpOAuthToken(t, merged, "s|1")
		if token != "overlay" {
			t.Errorf("expected overlay to win when base lacks expiresAt, got %q", token)
		}
	})

	t.Run("invalid base JSON returns error", func(t *testing.T) {
		_, _, err := MergeCredentialJSON([]byte("not json"), []byte(`{}`))
		if err == nil {
			t.Fatal("expected error for invalid base JSON")
		}
	})

	t.Run("invalid overlay JSON returns error", func(t *testing.T) {
		_, _, err := MergeCredentialJSON([]byte(`{}`), []byte("not json"))
		if err == nil {
			t.Fatal("expected error for invalid overlay JSON")
		}
	})

	t.Run("base-only mcpOAuth preserved when overlay has no mcpOAuth", func(t *testing.T) {
		base := `{"mcpOAuth":{"s|1":{"accessToken":"base","expiresAt":1000}}}`
		overlay := `{"claudeAiOauth":{"accessToken":"new"}}`

		merged, changed, err := MergeCredentialJSON([]byte(base), []byte(overlay))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !changed {
			t.Fatal("expected changed=true")
		}

		token := extractMcpOAuthToken(t, merged, "s|1")
		if token != "base" {
			t.Errorf("expected base mcpOAuth to be preserved, got token %q", token)
		}
	})
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
