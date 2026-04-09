package cmd

import (
	"fmt"
	"os"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// startSpinner displays an animated spinner with the given message on stderr.
// Returns a stop function that clears the spinner line. The caller must call
// stop() when the operation completes.
func startSpinner(message string) (stop func()) {
	done := make(chan struct{})

	go func() {
		i := 0
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()

		// Show initial frame immediately
		fmt.Fprintf(os.Stderr, "\r%s %s", spinnerFrames[0], message)
		i++

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				fmt.Fprintf(os.Stderr, "\r%s %s", spinnerFrames[i%len(spinnerFrames)], message)
				i++
			}
		}
	}()

	return func() {
		close(done)
		// Clear the spinner line
		fmt.Fprintf(os.Stderr, "\r\033[K")
	}
}
