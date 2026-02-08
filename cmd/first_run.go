package cmd

import (
	"fmt"
	"os"

	"github.com/mattn/go-isatty"
	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
)

// handleFirstRun checks whether this is the first time agenc is running
// (i.e. the agenc directory does not exist yet). If stdin is a TTY, it
// prints a welcome message. Config repo cloning and other onboarding
// steps are handled by ensureConfigured(), which calls this early on.
func handleFirstRun(agencDirpath string) error {
	isFirst, err := config.IsFirstRun(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to check first-run status")
	}
	if !isFirst {
		return nil
	}

	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return nil
	}

	fmt.Println("Welcome to agenc! Setting up for the first time.")
	fmt.Println()
	return nil
}
