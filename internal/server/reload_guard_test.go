package server

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestTryAcquireReloadLock_ExclusivePerMission(t *testing.T) {
	s := &Server{}

	release1, ok1 := s.tryAcquireReloadLock("mission-a")
	if !ok1 {
		t.Fatalf("first acquire should succeed")
	}

	_, ok2 := s.tryAcquireReloadLock("mission-a")
	if ok2 {
		t.Fatalf("second concurrent acquire on same mission should fail")
	}

	release1()

	release3, ok3 := s.tryAcquireReloadLock("mission-a")
	if !ok3 {
		t.Fatalf("acquire after release should succeed")
	}
	release3()
}

func TestTryAcquireReloadLock_DifferentMissionsConcurrent(t *testing.T) {
	s := &Server{}

	release1, ok1 := s.tryAcquireReloadLock("mission-a")
	release2, ok2 := s.tryAcquireReloadLock("mission-b")

	if !ok1 || !ok2 {
		t.Fatalf("acquires for different missions should both succeed")
	}

	release1()
	release2()
}

func TestTryAcquireReloadLock_NoLeaksAfterContention(t *testing.T) {
	s := &Server{}
	const goroutines = 100
	var wg sync.WaitGroup

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			release, ok := s.tryAcquireReloadLock("mission-x")
			if ok {
				release()
			}
		}()
	}
	wg.Wait()

	if _, exists := s.reloadsInProgress.Load("mission-x"); exists {
		t.Fatalf("reload lock should be empty after all goroutines complete")
	}
}

func TestTryAcquireReloadLock_StrictMutualExclusion(t *testing.T) {
	s := &Server{}
	const goroutines = 50
	var wg sync.WaitGroup
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	heldGate := make(chan struct{})

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			release, ok := s.tryAcquireReloadLock("mission-y")
			if !ok {
				return
			}
			n := inFlight.Add(1)
			for {
				cur := maxInFlight.Load()
				if n <= cur || maxInFlight.CompareAndSwap(cur, n) {
					break
				}
			}
			<-heldGate
			inFlight.Add(-1)
			release()
		}()
	}

	close(heldGate)
	wg.Wait()

	if maxInFlight.Load() > 1 {
		t.Fatalf("more than one goroutine held the lock simultaneously: max=%d", maxInFlight.Load())
	}
}
