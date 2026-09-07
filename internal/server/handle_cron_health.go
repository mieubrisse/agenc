package server

import (
	"net/http"
	"time"

	"github.com/odyssey/agenc/internal/cronhealth"
)

// CronHealthFindingResponse is one cron that has gone quiet.
type CronHealthFindingResponse struct {
	Name          string    `json:"name"`
	Schedule      string    `json:"schedule"`
	QuietSince    time.Time `json:"quiet_since"`
	HasEverRun    bool      `json:"has_ever_run"`
	Detail        string    `json:"detail"`
	LaunchdStatus string    `json:"launchd_status,omitempty"`
	// AlreadyReported is true when the user has already been sent a
	// notification about this cron's current quiet spell.
	AlreadyReported bool `json:"already_reported"`
}

// CronHealthResponse is the monitor's current picture of the cron fleet.
type CronHealthResponse struct {
	MonitoredCronCount int `json:"monitored_cron_count"`
	// LastBackgroundCheckAt is when the background monitor last completed a
	// pass. Nil until its first pass, which is delayed after server startup.
	// It is reported so the monitor's own liveness is inspectable — otherwise
	// the only signal a healthy monitor produces is silence, and silence cannot
	// be told apart from a monitor that has stopped running.
	LastBackgroundCheckAt *time.Time                  `json:"last_background_check_at,omitempty"`
	QuietCrons            []CronHealthFindingResponse `json:"quiet_crons"`
}

// handleGetCronHealth reports which crons have stopped producing missions.
//
// Read-only: it evaluates and returns, and never notifies or records anything.
// The background monitor owns those side effects; this endpoint exists so the
// judgment can be inspected on demand rather than only inferred from the
// notifications it produces.
func (s *Server) handleGetCronHealth(w http.ResponseWriter, r *http.Request) error {
	now := time.Now()

	monitored, err := s.gatherMonitoredCrons(now, mustNotTouchMonitorMemory)
	if err != nil {
		return err
	}

	alreadyReportedByName := make(map[string]bool, len(monitored))
	cronIDsByName := make(map[string]string, len(monitored))
	for _, cron := range monitored {
		alreadyReportedByName[cron.state.Name] = cron.hasBeenReportedQuiet
		cronIDsByName[cron.state.Name] = cron.cronID
	}

	findings := cronhealth.Evaluate(collectCronStates(monitored), now)
	quietCrons := make([]CronHealthFindingResponse, 0, len(findings))
	for _, finding := range findings {
		quietCrons = append(quietCrons, CronHealthFindingResponse{
			Name:            finding.CronName,
			Schedule:        finding.ScheduleExpression,
			QuietSince:      finding.QuietSince,
			HasEverRun:      finding.HasEverRun,
			Detail:          finding.Detail,
			LaunchdStatus:   s.describeLaunchdJob(cronIDsByName[finding.CronName]),
			AlreadyReported: alreadyReportedByName[finding.CronName],
		})
	}

	writeJSON(w, http.StatusOK, CronHealthResponse{
		MonitoredCronCount:    len(monitored),
		LastBackgroundCheckAt: s.lastCronHealthCycleAt.Load(),
		QuietCrons:            quietCrons,
	})
	return nil
}
