package main

import (
	"sync"
	"time"
)

type creativeAlbumCandidate struct {
	ID         string
	Name       string
	ArtistName string
	Genre      string
	Year       int
	PlayCount  int
	LastPlayed string
}

type creativeAlbumSetState struct {
	mode       string
	queryText  string
	updatedAt  time.Time
	candidates []creativeAlbumCandidate
}

type creativeAlbumSetStore struct {
	mu       sync.RWMutex
	sessions map[string]creativeAlbumSetState
}

var lastCreativeAlbumSet = creativeAlbumSetStore{
	sessions: make(map[string]creativeAlbumSetState),
}

func setLastCreativeAlbumSet(sessionID, mode, queryText string, candidates []creativeAlbumCandidate) {
	newTurnSessionMemoryWriter(sessionID).SetCreativeAlbumSet(mode, queryText, candidates)
}

func getLastCreativeAlbumSet(sessionID string) ([]creativeAlbumCandidate, time.Time, string, string) {
	lastCreativeAlbumSet.mu.RLock()
	state, ok := lastCreativeAlbumSet.sessions[normalizeChatSessionID(sessionID)]
	lastCreativeAlbumSet.mu.RUnlock()
	if !ok {
		return nil, time.Time{}, "", ""
	}

	copied := make([]creativeAlbumCandidate, len(state.candidates))
	copy(copied, state.candidates)
	return copied, state.updatedAt, state.mode, state.queryText
}
