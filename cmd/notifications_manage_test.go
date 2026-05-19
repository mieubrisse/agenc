package cmd

import (
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/server"
)

func TestBuildNotificationsManageFzfInput_PrefixesShortID(t *testing.T) {
	notifs := []server.NotificationResponse{
		{ID: "11111111-2222-3333-4444-555555555555", Kind: "cron.triggered", Title: "first", CreatedAt: "2026-05-09T10:00:00Z", MissionID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"},
		{ID: "22222222-3333-4444-5555-666666666666", Kind: "test", Title: "second", CreatedAt: "2026-05-09T09:00:00Z"},
	}
	got := buildNotificationsManageFzfInput(notifs)

	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (header + 2 rows), got %d:\n%v", len(lines), got)
	}

	if !strings.HasPrefix(lines[0], "HEADER\t") {
		t.Errorf("header line should start with 'HEADER\\t', got '%v'", lines[0])
	}
	if !strings.HasPrefix(lines[1], "11111111\t") {
		t.Errorf("row 1 should start with short ID '11111111\\t', got '%v'", lines[1])
	}
	if !strings.HasPrefix(lines[2], "22222222\t") {
		t.Errorf("row 2 should start with short ID '22222222\\t', got '%v'", lines[2])
	}
}

func TestBuildNotificationsManageFzfInput_SortsUnreadFirstWithReadColumn(t *testing.T) {
	notifs := []server.NotificationResponse{
		{ID: "11111111-aaaa-aaaa-aaaa-aaaaaaaaaaaa", Kind: "test", Title: "read-newer", CreatedAt: "2026-05-09T12:00:00Z", ReadAt: "2026-05-09T13:00:00Z"},
		{ID: "22222222-bbbb-bbbb-bbbb-bbbbbbbbbbbb", Kind: "test", Title: "unread-older", CreatedAt: "2026-05-09T10:00:00Z"},
		{ID: "33333333-cccc-cccc-cccc-cccccccccccc", Kind: "test", Title: "read-older", CreatedAt: "2026-05-09T09:00:00Z", ReadAt: "2026-05-09T11:00:00Z"},
		{ID: "44444444-dddd-dddd-dddd-dddddddddddd", Kind: "test", Title: "unread-newer", CreatedAt: "2026-05-09T11:00:00Z"},
	}
	got := buildNotificationsManageFzfInput(notifs)

	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines (header + 4 rows), got %d:\n%v", len(lines), got)
	}

	// Unread rows (caller-order preserved within group) come before read rows.
	if !strings.HasPrefix(lines[1], "22222222\t") {
		t.Errorf("row 1 should be first unread '22222222\\t', got '%v'", lines[1])
	}
	if !strings.HasPrefix(lines[2], "44444444\t") {
		t.Errorf("row 2 should be second unread '44444444\\t', got '%v'", lines[2])
	}
	if !strings.HasPrefix(lines[3], "11111111\t") {
		t.Errorf("row 3 should be first read '11111111\\t', got '%v'", lines[3])
	}
	if !strings.HasPrefix(lines[4], "33333333\t") {
		t.Errorf("row 4 should be second read '33333333\\t', got '%v'", lines[4])
	}

	// READ column shows the unread marker on unread rows only.
	if !strings.Contains(lines[1], notificationsManageUnreadMarker) {
		t.Errorf("unread row 1 should contain marker '%v', got '%v'", notificationsManageUnreadMarker, lines[1])
	}
	if !strings.Contains(lines[2], notificationsManageUnreadMarker) {
		t.Errorf("unread row 2 should contain marker '%v', got '%v'", notificationsManageUnreadMarker, lines[2])
	}
	if strings.Contains(lines[3], notificationsManageUnreadMarker) {
		t.Errorf("read row 3 should NOT contain marker, got '%v'", lines[3])
	}
	if strings.Contains(lines[4], notificationsManageUnreadMarker) {
		t.Errorf("read row 4 should NOT contain marker, got '%v'", lines[4])
	}
}

func TestFormatReadCell(t *testing.T) {
	if got := formatReadCell(""); got != notificationsManageUnreadMarker {
		t.Errorf("unread should render marker '%v', got '%v'", notificationsManageUnreadMarker, got)
	}
	if got := formatReadCell("2026-05-09T12:00:00Z"); got != "" {
		t.Errorf("read should render blank, got '%v'", got)
	}
}

func TestFormatMissionCell_EmptyShowsPlaceholder(t *testing.T) {
	got := formatMissionCell("")
	if got != notificationsManageMissionMissingPlaceholder {
		t.Errorf("expected placeholder '%v', got '%v'", notificationsManageMissionMissingPlaceholder, got)
	}
}

func TestFormatMissionCell_NonEmptyColored(t *testing.T) {
	got := formatMissionCell("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	if !strings.Contains(got, "aaaaaaaa") {
		t.Errorf("expected mission short ID in cell, got '%v'", got)
	}
	if !strings.Contains(got, missionIDColorANSI) || !strings.Contains(got, missionIDResetANSI) {
		t.Errorf("expected ANSI color wrapping, got '%v'", got)
	}
}
