## agenc config claude-md

Manage AgenC-specific CLAUDE.md instructions

### Synopsis

Read and write the AgenC-specific CLAUDE.md that gets merged into every mission's config.

This file contains instructions that apply to all AgenC missions but not to
Claude Code sessions outside of AgenC. Content is appended after the user's
~/.claude/CLAUDE.md when building per-mission config.

Changes take effect for new missions automatically. Use 'agenc mission reconfig'
to propagate changes to existing missions.

### Options

```
  -h, --help   help for claude-md
```

### SEE ALSO

* [agenc config](agenc_config.md)	 - Manage agenc configuration
* [agenc config claude-md get](agenc_config_claude-md_get.md)	 - Print the AgenC-specific CLAUDE.md content
* [agenc config claude-md set](agenc_config_claude-md_set.md)	 - Update the AgenC-specific CLAUDE.md content

