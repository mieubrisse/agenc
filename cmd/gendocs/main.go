package main

import (
	"log"
	"os"

	"github.com/spf13/cobra/doc"

	"github.com/odyssey/agenc/cmd"
)

func main() {
	outputDirpath := "./docs/cli"
	if len(os.Args) > 1 {
		outputDirpath = os.Args[1]
	}

	// Remove and recreate the output directory so stale docs from deleted
	// commands don't linger.
	if err := os.RemoveAll(outputDirpath); err != nil {
		log.Fatalf("failed to clean output directory: %v", err)
	}
	if err := os.MkdirAll(outputDirpath, 0755); err != nil {
		log.Fatalf("failed to create output directory: %v", err)
	}

	rootCmd := cmd.GetRootCmd()
	rootCmd.DisableAutoGenTag = true
	if err := doc.GenMarkdownTree(rootCmd, outputDirpath); err != nil {
		log.Fatalf("failed to generate docs: %v", err)
	}

	log.Printf("Documentation generated in %s", outputDirpath)
}
