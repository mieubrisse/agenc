package devcontainer

import "strings"

// SessionBindMount contains the host and container project directory names
// for the session files bind mount.
type SessionBindMount struct {
	// HostProjectDirName is the encoded directory name as it appears on the host
	// under ~/.claude/projects/
	HostProjectDirName string

	// ContainerProjectDirName is the encoded directory name as Claude inside
	// the container will write to under ~/.claude/projects/
	ContainerProjectDirName string
}

// EncodeProjectPath encodes a directory path the same way Claude Code does:
// replace "/" and "." with "-".
func EncodeProjectPath(dirpath string) string {
	return strings.ReplaceAll(strings.ReplaceAll(dirpath, "/", "-"), ".", "-")
}

// ComputeSessionBindMount computes the host and container project directory
// names needed to bind-mount the session files so they land at the correct
// host location despite Claude running inside a container with a different
// workspace path.
func ComputeSessionBindMount(hostAgentDirpath string, containerWorkspacePath string) SessionBindMount {
	return SessionBindMount{
		HostProjectDirName:      EncodeProjectPath(hostAgentDirpath),
		ContainerProjectDirName: EncodeProjectPath(containerWorkspacePath),
	}
}
