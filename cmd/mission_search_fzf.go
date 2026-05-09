package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/server"
	"github.com/odyssey/agenc/internal/tableprinter"
)

// missionSearchFzfCmd is a hidden command used by fzf's change:reload binding.
// It outputs search results formatted for fzf consumption with tab-separated
// index columns. When query is empty, outputs recent missions sorted by recency.
var missionSearchFzfCmd = &cobra.Command{
	Use:    "search-fzf [query...]",
	Short:  "Search missions (fzf helper)",
	Hidden: true,
	Args:   cobra.ArbitraryArgs,
	RunE:   runMissionSearchFzf,
}

func init() {
	missionCmd.AddCommand(missionSearchFzfCmd)
}

// substringMergeCap limits the number of substring-match rows merged in
// addition to FTS results, mirroring the FTS cap so the picker stays
// scannable even when many missions match.
const substringMergeCap = 30

// searchFzfRow is one row of the mission search-fzf output: the leading
// short-ID is consumed by fzf as the result index; cols are the visible
// table columns (LAST PROMPT, ID, SESSION, REPO, MATCH).
type searchFzfRow struct {
	shortID string
	cols    []string
}

func runMissionSearchFzf(cmd *cobra.Command, args []string) error {
	query := strings.Join(args, " ")

	if query == "" {
		return printRecentMissionsForFzf()
	}

	client, err := serverClient()
	if err != nil {
		return err
	}

	cfg, _ := readConfig()

	var rows []searchFzfRow
	seenMissionIDs := make(map[string]bool)

	// If the query looks like a mission ID, try direct resolution first.
	// Mission IDs aren't in the FTS content index, so without this the
	// picker would return no results when searching by ID.
	if looksLikeMissionID(query) {
		if m, resolveErr := client.GetMission(query); resolveErr == nil {
			session := resolveSessionName(m)
			repo := formatRepoDisplay(m.GitRepo, m.IsAdjutant, cfg)
			lastPrompt := formatLastPrompt(m.LastUserPromptAt, m.CreatedAt)
			rows = append(rows, searchFzfRow{
				shortID: m.ShortID,
				cols:    []string{lastPrompt, m.ShortID, session, repo, ""},
			})
			seenMissionIDs[m.ID] = true
		}
	}

	// Full-text search on session content
	results, err := client.SearchMissions(query, 30)
	if err != nil {
		return err
	}

	for _, r := range results {
		if seenMissionIDs[r.MissionID] {
			continue
		}
		seenMissionIDs[r.MissionID] = true

		shortID := r.ShortID
		if shortID == "" && len(r.MissionID) >= 8 {
			shortID = r.MissionID[:8]
		}

		session := r.ResolvedSessionTitle
		if session == "" {
			session = truncatePrompt(r.Prompt, 30)
		} else {
			session = truncatePrompt(session, 30)
		}

		repo := formatRepoDisplay(r.GitRepo, false, cfg)

		snippet := strings.ReplaceAll(r.Snippet, "\n", " ")
		snippet = database.ColorizeSnippet(snippet)

		lastPrompt := formatLastPromptFromStrings(r.LastUserPromptAt, r.CreatedAt)

		rows = append(rows, searchFzfRow{
			shortID: shortID,
			cols:    []string{lastPrompt, shortID, session, repo, snippet},
		})
	}

	// Merge: case-insensitive substring matches over ListMissions for
	// missions not seen via FTS. This recovers unprompted missions and
	// any whose ResolvedSessionTitle/repo aren't in the FTS index.
	rows = appendSubstringMatches(client, cfg, rows, query, seenMissionIDs)

	if len(rows) == 0 {
		return nil
	}

	// Render through tableprinter for alignment
	var buf strings.Builder
	tbl := tableprinter.NewTable("LAST PROMPT", "ID", "SESSION", "REPO", "MATCH").WithWriter(&buf)
	for _, r := range rows {
		tbl.AddRow(toAnySlice(r.cols)...)
	}
	tbl.Print()

	// Output: header line (skip) + data lines prefixed with mission short ID
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	// First line is the header — fzf will use it if --header-lines is set,
	// but since the caller already has headers, skip it
	for i, line := range lines {
		if i == 0 {
			continue // skip header
		}
		idx := i - 1
		if idx < len(rows) {
			fmt.Printf("%s\t%s\n", rows[idx].shortID, line)
		}
	}

	return nil
}

// appendSubstringMatches walks ListMissions and appends rows for missions
// not already seen via FTS that match the query as a case-insensitive
// substring of ResolvedSessionTitle, Prompt, or GitRepo. Capped at
// substringMergeCap. ListMissions errors are tolerated silently — FTS
// results alone are returned.
func appendSubstringMatches(
	client *server.Client,
	cfg *config.AgencConfig,
	rows []searchFzfRow,
	query string,
	seenMissionIDs map[string]bool,
) []searchFzfRow {
	allMissions, listErr := client.ListMissions(server.ListMissionsRequest{IncludeArchived: true})
	if listErr != nil {
		return rows
	}
	lowerQuery := strings.ToLower(query)
	appended := 0
	for _, m := range allMissions {
		if appended >= substringMergeCap {
			break
		}
		if seenMissionIDs[m.ID] {
			continue
		}
		if !matchMissionSubstring(m, lowerQuery) {
			continue
		}
		seenMissionIDs[m.ID] = true
		appended++

		session := truncatePrompt(resolveSessionName(m), 30)
		repo := formatRepoDisplay(m.GitRepo, m.IsAdjutant, cfg)
		lastPrompt := formatLastPrompt(m.LastUserPromptAt, m.CreatedAt)
		rows = append(rows, searchFzfRow{
			shortID: m.ShortID,
			cols:    []string{lastPrompt, m.ShortID, session, repo, ""},
		})
	}
	return rows
}

// matchMissionSubstring returns true if the lowercased query is a substring
// of any of the mission's lowercased ResolvedSessionTitle (via resolveSessionName),
// initial Prompt, or GitRepo. Empty fields cannot match a non-empty query.
func matchMissionSubstring(m *database.Mission, lowerQuery string) bool {
	title := resolveSessionName(m)
	if strings.Contains(strings.ToLower(title), lowerQuery) {
		return true
	}
	if strings.Contains(strings.ToLower(m.Prompt), lowerQuery) {
		return true
	}
	if strings.Contains(strings.ToLower(m.GitRepo), lowerQuery) {
		return true
	}
	return false
}

// formatLastPromptFromStrings parses the RFC3339 timestamps that
// SearchMissionsResponse carries (over JSON) and delegates to formatLastPrompt.
// Returns "--" when the prompt timestamp is nil or unparseable.
func formatLastPromptFromStrings(lastUserPromptAt *string, createdAtRFC3339 string) string {
	var promptPtr *time.Time
	if lastUserPromptAt != nil {
		if t, err := time.Parse(time.RFC3339, *lastUserPromptAt); err == nil {
			promptPtr = &t
		}
	}
	createdAt, _ := time.Parse(time.RFC3339, createdAtRFC3339)
	return formatLastPrompt(promptPtr, createdAt)
}

func printRecentMissionsForFzf() error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	missions, err := client.ListMissions(server.ListMissionsRequest{IncludeArchived: true})
	if err != nil {
		return err
	}

	sortMissionsForPicker(missions)
	entries := buildMissionPickerEntries(missions, 30)

	var buf strings.Builder
	tbl := tableprinter.NewTable("LAST PROMPT", "ID", "SESSION", "REPO", "MATCH").WithWriter(&buf)
	for _, e := range entries {
		tbl.AddRow(e.LastPrompt, e.ShortID, e.Session, e.Repo, "")
	}
	tbl.Print()

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	for i, line := range lines {
		if i == 0 {
			continue
		}
		idx := i - 1
		if idx < len(entries) {
			fmt.Printf("%s\t%s\n", entries[idx].ShortID, line)
		}
	}

	return nil
}
