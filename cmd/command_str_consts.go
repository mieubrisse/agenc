package cmd

// Centralized command name strings for all CLI commands and subcommands.
// Use these constants in Cobra Use fields and user-facing messages (error
// text, help text, remediation suggestions) so that command names are
// defined in exactly one place.

const (
	// Root command
	agencCmdStr = "agenc"

	// Top-level commands
	missionCmdStr  = "mission"
	repoCmdStr     = "repo"
	templateCmdStr = "template"
	daemonCmdStr   = "daemon"
	versionCmdStr  = "version"
	loginCmdStr    = "login"

	// Subcommands shared across multiple parent commands
	lsCmdStr     = "ls"
	rmCmdStr     = "rm"
	addCmdStr    = "add"
	updateCmdStr = "update"
	stopCmdStr   = "stop"

	// Mission subcommands
	newCmdStr     = "new"
	resumeCmdStr  = "resume"
	archiveCmdStr = "archive"
	inspectCmdStr = "inspect"
	nukeCmdStr    = "nuke"

	// Template subcommands
	editCmdStr = "edit"

	// Daemon subcommands
	startCmdStr   = "start"
	restartCmdStr = "restart"
	statusCmdStr  = "status"
)
