package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var configSettingsJsonGetCmd = &cobra.Command{
	Use:   getCmdStr,
	Short: "Print the AgenC-specific settings.json content",
	RunE:  runConfigSettingsJsonGet,
}

func init() {
	configSettingsJsonCmd.AddCommand(configSettingsJsonGetCmd)
}

func runConfigSettingsJsonGet(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	resp, err := client.GetSettingsJson()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get settings.json")
	}

	fmt.Printf("Content-Hash: %s\n\n--- Content ---\n%s", resp.ContentHash, resp.Content)
	return nil
}
