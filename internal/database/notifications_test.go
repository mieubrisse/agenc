package database

import (
	"fmt"
	"testing"
	"time"
)

func TestCreateAndGetNotification(t *testing.T) {
	db := openTestDB(t)

	n := &Notification{
		ID:           "11111111-2222-3333-4444-555555555555",
		Kind:         "writeable_copy.conflict",
		SourceRepo:   "github.com/owner/repo",
		Title:        "Test conflict",
		BodyMarkdown: "# Hello\n\nbody content",
	}
	if err := db.CreateNotification(n); err != nil {
		t.Fatalf("CreateNotification failed: %v", err)
	}

	got, err := db.GetNotification(n.ID)
	if err != nil {
		t.Fatalf("GetNotification failed: %v", err)
	}
	if got.ID != n.ID {
		t.Errorf("ID mismatch: want '%v' got '%v'", n.ID, got.ID)
	}
	if got.Kind != n.Kind {
		t.Errorf("Kind mismatch: want '%v' got '%v'", n.Kind, got.Kind)
	}
	if got.SourceRepo != n.SourceRepo {
		t.Errorf("SourceRepo mismatch: want '%v' got '%v'", n.SourceRepo, got.SourceRepo)
	}
	if got.Title != n.Title {
		t.Errorf("Title mismatch: want '%v' got '%v'", n.Title, got.Title)
	}
	if got.BodyMarkdown != n.BodyMarkdown {
		t.Errorf("BodyMarkdown mismatch: want '%v' got '%v'", n.BodyMarkdown, got.BodyMarkdown)
	}
	if got.ReadAt != nil {
		t.Errorf("expected ReadAt nil, got %v", got.ReadAt)
	}
	if got.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestCreateNotification_NoSourceRepo(t *testing.T) {
	db := openTestDB(t)

	n := &Notification{
		ID:           "aaaaaaaa-0000-0000-0000-000000000001",
		Kind:         "custom.agent_finding",
		Title:        "No repo",
		BodyMarkdown: "x",
	}
	if err := db.CreateNotification(n); err != nil {
		t.Fatalf("CreateNotification failed: %v", err)
	}
	got, err := db.GetNotification(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceRepo != "" {
		t.Errorf("expected empty SourceRepo, got '%v'", got.SourceRepo)
	}
}

func TestListNotifications_UnreadOnly(t *testing.T) {
	db := openTestDB(t)

	unread := &Notification{ID: "aaaaaaaa-0000-0000-0000-000000000001", Kind: "k", Title: "u", BodyMarkdown: "u"}
	read := &Notification{ID: "bbbbbbbb-0000-0000-0000-000000000002", Kind: "k", Title: "r", BodyMarkdown: "r"}
	if err := db.CreateNotification(unread); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateNotification(read); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkNotificationRead(read.ID); err != nil {
		t.Fatal(err)
	}

	list, err := db.ListNotifications(ListNotificationsParams{UnreadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 unread notification, got %d", len(list))
	}
	if list[0].ID != unread.ID {
		t.Errorf("expected unread notification, got %v", list[0].ID)
	}
}

func TestListNotifications_FilterByRepoAndKind(t *testing.T) {
	db := openTestDB(t)

	a := &Notification{ID: "aaaaaaaa-0000-0000-0000-000000000010", Kind: "writeable_copy.conflict", SourceRepo: "github.com/o/a", Title: "a", BodyMarkdown: "a"}
	b := &Notification{ID: "bbbbbbbb-0000-0000-0000-000000000020", Kind: "writeable_copy.conflict", SourceRepo: "github.com/o/b", Title: "b", BodyMarkdown: "b"}
	c := &Notification{ID: "cccccccc-0000-0000-0000-000000000030", Kind: "custom.agent_finding", SourceRepo: "github.com/o/a", Title: "c", BodyMarkdown: "c"}
	for _, n := range []*Notification{a, b, c} {
		if err := db.CreateNotification(n); err != nil {
			t.Fatal(err)
		}
	}

	repoOnly, err := db.ListNotifications(ListNotificationsParams{SourceRepo: "github.com/o/a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(repoOnly) != 2 {
		t.Errorf("expected 2 results filtering by repo, got %d", len(repoOnly))
	}

	kindOnly, err := db.ListNotifications(ListNotificationsParams{Kind: "writeable_copy.conflict"})
	if err != nil {
		t.Fatal(err)
	}
	if len(kindOnly) != 2 {
		t.Errorf("expected 2 results filtering by kind, got %d", len(kindOnly))
	}

	combined, err := db.ListNotifications(ListNotificationsParams{SourceRepo: "github.com/o/a", Kind: "writeable_copy.conflict"})
	if err != nil {
		t.Fatal(err)
	}
	if len(combined) != 1 || combined[0].ID != a.ID {
		t.Errorf("expected only 'a', got %+v", combined)
	}
}

func TestMarkNotificationRead_Idempotent(t *testing.T) {
	db := openTestDB(t)
	n := &Notification{ID: "cccccccc-0000-0000-0000-000000000003", Kind: "k", Title: "x", BodyMarkdown: "x"}
	if err := db.CreateNotification(n); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkNotificationRead(n.ID); err != nil {
		t.Fatalf("first mark failed: %v", err)
	}
	first, err := db.GetNotification(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.ReadAt == nil {
		t.Fatal("expected ReadAt set after first mark")
	}
	firstReadAt := *first.ReadAt

	// Sleep > 1s because timestamps are RFC3339 (second precision)
	time.Sleep(1100 * time.Millisecond)

	if err := db.MarkNotificationRead(n.ID); err != nil {
		t.Fatalf("second mark failed: %v", err)
	}
	second, err := db.GetNotification(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.ReadAt == nil {
		t.Fatal("expected ReadAt still set")
	}
	if !firstReadAt.Equal(*second.ReadAt) {
		t.Errorf("read_at should be unchanged on idempotent re-mark; first=%v second=%v", firstReadAt, *second.ReadAt)
	}
}

func TestCountUnreadNotifications(t *testing.T) {
	db := openTestDB(t)
	count, err := db.CountUnreadNotifications()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0 unread on empty DB, got %d", count)
	}

	for i := range 3 {
		n := &Notification{
			ID:           fmt.Sprintf("dddddddd-0000-0000-0000-00000000000%d", i),
			Kind:         "k",
			Title:        "x",
			BodyMarkdown: "x",
		}
		if err := db.CreateNotification(n); err != nil {
			t.Fatal(err)
		}
	}
	count, err = db.CountUnreadNotifications()
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("expected 3 unread, got %d", count)
	}
}
