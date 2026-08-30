package cmd

import (
	"fmt"
	"sort"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/server"
	"github.com/odyssey/agenc/internal/tableprinter"
)

var missionPeersCmd = &cobra.Command{
	Use:   peersCmdStr,
	Short: "List missions reachable by Claude Code's SendMessage tool",
	Long: `List missions reachable by Claude Code's SendMessage tool.

Claude Code's ListAgents tool prints one row per live session, addressed by a
peer name like 'agent-da' that carries no mission, repo, or task identity. This
command prints the same peer names alongside the mission each one belongs to,
so a row in ListAgents can be matched to the work it is doing.

The PEER and PANE columns are exactly the strings ListAgents prints. Match on
PEER; if two live sessions share a peer name, match on PANE instead. Then
address the peer with SendMessage, appending the '[ref]' that ListAgents shows
for that row.

A peer in ListAgents with no row here is not an active AgenC mission — another
Claude session on this machine, or a session on another machine.`,
	Args: cobra.NoArgs,
	RunE: runMissionPeers,
}

func init() {
	missionCmd.AddCommand(missionPeersCmd)
}

func runMissionPeers(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	const shouldIncludeArchivedMissionsWhenListingPeers = false
	missions, err := client.ListMissions(server.ListMissionsRequest{
		IncludeArchived: shouldIncludeArchivedMissionsWhenListingPeers,
	})
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	peerMissions := []*database.Mission{}
	for _, m := range missions {
		if m.PeerName == "" {
			continue
		}
		peerMissions = append(peerMissions, m)
	}

	if len(peerMissions) == 0 {
		fmt.Println("No missions are running a Claude session that can be messaged.")
		return nil
	}

	sort.Slice(peerMissions, func(i, j int) bool {
		return peerMissions[i].PeerName < peerMissions[j].PeerName
	})

	// Deliberately uncolorized: this table exists to be read and joined by
	// agents, and ANSI codes would land in the middle of the values they parse.
	tbl := tableprinter.NewTable("PEER", "PANE", "MISSION", "STATUS", "REPO", "TASK")
	for _, m := range peerMissions {
		tbl.AddRow(
			m.PeerName,
			m.PeerTmuxTarget,
			m.ShortID,
			string(getMissionStatus(m.ID, m.Status, m.ClaudeState)),
			formatPlainRepoName(m),
			truncatePrompt(resolveSessionName(m), peersTaskMaxLen),
		)
	}
	tbl.Print()

	return nil
}

// peersTaskMaxLen keeps the TASK column narrow enough that the join columns
// stay visible on a normal terminal.
const peersTaskMaxLen = 60

// formatPlainRepoName returns a mission's repo with no emoji, title lookup, or
// ANSI coloring, so the value survives being parsed out of the table.
func formatPlainRepoName(m *database.Mission) string {
	if m.IsAdjutant {
		return "adjutant"
	}
	repoName := plainGitRepoName(m.GitRepo)
	if repoName == "" {
		return "--"
	}
	return repoName
}
