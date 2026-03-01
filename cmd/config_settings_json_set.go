package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var configSettingsJsonSetCmd = &cobra.Command{
	Use:   setCmdStr,
	Short: "Update the AgenC-specific settings.json content",
	Long: `Update the AgenC-specific settings.json content. Reads new content from stdin.

Content must be valid JSON. Requires --content-hash from a previous 'get' to
prevent overwriting concurrent changes.

Example:
  agenc config settings-json get                                         # note the Content-Hash
  echo '{"permissions":{"allow":["Bash(npm:*)"]}}' | agenc config settings-json set --content-hash=abc123`,
	RunE: runConfigSettingsJsonSet,
}

func init() {
	configSettingsJsonCmd.AddCommand(configSettingsJsonSetCmd)
	configSettingsJsonSetCmd.Flags().String(contentHashFlagName, "", "content hash from the last get (required)")
	_ = configSettingsJsonSetCmd.MarkFlagRequired(contentHashFlagName)
}

func runConfigSettingsJsonSet(cmd *cobra.Command, args []string) error {
	contentHash, err := cmd.Flags().GetString(contentHashFlagName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read --%s flag", contentHashFlagName)
	}

	content, err := io.ReadAll(os.Stdin)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read content from stdin")
	}

	// Validate JSON client-side for fast feedback
	if !json.Valid(content) {
		return stacktrace.NewError("content is not valid JSON")
	}

	client, err := serverClient()
	if err != nil {
		return err
	}

	resp, err := client.UpdateSettingsJson(string(content), contentHash)
	if err != nil {
		if strings.Contains(err.Error(), "modified since last read") {
			return fmt.Errorf("settings.json has been modified since last read\n\nTo resolve:\n  1. agenc config %s %s    (fetch current content and hash)\n  2. Re-apply your changes to the new content\n  3. agenc config %s %s --content-hash=<new-hash>",
				settingsJsonCmdStr, getCmdStr, settingsJsonCmdStr, setCmdStr)
		}
		return stacktrace.Propagate(err, "failed to update settings.json")
	}

	fmt.Printf("Updated settings.json (content hash: %s)\n", resp.ContentHash)
	return nil
}
