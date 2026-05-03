package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
)

var searchJSONFlag bool
var searchLimitFlag int

var missionSearchCmd = &cobra.Command{
	Use:   searchCmdStr + " <query>",
	Short: "Search missions by conversation content",
	Long: `Search missions using full-text search over conversation transcripts.

Searches user messages, assistant responses, session titles, and mission prompts.
Results are ranked by relevance using BM25.

The search index is populated automatically by the server. New content becomes
searchable within ~30 seconds of being written.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runMissionSearch,
}

func init() {
	missionCmd.AddCommand(missionSearchCmd)
	missionSearchCmd.Flags().BoolVar(&searchJSONFlag, "json", false, "output results as JSON")
	missionSearchCmd.Flags().IntVar(&searchLimitFlag, "limit", 20, "maximum number of results")
}

func runMissionSearch(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	query := strings.Join(args, " ")
	results, err := client.SearchMissions(query, searchLimitFlag)
	if err != nil {
		return stacktrace.Propagate(err, "search failed")
	}

	if searchJSONFlag {
		// Strip binary markers from snippets for JSON output
		for i := range results {
			results[i].Snippet = database.StripSnippetMarkers(results[i].Snippet)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}

	if len(results) == 0 {
		fmt.Println("No results.")
		return nil
	}

	cfg, _ := readConfig()

	for _, r := range results {
		shortID := r.ShortID
		if shortID == "" && len(r.MissionID) >= 8 {
			shortID = r.MissionID[:8]
		}

		session := r.ResolvedSessionTitle
		if session == "" {
			session = truncatePrompt(r.Prompt, 60)
		}

		repo := formatRepoDisplay(r.GitRepo, false, cfg)

		fmt.Printf("%s  %s  %s\n", shortID, session, repo)
		if r.Snippet != "" {
			fmt.Printf("  %s\n\n", database.ColorizeSnippet(r.Snippet))
		}
	}

	return nil
}
