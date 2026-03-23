package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"groovarr/internal/discovery"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type PendingAction struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	Details   []string `json:"details,omitempty"`
	Approve   string   `json:"approveLabel,omitempty"`
	Discard   string   `json:"discardLabel,omitempty"`
	ExpiresAt string   `json:"expiresAt"`
}

type pendingActionState struct {
	payload   PendingAction
	execute   func(context.Context) (string, error)
	sessionID string
	requestID string
	createdAt time.Time
}

func copyPendingAction(payload PendingAction) *PendingAction {
	copyPayload := payload
	return &copyPayload
}

func newActionToken(prefix string) string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%s%d", prefix, time.Now().UTC().UnixNano())
	}
	return prefix + hex.EncodeToString(raw[:])
}

func newPendingActionID() string {
	return newActionToken("act_")
}

func newChatRequestID() string {
	return newActionToken("req_")
}

func (s *Server) registerPendingAction(sessionID, kind, title, summary string, details []string, execute func(context.Context) (string, error)) *PendingAction {
	return s.registerPendingActionWithRequest(sessionID, "", kind, title, summary, details, execute)
}

func (s *Server) registerPendingActionWithRequest(sessionID, requestID, kind, title, summary string, details []string, execute func(context.Context) (string, error)) *PendingAction {
	now := time.Now().UTC()
	sessionID = normalizeChatSessionID(sessionID)
	payload := PendingAction{
		ID:        newPendingActionID(),
		Kind:      strings.TrimSpace(kind),
		Title:     strings.TrimSpace(title),
		Summary:   strings.TrimSpace(summary),
		Details:   append([]string(nil), details...),
		Approve:   "Approve",
		Discard:   "Discard",
		ExpiresAt: now.Add(pendingActionTTL()).Format(time.RFC3339),
	}

	s.approvalMu.Lock()
	defer s.approvalMu.Unlock()
	for id, state := range s.approvals {
		expiresAt, err := time.Parse(time.RFC3339, state.payload.ExpiresAt)
		if err != nil || now.After(expiresAt) {
			if latestID, ok := s.latestPending[state.sessionID]; ok && latestID == id {
				delete(s.latestPending, state.sessionID)
			}
			delete(s.approvals, id)
		}
	}
	s.approvals[payload.ID] = &pendingActionState{
		payload:   payload,
		execute:   execute,
		sessionID: sessionID,
		requestID: strings.TrimSpace(requestID),
		createdAt: now,
	}
	s.latestPending[sessionID] = payload.ID
	return copyPendingAction(payload)
}

func (s *Server) registerPendingActionForContext(ctx context.Context, kind, title, summary string, details []string, execute func(context.Context) (string, error)) *PendingAction {
	return s.registerPendingActionWithRequest(
		chatSessionIDFromContext(ctx),
		chatRequestIDFromContext(ctx),
		kind,
		title,
		summary,
		details,
		execute,
	)
}

func (s *Server) resolvePendingAction(id string) (*pendingActionState, bool) {
	s.approvalMu.Lock()
	defer s.approvalMu.Unlock()

	state, ok := s.approvals[id]
	if !ok {
		return nil, false
	}
	expiresAt, err := time.Parse(time.RFC3339, state.payload.ExpiresAt)
	if err != nil || time.Now().UTC().After(expiresAt) {
		s.removePendingActionLocked(state.sessionID, id)
		return nil, false
	}
	s.removePendingActionLocked(state.sessionID, id)
	return state, true
}

func (s *Server) removePendingActionLocked(sessionID, actionID string) {
	if latestID, ok := s.latestPending[sessionID]; ok && latestID == actionID {
		delete(s.latestPending, sessionID)
	}
	delete(s.approvals, actionID)
}

func (s *Server) latestPendingStateLocked(sessionID string) (*pendingActionState, bool) {
	actionID, ok := s.latestPending[sessionID]
	if !ok || strings.TrimSpace(actionID) == "" {
		return nil, false
	}
	state, ok := s.approvals[actionID]
	if !ok {
		delete(s.latestPending, sessionID)
		return nil, false
	}
	expiresAt, err := time.Parse(time.RFC3339, state.payload.ExpiresAt)
	if err != nil || time.Now().UTC().After(expiresAt) {
		s.removePendingActionLocked(sessionID, actionID)
		return nil, false
	}
	return state, true
}

func (s *Server) discardPendingAction(id string) bool {
	s.approvalMu.Lock()
	defer s.approvalMu.Unlock()

	state, ok := s.approvals[id]
	if !ok {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339, state.payload.ExpiresAt)
	if err != nil || time.Now().UTC().After(expiresAt) {
		s.removePendingActionLocked(state.sessionID, id)
		return false
	}
	s.removePendingActionLocked(state.sessionID, id)
	return true
}

func (s *Server) latestPendingAction(sessionID string) *PendingAction {
	sessionID = normalizeChatSessionID(sessionID)

	s.approvalMu.Lock()
	defer s.approvalMu.Unlock()

	state, ok := s.latestPendingStateLocked(sessionID)
	if !ok {
		return nil
	}
	return copyPendingAction(state.payload)
}

func (s *Server) latestPendingActionState(sessionID string) (*pendingActionState, bool) {
	sessionID = normalizeChatSessionID(sessionID)

	s.approvalMu.Lock()
	defer s.approvalMu.Unlock()

	return s.latestPendingStateLocked(sessionID)
}

func (s *Server) latestPendingActionSince(sessionID string, minCreatedAt time.Time) *PendingAction {
	sessionID = normalizeChatSessionID(sessionID)

	s.approvalMu.Lock()
	defer s.approvalMu.Unlock()

	state, ok := s.latestPendingStateLocked(sessionID)
	if !ok {
		return nil
	}
	if !minCreatedAt.IsZero() && state.createdAt.Before(minCreatedAt) {
		return nil
	}
	return copyPendingAction(state.payload)
}

func (s *Server) latestPendingActionForRequest(sessionID, requestID string) *PendingAction {
	sessionID = normalizeChatSessionID(sessionID)
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil
	}

	s.approvalMu.Lock()
	defer s.approvalMu.Unlock()

	state, ok := s.latestPendingStateLocked(sessionID)
	if !ok {
		return nil
	}
	if strings.TrimSpace(state.requestID) != requestID {
		return nil
	}
	return copyPendingAction(state.payload)
}

func (s *Server) handlePendingAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/pending-actions/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
		s.sendError(w, "Invalid pending action path", http.StatusBadRequest)
		return
	}

	actionID := strings.TrimSpace(parts[0])
	command := strings.TrimSpace(parts[1])

	switch command {
	case "approve":
		state, ok := s.resolvePendingAction(actionID)
		if !ok {
			s.sendError(w, "Pending action not found or expired", http.StatusNotFound)
			return
		}
		timeoutSec := envInt("PENDING_ACTION_TIMEOUT_SEC", 90)
		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeoutSec)*time.Second)
		defer cancel()
		ctx = context.WithValue(ctx, chatSessionKey, normalizeChatSessionID(state.sessionID))

		response, err := state.execute(ctx)
		if err != nil {
			log.Warn().
				Err(err).
				Str("action_id", actionID).
				Str("action_kind", state.payload.Kind).
				Str("session_id", state.sessionID).
				Int("timeout_seconds", timeoutSec).
				Msg("Pending action execution failed")
			s.sendError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.sendJSON(w, ChatResponse{Response: response})
	case "discard":
		if !s.discardPendingAction(actionID) {
			s.sendError(w, "Pending action not found or expired", http.StatusNotFound)
			return
		}
		s.sendJSON(w, ChatResponse{Response: "Request discarded."})
	default:
		s.sendError(w, "Unsupported pending action command", http.StatusBadRequest)
	}
}

func (s *Server) maybeBuildPendingAction(ctx context.Context, minStateAt time.Time, userMsg string) *PendingAction {
	sessionID := chatSessionIDFromContext(ctx)
	requestID := chatRequestIDFromContext(ctx)
	if action := s.latestPendingActionForRequest(sessionID, requestID); action != nil {
		return action
	}
	if requestID == "" {
		if action := s.latestPendingActionSince(sessionID, minStateAt); action != nil {
			return action
		}
	}
	if action := s.pendingPlaylistCreateAction(ctx, minStateAt); action != nil {
		return action
	}
	if action := s.pendingDiscoveredAlbumsAction(ctx, minStateAt, userMsg); action != nil {
		return action
	}
	return nil
}

func isConversationalApproveMessage(msg string) bool {
	switch strings.ToLower(strings.TrimSpace(msg)) {
	case "yes", "y", "approve", "approved", "do it", "go ahead", "proceed", "confirm", "ok":
		return true
	default:
		return false
	}
}

func isConversationalDiscardMessage(msg string) bool {
	switch strings.ToLower(strings.TrimSpace(msg)) {
	case "no", "n", "discard", "cancel", "stop", "never mind", "nevermind":
		return true
	default:
		return false
	}
}

func (s *Server) tryConversationalPendingAction(ctx context.Context, msg string) (ChatResponse, bool) {
	sessionID := chatSessionIDFromContext(ctx)
	if isConversationalApproveMessage(msg) {
		state, ok := s.latestPendingActionState(sessionID)
		if !ok {
			return ChatResponse{Response: "There isn't a pending action to approve right now."}, true
		}
		resolved, ok := s.resolvePendingAction(state.payload.ID)
		if !ok {
			return ChatResponse{Response: "That pending action is no longer available."}, true
		}
		timeoutSec := envInt("PENDING_ACTION_TIMEOUT_SEC", 90)
		approvalCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()
		approvalCtx = context.WithValue(approvalCtx, chatSessionKey, normalizeChatSessionID(resolved.sessionID))
		response, err := resolved.execute(approvalCtx)
		if err != nil {
			log.Warn().
				Err(err).
				Str("action_id", resolved.payload.ID).
				Str("action_kind", resolved.payload.Kind).
				Str("session_id", resolved.sessionID).
				Int("timeout_seconds", timeoutSec).
				Msg("Conversational pending action execution failed")
			return ChatResponse{Response: err.Error()}, true
		}
		return ChatResponse{Response: response}, true
	}
	if isConversationalDiscardMessage(msg) {
		state, ok := s.latestPendingActionState(sessionID)
		if !ok {
			return ChatResponse{Response: "There isn't a pending action to discard right now."}, true
		}
		if !s.discardPendingAction(state.payload.ID) {
			return ChatResponse{Response: "That pending action is no longer available."}, true
		}
		return ChatResponse{Response: "Request discarded."}, true
	}
	return ChatResponse{}, false
}

func (s *Server) pendingPlaylistCreateAction(ctx context.Context, minStateAt time.Time) *PendingAction {
	sessionID := chatSessionIDFromContext(ctx)
	prompt, playlistName, plannedAt, candidates, _, _, ok := loadTurnSessionMemory(sessionID).PlaylistContext()
	if !ok || len(candidates) == 0 || plannedAt.IsZero() || plannedAt.Before(minStateAt) {
		return nil
	}
	if strings.TrimSpace(playlistName) == "" {
		playlistName = "Discover: Mixed"
	}

	details := []string{
		fmt.Sprintf("Playlist: %s", playlistName),
		fmt.Sprintf("Planned tracks: %d", len(candidates)),
	}
	if strings.TrimSpace(prompt) != "" {
		details = append(details, fmt.Sprintf("Prompt: %s", prompt))
	}

	return s.registerPendingActionForContext(
		ctx,
		"playlist_create",
		"Create playlist",
		fmt.Sprintf("Create or update playlist %q from the current plan and queue missing tracks if needed.", playlistName),
		details,
		func(ctx context.Context) (string, error) {
			return s.executePlaylistCreateApproval(ctx, plannedAt)
		},
	)
}

func (s *Server) pendingDiscoveredAlbumsAction(ctx context.Context, minStateAt time.Time, userMsg string) *PendingAction {
	if !wantsDiscoveredAlbumApproval(userMsg) {
		return nil
	}
	action, _, ok := s.buildDiscoveredAlbumsPendingAction(ctx, "all", minStateAt)
	if !ok {
		return nil
	}
	return action
}

func (s *Server) buildDiscoveredAlbumsPendingAction(ctx context.Context, selection string, minStateAt time.Time) (*PendingAction, int, bool) {
	sessionID := chatSessionIDFromContext(ctx)
	candidates, discoveredAt, sourceQuery, ok := loadTurnSessionMemory(sessionID).DiscoveredAlbums()
	if !ok || len(candidates) == 0 || discoveredAt.IsZero() {
		return nil, 0, false
	}
	if time.Since(discoveredAt) > 30*time.Minute {
		return nil, 0, false
	}
	if !minStateAt.IsZero() && discoveredAt.Before(minStateAt) {
		return nil, 0, false
	}
	selection = strings.TrimSpace(selection)
	selected := candidates
	if selection != "" && !strings.EqualFold(selection, "all") {
		var err error
		selected, err = discovery.SelectCandidates(candidates, selection)
		if err != nil {
			return nil, 0, false
		}
	}
	if selection == "" {
		selection = "all"
	}

	details := []string{
		fmt.Sprintf("Albums selected: %d", len(selected)),
		"Mode: add to your library and search",
	}
	if strings.TrimSpace(sourceQuery) != "" {
		details = append(details, fmt.Sprintf("Source: %s", sourceQuery))
	}

	return s.registerPendingActionForContext(
		ctx,
		"lidarr_discovery_apply",
		"Add albums to your library",
		fmt.Sprintf("Apply library actions for %d discovered album(s).", len(selected)),
		details,
		func(ctx context.Context) (string, error) {
			approvalCtx := context.WithValue(ctx, chatSessionKey, normalizeChatSessionID(sessionID))
			return s.executeDiscoveredAlbumsApproval(approvalCtx, discoveredAt, selection)
		},
	), len(selected), true
}

func wantsDiscoveredAlbumApproval(msg string) bool {
	q := strings.ToLower(strings.TrimSpace(msg))
	if q == "" {
		return false
	}
	cues := []string{
		"add ",
		" add",
		"can i add",
		"can we add",
		"could we add",
		"could you add",
		"put ",
		"import ",
		"monitor",
		"lidarr",
		"to library",
		"to my library",
		"search for",
	}
	for _, cue := range cues {
		if strings.Contains(q, cue) {
			return true
		}
	}
	return false
}

func (s *Server) buildLidarrCleanupPendingAction(ctx context.Context, requestedAction, selection string, minStateAt time.Time) (*PendingAction, int, string, bool) {
	sessionID := chatSessionIDFromContext(ctx)
	candidates, updatedAt, ok := loadTurnSessionMemory(sessionID).CleanupCandidates()
	if !ok || len(candidates) == 0 || updatedAt.IsZero() || updatedAt.Before(minStateAt) {
		return nil, 0, "", false
	}

	selection = strings.TrimSpace(selection)
	if selection == "" {
		selection = "all"
	}
	selectedIDs, err := selectAlbumIDsFromCandidates(selection, candidates)
	if err != nil || len(selectedIDs) == 0 {
		return nil, 0, "", false
	}
	action := recommendedLidarrCleanupAction(candidates)
	if explicitAction := strings.TrimSpace(requestedAction); explicitAction != "" {
		action = explicitAction
	}
	if !isSupportedLidarrCleanupAction(action) {
		return nil, 0, "", false
	}
	summaryCounts := make(map[string]int)
	for _, candidate := range candidates {
		summaryCounts[candidate.Reason]++
	}
	reasons := make([]string, 0, len(summaryCounts))
	for reason := range summaryCounts {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)

	details := []string{
		fmt.Sprintf("Albums selected: %d", len(candidates)),
		fmt.Sprintf("Action: %s", strings.ReplaceAll(action, "_", " ")),
	}
	for _, reason := range reasons {
		count := summaryCounts[reason]
		details = append(details, fmt.Sprintf("%s: %d", reason, count))
	}

	return s.registerPendingActionForContext(
		ctx,
		"lidarr_cleanup_apply",
		"Apply library cleanup",
		fmt.Sprintf("Apply the recommended library cleanup action to %d album(s).", len(selectedIDs)),
		details,
		func(ctx context.Context) (string, error) {
			approvalCtx := context.WithValue(ctx, chatSessionKey, normalizeChatSessionID(sessionID))
			return s.executeLidarrCleanupApproval(approvalCtx, updatedAt, action, selection)
		},
	), len(selectedIDs), action, true
}

func isSupportedLidarrCleanupAction(action string) bool {
	switch strings.TrimSpace(action) {
	case "unmonitor", "delete", "search_missing", "refresh_metadata":
		return true
	default:
		return false
	}
}
