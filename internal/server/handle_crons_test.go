package server

import (
	"encoding/json"
	"log"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

func TestHandleListCrons_ReturnsCronInfo(t *testing.T) {
	enabled := true
	cfg := &config.AgencConfig{
		Crons: map[string]config.CronConfig{
			"daily-report": {
				ID:       "abc-123",
				Schedule: "0 9 * * *",
				Repo:     "mieubrisse/my-repo",
				Enabled:  &enabled,
			},
			"weekly-cleanup": {
				ID:       "def-456",
				Schedule: "0 0 * * SUN",
			},
		},
	}

	srv := &Server{logger: log.New(os.Stderr, "", 0)}
	srv.cachedConfig.Store(cfg)

	req := httptest.NewRequest("GET", "/crons", nil)
	w := httptest.NewRecorder()

	err := srv.handleListCrons(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result []CronInfo
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 crons, got %d", len(result))
	}

	// Build a map for order-independent checks
	byName := make(map[string]CronInfo)
	for _, c := range result {
		byName[c.Name] = c
	}

	daily, ok := byName["daily-report"]
	if !ok {
		t.Fatal("missing daily-report")
	}
	if daily.ID != "abc-123" {
		t.Errorf("expected ID abc-123, got %s", daily.ID)
	}
	if daily.Schedule != "0 9 * * *" {
		t.Errorf("expected schedule '0 9 * * *', got %s", daily.Schedule)
	}
	if daily.Repo != "mieubrisse/my-repo" {
		t.Errorf("expected repo mieubrisse/my-repo, got %s", daily.Repo)
	}
}

func TestHandleListCrons_EmptyConfig(t *testing.T) {
	cfg := &config.AgencConfig{}
	srv := &Server{logger: log.New(os.Stderr, "", 0)}
	srv.cachedConfig.Store(cfg)

	req := httptest.NewRequest("GET", "/crons", nil)
	w := httptest.NewRecorder()

	err := srv.handleListCrons(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result []CronInfo
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 crons, got %d", len(result))
	}
}
