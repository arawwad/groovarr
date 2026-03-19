package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"groovarr/internal/agent"
)

func (s *Server) handleStructuredArtistRemoval(ctx context.Context, resolved *resolvedTurnContext) (string, *PendingAction, bool) {
	if resolved == nil || strings.TrimSpace(resolved.Turn.SubIntent) != "artist_remove" {
		return "", nil, false
	}
	artistName := strings.TrimSpace(resolved.Turn.ArtistName)
	if artistName == "" {
		return "Which artist do you want me to remove from Lidarr?", nil, true
	}
	response, pendingAction, err := s.startArtistRemovalPreview(ctx, artistName)
	if err != nil {
		return err.Error(), nil, true
	}
	return response, pendingAction, true
}

func (s *Server) resolveStructuredLidarrCleanupApplyOutcome(ctx context.Context, resolved *resolvedTurnContext) (resultSetActionResult, bool) {
	if resolved == nil {
		return resultSetActionResult{}, false
	}
	turn := resolved.Turn
	ref := resolved.resultReference()
	if strings.TrimSpace(turn.SubIntent) != "lidarr_cleanup_apply" &&
		!(ref.effectiveSetKind() == "cleanup_candidates" && ref.Action == "preview_apply") {
		return resultSetActionResult{}, false
	}
	sessionID := chatSessionIDFromContext(ctx)
	return handleResultSetAction(ctx, s, sessionID, ref)
}

func renderStructuredLidarrCleanupApply(outcome resultSetActionResult) (ChatResponse, bool) {
	switch outcome.Kind {
	case "cleanup_preview_apply":
		return ChatResponse{
			Response:      "I’m ready to apply the cleanup preview. Use the approval buttons if you want me to proceed.",
			PendingAction: outcome.PendingAction,
		}, true
	default:
		return ChatResponse{}, false
	}
}

func (s *Server) handleStructuredLidarrCleanupApply(ctx context.Context, resolved *resolvedTurnContext) (string, *PendingAction, bool) {
	outcome, ok := s.resolveStructuredLidarrCleanupApplyOutcome(ctx, resolved)
	if !ok {
		return "", nil, false
	}
	resp, ok := renderStructuredLidarrCleanupApply(outcome)
	if !ok {
		return "", nil, false
	}
	return resp.Response, resp.PendingAction, true
}

func (s *Server) resolveStructuredBadlyRatedCleanupOutcome(ctx context.Context, resolved *resolvedTurnContext) (resultSetActionResult, bool) {
	if resolved == nil {
		return resultSetActionResult{}, false
	}
	turn := resolved.Turn
	ref := resolved.resultReference()
	if strings.TrimSpace(turn.SubIntent) != "badly_rated_cleanup" &&
		!(ref.effectiveSetKind() == "badly_rated_albums" && ref.Action == "preview_apply") {
		return resultSetActionResult{}, false
	}
	sessionID := chatSessionIDFromContext(ctx)
	return handleResultSetAction(ctx, s, sessionID, ref)
}

func renderStructuredBadlyRatedCleanup(outcome resultSetActionResult) (ChatResponse, bool) {
	switch outcome.Kind {
	case "badly_rated_preview_apply":
		return ChatResponse{
			Response:      fmt.Sprintf("I’m ready to delete %d badly rated album(s) from Lidarr. Use the approval buttons if you want me to proceed.", outcome.Count),
			PendingAction: outcome.PendingAction,
		}, true
	case "badly_rated_empty_all":
		return ChatResponse{Response: "There aren't any recently identified badly rated albums to clean from Lidarr in this chat."}, true
	case "badly_rated_empty_selection":
		return ChatResponse{Response: "There aren't any recently identified badly rated albums to clean from Lidarr in this chat, so there isn't a matching selection to apply."}, true
	default:
		return ChatResponse{}, false
	}
}

func (s *Server) handleStructuredBadlyRatedCleanup(ctx context.Context, resolved *resolvedTurnContext) (string, *PendingAction, bool) {
	outcome, ok := s.resolveStructuredBadlyRatedCleanupOutcome(ctx, resolved)
	if !ok {
		return "", nil, false
	}
	resp, ok := renderStructuredBadlyRatedCleanup(outcome)
	if !ok {
		return "", nil, false
	}
	return resp.Response, resp.PendingAction, true
}

func cleanupExecutionHandlers() []serverExecutionHandler {
	return []serverExecutionHandler{
		{
			name: "cleanup_preview_apply",
			canHandle: func(request serverExecutionRequest) bool {
				return strings.TrimSpace(request.SetKind) == "cleanup_candidates" &&
					strings.TrimSpace(request.Operation) == "preview_apply"
			},
			execute: func(ctx context.Context, s *Server, _ []agent.Message, resolved *resolvedTurnContext) (ChatResponse, bool) {
				outcome, ok := s.resolveStructuredLidarrCleanupApplyOutcome(ctx, resolved)
				if !ok {
					return ChatResponse{}, false
				}
				if resp, ok := renderStructuredLidarrCleanupApply(outcome); ok {
					return resp, true
				}
				return ChatResponse{}, false
			},
		},
		{
			name: "badly_rated_preview_apply",
			canHandle: func(request serverExecutionRequest) bool {
				return strings.TrimSpace(request.SetKind) == "badly_rated_albums" &&
					strings.TrimSpace(request.Operation) == "preview_apply"
			},
			execute: func(ctx context.Context, s *Server, _ []agent.Message, resolved *resolvedTurnContext) (ChatResponse, bool) {
				outcome, ok := s.resolveStructuredBadlyRatedCleanupOutcome(ctx, resolved)
				if !ok {
					return ChatResponse{}, false
				}
				if resp, ok := renderStructuredBadlyRatedCleanup(outcome); ok {
					return resp, true
				}
				return ChatResponse{}, false
			},
		},
	}
}

func buildCleanupSelectionFromTurn(turn normalizedTurn) string {
	switch strings.TrimSpace(turn.SelectionMode) {
	case "", "none", "all":
		return "all"
	case "top_n":
		if count, ok := parseTurnSelectionCount(turn.SelectionValue); ok {
			return fmt.Sprintf("first %d", count)
		}
	case "ordinal", "explicit_names":
		if value := strings.TrimSpace(turn.SelectionValue); value != "" {
			return value
		}
	}
	return "all"
}

func parseTurnSelectionCount(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(raw); err == nil && n > 0 {
		return n, true
	}
	if n, ok := parseSmallNumberWord(raw); ok && n > 0 {
		return n, true
	}
	for _, field := range strings.Fields(raw) {
		if n, err := strconv.Atoi(field); err == nil && n > 0 {
			return n, true
		}
		if n, ok := parseSmallNumberWord(field); ok && n > 0 {
			return n, true
		}
	}
	return 0, false
}
