package merge

import (
	"encoding/json"
	"maps"
	"os"

	"github.com/kurtosis-tech/stacktrace"
)

// MergeCLAUDEMD concatenates the global CLAUDE.md and agent-template CLAUDE.md,
// separated by a newline. If either file doesn't exist, only the other is returned.
func MergeCLAUDEMD(globalFilepath string, agentFilepath string) (string, error) {
	globalContent, globalErr := readFileIfExists(globalFilepath)
	agentContent, agentErr := readFileIfExists(agentFilepath)

	if globalErr != nil {
		return "", stacktrace.Propagate(globalErr, "failed to read global CLAUDE.md")
	}
	if agentErr != nil {
		return "", stacktrace.Propagate(agentErr, "failed to read agent CLAUDE.md")
	}

	if globalContent == "" && agentContent == "" {
		return "", nil
	}
	if globalContent == "" {
		return agentContent, nil
	}
	if agentContent == "" {
		return globalContent, nil
	}
	return globalContent + "\n\n" + agentContent, nil
}

// MergeSettingsJSON deep-merges two JSON settings files. The agent file's
// values override the global file's values. Returns the merged JSON bytes.
func MergeSettingsJSON(globalFilepath string, agentFilepath string) ([]byte, error) {
	return mergeJSONFiles(globalFilepath, agentFilepath)
}

// MergeMCPJSON deep-merges two MCP JSON config files. The agent file's
// values override the global file's values. Returns the merged JSON bytes.
func MergeMCPJSON(globalFilepath string, agentFilepath string) ([]byte, error) {
	return mergeJSONFiles(globalFilepath, agentFilepath)
}

func mergeJSONFiles(globalFilepath string, agentFilepath string) ([]byte, error) {
	globalContent, globalErr := readFileIfExists(globalFilepath)
	agentContent, agentErr := readFileIfExists(agentFilepath)

	if globalErr != nil {
		return nil, stacktrace.Propagate(globalErr, "failed to read global JSON file '%s'", globalFilepath)
	}
	if agentErr != nil {
		return nil, stacktrace.Propagate(agentErr, "failed to read agent JSON file '%s'", agentFilepath)
	}

	if globalContent == "" && agentContent == "" {
		return nil, nil
	}

	var globalMap map[string]any
	if globalContent != "" {
		if err := json.Unmarshal([]byte(globalContent), &globalMap); err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse global JSON file '%s'", globalFilepath)
		}
	} else {
		globalMap = make(map[string]any)
	}

	var agentMap map[string]any
	if agentContent != "" {
		if err := json.Unmarshal([]byte(agentContent), &agentMap); err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse agent JSON file '%s'", agentFilepath)
		}
	}

	merged := deepMerge(globalMap, agentMap)
	result, err := json.MarshalIndent(merged, "", "    ")
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal merged JSON")
	}
	return result, nil
}

// deepMerge recursively merges the override map into the base map.
// Override values take precedence. Both maps are merged at the key level;
// nested maps are merged recursively, while non-map values are replaced.
func deepMerge(base map[string]any, override map[string]any) map[string]any {
	result := make(map[string]any)
	maps.Copy(result, base)

	for k, overrideVal := range override {
		baseVal, exists := result[k]
		if !exists {
			result[k] = overrideVal
			continue
		}

		baseMap, baseIsMap := baseVal.(map[string]any)
		overrideMap, overrideIsMap := overrideVal.(map[string]any)

		if baseIsMap && overrideIsMap {
			result[k] = deepMerge(baseMap, overrideMap)
		} else {
			result[k] = overrideVal
		}
	}

	return result
}

func readFileIfExists(filepath string) (string, error) {
	content, err := os.ReadFile(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(content), nil
}
