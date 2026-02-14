// cmd/genskill generates the AgenC CLI quick reference content by introspecting
// the Cobra command tree. The output is embedded into the binary via go:embed
// at compile time and printed by `agenc prime`. Run via: go run ./cmd/genskill
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

// commandEntry represents a single command in the skill reference.
type commandEntry struct {
	Usage       string
	Description string
}

func main() {
	rootCmd := cmd.GetRootCmd()

	groups := buildCommandGroups(rootCmd)

	content, err := renderSkill(groups)
	if err != nil {
		log.Fatalf("failed to render skill: %v", err)
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
	"daemon",
}

// buildCommandGroups walks the Cobra command tree and organizes commands
// into groups for template rendering.
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
			for _, sub := range subcommands {
				if sub.Hidden || !sub.IsAvailableCommand() {
					continue
				}
				group.Commands = append(group.Commands, commandEntry{
					Usage:       fmt.Sprintf("agenc %s %s", name, sub.Use),
					Description: sub.Short,
				})
			}
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

// renderSkill renders the SKILL.md content from the command groups.
func renderSkill(groups []commandGroup) (string, error) {
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

	tmpl, err := template.New("skill").Funcs(funcMap).Parse(skillTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var sb strings.Builder
	if err := tmpl.Execute(&sb, groups); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return sb.String(), nil
}

// skillTemplate is the Go text/template for the SKILL.md content.
// The static preamble provides context for agents; the dynamic sections
// are populated from the Cobra command tree.
var skillTemplate = `AgenC CLI Quick Reference
=========================

You are running inside an **AgenC mission** — an isolated sandbox managed by the ` + "`agenc`" + ` CLI. You can use ` + "`agenc`" + ` to manage the system you are running in: spawn new missions, manage repos, configure cron jobs, check status, and update config.

The ` + "`agenc`" + ` binary is in your PATH. Your current mission's UUID is in ` + "`$AGENC_MISSION_UUID`" + `.

**Critical constraint:** Missions are ephemeral. Local filesystems do not persist after a mission ends. Always commit and push your work.

**Never use interactive commands** that open ` + "`$EDITOR`" + ` or require terminal input without arguments — they will hang. Avoid: ` + "`agenc config edit`" + `, ` + "`agenc cron new`" + `. Use non-interactive alternatives (` + "`agenc config set`" + `, direct config.yml editing).

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
Key Concepts
------------

- **Missions** are isolated workspaces. Each mission gets its own tmux window, its own copy of a git repo (the ` + "`agent/`" + ` directory), and its own Claude Code config (` + "`claude-config/`" + `). Missions are ephemeral — local filesystems do not survive archival. Always commit and push.
- **Repos** are git repositories in the repo library (` + "`~/.agenc/repos/`" + `). When a mission is created from a repo, the repo is cloned into the mission's ` + "`agent/`" + ` directory.
- **Tmux** is the primary interface. Each mission is a tmux window. "Window title" means the tmux window name — the text shown in the status bar. AgenC sets window titles from the repo name or a custom string.
- **Palette commands** are quick-launch entries in the AgenC tmux command palette. Each has a ` + "`name`" + ` (internal key), ` + "`title`" + ` (display text), ` + "`command`" + ` (what it runs), and optional ` + "`tmuxKeybinding`" + `. Stored in ` + "`config.yml`" + ` under ` + "`paletteCommands`" + `. Manage with ` + "`agenc config paletteCommand`" + `. Keybinding values are passed through to tmux's bind-key: a bare key like ` + "`\"f\"`" + ` binds in the agenc table (prefix + a, f), while ` + "`\"-n C-s\"`" + ` binds globally in the root table (no prefix needed).
- **Cron jobs** are scheduled headless missions defined in ` + "`config.yml`" + `. Manage with ` + "`agenc cron`" + `.
- **The daemon** is a background process that handles scheduled tasks, repo syncing, and credential management.

Repo Formats
------------

All repo arguments accept these formats:

- ` + "`owner/repo`" + ` — shorthand
- ` + "`github.com/owner/repo`" + ` — canonical
- ` + "`https://github.com/owner/repo`" + ` — HTTPS URL
- ` + "`git@github.com:owner/repo.git`" + ` — SSH URL
`
