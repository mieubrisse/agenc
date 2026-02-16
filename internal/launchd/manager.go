package launchd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mieubrisse/stacktrace"
)

// Manager wraps launchctl operations.
type Manager struct {
	timeout time.Duration
}

// NewManager creates a new Manager with a default timeout of 30 seconds.
func NewManager() *Manager {
	return &Manager{
		timeout: 30 * time.Second,
	}
}

// LoadPlist loads a plist file into launchd.
func (m *Manager) LoadPlist(plistPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "launchctl", "load", plistPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return stacktrace.Propagate(err, "failed to load plist %s: %s", plistPath, string(output))
	}

	return nil
}

// UnloadPlist unloads a plist file from launchd.
func (m *Manager) UnloadPlist(plistPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "launchctl", "unload", plistPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Ignore errors if the plist is not loaded
		if strings.Contains(string(output), "Could not find specified service") {
			return nil
		}
		return stacktrace.Propagate(err, "failed to unload plist %s: %s", plistPath, string(output))
	}

	return nil
}

// IsLoaded checks if a job with the given label is loaded in launchd.
func (m *Manager) IsLoaded(label string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "launchctl", "list", label)
	err := cmd.Run()

	// If the command succeeds, the job is loaded
	// If it fails with "Could not find service", the job is not loaded
	return err == nil, nil
}

// RemovePlist removes a plist file from both launchd and the filesystem.
// CRITICAL: unloads from launchd first, then deletes the file.
func (m *Manager) RemovePlist(plistPath string) error {
	// Step 1: Unload from launchd
	if err := m.UnloadPlist(plistPath); err != nil {
		return stacktrace.Propagate(err, "failed to unload plist before deletion")
	}

	// Step 2: Delete the file
	if err := os.Remove(plistPath); err != nil {
		// Ignore "file not found" errors
		if !os.IsNotExist(err) {
			return stacktrace.Propagate(err, "failed to delete plist file")
		}
	}

	return nil
}

// ListAgencCronJobs returns a list of all agenc-cron-* job labels currently loaded in launchd.
func (m *Manager) ListAgencCronJobs() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "launchctl", "list")
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return nil, stacktrace.Propagate(err, "failed to list launchd jobs")
	}

	var cronJobs []string
	lines := strings.Split(out.String(), "\n")
	for _, line := range lines {
		// Each line format: "PID\tStatus\tLabel"
		// We're looking for labels starting with "agenc-cron-"
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			label := fields[2]
			if strings.HasPrefix(label, "agenc-cron-") {
				cronJobs = append(cronJobs, label)
			}
		}
	}

	return cronJobs, nil
}

// VerifyLaunchctlAvailable checks if launchctl is available on the system.
func VerifyLaunchctlAvailable() error {
	cmd := exec.Command("launchctl", "version")
	if err := cmd.Run(); err != nil {
		return stacktrace.NewError("launchctl not available (required for cron scheduling)")
	}
	return nil
}

// RemoveJobByLabel removes a job by its label (unloads it from launchd).
// This is useful for cleaning up orphaned jobs when the plist file no longer exists.
func (m *Manager) RemoveJobByLabel(label string) error {
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "launchctl", "remove", label)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Ignore errors if the service is not found
		if strings.Contains(string(output), "Could not find specified service") {
			return nil
		}
		return stacktrace.Propagate(err, "failed to remove job %s: %s", label, string(output))
	}

	return nil
}

// GetPlistPathForLabel returns the expected plist file path for a given label.
func GetPlistPathForLabel(label string) string {
	// Extract cron name from label (agenc-cron-{cronName})
	cronName := strings.TrimPrefix(label, "agenc-cron-")
	filename := fmt.Sprintf("agenc-cron-%s.plist", cronName)
	return fmt.Sprintf("%s/%s", PlistDirpath(), filename)
}
