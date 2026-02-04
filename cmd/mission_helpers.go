package cmd

import (
	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

// openDB centralizes the database opening boilerplate used by every command
// that touches the mission database.
func openDB() (*database.DB, error) {
	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to open database")
	}
	return db, nil
}

// resolveAndRunForMission handles the common pattern for commands that always
// receive exactly one mission ID: open DB, resolve the ID, run the action.
func resolveAndRunForMission(rawID string, fn func(*database.DB, string) error) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	missionID, err := db.ResolveMissionID(rawID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve mission ID")
	}

	return fn(db, missionID)
}

// resolveAndRunForEachMission handles the common pattern of: if no args use
// fzf selector, resolve all IDs (fail fast), then run action on each.
func resolveAndRunForEachMission(
	args []string,
	selectFn func(*database.DB) ([]string, error),
	actionFn func(*database.DB, string) error,
) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

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
