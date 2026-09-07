package database

import (
	"testing"
	"time"
)

func TestRecordCronFirstSeenKeepsTheOriginalObservation(t *testing.T) {
	db := openTestDB(t)

	firstObservation := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if err := db.RecordCronFirstSeen("cron-a", firstObservation); err != nil {
		t.Fatalf("first record failed: %v", err)
	}

	// Every cycle calls this for every enabled cron; only the first may count,
	// or the reference point would advance forever and the cron could never
	// look quiet.
	laterObservation := firstObservation.Add(48 * time.Hour)
	if err := db.RecordCronFirstSeen("cron-a", laterObservation); err != nil {
		t.Fatalf("repeat record failed: %v", err)
	}

	states, err := db.ListCronMonitorStates()
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("expected 1 state row, got %d", len(states))
	}
	if !states[0].FirstSeenAt.Equal(firstObservation) {
		t.Errorf("expected first-seen to stay %v, got %v", firstObservation, states[0].FirstSeenAt)
	}
	if states[0].QuietNotifiedAt != nil {
		t.Errorf("expected a newly recorded cron to have no quiet notice, got %v", states[0].QuietNotifiedAt)
	}
}

func TestSetCronQuietNotifiedAtRoundTripsAndClears(t *testing.T) {
	db := openTestDB(t)

	if err := db.RecordCronFirstSeen("cron-a", time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	notifiedAt := time.Date(2026, 9, 5, 8, 30, 0, 0, time.UTC)
	if err := db.SetCronQuietNotifiedAt("cron-a", &notifiedAt); err != nil {
		t.Fatalf("setting quiet notice failed: %v", err)
	}

	states, err := db.ListCronMonitorStates()
	if err != nil {
		t.Fatal(err)
	}
	if states[0].QuietNotifiedAt == nil || !states[0].QuietNotifiedAt.Equal(notifiedAt) {
		t.Fatalf("expected quiet notice at %v, got %v", notifiedAt, states[0].QuietNotifiedAt)
	}

	// Clearing is what lets a cron that recovers earn a fresh note if it goes
	// quiet again later.
	if err := db.SetCronQuietNotifiedAt("cron-a", nil); err != nil {
		t.Fatalf("clearing quiet notice failed: %v", err)
	}

	states, err = db.ListCronMonitorStates()
	if err != nil {
		t.Fatal(err)
	}
	if states[0].QuietNotifiedAt != nil {
		t.Errorf("expected quiet notice to be cleared, got %v", states[0].QuietNotifiedAt)
	}
}

func TestDeleteCronMonitorStateForgetsOnlyThatCron(t *testing.T) {
	db := openTestDB(t)

	seenAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if err := db.RecordCronFirstSeen("cron-a", seenAt); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordCronFirstSeen("cron-b", seenAt); err != nil {
		t.Fatal(err)
	}

	if err := db.DeleteCronMonitorState("cron-a"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	states, err := db.ListCronMonitorStates()
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].CronID != "cron-b" {
		t.Fatalf("expected only cron-b to survive, got %+v", states)
	}
}

func TestListCronMonitorStatesIsEmptyBeforeAnyCronIsSeen(t *testing.T) {
	db := openTestDB(t)

	states, err := db.ListCronMonitorStates()
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 0 {
		t.Fatalf("expected no state rows, got %d", len(states))
	}
}
