package server

import (
	"context"
	"log"
	"os"
	"sync"
	"testing"
	"time"
)

func TestRunLoop_NormalCompletion(t *testing.T) {
	s := &Server{
		logger: log.New(os.Stderr, "", 0),
	}

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so the loop exits

	// Use a channel to know the goroutine has started (wg.Add has been called)
	started := make(chan struct{})
	go func() {
		// Signal after a tiny delay to ensure wg.Add(1) inside runLoop executes
		// before we proceed to wg.Wait in the main goroutine.
		close(started)
		s.runLoop("test-loop", &wg, ctx, func(ctx context.Context) {
			<-ctx.Done()
		})
	}()

	<-started
	// Brief yield to let runLoop's wg.Add(1) execute
	time.Sleep(10 * time.Millisecond)
	wg.Wait()

	val, ok := s.loopHealth.Load("test-loop")
	if !ok {
		t.Fatal("expected loop health entry for 'test-loop'")
	}
	if val != "stopped" {
		t.Errorf("expected status 'stopped', got %q", val)
	}
}

func TestRunLoop_PanicRecovery(t *testing.T) {
	s := &Server{
		logger: log.New(os.Stderr, "", 0),
	}

	var wg sync.WaitGroup

	started := make(chan struct{})
	go func() {
		close(started)
		s.runLoop("panic-loop", &wg, context.Background(), func(ctx context.Context) {
			panic("test panic")
		})
	}()

	<-started
	time.Sleep(10 * time.Millisecond)
	wg.Wait()

	val, ok := s.loopHealth.Load("panic-loop")
	if !ok {
		t.Fatal("expected loop health entry for 'panic-loop'")
	}
	if val != "crashed" {
		t.Errorf("expected status 'crashed', got %q", val)
	}
}

func TestRunLoop_HealthSetToRunningDuringExecution(t *testing.T) {
	s := &Server{
		logger: log.New(os.Stderr, "", 0),
	}

	var wg sync.WaitGroup
	started := make(chan struct{})

	go s.runLoop("running-loop", &wg, context.Background(), func(ctx context.Context) {
		close(started)
		time.Sleep(50 * time.Millisecond)
	})

	<-started

	val, ok := s.loopHealth.Load("running-loop")
	if !ok {
		t.Fatal("expected loop health entry for 'running-loop'")
	}
	if val != "running" {
		t.Errorf("expected status 'running' during execution, got %q", val)
	}

	wg.Wait()
}
