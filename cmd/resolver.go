package cmd

import (
	"strings"

	"github.com/mieubrisse/stacktrace"
)

// ResolveResult represents the outcome of a resolution operation.
type ResolveResult[T any] struct {
	Items        []T  // Selected items (empty if cancelled or no items)
	WasCancelled bool // True if user cancelled fzf (Ctrl-C)
}

// Resolver configures the generic resolution logic for type T.
type Resolver[T any] struct {
	// TryCanonical attempts to interpret the input as a canonical reference.
	// Returns (item, true, nil) if it's a valid canonical ref.
	// Returns (zero, false, nil) if it doesn't look canonical (fall through to search).
	// Returns (zero, false, error) if it looks canonical but is invalid.
	// Optional: if nil, canonical resolution is skipped.
	TryCanonical func(input string) (T, bool, error)

	// GetItems returns all available items for search and fzf display.
	GetItems func() ([]T, error)

	// ExtractText returns the searchable text representation of an item.
	// Used for sequential substring matching.
	ExtractText func(T) string

	// FormatRow formats an item as columns for fzf display.
	FormatRow func(T) []string

	// FzfPrompt is the prompt shown to the user in fzf.
	FzfPrompt string

	// FzfHeaders are the column headers for the fzf table.
	FzfHeaders []string

	// MultiSelect enables TAB multi-select in fzf.
	MultiSelect bool
}

// Resolve implements the 4-step resolution pattern:
//  1. Empty input → show fzf picker with all items
//  2. Input that looks canonical → try TryCanonical
//  3. Otherwise → all terms are search query for sequential substring matching
//  4. Exactly 1 match → auto-select; 0 or 2+ matches → fzf with initial query
//
// Callers should join positional args before calling: Resolve(strings.Join(args, " "), ...)
func Resolve[T any](input string, r Resolver[T]) (*ResolveResult[T], error) {
	input = strings.TrimSpace(input)

	// Step 1: Empty input → show fzf picker
	if input == "" {
		return resolveWithFzf(r, "")
	}

	// Step 2: Try canonical resolution (if configured)
	if r.TryCanonical != nil {
		item, isCanonical, err := r.TryCanonical(input)
		if err != nil {
			return nil, err
		}
		if isCanonical {
			return &ResolveResult[T]{Items: []T{item}}, nil
		}
	}

	// Step 3: Treat input as search terms
	items, err := r.GetItems()
	if err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return &ResolveResult[T]{Items: nil}, nil
	}

	terms := strings.Fields(input)
	matches := filterBySubstringMatch(items, terms, r.ExtractText)

	// Step 4: Single match → auto-select; otherwise → fzf
	if len(matches) == 1 {
		return &ResolveResult[T]{Items: matches}, nil
	}

	return resolveWithFzf(r, input)
}

// filterBySubstringMatch filters items using sequential substring matching.
func filterBySubstringMatch[T any](items []T, terms []string, extractText func(T) string) []T {
	if len(terms) == 0 {
		return items
	}

	var matches []T
	for _, item := range items {
		text := extractText(item)
		if matchesSequentialSubstrings(text, terms) {
			matches = append(matches, item)
		}
	}
	return matches
}

// resolveWithFzf shows the fzf picker with all items.
func resolveWithFzf[T any](r Resolver[T], initialQuery string) (*ResolveResult[T], error) {
	items, err := r.GetItems()
	if err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return &ResolveResult[T]{Items: nil}, nil
	}

	// Build rows for fzf
	rows := make([][]string, len(items))
	for i, item := range items {
		rows[i] = r.FormatRow(item)
	}

	indices, err := runFzfPicker(FzfPickerConfig{
		Prompt:       r.FzfPrompt,
		Headers:      r.FzfHeaders,
		Rows:         rows,
		MultiSelect:  r.MultiSelect,
		InitialQuery: initialQuery,
	})
	if err != nil {
		return nil, stacktrace.Propagate(err, "'fzf' binary not found in PATH; pass arguments instead")
	}

	// User cancelled
	if indices == nil {
		return &ResolveResult[T]{WasCancelled: true}, nil
	}

	// Map indices back to items
	selected := make([]T, len(indices))
	for i, idx := range indices {
		selected[i] = items[idx]
	}

	return &ResolveResult[T]{Items: selected}, nil
}
