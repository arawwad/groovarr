package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"groovarr/internal/agent"
)

func (s *Server) tryTurnIntentRoute(ctx context.Context, turn *Turn, history []agent.Message) (ChatResponse, bool) {
	if turn == nil {
		return ChatResponse{}, false
	}
	resolved := turnToResolvedTurnContext(turn)
	if resolved == nil {
		return ChatResponse{}, false
	}
	msg := turn.UserMessage
	lowerMsg := strings.ToLower(strings.TrimSpace(msg))
	normalized := resolved.Turn

	if resolved.HasResolvedScene {
		if resp, ok := s.handleStructuredSceneSelectionTurn(ctx, turn); ok {
			return ChatResponse{Response: resp}, true
		}
	}
	if resp, ok := s.handleStructuredSceneOverviewTurn(ctx, turn); ok {
		return ChatResponse{Response: resp}, true
	}

	switch normalized.Intent {
	case "general_chat", "other":
		if resp, pendingAction, ok := s.handleStructuredArtistRemovalTurn(ctx, turn); ok {
			return ChatResponse{Response: resp, PendingAction: pendingAction}, true
		}
		if resp, pendingAction, ok := s.handleStructuredLidarrCleanupApplyTurn(ctx, turn); ok {
			return ChatResponse{Response: resp, PendingAction: pendingAction}, true
		}
		if resp, pendingAction, ok := s.handleStructuredBadlyRatedCleanupTurn(ctx, turn); ok {
			return ChatResponse{Response: resp, PendingAction: pendingAction}, true
		}
		return ChatResponse{}, false
	case "track_discovery":
		if resp, ok := s.handleStructuredSongPathSummaryTurn(ctx, turn); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredTrackCompareTurn(ctx, turn); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredTrackVariantPickTurn(ctx, turn); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredTrackDescriptionTurn(ctx, turn); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredTrackSimilarityTurn(ctx, turn); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredTrackSearchTurn(ctx, turn); ok {
			return ChatResponse{Response: resp}, true
		}
	case "artist_discovery":
		if resp, ok := s.handleStructuredArtistCompareTurn(ctx, turn); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredArtistVariantPickTurn(ctx, turn); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredArtistStartingAlbumTurn(ctx, turn); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredArtistSimilarityTurn(ctx, turn); ok {
			return ChatResponse{Response: resp}, true
		}
	case "scene_discovery":
		if resp, ok := s.handleStructuredSceneSelectionTurn(ctx, turn); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredSceneOverviewTurn(ctx, turn); ok {
			return ChatResponse{Response: resp}, true
		}
	case "listening":
		if strings.TrimSpace(normalized.SubIntent) == "listening_interpretation" || strings.TrimSpace(normalized.SubIntent) == "artist_dominance" {
			if resp, ok := s.tryRecentListeningInterpretationTurn(chatCtxOrBackground(ctx), turn); ok {
				return ChatResponse{Response: resp}, true
			}
		}
		if normalized.FollowupMode != "none" {
			if resp, ok := s.handleAlbumResultSetListeningFollowUpTurn(ctx, turn); ok {
				return ChatResponse{Response: resp}, true
			}
			if resp, ok := s.tryRecentListeningInterpretationTurn(chatCtxOrBackground(ctx), turn); ok {
				return ChatResponse{Response: resp}, true
			}
			if resp, ok := s.handleStructuredCreativeAlbumSetFollowUpTurn(ctx, turn); ok {
				return ChatResponse{Response: resp}, true
			}
		}
		if normalized.TimeWindow != "none" {
			if resp, ok := s.tryNormalizedRecentListeningSummary(ctx, normalized, lowerMsg); ok {
				return resp, true
			}
		}
	case "stats":
		if normalized.FollowupMode != "none" || strings.TrimSpace(normalized.SubIntent) == "listening_interpretation" || strings.TrimSpace(normalized.SubIntent) == "artist_dominance" {
			if resp, ok := s.tryRecentListeningInterpretationTurn(chatCtxOrBackground(ctx), turn); ok {
				return ChatResponse{Response: resp}, true
			}
		}
		if normalized.TimeWindow != "none" && (normalized.QueryScope == "stats" || normalized.QueryScope == "listening" || normalized.QueryScope == "unknown" || normalized.QueryScope == "general" || strings.TrimSpace(normalized.SubIntent) == "artist_dominance" || strings.TrimSpace(normalized.SubIntent) == "library_top_artists") {
			if resp, ok := s.tryNormalizedArtistListeningStats(ctx, normalized); ok {
				return ChatResponse{Response: resp}, true
			}
		}
		if normalized.QueryScope == "library" {
			if resp, ok := s.handleLibraryFacetQuery(ctx, lowerMsg); ok {
				return ChatResponse{Response: resp}, true
			}
			if resp, ok := s.handleArtistLibraryStatsQuery(ctx, lowerMsg); ok {
				return ChatResponse{Response: resp}, true
			}
			if resp, ok := s.handleAlbumLibraryStatsQuery(ctx, lowerMsg); ok {
				return ChatResponse{Response: resp}, true
			}
			if resp, ok := s.tryNormalizedTopLibraryArtists(ctx, normalized); ok {
				return ChatResponse{Response: resp}, true
			}
		}
	case "album_discovery":
		if normalized.FollowupMode != "none" {
			if resp, ok := s.handleAlbumResultSetListeningFollowUpTurn(ctx, turn); ok {
				return ChatResponse{Response: resp}, true
			}
			if resp, ok := s.handleStructuredDiscoveredAlbumsAvailabilityTurn(ctx, turn); ok {
				return ChatResponse{Response: resp}, true
			}
			if resp, pendingAction, ok := s.handleStructuredDiscoveredAlbumsApplyTurn(ctx, turn); ok {
				return ChatResponse{Response: resp, PendingAction: pendingAction}, true
			}
			if resp, ok := s.handleStructuredCreativeAlbumSetFollowUpTurn(ctx, turn); ok {
				return ChatResponse{Response: resp}, true
			}
		}
		if normalized.QueryScope == "library" {
			if resp, ok := s.handleStructuredCreativeLibraryDiscoveryTurn(ctx, turn); ok {
				return ChatResponse{Response: resp}, true
			}
			if resp, ok := s.handleUnderplayedAlbums(ctx, msg); ok {
				return ChatResponse{Response: resp}, true
			}
			if resp, ok := s.tryCreativeLibraryAlbumsRoute(ctx, lowerMsg); ok {
				return ChatResponse{Response: resp}, true
			}
		}
		if resp, ok := s.handleSpecificAlbumDiscovery(ctx, msg); ok {
			return ChatResponse{Response: resp}, true
		}
	case "playlist":
		if resp, ok := s.handleStructuredPlaylistInventoryTurn(ctx, turn); ok {
			return ChatResponse{Response: resp}, true
		}
		if shouldAttemptPlaylistCreateTurn(turn) {
			if resp, ok := s.tryPlaylistCreateTurn(ctx, turn); ok {
				return resp, true
			}
		}
		if resp, ok := s.handleStructuredSavedPlaylistVibeTurn(ctx, turn, history); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredSavedPlaylistArtistCoverageTurn(ctx, turn, history); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredSavedPlaylistAppendTurn(ctx, turn, history); ok {
			return resp, true
		}
		if resp, ok := s.handleStructuredSavedPlaylistRefreshTurn(ctx, turn, history); ok {
			return resp, true
		}
		if resp, ok := s.handleStructuredSavedPlaylistRepairTurn(ctx, turn, history); ok {
			return resp, true
		}
		if resp, ok := s.handleStructuredPlaylistQueueRequestTurn(ctx, turn, history); ok {
			return resp, true
		}
		if normalized.FollowupMode == "none" {
			if resp, ok := s.tryPlaylistCreateTurn(ctx, turn); ok {
				return resp, true
			}
		}
		if resp, ok := s.handleStructuredPlaylistTracksQueryTurn(ctx, turn, history); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredPlaylistAvailabilityTurn(ctx, turn); ok {
			return ChatResponse{Response: resp}, true
		}
	}

	return ChatResponse{}, false
}

func (s *Server) tryNormalizedIntentRoute(ctx context.Context, msg string, history []agent.Message, resolved *resolvedTurnContext) (ChatResponse, bool) {
	turn := turnFromResolved(resolved)
	if turn == nil {
		return ChatResponse{}, false
	}
	turn.UserMessage = strings.TrimSpace(msg)
	turn.SessionID = chatSessionIDFromContext(ctx)
	return s.tryTurnIntentRoute(ctx, turn, history)
}

func (s *Server) handleAlbumResultSetListeningFollowUp(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	outcome, ok := s.resolveAlbumResultSetListeningFollowUpOutcome(ctx, resolved)
	if !ok {
		return "", false
	}
	return renderAlbumResultSetListeningFollowUp(outcome)
}

func (s *Server) handleAlbumResultSetListeningFollowUpTurn(ctx context.Context, turn *Turn) (string, bool) {
	return s.handleAlbumResultSetListeningFollowUp(ctx, turnToResolvedTurnContext(turn))
}

type albumResultSetListeningOutcome struct {
	candidates []creativeAlbumCandidate
	start      time.Time
	end        time.Time
}

func (s *Server) resolveAlbumResultSetListeningFollowUpOutcome(ctx context.Context, resolved *resolvedTurnContext) (albumResultSetListeningOutcome, bool) {
	ref := resolved.resultReference()
	if resolved == nil || strings.TrimSpace(resolved.Turn.SubIntent) != "result_set_play_recency" || resolved.Turn.TimeWindow == "none" {
		return albumResultSetListeningOutcome{}, false
	}
	sessionID := chatSessionIDFromContext(ctx)
	windowStart, windowEnd, ok := resolveListeningPeriodFromWindow(resolved.Turn.TimeWindow)
	if !ok {
		return albumResultSetListeningOutcome{}, false
	}
	if candidates, _, ok := creativeCandidatesFromResolvedReference(sessionID, resolved); ok {
		if ref.ResolvedItemKey != "" {
			candidates = narrowCreativeCandidatesToFocusedItem(candidates, ref.ResolvedItemKey)
		}
		if len(candidates) == 0 {
			return albumResultSetListeningOutcome{}, false
		}
		return albumResultSetListeningOutcome{
			candidates: candidates,
			start:      windowStart,
			end:        windowEnd,
		}, true
	}
	return albumResultSetListeningOutcome{}, false
}

func renderAlbumResultSetListeningFollowUp(outcome albumResultSetListeningOutcome) (string, bool) {
	if len(outcome.candidates) == 0 {
		return "", false
	}
	return renderAlbumResultSetListeningWindow(outcome.candidates, outcome.start, outcome.end), true
}

func renderAlbumResultSetListeningWindow(candidates []creativeAlbumCandidate, start, end time.Time) string {
	matches := filterCreativeCandidatesByLastPlayed(candidates, func(ts time.Time) bool {
		return (ts.Equal(start) || ts.After(start)) && (ts.Equal(end) || ts.Before(end))
	})
	if len(matches) == 0 {
		return fmt.Sprintf("None of those show a play timestamp between %s and %s.", start.Local().Format("Jan 2, 2006"), end.Local().Format("Jan 2, 2006"))
	}
	return renderCreativeAlbumSet(
		fmt.Sprintf("From those, these show a play timestamp between %s and %s", start.Local().Format("Jan 2, 2006"), end.Local().Format("Jan 2, 2006")),
		matches,
		4,
	)
}

func (s *Server) tryRecentListeningInterpretationTurn(ctx context.Context, turn *Turn) (string, bool) {
	if turn == nil {
		return "", false
	}
	state, ok := loadTurnSessionMemory(chatSessionIDFromContext(ctx)).RecentListeningSummary()
	if !ok || state.updatedAt.IsZero() || time.Since(state.updatedAt) > llmContextRecentListeningTTL {
		return "", false
	}
	return describeStructuredRecentListeningInterpretation(state, turn.Normalized.SubIntent)
}

func (s *Server) tryNormalizedRecentListeningInterpretation(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	return s.tryRecentListeningInterpretationTurn(ctx, turnFromResolved(resolved))
}

func (s *Server) tryNormalizedRecentListeningSummary(ctx context.Context, turn normalizedTurn, lowerMsg string) (ChatResponse, bool) {
	if s.resolver == nil {
		return ChatResponse{}, false
	}
	start, end, ok := resolveListeningPeriodFromWindow(turn.TimeWindow)
	if !ok {
		return ChatResponse{}, false
	}
	return s.buildRecentListeningSummaryResponse(ctx, start, end, lowerMsg)
}

func (s *Server) tryNormalizedArtistListeningStats(ctx context.Context, turn normalizedTurn) (string, bool) {
	if s.resolver == nil {
		return "", false
	}
	start, end, ok := resolveListeningPeriodFromWindow(turn.TimeWindow)
	if !ok {
		return "", false
	}
	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "artistListeningStats", map[string]interface{}{
		"filter": map[string]interface{}{
			"playedSince": start.Format(time.RFC3339),
			"playedUntil": end.Format(time.RFC3339),
		},
		"sort":  "plays",
		"limit": 8,
	})
	if err != nil {
		return "", false
	}

	var parsed struct {
		Data struct {
			Items []struct {
				ArtistName    string `json:"artistName"`
				AlbumCount    int    `json:"albumCount"`
				PlaysInWindow int    `json:"playsInWindow"`
			} `json:"artistListeningStats"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || len(parsed.Data.Items) == 0 {
		return "", false
	}

	if strings.TrimSpace(turn.SubIntent) == "artist_dominance" {
		return renderNormalizedArtistDominance(parsed.Data.Items, turn), true
	}

	items := make([]string, 0, len(parsed.Data.Items))
	for _, item := range parsed.Data.Items {
		name := strings.TrimSpace(item.ArtistName)
		if name == "" {
			continue
		}
		items = append(items, fmt.Sprintf("%s (%d plays, %d albums)", name, item.PlaysInWindow, item.AlbumCount))
	}
	if len(items) == 0 {
		return "", false
	}

	return renderRouteBulletList("Top artists in this window", items, 8), true
}

func renderNormalizedArtistDominance(items []struct {
	ArtistName    string `json:"artistName"`
	AlbumCount    int    `json:"albumCount"`
	PlaysInWindow int    `json:"playsInWindow"`
}, turn normalizedTurn) string {
	topName := strings.TrimSpace(items[0].ArtistName)
	topPlays := items[0].PlaysInWindow
	window := normalizedTimeWindowLabel(turn.TimeWindow)
	if len(items) == 1 || strings.TrimSpace(items[1].ArtistName) == "" {
		return fmt.Sprintf("%s is leading %s with %d plays.", topName, window, topPlays)
	}
	secondName := strings.TrimSpace(items[1].ArtistName)
	secondPlays := items[1].PlaysInWindow
	if artist := strings.TrimSpace(turn.ArtistName); artist != "" && strings.EqualFold(artist, topName) {
		return fmt.Sprintf("%s is clearly ahead %s with %d plays, versus %s on %d.", topName, window, topPlays, secondName, secondPlays)
	}
	if topPlays <= secondPlays {
		return fmt.Sprintf("%s and %s are effectively tied %s.", topName, secondName, window)
	}
	return fmt.Sprintf("%s is ahead %s with %d plays, versus %s on %d.", topName, window, topPlays, secondName, secondPlays)
}

func normalizedTimeWindowLabel(window string) string {
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "this_month":
		return "this month"
	case "last_month", "ambiguous_recent":
		return "in the last month"
	case "this_year":
		return "this year"
	default:
		return "in this window"
	}
}

func chatCtxOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (s *Server) tryPlaylistCreateTurn(ctx context.Context, turn *Turn) (ChatResponse, bool) {
	if turn != nil {
		subIntent := strings.TrimSpace(turn.Normalized.SubIntent)
		if subIntent != "" && subIntent != "playlist_vibe" {
			return ChatResponse{}, false
		}
	}
	rawMsg := ""
	var normalized TurnNormalized
	if turn != nil {
		rawMsg = turn.UserMessage
		normalized = turn.Normalized
	}
	prompt := extractNormalizedPlaylistCreateIntent(rawMsg)
	if prompt == "" {
		prompt = normalizedPlaylistCreatePrompt(normalized)
	}
	if prompt == "" {
		return ChatResponse{Response: "What kind of playlist do you want me to make?"}, true
	}
	if s.resolver == nil {
		return ChatResponse{}, false
	}
	trackCount := extractPlaylistTrackCount(strings.ToLower(strings.TrimSpace(rawMsg)), 20)
	response, pendingAction, err := s.startPlaylistCreatePreview(ctx, "", prompt, trackCount)
	if err != nil {
		return ChatResponse{}, false
	}
	return ChatResponse{Response: response, PendingAction: pendingAction}, true
}

func (s *Server) tryNormalizedPlaylistCreate(ctx context.Context, rawMsg string, resolved *resolvedTurnContext) (ChatResponse, bool) {
	turn := turnFromResolved(resolved)
	if turn == nil {
		turn = &Turn{}
	}
	turn.UserMessage = strings.TrimSpace(rawMsg)
	return s.tryPlaylistCreateTurn(ctx, turn)
}

func shouldAttemptPlaylistCreateTurn(turn *Turn) bool {
	if turn == nil || strings.TrimSpace(turn.UserMessage) == "" {
		return false
	}
	if strings.TrimSpace(turn.Normalized.FollowupMode) != "" && strings.TrimSpace(turn.Normalized.FollowupMode) != "none" {
		return false
	}
	subIntent := strings.TrimSpace(turn.Normalized.SubIntent)
	if subIntent != "" && subIntent != "playlist_vibe" {
		return false
	}
	if extractNormalizedPlaylistCreateIntent(turn.UserMessage) != "" {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(turn.UserMessage))
	return strings.Contains(lower, " playlist") &&
		(strings.Contains(lower, "make ") ||
			strings.Contains(lower, "create ") ||
			strings.Contains(lower, "build ") ||
			strings.Contains(lower, "put together ") ||
			strings.Contains(lower, "assemble ") ||
			strings.Contains(lower, "spin up ") ||
			strings.Contains(lower, "queue up "))
}

func normalizedPlaylistCreatePrompt(turn TurnNormalized) string {
	if text := strings.TrimSpace(turn.PromptHint); text != "" {
		return text
	}
	parts := make([]string, 0, len(turn.StyleHints)+1)
	if len(turn.StyleHints) > 0 {
		parts = append(parts, strings.Join(turn.StyleHints, " "))
	}
	if target := strings.TrimSpace(turn.TargetName); target != "" {
		parts = append(parts, "for "+target)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}
