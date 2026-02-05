package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
)

// addTemplateToLibrary adds a template to the agenc config. Returns true if
// the template was newly added, false if it already existed. The nickname and
// defaultFor parameters are optional (pass empty strings to skip).
func addTemplateToLibrary(agencDirpath string, repoName string, nickname string, defaultFor string) (bool, error) {
	cfg, cm, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return false, stacktrace.Propagate(err, "failed to read config")
	}

	if _, exists := cfg.AgentTemplates[repoName]; exists {
		return false, nil
	}

	// Validate nickname uniqueness
	if nickname != "" {
		for otherRepo, props := range cfg.AgentTemplates {
			if props.Nickname == nickname {
				return false, stacktrace.NewError("nickname '%s' is already in use by '%s'", nickname, otherRepo)
			}
		}
	}

	// Validate defaultFor
	if defaultFor != "" {
		if !config.IsValidDefaultForValue(defaultFor) {
			return false, stacktrace.NewError("invalid defaultFor value '%s'; must be one of: %s", defaultFor, config.FormatDefaultForValues())
		}
		for otherRepo, props := range cfg.AgentTemplates {
			if props.DefaultFor == defaultFor {
				return false, stacktrace.NewError("defaultFor '%s' is already claimed by '%s'", defaultFor, otherRepo)
			}
		}
	}

	cfg.AgentTemplates[repoName] = config.AgentTemplateProperties{
		Nickname:   nickname,
		DefaultFor: defaultFor,
	}

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return false, stacktrace.Propagate(err, "failed to write config")
	}

	return true, nil
}

// launchTemplateEditMission launches a mission to edit the given template.
// This is the core logic shared by 'template edit' and 'template new'.
func launchTemplateEditMission(agencDirpath string, templateName string) error {
	ensureDaemonRunning(agencDirpath)

	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	if _, exists := cfg.AgentTemplates[templateName]; !exists {
		return stacktrace.NewError("template '%s' not found in library", templateName)
	}

	agentTemplate, err := resolveAgentTemplate(cfg, "", templateName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve agent template for editing")
	}

	templateCloneDirpath := config.GetRepoDirpath(agencDirpath, templateName)
	return createAndLaunchMission(agencDirpath, agentTemplate, templateName, templateCloneDirpath)
}

// printTemplateAdded prints a message indicating a template was added.
func printTemplateAdded(repoName string) {
	fmt.Printf("Added template '%s'\n", repoName)
}

// printTemplateAlreadyExists prints a message indicating a template already exists.
func printTemplateAlreadyExists(repoName string) {
	fmt.Printf("Template '%s' already added\n", repoName)
}
