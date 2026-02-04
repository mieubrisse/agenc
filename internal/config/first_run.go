package config

import (
	"os"

	"github.com/mieubrisse/stacktrace"
)

// IsFirstRun returns true if the agenc directory does not yet exist.
func IsFirstRun(agencDirpath string) (bool, error) {
	_, err := os.Stat(agencDirpath)
	if err == nil {
		return false, nil
	}
	if os.IsNotExist(err) {
		return true, nil
	}
	return false, stacktrace.Propagate(err, "failed to stat agenc directory '%s'", agencDirpath)
}
