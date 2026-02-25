## agenc config repoConfig

Manage per-repo configuration

### Synopsis

Manage per-repo configuration in config.yml.

Each repo is identified by its canonical name (github.com/owner/repo) and
supports four optional settings:

  alwaysSynced       - daemon keeps the repo continuously fetched (every 60s)
  windowTitle        - custom tmux window name for missions using this repo
  defaultModel       - default Claude model for missions using this repo
  trustedMcpServers  - pre-approve MCP servers to skip the consent prompt

Example config.yml:

  repoConfig:
    github.com/owner/repo:
      alwaysSynced: true
      windowTitle: "my-repo"
      defaultModel: opus
      trustedMcpServers: all
    github.com/owner/other:
      alwaysSynced: true


### Options

```
  -h, --help   help for repoConfig
```

### SEE ALSO

* [agenc config](agenc_config.md)	 - Manage agenc configuration
* [agenc config repoConfig ls](agenc_config_repoConfig_ls.md)	 - List per-repo configuration
* [agenc config repoConfig rm](agenc_config_repoConfig_rm.md)	 - Remove per-repo configuration
* [agenc config repoConfig set](agenc_config_repoConfig_set.md)	 - Set per-repo configuration

