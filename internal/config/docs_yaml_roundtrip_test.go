package config

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// configurationDocRelFilepath is docs/configuration.md relative to this package
// directory. That doc is the hand-written reference for config.yml, so its
// examples are the ones most likely to drift away from the schema.
const configurationDocRelFilepath = "../../docs/configuration.md"

const (
	yamlFenceOpen  = "```yaml"
	yamlFenceClose = "```"
)

// TestConfigurationDocYAMLBlocksMatchSchema parses every ```yaml block in
// docs/configuration.md against the real config schema with unknown fields
// rejected. A documented key the schema does not have is exactly the drift that
// sends agents chasing configuration that was never implemented, and nothing
// else in the build would notice it.
func TestConfigurationDocYAMLBlocksMatchSchema(t *testing.T) {
	docContent, err := os.ReadFile(configurationDocRelFilepath)
	if err != nil {
		t.Fatalf("Couldn't read the configuration reference doc at '%v': %v", configurationDocRelFilepath, err)
	}

	blocks := extractFencedYAMLBlocks(string(docContent))
	if len(blocks) == 0 {
		t.Fatalf(
			"Found no %v blocks in '%v'; either the doc lost its config examples or the fence convention changed, and this test is now inspecting nothing",
			yamlFenceOpen,
			configurationDocRelFilepath,
		)
	}

	for _, block := range blocks {
		t.Run("line "+strconv.Itoa(block.startLineNum), func(t *testing.T) {
			requireYAMLMatchesConfigSchema(t, block.content)
		})
	}
}

// fencedYAMLBlock is one ```yaml block, carrying the line it started on so a
// failure points at the spot in the doc that needs editing.
type fencedYAMLBlock struct {
	startLineNum int
	content      string
}

func extractFencedYAMLBlocks(markdown string) []fencedYAMLBlock {
	var blocks []fencedYAMLBlock

	lines := strings.Split(markdown, "\n")
	isInsideBlock := false
	var currentBlockLines []string
	currentBlockStartLineNum := 0

	for lineIdx, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		if !isInsideBlock {
			if trimmedLine == yamlFenceOpen {
				isInsideBlock = true
				currentBlockLines = nil
				// Human-facing line numbers are 1-indexed, and the block's
				// content starts on the line after the opening fence.
				currentBlockStartLineNum = lineIdx + 2
			}
			continue
		}

		if trimmedLine == yamlFenceClose {
			isInsideBlock = false
			blocks = append(blocks, fencedYAMLBlock{
				startLineNum: currentBlockStartLineNum,
				content:      strings.Join(currentBlockLines, "\n"),
			})
			continue
		}

		currentBlockLines = append(currentBlockLines, line)
	}

	return blocks
}

// requireYAMLMatchesConfigSchema parses the snippet as a config.yml document
// with unknown fields rejected. Snippets that are entirely comments carry no
// keys to check, so they pass trivially.
func requireYAMLMatchesConfigSchema(t *testing.T, yamlSnippet string) {
	t.Helper()

	var cfg AgencConfig
	if err := yaml.UnmarshalWithOptions([]byte(yamlSnippet), &cfg, yaml.Strict()); err != nil {
		t.Errorf(
			"This config.yml example doesn't match the schema in agenc_config.go: %v\nFix the example, or add the key to the schema if the example describes a real feature.\n--- example ---\n%v\n--- end example ---",
			err,
			yamlSnippet,
		)
		return
	}

	// Matching the schema only proves the example's keys exist. A schedule the
	// CLI would reject still reads as a working example, so the values that have
	// a validator get run through it. Examples that omit the schedule are
	// documenting some other key and are left alone.
	for cronName, cronConfig := range cfg.Crons {
		if cronConfig.Schedule == "" {
			continue
		}
		if err := ValidateCronSchedule(cronConfig.Schedule); err != nil {
			t.Errorf(
				"The '%v' cron in this config.yml example is scheduled '%v', which agenc rejects: %v",
				cronName,
				cronConfig.Schedule,
				err,
			)
		}
	}
}
