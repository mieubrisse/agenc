package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/tmux"
)

// The palette's contract with tmux is a set of generated shell strings: the
// keybindings file that opens the palette and fires each command, and the
// run-shell command the palette dispatches on selection. A refactor that claims
// to leave them byte-identical (PR #55 swapped a literal env var name for its
// constant) could previously be verified only by a human pressing keys, because
// a popup cannot be driven from outside (KEV-123). These goldens pin the strings
// themselves, so such a refactor proves itself under `make check`.
//
// Regenerate after an intended change with:
//
//	UPDATE_GOLDEN=1 go test ./cmd -run TestPaletteGolden
//
// and review the diff of testdata/golden/ as the change's real effect.

const (
	paletteGoldenCallingPaneID      = "%42"
	paletteGoldenCallingSessionName = "work"
	paletteGoldenCallingMissionUUID = "11111111-2222-3333-4444-555555555555"

	// paletteGoldenDirpathPlaceholder replaces the per-run temp AGENC_DIRPATH
	// so the golden is stable across machines and runs.
	paletteGoldenDirpathPlaceholder = "<AGENC_DIRPATH>"

	// tmux 3.4 is past the 3.2 display-popup gate, so every keybinding is emitted.
	paletteGoldenTmuxMajor = 3
	paletteGoldenTmuxMinor = 4
)

// TestPaletteGolden_DispatchCommands pins buildPaletteDispatchCommand for every
// builtin palette command, under each calling context the palette meets: opened
// from a mission pane, opened from a plain pane, opened in a test env, and
// opened from a default install, whose AGENC_DIRPATH is never exported.
func TestPaletteGolden_DispatchCommands(t *testing.T) {
	scenarios := []struct {
		name               string
		goldenFilename     string
		isDefaultDirpath   bool
		isTestEnv          bool
		callingPaneID      string
		callingSessionName string
		callingMissionUUID string
	}{
		{
			name:               "from a default install",
			goldenFilename:     "palette_dispatch_default_install.txt",
			isDefaultDirpath:   true,
			callingPaneID:      paletteGoldenCallingPaneID,
			callingSessionName: paletteGoldenCallingSessionName,
			callingMissionUUID: paletteGoldenCallingMissionUUID,
		},
		{
			// A test-env flag on a default install must not export anything:
			// the AGENC_TEST_ENV export sits inside the namespace gate.
			name:               "from a default install with the test-env flag set",
			goldenFilename:     "palette_dispatch_default_install_test_env.txt",
			isDefaultDirpath:   true,
			isTestEnv:          true,
			callingPaneID:      paletteGoldenCallingPaneID,
			callingSessionName: paletteGoldenCallingSessionName,
		},
		{
			// Pane and session are exported independently; set only one so a
			// change coupling them shows up.
			name:           "with a calling pane but no session",
			goldenFilename: "palette_dispatch_pane_without_session.txt",
			callingPaneID:  paletteGoldenCallingPaneID,
		},
		{
			name:               "from a mission pane",
			goldenFilename:     "palette_dispatch_mission_pane.txt",
			callingPaneID:      paletteGoldenCallingPaneID,
			callingSessionName: paletteGoldenCallingSessionName,
			callingMissionUUID: paletteGoldenCallingMissionUUID,
		},
		{
			name:               "from a plain pane",
			goldenFilename:     "palette_dispatch_plain_pane.txt",
			callingPaneID:      paletteGoldenCallingPaneID,
			callingSessionName: paletteGoldenCallingSessionName,
		},
		{
			name:               "with no calling context",
			goldenFilename:     "palette_dispatch_no_calling_context.txt",
			callingMissionUUID: paletteGoldenCallingMissionUUID,
		},
		{
			name:               "in a test env",
			goldenFilename:     "palette_dispatch_test_env.txt",
			isTestEnv:          true,
			callingPaneID:      paletteGoldenCallingPaneID,
			callingSessionName: paletteGoldenCallingSessionName,
			callingMissionUUID: paletteGoldenCallingMissionUUID,
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			agencDirpath := t.TempDir()
			if scenario.isDefaultDirpath {
				// A default install is $HOME/.agenc, so point HOME at a temp
				// dir rather than touching the real one.
				homeDirpath := t.TempDir()
				t.Setenv("HOME", homeDirpath)
				agencDirpath = filepath.Join(homeDirpath, ".agenc")
			}
			t.Setenv(config.AgencDirpathEnvVarName(), agencDirpath)
			t.Setenv(config.CallingPaneIDEnvVar, scenario.callingPaneID)
			t.Setenv(config.CallingSessionNameEnvVar, scenario.callingSessionName)
			// The UUID reaches buildPaletteDispatchCommand as an argument, not
			// from the environment; clear the env var so it cannot leak in.
			t.Setenv(config.CallingMissionUUIDEnvVar, "")
			testEnvValue := ""
			if scenario.isTestEnv {
				testEnvValue = "1"
			}
			t.Setenv(config.TestEnvVar, testEnvValue)

			var got strings.Builder
			for _, entry := range (&config.AgencConfig{}).GetResolvedPaletteCommands() {
				fmt.Fprintf(&got, "[%s]\n%s\n\n", entry.Name, buildPaletteDispatchCommand(entry, scenario.callingMissionUUID))
			}

			assertPaletteGolden(t, scenario.goldenFilename, strings.ReplaceAll(got.String(), agencDirpath, paletteGoldenDirpathPlaceholder))
		})
	}
}

// TestPaletteGolden_KeybindingsFile pins the whole generated keybindings file
// for the builtin palette commands. Its unit tests in internal/tmux assert
// fragments; this catches any byte of drift outside those fragments.
func TestPaletteGolden_KeybindingsFile(t *testing.T) {
	cfg := &config.AgencConfig{}
	logFilepath := config.GetPaletteLogFilepath(paletteGoldenDirpathPlaceholder)
	got := tmux.GenerateKeybindingsContent(
		paletteGoldenTmuxMajor,
		paletteGoldenTmuxMinor,
		cfg.GetPaletteTmuxKeybinding(),
		tmux.BuildKeybindingsFromCommands(cfg.GetResolvedPaletteCommands()),
		logFilepath,
	)
	assertPaletteGolden(t, "palette_keybindings.txt", got)
}

func assertPaletteGolden(t *testing.T, goldenFilename string, got string) {
	t.Helper()
	goldenFilepath := filepath.Join("testdata", "golden", goldenFilename)

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenFilepath, []byte(got), 0644); err != nil {
			t.Fatalf("failed to write golden file '%s': %v", goldenFilepath, err)
		}
		return
	}

	expected, err := os.ReadFile(goldenFilepath)
	if err != nil {
		t.Fatalf("golden file '%s' not readable (run with UPDATE_GOLDEN=1 to create it): %v", goldenFilepath, err)
	}
	if got != string(expected) {
		t.Errorf("palette strings drifted from golden file '%s'\n\n--- expected ---\n%s\n--- got ---\n%s",
			goldenFilepath, string(expected), got)
	}
}
