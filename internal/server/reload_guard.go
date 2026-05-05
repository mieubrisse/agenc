package server

// tryAcquireReloadLock attempts to claim an exclusive reload slot for the
// given mission. Returns a release func and true on success, or (nil, false)
// if a reload is already in progress for this mission.
//
// Callers MUST defer the release func to ensure the slot is freed even on
// panic or error.
//
// The lock is per-mission: different missions reload concurrently. Memory
// footprint is O(active reloads) — entries are deleted on release.
func (s *Server) tryAcquireReloadLock(missionID string) (func(), bool) {
	if _, loaded := s.reloadsInProgress.LoadOrStore(missionID, struct{}{}); loaded {
		return nil, false
	}
	return func() { s.reloadsInProgress.Delete(missionID) }, true
}
