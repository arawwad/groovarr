package main

import (
	"sync"
	"time"
)

type trackCandidate struct {
	ID         string
	Title      string
	ArtistName string
	AlbumName  string
	PlayCount  int
	LastPlayed string
	Score      float64
}

type trackCandidateSetState struct {
	mode       string
	queryText  string
	updatedAt  time.Time
	candidates []trackCandidate
}

type trackCandidateSetStore struct {
	mu       sync.RWMutex
	sessions map[string]trackCandidateSetState
}

var lastTrackCandidateSet = trackCandidateSetStore{
	sessions: make(map[string]trackCandidateSetState),
}

func setLastTrackCandidateSet(sessionID, mode, queryText string, candidates []trackCandidate) {
	newTurnSessionMemoryWriter(sessionID).SetTrackCandidateSet(mode, queryText, candidates)
}

func getLastTrackCandidateSet(sessionID string) ([]trackCandidate, time.Time, string, string) {
	lastTrackCandidateSet.mu.RLock()
	state, ok := lastTrackCandidateSet.sessions[normalizeChatSessionID(sessionID)]
	lastTrackCandidateSet.mu.RUnlock()
	if !ok {
		return nil, time.Time{}, "", ""
	}

	copied := make([]trackCandidate, len(state.candidates))
	copy(copied, state.candidates)
	return copied, state.updatedAt, state.mode, state.queryText
}

type artistCandidate struct {
	ID        string
	Name      string
	PlayCount int
	Rating    int
	Score     float64
}

type artistCandidateSetState struct {
	queryText  string
	updatedAt  time.Time
	candidates []artistCandidate
}

type artistCandidateSetStore struct {
	mu       sync.RWMutex
	sessions map[string]artistCandidateSetState
}

var lastArtistCandidateSet = artistCandidateSetStore{
	sessions: make(map[string]artistCandidateSetState),
}

func setLastArtistCandidateSet(sessionID, queryText string, candidates []artistCandidate) {
	newTurnSessionMemoryWriter(sessionID).SetArtistCandidateSet(queryText, candidates)
}

func getLastArtistCandidateSet(sessionID string) ([]artistCandidate, time.Time, string) {
	lastArtistCandidateSet.mu.RLock()
	state, ok := lastArtistCandidateSet.sessions[normalizeChatSessionID(sessionID)]
	lastArtistCandidateSet.mu.RUnlock()
	if !ok {
		return nil, time.Time{}, ""
	}

	copied := make([]artistCandidate, len(state.candidates))
	copy(copied, state.candidates)
	return copied, state.updatedAt, state.queryText
}
