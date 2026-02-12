package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/tmux"
)

var configPaletteCommandAddCmd = &cobra.Command{
	Use:   addCmdStr + " <name>",
	Short: "Add a custom palette command",
	Long: `Add a custom palette command to config.yml.

The name is used as the config key and must start with a letter, containing
only letters, numbers, hyphens, and underscores (max 64 characters).
Built-in names cannot be used — use 'update' to override builtins.

Commands that reference $AGENC_CALLING_MISSION_UUID are "mission-scoped" —
they only appear in the palette when the focused pane is running a mission.

Examples:
  agenc config paletteCommand add dotfiles \
    --title="📁 Open dotfiles" \
    --command="agenc tmux window new -- agenc mission new mieubrisse/dotfiles" \
    --keybinding="f"

  agenc config paletteCommand add stopThisMission \
    --title="🛑 Stop Mission" \
    --command="agenc mission stop \$AGENC_CALLING_MISSION_UUID"
`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigPaletteCommandAdd,
}

func init() {
	configPaletteCommandCmd.AddCommand(configPaletteCommandAddCmd)
	configPaletteCommandAddCmd.Flags().String(paletteCommandTitleFlagName, "", "title shown in the palette picker (required)")
	configPaletteCommandAddCmd.Flags().String(paletteCommandCommandFlagName, "", "full command to execute (required)")
	configPaletteCommandAddCmd.Flags().String(paletteCommandKeybindingFlagName, "", "tmux keybinding (optional, e.g. \"f\" or \"C-y\")")
	configPaletteCommandAddCmd.Flags().String(paletteCommandDescriptionFlagName, "", "description shown alongside the title (optional)")
	_ = configPaletteCommandAddCmd.MarkFlagRequired(paletteCommandTitleFlagName)
	_ = configPaletteCommandAddCmd.MarkFlagRequired(paletteCommandCommandFlagName)
}

func runConfigPaletteCommandAdd(cmd *cobra.Command, args []string) error {
	name := args[0]

	if err := config.ValidatePaletteCommandName(name); err != nil {
		return err
	}

	// Reject builtin names — use 'update' for those
	if config.IsBuiltinPaletteCommand(name) {
		return stacktrace.NewError("'%s' is a built-in palette command; use '%s %s %s %s %s' to override it",
			name, agencCmdStr, configCmdStr, paletteCommandCmdStr, updateCmdStr, name)
	}

	title, err := cmd.Flags().GetString(paletteCommandTitleFlagName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read --%s flag", paletteCommandTitleFlagName)
	}

	command, err := cmd.Flags().GetString(paletteCommandCommandFlagName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read --%s flag", paletteCommandCommandFlagName)
	}

	keybinding, _ := cmd.Flags().GetString(paletteCommandKeybindingFlagName)
	description, _ := cmd.Flags().GetString(paletteCommandDescriptionFlagName)

	cfg, cm, err := readConfigWithComments()
	if err != nil {
		return err
	}

	if existing, exists := cfg.PaletteCommands[name]; exists && !existing.IsEmpty() {
		return stacktrace.NewError("palette command '%s' already exists; use '%s %s %s %s %s' to modify it",
			name, agencCmdStr, configCmdStr, paletteCommandCmdStr, updateCmdStr, name)
	}

	if cfg.PaletteCommands == nil {
		cfg.PaletteCommands = make(map[string]config.PaletteCommandConfig)
	}
	cfg.PaletteCommands[name] = config.PaletteCommandConfig{
		Title:          title,
		Description:    description,
		Command:        command,
		TmuxKeybinding: keybinding,
	}

	// Validate uniqueness before writing
	configFilepath := config.GetConfigFilepath(agencDirpath)
	resolved := cfg.GetResolvedPaletteCommands()
	if err := validateResolvedUniqueness(resolved, configFilepath); err != nil {
		return err
	}

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("Added palette command '%s'\n", name)

	if err := tmux.RefreshKeybindings(agencDirpath); err != nil {
		fmt.Printf("Warning: failed to reload tmux keybindings: %v\n", err)
	}

	return nil
}

// validateResolvedUniqueness checks title and keybinding uniqueness across
// resolved palette commands. Used by add/update commands before writing config.
func validateResolvedUniqueness(resolved []config.ResolvedPaletteCommand, configFilepath string) error {
	seenTitles := make(map[string]string)
	seenKeybindings := make(map[string]string)

	for _, cmd := range resolved {
		if cmd.Title != "" {
			if existingName, ok := seenTitles[cmd.Title]; ok {
				return stacktrace.NewError(
					"duplicate palette title '%s': used by both '%s' and '%s'",
					cmd.Title, existingName, cmd.Name,
				)
			}
			seenTitles[cmd.Title] = cmd.Name
		}

		if cmd.TmuxKeybinding != "" {
			if existingName, ok := seenKeybindings[cmd.TmuxKeybinding]; ok {
				return stacktrace.NewError(
					"duplicate palette keybinding '%s': used by both '%s' and '%s'",
					cmd.TmuxKeybinding, existingName, cmd.Name,
				)
			}
			seenKeybindings[cmd.TmuxKeybinding] = cmd.Name
		}
	}

	return nil
}
