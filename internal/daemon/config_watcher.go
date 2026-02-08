package daemon

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
)

const (
	// ingestDebounce is the delay after the last filesystem event before
	// triggering an ingest. This batches rapid changes (e.g., writing
	// multiple files in quick succession).
	ingestDebounce = 500 * time.Millisecond
)

// runConfigWatcherLoop watches ~/.claude for changes to tracked files and
// ingests them into the shadow repo with path normalization.
func (d *Daemon) runConfigWatcherLoop(ctx context.Context) {
	userClaudeDirpath, err := config.GetUserClaudeDirpath()
	if err != nil {
		d.logger.Printf("Config watcher: failed to determine ~/.claude path: %v", err)
		return
	}

	shadowDirpath := claudeconfig.GetShadowRepoDirpath(d.agencDirpath)

	// Wait for shadow repo to exist before starting
	if !waitForDirectory(ctx, shadowDirpath, 30*time.Second) {
		d.logger.Println("Config watcher: shadow repo not found, stopping")
		return
	}

	d.watchClaudeDir(ctx, userClaudeDirpath, shadowDirpath)
}

// watchClaudeDir sets up fsnotify watches and runs the event loop.
func (d *Daemon) watchClaudeDir(ctx context.Context, userClaudeDirpath string, shadowDirpath string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		d.logger.Printf("Config watcher: failed to create watcher: %v", err)
		return
	}
	defer watcher.Close()

	// Add watches for tracked files and directories
	d.addTrackedWatches(watcher, userClaudeDirpath)

	var debounceTimer *time.Timer

	for {
		select {
		case <-ctx.Done():
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			if !isTrackedPath(event.Name, userClaudeDirpath) {
				continue
			}

			// Reset debounce timer on each event
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(ingestDebounce, func() {
				d.ingestClaudeConfig(userClaudeDirpath, shadowDirpath)
				// Re-add watches in case directories were created/removed
				d.addTrackedWatches(watcher, userClaudeDirpath)
			})

		case watchErr, ok := <-watcher.Errors:
			if !ok {
				return
			}
			d.logger.Printf("Config watcher: fsnotify error: %v", watchErr)
		}
	}
}

// addTrackedWatches adds fsnotify watches for the ~/.claude directory and
// all tracked subdirectories. Resolves symlinks so we watch actual targets.
func (d *Daemon) addTrackedWatches(watcher *fsnotify.Watcher, userClaudeDirpath string) {
	// Watch the ~/.claude directory itself (for file creates/deletes)
	addWatch(watcher, userClaudeDirpath)

	// Watch tracked directories (resolve symlinks)
	for _, dirName := range claudeconfig.TrackedDirNames {
		dirpath := filepath.Join(userClaudeDirpath, dirName)
		resolved, err := filepath.EvalSymlinks(dirpath)
		if err != nil {
			continue // Directory doesn't exist
		}

		// Walk and add watches for all subdirectories
		filepath.Walk(resolved, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				addWatch(watcher, path)
			}
			return nil
		})
	}

	// Watch symlink targets for tracked files
	for _, fileName := range claudeconfig.TrackedFileNames {
		filePath := filepath.Join(userClaudeDirpath, fileName)
		resolved, err := filepath.EvalSymlinks(filePath)
		if err != nil {
			continue
		}
		// Watch the directory containing the resolved file
		addWatch(watcher, filepath.Dir(resolved))
	}
}

// addWatch adds a path to the watcher, ignoring errors (e.g., already watched
// or path doesn't exist).
func addWatch(watcher *fsnotify.Watcher, path string) {
	// Ignore errors — path may not exist or may already be watched
	watcher.Add(path)
}

// isTrackedPath returns true if the filesystem event path corresponds to a
// tracked file or is inside a tracked directory.
func isTrackedPath(eventPath string, userClaudeDirpath string) bool {
	// Check if it's a tracked file
	for _, fileName := range claudeconfig.TrackedFileNames {
		trackedFilepath := filepath.Join(userClaudeDirpath, fileName)
		resolved, err := filepath.EvalSymlinks(trackedFilepath)
		if err == nil && eventPath == resolved {
			return true
		}
		if eventPath == trackedFilepath {
			return true
		}
	}

	// Check if it's inside a tracked directory
	for _, dirName := range claudeconfig.TrackedDirNames {
		trackedDirpath := filepath.Join(userClaudeDirpath, dirName)
		resolved, err := filepath.EvalSymlinks(trackedDirpath)
		if err == nil {
			if isPathUnder(eventPath, resolved) {
				return true
			}
		}
		if isPathUnder(eventPath, trackedDirpath) {
			return true
		}
	}

	return false
}

// isPathUnder returns true if child is under or equal to parent.
func isPathUnder(child string, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	// rel should not start with ".." if child is under parent
	return rel != ".." && (len(rel) < 3 || rel[:3] != "../")
}

// ingestClaudeConfig runs the ingest from ~/.claude to the shadow repo.
func (d *Daemon) ingestClaudeConfig(userClaudeDirpath string, shadowDirpath string) {
	if err := claudeconfig.IngestFromClaudeDir(userClaudeDirpath, shadowDirpath); err != nil {
		d.logger.Printf("Config watcher: ingest failed: %v", err)
	}
}

// waitForDirectory blocks until the given directory exists or the context is
// cancelled. Returns true if the directory was found, false if the context
// was cancelled or the timeout expired.
func waitForDirectory(ctx context.Context, dirpath string, timeout time.Duration) bool {
	deadline := time.After(timeout)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		if _, err := os.Stat(dirpath); err == nil {
			return true
		}

		select {
		case <-ctx.Done():
			return false
		case <-deadline:
			return false
		case <-ticker.C:
			continue
		}
	}
}
