package claudeconfig

import (
	"testing"
)

func TestComputeCredentialHash(t *testing.T) {
	t.Run("equivalent JSON with different whitespace produces same hash", func(t *testing.T) {
		compact := `{"claudeAiOauth":{"accessToken":"abc","expiresAt":1700000000}}`
		spaced := `{ "claudeAiOauth" : { "accessToken" : "abc" , "expiresAt" : 1700000000 } }`

		hash1, err := ComputeCredentialHash(compact)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		hash2, err := ComputeCredentialHash(spaced)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hash1 != hash2 {
			t.Errorf("expected same hash for equivalent JSON, got %q and %q", hash1, hash2)
		}
	})

	t.Run("equivalent JSON with different key order produces same hash", func(t *testing.T) {
		order1 := `{"a":"1","b":"2"}`
		order2 := `{"b":"2","a":"1"}`

		hash1, err := ComputeCredentialHash(order1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		hash2, err := ComputeCredentialHash(order2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hash1 != hash2 {
			t.Errorf("expected same hash for reordered keys, got %q and %q", hash1, hash2)
		}
	})

	t.Run("different JSON produces different hash", func(t *testing.T) {
		json1 := `{"accessToken":"abc"}`
		json2 := `{"accessToken":"xyz"}`

		hash1, err := ComputeCredentialHash(json1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		hash2, err := ComputeCredentialHash(json2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hash1 == hash2 {
			t.Errorf("expected different hashes for different JSON, both got %q", hash1)
		}
	})

	t.Run("hash is 64 hex characters", func(t *testing.T) {
		hash, err := ComputeCredentialHash(`{"key":"value"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(hash) != 64 {
			t.Errorf("expected 64-char hex digest, got %d chars: %q", len(hash), hash)
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		_, err := ComputeCredentialHash(`not json`)
		if err == nil {
			t.Error("expected error for invalid JSON, got nil")
		}
	})
}
