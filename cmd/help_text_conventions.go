package cmd

// configYAMLExampleMarker introduces an inline config.yml example in a command's
// help text. It is a shared constant rather than prose repeated in each help
// string because the tests in help_text_drift_test.go locate examples by this
// exact line and parse each one against the real config schema. A help text that
// introduces its example any other way is silently never checked, so interpolate
// this constant instead of retyping the words.
const configYAMLExampleMarker = "Example config.yml:"
