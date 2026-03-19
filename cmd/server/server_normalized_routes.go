package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"groovarr/internal/agent"
)

func (s *Server) tryNormalizedIntentRoute(ctx context.Context, msg string, history []agent.Message, resolved *resolvedTurnContext) (ChatResponse, bool) {
	if resolved == nil {
		return ChatResponse{}, false
	}
	lowerMsg := strings.ToLower(strings.TrimSpace(msg))
	turn := resolved.Turn

	if resolved.HasResolvedScene {
		if resp, ok := s.handleStructuredSceneSelection(ctx, resolved); ok {
			return ChatResponse{Response: resp}, true
		}
	}
	if resp, ok := s.handleStructuredSceneOverview(ctx, resolved); ok {
		return ChatResponse{Response: resp}, true
	}

	switch turn.Intent {
	case "general_chat", "other":
		if resp, pendingAction, ok := s.handleStructuredArtistRemoval(ctx, resolved); ok {
			return ChatResponse{Response: resp, PendingAction: pendingAction}, true
		}
		if resp, pendingAction, ok := s.handleStructuredLidarrCleanupApply(ctx, resolved); ok {
			return ChatResponse{Response: resp, PendingAction: pendingAction}, true
		}
		if resp, pendingAction, ok := s.handleStructuredBadlyRatedCleanup(ctx, resolved); ok {
			return ChatResponse{Response: resp, PendingAction: pendingAction}, true
		}
		return ChatResponse{}, false
	case "track_discovery":
		if resp, ok := s.handleStructuredSongPathSummary(ctx, resolved); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredTrackVariantPick(ctx, resolved); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredTrackDescription(ctx, resolved); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredTrackSimilarity(ctx, msg, resolved); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredTrackSearch(ctx, msg, resolved); ok {
			return ChatResponse{Response: resp}, true
		}
	case "artist_discovery":
		if resp, ok := s.handleStructuredArtistVariantPick(ctx, resolved); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredArtistStartingAlbum(ctx, resolved); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredArtistSimilarity(ctx, resolved); ok {
			return ChatResponse{Response: resp}, true
		}
	case "scene_discovery":
		if resp, ok := s.handleStructuredSceneSelection(ctx, resolved); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredSceneOverview(ctx, resolved); ok {
			return ChatResponse{Response: resp}, true
		}
	case "listening":
		if strings.TrimSpace(turn.SubIntent) == "listening_interpretation" || strings.TrimSpace(turn.SubIntent) == "artist_dominance" {
			if resp, ok := s.tryNormalizedRecentListeningInterpretation(chatCtxOrBackground(ctx), resolved); ok {
				return ChatResponse{Response: resp}, true
			}
		}
		if turn.FollowupMode != "none" {
			if resp, ok := s.handleAlbumResultSetListeningFollowUp(ctx, resolved); ok {
				return ChatResponse{Response: resp}, true
			}
			if resp, ok := s.tryNormalizedRecentListeningInterpretation(chatCtxOrBackground(ctx), resolved); ok {
				return ChatResponse{Response: resp}, true
			}
			if resp, ok := s.handleStructuredCreativeAlbumSetFollowUp(ctx, resolved); ok {
				return ChatResponse{Response: resp}, true
			}
		}
		if turn.TimeWindow != "none" {
			if resp, ok := s.tryNormalizedRecentListeningSummary(ctx, turn, lowerMsg); ok {
				return resp, true
			}
		}
	case "stats":
		if turn.FollowupMode != "none" || strings.TrimSpace(turn.SubIntent) == "listening_interpretation" || strings.TrimSpace(turn.SubIntent) == "artist_dominance" {
			if resp, ok := s.tryNormalizedRecentListeningInterpretation(chatCtxOrBackground(ctx), resolved); ok {
				return ChatResponse{Response: resp}, true
			}
		}
		if turn.TimeWindow != "none" && (turn.QueryScope == "stats" || turn.QueryScope == "listening" || turn.QueryScope == "unknown" || turn.QueryScope == "general" || strings.TrimSpace(turn.SubIntent) == "artist_dominance") {
			if resp, ok := s.tryNormalizedArtistListeningStats(ctx, turn); ok {
				return ChatResponse{Response: resp}, true
			}
		}
		if turn.QueryScope == "library" {
			if resp, ok := s.handleLibraryFacetQuery(ctx, lowerMsg); ok {
				return ChatResponse{Response: resp}, true
			}
			if resp, ok := s.handleArtistLibraryStatsQuery(ctx, lowerMsg); ok {
				return ChatResponse{Response: resp}, true
			}
			if resp, ok := s.handleAlbumLibraryStatsQuery(ctx, lowerMsg); ok {
				return ChatResponse{Response: resp}, true
			}
			if resp, ok := s.tryNormalizedTopLibraryArtists(ctx, turn); ok {
				return ChatResponse{Response: resp}, true
			}
		}
	case "album_discovery":
		if turn.FollowupMode != "none" {
			if resp, ok := s.handleStructuredDiscoveredAlbumsAvailability(ctx, resolved); ok {
				return ChatResponse{Response: resp}, true
			}
			if resp, pendingAction, ok := s.handleStructuredDiscoveredAlbumsApply(ctx, resolved); ok {
				return ChatResponse{Response: resp, PendingAction: pendingAction}, true
			}
			if resp, ok := s.handleStructuredCreativeAlbumSetFollowUp(ctx, resolved); ok {
				return ChatResponse{Response: resp}, true
			}
		}
		if turn.QueryScope == "library" {
			if resp, ok := s.handleStructuredCreativeLibraryDiscovery(ctx, resolved); ok {
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
		if resp, ok := s.handleStructuredPlaylistInventory(ctx, resolved); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredSavedPlaylistVibe(ctx, resolved, history); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredSavedPlaylistArtistCoverage(ctx, resolved, history); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredSavedPlaylistAppend(ctx, resolved, history); ok {
			return resp, true
		}
		if resp, ok := s.handleStructuredSavedPlaylistRefresh(ctx, resolved, history); ok {
			return resp, true
		}
		if resp, ok := s.handleStructuredSavedPlaylistRepair(ctx, resolved, history); ok {
			return resp, true
		}
		if resp, ok := s.handleStructuredPlaylistQueueRequest(ctx, resolved, history); ok {
			return resp, true
		}
		if turn.FollowupMode == "none" {
			if resp, ok := s.tryNormalizedPlaylistCreate(ctx, msg, resolved); ok {
				return resp, true
			}
		}
		if resp, ok := s.handleStructuredPlaylistTracksQuery(ctx, resolved, history); ok {
			return ChatResponse{Response: resp}, true
		}
		if resp, ok := s.handleStructuredPlaylistAvailability(ctx, resolved); ok {
			return ChatResponse{Response: resp}, true
		}
	}

	return ChatResponse{}, false
}

func (s *Server) handleAlbumResultSetListeningFollowUp(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	outcome, ok := s.resolveAlbumResultSetListeningFollowUpOutcome(ctx, resolved)
	if !ok {
		return "", false
	}
	return renderAlbumResultSetListeningFollowUp(outcome)
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

func (s *Server) tryNormalizedRecentListeningInterpretation(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	if resolved == nil {
		return "", false
	}
	state, ok := getLastRecentListeningSummary(chatSessionIDFromContext(ctx))
	if !ok || state.updatedAt.IsZero() || time.Since(state.updatedAt) > llmContextRecentListeningTTL {
		return "", false
	}
	return describeStructuredRecentListeningInterpretation(state, resolved.Turn.SubIntent)
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

func (s *Server) tryNormalizedPlaylistCreate(ctx context.Context, rawMsg string, resolved *resolvedTurnContext) (ChatResponse, bool) {
	if resolved != nil && strings.TrimSpace(resolved.Turn.SubIntent) != "" {
		return ChatResponse{}, false
	}
	prompt := extractNormalizedPlaylistCreateIntent(rawMsg)
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
