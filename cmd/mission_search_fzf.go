package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

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

	// Build table rows and track IDs for the index column
	type row struct {
		shortID string
		cols    []string
	}
	var rows []row
	seenMissionIDs := make(map[string]bool)

	// If the query looks like a mission ID, try direct resolution first.
	// Mission IDs aren't in the FTS content index, so without this the
	// picker would return no results when searching by ID.
	if looksLikeMissionID(query) {
		if m, resolveErr := client.GetMission(query); resolveErr == nil {
			session := resolveSessionName(m)
			repo := formatRepoDisplay(m.GitRepo, m.IsAdjutant, cfg)
			rows = append(rows, row{
				shortID: m.ShortID,
				cols:    []string{m.ShortID, session, repo, ""},
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
			session = truncatePrompt(r.Prompt, 50)
		}

		repo := formatRepoDisplay(r.GitRepo, false, cfg)

		snippet := strings.ReplaceAll(r.Snippet, "\n", " ")
		if len(snippet) > 60 {
			snippet = snippet[:60] + "…"
		}

		rows = append(rows, row{
			shortID: shortID,
			cols:    []string{shortID, session, repo, snippet},
		})
	}

	if len(rows) == 0 {
		return nil
	}

	// Render through tableprinter for alignment
	var buf strings.Builder
	tbl := tableprinter.NewTable("ID", "SESSION", "REPO", "MATCH").WithWriter(&buf)
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
	entries := buildMissionPickerEntries(missions, 50)

	var buf strings.Builder
	tbl := tableprinter.NewTable("ID", "SESSION", "REPO", "MATCH").WithWriter(&buf)
	for _, e := range entries {
		tbl.AddRow(e.ShortID, e.Session, e.Repo, "")
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
