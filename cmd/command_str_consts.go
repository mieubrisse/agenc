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
	primeCmdStr   = "prime"
	summaryCmdStr = "summary"

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
	injectCmdStr         = "inject"
	paletteCmdStr        = "palette"
	switchCmdStr         = "switch"
	resolveMissionCmdStr = "resolve-mission"

	// Mission subcommands
	newCmdStr          = "new"
	resumeCmdStr       = "resume"
	archiveCmdStr      = "archive"
	inspectCmdStr      = "inspect"
	nukeCmdStr         = "nuke"
	reconfigCmdStr     = "reconfig"
	updateConfigCmdStr = "update-config"
	sendCmdStr         = "send"
	claudeUpdateCmdStr = "claude-update"

	// Config subcommands
	initCmdStr           = "init"
	getCmdStr            = "get"
	setCmdStr            = "set"
	editCmdStr           = "edit"
	paletteCommandCmdStr = "paletteCommand"
	repoConfigCmdStr     = "repoConfig"

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
	cloneFlagName    = "clone"
	promptFlagName   = "prompt"
	blankFlagName    = "blank"
	adjutantFlagName = "adjutant"

	// mission ls flags
	allFlagName = "all"

	// mission inspect flags
	dirFlagName = "dir"

	// mission nuke flags
	forceFlagName = "force"

	// daemon status flags
	jsonFlagName = "json"

	// paletteCommand flags
	paletteCommandCommandFlagName     = "command"
	paletteCommandTitleFlagName       = "title"
	paletteCommandDescriptionFlagName = "description"
	paletteCommandKeybindingFlagName  = "keybinding"

	// repoConfig flags
	repoConfigAlwaysSyncedFlagName = "always-synced"
	repoConfigWindowTitleFlagName  = "window-title"

	// cron flags
	headlessFlagName = "headless"
	timeoutFlagName  = "timeout"
	cronIDFlagName   = "cron-id"
	cronNameFlagName = "cron-name"
	followFlagName   = "follow"
	cronFlagName     = "cron"
	dateFlagName     = "date"
)
