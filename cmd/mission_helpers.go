package cmd

import (
	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/database"
)

// resolveAndRunForEachMission handles the common pattern of: if no args use
// fzf selector, resolve all IDs (fail fast), then run action on each.
func resolveAndRunForEachMission(
	db *database.DB,
	args []string,
	selectFn func(*database.DB) ([]string, error),
	actionFn func(*database.DB, string) error,
) error {
	missionIDs := args
	if len(missionIDs) == 0 {
		selectedIDs, err := selectFn(db)
		if err != nil {
			return err
		}
		if len(selectedIDs) == 0 {
			return nil
		}
		missionIDs = selectedIDs
	}

	// Resolve all IDs up front (fail fast on any bad input)
	resolvedIDs := make([]string, 0, len(missionIDs))
	for _, rawID := range missionIDs {
		resolved, err := db.ResolveMissionID(rawID)
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve mission ID")
		}
		resolvedIDs = append(resolvedIDs, resolved)
	}

	for _, missionID := range resolvedIDs {
		if err := actionFn(db, missionID); err != nil {
			return err
		}
	}

	return nil
}
