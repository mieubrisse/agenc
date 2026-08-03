package claudeconfig

import (
	"encoding/json"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
)

// mergeAgencSandbox ensures the AgenC server socket is included in the
// sandbox.network.allowUnixSockets list so agents can reach the server.
func mergeAgencSandbox(settings map[string]json.RawMessage, agencDirpath string) error {
	serverSocketFilepath := config.GetServerSocketFilepath(agencDirpath)

	var sandboxMap map[string]json.RawMessage
	if existing, ok := settings["sandbox"]; ok {
		if err := json.Unmarshal(existing, &sandboxMap); err != nil {
			return stacktrace.Propagate(err, "failed to parse existing sandbox object")
		}
	} else {
		sandboxMap = make(map[string]json.RawMessage)
	}

	var networkMap map[string]json.RawMessage
	if existing, ok := sandboxMap["network"]; ok {
		if err := json.Unmarshal(existing, &networkMap); err != nil {
			return stacktrace.Propagate(err, "failed to parse existing sandbox.network object")
		}
	} else {
		networkMap = make(map[string]json.RawMessage)
	}

	var sockets []string
	if existing, ok := networkMap["allowUnixSockets"]; ok {
		if err := json.Unmarshal(existing, &sockets); err != nil {
			return stacktrace.Propagate(err, "failed to parse existing allowUnixSockets array")
		}
	}

	isAlreadyPresent := false
	for _, s := range sockets {
		if s == serverSocketFilepath {
			isAlreadyPresent = true
			break
		}
	}
	if !isAlreadyPresent {
		sockets = append(sockets, serverSocketFilepath)
	}

	socketsBytes, err := json.Marshal(sockets)
	if err != nil {
		return stacktrace.Propagate(err, "failed to marshal allowUnixSockets")
	}
	networkMap["allowUnixSockets"] = json.RawMessage(socketsBytes)

	networkBytes, err := json.Marshal(networkMap)
	if err != nil {
		return stacktrace.Propagate(err, "failed to marshal sandbox.network")
	}
	sandboxMap["network"] = json.RawMessage(networkBytes)

	sandboxBytes, err := json.Marshal(sandboxMap)
	if err != nil {
		return stacktrace.Propagate(err, "failed to marshal sandbox")
	}
	settings["sandbox"] = json.RawMessage(sandboxBytes)

	return nil
}
