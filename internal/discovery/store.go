package discovery

import (
	"strings"
	"sync"
	"time"
)

type sessionState struct {
	candidates []Candidate
	updatedAt  time.Time
	query      string
}

type Store struct {
	mu       sync.RWMutex
	sessions map[string]sessionState
}

func NewStore() *Store {
	return &Store{
		sessions: make(map[string]sessionState),
	}
}

func (s *Store) Set(sessionID, query string, candidates []Candidate) {
	copied := make([]Candidate, len(candidates))
	copy(copied, candidates)

	s.mu.Lock()
	s.sessions[strings.TrimSpace(sessionID)] = sessionState{
		candidates: copied,
		updatedAt:  time.Now(),
		query:      strings.TrimSpace(query),
	}
	s.mu.Unlock()
}

func (s *Store) Get(sessionID string) ([]Candidate, time.Time, string, bool) {
	s.mu.RLock()
	state, ok := s.sessions[strings.TrimSpace(sessionID)]
	s.mu.RUnlock()
	if !ok {
		return nil, time.Time{}, "", false
	}

	copied := make([]Candidate, len(state.candidates))
	copy(copied, state.candidates)
	return copied, state.updatedAt, state.query, true
}
