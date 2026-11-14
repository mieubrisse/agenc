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

// When a repo's failure turns into a different failure, the one row follows it
// and a fresh notification is posted — escalation on change, where repetition
// writes nothing.
func TestReplacePauseAndNotification_RepointsTheRowAndNotifies(t *testing.T) {
	db := openTestDB(t)

	first := &Notification{ID: "33333333-0000-0000-0000-000000000009", Kind: "k", Title: "x", BodyMarkdown: "x"}
	p := &WriteableCopyPause{RepoName: "github.com/o/r", PausedReason: "auth_failure", LocalHeadAtPause: "h1", NotificationID: first.ID}
	if _, err := db.UpsertPauseAndNotification(p, first); err != nil {
		t.Fatal(err)
	}

	second := &Notification{ID: "33333333-0000-0000-0000-00000000000a", Kind: "k2", Title: "y", BodyMarkdown: "y"}
	replaced := &WriteableCopyPause{RepoName: p.RepoName, PausedReason: "push_refused", LocalHeadAtPause: "h2", NotificationID: second.ID}
	ok, err := db.ReplacePauseAndNotification(replaced, second)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected the replacement to find the existing row")
	}

	got, err := db.GetPause(p.RepoName)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected the pause to still exist")
	}
	if got.PausedReason != "push_refused" {
		t.Errorf("expected the reason to follow the new failure, got '%s'", got.PausedReason)
	}
	if got.NotificationID != second.ID {
		t.Errorf("expected the row to point at the new notification, got '%s'", got.NotificationID)
	}

	pauses, err := db.ListPauses()
	if err != nil {
		t.Fatal(err)
	}
	if len(pauses) != 1 {
		t.Errorf("expected exactly one pause row, got %d", len(pauses))
	}
	for _, id := range []string{first.ID, second.ID} {
		notification, err := db.GetNotification(id)
		if err != nil || notification == nil {
			t.Errorf("expected notification '%s' to survive, got %v (err %v)", id, notification, err)
		}
	}
}

// Nothing to repoint means nothing is written, so the caller can insert.
func TestReplacePauseAndNotification_NoRowIsNotAnError(t *testing.T) {
	db := openTestDB(t)

	n := &Notification{ID: "33333333-0000-0000-0000-00000000000b", Kind: "k", Title: "x", BodyMarkdown: "x"}
	p := &WriteableCopyPause{RepoName: "github.com/o/absent", PausedReason: "auth_failure", LocalHeadAtPause: "h", NotificationID: n.ID}

	ok, err := db.ReplacePauseAndNotification(p, n)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected false when there is no row to replace")
	}
	if notification, _ := db.GetNotification(n.ID); notification != nil {
		t.Error("expected no notification to be written when there was no row")
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
