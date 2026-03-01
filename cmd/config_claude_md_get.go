package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var configClaudeMdGetCmd = &cobra.Command{
	Use:   getCmdStr,
	Short: "Print the AgenC-specific CLAUDE.md content",
	RunE:  runConfigClaudeMdGet,
}

func init() {
	configClaudeMdCmd.AddCommand(configClaudeMdGetCmd)
}

func runConfigClaudeMdGet(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	resp, err := client.GetClaudeMd()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get CLAUDE.md")
	}

	fmt.Printf("Content-Hash: %s\n\n--- Content ---\n%s", resp.ContentHash, resp.Content)
	return nil
}
