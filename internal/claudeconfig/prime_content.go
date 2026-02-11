package claudeconfig

import (
	_ "embed"
)

// primeContent is the AgenC CLI quick reference generated at build time by
// cmd/genskill from the Cobra command tree. It is embedded via go:embed so
// it stays in sync with the CLI automatically.
//
//go:embed prime_content.md
var primeContent string

// GetPrimeContent returns the AgenC CLI quick reference content, suitable for
// printing to stdout or injecting into an agent session via a hook.
func GetPrimeContent() string {
	return primeContent
}
