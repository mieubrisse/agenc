## agenc config settings-json

Manage AgenC-specific settings.json overrides

### Synopsis

Read and write the AgenC-specific settings.json that gets merged into every mission's config.

This file contains settings overrides that apply to all AgenC missions but not
to Claude Code sessions outside of AgenC. Settings are deep-merged over the
user's ~/.claude/settings.json when building per-mission config (objects merge
recursively, arrays are concatenated, scalars from this file win).

Changes propagate to existing missions automatically — running missions pick them up on their next reload.

### Options

```
  -h, --help   help for settings-json
```

### SEE ALSO

* [agenc config](agenc_config.md)	 - Manage agenc configuration
* [agenc config settings-json get](agenc_config_settings-json_get.md)	 - Print the AgenC-specific settings.json content
* [agenc config settings-json set](agenc_config_settings-json_set.md)	 - Update the AgenC-specific settings.json content

