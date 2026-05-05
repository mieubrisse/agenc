package database

import (
	"fmt"
	"sync"
	"testing"
)

func TestUpsertPauseAndNotification_FirstCallInserts(t *testing.T) {
	db := openTestDB(t)

	n := &Notification{
		ID:           "11111111-0000-0000-0000-000000000001",
		Kind:         "writeable_copy.conflict",
		SourceRepo:   "github.com/owner/repo",
		Title:        "Conflict",
		BodyMarkdown: "body",
	}
	p := &WriteableCopyPause{
		RepoName:         "github.com/owner/repo",
		PausedReason:     "rebase_conflict",
		LocalHeadAtPause: "abc123",
		NotificationID:   n.ID,
	}

	inserted, err := db.UpsertPauseAndNotification(p, n)
	if err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}
	if !inserted {
		t.Errorf("expected first upsert to return inserted=true")
	}

	gotPause, err := db.GetPause(p.RepoName)
	if err != nil {
		t.Fatal(err)
	}
	if gotPause == nil || gotPause.NotificationID != n.ID {
		t.Errorf("expected pause linked to notification, got %+v", gotPause)
	}

	gotNotif, err := db.GetNotification(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotNotif.Title != n.Title {
		t.Errorf("notification title mismatch: %v", gotNotif.Title)
	}
}

func TestUpsertPauseAndNotification_SecondCallIsNoOp(t *testing.T) {
	db := openTestDB(t)

	n1 := &Notification{ID: "11111111-0000-0000-0000-000000000001", Kind: "k", Title: "first", BodyMarkdown: "x"}
	p1 := &WriteableCopyPause{RepoName: "github.com/o/r", PausedReason: "rebase_conflict", LocalHeadAtPause: "abc", NotificationID: n1.ID}
	if _, err := db.UpsertPauseAndNotification(p1, n1); err != nil {
		t.Fatal(err)
	}

	n2 := &Notification{ID: "22222222-0000-0000-0000-000000000002", Kind: "k", Title: "second", BodyMarkdown: "y"}
	p2 := &WriteableCopyPause{RepoName: p1.RepoName, PausedReason: "auth_failure", LocalHeadAtPause: "def", NotificationID: n2.ID}
	inserted, err := db.UpsertPauseAndNotification(p2, n2)
	if err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}
	if inserted {
		t.Errorf("expected second upsert to be a no-op")
	}

	list, err := db.ListNotifications(ListNotificationsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("expected exactly one notification (no second insert), got %d", len(list))
	}
}

func TestUpsertPauseAndNotification_ConcurrentAtomic(t *testing.T) {
	db := openTestDB(t)

	const goroutines = 8
	var wg sync.WaitGroup
	insertCounts := make([]int, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			n := &Notification{
				ID:           fmt.Sprintf("aaaaaaaa-0000-0000-0000-00000000000%d", idx),
				Kind:         "k",
				Title:        "t",
				BodyMarkdown: "b",
			}
			p := &WriteableCopyPause{
				RepoName:         "github.com/o/r",
				PausedReason:     "rebase_conflict",
				LocalHeadAtPause: "h",
				NotificationID:   n.ID,
			}
			inserted, err := db.UpsertPauseAndNotification(p, n)
			if err != nil {
				t.Errorf("goroutine %d failed: %v", idx, err)
				return
			}
			if inserted {
				insertCounts[idx] = 1
			}
		}(i)
	}
	wg.Wait()

	totalInserts := 0
	for _, c := range insertCounts {
		totalInserts += c
	}
	if totalInserts != 1 {
		t.Errorf("expected exactly one successful insert across %d goroutines, got %d", goroutines, totalInserts)
	}

	list, _ := db.ListNotifications(ListNotificationsParams{})
	if len(list) != 1 {
		t.Errorf("expected exactly one notification after concurrent calls, got %d", len(list))
	}
}

func TestDeletePause_LeavesNotification(t *testing.T) {
	db := openTestDB(t)

	n := &Notification{ID: "33333333-0000-0000-0000-000000000003", Kind: "k", Title: "x", BodyMarkdown: "x"}
	p := &WriteableCopyPause{RepoName: "github.com/o/r", PausedReason: "x", LocalHeadAtPause: "h", NotificationID: n.ID}
	if _, err := db.UpsertPauseAndNotification(p, n); err != nil {
		t.Fatal(err)
	}

	if err := db.DeletePause(p.RepoName); err != nil {
		t.Fatal(err)
	}

	if got, _ := db.GetPause(p.RepoName); got != nil {
		t.Errorf("expected pause cleared, got %+v", got)
	}

	notif, err := db.GetNotification(n.ID)
	if err != nil {
		t.Fatalf("notification should remain after pause deletion: %v", err)
	}
	if notif == nil {
		t.Error("notification should not have been deleted")
	}
}

func TestDeletePause_Idempotent(t *testing.T) {
	db := openTestDB(t)
	if err := db.DeletePause("github.com/never/existed"); err != nil {
		t.Errorf("DeletePause on non-existent repo should be no-op, got %v", err)
	}
}

func TestListPauses(t *testing.T) {
	db := openTestDB(t)
	pauses, err := db.ListPauses()
	if err != nil {
		t.Fatal(err)
	}
	if len(pauses) != 0 {
		t.Errorf("expected empty list, got %d", len(pauses))
	}

	for i := range 3 {
		n := &Notification{
			ID:           fmt.Sprintf("bbbbbbbb-0000-0000-0000-00000000000%d", i),
			Kind:         "k",
			Title:        "t",
			BodyMarkdown: "b",
		}
		p := &WriteableCopyPause{
			RepoName:         fmt.Sprintf("github.com/o/r%d", i),
			PausedReason:     "rebase_conflict",
			LocalHeadAtPause: "h",
			NotificationID:   n.ID,
		}
		if _, err := db.UpsertPauseAndNotification(p, n); err != nil {
			t.Fatal(err)
		}
	}

	pauses, err = db.ListPauses()
	if err != nil {
		t.Fatal(err)
	}
	if len(pauses) != 3 {
		t.Errorf("expected 3 pauses, got %d", len(pauses))
	}
}
