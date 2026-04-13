package devcontainer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectDevcontainer_InDotDevcontainerDir(t *testing.T) {
	dir := t.TempDir()
	devcontainerDir := filepath.Join(dir, ".devcontainer")
	require.NoError(t, os.MkdirAll(devcontainerDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(`{"image":"ubuntu"}`), 0644))

	path, found := DetectDevcontainer(dir)
	require.True(t, found, "expected to find devcontainer.json")
	require.Equal(t, filepath.Join(devcontainerDir, "devcontainer.json"), path)
}

func TestDetectDevcontainer_RootFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".devcontainer.json"), []byte(`{"image":"ubuntu"}`), 0644))

	path, found := DetectDevcontainer(dir)
	require.True(t, found, "expected to find .devcontainer.json")
	require.Equal(t, filepath.Join(dir, ".devcontainer.json"), path)
}

func TestDetectDevcontainer_PrefersSubdir(t *testing.T) {
	dir := t.TempDir()
	// Both exist — .devcontainer/ dir takes precedence per spec
	devcontainerDir := filepath.Join(dir, ".devcontainer")
	require.NoError(t, os.MkdirAll(devcontainerDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(`{"image":"ubuntu"}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".devcontainer.json"), []byte(`{"image":"debian"}`), 0644))

	path, found := DetectDevcontainer(dir)
	require.True(t, found, "expected to find devcontainer.json")
	require.Equal(t, filepath.Join(devcontainerDir, "devcontainer.json"), path, "should prefer .devcontainer/ dir over root file")
}

func TestDetectDevcontainer_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, found := DetectDevcontainer(dir)
	require.False(t, found, "should not find devcontainer.json")
}
