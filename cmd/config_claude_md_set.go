package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var configClaudeMdSetCmd = &cobra.Command{
	Use:   setCmdStr,
	Short: "Update the AgenC-specific CLAUDE.md content",
	Long: `Update the AgenC-specific CLAUDE.md content. Reads new content from stdin.

Requires --content-hash from a previous 'get' to prevent overwriting concurrent
changes. If the file was modified since your last read, the update is rejected
and you must re-read before retrying.

Example:
  agenc config claude-md get                                    # note the Content-Hash
  echo "New instructions" | agenc config claude-md set --content-hash=abc123`,
	RunE: runConfigClaudeMdSet,
}

func init() {
	configClaudeMdCmd.AddCommand(configClaudeMdSetCmd)
	configClaudeMdSetCmd.Flags().String(contentHashFlagName, "", "content hash from the last get (required)")
	_ = configClaudeMdSetCmd.MarkFlagRequired(contentHashFlagName)
}

func runConfigClaudeMdSet(cmd *cobra.Command, args []string) error {
	contentHash, err := cmd.Flags().GetString(contentHashFlagName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read --%s flag", contentHashFlagName)
	}

	content, err := io.ReadAll(os.Stdin)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read content from stdin")
	}

	client, err := serverClient()
	if err != nil {
		return err
	}

	resp, err := client.UpdateClaudeMd(string(content), contentHash)
	if err != nil {
		if strings.Contains(err.Error(), "modified since last read") {
			return fmt.Errorf("CLAUDE.md has been modified since last read\n\nTo resolve:\n  1. agenc config %s %s    (fetch current content and hash)\n  2. Re-apply your changes to the new content\n  3. agenc config %s %s --content-hash=<new-hash>",
				claudeMdCmdStr, getCmdStr, claudeMdCmdStr, setCmdStr)
		}
		return stacktrace.Propagate(err, "failed to update CLAUDE.md")
	}

	fmt.Printf("Updated CLAUDE.md (content hash: %s)\n", resp.ContentHash)
	return nil
}
