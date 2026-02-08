package cmd

// Centralized command name strings for all CLI commands and subcommands.
// Use these constants in Cobra Use fields and user-facing messages (error
// text, help text, remediation suggestions) so that command names are
// defined in exactly one place.

const (
	// Root command
	agencCmdStr = "agenc"

	// Top-level commands
	configCmdStr  = "config"
	missionCmdStr = "mission"
	repoCmdStr    = "repo"
	daemonCmdStr  = "daemon"
	tmuxCmdStr    = "tmux"
	versionCmdStr = "version"
	loginCmdStr   = "login"
	cronCmdStr    = "cron"
	doctorCmdStr  = "doctor"

	// Subcommands shared across multiple parent commands
	lsCmdStr     = "ls"
	rmCmdStr     = "rm"
	addCmdStr    = "add"
	updateCmdStr = "update"
	stopCmdStr   = "stop"
	attachCmdStr = "attach"
	detachCmdStr = "detach"
	windowCmdStr = "window"
	paneCmdStr   = "pane"

	// Tmux subcommands
	injectCmdStr  = "inject"
	paletteCmdStr = "palette"

	// Mission subcommands
	newCmdStr          = "new"
	resumeCmdStr       = "resume"
	archiveCmdStr      = "archive"
	inspectCmdStr      = "inspect"
	nukeCmdStr         = "nuke"
	updateConfigCmdStr = "update-config"

	// Config subcommands
	initCmdStr          = "init"
	customCommandCmdStr = "custom-command"

	// Repo subcommands
	editCmdStr = "edit"

	// Daemon subcommands
	startCmdStr   = "start"
	restartCmdStr = "restart"
	statusCmdStr  = "status"

	// Cron subcommands
	enableCmdStr  = "enable"
	disableCmdStr = "disable"
	runCmdStr     = "run"
	logsCmdStr    = "logs"
	historyCmdStr = "history"
)

// Centralized flag name strings for CLI flags. Use these constants in flag
// registration, Flags().Changed() calls, and user-facing messages so that
// flag names are defined in exactly one place.

const (
	// mission new flags
	cloneFlagName  = "clone"
	promptFlagName = "prompt"

	// mission ls flags
	allFlagName = "all"

	// mission inspect flags
	dirFlagName = "dir"

	// mission nuke flags
	forceFlagName = "force"

	// repo add flags
	syncFlagName = "sync"

	// daemon status flags
	jsonFlagName = "json"

	// tmux window new flags
	parentPaneFlagName = "parent-pane"

	// custom-command flags
	customCommandArgsFlagName        = "args"
	customCommandPaletteNameFlagName = "palette-name"

	// cron flags
	headlessFlagName = "headless"
	timeoutFlagName  = "timeout"
	cronIDFlagName   = "cron-id"
	cronNameFlagName = "cron-name"
	followFlagName   = "follow"
	cronFlagName     = "cron"
)
