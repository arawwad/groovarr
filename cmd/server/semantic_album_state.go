package main

import (
	"sync"
	"time"
)

type semanticAlbumSearchMatch struct {
	ID         string
	Name       string
	ArtistName string
	Year       int
	Genre      string
	PlayCount  int
	LastPlayed string
}

type semanticAlbumSearchState struct {
	queryText string
	updatedAt time.Time
	matches   []semanticAlbumSearchMatch
}

type semanticAlbumSearchStore struct {
	mu       sync.RWMutex
	sessions map[string]semanticAlbumSearchState
}

var lastSemanticAlbumSearch = semanticAlbumSearchStore{
	sessions: make(map[string]semanticAlbumSearchState),
}

func setLastSemanticAlbumSearch(sessionID, queryText string, matches []semanticAlbumSearchMatch) {
	newTurnSessionMemoryWriter(sessionID).SetSemanticAlbumSearch(queryText, matches)
}

func getLastSemanticAlbumSearch(sessionID string) ([]semanticAlbumSearchMatch, time.Time, string) {
	lastSemanticAlbumSearch.mu.RLock()
	state, ok := lastSemanticAlbumSearch.sessions[normalizeChatSessionID(sessionID)]
	lastSemanticAlbumSearch.mu.RUnlock()
	if !ok {
		return nil, time.Time{}, ""
	}

	copied := make([]semanticAlbumSearchMatch, len(state.matches))
	copy(copied, state.matches)
	return copied, state.updatedAt, state.queryText
}
