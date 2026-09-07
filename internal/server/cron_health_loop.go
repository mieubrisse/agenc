package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/cronhealth"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/launchd"
)

const (
	// cronHealthCheckInterval is how often the monitor re-derives which crons
	// have gone quiet. Each cycle is a cached config read, one missions query
	// and one small state query, so a short interval costs almost nothing and
	// keeps the notice close behind the event.
	cronHealthCheckInterval = 1 * time.Minute

	// cronHealthStartupDelay keeps the monitor from judging during the first
	// moments after the server comes up. A machine waking from sleep or booting
	// starts the server and replays launchd's deferred cron fires at roughly
	// the same time; without this the monitor could read the seconds before
	// those runs land as silence.
	cronHealthStartupDelay = 3 * time.Minute

	// cronQuietNotificationKind tags the notification this monitor posts.
	cronQuietNotificationKind = "cron.quiet"

	// cronMissionSource is the missions.source value the cron scheduler writes.
	// Missions carrying it are how a cron proves it ran.
	cronMissionSource = "cron"
)

// cronHealthStartupDelayForEnvironment returns how long to wait before the
// first pass.
//
// The delay exists to let launchd replay the cron fires it deferred while the
// machine slept, so that the monitor does not read the seconds between "machine
// woke up" and "those runs landed" as silence. The test environment never
// registers plists, so there is no launchd to wait for and no reason to wait.
func cronHealthStartupDelayForEnvironment() time.Duration {
	if config.IsTestEnv() {
		return 0
	}
	return cronHealthStartupDelay
}

// monitoredCron pairs a cron's judgeable state with the identity and memory the
// monitor needs in order to act on the judgment.
type monitoredCron struct {
	cronID string
	state  cronhealth.CronState
	// hasBeenReportedQuiet is true when the user has already been told about
	// this cron's current quiet spell, which is what keeps a cron broken for a
	// month to one note rather than thirty.
	hasBeenReportedQuiet bool
}

// runCronHealthLoop periodically checks whether every enabled cron is still
// producing missions, and posts one informational notification naming the crons
// that have gone quiet.
//
// This exists because a cron that stops running emits no signal of its own. In
// the failure it was built for, launchd fired each job on time and the spawn
// aborted before agenc executed, so there was no error anywhere to catch — only
// an absence, which nothing was comparing the schedule against.
func (s *Server) runCronHealthLoop(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(cronHealthStartupDelayForEnvironment()):
		s.runCronHealthCycle(time.Now())
	}

	ticker := time.NewTicker(cronHealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runCronHealthCycle(time.Now())
		}
	}
}

// runCronHealthCycle performs one pass: gather, judge, notify about crons that
// have newly gone quiet, and forget the ones that have started running again.
func (s *Server) runCronHealthCycle(now time.Time) {
	monitored, err := s.gatherMonitoredCrons(now, mayUpdateMonitorMemory)
	if err != nil {
		s.logger.Printf("Cron health: skipping this cycle, failed to read cron state: %v", err)
		return
	}
	s.lastCronHealthCycleAt.Store(&now)

	quietCronNames := indexFindingsByCronName(cronhealth.Evaluate(collectCronStates(monitored), now))

	newlyQuiet := []cronhealth.Finding{}
	for _, cron := range monitored {
		finding, isQuiet := quietCronNames[cron.state.Name]

		if isQuiet && !cron.hasBeenReportedQuiet {
			newlyQuiet = append(newlyQuiet, finding)
			continue
		}

		if !isQuiet && cron.hasBeenReportedQuiet {
			s.logger.Printf("Cron health: '%v' is producing missions again", cron.state.Name)
			if err := s.db.SetCronQuietNotifiedAt(cron.cronID, nil); err != nil {
				s.logger.Printf("Cron health: failed to clear the quiet notice for '%v': %v", cron.state.Name, err)
			}
		}
	}

	if len(newlyQuiet) == 0 {
		return
	}

	s.reportQuietCrons(newlyQuiet, monitored, now)
}

// reportQuietCrons posts a single notification naming every cron that just went
// quiet, then records that each has been reported.
//
// One notification rather than one per cron: the outage this monitor was built
// for took six crons at once, and six separate notes would have been six times
// the noise carrying the same news.
func (s *Server) reportQuietCrons(findings []cronhealth.Finding, monitored []monitoredCron, now time.Time) {
	launchdNotes := make(map[string]string, len(findings))
	for _, finding := range findings {
		for _, cron := range monitored {
			if cron.state.Name == finding.CronName {
				launchdNotes[finding.CronName] = s.describeLaunchdJob(cron.cronID)
				break
			}
		}
	}

	notification := buildCronQuietNotification(findings, launchdNotes)
	if err := s.db.CreateNotification(notification); err != nil {
		// Leaving the crons unmarked means the next cycle tries again, which is
		// the right way round: a repeated note beats a silent one.
		s.logger.Printf("Cron health: failed to post the quiet-cron notification: %v", err)
		return
	}

	reportedNames := make([]string, 0, len(findings))
	for _, finding := range findings {
		reportedNames = append(reportedNames, finding.CronName)
		for _, cron := range monitored {
			if cron.state.Name != finding.CronName {
				continue
			}
			if err := s.db.SetCronQuietNotifiedAt(cron.cronID, &now); err != nil {
				s.logger.Printf("Cron health: failed to record that '%v' was reported quiet: %v", finding.CronName, err)
			}
			break
		}
	}
	s.logger.Printf("Cron health: reported %d quiet cron(s): %v", len(findings), strings.Join(reportedNames, ", "))
}

// Whether a caller of gatherMonitoredCrons may write to the monitor's memory.
// The background loop owns that memory; the health endpoint only reads it, so a
// user asking what the monitor currently thinks cannot change what it thinks.
const (
	mayUpdateMonitorMemory    = true
	mustNotTouchMonitorMemory = false
)

// gatherMonitoredCrons assembles the monitor's view of every enabled cron.
//
// When allowed to update the monitor's memory it also records crons it has not
// seen before and forgets ones it no longer watches. A caller that is not
// allowed to sees an unrecorded cron as first seen right now, which is the same
// conclusion the next cycle will persist.
func (s *Server) gatherMonitoredCrons(now time.Time, mayUpdateMemory bool) ([]monitoredCron, error) {
	enabledCrons := map[string]config.CronConfig{}
	for name, cronCfg := range s.getConfig().Crons {
		// A cron with no ID was never synced to launchd and has no mission
		// history to look up; the syncer skips it and logs why.
		if cronCfg.ID == "" || !cronCfg.IsEnabled() {
			continue
		}
		enabledCrons[name] = cronCfg
	}

	storedStates, err := s.db.ListCronMonitorStates()
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to read stored cron monitor state")
	}
	storedStatesByCronID := make(map[string]*database.CronMonitorState, len(storedStates))
	for _, storedState := range storedStates {
		storedStatesByCronID[storedState.CronID] = storedState
	}

	if mayUpdateMemory {
		s.forgetUnwatchedCrons(enabledCrons, storedStates)
	}

	lastMissionByCronID, err := s.readLastCronMissionTimes()
	if err != nil {
		return nil, err
	}

	monitored := make([]monitoredCron, 0, len(enabledCrons))
	for name, cronCfg := range enabledCrons {
		firstSeenAt := now
		var hasBeenReportedQuiet bool
		if storedState, isKnown := storedStatesByCronID[cronCfg.ID]; isKnown {
			firstSeenAt = storedState.FirstSeenAt
			hasBeenReportedQuiet = storedState.QuietNotifiedAt != nil
		} else if mayUpdateMemory {
			if err := s.db.RecordCronFirstSeen(cronCfg.ID, now); err != nil {
				// Without a stable reference point this cron cannot be judged
				// without risking a false report, so leave it for the next
				// cycle.
				s.logger.Printf("Cron health: skipping '%v', failed to record when it was first seen: %v", name, err)
				continue
			}
		}

		// A schedule agenc cannot parse was never handed to launchd. The
		// judgment layer treats that as its own reason for a cron being silent.
		schedule, err := launchd.ParseCronExpression(cronCfg.Schedule)
		if err != nil {
			schedule = nil
		}

		state := cronhealth.CronState{
			Name:               name,
			ScheduleExpression: cronCfg.Schedule,
			Schedule:           schedule,
			FirstSeenAt:        firstSeenAt,
		}
		if lastMissionAt, hasRun := lastMissionByCronID[cronCfg.ID]; hasRun {
			state.LastMissionAt = &lastMissionAt
		}

		monitored = append(monitored, monitoredCron{
			cronID:               cronCfg.ID,
			state:                state,
			hasBeenReportedQuiet: hasBeenReportedQuiet,
		})
	}

	sortMonitoredCronsByName(monitored)
	return monitored, nil
}

// forgetUnwatchedCrons deletes stored state for crons that have been removed or
// disabled, so that re-enabling one starts its clock fresh rather than making it
// look overdue by however long it sat disabled.
func (s *Server) forgetUnwatchedCrons(enabledCrons map[string]config.CronConfig, storedStates []*database.CronMonitorState) {
	watchedCronIDs := make(map[string]bool, len(enabledCrons))
	for _, cronCfg := range enabledCrons {
		watchedCronIDs[cronCfg.ID] = true
	}

	for _, storedState := range storedStates {
		if watchedCronIDs[storedState.CronID] {
			continue
		}
		if err := s.db.DeleteCronMonitorState(storedState.CronID); err != nil {
			s.logger.Printf("Cron health: failed to forget state for cron '%v': %v", storedState.CronID, err)
		}
	}
}

// readLastCronMissionTimes returns, per cron ID, when that cron most recently
// produced a mission. A mission carrying the cron's source ID is the evidence
// that the cron actually fired; its absence is the whole signal this monitor
// watches for.
func (s *Server) readLastCronMissionTimes() (map[string]time.Time, error) {
	source := cronMissionSource
	// Archived missions still prove the cron fired, so they count.
	missions, err := s.db.ListMissions(database.ListMissionsParams{IncludeArchived: true, Source: &source})
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list cron-created missions")
	}

	lastMissionByCronID := map[string]time.Time{}
	for _, mission := range missions {
		if mission.SourceID == nil || *mission.SourceID == "" {
			continue
		}
		previous, isKnown := lastMissionByCronID[*mission.SourceID]
		if !isKnown || mission.CreatedAt.After(previous) {
			lastMissionByCronID[*mission.SourceID] = mission.CreatedAt
		}
	}
	return lastMissionByCronID, nil
}

// describeLaunchdJob returns launchd's account of a cron's most recent spawn, or
// an empty string when it cannot be read.
//
// Strictly best-effort enrichment for a cron that has already been found quiet:
// it explains *why* a cron stopped, which is the fact that took an entire
// investigation to surface the last time this happened. It must never decide
// whether a finding is reported.
func (s *Server) describeLaunchdJob(cronID string) string {
	// The test environment deliberately never registers plists, so asking
	// launchd about them would report a failure that is really a test-env
	// property.
	if config.IsTestEnv() {
		return ""
	}

	label := launchd.CronToLabel(config.GetCronPlistPrefix(s.agencDirpath), cronID)
	status, err := launchd.NewManager().GetJobStatus(label)
	if err != nil {
		s.logger.Printf("Cron health: could not read launchd status for cron '%v': %v", cronID, err)
		return ""
	}
	return status.Describe()
}

// buildCronQuietNotification composes the note the user reads. Pure so its
// wording is testable without a database.
//
// The register is deliberately a colleague mentioning something rather than a
// monitoring system escalating: this reports that a cron has stopped happening,
// which is worth knowing and is not an emergency.
func buildCronQuietNotification(findings []cronhealth.Finding, launchdNotes map[string]string) *database.Notification {
	title := fmt.Sprintf("%d cron jobs haven't run in a while", len(findings))
	if len(findings) == 1 {
		title = fmt.Sprintf("%v hasn't run in a while", findings[0].CronName)
	}

	var body strings.Builder
	body.WriteString("Heads up — these cron jobs haven't produced a mission recently:\n\n")
	for _, finding := range findings {
		fmt.Fprintf(
			&body,
			"- **%v** — %v",
			sanitizeNotificationLine(finding.CronName),
			sanitizeNotificationLine(finding.Detail),
		)
		if launchdNote := launchdNotes[finding.CronName]; launchdNote != "" {
			body.WriteString(". ")
			body.WriteString(sanitizeNotificationLine(launchdNote))
		}
		body.WriteString("\n")
	}

	body.WriteString("\nAgenC watches for this by looking for a mission from each cron, and mentions a cron once when it goes quiet. ")
	body.WriteString("If it starts producing missions again and later goes quiet a second time, you'll get a fresh note.\n\n")
	body.WriteString("`agenc cron health` shows the current picture; `agenc cron history <name>` shows one cron's recent runs.\n")

	return &database.Notification{
		ID:           uuid.New().String(),
		Kind:         cronQuietNotificationKind,
		Title:        sanitizeNotificationLine(title),
		BodyMarkdown: body.String(),
	}
}

// collectCronStates extracts the judgeable state from the monitor's view.
func collectCronStates(monitored []monitoredCron) []cronhealth.CronState {
	states := make([]cronhealth.CronState, 0, len(monitored))
	for _, cron := range monitored {
		states = append(states, cron.state)
	}
	return states
}

// indexFindingsByCronName keys findings by cron name, which is unique because
// crons are a map in config.yml.
func indexFindingsByCronName(findings []cronhealth.Finding) map[string]cronhealth.Finding {
	byName := make(map[string]cronhealth.Finding, len(findings))
	for _, finding := range findings {
		byName[finding.CronName] = finding
	}
	return byName
}

// sortMonitoredCronsByName gives every pass a stable order, so the crons listed
// in a notification do not shuffle between cycles. Config stores crons in a map,
// whose iteration order Go deliberately randomises.
func sortMonitoredCronsByName(monitored []monitoredCron) {
	sort.Slice(monitored, func(i, j int) bool {
		return monitored[i].state.Name < monitored[j].state.Name
	})
}
