package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var tmuxUninjectCmd = &cobra.Command{
	Use:   uninjectCmdStr,
	Short: "Remove AgenC tmux keybindings",
	Long: `Remove the AgenC-managed keybindings source directive from your tmux.conf.
This removes the sentinel-wrapped block that sources the AgenC keybindings file.
The keybindings file itself remains in ~/.agenc/tmux/keybindings.conf but will
no longer be sourced by tmux.

If a tmux server is running when you uninject, you may need to restart it or
manually unbind the keys for the changes to take full effect.`,
	Args: cobra.NoArgs,
	RunE: runTmuxUninject,
}

func init() {
	tmuxCmd.AddCommand(tmuxUninjectCmd)
}

func runTmuxUninject(cmd *cobra.Command, args []string) error {
	tmuxConfFilepath, exists, err := findTmuxConfFilepath()
	if err != nil {
		return err
	}

	if !exists {
		fmt.Printf("No tmux.conf found at %s\n", tmuxConfFilepath)
		fmt.Println("Nothing to uninject")
		return nil
	}

	content, err := os.ReadFile(tmuxConfFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read '%s'", tmuxConfFilepath)
	}
	fileContent := string(content)

	beginIdx := strings.Index(fileContent, sentinelBegin)
	endIdx := strings.Index(fileContent, sentinelEnd)

	if beginIdx < 0 || endIdx < 0 {
		fmt.Printf("No AgenC keybindings found in %s\n", tmuxConfFilepath)
		fmt.Println("Nothing to uninject")
		return nil
	}

	// Remove the entire sentinel block, including the sentinels themselves
	beforeBlock := fileContent[:beginIdx]
	afterBlock := fileContent[endIdx+len(sentinelEnd):]

	// Clean up: remove trailing newline from beforeBlock and leading newline from afterBlock
	// to avoid leaving blank lines where the block was
	beforeBlock = strings.TrimRight(beforeBlock, "\n")
	afterBlock = strings.TrimLeft(afterBlock, "\n")

	newContent := beforeBlock
	if len(beforeBlock) > 0 && len(afterBlock) > 0 {
		newContent += "\n"
	}
	if len(afterBlock) > 0 {
		newContent += afterBlock
	}
	if len(newContent) > 0 && !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}

	if err := os.WriteFile(tmuxConfFilepath, []byte(newContent), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to update '%s'", tmuxConfFilepath)
	}

	fmt.Printf("Removed AgenC keybindings from %s\n", tmuxConfFilepath)
	fmt.Println("\nNote: If you have a running tmux server, you may need to restart it")
	fmt.Println("or manually unbind keys for changes to take full effect.")

	return nil
}
