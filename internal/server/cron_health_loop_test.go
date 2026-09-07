package server

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/cronhealth"
	"github.com/odyssey/agenc/internal/database"
)

// newCronHealthTestServer constructs a *Server with a real database and an empty
// config, suitable for exercising the cron health cycle end to end.
func newCronHealthTestServer(t *testing.T) *Server {
	t.Helper()

	tmpDir := t.TempDir()
	db, err := database.Open(filepath.Join(tmpDir, "database.sqlite"))
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	srv := &Server{
		agencDirpath: tmpDir,
		logger:       log.New(os.Stderr, "", 0),
		db:           db,
	}
	srv.cachedConfig.Store(&config.AgencConfig{})
	return srv
}

// setCrons replaces the server's cached cron configuration.
func setCrons(srv *Server, crons map[string]config.CronConfig) {
	srv.cachedConfig.Store(&config.AgencConfig{Crons: crons})
}

// dailyCron builds an enabled cron firing once a day at 02:00.
func dailyCron(cronID string) config.CronConfig {
	return config.CronConfig{ID: cronID, Schedule: "0 2 * * *", Prompt: "do the thing"}
}

// countQuietNotifications returns how many quiet-cron notifications have been
// posted, and the most recent one.
func countQuietNotifications(t *testing.T, srv *Server) (int, *database.Notification) {
	t.Helper()
	notifications, err := srv.db.ListNotifications(database.ListNotificationsParams{Kind: cronQuietNotificationKind})
	if err != nil {
		t.Fatalf("failed to list notifications: %v", err)
	}
	if len(notifications) == 0 {
		return 0, nil
	}
	return len(notifications), notifications[0]
}

// recordCronMission inserts a mission attributed to a cron, which is the
// evidence that the cron actually fired.
func recordCronMission(t *testing.T, srv *Server, cronID string) {
	t.Helper()
	source := cronMissionSource
	if _, err := srv.db.CreateMission("", &database.CreateMissionParams{Source: &source, SourceID: &cronID}); err != nil {
		t.Fatalf("failed to create cron mission: %v", err)
	}
}

// recordHandTriggeredCronMission inserts the mission `agenc cron run` writes: the
// same source as a scheduled run, but tagged as hand-triggered.
func recordHandTriggeredCronMission(t *testing.T, srv *Server, cronID string) {
	t.Helper()
	source := cronMissionSource
	sourceMetadata := `{"cron_name":"hn-daily-pull","trigger":"` + ManualCronTrigger + `"}`
	if _, err := srv.db.CreateMission("", &database.CreateMissionParams{
		Source:         &source,
		SourceID:       &cronID,
		SourceMetadata: &sourceMetadata,
	}); err != nil {
		t.Fatalf("failed to create hand-triggered cron mission: %v", err)
	}
}

func TestCronHealthCycleTreatsAHandTriggeredRunAsNoProofTheScheduleFired(t *testing.T) {
	srv := newCronHealthTestServer(t)
	setCrons(srv, map[string]config.CronConfig{"hn-daily-pull": dailyCron("cron-hn")})

	now := time.Now()
	srv.runCronHealthCycle(now.Add(-72 * time.Hour))

	// Three days on, the user notices something is stale and triggers the cron
	// by hand to catch up. launchd is still not firing it, so this run is no
	// evidence the schedule recovered and must not restart the quiet clock.
	recordHandTriggeredCronMission(t, srv, "cron-hn")

	srv.runCronHealthCycle(now)

	count, notification := countQuietNotifications(t, srv)
	if count != 1 {
		t.Fatalf("expected the cron to still be reported quiet after a hand-triggered run, got %d notifications", count)
	}
	if !strings.Contains(notification.Title, "hn-daily-pull") {
		t.Errorf("expected the title to name the cron, got '%v'", notification.Title)
	}
}

func TestCronHealthCycleKeepsItsMemoryWhenNoConfigHasLoaded(t *testing.T) {
	srv := newCronHealthTestServer(t)
	setCrons(srv, map[string]config.CronConfig{"hn-daily-pull": dailyCron("cron-hn")})

	now := time.Now()
	srv.runCronHealthCycle(now)

	// The server came up while config.yml was unreadable, so nothing was ever
	// cached. "No crons configured" must not be read as "the user deleted them
	// all" -- doing so forgets every cron's clock and buys a dead one another
	// two cadences of silence.
	srv.cachedConfig.Store(nil)
	srv.runCronHealthCycle(now.Add(time.Minute))

	states, err := srv.db.ListCronMonitorStates()
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].CronID != "cron-hn" {
		t.Fatalf("expected the cron's first-seen clock to survive a cycle with no config loaded, got %+v", states)
	}
}

func TestCronHealthCycleSaysNothingAboutACronItHasJustMet(t *testing.T) {
	srv := newCronHealthTestServer(t)
	setCrons(srv, map[string]config.CronConfig{"fresh": dailyCron("cron-fresh")})

	now := time.Now()
	srv.runCronHealthCycle(now)

	if count, _ := countQuietNotifications(t, srv); count != 0 {
		t.Fatalf("expected no notification on first sight of a cron, got %d", count)
	}

	states, err := srv.db.ListCronMonitorStates()
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].CronID != "cron-fresh" {
		t.Fatalf("expected the cron to be recorded as first seen, got %+v", states)
	}
}

func TestCronHealthCycleReportsACronThatGoesQuietExactlyOnce(t *testing.T) {
	srv := newCronHealthTestServer(t)
	setCrons(srv, map[string]config.CronConfig{"hn-daily-pull": dailyCron("cron-hn")})

	now := time.Now()
	srv.runCronHealthCycle(now)

	// Three days on with no mission, a daily cron has skipped three cycles.
	srv.runCronHealthCycle(now.Add(72 * time.Hour))

	count, notification := countQuietNotifications(t, srv)
	if count != 1 {
		t.Fatalf("expected exactly 1 notification, got %d", count)
	}
	if !strings.Contains(notification.Title, "hn-daily-pull") {
		t.Errorf("expected the title to name the cron, got '%v'", notification.Title)
	}
	if !strings.Contains(notification.BodyMarkdown, "Heads up") {
		t.Errorf("expected an informational opening, got '%v'", notification.BodyMarkdown)
	}

	// Still quiet a day later: the user has already been told, so say nothing.
	srv.runCronHealthCycle(now.Add(96 * time.Hour))
	if count, _ := countQuietNotifications(t, srv); count != 1 {
		t.Fatalf("expected the cron to be mentioned once, got %d notifications", count)
	}
}

func TestCronHealthCycleMentionsACronAgainAfterItRecoversAndRelapses(t *testing.T) {
	srv := newCronHealthTestServer(t)
	setCrons(srv, map[string]config.CronConfig{"hn-daily-pull": dailyCron("cron-hn")})

	// Missions are stamped with the real clock, so the timeline is anchored in
	// the past and walked forward to the present in order for a mission created
	// during the test to read as recent.
	now := time.Now()
	firstSeenAt := now.Add(-200 * time.Hour)

	srv.runCronHealthCycle(firstSeenAt)
	srv.runCronHealthCycle(firstSeenAt.Add(72 * time.Hour))
	if count, _ := countQuietNotifications(t, srv); count != 1 {
		t.Fatalf("expected the first quiet spell to be reported, got %d notifications", count)
	}

	// The cron produces a mission again; the monitor should forget the spell.
	recordCronMission(t, srv, "cron-hn")
	srv.runCronHealthCycle(now)

	states, err := srv.db.ListCronMonitorStates()
	if err != nil {
		t.Fatal(err)
	}
	if states[0].QuietNotifiedAt != nil {
		t.Fatalf("expected the quiet notice to be cleared on recovery, got %v", states[0].QuietNotifiedAt)
	}

	// It goes quiet a second time, which earns a fresh mention.
	srv.runCronHealthCycle(now.Add(72 * time.Hour))
	if count, _ := countQuietNotifications(t, srv); count != 2 {
		t.Fatalf("expected a relapse to earn a second notification, got %d", count)
	}
}

func TestCronHealthCycleSaysNothingWhileACronKeepsProducingMissions(t *testing.T) {
	srv := newCronHealthTestServer(t)
	setCrons(srv, map[string]config.CronConfig{"healthy": dailyCron("cron-healthy")})

	// Watched for three days, and it has just produced a mission.
	now := time.Now()
	srv.runCronHealthCycle(now.Add(-72 * time.Hour))
	recordCronMission(t, srv, "cron-healthy")
	srv.runCronHealthCycle(now)

	if count, _ := countQuietNotifications(t, srv); count != 0 {
		t.Fatalf("expected no notification for a cron that has run, got %d", count)
	}
}

func TestCronHealthCyclePutsEveryQuietCronInOneNotification(t *testing.T) {
	// The outage this monitor was built for took six crons at once. Six separate
	// notes would have been six times the noise carrying the same news.
	srv := newCronHealthTestServer(t)
	setCrons(srv, map[string]config.CronConfig{
		"first":  dailyCron("cron-first"),
		"second": dailyCron("cron-second"),
		"third":  dailyCron("cron-third"),
	})

	now := time.Now()
	srv.runCronHealthCycle(now)
	srv.runCronHealthCycle(now.Add(72 * time.Hour))

	count, notification := countQuietNotifications(t, srv)
	if count != 1 {
		t.Fatalf("expected a single notification covering all three, got %d", count)
	}
	for _, cronName := range []string{"first", "second", "third"} {
		if !strings.Contains(notification.BodyMarkdown, cronName) {
			t.Errorf("expected the body to name '%v', got '%v'", cronName, notification.BodyMarkdown)
		}
	}
	if !strings.Contains(notification.Title, "3 cron jobs") {
		t.Errorf("expected a plural title, got '%v'", notification.Title)
	}
}

func TestCronHealthCycleIgnoresDisabledCronsAndForgetsThem(t *testing.T) {
	srv := newCronHealthTestServer(t)
	setCrons(srv, map[string]config.CronConfig{"toggled": dailyCron("cron-toggled")})

	now := time.Now()
	srv.runCronHealthCycle(now)

	disabled := dailyCron("cron-toggled")
	isDisabled := false
	disabled.Enabled = &isDisabled
	setCrons(srv, map[string]config.CronConfig{"toggled": disabled})
	srv.runCronHealthCycle(now.Add(72 * time.Hour))

	if count, _ := countQuietNotifications(t, srv); count != 0 {
		t.Fatalf("expected a disabled cron to be left alone, got %d notifications", count)
	}
	states, err := srv.db.ListCronMonitorStates()
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 0 {
		t.Fatalf("expected a disabled cron to be forgotten, got %+v", states)
	}
}

func TestCronHealthCycleStartsAReEnabledCronsClockFresh(t *testing.T) {
	// Forgetting a disabled cron is what stops it from looking months overdue
	// the moment it is switched back on.
	srv := newCronHealthTestServer(t)
	enabled := map[string]config.CronConfig{"toggled": dailyCron("cron-toggled")}
	disabledCron := dailyCron("cron-toggled")
	isDisabled := false
	disabledCron.Enabled = &isDisabled

	now := time.Now()
	setCrons(srv, enabled)
	srv.runCronHealthCycle(now)

	setCrons(srv, map[string]config.CronConfig{"toggled": disabledCron})
	srv.runCronHealthCycle(now.Add(72 * time.Hour))

	setCrons(srv, enabled)
	srv.runCronHealthCycle(now.Add(73 * time.Hour))

	if count, _ := countQuietNotifications(t, srv); count != 0 {
		t.Fatalf("expected a just-re-enabled cron to be left alone, got %d notifications", count)
	}
}

func TestBuildCronQuietNotificationNamesASingleCronInTheTitle(t *testing.T) {
	findings := []cronhealth.Finding{{
		CronName:           "hn-daily-pull",
		ScheduleExpression: "0 2 * * *",
		Detail:             "last produced a mission on 2026-09-01 02:00 (6 days ago)",
		HasEverRun:         true,
	}}

	notification := buildCronQuietNotification(findings, nil)
	if notification.Title != "hn-daily-pull hasn't run in a while" {
		t.Errorf("unexpected title: '%v'", notification.Title)
	}
	if notification.Kind != cronQuietNotificationKind {
		t.Errorf("unexpected kind: '%v'", notification.Kind)
	}
	// The note is the only thing this monitor produces, so it has to carry the
	// next step itself rather than assume the reader knows where to look.
	if !strings.Contains(notification.BodyMarkdown, "agenc cron history") {
		t.Errorf("expected the body to point at the run history command, got '%v'", notification.BodyMarkdown)
	}
}

func TestBuildCronQuietNotificationCarriesTheLaunchdExplanation(t *testing.T) {
	// launchd's account of the last spawn is the fact that took an entire
	// investigation to surface the last time crons stopped firing.
	findings := []cronhealth.Finding{{
		CronName: "hn-daily-pull",
		Detail:   "last produced a mission on 2026-09-01 02:00 (6 days ago)",
	}}
	launchdNotes := map[string]string{"hn-daily-pull": "launchd's last spawn of it exited 78"}

	notification := buildCronQuietNotification(findings, launchdNotes)
	if !strings.Contains(notification.BodyMarkdown, "exited 78") {
		t.Errorf("expected the launchd status in the body, got '%v'", notification.BodyMarkdown)
	}
}

func TestBuildCronQuietNotificationStripsControlSequencesFromConfigSourcedText(t *testing.T) {
	// Cron names and schedules come from user-edited config, and AgenC command
	// output must never carry ANSI escapes.
	findings := []cronhealth.Finding{{
		CronName: "\x1b[31mred-cron\x1b[0m",
		Detail:   "last produced a mission \x1b[1mages\x1b[0m ago",
	}}

	notification := buildCronQuietNotification(findings, nil)
	if strings.Contains(notification.Title, "\x1b") || strings.Contains(notification.BodyMarkdown, "\x1b") {
		t.Errorf("expected ANSI escapes to be stripped, got title '%q' body '%q'", notification.Title, notification.BodyMarkdown)
	}
}
