Matching Functions Consolidation
=================================

Overview
--------
The `cmd` package contains multiple matching/filtering functions that share the same core algorithm but operate on different types. This spec analyzes the duplication and proposes a consolidation strategy.

Current State
-------------

### Core Matching Functions: Identical Implementations

Two functions implement the exact same algorithm:

**`matchesSequentialSubstrings`** (`mission_new.go:402`):
```go
func matchesSequentialSubstrings(text string, substrings []string) bool {
    lower := strings.ToLower(text)
    pos := 0
    for _, sub := range substrings {
        idx := strings.Index(lower[pos:], strings.ToLower(sub))
        if idx == -1 {
            return false
        }
        pos += idx + len(sub)
    }
    return true
}
```

**`matchesGlobTerms`** (`repo_resolution.go:400`):
```go
func matchesGlobTerms(repo string, terms []string) bool {
    lower := strings.ToLower(repo)
    pos := 0
    for _, term := range terms {
        termLower := strings.ToLower(term)
        idx := strings.Index(lower[pos:], termLower)
        if idx == -1 {
            return false
        }
        pos += idx + len(termLower)
    }
    return true
}
```

**These are byte-for-byte identical algorithms** (modulo variable names). The only difference is `matchesGlobTerms` lowercases each term inside the loop while `matchesSequentialSubstrings` does it inline — same result.

**Naming issue:** `matchesGlobTerms` is a misnomer. It does not perform glob matching (no `*`, `?`, `[...]` wildcards). It performs sequential substring matching, identical to `matchesSequentialSubstrings`. The name likely came from the mental model "search terms work like `*term1*term2*`" but the implementation is just substring search.

### Filter Functions: 4 Implementations of Same Pattern

| Function | Location | Input | Extractor | Output |
|----------|----------|-------|-----------|--------|
| `matchMissionEntries` | `mission_helpers.go:125` | `[]missionPickerEntry` | `formatMissionMatchLine` | `[]missionPickerEntry` |
| `matchRepoLibraryEntries` | `mission_new.go:389` | `[]repoLibraryEntry` | `formatLibraryFzfLine` | `[]repoLibraryEntry` |
| `matchTemplateEntries` | `mission_new.go:589` | `map[string]Props` | `formatTemplateFzfLine` | `[]string` (keys only) |
| `matchReposGlob` | `repo_resolution.go:385` | `[]string` | identity | `[]string` |

All follow this pattern:
```go
var matches []OutputType
for _, item := range items {
    text := extractSearchableText(item)
    if matchesSequentialSubstrings(text, terms) {
        matches = append(matches, transformedItem)
    }
}
return matches
```

**Quirk:** `matchTemplateEntries` iterates via `sortedRepoKeys()` for deterministic ordering. The others iterate in input order.

### Usage Sites

| Function | Called From | Times Used |
|----------|-------------|------------|
| `matchMissionEntries` | `mission_resume.go` | 1 |
| `matchRepoLibraryEntries` | `mission_new.go` | 1 |
| `matchTemplateEntries` | `mission_new.go` | 1 |
| `matchReposGlob` | `repo_resolution.go` | 1 |

Each filter function is used exactly once. This suggests they were written inline for specific use cases rather than designed as reusable utilities.

Analysis
--------

### What's Actually Duplicated

1. **Critical:** Two identical implementations of the matching algorithm (`matchesSequentialSubstrings` and `matchesGlobTerms`)
2. **Minor:** Four 6-line filter functions that follow the same loop pattern

### Cost/Benefit of Consolidation

**Deleting duplicate algorithm (high value, low cost):**
- Eliminates risk of implementations diverging
- Fixes misleading `matchesGlobTerms` name
- Zero impact on call sites

**Consolidating filter functions (moderate value, moderate cost):**
- Pro: Single place to change filtering behavior
- Pro: Clearer that all filtering uses the same algorithm
- Con: Generic function is more abstract than named wrappers
- Con: `matchTemplateEntries` has map+sorting behavior that doesn't fit the pattern cleanly
- Con: Each function is only used once — wrapper names document intent at their single call site

Proposed Changes
----------------

### Phase 1: Eliminate Duplicate Algorithm (Do This)

1. Delete `matchesGlobTerms` from `repo_resolution.go`
2. Update `matchReposGlob` to call `matchesSequentialSubstrings`
3. Move `matchesSequentialSubstrings` to `cmd/matching.go` as the canonical location

**Result:** One implementation of the matching algorithm, clear ownership.

### Phase 2: Consolidate Filter Functions (Optional)

If the codebase grows more filter functions, consider:

```go
// filterByMatch returns items where extractor(item) matches all terms sequentially.
func filterByMatch[T any](items []T, terms []string, extractor func(T) string) []T {
    if len(terms) == 0 {
        return items
    }
    var matches []T
    for _, item := range items {
        if matchesSequentialSubstrings(extractor(item), terms) {
            matches = append(matches, item)
        }
    }
    return matches
}
```

**Do not do this now** because:
- 4 single-use functions don't justify the abstraction
- `matchTemplateEntries` doesn't fit cleanly (map input, sorted iteration)
- Named functions (`matchMissionEntries`) are self-documenting at call sites

**Revisit if:**
- A 5th or 6th filter function is added
- Filtering logic needs to change (fuzzy matching, ranking, etc.)
- Multiple call sites emerge for the same filter function

Implementation Plan
-------------------

### Files Changed
- `cmd/matching.go` (new) — home for `matchesSequentialSubstrings`
- `cmd/repo_resolution.go` — delete `matchesGlobTerms`, import from `matching.go`
- `cmd/mission_new.go` — delete local `matchesSequentialSubstrings`, import from `matching.go`
- `cmd/mission_helpers.go` — import from `matching.go` (already uses it via `mission_new.go` same package, but making dependency explicit)

### Migration Steps
1. Create `cmd/matching.go` with `matchesSequentialSubstrings`
2. Delete `matchesGlobTerms` from `repo_resolution.go`
3. Delete `matchesSequentialSubstrings` from `mission_new.go`
4. Update `matchReposGlob` to call `matchesSequentialSubstrings`
5. Verify: `go build ./...` and `go test ./...`

Verification
------------
1. `go build ./...` — compilation succeeds
2. `go test ./...` — all tests pass
3. Manual tests:
   - `./agenc mission new "search terms"` — repo/template filtering works
   - `./agenc mission resume "search terms"` — mission filtering works

Open Questions
--------------
1. Should `matchesSequentialSubstrings` be renamed? Alternatives:
   - `matchesAllTermsInOrder`
   - `containsSubstringsSequentially`
   - Keep current name (it's accurate)

2. Should `matchReposGlob` be renamed since it doesn't do glob matching?
   - `matchReposByTerms`
   - `filterReposBySubstrings`
   - Keep current name (changing would touch call sites for marginal benefit)
