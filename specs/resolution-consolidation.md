Resolution Pattern Consolidation
=================================

Overview
--------
Multiple CLI commands implement the same 4-step resolution pattern for selecting entities (repos, missions, templates). This spec proposes consolidating them into a generic resolver.

The Pattern
-----------

Every resolution follows this flow:

```
User Input (args)
      │
      ▼
┌─────────────────┐
│ TryCanonical()  │──── match ────▶ Done (return item)
└─────────────────┘
      │ no match
      ▼
┌─────────────────┐
│ GetItems()      │
│ FilterByTerms() │
└─────────────────┘
      │
      ▼
┌─────────────────┐
│ Count matches   │
└─────────────────┘
      │
      ├── 1 match ────▶ Auto-select, Done
      │
      ▼
┌─────────────────┐
│ FzfPicker()     │──── selection ──▶ Done
└─────────────────┘
```

Current Implementations
-----------------------

| Command | Entity | File | Canonical Resolution | Fallback Filter |
|---------|--------|------|---------------------|-----------------|
| `mission new` | repo/template | mission_new.go | `looksLikeRepoReference()` + `ParseRepoReference()` | `matchRepoLibraryEntries()` |
| `mission resume` | mission | mission_resume.go | `db.ResolveMissionID()` | `matchMissionEntries()` |
| `repo rm` | repo | repo_rm.go | `ParseRepoReference()` | `matchReposGlob()` |
| `repo update` | repo | repo_update.go | `ParseRepoReference()` | `matchReposGlob()` |
| `repo edit` | repo | repo_edit.go | via `ResolveRepoInputs()` | `matchReposGlob()` |
| `template rm` | template | template_rm.go | `ParseRepoReference()` | `matchTemplateEntries()` |

All use `matchesSequentialSubstrings()` as the core matching algorithm.

What Varies
-----------

### 1. Canonical Resolution (optional)

Some commands try to parse input as a specific format before falling back to search:

| Entity | Canonical Formats |
|--------|-------------------|
| Repo | URL, `owner/repo`, `github.com/owner/repo`, local path |
| Mission | Full UUID, short UUID prefix |
| Template | Same as repo (templates are repos) |

Some commands skip canonical resolution entirely and go straight to search.

### 2. Item Source

| Entity | Source |
|--------|--------|
| Repo | `findReposOnDisk()` — scans `$AGENC_DIRPATH/repos/` |
| Mission | `db.ListMissions()` — database query |
| Template | `cfg.AgentTemplates` — config map |
| Library | Union of repos + templates |

### 3. Text Extraction

Each entity type has a function to convert items to searchable text:

| Entity | Extractor | Example Output |
|--------|-----------|----------------|
| Repo | identity | `github.com/owner/repo` |
| Mission | `formatMissionMatchLine()` | `abc123 my-agent running github.com/owner/repo` |
| Template | `formatTemplateFzfLine()` | `my-agent (github.com/owner/repo)` |
| Library entry | `formatLibraryFzfLine()` | `🤖 my-agent (github.com/owner/repo)` |

### 4. FZF Configuration

| Aspect | Variations |
|--------|------------|
| Headers | `["REPO"]`, `["NICKNAME", "REPO"]`, `["LAST ACTIVE", "ID", "AGENT", ...]` |
| Prompt | `"Select repo: "`, `"Select mission: "`, etc. |
| Multi-select | true for batch operations (rm, update), false for single-select (new) |
| Sentinel rows | Mission picker has "blank mission" option |
| Row formatting | Type-specific display with colors, icons |

### 5. Pre-filtering

Some commands filter the item list before resolution:

- `repo rm`: Excludes repos that are agent templates
- `mission resume`: Excludes archived missions (configurable)

### 6. Return Type

- Repos: `[]string` (canonical names)
- Missions: `[]*database.Mission` or `[]missionPickerEntry`
- Templates: `[]string` (repo keys)

Proposed Design
---------------

### Generic Resolver

```go
// Resolver defines how to resolve user input to a selection of items.
type Resolver[T any] struct {
    // TryCanonical attempts to resolve input as a canonical reference.
    // Returns (item, true, nil) if resolved, (zero, false, nil) if not a reference,
    // or (zero, false, err) on error.
    // Optional: if nil, skips canonical resolution.
    TryCanonical func(input string) (T, bool, error)

    // GetItems returns all items available for selection.
    GetItems func() ([]T, error)

    // ExtractText returns the searchable text for an item.
    // Used for sequential substring matching.
    ExtractText func(T) string

    // FormatRow returns the display columns for an item in fzf.
    FormatRow func(T) []string

    // FzfPrompt is the prompt shown in fzf.
    FzfPrompt string

    // FzfHeaders are the column headers shown in fzf.
    FzfHeaders []string

    // MultiSelect allows selecting multiple items in fzf.
    MultiSelect bool
}

// Resolve resolves user input to a selection of items.
// The input is space-separated terms (e.g., "term1 term2").
func Resolve[T any](input string, r Resolver[T]) ([]T, error) {
    terms := strings.Fields(input)

    // 1. Try canonical resolution (if configured)
    if r.TryCanonical != nil && len(terms) == 1 {
        if item, ok, err := r.TryCanonical(terms[0]); err != nil {
            return nil, err
        } else if ok {
            return []T{item}, nil
        }
    }

    // 2. Get all items
    items, err := r.GetItems()
    if err != nil {
        return nil, err
    }

    // 3. Filter by sequential substring matching
    var matches []T
    if len(terms) > 0 {
        for _, item := range items {
            if matchesSequentialSubstrings(r.ExtractText(item), terms) {
                matches = append(matches, item)
            }
        }
    } else {
        matches = items
    }

    // 4. Auto-select if exactly one match
    if len(matches) == 1 {
        return matches, nil
    }

    // 5. Launch fzf picker
    return r.pickWithFzf(matches, terms)
}
```

### Usage Example

```go
// Resolving repos for `repo rm`
repos, err := Resolve(strings.Join(args, " "), Resolver[string]{
    TryCanonical: func(input string) (string, bool, error) {
        ref, err := mission.ParseRepoReference(input)
        if err != nil {
            return "", false, nil // not a reference, fall through to search
        }
        canonical := ref.Canonical()
        if slices.Contains(allRepos, canonical) {
            return canonical, true, nil
        }
        return "", false, nil
    },
    GetItems: func() ([]string, error) {
        return filterOutTemplates(findReposOnDisk(agencDirpath))
    },
    ExtractText: func(repo string) string { return repo },
    FormatRow:   func(repo string) []string { return []string{displayGitRepo(repo)} },
    FzfPrompt:   "Select repos to remove: ",
    FzfHeaders:  []string{"REPO"},
    MultiSelect: true,
})
```

Design Decisions
----------------

### 1. Input semantics

`"term1 term2"` is always treated as search terms for sequential substring matching, not as multiple references. Callers that need to resolve multiple items should either:
- Let the user multi-select in fzf
- Call `Resolve()` multiple times in a loop

### 2. Sentinel rows

Handled externally. Commands that need sentinel rows (e.g., "blank mission") wrap or compose around the resolver rather than adding complexity to the core.

### 3. Pre-filtering

Baked into `GetItems()`. The caller is responsible for returning the appropriate subset of items.

### 4. Auto-select confirmation

The resolver does not print anything. Callers can determine auto-selection from the return value and context (e.g., if input was provided and exactly one item returned).

### 5. Location

`cmd/resolver.go` — lives alongside other cmd utilities.

### 6. Filter functions

Delete `matchMissionEntries()`, `matchRepoLibraryEntries()`, `matchTemplateEntries()`, and `matchReposGlob()` once all callers are migrated. The filtering logic is subsumed into `Resolve()`.

Implementation Plan
-------------------

### Phase 1: Core Resolver

1. Create `cmd/resolver.go` with generic `Resolve[T]` function
2. Implement for simplest case: `repo update` (single-select, no sentinel)
3. Verify behavior matches current implementation

### Phase 2: Migrate Commands

Migrate in order of complexity:

1. `repo update` — simplest case
2. `repo rm` — adds pre-filtering (templates excluded)
3. `repo edit` — uses `ResolveRepoInputs` (multi-input)
4. `template rm` — different entity type, same pattern
5. `mission resume` — database-backed items
6. `mission new` — sentinel row, library union

### Phase 3: Cleanup

1. Delete redundant filter functions if no longer needed
2. Delete old resolution code from individual commands
3. Update tests

Verification
------------

1. `go build ./...` — compiles
2. `go test ./...` — tests pass
3. Manual testing for each migrated command:
   - Canonical input resolves correctly
   - Search terms filter correctly
   - Single match auto-selects
   - Multiple matches open fzf with correct pre-filter
   - Fzf selection works
   - Ctrl-C cancellation works
