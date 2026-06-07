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

const (
	outputFilepath    = "./internal/claudeconfig/prime_content.md"
	preambleFilepath  = "./internal/claudeconfig/prime_preamble.md"
	postambleFilepath = "./internal/claudeconfig/prime_postamble.md"
)

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

	preamble, err := os.ReadFile(preambleFilepath)
	if err != nil {
		log.Fatalf("failed to read %s: %v", preambleFilepath, err)
	}

	postamble, err := os.ReadFile(postambleFilepath)
	if err != nil {
		log.Fatalf("failed to read %s: %v", postambleFilepath, err)
	}

	commandGroups, err := renderCommandGroups(groups)
	if err != nil {
		log.Fatalf("failed to render command groups: %v", err)
	}

	var sb strings.Builder
	sb.Write(preamble)
	sb.WriteString(commandGroups)
	sb.WriteString("\n")
	sb.Write(postamble)

	if err := os.WriteFile(outputFilepath, []byte(sb.String()), 0644); err != nil {
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

// renderCommandGroups renders the auto-generated middle section of prime_content.md
// from the Cobra command tree. The hand-written preamble and postamble live in
// prime_preamble.md and prime_postamble.md respectively and are concatenated
// around this rendered middle by main().
func renderCommandGroups(groups []commandGroup) (string, error) {
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

	tmpl, err := template.New("commandGroups").Funcs(funcMap).Parse(commandGroupsTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var sb strings.Builder
	if err := tmpl.Execute(&sb, groups); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return sb.String(), nil
}

// commandGroupsTemplate renders the per-group sections of the CLI command
// reference. Non-"other" groups render first in topLevelOrder; the catch-all
// "other" group renders last.
var commandGroupsTemplate = `{{ range . }}{{ if ne .Name "other" }}
{{ sectionH2 .Description }}

` + "```" + `
{{ $maxLen := maxUsageLen .Commands }}{{ range .Commands }}{{ padRight .Usage $maxLen }}  # {{ .Description }}
{{ end }}` + "```" + `
{{ end }}{{ end }}{{ range . }}{{ if eq .Name "other" }}
{{ sectionH2 .Description }}

` + "```" + `
{{ $maxLen := maxUsageLen .Commands }}{{ range .Commands }}{{ padRight .Usage $maxLen }}  # {{ .Description }}
{{ end }}` + "```" + `
{{ end }}{{ end }}`
