package cmd

import (
	"reflect"
	"testing"

	"github.com/odyssey/agenc/internal/mission"
)

// The --env flag must be registered as a string ARRAY, not a string slice.
// pflag's slice variant splits every value on commas, which would silently cut
// a value that legitimately contains one — a list, a JSON fragment, a sentence
// — into several environment variables, most of them malformed. "stringArray"
// versus "stringSlice" is the only externally visible difference between the
// two registrations, so it is what this pins.
func TestMissionNewEnvFlagIsAStringArray(t *testing.T) {
	envFlag := missionNewCmd.Flags().Lookup(envFlagName)
	if envFlag == nil {
		t.Fatalf("expected `mission new` to register a --%s flag", envFlagName)
	}
	const wantType = "stringArray"
	if gotType := envFlag.Value.Type(); gotType != wantType {
		t.Fatalf("expected the --%s flag to be of type %q, got %q", envFlagName, wantType, gotType)
	}
}

// The real Cobra parse, end to end into the map that reaches the mission
// record: a repeated flag accumulates, a value containing a comma stays one
// value, and a value containing an "=" keeps it.
//
// It parses once rather than per-case because pflag's string-array value
// accumulates across parses of the same registered flag, and the flag here is
// the real package-level one.
func TestMissionNewEnvFlagParsesIntoTheMissionEnvMap(t *testing.T) {
	originalAssignments := missionEnvAssignmentFlagValues
	t.Cleanup(func() { missionEnvAssignmentFlagValues = originalAssignments })

	if err := missionNewCmd.Flags().Parse([]string{
		"--" + envFlagName, "REFLECTION_SESSION=1",
		"--" + envFlagName, "LIST=a,b,c",
		"--" + envFlagName, "QUERY=x=1&y=2",
	}); err != nil {
		t.Fatalf("failed to parse the --%s flags: %v", envFlagName, err)
	}

	parsedMissionEnv, err := mission.ParseMissionEnvAssignments(missionEnvAssignmentFlagValues)
	if err != nil {
		t.Fatalf("failed to read the parsed --%s flags: %v", envFlagName, err)
	}

	expectedMissionEnv := map[string]string{
		"REFLECTION_SESSION": "1",
		"LIST":               "a,b,c",
		"QUERY":              "x=1&y=2",
	}
	if !reflect.DeepEqual(parsedMissionEnv, expectedMissionEnv) {
		t.Fatalf("expected mission environment %v, got %v", expectedMissionEnv, parsedMissionEnv)
	}
}
