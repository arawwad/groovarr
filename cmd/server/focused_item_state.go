package main

import (
	"sync"
	"time"
)

type focusedResultItemState struct {
	kind      string
	key       string
	updatedAt time.Time
}

type focusedResultItemStore struct {
	mu       sync.RWMutex
	sessions map[string]focusedResultItemState
}

var lastFocusedResultItem = focusedResultItemStore{
	sessions: make(map[string]focusedResultItemState),
}

func setLastFocusedResultItem(sessionID, kind, key string) {
	sessionID = normalizeChatSessionID(sessionID)
	lastFocusedResultItem.mu.Lock()
	lastFocusedResultItem.sessions[sessionID] = focusedResultItemState{
		kind:      kind,
		key:       key,
		updatedAt: time.Now().UTC(),
	}
	lastFocusedResultItem.mu.Unlock()
}

func getLastFocusedResultItem(sessionID string) (focusedResultItemState, bool) {
	lastFocusedResultItem.mu.RLock()
	state, ok := lastFocusedResultItem.sessions[normalizeChatSessionID(sessionID)]
	lastFocusedResultItem.mu.RUnlock()
	if !ok {
		return focusedResultItemState{}, false
	}
	return state, true
}
