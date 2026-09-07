package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/crondoctor"
	"github.com/odyssey/agenc/internal/launchd"
	"github.com/odyssey/agenc/internal/server"
)

// cronMissionSourceName is the mission source tag the cron scheduler stamps on
// the missions it spawns.
const cronMissionSourceName = "cron"

const cronDoctorRepairFlagName = "repair"
const cronDoctorNotifyFlagName = "notify"

// cronDoctorNotificationKind tags the notifications this command posts so they
// can be recognised for de-duplication and filtered by the picker.
const cronDoctorNotificationKind = "cron.health"

// cronDoctorFindingsExitCode is returned when the audit found something wrong.
// A scheduled invocation can key an alert off this without parsing output.
const cronDoctorFindingsExitCode = 2

var cronDoctorCmd = &cobra.Command{
	Use:   doctorCmdStr,
	Short: "Audit scheduled cron jobs for silent failure",
	Long: `Audit every enabled cron job and report the ways it can silently stop delivering.

A cron loop that dies emits no signal on its own. This command checks the three
places a failure hides:

  - launchd has no job registered, so the cron can never fire
  - launchd fires on schedule but the process dies before AgenC runs, writing
    nothing to the cron log and creating no mission
  - the mission starts and dies before doing any work, leaving a run record
    that looks exactly like a healthy one

Exits ` + fmt.Sprintf("%d", cronDoctorFindingsExitCode) + ` when findings exist, 0 when everything is delivering.

Examples:
  agenc cron doctor
  agenc cron doctor --repair --notify
`,
	Args: cobra.NoArgs,
	RunE: runCronDoctor,
}

func init() {
	cronDoctorCmd.Flags().Bool(cronDoctorRepairFlagName, false, "reload launchd jobs whose registration has gone bad")
	cronDoctorCmd.Flags().Bool(cronDoctorNotifyFlagName, false, "post an AgenC notification when findings exist")
	cronCmd.AddCommand(cronDoctorCmd)
}

func runCronDoctor(cmd *cobra.Command, args []string) error {
	shouldRepair, _ := cmd.Flags().GetBool(cronDoctorRepairFlagName)
	shouldNotify, _ := cmd.Flags().GetBool(cronDoctorNotifyFlagName)

	cfg, err := readConfig()
	if err != nil {
		return err
	}

	client, err := serverClient()
	if err != nil {
		return err
	}

	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to determine the agenc directory")
	}

	states, err := gatherCronStates(cfg, client, agencDirpath)
	if err != nil {
		return err
	}

	findings := crondoctor.Diagnose(states, time.Now())

	if shouldRepair {
		repaired, err := repairBrokenCronJobs(findings, cfg, agencDirpath)
		if err != nil {
			return err
		}
		if len(repaired) > 0 {
			fmt.Printf("Reloaded %d launchd job(s): %v\n\n", len(repaired), strings.Join(repaired, ", "))
		}
	}

	printCronDoctorReport(findings, len(states))

	if len(findings) == 0 {
		return nil
	}

	if shouldNotify {
		if err := postCronDoctorNotification(client, findings); err != nil {
			return err
		}
	}

	os.Exit(cronDoctorFindingsExitCode)
	return nil
}

// gatherCronStates collects, for every enabled cron, the facts the doctor needs
// to judge it: what launchd thinks of the job, when the cron last produced a
// mission, and whether that mission finished its work.
func gatherCronStates(
	cfg *config.AgencConfig,
	client *server.Client,
	agencDirpath string,
) ([]crondoctor.CronState, error) {
	cronPlistPrefix := config.GetCronPlistPrefix(agencDirpath)
	manager := launchd.NewManager()

	// Sort so the report reads the same way twice in a row.
	cronNames := make([]string, 0, len(cfg.Crons))
	for name := range cfg.Crons {
		cronNames = append(cronNames, name)
	}
	sort.Strings(cronNames)

	states := make([]crondoctor.CronState, 0, len(cronNames))
	for _, name := range cronNames {
		cronCfg := cfg.Crons[name]
		if !cronCfg.IsEnabled() {
			continue
		}
		if cronCfg.ID == "" {
			// Without an ID the syncer never created a plist for this cron, and
			// the cron cannot be correlated to its missions either.
			continue
		}

		state := crondoctor.CronState{Name: name}

		label := launchd.CronToLabel(cronPlistPrefix, cronCfg.ID)
		jobStatus, err := manager.GetJobStatus(label)
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to read launchd status for cron '%v'", name)
		}
		state.JobStatus = jobStatus

		// A schedule launchd could not parse was never scheduled at all, so
		// leaving Schedule nil correctly skips the missed-run check.
		if interval, err := launchd.ParseCronExpression(cronCfg.Schedule); err == nil {
			state.Schedule = interval
		}

		lastRun, err := findMostRecentCronRun(client, cronCfg.ID)
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to read run history for cron '%v'", name)
		}
		if lastRun != nil {
			runCreatedAt := lastRun.CreatedAt
			state.LastRunAt = &runCreatedAt

			wrapperLogFilepath := filepath.Join(
				config.GetMissionDirpath(agencDirpath, lastRun.ID),
				config.WrapperLogFilename,
			)
			completion, err := crondoctor.InspectRunCompletion(wrapperLogFilepath)
			if err != nil {
				return nil, stacktrace.Propagate(
					err,
					"failed to judge completion of cron '%v' run '%v'",
					name,
					lastRun.ID,
				)
			}
			if !completion.StillRunning {
				didWork := completion.DidWork
				state.LastRunDidWork = &didWork
			}
		}

		states = append(states, state)
	}

	return states, nil
}

// findMostRecentCronRun returns the newest mission this cron spawned, or nil
// when it has never run.
func findMostRecentCronRun(client *server.Client, cronID string) (*missionRunRecord, error) {
	missions, err := client.ListMissions(server.ListMissionsRequest{
		IncludeArchived: true,
		Source:          cronMissionSourceName,
		SourceID:        cronID,
	})
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list missions for cron id '%v'", cronID)
	}
	if len(missions) == 0 {
		return nil, nil
	}

	newest := missions[0]
	for _, mission := range missions {
		if mission.CreatedAt.After(newest.CreatedAt) {
			newest = mission
		}
	}

	return &missionRunRecord{ID: newest.ID, CreatedAt: newest.CreatedAt}, nil
}

// missionRunRecord is the slice of a mission the doctor cares about.
type missionRunRecord struct {
	ID        string
	CreatedAt time.Time
}

// repairBrokenCronJobs reloads the launchd registration for every cron whose
// job cannot spawn, and returns the names it repaired.
//
// Reloading is safe to do unconditionally: it rebuilds the job from the plist
// already on disk and changes no configuration.
func repairBrokenCronJobs(
	findings []crondoctor.Finding,
	cfg *config.AgencConfig,
	agencDirpath string,
) ([]string, error) {
	plistDirpath, err := launchd.PlistDirpath()
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to locate the launchd plist directory")
	}
	cronPlistPrefix := config.GetCronPlistPrefix(agencDirpath)
	manager := launchd.NewManager()

	repaired := []string{}
	for _, finding := range findings {
		if finding.Kind != crondoctor.FindingJobNotLoaded && finding.Kind != crondoctor.FindingJobSpawnFailed {
			continue
		}

		cronCfg, cronExists := cfg.Crons[finding.CronName]
		if !cronExists || cronCfg.ID == "" {
			continue
		}

		label := launchd.CronToLabel(cronPlistPrefix, cronCfg.ID)
		plistFilepath := filepath.Join(plistDirpath, launchd.CronToPlistFilename(cronPlistPrefix, cronCfg.ID))

		if _, err := os.Stat(plistFilepath); err != nil {
			// No plist to reload from. The config watcher writes these, so this
			// is a different failure than a bad registration and reporting it
			// is more useful than a confusing reload error.
			fmt.Printf(
				"Cannot repair '%v': no plist at '%v' — the server writes these when config changes\n",
				finding.CronName,
				plistFilepath,
			)
			continue
		}

		if err := manager.ReloadJob(label, plistFilepath); err != nil {
			return nil, stacktrace.Propagate(err, "failed to reload launchd job for cron '%v'", finding.CronName)
		}
		repaired = append(repaired, finding.CronName)
	}

	return repaired, nil
}

// printCronDoctorReport writes the audit result to stdout.
func printCronDoctorReport(findings []crondoctor.Finding, cronsChecked int) {
	if len(findings) == 0 {
		fmt.Printf("All %d enabled cron job(s) are delivering.\n", cronsChecked)
		return
	}

	fmt.Printf("%d finding(s) across %d enabled cron job(s):\n\n", len(findings), cronsChecked)
	for _, finding := range findings {
		fmt.Printf("  %v\n", finding.String())
	}
	fmt.Println()
}

// buildCronDoctorNotificationBody renders findings as the markdown body of a
// notification.
func buildCronDoctorNotificationBody(findings []crondoctor.Finding) string {
	var builder strings.Builder
	builder.WriteString("AgenC's cron health audit found scheduled work that is not being delivered.\n\n")
	for _, finding := range findings {
		builder.WriteString(fmt.Sprintf("- **%v** (%v): %v\n", finding.CronName, finding.Severity, finding.Detail))
	}
	builder.WriteString("\nRun `agenc cron doctor` for the current state, or `agenc cron doctor --repair` to reload broken launchd jobs.\n")
	return builder.String()
}

// buildCronDoctorNotificationTitle summarises the findings in one line.
func buildCronDoctorNotificationTitle(findings []crondoctor.Finding) string {
	affectedCrons := map[string]bool{}
	for _, finding := range findings {
		affectedCrons[finding.CronName] = true
	}
	if len(affectedCrons) == 1 {
		return fmt.Sprintf("Cron not delivering: %v", findings[0].CronName)
	}
	return fmt.Sprintf("%d crons not delivering", len(affectedCrons))
}

// postCronDoctorNotification posts the findings, unless an unread notification
// with the same body is already waiting.
//
// The audit runs on a schedule, so without this check a cron that stays broken
// overnight would bury every other notification under identical copies — and a
// channel that cries wolf is a channel that stops being read.
func postCronDoctorNotification(client *server.Client, findings []crondoctor.Finding) error {
	body := buildCronDoctorNotificationBody(findings)

	const onlyUnread = true
	const anySourceRepo = ""
	existing, err := client.ListNotifications(onlyUnread, anySourceRepo, cronDoctorNotificationKind)
	if err != nil {
		return stacktrace.Propagate(err, "failed to check for an existing cron health notification")
	}
	for _, notification := range existing {
		if notification.BodyMarkdown == body {
			fmt.Println("Findings already posted in an unread notification; not posting a duplicate.")
			return nil
		}
	}

	_, err = client.CreateNotification(server.CreateNotificationRequest{
		Kind:         cronDoctorNotificationKind,
		Title:        buildCronDoctorNotificationTitle(findings),
		BodyMarkdown: body,
	})
	if err != nil {
		return stacktrace.Propagate(err, "failed to post the cron health notification")
	}

	fmt.Println("Posted an AgenC notification with these findings.")
	return nil
}
