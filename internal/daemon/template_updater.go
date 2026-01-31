package daemon

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/odyssey/agenc/internal/config"
)

const (
	templateUpdateInterval = 60 * time.Second
)

// runTemplateUpdateLoop periodically fetches and fast-forwards all agent
// template repos to match their origin/main branch.
func (d *Daemon) runTemplateUpdateLoop(ctx context.Context) {
	d.runTemplateUpdateCycle(ctx)

	ticker := time.NewTicker(templateUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.runTemplateUpdateCycle(ctx)
		}
	}
}

func (d *Daemon) runTemplateUpdateCycle(ctx context.Context) {
	templateNames, err := config.ListAgentTemplates(d.agencDirpath)
	if err != nil {
		d.logger.Printf("Template update: failed to list templates: %v", err)
		return
	}

	for _, templateName := range templateNames {
		if ctx.Err() != nil {
			return
		}
		d.updateTemplate(ctx, templateName)
	}
}

func (d *Daemon) updateTemplate(ctx context.Context, templateName string) {
	templateDirpath := config.GetAgentTemplateDirpath(d.agencDirpath, templateName)

	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin")
	fetchCmd.Dir = templateDirpath
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		d.logger.Printf("Template update: git fetch failed for '%s': %v\n%s", templateName, err, string(output))
		return
	}

	localHash, err := gitRevParse(ctx, templateDirpath, "main")
	if err != nil {
		d.logger.Printf("Template update: failed to rev-parse main for '%s': %v", templateName, err)
		return
	}

	remoteHash, err := gitRevParse(ctx, templateDirpath, "origin/main")
	if err != nil {
		d.logger.Printf("Template update: failed to rev-parse origin/main for '%s': %v", templateName, err)
		return
	}

	if localHash == remoteHash {
		return
	}

	resetCmd := exec.CommandContext(ctx, "git", "reset", "--hard", "origin/main")
	resetCmd.Dir = templateDirpath
	if output, err := resetCmd.CombinedOutput(); err != nil {
		d.logger.Printf("Template update: git reset failed for '%s': %v\n%s", templateName, err, string(output))
		return
	}

	d.logger.Printf("Template update: updated '%s' to %s", templateName, remoteHash[:8])
}

// gitRevParse runs `git rev-parse <ref>` in the given directory and returns
// the trimmed commit hash.
func gitRevParse(ctx context.Context, dirpath string, ref string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", ref)
	cmd.Dir = dirpath
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
