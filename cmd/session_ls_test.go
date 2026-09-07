package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/odyssey/agenc/internal/database"
)

func TestPrintMissionSessionsDescribesEachTranscript(t *testing.T) {
	withAgents := sessionWithAgentAndWorkflow(t)
	forked := writeFakeSession(t, []string{
		`{"type":"user","uuid":"f1","timestamp":"2026-02-02T00:00:00.000Z","forkedFrom":{"sessionId":"aaaaaaaa-1111-2222-3333-444444444444","messageUuid":"x"},"message":{"role":"user","content":"forked"}}`,
		`{"type":"system","subtype":"compact_boundary","uuid":"f2","timestamp":"2026-02-02T00:01:00.000Z","compactMetadata":{"trigger":"auto","preTokens":1,"postTokens":1}}`,
	}, nil)
	paths := map[string]string{"11111111-2222-3333-4444-555555555555": withAgents, "22222222-0000-0000-0000-000000000000": forked}
	lookup := func(id string) (string, error) {
		if p, ok := paths[id]; ok {
			return p, nil
		}
		return "", errors.New("no transcript on disk")
	}
	sessions := []*database.Session{
		{ID: "11111111-2222-3333-4444-555555555555", UpdatedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), CustomTitle: "with agents"},
		{ID: "22222222-0000-0000-0000-000000000000", UpdatedAt: time.Date(2026, 2, 2, 12, 0, 0, 0, time.UTC)},
		{ID: "33333333-0000-0000-0000-000000000000", UpdatedAt: time.Date(2026, 3, 3, 12, 0, 0, 0, time.UTC), AgencCustomTitle: "missing"},
	}

	var stdout, stderr bytes.Buffer
	if err := printMissionSessions(sessions, lookup, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()

	if !strings.HasPrefix(out, "UPDATED") || !strings.Contains(strings.SplitN(out, "\n", 2)[0], "FORKED-FROM") {
		t.Errorf("header = %q", strings.SplitN(out, "\n", 2)[0])
	}
	// Whitespace-split tokens: UPDATED(2) SESSION STARTED(2) MSGS TOOLS ERR
	// COMPACT AGENTS "(N" "wf)" FORKED-FROM SIZE(2) TITLE...
	row := rowContaining(t, out, "11111111")
	if row[5] != "4" || row[6] != "2" || row[7] != "0" || row[8] != "0" || row[9] != "3" || row[10] != "(1" || row[11] != "wf)" || row[12] != "-" {
		t.Errorf("with-agents row = %v", row)
	}
	row = rowContaining(t, out, "22222222")
	if row[8] != "1" || row[9] != "0" || row[10] != "aaaaaaaa" {
		t.Errorf("forked row = %v", row)
	}
	row = rowContaining(t, out, "33333333")
	if row[3] != "-" || row[len(row)-1] != "missing" {
		t.Errorf("missing-transcript row must still list with dashes and its title, got %v", row)
	}
	if !strings.Contains(stderr.String(), "warning: session 33333333: no transcript on disk") {
		t.Errorf("stderr = %q", stderr.String())
	}
}
