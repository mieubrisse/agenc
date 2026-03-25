package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestGoldenCLIOutput(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		goldenFilename string
	}{
		{
			name:           "root help",
			args:           []string{"--help"},
			goldenFilename: "root_help.txt",
		},
		{
			name:           "config get help",
			args:           []string{"config", "get", "--help"},
			goldenFilename: "config_get_help.txt",
		},
		{
			name:           "repo help",
			args:           []string{"repo", "--help"},
			goldenFilename: "repo_help.txt",
		},
		{
			name:           "mission help",
			args:           []string{"mission", "--help"},
			goldenFilename: "mission_help.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)
			rootCmd.SetArgs(tt.args)
			defer func() {
				rootCmd.SetOut(nil)
				rootCmd.SetErr(nil)
				rootCmd.SetArgs(nil)
			}()

			err := rootCmd.Execute()
			if err != nil {
				t.Fatalf("command %v failed: %v", tt.args, err)
			}

			got := buf.String()
			goldenFilepath := filepath.Join("testdata", "golden", tt.goldenFilename)

			if os.Getenv("UPDATE_GOLDEN") == "1" {
				err := os.MkdirAll(filepath.Dir(goldenFilepath), 0755)
				if err != nil {
					t.Fatalf("failed to create golden directory: %v", err)
				}
				err = os.WriteFile(goldenFilepath, []byte(got), 0644)
				if err != nil {
					t.Fatalf("failed to write golden file: %v", err)
				}
				return
			}

			expected, err := os.ReadFile(goldenFilepath)
			if err != nil {
				t.Fatalf("golden file %s not found — run with UPDATE_GOLDEN=1 to create", goldenFilepath)
			}

			if got != string(expected) {
				t.Errorf("output does not match golden file %s\n\n--- expected ---\n%s\n--- got ---\n%s",
					goldenFilepath, string(expected), got)
			}
		})
	}
}
