package main

import (
	"sync"
	"time"
)

type recentListeningArtistState struct {
	ArtistName string
	TrackCount int
}

type recentListeningTrackState struct {
	ID         string
	Title      string
	ArtistName string
	PlayCount  int
	LastPlayed string
}

type recentListeningState struct {
	updatedAt    time.Time
	windowStart  string
	windowEnd    string
	totalPlays   int
	tracksHeard  int
	artistsHeard int
	topArtists   []recentListeningArtistState
	topTracks    []recentListeningTrackState
}

type recentListeningStore struct {
	mu       sync.RWMutex
	sessions map[string]recentListeningState
}

var lastRecentListening = recentListeningStore{
	sessions: make(map[string]recentListeningState),
}

func setLastRecentListeningSummary(sessionID string, state recentListeningState) {
	newTurnSessionMemoryWriter(sessionID).SetRecentListeningSummary(state)
}

func getLastRecentListeningSummary(sessionID string) (recentListeningState, bool) {
	lastRecentListening.mu.RLock()
	state, ok := lastRecentListening.sessions[normalizeChatSessionID(sessionID)]
	lastRecentListening.mu.RUnlock()
	if !ok {
		return recentListeningState{}, false
	}

	artists := make([]recentListeningArtistState, len(state.topArtists))
	copy(artists, state.topArtists)
	tracks := make([]recentListeningTrackState, len(state.topTracks))
	copy(tracks, state.topTracks)
	state.topArtists = artists
	state.topTracks = tracks
	return state, true
}
