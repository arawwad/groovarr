package main

import (
	"sync"
	"time"
)

type normalizedTurnState struct {
	updatedAt time.Time
	turn      normalizedTurn
}

type normalizedTurnStore struct {
	mu       sync.RWMutex
	sessions map[string]normalizedTurnState
}

var lastNormalizedTurn = normalizedTurnStore{
	sessions: make(map[string]normalizedTurnState),
}

func setLastNormalizedTurn(sessionID string, turn normalizedTurn) {
	lastNormalizedTurn.mu.Lock()
	lastNormalizedTurn.sessions[normalizeChatSessionID(sessionID)] = normalizedTurnState{
		updatedAt: time.Now().UTC(),
		turn:      turn,
	}
	lastNormalizedTurn.mu.Unlock()
}

func getLastNormalizedTurn(sessionID string) (normalizedTurn, time.Time, bool) {
	lastNormalizedTurn.mu.RLock()
	state, ok := lastNormalizedTurn.sessions[normalizeChatSessionID(sessionID)]
	lastNormalizedTurn.mu.RUnlock()
	if !ok {
		return normalizedTurn{}, time.Time{}, false
	}
	return state.turn, state.updatedAt, true
}
