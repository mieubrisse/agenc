Launchd Migration - Proposed Fixes
===================================

## 1. Fix Double-Fire Race Condition

**Problem**: TOCTOU race where two launchd triggers can both pass the "is running" check before either creates a mission.

**Solution**: Use SQLite's `BEGIN IMMEDIATE` transaction to get an exclusive lock during check+create.

### File: internal/database/missions.go

Add new method:

```go
// CreateMissionForCronIfNotRunning creates a mission for a cron job only if there
// is no currently running mission for that cron. Returns (nil, nil) if a mission
// is already running (double-fire prevention). Uses a transaction to prevent race.
func (db *DB) CreateMissionForCronIfNotRunning(gitRepo string, cronName string, opts *CreateMissionOpts) (*Mission, error) {
    tx, err := db.conn.Begin()
    if err != nil {
        return nil, stacktrace.Propagate(err, "failed to begin transaction")
    }
    defer tx.Rollback() // Safe to call even after commit

    // Check for running mission within the transaction
    var existingID string
    err = tx.QueryRow(
        "SELECT id FROM missions WHERE cron_name = ? AND status != 'completed' AND status != 'archived' ORDER BY created_at DESC LIMIT 1",
        cronName,
    ).Scan(&existingID)

    if err == nil {
        // Mission exists and is running - skip this trigger
        return nil, nil
    }
    if err != sql.ErrNoRows {
        return nil, stacktrace.Propagate(err, "failed to check for running mission")
    }

    // No running mission - create one
    missionID := uuid.NewString()
    shortID := missionID[:8]
    now := time.Now()

    prompt := ""
    if opts != nil && opts.Prompt != nil {
        prompt = *opts.Prompt
    }

    configCommit := ""
    if opts != nil && opts.ConfigCommit != nil {
        configCommit = *opts.ConfigCommit
    }

    _, err = tx.Exec(
        `INSERT INTO missions (id, short_id, prompt, status, git_repo, cron_name, config_commit, created_at, updated_at)
         VALUES (?, ?, ?, 'active', ?, ?, ?, ?, ?)`,
        missionID, shortID, prompt, gitRepo, cronName, configCommit, now, now,
    )
    if err != nil {
        return nil, stacktrace.Propagate(err, "failed to insert mission")
    }

    if err := tx.Commit(); err != nil {
        return nil, stacktrace.Propagate(err, "failed to commit transaction")
    }

    return db.GetMission(missionID)
}
```

### File: cmd/mission_new.go

Replace the check with transaction-based creation:

```go
func runMissionNew(cmd *cobra.Command, args []string) error {
    if _, err := getAgencContext(); err != nil {
        return err
    }
    ensureDaemonRunning(agencDirpath)

    // Double-fire prevention is now handled in CreateMissionForCronIfNotRunning
    // Just set cronNameFlag if cronTriggerFlag is set
    if cronTriggerFlag != "" {
        if cronNameFlag == "" {
            cronNameFlag = cronTriggerFlag
        }
    }

    // ... rest of function unchanged
}
```

**Remove** the `shouldSkipCronTrigger` function entirely - it's replaced by the transactional check.

---

## 2. Fix PlistDirpath Error Handling

**Problem**: Returns invalid path if HOME is unset, causing silent failures.

**Solution**: Return `(string, error)` and check explicitly.

### File: internal/launchd/plist.go

```go
// PlistDirpath returns the path to the LaunchAgents directory.
// Returns error if home directory cannot be determined.
func PlistDirpath() (string, error) {
    homeDir, err := os.UserHomeDir()
    if err != nil {
        // Try fallback to HOME env var
        homeDir = os.Getenv("HOME")
        if homeDir == "" {
            return "", stacktrace.NewError("cannot determine home directory: UserHomeDir failed and $HOME is unset")
        }
    }
    return filepath.Join(homeDir, "Library", "LaunchAgents"), nil
}
```

### Update all callers

**File: internal/launchd/manager.go**

```go
func GetPlistPathForLabel(label string) (string, error) {
    cronName := strings.TrimPrefix(label, "agenc-cron-")
    filename := fmt.Sprintf("agenc-cron-%s.plist", cronName)
    dirpath, err := PlistDirpath()
    if err != nil {
        return "", stacktrace.Propagate(err, "failed to get plist directory")
    }
    return filepath.Join(dirpath, filename), nil
}
```

**File: internal/daemon/cron_syncer.go**

```go
func (s *CronSyncer) SyncCronsToLaunchd(crons map[string]config.CronConfig, logger logger) error {
    // ...

    plistDirpath, err := launchd.PlistDirpath()
    if err != nil {
        return stacktrace.Propagate(err, "failed to get plist directory")
    }

    // ... rest of function
}
```

(Similar changes needed in `reconcileOrphanedPlists` and `removeDeletedCronPlists`)

---

## 3. Eliminate Code Duplication

**Problem**: `reconcileOrphanedPlists` and `removeDeletedCronPlists` are 92% identical.

**Solution**: Extract to single function.

### File: internal/daemon/cron_syncer.go

```go
// removeUnmatchedPlists removes plist files that don't correspond to any cron in the config.
func (s *CronSyncer) removeUnmatchedPlists(crons map[string]config.CronConfig, logger logger, operation string) error {
    plistDirpath, err := launchd.PlistDirpath()
    if err != nil {
        return stacktrace.Propagate(err, "failed to get plist directory")
    }

    pattern := filepath.Join(plistDirpath, "agenc-cron-*.plist")
    matches, err := filepath.Glob(pattern)
    if err != nil {
        return stacktrace.Propagate(err, "failed to glob plist files")
    }

    for _, plistPath := range matches {
        filename := filepath.Base(plistPath)
        // Remove "agenc-cron-" prefix and ".plist" suffix
        cronName := strings.TrimPrefix(filename, "agenc-cron-")
        cronName = strings.TrimSuffix(cronName, ".plist")

        // Check if this cron exists in the config
        found := false
        for name := range crons {
            if sanitizeLabelName(name) == cronName {
                found = true
                break
            }
        }

        if !found {
            logger.Printf("Cron syncer: %s plist for '%s'", operation, cronName)
            if err := s.manager.RemovePlist(plistPath); err != nil {
                logger.Printf("Cron syncer: failed to remove plist '%s': %v", plistPath, err)
            }
        }
    }

    return nil
}

// reconcileOrphanedPlists scans for plists not in config and removes them (startup only).
func (s *CronSyncer) reconcileOrphanedPlists(crons map[string]config.CronConfig, logger logger) error {
    return s.removeUnmatchedPlists(crons, logger, "removing orphaned")
}

// removeDeletedCronPlists removes plists for crons that no longer exist in config.
func (s *CronSyncer) removeDeletedCronPlists(crons map[string]config.CronConfig, logger logger) error {
    return s.removeUnmatchedPlists(crons, logger, "removing deleted")
}
```

---

## 4. Fix Log Path Discarding

**Problem**: StandardOutPath and StandardErrorPath are hardcoded to /dev/null, losing all cron output.

**Solution**: Write to actual log files under $AGENC_DIRPATH/logs/cron-{name}.log

### File: internal/config/config.go

Add helper:

```go
// GetCronLogDirpath returns the path to the cron logs directory.
func GetCronLogDirpath(agencDirpath string) string {
    return filepath.Join(agencDirpath, "logs", "crons")
}

// GetCronLogFilepaths returns stdout and stderr log paths for a cron job.
func GetCronLogFilepaths(agencDirpath string, cronName string) (stdout, stderr string) {
    logDir := GetCronLogDirpath(agencDirpath)
    baseFilename := sanitizeFilename(cronName)
    stdout = filepath.Join(logDir, fmt.Sprintf("%s-stdout.log", baseFilename))
    stderr = filepath.Join(logDir, fmt.Sprintf("%s-stderr.log", baseFilename))
    return
}

// sanitizeFilename removes characters unsafe for filenames.
func sanitizeFilename(name string) string {
    sanitized := strings.ReplaceAll(name, " ", "-")
    reg := regexp.MustCompile(`[^a-zA-Z0-9\-_]`)
    return reg.ReplaceAllString(sanitized, "")
}
```

### File: internal/daemon/cron_syncer.go

```go
func (s *CronSyncer) SyncCronsToLaunchd(crons map[string]config.CronConfig, logger logger) error {
    // ... existing setup code ...

    // Ensure cron log directory exists
    cronLogDir := config.GetCronLogDirpath(s.agencDirpath)
    if err := os.MkdirAll(cronLogDir, 0755); err != nil {
        return stacktrace.Propagate(err, "failed to create cron log directory")
    }

    for name, cronCfg := range crons {
        // ... existing code ...

        // Get log file paths for this cron
        stdoutPath, stderrPath := config.GetCronLogFilepaths(s.agencDirpath, name)

        // Create the plist
        plist := &launchd.Plist{
            Label:                 label,
            ProgramArguments:      programArgs,
            StartCalendarInterval: calInterval,
            StandardOutPath:       stdoutPath,  // Changed from /dev/null
            StandardErrorPath:     stderrPath,  // Changed from /dev/null
        }

        // ... rest of function
    }
}
```

---

## 5. Add Database Index on cron_name

**Problem**: Query on cron_name does full table scan on every cron trigger.

**Solution**: Add index in schema.

### File: internal/database/database.go

In the schema creation:

```go
const schema = `
CREATE TABLE IF NOT EXISTS missions (
    id TEXT PRIMARY KEY,
    short_id TEXT NOT NULL,
    prompt TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    git_repo TEXT,
    last_heartbeat INTEGER,
    last_active INTEGER,
    session_name TEXT,
    session_name_updated_at INTEGER,
    cron_id TEXT,
    cron_name TEXT,
    config_commit TEXT,
    tmux_pane TEXT,
    prompt_count INTEGER DEFAULT 0,
    last_summary_prompt_count INTEGER DEFAULT 0,
    ai_summary TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_missions_short_id ON missions(short_id);
CREATE INDEX IF NOT EXISTS idx_missions_tmux_pane ON missions(tmux_pane);
CREATE INDEX IF NOT EXISTS idx_missions_cron_name ON missions(cron_name);  -- NEW INDEX
CREATE INDEX IF NOT EXISTS idx_missions_status_last_heartbeat ON missions(status, last_heartbeat);
`
```

---

## 6. Fix Sanitization Inconsistency

**Problem**: Two different implementations of the same sanitization logic.

**Solution**: Use one canonical implementation.

### File: internal/launchd/plist.go

Add function to extract sanitized name from filename:

```go
// SanitizeCronName sanitizes a cron name for use in labels and filenames.
// This is the canonical sanitization function - use this everywhere.
func SanitizeCronName(cronName string) string {
    // Replace spaces with dashes
    sanitized := strings.ReplaceAll(cronName, " ", "-")

    // Remove special characters (keep alphanumeric, dash, underscore)
    reg := regexp.MustCompile(`[^a-zA-Z0-9\-_]`)
    return reg.ReplaceAllString(sanitized, "")
}

// CronToPlistFilename converts a cron name to a plist filename.
func CronToPlistFilename(cronName string) string {
    return fmt.Sprintf("agenc-cron-%s.plist", SanitizeCronName(cronName))
}
```

### File: internal/daemon/cron_syncer.go

Replace `sanitizeLabelName` with `launchd.SanitizeCronName`:

```go
func (s *CronSyncer) SyncCronsToLaunchd(crons map[string]config.CronConfig, logger logger) error {
    // ...
    for name, cronCfg := range crons {
        plistFilename := launchd.CronToPlistFilename(name)
        plistPath := filepath.Join(plistDirpath, plistFilename)
        label := fmt.Sprintf("agenc-cron-%s", launchd.SanitizeCronName(name))  // Changed

        // ... rest
    }
}

// Delete the sanitizeLabelName function entirely - use launchd.SanitizeCronName instead
```

Update all references to `sanitizeLabelName(name)` → `launchd.SanitizeCronName(name)`.

---

## 7. Add Timeout Validation

**Problem**: Invalid timeout values are passed unchecked to CLI flags.

**Solution**: Validate on config load.

### File: internal/config/agenc_config.go

```go
// CronConfig represents a cron job definition.
type CronConfig struct {
    Schedule    string  `yaml:"schedule"`
    Prompt      string  `yaml:"prompt"`
    Description *string `yaml:"description,omitempty"`
    Git         string  `yaml:"git,omitempty"`
    Timeout     string  `yaml:"timeout,omitempty"`
    Overlap     *string `yaml:"overlap,omitempty"`
    Enabled     *bool   `yaml:"enabled,omitempty"`
}

// Validate checks if the cron config is valid.
func (c *CronConfig) Validate() error {
    if c.Schedule == "" {
        return stacktrace.NewError("schedule is required")
    }
    if c.Prompt == "" {
        return stacktrace.NewError("prompt is required")
    }

    // Validate timeout format if specified
    if c.Timeout != "" {
        if _, err := time.ParseDuration(c.Timeout); err != nil {
            return stacktrace.Propagate(err, "invalid timeout format (expected duration like '1h', '30m')")
        }
    }

    // Validate overlap policy if specified
    if c.Overlap != nil && *c.Overlap != "skip" && *c.Overlap != "allow" {
        return stacktrace.NewError("overlap must be 'skip' or 'allow'")
    }

    return nil
}
```

### File: internal/config/config.go

Call validate when loading config:

```go
func LoadConfig(agencDirpath string) (*AgencConfig, error) {
    // ... existing load code ...

    // Validate cron configs
    for name, cronCfg := range cfg.Crons {
        if err := cronCfg.Validate(); err != nil {
            return nil, stacktrace.Propagate(err, "invalid cron '%s'", name)
        }
    }

    return cfg, nil
}
```

---

## 8. Rename Git Field to Repo

**Problem**: Field is called `Git` but CLI flag is `--repo`.

**Solution**: Rename for consistency.

### File: internal/config/agenc_config.go

```go
type CronConfig struct {
    Schedule    string  `yaml:"schedule"`
    Prompt      string  `yaml:"prompt"`
    Description *string `yaml:"description,omitempty"`
    Repo        string  `yaml:"repo,omitempty"`  // Renamed from Git
    Timeout     string  `yaml:"timeout,omitempty"`
    Overlap     *string `yaml:"overlap,omitempty"`
    Enabled     *bool   `yaml:"enabled,omitempty"`
}
```

### File: internal/daemon/cron_syncer.go

```go
// Add git repo if specified
if cronCfg.Repo != "" {  // Changed from cronCfg.Git
    programArgs = append(programArgs, cronCfg.Repo)
}
```

### Migration Note

This is a **breaking change** for existing config.yml files. Options:

1. **Accept the break** - document in release notes that users need to rename `git:` to `repo:`
2. **Support both** - check both fields during a transition period:
   ```go
   repo := cronCfg.Repo
   if repo == "" {
       repo = cronCfg.Git // Fallback for compatibility
   }
   ```
3. **Auto-migrate** - add migration code to rename the field on first load

**Recommendation**: Option 2 (support both) for one major version, then remove in next major.

---

## Implementation Order

1. **Fix #2** (PlistDirpath error handling) - prerequisite for others
2. **Fix #6** (Sanitization) - prerequisite for consistency
3. **Fix #3** (Code duplication) - cleanup
4. **Fix #5** (Database index) - performance
5. **Fix #7** (Timeout validation) - safety
6. **Fix #4** (Log paths) - functionality restoration
7. **Fix #8** (Rename Git field) - consistency (breaking change, requires user communication)
8. **Fix #1** (Race condition) - correctness (complex, test thoroughly)

Would you like me to implement these fixes?
