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
