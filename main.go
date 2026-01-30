package main

import (
	"os"

	"github.com/odyssey/agenc/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
