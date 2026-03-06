// cmd/genprime generates the AgenC CLI quick reference content by introspecting
// the Cobra command tree. The output is embedded into the binary via go:embed
// at compile time and printed by `agenc prime`. Run via: go run ./cmd/genprime
package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/cmd"
)

const outputFilepath = "./internal/claudeconfig/prime_content.md"

// commandGroup represents a top-level command and its subcommands for
// template rendering.
type commandGroup struct {
	Name        string
	Description string
	Commands    []commandEntry
}

// commandEntry represents a single command in the prime reference.
type commandEntry struct {
	Usage       string
	Description string
}

func main() {
	rootCmd := cmd.GetRootCmd()

	groups := buildCommandGroups(rootCmd)

	content, err := renderPrimeContent(groups)
	if err != nil {
		log.Fatalf("failed to render prime content: %v", err)
	}

	if err := os.WriteFile(outputFilepath, []byte(content), 0644); err != nil {
		log.Fatalf("failed to write %s: %v", outputFilepath, err)
	}

	log.Printf("Generated %s", outputFilepath)
}

// topLevelOrder defines the display order for top-level command groups.
// Commands not in this list are appended at the end under "Other Commands".
var topLevelOrder = []string{
	"mission",
	"repo",
	"config",
	"cron",
	"server",
}

// collectLeafCommands recursively walks a command tree and collects all leaf
// commands (those with a Run/RunE handler or no subcommands). Intermediate
// group commands that only exist to hold subcommands are skipped.
func collectLeafCommands(cmd *cobra.Command, prefix string, entries *[]commandEntry) {
	for _, sub := range cmd.Commands() {
		if sub.Hidden || !sub.IsAvailableCommand() {
			continue
		}

		fullUsage := fmt.Sprintf("%s %s", prefix, sub.Use)
		children := sub.Commands()

		// If this command has visible children, recurse into them
		hasVisibleChildren := false
		for _, child := range children {
			if !child.Hidden && child.IsAvailableCommand() {
				hasVisibleChildren = true
				break
			}
		}

		if hasVisibleChildren {
			collectLeafCommands(sub, fmt.Sprintf("%s %s", prefix, sub.Name()), entries)
		} else {
			*entries = append(*entries, commandEntry{
				Usage:       fullUsage,
				Description: sub.Short,
			})
		}
	}
}

// buildCommandGroups walks the Cobra command tree and organizes commands
// into groups for template rendering. Recursively descends into nested
// subcommand groups to capture all leaf commands.
func buildCommandGroups(rootCmd *cobra.Command) []commandGroup {
	groupMap := make(map[string]*commandGroup)

	for _, child := range rootCmd.Commands() {
		if child.Hidden || !child.IsAvailableCommand() {
			continue
		}

		name := child.Name()

		subcommands := child.Commands()
		if len(subcommands) > 0 {
			group := &commandGroup{
				Name:        name,
				Description: child.Short,
			}
			collectLeafCommands(child, fmt.Sprintf("agenc %s", name), &group.Commands)
			groupMap[name] = group
		}
	}

	// Build ordered result
	var groups []commandGroup
	seen := make(map[string]bool)

	for _, name := range topLevelOrder {
		if group, ok := groupMap[name]; ok {
			groups = append(groups, *group)
			seen[name] = true
		}
	}

	// Collect ungrouped top-level commands (no subcommands) into "Other Commands"
	var otherCommands []commandEntry
	for _, child := range rootCmd.Commands() {
		if child.Hidden || !child.IsAvailableCommand() {
			continue
		}
		name := child.Name()
		if seen[name] {
			continue
		}
		if _, hasGroup := groupMap[name]; hasGroup {
			// Grouped command not in topLevelOrder — append its group
			groups = append(groups, *groupMap[name])
			seen[name] = true
			continue
		}
		otherCommands = append(otherCommands, commandEntry{
			Usage:       fmt.Sprintf("agenc %s", child.Use),
			Description: child.Short,
		})
	}

	if len(otherCommands) > 0 {
		groups = append(groups, commandGroup{
			Name:        "other",
			Description: "Other commands",
			Commands:    otherCommands,
		})
	}

	return groups
}

// padRight pads a string with spaces to the given width.
func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// renderPrimeContent renders the prime content from the command groups.
func renderPrimeContent(groups []commandGroup) (string, error) {
	funcMap := template.FuncMap{
		"padRight":  padRight,
		"sectionH2": func(s string) string { return s + "\n" + strings.Repeat("-", len(s)) },
		"maxUsageLen": func(cmds []commandEntry) int {
			maxLen := 0
			for _, c := range cmds {
				if len(c.Usage) > maxLen {
					maxLen = len(c.Usage)
				}
			}
			return maxLen
		},
	}

	tmpl, err := template.New("prime").Funcs(funcMap).Parse(primeTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var sb strings.Builder
	if err := tmpl.Execute(&sb, groups); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return sb.String(), nil
}

// primeTemplate is the Go text/template for the prime content.
// The static preamble provides context for agents; the dynamic sections
// are populated from the Cobra command tree.
var primeTemplate = `AgenC CLI Quick Reference
=========================

**Never use interactive commands** that open ` + "`$EDITOR`" + ` or require terminal input without arguments — they will hang. Use non-interactive alternatives with flags instead.

{{ range . }}{{ if ne .Name "other" }}
{{ sectionH2 .Description }}

` + "```" + `
{{ $maxLen := maxUsageLen .Commands }}{{ range .Commands }}{{ padRight .Usage $maxLen }}  # {{ .Description }}
{{ end }}` + "```" + `
{{ end }}{{ end }}{{ range . }}{{ if eq .Name "other" }}
{{ sectionH2 .Description }}

` + "```" + `
{{ $maxLen := maxUsageLen .Commands }}{{ range .Commands }}{{ padRight .Usage $maxLen }}  # {{ .Description }}
{{ end }}` + "```" + `
{{ end }}{{ end }}
Repo Formats
------------

All repo arguments accept these formats:

- ` + "`owner/repo`" + ` — shorthand
- ` + "`github.com/owner/repo`" + ` — canonical
- ` + "`https://github.com/owner/repo`" + ` — HTTPS URL
- ` + "`git@github.com:owner/repo.git`" + ` — SSH URL
`
