package main

import (
	"strings"
	"sync"
	"time"
)

type songPathTrack struct {
	ID         string
	Title      string
	ArtistName string
	AlbumName  string
	Position   int
}

type songPathState struct {
	start     songPathTrack
	end       songPathTrack
	path      []songPathTrack
	maxSteps  int
	exactSize bool
	updatedAt time.Time
}

type songPathStore struct {
	mu       sync.RWMutex
	sessions map[string]songPathState
}

var lastSongPath = songPathStore{
	sessions: make(map[string]songPathState),
}

func setLastSongPath(sessionID string, start, end songPathTrack, path []songPathTrack, maxSteps int, exactSize bool) {
	newTurnSessionMemoryWriter(sessionID).SetSongPath(start, end, path, maxSteps, exactSize)
}

func getLastSongPath(sessionID string) (songPathState, bool) {
	lastSongPath.mu.RLock()
	state, ok := lastSongPath.sessions[normalizeChatSessionID(sessionID)]
	lastSongPath.mu.RUnlock()
	if !ok {
		return songPathState{}, false
	}
	copied := make([]songPathTrack, len(state.path))
	copy(copied, state.path)
	state.path = copied
	return state, true
}

func normalizeSongPathTrack(track songPathTrack) songPathTrack {
	return songPathTrack{
		ID:         strings.TrimSpace(track.ID),
		Title:      strings.TrimSpace(track.Title),
		ArtistName: strings.TrimSpace(track.ArtistName),
		AlbumName:  strings.TrimSpace(track.AlbumName),
		Position:   track.Position,
	}
}

func normalizedSongPathTrackKey(track songPathTrack) string {
	if id := strings.TrimSpace(track.ID); id != "" {
		return "song_path:" + id
	}
	return "song_path:" + normalizeReferenceText(track.Title+" "+track.ArtistName)
}
