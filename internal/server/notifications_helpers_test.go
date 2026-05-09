package server

import (
	"strings"
	"testing"
)

func TestSanitizeNotificationTitle_StripsControlChars(t *testing.T) {
	got := sanitizeNotificationTitle("hello\nworld\ttabbed\rcr")
	want := "hello world tabbed cr"
	if got != want {
		t.Fatalf("got '%v' want '%v'", got, want)
	}
}

func TestSanitizeNotificationTitle_StripsANSI(t *testing.T) {
	got := sanitizeNotificationTitle("\x1b[31mred\x1b[0m text")
	want := "red text"
	if got != want {
		t.Fatalf("got '%v' want '%v'", got, want)
	}
}

func TestSanitizeNotificationTitle_TruncatesLongInput(t *testing.T) {
	in := strings.Repeat("a", 500)
	got := sanitizeNotificationTitle(in)
	if len([]rune(got)) != notificationTitleMaxRunes {
		t.Fatalf("expected %d runes, got %d", notificationTitleMaxRunes, len([]rune(got)))
	}
}

func TestSanitizeNotificationTitle_PassesThroughNormal(t *testing.T) {
	got := sanitizeNotificationTitle("Cron triggered: daily-review")
	want := "Cron triggered: daily-review"
	if got != want {
		t.Fatalf("got '%v' want '%v'", got, want)
	}
}
