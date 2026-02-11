package claudeconfig

import (
	_ "embed"
	"os"
	"path/filepath"

	"github.com/mieubrisse/stacktrace"
)

const (
	// AgencUsageSkillDirname is the directory name for the auto-generated
	// agenc self-usage skill inside the per-mission skills/ directory.
	AgencUsageSkillDirname = "agenc-self-usage"

	agencUsageSkillFilename = "SKILL.md"
)

// agencUsageSkillContent is the SKILL.md content generated at build time by
// cmd/genskill from the Cobra command tree. It is embedded via go:embed so
// it stays in sync with the CLI automatically.
//
//go:embed agenc_usage_skill.md
var agencUsageSkillContent string

// writeAgencUsageSkill creates the agenc-self-usage skill directory and writes
// the SKILL.md file into the per-mission config. This is called during
// BuildMissionConfigDir after the shadow repo copy, so it overwrites any
// user-defined skill with the same name.
func writeAgencUsageSkill(claudeConfigDirpath string) error {
	skillDirpath := filepath.Join(claudeConfigDirpath, "skills", AgencUsageSkillDirname)

	// Remove any user-defined skill with the same name (may have been copied
	// from the shadow repo) so stale files don't leak into the auto-generated
	// skill directory.
	os.RemoveAll(skillDirpath)

	if err := os.MkdirAll(skillDirpath, 0755); err != nil {
		return stacktrace.Propagate(err, "failed to create agenc-self-usage skill directory")
	}

	skillFilepath := filepath.Join(skillDirpath, agencUsageSkillFilename)
	return WriteIfChanged(skillFilepath, []byte(agencUsageSkillContent))
}
