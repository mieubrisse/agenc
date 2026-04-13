package devcontainer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodeProjectPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "simple workspace path",
			path:     "/workspaces/my-repo",
			expected: "-workspaces-my-repo",
		},
		{
			name:     "host mission agent path",
			path:     "/Users/odyssey/.agenc/missions/abc-123/agent",
			expected: "-Users-odyssey--agenc-missions-abc-123-agent",
		},
		{
			name:     "path with dots",
			path:     "/home/user/.config/app",
			expected: "-home-user--config-app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EncodeProjectPath(tt.path)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestComputeSessionBindMount(t *testing.T) {
	mount := ComputeSessionBindMount(
		"/Users/odyssey/.agenc/missions/abc-123/agent",
		"/workspaces/my-repo",
	)

	require.Equal(t, "-Users-odyssey--agenc-missions-abc-123-agent", mount.HostProjectDirName)
	require.Equal(t, "-workspaces-my-repo", mount.ContainerProjectDirName)
}
