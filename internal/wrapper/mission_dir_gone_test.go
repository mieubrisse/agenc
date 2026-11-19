package wrapper

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// newMissionDirGoneTestWrapper builds the minimum Wrapper the mission-directory
// watch needs: a mission directory, a logger, and the exit channel.
func newMissionDirGoneTestWrapper(t *testing.T) *Wrapper {
	t.Helper()
	missionDirpath := filepath.Join(t.TempDir(), "mission")
	if err := os.MkdirAll(missionDirpath, 0755); err != nil {
		t.Fatalf("failed to create mission directory: %v", err)
	}
	return &Wrapper{
		missionDirpath: missionDirpath,
		logger:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
		claudeExited:   make(chan error, 1),
		missionDirGone: make(chan struct{}, 1),
	}
}

// startMockClaude starts cmd as the wrapper's Claude child and reaps it into
// claudeExited, the way setupRun does.
func startMockClaude(t *testing.T, w *Wrapper, cmd *exec.Cmd) {
	t.Helper()
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start mock Claude: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	w.claudeCmd = cmd
	go func() {
		w.claudeExited <- cmd.Wait()
	}()
}

func TestIsMissionDirGone(t *testing.T) {
	w := newMissionDirGoneTestWrapper(t)

	if w.isMissionDirGone() {
		t.Fatal("mission directory exists, but isMissionDirGone reported it gone")
	}
	if err := os.RemoveAll(w.missionDirpath); err != nil {
		t.Fatalf("failed to remove mission directory: %v", err)
	}
	if !w.isMissionDirGone() {
		t.Fatal("mission directory was removed, but isMissionDirGone reported it present")
	}
}

// The whole point of the watch is that Claude does not outlive the wrapper:
// the Claude child is most of what a leaked mission costs.
func TestHandleMissionDirGoneStopsClaude(t *testing.T) {
	w := newMissionDirGoneTestWrapper(t)
	startMockClaude(t, w, exec.Command("sleep", "60"))
	pid := w.claudeCmd.Process.Pid

	returned := make(chan struct{})
	go func() {
		w.handleMissionDirGone(5 * time.Second)
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(3 * time.Second):
		t.Fatal("handleMissionDirGone did not return after Claude was sent SIGTERM")
	}
	if err := syscall.Kill(pid, syscall.Signal(0)); err == nil {
		t.Fatal("Claude is still running after handleMissionDirGone returned")
	}
}

// Nobody else escalates on this path, so a Claude that ignores SIGTERM must be
// killed rather than left running behind an exited wrapper.
func TestHandleMissionDirGoneKillsClaudeThatIgnoresSIGTERM(t *testing.T) {
	w := newMissionDirGoneTestWrapper(t)
	// exec replaces the shell, so the process that ignores SIGTERM is the one
	// the wrapper holds, and SIGKILL leaves no grandchild behind.
	startMockClaude(t, w, exec.Command("sh", "-c", `trap "" TERM; exec sleep 60`))
	pid := w.claudeCmd.Process.Pid
	time.Sleep(200 * time.Millisecond) // let the trap install before signalling

	returned := make(chan struct{})
	go func() {
		w.handleMissionDirGone(500 * time.Millisecond)
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("handleMissionDirGone did not return for a Claude that ignores SIGTERM")
	}
	if err := syscall.Kill(pid, syscall.Signal(0)); err == nil {
		t.Fatal("Claude ignoring SIGTERM is still running after handleMissionDirGone returned")
	}
}

// The heartbeat loop is what notices the directory has gone; it must hand the
// shutdown to the main loop rather than keep heartbeating for a dead mission.
func TestWriteHeartbeatSignalsWhenMissionDirGone(t *testing.T) {
	w := newMissionDirGoneTestWrapper(t)
	if err := os.RemoveAll(w.missionDirpath); err != nil {
		t.Fatalf("failed to remove mission directory: %v", err)
	}

	loopDone := make(chan struct{})
	go func() {
		w.writeHeartbeat(t.Context())
		close(loopDone)
	}()

	select {
	case <-w.missionDirGone:
	case <-time.After(heartbeatInterval + 5*time.Second):
		t.Fatal("heartbeat loop did not signal that the mission directory is gone")
	}
	select {
	case <-loopDone:
	case <-time.After(time.Second):
		t.Fatal("heartbeat loop kept running after signalling the mission directory is gone")
	}
}
