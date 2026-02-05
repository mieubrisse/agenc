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

	if err := os.MkdirAll(outputDirpath, 0755); err != nil {
		log.Fatalf("failed to create output directory: %v", err)
	}

	rootCmd := cmd.GetRootCmd()
	if err := doc.GenMarkdownTree(rootCmd, outputDirpath); err != nil {
		log.Fatalf("failed to generate docs: %v", err)
	}

	log.Printf("Documentation generated in %s", outputDirpath)
}
