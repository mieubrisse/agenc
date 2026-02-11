package claudeconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/mieubrisse/stacktrace"
)

// ComputeCredentialHash normalizes a credential JSON blob and returns its
// SHA-256 hex digest. Normalization (unmarshal → marshal) ensures that
// whitespace and key-order differences do not produce different hashes.
func ComputeCredentialHash(credentialJSON string) (string, error) {
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(credentialJSON), &parsed); err != nil {
		return "", stacktrace.Propagate(err, "failed to unmarshal credential JSON for hashing")
	}

	normalized, err := json.Marshal(parsed)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to marshal normalized credential JSON")
	}

	hash := sha256.Sum256(normalized)
	return hex.EncodeToString(hash[:]), nil
}
