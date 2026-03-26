package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"groovarr/internal/agent"
)

func trackCandidateResultSetCapability() resultSetCapability {
	return resultSetCapability{
		SetKind:    "track_candidates",
		Operations: []string{"describe_item", "pick_riskier", "pick_safer", "compare"},
		Selectors:  []string{"all", "top_n", "ordinal", "explicit_names", "item_key"},
	}
}

func artistCandidateResultSetCapability() resultSetCapability {
	return resultSetCapability{
		SetKind:    "artist_candidates",
		Operations: []string{"pick_riskier", "pick_safer", "compare"},
		Selectors:  []string{"all", "top_n", "ordinal", "explicit_names", "item_key"},
	}
}

func trackExecutionHandlers() []serverExecutionHandler {
	return []serverExecutionHandler{
		{
			name: "song_path_summary",
			canHandle: func(turn *Turn) bool {
				request := executionRequestFromTurn(turn)
				return strings.TrimSpace(request.SetKind) == "song_path" &&
					(strings.TrimSpace(request.Operation) == "describe_item" || strings.TrimSpace(request.Domain) == "track_discovery")
			},
			executeWithTurn: func(ctx context.Context, s *Server, _ []agent.Message, turn *Turn) (ChatResponse, bool) {
				if resp, ok := s.handleStructuredSongPathSummaryTurn(ctx, turn); ok {
					return ChatResponse{Response: resp}, true
				}
				return ChatResponse{}, false
			},
		},
		{
			name: "track_candidates_pick_variant",
			canHandle: func(turn *Turn) bool {
				request := executionRequestFromTurn(turn)
				return strings.TrimSpace(request.SetKind) == "track_candidates" &&
					(strings.TrimSpace(request.Operation) == "pick_riskier" || strings.TrimSpace(request.Operation) == "pick_safer")
			},
			executeWithTurn: func(ctx context.Context, s *Server, _ []agent.Message, turn *Turn) (ChatResponse, bool) {
				if resp, ok := s.handleStructuredTrackVariantPickTurn(ctx, turn); ok {
					return ChatResponse{Response: resp}, true
				}
				return ChatResponse{}, false
			},
		},
		{
			name: "track_candidates_compare",
			canHandle: func(turn *Turn) bool {
				request := executionRequestFromTurn(turn)
				return strings.TrimSpace(request.SetKind) == "track_candidates" &&
					strings.TrimSpace(request.Operation) == "compare"
			},
			executeWithTurn: func(ctx context.Context, s *Server, _ []agent.Message, turn *Turn) (ChatResponse, bool) {
				if resp, ok := s.handleStructuredTrackCompareTurn(ctx, turn); ok {
					return ChatResponse{Response: resp}, true
				}
				return ChatResponse{}, false
			},
		},
		{
			name: "track_candidates_description",
			canHandle: func(turn *Turn) bool {
				request := executionRequestFromTurn(turn)
				return strings.TrimSpace(request.SetKind) == "track_candidates" &&
					(strings.TrimSpace(request.Operation) == "describe_item" || strings.TrimSpace(request.Domain) == "track_discovery")
			},
			executeWithTurn: func(ctx context.Context, s *Server, _ []agent.Message, turn *Turn) (ChatResponse, bool) {
				if resp, ok := s.handleStructuredTrackDescriptionTurn(ctx, turn); ok {
					return ChatResponse{Response: resp}, true
				}
				if resp, ok := s.handleStructuredTrackSimilarityTurn(ctx, turn); ok {
					return ChatResponse{Response: resp}, true
				}
				return ChatResponse{}, false
			},
		},
		{
			name: "artist_candidates_pick_variant",
			canHandle: func(turn *Turn) bool {
				request := executionRequestFromTurn(turn)
				return strings.TrimSpace(request.SetKind) == "artist_candidates" &&
					(strings.TrimSpace(request.Operation) == "pick_riskier" || strings.TrimSpace(request.Operation) == "pick_safer")
			},
			executeWithTurn: func(ctx context.Context, s *Server, _ []agent.Message, turn *Turn) (ChatResponse, bool) {
				if resp, ok := s.handleStructuredArtistVariantPickTurn(ctx, turn); ok {
					return ChatResponse{Response: resp}, true
				}
				return ChatResponse{}, false
			},
		},
		{
			name: "artist_candidates_compare",
			canHandle: func(turn *Turn) bool {
				request := executionRequestFromTurn(turn)
				return strings.TrimSpace(request.SetKind) == "artist_candidates" &&
					strings.TrimSpace(request.Operation) == "compare"
			},
			executeWithTurn: func(ctx context.Context, s *Server, _ []agent.Message, turn *Turn) (ChatResponse, bool) {
				if resp, ok := s.handleStructuredArtistCompareTurn(ctx, turn); ok {
					return ChatResponse{Response: resp}, true
				}
				return ChatResponse{}, false
			},
		},
		{
			name: "artist_candidates_starting_album",
			canHandle: func(turn *Turn) bool {
				request := executionRequestFromTurn(turn)
				return strings.TrimSpace(request.SetKind) == "artist_candidates" &&
					strings.TrimSpace(request.Domain) == "artist_discovery"
			},
			executeWithTurn: func(ctx context.Context, s *Server, _ []agent.Message, turn *Turn) (ChatResponse, bool) {
				if resp, ok := s.handleStructuredArtistStartingAlbumTurn(ctx, turn); ok {
					return ChatResponse{Response: resp}, true
				}
				return ChatResponse{}, false
			},
		},
	}
}

func (s *Server) handleStructuredTrackVariantPick(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	if resolved == nil {
		return "", false
	}
	candidates, _, ok := trackCandidatesFromResolvedReference(chatSessionIDFromContext(ctx), resolved)
	if !ok || len(candidates) == 0 {
		return "", false
	}
	var pick trackCandidate
	switch strings.TrimSpace(resolved.Turn.SubIntent) {
	case "creative_risk_pick":
		pick = chooseRiskierTrackCandidate(candidates)
	case "creative_safe_pick":
		pick = chooseSaferTrackCandidate(candidates)
	default:
		return "", false
	}
	setLastFocusedResultItem(chatSessionIDFromContext(ctx), "track_candidates", normalizedTrackCandidateKey(pick))
	prefix := "The less expected one is"
	if strings.TrimSpace(resolved.Turn.SubIntent) == "creative_safe_pick" {
		prefix = "The safer one is"
	}
	return fmt.Sprintf("%s %s.", prefix, formatTrackCandidate(pick)), true
}

func (s *Server) handleStructuredTrackVariantPickTurn(ctx context.Context, turn *Turn) (string, bool) {
	return s.handleStructuredTrackVariantPick(ctx, turnToResolvedTurnContext(turn))
}

func (s *Server) handleStructuredArtistVariantPick(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	if resolved == nil {
		return "", false
	}
	candidates, _, ok := artistCandidatesFromResolvedReference(chatSessionIDFromContext(ctx), resolved)
	if !ok || len(candidates) == 0 {
		return "", false
	}
	var pick artistCandidate
	switch strings.TrimSpace(resolved.Turn.SubIntent) {
	case "creative_risk_pick":
		pick = chooseRiskierArtistCandidate(candidates)
	case "creative_safe_pick":
		pick = chooseSaferArtistCandidate(candidates)
	default:
		return "", false
	}
	setLastFocusedResultItem(chatSessionIDFromContext(ctx), "artist_candidates", normalizedArtistCandidateKey(pick))
	prefix := "The less expected one is"
	if strings.TrimSpace(resolved.Turn.SubIntent) == "creative_safe_pick" {
		prefix = "The safer one is"
	}
	return fmt.Sprintf("%s %s.", prefix, strings.TrimSpace(pick.Name)), true
}

func (s *Server) handleStructuredArtistVariantPickTurn(ctx context.Context, turn *Turn) (string, bool) {
	return s.handleStructuredArtistVariantPick(ctx, turnToResolvedTurnContext(turn))
}

func (s *Server) handleStructuredTrackCompare(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	if resolved == nil || strings.TrimSpace(resolved.Turn.ResultAction) != "compare" {
		return "", false
	}
	ref := resolved.resultReference()
	sessionID := chatSessionIDFromContext(ctx)
	memory := loadTurnSessionMemory(sessionID)
	candidates, _, _, _, found := memory.TrackCandidateSet()
	if !found || len(candidates) == 0 {
		selected, _, ok := trackCandidatesFromResolvedReference(sessionID, resolved)
		if !ok {
			return "", false
		}
		candidates = selected
	}
	primary, secondary, sameItem, ok := resolveTrackComparisonPair(ref, resolved.Turn, candidates)
	if !ok {
		if sameItem {
			return fmt.Sprintf("%s is already the first result, so there isn't a second track to compare it against.", formatTrackCandidate(primary)), true
		}
		return "", false
	}
	return renderTrackCandidateComparison(primary, secondary), true
}

func (s *Server) handleStructuredTrackCompareTurn(ctx context.Context, turn *Turn) (string, bool) {
	return s.handleStructuredTrackCompare(ctx, turnToResolvedTurnContext(turn))
}

func (s *Server) handleStructuredArtistCompare(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	if resolved == nil || strings.TrimSpace(resolved.Turn.ResultAction) != "compare" {
		return "", false
	}
	ref := resolved.resultReference()
	sessionID := chatSessionIDFromContext(ctx)
	memory := loadTurnSessionMemory(sessionID)
	candidates, _, _, found := memory.ArtistCandidateSet()
	if !found || len(candidates) == 0 {
		selected, _, ok := artistCandidatesFromResolvedReference(sessionID, resolved)
		if !ok {
			return "", false
		}
		candidates = selected
	}
	primary, secondary, sameItem, ok := resolveArtistComparisonPair(ref, resolved.Turn, candidates)
	if !ok {
		if sameItem {
			return fmt.Sprintf("%s is already the first result, so there isn't a second artist to compare against.", strings.TrimSpace(primary.Name)), true
		}
		return "", false
	}
	return renderArtistCandidateComparison(primary, secondary), true
}

func (s *Server) handleStructuredArtistCompareTurn(ctx context.Context, turn *Turn) (string, bool) {
	return s.handleStructuredArtistCompare(ctx, turnToResolvedTurnContext(turn))
}

func (s *Server) handleStructuredTrackSearch(ctx context.Context, rawMsg string, resolved *resolvedTurnContext) (string, bool) {
	if resolved == nil || strings.TrimSpace(resolved.Turn.SubIntent) != "track_search" {
		return "", false
	}
	queryText := strings.TrimSpace(resolved.Turn.PromptHint)
	if queryText == "" {
		queryText = strings.TrimSpace(rawMsg)
	}
	if queryText == "" {
		return "", false
	}
	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "textToTrack", map[string]interface{}{
		"queryText": queryText,
		"limit":     trackQueryLimit(resolved.Turn, 6),
	})
	if err != nil {
		return "", false
	}
	candidates, _, ok := parseTrackSearchCandidates(raw)
	if !ok {
		return "", false
	}
	if len(candidates) == 0 {
		return "I couldn't find strong track-level matches for that description in your library yet.", true
	}
	sessionID := chatSessionIDFromContext(ctx)
	setLastTrackCandidateSet(sessionID, "text_to_track", queryText, candidates)
	if len(candidates) == 1 {
		setLastFocusedResultItem(sessionID, "track_candidates", normalizedTrackCandidateKey(candidates[0]))
	}
	return renderTrackCandidateSet("Closest track matches in your library", candidates, 5), true
}

func (s *Server) handleStructuredTrackLibraryLookup(ctx context.Context, rawMsg string, resolved *resolvedTurnContext) (string, bool) {
	if resolved == nil {
		return "", false
	}
	lower := strings.ToLower(strings.TrimSpace(rawMsg))
	directLookup := isLibraryInventoryLookupPrompt(lower)
	objectLookup := strings.TrimSpace(resolved.ConversationObjectKind) == "library_inventory_lookup" ||
		strings.TrimSpace(resolved.ActiveFocusKind) == "library_inventory_lookup"
	stickyLookup := objectLookup &&
		strings.TrimSpace(resolved.Turn.ReferenceTarget) == "previous_results" &&
		strings.TrimSpace(resolved.Turn.QueryScope) == "library"
	if !directLookup && !stickyLookup {
		return "", false
	}
	if !stickyLookup && strings.TrimSpace(resolved.Turn.QueryScope) != "library" {
		return "", false
	}
	if !stickyLookup && strings.TrimSpace(resolved.Turn.Intent) != "track_discovery" {
		return "", false
	}
	title := strings.TrimSpace(resolved.Turn.TrackTitle)
	artist := strings.TrimSpace(resolved.Turn.ArtistName)
	if title == "" {
		query := extractInventoryLookupQueryFromTurn(resolved.Turn)
		if query == "" {
			query = extractLibraryLookupQuery(rawMsg)
		}
		if query == "" && stickyLookup {
			query = extractInventoryLookupContinuationQuery(rawMsg)
		}
		if query == "" {
			return "", false
		}
		if parsedTitle, parsedArtist, ok := splitLookupQueryArtist(query); ok {
			title = parsedTitle
			if artist == "" {
				artist = parsedArtist
			}
		} else {
			title = query
		}
	}
	if title == "" {
		return "", false
	}
	args := map[string]interface{}{
		"queryText": title,
		"limit":     10,
	}
	if artist != "" {
		args["artistName"] = artist
	}
	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "tracks", args)
	if err != nil {
		return "", false
	}
	candidates, ok := parseTrackLookupCandidates(raw)
	if !ok {
		return "", false
	}
	match, found := findExactTrackLookupMatch(title, artist, candidates)
	sessionID := chatSessionIDFromContext(ctx)
	setLastActiveFocus(sessionID, "library_inventory_lookup", "track_mode")
	if !found {
		if artist != "" {
			return fmt.Sprintf("No, I couldn't find %s by %s in your library.", title, artist), true
		}
		return fmt.Sprintf("No, I couldn't find %s in your library.", title), true
	}
	setLastFocusedResultItem(sessionID, "track_candidates", normalizedTrackCandidateKey(match))
	if artist != "" {
		return fmt.Sprintf("Yes, you have %s by %s in your library.", match.Title, match.ArtistName), true
	}
	return fmt.Sprintf("Yes, you have %s in your library.", match.Title), true
}

func (s *Server) handleStructuredTrackLibraryLookupTurn(ctx context.Context, turn *Turn) (string, bool) {
	if turn == nil {
		return "", false
	}
	return s.handleStructuredTrackLibraryLookup(ctx, turn.UserMessage, turnToResolvedTurnContext(turn))
}

func (s *Server) handleStructuredTrackSearchTurn(ctx context.Context, turn *Turn) (string, bool) {
	if turn == nil {
		return "", false
	}
	return s.handleStructuredTrackSearch(ctx, turn.UserMessage, turnToResolvedTurnContext(turn))
}

func (s *Server) handleStructuredSongPathSummary(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	if resolved == nil || strings.TrimSpace(resolved.Turn.SubIntent) != "song_path_summary" {
		return "", false
	}
	state, ok := loadTurnSessionMemory(chatSessionIDFromContext(ctx)).SongPath()
	if !ok || len(state.path) == 0 {
		return "", false
	}
	middle := state.path[len(state.path)/2]
	setLastFocusedResultItem(chatSessionIDFromContext(ctx), "song_path", normalizedSongPathTrackKey(middle))
	args := map[string]interface{}{
		"neighborLimit": 4,
	}
	if middle.ID != "" {
		args["trackId"] = middle.ID
	} else {
		args["trackTitle"] = middle.Title
		if middle.ArtistName != "" {
			args["artistName"] = middle.ArtistName
		}
	}
	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "describeTrackSound", args)
	if err == nil {
		if outcome, ok := parseTrackDescriptionOutcome(raw); ok && strings.TrimSpace(outcome.Title) != "" {
			return renderSongPathSummaryOutcome(state, outcome), true
		}
	}
	return renderSongPathFallback(state), true
}

func (s *Server) handleStructuredSongPathSummaryTurn(ctx context.Context, turn *Turn) (string, bool) {
	return s.handleStructuredSongPathSummary(ctx, turnToResolvedTurnContext(turn))
}

func (s *Server) handleStructuredTrackSimilarity(ctx context.Context, rawMsg string, resolved *resolvedTurnContext) (string, bool) {
	if resolved == nil || strings.TrimSpace(resolved.Turn.SubIntent) != "track_similarity" {
		return "", false
	}
	seed, ok := s.resolveTrackSeedForExecution(ctx, resolved)
	if !ok {
		return "", false
	}
	args := map[string]interface{}{
		"limit": trackQueryLimit(resolved.Turn, 5),
	}
	if seed.ID != "" {
		args["seedTrackId"] = seed.ID
	} else {
		args["seedTrackTitle"] = seed.Title
		if seed.ArtistName != "" {
			args["seedArtistName"] = seed.ArtistName
		}
	}
	raw, err := executeToolWithSimilarity(ctx, s.resolver, s.similarity, s.embeddingsURL, "similarTracks", args)
	if err != nil {
		return "", false
	}
	candidates, ok := parseSimilarTrackCandidates(raw)
	if !ok {
		return "", false
	}
	if len(candidates) == 0 {
		if seed.Title != "" {
			return fmt.Sprintf("I couldn't find convincing nearby tracks for %s in your library yet.", formatTrackSeed(seed)), true
		}
		return "I couldn't find convincing nearby tracks for that seed in your library yet.", true
	}
	sessionID := chatSessionIDFromContext(ctx)
	label := seed.Title
	if label == "" {
		label = strings.TrimSpace(rawMsg)
	}
	setLastTrackCandidateSet(sessionID, "similar_tracks", label, candidates)
	if len(candidates) == 1 {
		setLastFocusedResultItem(sessionID, "track_candidates", normalizedTrackCandidateKey(candidates[0]))
	}
	return renderTrackCandidateSet(fmt.Sprintf("Nearest tracks to %s", formatTrackSeed(seed)), candidates, 5), true
}

func (s *Server) handleStructuredTrackSimilarityTurn(ctx context.Context, turn *Turn) (string, bool) {
	if turn == nil {
		return "", false
	}
	return s.handleStructuredTrackSimilarity(ctx, turn.UserMessage, turnToResolvedTurnContext(turn))
}

func (s *Server) handleStructuredTrackDescription(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	if resolved == nil || strings.TrimSpace(resolved.Turn.SubIntent) != "track_description" {
		return "", false
	}
	seed, ok := s.resolveTrackSeedForExecution(ctx, resolved)
	if !ok {
		return "", false
	}
	args := map[string]interface{}{
		"neighborLimit": 4,
	}
	if seed.ID != "" {
		args["trackId"] = seed.ID
	} else {
		args["trackTitle"] = seed.Title
		if seed.ArtistName != "" {
			args["artistName"] = seed.ArtistName
		}
	}
	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "describeTrackSound", args)
	if err != nil {
		return "", false
	}
	outcome, ok := parseTrackDescriptionOutcome(raw)
	if !ok || strings.TrimSpace(outcome.Title) == "" {
		return "", false
	}
	sessionID := chatSessionIDFromContext(ctx)
	setLastFocusedResultItem(sessionID, "track_candidates", normalizedTrackCandidateKey(trackCandidate{
		ID:         outcome.ID,
		Title:      outcome.Title,
		ArtistName: outcome.ArtistName,
		AlbumName:  outcome.AlbumName,
	}))
	return renderTrackDescriptionOutcome(outcome), true
}

func (s *Server) handleStructuredTrackDescriptionTurn(ctx context.Context, turn *Turn) (string, bool) {
	return s.handleStructuredTrackDescription(ctx, turnToResolvedTurnContext(turn))
}

func (s *Server) handleStructuredArtistSimilarity(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	if resolved == nil || strings.TrimSpace(resolved.Turn.SubIntent) != "artist_similarity" {
		return "", false
	}
	artistName := strings.TrimSpace(resolved.Turn.ArtistName)
	if artistName == "" {
		return "", false
	}
	raw, err := executeToolWithSimilarity(ctx, s.resolver, s.similarity, s.embeddingsURL, "similarArtists", map[string]interface{}{
		"seedArtist": artistName,
		"limit":      trackQueryLimit(resolved.Turn, 5),
	})
	if err != nil {
		return "", false
	}
	candidates, ok := parseArtistCandidates(raw)
	if !ok {
		return "", false
	}
	if len(candidates) == 0 {
		return fmt.Sprintf("I couldn't find strong artist neighbors for %s in your library yet.", artistName), true
	}
	sessionID := chatSessionIDFromContext(ctx)
	setLastArtistCandidateSet(sessionID, artistName, candidates)
	if len(candidates) == 1 {
		setLastFocusedResultItem(sessionID, "artist_candidates", normalizedArtistCandidateKey(candidates[0]))
	}
	return renderArtistCandidateSet(fmt.Sprintf("Nearest artists to %s in your library", artistName), candidates, 5), true
}

func (s *Server) handleStructuredArtistSimilarityTurn(ctx context.Context, turn *Turn) (string, bool) {
	return s.handleStructuredArtistSimilarity(ctx, turnToResolvedTurnContext(turn))
}

func (s *Server) handleStructuredArtistStartingAlbum(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	if resolved == nil || strings.TrimSpace(resolved.Turn.SubIntent) != "artist_starting_album" {
		return "", false
	}
	artist, ok := resolveArtistSeed(ctx, resolved)
	if !ok {
		return "", false
	}
	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "albums", map[string]interface{}{
		"artistName": artist.Name,
		"sortBy":     "rating",
		"limit":      3,
	})
	if err != nil {
		return "", false
	}
	albums, ok := parseArtistAlbums(raw)
	if !ok {
		return "", false
	}
	if len(albums) == 0 {
		return fmt.Sprintf("I couldn't find a solid starting album for %s in your library yet.", artist.Name), true
	}
	setLastFocusedResultItem(chatSessionIDFromContext(ctx), "artist_candidates", normalizedArtistCandidateKey(artist))
	return renderRouteBulletList(fmt.Sprintf("A good place to start with %s from your library", artist.Name), albums, 3), true
}

func (s *Server) handleStructuredArtistStartingAlbumTurn(ctx context.Context, turn *Turn) (string, bool) {
	return s.handleStructuredArtistStartingAlbum(ctx, turnToResolvedTurnContext(turn))
}

func (s *Server) lookupTopRatedArtistAlbumCandidate(ctx context.Context, artistName string) (creativeAlbumCandidate, bool) {
	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "albums", map[string]interface{}{
		"artistName": artistName,
		"sortBy":     "rating",
		"limit":      1,
	})
	if err != nil {
		return creativeAlbumCandidate{}, false
	}
	albums, ok := parseArtistAlbumCandidates(raw)
	if !ok || len(albums) == 0 {
		return creativeAlbumCandidate{}, false
	}
	return albums[0], true
}

type trackSeed struct {
	ID         string
	Title      string
	ArtistName string
	AlbumName  string
}

func (s *Server) resolveTrackSeedForExecution(ctx context.Context, resolved *resolvedTurnContext) (trackSeed, bool) {
	seed, ok := resolveTrackSeed(ctx, resolved)
	if !ok {
		return trackSeed{}, false
	}
	if seed.ID != "" || seed.Title == "" || seed.ArtistName != "" {
		return seed, true
	}
	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "tracks", map[string]interface{}{
		"queryText": seed.Title,
		"limit":     5,
	})
	if err != nil {
		return seed, true
	}
	candidates, ok := parseTrackLookupCandidates(raw)
	if !ok || len(candidates) == 0 {
		return seed, true
	}
	if match, ok := pickTrackCandidateForSeed(seed.Title, candidates); ok {
		return trackSeed{
			ID:         match.ID,
			Title:      match.Title,
			ArtistName: match.ArtistName,
			AlbumName:  match.AlbumName,
		}, true
	}
	return seed, true
}

func resolveTrackSeed(ctx context.Context, resolved *resolvedTurnContext) (trackSeed, bool) {
	if resolved == nil {
		return trackSeed{}, false
	}
	if resolved.Turn.FollowupMode != "none" && strings.TrimSpace(resolved.Turn.ReferenceTarget) == "previous_results" {
		if seed, ok := resolveTrackSeedFromReference(chatSessionIDFromContext(ctx), resolved); ok {
			return seed, true
		}
	}
	if title := strings.TrimSpace(resolved.Turn.TrackTitle); title != "" {
		return trackSeed{
			Title:      title,
			ArtistName: strings.TrimSpace(resolved.Turn.ArtistName),
		}, true
	}
	candidates, _, ok := trackCandidatesFromResolvedReference(chatSessionIDFromContext(ctx), resolved)
	if !ok || len(candidates) == 0 {
		return trackSeed{}, false
	}
	selected := selectTrackSeedCandidateFromIntent(resolved.Turn, candidates)
	return trackSeed{
		ID:         selected.ID,
		Title:      selected.Title,
		ArtistName: selected.ArtistName,
		AlbumName:  selected.AlbumName,
	}, true
}

func resolveTrackSeedFromReference(sessionID string, resolved *resolvedTurnContext) (trackSeed, bool) {
	if resolved == nil {
		return trackSeed{}, false
	}
	if candidates, _, ok := trackCandidatesFromResolvedReference(sessionID, resolved); ok && len(candidates) > 0 {
		selected := selectTrackSeedCandidateFromIntent(resolved.Turn, candidates)
		return trackSeed{
			ID:         selected.ID,
			Title:      selected.Title,
			ArtistName: selected.ArtistName,
			AlbumName:  selected.AlbumName,
		}, true
	}
	if strings.TrimSpace(resolved.ResolvedReferenceKind) != "song_path" {
		return trackSeed{}, false
	}
	state, ok := loadTurnSessionMemory(sessionID).SongPath()
	if !ok || len(state.path) == 0 {
		return trackSeed{}, false
	}
	if key := strings.TrimSpace(resolved.ResolvedItemKey); key != "" {
		for _, candidate := range state.path {
			if normalizedSongPathTrackKey(candidate) == key {
				return trackSeed{
					ID:         candidate.ID,
					Title:      candidate.Title,
					ArtistName: candidate.ArtistName,
					AlbumName:  candidate.AlbumName,
				}, true
			}
		}
	}
	middle := state.path[len(state.path)/2]
	return trackSeed{
		ID:         middle.ID,
		Title:      middle.Title,
		ArtistName: middle.ArtistName,
		AlbumName:  middle.AlbumName,
	}, true
}

func renderSongPathSummaryOutcome(state songPathState, outcome trackDescriptionOutcome) string {
	return fmt.Sprintf(
		"The middle stretch of that path pivots through %s. Around the center, it feels %s and helps bridge %s toward %s.",
		formatTrackDescriptionTarget(outcome.Title, outcome.ArtistName),
		summarizeTrackDescriptionOutcome(outcome),
		formatSongPathTrack(state.start),
		formatSongPathTrack(state.end),
	)
}

func renderSongPathFallback(state songPathState) string {
	middle := state.path[len(state.path)/2]
	return fmt.Sprintf(
		"The middle stretch of that path pivots through %s, bridging %s toward %s.",
		formatSongPathTrack(middle),
		formatSongPathTrack(state.start),
		formatSongPathTrack(state.end),
	)
}

func resolveArtistSeed(ctx context.Context, resolved *resolvedTurnContext) (artistCandidate, bool) {
	if resolved == nil {
		return artistCandidate{}, false
	}
	if resolved.Turn.FollowupMode != "none" && strings.TrimSpace(resolved.Turn.ReferenceTarget) == "previous_results" {
		candidates, _, ok := artistCandidatesFromResolvedReference(chatSessionIDFromContext(ctx), resolved)
		if ok && len(candidates) > 0 {
			return selectArtistSeedCandidateFromIntent(resolved.Turn, candidates), true
		}
	}
	if name := strings.TrimSpace(resolved.Turn.ArtistName); name != "" {
		return artistCandidate{Name: name}, true
	}
	candidates, _, ok := artistCandidatesFromResolvedReference(chatSessionIDFromContext(ctx), resolved)
	if !ok || len(candidates) == 0 {
		return artistCandidate{}, false
	}
	return selectArtistSeedCandidateFromIntent(resolved.Turn, candidates), true
}

func selectTrackSeedCandidateFromIntent(turn normalizedTurn, candidates []trackCandidate) trackCandidate {
	if len(candidates) == 0 {
		return trackCandidate{}
	}
	switch {
	case strings.TrimSpace(turn.ReferenceQualifier) == "riskier" || strings.TrimSpace(turn.SubIntent) == "creative_risk_pick":
		return chooseRiskierTrackCandidate(candidates)
	case strings.TrimSpace(turn.ReferenceQualifier) == "safer" || strings.TrimSpace(turn.SubIntent) == "creative_safe_pick":
		return chooseSaferTrackCandidate(candidates)
	default:
		return candidates[0]
	}
}

func selectArtistSeedCandidateFromIntent(turn normalizedTurn, candidates []artistCandidate) artistCandidate {
	if len(candidates) == 0 {
		return artistCandidate{}
	}
	switch {
	case strings.TrimSpace(turn.ReferenceQualifier) == "riskier" || strings.TrimSpace(turn.SubIntent) == "creative_risk_pick":
		return chooseRiskierArtistCandidate(candidates)
	case strings.TrimSpace(turn.ReferenceQualifier) == "safer" || strings.TrimSpace(turn.SubIntent) == "creative_safe_pick":
		return chooseSaferArtistCandidate(candidates)
	default:
		return candidates[0]
	}
}

func resolveTrackComparisonPair(ref resolvedResultReference, turn normalizedTurn, candidates []trackCandidate) (trackCandidate, trackCandidate, bool, bool) {
	if len(candidates) < 2 {
		return trackCandidate{}, trackCandidate{}, false, false
	}
	primary, ok := selectTrackPrimaryComparisonCandidate(ref, turn, candidates)
	if !ok {
		return trackCandidate{}, trackCandidate{}, false, false
	}
	secondary, sameItem, ok := selectTrackComparisonCandidate(turn, candidates, normalizedTrackCandidateKey(primary))
	if !ok {
		return primary, trackCandidate{}, sameItem, false
	}
	return primary, secondary, false, true
}

func resolveArtistComparisonPair(ref resolvedResultReference, turn normalizedTurn, candidates []artistCandidate) (artistCandidate, artistCandidate, bool, bool) {
	if len(candidates) < 2 {
		return artistCandidate{}, artistCandidate{}, false, false
	}
	primary, ok := selectArtistPrimaryComparisonCandidate(ref, turn, candidates)
	if !ok {
		return artistCandidate{}, artistCandidate{}, false, false
	}
	secondary, sameItem, ok := selectArtistComparisonCandidate(turn, candidates, normalizedArtistCandidateKey(primary))
	if !ok {
		return primary, artistCandidate{}, sameItem, false
	}
	return primary, secondary, false, true
}

func selectTrackPrimaryComparisonCandidate(ref resolvedResultReference, turn normalizedTurn, candidates []trackCandidate) (trackCandidate, bool) {
	if len(candidates) == 0 {
		return trackCandidate{}, false
	}
	if ref.ResolvedItemKey != "" {
		for _, candidate := range candidates {
			if normalizedTrackCandidateKey(candidate) == ref.ResolvedItemKey {
				return candidate, true
			}
		}
	}
	if selected, ok := selectTrackCandidates(ref, candidates); ok && len(selected) > 0 {
		return selectTrackSeedCandidateFromIntent(turn, selected), true
	}
	return selectTrackSeedCandidateFromIntent(turn, candidates), true
}

func selectArtistPrimaryComparisonCandidate(ref resolvedResultReference, turn normalizedTurn, candidates []artistCandidate) (artistCandidate, bool) {
	if len(candidates) == 0 {
		return artistCandidate{}, false
	}
	if ref.ResolvedItemKey != "" {
		for _, candidate := range candidates {
			if normalizedArtistCandidateKey(candidate) == ref.ResolvedItemKey {
				return candidate, true
			}
		}
	}
	if selected, ok := selectArtistCandidates(ref, candidates); ok && len(selected) > 0 {
		return selectArtistSeedCandidateFromIntent(turn, selected), true
	}
	return selectArtistSeedCandidateFromIntent(turn, candidates), true
}

func selectTrackComparisonCandidate(turn normalizedTurn, candidates []trackCandidate, excludeKey string) (trackCandidate, bool, bool) {
	selected, ok := selectTrackCandidates(resolvedResultReference{
		resultReference: resultReference{
			Selection: resultSelection{
				Mode:  strings.TrimSpace(turn.CompareSelectionMode),
				Value: strings.TrimSpace(turn.CompareSelectionValue),
			},
		},
	}, candidates)
	if !ok {
		return trackCandidate{}, false, false
	}
	sameItem := false
	for _, candidate := range selected {
		if normalizedTrackCandidateKey(candidate) != excludeKey {
			return candidate, false, true
		}
		sameItem = true
	}
	return trackCandidate{}, sameItem, false
}

func selectArtistComparisonCandidate(turn normalizedTurn, candidates []artistCandidate, excludeKey string) (artistCandidate, bool, bool) {
	selected, ok := selectArtistCandidates(resolvedResultReference{
		resultReference: resultReference{
			Selection: resultSelection{
				Mode:  strings.TrimSpace(turn.CompareSelectionMode),
				Value: strings.TrimSpace(turn.CompareSelectionValue),
			},
		},
	}, candidates)
	if !ok {
		return artistCandidate{}, false, false
	}
	sameItem := false
	for _, candidate := range selected {
		if normalizedArtistCandidateKey(candidate) != excludeKey {
			return candidate, false, true
		}
		sameItem = true
	}
	return artistCandidate{}, sameItem, false
}

func trackCandidatesFromResolvedReference(sessionID string, resolved *resolvedTurnContext) ([]trackCandidate, string, bool) {
	candidates, _, mode, _, ok := loadTurnSessionMemory(sessionID).TrackCandidateSet()
	if !ok || len(candidates) == 0 {
		return nil, "", false
	}
	ref := resolved.resultReference()
	if ref.ResolvedItemKey != "" {
		for _, candidate := range candidates {
			if normalizedTrackCandidateKey(candidate) == ref.ResolvedItemKey {
				return []trackCandidate{candidate}, mode, true
			}
		}
	}
	selected, ok := selectTrackCandidates(ref, candidates)
	if !ok {
		return nil, "", false
	}
	return selected, mode, true
}

func artistCandidatesFromResolvedReference(sessionID string, resolved *resolvedTurnContext) ([]artistCandidate, string, bool) {
	candidates, _, queryText, ok := loadTurnSessionMemory(sessionID).ArtistCandidateSet()
	if !ok || len(candidates) == 0 {
		return nil, "", false
	}
	ref := resolved.resultReference()
	if ref.ResolvedItemKey != "" {
		for _, candidate := range candidates {
			if normalizedArtistCandidateKey(candidate) == ref.ResolvedItemKey {
				return []artistCandidate{candidate}, queryText, true
			}
		}
	}
	selected, ok := selectArtistCandidates(ref, candidates)
	if !ok {
		return nil, "", false
	}
	return selected, queryText, true
}

func selectTrackCandidates(ref resolvedResultReference, candidates []trackCandidate) ([]trackCandidate, bool) {
	if len(candidates) == 0 {
		return nil, false
	}
	switch strings.TrimSpace(ref.Selection.Mode) {
	case "", "none", "all":
		return candidates, true
	case "top_n":
		if count, ok := parseTurnSelectionCount(ref.Selection.Value); ok && count > 0 {
			if count > len(candidates) {
				count = len(candidates)
			}
			return candidates[:count], true
		}
	case "ordinal":
		if selected := selectTrackCandidatesByOrdinal(candidates, ref.Selection.Value); len(selected) > 0 {
			return selected, true
		}
	case "explicit_names":
		if selected := selectTrackCandidatesByName(candidates, ref.Selection.Value); len(selected) > 0 {
			return selected, true
		}
	case "item_key":
		if ref.ResolvedItemKey == "" {
			return nil, false
		}
		for _, candidate := range candidates {
			if normalizedTrackCandidateKey(candidate) == ref.ResolvedItemKey {
				return []trackCandidate{candidate}, true
			}
		}
	}
	return nil, false
}

func selectArtistCandidates(ref resolvedResultReference, candidates []artistCandidate) ([]artistCandidate, bool) {
	if len(candidates) == 0 {
		return nil, false
	}
	switch strings.TrimSpace(ref.Selection.Mode) {
	case "", "none", "all":
		return candidates, true
	case "top_n":
		if count, ok := parseTurnSelectionCount(ref.Selection.Value); ok && count > 0 {
			if count > len(candidates) {
				count = len(candidates)
			}
			return candidates[:count], true
		}
	case "ordinal":
		if selected := selectArtistCandidatesByOrdinal(candidates, ref.Selection.Value); len(selected) > 0 {
			return selected, true
		}
	case "explicit_names":
		if selected := selectArtistCandidatesByName(candidates, ref.Selection.Value); len(selected) > 0 {
			return selected, true
		}
	case "item_key":
		if ref.ResolvedItemKey == "" {
			return nil, false
		}
		for _, candidate := range candidates {
			if normalizedArtistCandidateKey(candidate) == ref.ResolvedItemKey {
				return []artistCandidate{candidate}, true
			}
		}
	}
	return nil, false
}

func selectTrackCandidatesByOrdinal(candidates []trackCandidate, raw string) []trackCandidate {
	normalized := normalizeDiscoveredAlbumRankList(raw)
	if normalized == "" {
		return nil
	}
	var selected []trackCandidate
	for _, field := range strings.Split(normalized, ",") {
		index, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil || index <= 0 || index > len(candidates) {
			continue
		}
		selected = append(selected, candidates[index-1])
	}
	return selected
}

func selectArtistCandidatesByOrdinal(candidates []artistCandidate, raw string) []artistCandidate {
	normalized := normalizeDiscoveredAlbumRankList(raw)
	if normalized == "" {
		return nil
	}
	var selected []artistCandidate
	for _, field := range strings.Split(normalized, ",") {
		index, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil || index <= 0 || index > len(candidates) {
			continue
		}
		selected = append(selected, candidates[index-1])
	}
	return selected
}

func selectTrackCandidatesByName(candidates []trackCandidate, raw string) []trackCandidate {
	query := normalizeReferenceText(raw)
	if query == "" {
		return nil
	}
	selected := make([]trackCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		label := normalizeReferenceText(candidate.Title + " " + candidate.ArtistName)
		if strings.Contains(label, query) || strings.Contains(query, normalizeReferenceText(candidate.Title)) {
			selected = append(selected, candidate)
		}
	}
	return selected
}

func selectArtistCandidatesByName(candidates []artistCandidate, raw string) []artistCandidate {
	query := normalizeReferenceText(raw)
	if query == "" {
		return nil
	}
	selected := make([]artistCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		label := normalizeReferenceText(candidate.Name)
		if strings.Contains(label, query) || strings.Contains(query, label) {
			selected = append(selected, candidate)
		}
	}
	return selected
}

func normalizedTrackCandidateKey(candidate trackCandidate) string {
	if id := strings.TrimSpace(candidate.ID); id != "" {
		return "track:" + id
	}
	return "track:" + normalizeReferenceText(candidate.Title+" "+candidate.ArtistName)
}

func pickTrackCandidateForSeed(title string, candidates []trackCandidate) (trackCandidate, bool) {
	query := normalizeReferenceText(title)
	if query == "" || len(candidates) == 0 {
		return trackCandidate{}, false
	}
	for _, candidate := range candidates {
		if normalizeReferenceText(candidate.Title) == query {
			return candidate, true
		}
	}
	for _, candidate := range candidates {
		if strings.Contains(normalizeReferenceText(candidate.Title), query) || strings.Contains(query, normalizeReferenceText(candidate.Title)) {
			return candidate, true
		}
	}
	return trackCandidate{}, false
}

func findExactTrackLookupMatch(title, artist string, candidates []trackCandidate) (trackCandidate, bool) {
	titleKey := normalizeReferenceText(title)
	artistKey := normalizeReferenceText(artist)
	if titleKey == "" {
		return trackCandidate{}, false
	}
	for _, candidate := range candidates {
		if normalizeReferenceText(candidate.Title) != titleKey {
			continue
		}
		if artistKey != "" && normalizeReferenceText(candidate.ArtistName) != artistKey {
			continue
		}
		return candidate, true
	}
	return trackCandidate{}, false
}

func normalizedArtistCandidateKey(candidate artistCandidate) string {
	if id := strings.TrimSpace(candidate.ID); id != "" {
		return "artist:" + id
	}
	return "artist:" + normalizeReferenceText(candidate.Name)
}

func trackQueryLimit(turn normalizedTurn, defaultLimit int) int {
	if defaultLimit <= 0 {
		defaultLimit = 5
	}
	if strings.TrimSpace(turn.SelectionMode) == "top_n" {
		if count, ok := parseTurnSelectionCount(turn.SelectionValue); ok && count > 0 && count <= 10 {
			return count
		}
	}
	return defaultLimit
}

func formatTrackSeed(seed trackSeed) string {
	label := strings.TrimSpace(seed.Title)
	if label == "" {
		return "that track"
	}
	if artist := strings.TrimSpace(seed.ArtistName); artist != "" {
		label += " by " + artist
	}
	return label
}

func renderTrackCandidateSet(prefix string, candidates []trackCandidate, limit int) string {
	items := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		label := strings.TrimSpace(candidate.Title)
		if label == "" {
			continue
		}
		if strings.TrimSpace(candidate.ArtistName) != "" {
			label += " by " + strings.TrimSpace(candidate.ArtistName)
		}
		if strings.TrimSpace(candidate.AlbumName) != "" {
			label += " • " + strings.TrimSpace(candidate.AlbumName)
		}
		items = append(items, label)
	}
	return renderRouteBulletList(prefix, items, limit)
}

func formatTrackCandidate(candidate trackCandidate) string {
	label := strings.TrimSpace(candidate.Title)
	if strings.TrimSpace(candidate.ArtistName) != "" {
		label += " by " + strings.TrimSpace(candidate.ArtistName)
	}
	return label
}

func renderArtistCandidateSet(prefix string, candidates []artistCandidate, limit int) string {
	items := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		label := strings.TrimSpace(candidate.Name)
		if label == "" {
			continue
		}
		items = append(items, label)
	}
	return renderRouteBulletList(prefix, items, limit)
}

type trackDescriptionOutcome struct {
	ID          string
	Title       string
	ArtistName  string
	AlbumName   string
	ProfileText string
	TopMoods    []string
	TopFeatures []string
	Neighbors   []string
}

func renderTrackDescriptionOutcome(outcome trackDescriptionOutcome) string {
	label := strings.TrimSpace(outcome.Title)
	if strings.TrimSpace(outcome.ArtistName) != "" {
		label += " by " + strings.TrimSpace(outcome.ArtistName)
	}
	parts := make([]string, 0, 3)
	if text := strings.TrimSpace(outcome.ProfileText); text != "" {
		parts = append(parts, text)
	}
	if len(outcome.Neighbors) > 0 {
		parts = append(parts, "Nearby tracks: "+strings.Join(outcome.Neighbors, ", "))
	}
	if len(parts) == 0 {
		return label + "."
	}
	return label + ": " + strings.Join(parts, " ")
}

func renderTrackCandidateComparison(primary, secondary trackCandidate) string {
	parts := []string{
		fmt.Sprintf("Selected anchor: %s", formatTrackCandidate(primary)),
		fmt.Sprintf("comparison target: %s", formatTrackCandidate(secondary)),
	}
	if primary.PlayCount != secondary.PlayCount {
		parts = append(parts, fmt.Sprintf("plays: %d vs %d", primary.PlayCount, secondary.PlayCount))
	}
	if primary.Score != 0 || secondary.Score != 0 {
		parts = append(parts, fmt.Sprintf("similarity score: %.2f vs %.2f", primary.Score, secondary.Score))
	}
	return strings.Join(parts, "; ") + "."
}

func renderArtistCandidateComparison(primary, secondary artistCandidate) string {
	parts := []string{
		fmt.Sprintf("Selected anchor: %s", strings.TrimSpace(primary.Name)),
		fmt.Sprintf("comparison target: %s", strings.TrimSpace(secondary.Name)),
	}
	if primary.PlayCount != secondary.PlayCount {
		parts = append(parts, fmt.Sprintf("plays: %d vs %d", primary.PlayCount, secondary.PlayCount))
	}
	if primary.Rating != secondary.Rating {
		parts = append(parts, fmt.Sprintf("rating: %d vs %d", primary.Rating, secondary.Rating))
	}
	return strings.Join(parts, "; ") + "."
}

func formatTrackDescriptionTarget(title, artistName string) string {
	label := strings.TrimSpace(title)
	if strings.TrimSpace(artistName) != "" {
		label += " by " + strings.TrimSpace(artistName)
	}
	return strings.TrimSpace(label)
}

func summarizeTrackDescriptionOutcome(outcome trackDescriptionOutcome) string {
	if text := strings.TrimSpace(outcome.ProfileText); text != "" {
		return text
	}
	switch {
	case len(outcome.TopMoods) > 0 && len(outcome.TopFeatures) > 0:
		return strings.Join(outcome.TopMoods, ", ") + " with " + strings.Join(outcome.TopFeatures, ", ")
	case len(outcome.TopMoods) > 0:
		return strings.Join(outcome.TopMoods, ", ")
	case len(outcome.TopFeatures) > 0:
		return strings.Join(outcome.TopFeatures, ", ")
	default:
		return "like the pivot point of the bridge"
	}
}

func chooseRiskierTrackCandidate(candidates []trackCandidate) trackCandidate {
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.PlayCount < best.PlayCount {
			best = candidate
			continue
		}
		if candidate.PlayCount == best.PlayCount && candidate.Score < best.Score {
			best = candidate
		}
	}
	return best
}

func chooseSaferTrackCandidate(candidates []trackCandidate) trackCandidate {
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.PlayCount > best.PlayCount {
			best = candidate
			continue
		}
		if candidate.PlayCount == best.PlayCount && candidate.Score > best.Score {
			best = candidate
		}
	}
	return best
}

func chooseRiskierArtistCandidate(candidates []artistCandidate) artistCandidate {
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.PlayCount < best.PlayCount {
			best = candidate
			continue
		}
		if candidate.PlayCount == best.PlayCount && candidate.Rating < best.Rating {
			best = candidate
		}
	}
	return best
}

func chooseSaferArtistCandidate(candidates []artistCandidate) artistCandidate {
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.PlayCount > best.PlayCount {
			best = candidate
			continue
		}
		if candidate.PlayCount == best.PlayCount && candidate.Rating > best.Rating {
			best = candidate
		}
	}
	return best
}

func parseTrackSearchCandidates(raw string) ([]trackCandidate, string, bool) {
	var parsed struct {
		Data struct {
			TextToTrack struct {
				Warning string `json:"warning"`
				Matches []struct {
					ID         string   `json:"id"`
					Title      string   `json:"title"`
					ArtistName string   `json:"artistName"`
					AlbumName  string   `json:"albumName"`
					Similarity *float64 `json:"similarity"`
				} `json:"matches"`
			} `json:"textToTrack"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, "", false
	}
	candidates := make([]trackCandidate, 0, len(parsed.Data.TextToTrack.Matches))
	for _, match := range parsed.Data.TextToTrack.Matches {
		candidate := trackCandidate{
			ID:         strings.TrimSpace(match.ID),
			Title:      strings.TrimSpace(match.Title),
			ArtistName: strings.TrimSpace(match.ArtistName),
			AlbumName:  strings.TrimSpace(match.AlbumName),
		}
		if match.Similarity != nil {
			candidate.Score = *match.Similarity
		}
		if candidate.Title == "" {
			continue
		}
		candidates = append(candidates, candidate)
	}
	return candidates, strings.TrimSpace(parsed.Data.TextToTrack.Warning), true
}

func parseTrackLookupCandidates(raw string) ([]trackCandidate, bool) {
	var parsed struct {
		Data struct {
			Tracks []struct {
				ID         string `json:"id"`
				Title      string `json:"title"`
				ArtistName string `json:"artistName"`
				PlayCount  int    `json:"playCount"`
			} `json:"tracks"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, false
	}
	candidates := make([]trackCandidate, 0, len(parsed.Data.Tracks))
	for _, item := range parsed.Data.Tracks {
		if strings.TrimSpace(item.Title) == "" {
			continue
		}
		candidates = append(candidates, trackCandidate{
			ID:         strings.TrimSpace(item.ID),
			Title:      strings.TrimSpace(item.Title),
			ArtistName: strings.TrimSpace(item.ArtistName),
			PlayCount:  item.PlayCount,
		})
	}
	return candidates, true
}

func parseSimilarTrackCandidates(raw string) ([]trackCandidate, bool) {
	var parsed struct {
		Data struct {
			SimilarTracks struct {
				Results []struct {
					ID         string   `json:"id"`
					Title      string   `json:"title"`
					ArtistName string   `json:"artistName"`
					PlayCount  int      `json:"playCount"`
					LastPlayed *string  `json:"lastPlayed"`
					Score      *float64 `json:"score"`
				} `json:"results"`
			} `json:"similarTracks"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, false
	}
	candidates := make([]trackCandidate, 0, len(parsed.Data.SimilarTracks.Results))
	for _, item := range parsed.Data.SimilarTracks.Results {
		candidate := trackCandidate{
			ID:         strings.TrimSpace(item.ID),
			Title:      strings.TrimSpace(item.Title),
			ArtistName: strings.TrimSpace(item.ArtistName),
			PlayCount:  item.PlayCount,
		}
		if item.Score != nil {
			candidate.Score = *item.Score
		}
		if item.LastPlayed != nil {
			candidate.LastPlayed = strings.TrimSpace(*item.LastPlayed)
		}
		if candidate.Title == "" {
			continue
		}
		candidates = append(candidates, candidate)
	}
	return candidates, true
}

func parseArtistCandidates(raw string) ([]artistCandidate, bool) {
	var parsed struct {
		Data struct {
			SimilarArtists struct {
				Results []struct {
					ID        string   `json:"id"`
					Name      string   `json:"name"`
					PlayCount int      `json:"playCount"`
					Rating    *int     `json:"rating"`
					Score     *float64 `json:"score"`
				} `json:"results"`
			} `json:"similarArtists"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, false
	}
	candidates := make([]artistCandidate, 0, len(parsed.Data.SimilarArtists.Results))
	for _, item := range parsed.Data.SimilarArtists.Results {
		candidate := artistCandidate{
			ID:        strings.TrimSpace(item.ID),
			Name:      strings.TrimSpace(item.Name),
			PlayCount: item.PlayCount,
		}
		if item.Rating != nil {
			candidate.Rating = *item.Rating
		}
		if item.Score != nil {
			candidate.Score = *item.Score
		}
		if candidate.Name == "" {
			continue
		}
		candidates = append(candidates, candidate)
	}
	return candidates, true
}

func parseArtistAlbums(raw string) ([]string, bool) {
	candidates, ok := parseArtistAlbumCandidates(raw)
	if !ok {
		return nil, false
	}
	items := make([]string, 0, len(candidates))
	for _, album := range candidates {
		label := strings.TrimSpace(album.Name)
		if label == "" {
			continue
		}
		if album.Year > 0 {
			label += fmt.Sprintf(" (%d)", album.Year)
		}
		items = append(items, label)
	}
	return items, true
}

func parseArtistAlbumCandidates(raw string) ([]creativeAlbumCandidate, bool) {
	var parsed struct {
		Data struct {
			Albums []struct {
				Name       string  `json:"name"`
				ArtistName string  `json:"artistName"`
				Year       *int    `json:"year"`
				Genre      string  `json:"genre"`
				PlayCount  *int    `json:"playCount"`
				LastPlayed *string `json:"lastPlayed"`
			} `json:"albums"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, false
	}
	items := make([]creativeAlbumCandidate, 0, len(parsed.Data.Albums))
	for _, album := range parsed.Data.Albums {
		name := strings.TrimSpace(album.Name)
		if name == "" {
			continue
		}
		candidate := creativeAlbumCandidate{
			Name:       name,
			ArtistName: strings.TrimSpace(album.ArtistName),
			Genre:      strings.TrimSpace(album.Genre),
		}
		if album.Year != nil && *album.Year > 0 {
			candidate.Year = *album.Year
		}
		if album.PlayCount != nil {
			candidate.PlayCount = *album.PlayCount
		}
		if album.LastPlayed != nil {
			candidate.LastPlayed = strings.TrimSpace(*album.LastPlayed)
		}
		items = append(items, candidate)
	}
	return items, true
}

func parseTrackDescriptionOutcome(raw string) (trackDescriptionOutcome, bool) {
	var parsed struct {
		Data struct {
			DescribeTrackSound struct {
				Track struct {
					ID         string `json:"id"`
					Title      string `json:"title"`
					ArtistName string `json:"artistName"`
					AlbumName  string `json:"albumName"`
				} `json:"track"`
				Summary struct {
					ProfileText string `json:"profileText"`
					TopMoods    []struct {
						Label string `json:"label"`
					} `json:"topMoods"`
					TopFeatures []struct {
						Label string `json:"label"`
					} `json:"topFeatures"`
				} `json:"summary"`
				Neighbors []struct {
					Title      string `json:"title"`
					ArtistName string `json:"artistName"`
				} `json:"neighbors"`
			} `json:"describeTrackSound"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return trackDescriptionOutcome{}, false
	}
	outcome := trackDescriptionOutcome{
		ID:          strings.TrimSpace(parsed.Data.DescribeTrackSound.Track.ID),
		Title:       strings.TrimSpace(parsed.Data.DescribeTrackSound.Track.Title),
		ArtistName:  strings.TrimSpace(parsed.Data.DescribeTrackSound.Track.ArtistName),
		AlbumName:   strings.TrimSpace(parsed.Data.DescribeTrackSound.Track.AlbumName),
		ProfileText: strings.TrimSpace(parsed.Data.DescribeTrackSound.Summary.ProfileText),
	}
	for _, item := range parsed.Data.DescribeTrackSound.Summary.TopMoods {
		if label := strings.TrimSpace(item.Label); label != "" {
			outcome.TopMoods = append(outcome.TopMoods, label)
		}
	}
	for _, item := range parsed.Data.DescribeTrackSound.Summary.TopFeatures {
		if label := strings.TrimSpace(item.Label); label != "" {
			outcome.TopFeatures = append(outcome.TopFeatures, label)
		}
	}
	for _, neighbor := range parsed.Data.DescribeTrackSound.Neighbors {
		label := strings.TrimSpace(neighbor.Title)
		if label == "" {
			continue
		}
		if strings.TrimSpace(neighbor.ArtistName) != "" {
			label += " by " + strings.TrimSpace(neighbor.ArtistName)
		}
		outcome.Neighbors = append(outcome.Neighbors, label)
		if len(outcome.Neighbors) >= 3 {
			break
		}
	}
	return outcome, outcome.Title != ""
}
