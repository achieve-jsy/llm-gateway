package provider

import (
	"sync"
	"time"
)

// RuntimeState tracks the latest provider registry load result.
type RuntimeState struct {
	mu         sync.RWMutex
	loaded     bool
	source     string
	loadedAt   time.Time
	lastError  string
	modelCount int
	version    int64
}

// MarkLoadSuccess records a successful registry load.
func (s *RuntimeState) MarkLoadSuccess(source string, modelCount int, version int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loaded = true
	s.source = source
	s.loadedAt = time.Now().UTC()
	s.lastError = ""
	s.modelCount = modelCount
	s.version = version
}

// MarkLoadFailure records a failed registry load attempt.
func (s *RuntimeState) MarkLoadFailure(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loaded = false
	if err != nil {
		s.lastError = err.Error()
	}
	s.loadedAt = time.Now().UTC()
	s.modelCount = 0
}

// Snapshot returns a read-only view of the runtime state.
func (s *RuntimeState) Snapshot() (loaded bool, source string, loadedAt time.Time, lastError string, modelCount int, version int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loaded, s.source, s.loadedAt, s.lastError, s.modelCount, s.version
}
