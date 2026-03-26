package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"groovarr/internal/agent"
	"groovarr/internal/discovery"
)

var structuredCreativeLibrarySearchRunner = structuredCreativeLibrarySearchWithTool

func (s *Server) tryEmbeddingsUnavailableSemanticLibraryQuery(rawMsg string) (string, bool) {
	if strings.TrimSpace(s.embeddingsURL) != "" {
		return "", false
	}
	lowerMsg := strings.ToLower(strings.TrimSpace(rawMsg))
	if !containsLibraryOwnershipCue(lowerMsg) {
		return "", false
	}
	if !isSemanticLibraryPrompt(lowerMsg) {
		return "", false
	}
	return "Semantic vibe search across your library isn't available right now because EMBEDDINGS_ENDPOINT is not configured. I can still help with exact library lookups, stats, playlists, or non-semantic recommendations.", true
}

func needsBroadAlbumDiscoveryClarification(lowerMsg string) bool {
	normalized := strings.Trim(strings.TrimSpace(lowerMsg), "?!.,")
	if normalized == "" {
		return false
	}

	ownershipCues := []string{
		"my ",
		"in my library",
		"that i own",
		"what do i have",
	}
	for _, cue := range ownershipCues {
		if strings.Contains(normalized, cue) {
			return false
		}
	}

	specificityCues := []string{
		" by ",
		" for ",
		" from ",
		" like ",
		" similar to ",
		" jazz",
		" rock",
		" ambient",
		" metal",
		" hip hop",
		" electronic",
		" pop",
		" classical",
		" soundtrack",
		" 19",
		" 20",
	}
	for _, cue := range specificityCues {
		if strings.Contains(normalized, cue) {
			return false
		}
	}

	switch normalized {
	case "best albums",
		"top albums",
		"essential albums",
		"starter albums",
		"recommend albums",
		"find albums",
		"show best albums",
		"show me best albums",
		"albums to begin with",
		"what should i listen to",
		"what albums should i listen to":
		return true
	default:
		return false
	}
}

func artistDiscoveryScopeClarificationTarget(rawMsg string) (string, bool) {
	q := strings.TrimSpace(rawMsg)
	if q == "" {
		return "", false
	}
	lower := strings.ToLower(q)
	if containsLibraryOwnershipCue(lower) {
		return "", false
	}
	if strings.Contains(lower, "playlist") || strings.Contains(lower, "track") || strings.Contains(lower, "song") {
		return "", false
	}
	if !(strings.Contains(lower, "album") || strings.Contains(lower, "albums")) {
		return "", false
	}
	needsScope := strings.Contains(lower, "best") ||
		strings.Contains(lower, "top") ||
		strings.Contains(lower, "essential") ||
		strings.Contains(lower, "starter")
	if !needsScope {
		return "", false
	}
	artistName := strings.TrimSpace(discovery.InferArtistFocus(q))
	if artistName == "" {
		return "", false
	}
	return artistName, true
}

func (s *Server) handleSpecificAlbumDiscovery(ctx context.Context, rawMsg string) (string, bool) {
	args, ok := buildSpecificAlbumDiscoveryArgs(rawMsg)
	if !ok {
		return "", false
	}

	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "discoverAlbums", args)
	if err != nil {
		return "", false
	}

	var parsed struct {
		Data struct {
			Items []struct {
				ArtistName string `json:"artistName"`
				AlbumTitle string `json:"albumTitle"`
				Year       int    `json:"year"`
			} `json:"discoverAlbums"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || len(parsed.Data.Items) == 0 {
		return "", false
	}

	items := make([]string, 0, len(parsed.Data.Items))
	for _, item := range parsed.Data.Items {
		if strings.TrimSpace(item.AlbumTitle) == "" {
			continue
		}
		label := item.AlbumTitle
		if strings.TrimSpace(item.ArtistName) != "" {
			label += " by " + item.ArtistName
		}
		if item.Year > 0 {
			label += fmt.Sprintf(" (%d)", item.Year)
		}
		items = append(items, label)
	}
	if len(items) == 0 {
		return "", false
	}
	return renderRouteBulletList("A few album picks", items, 8), true
}

func formatDiscoveredAlbumCandidate(candidate discoveredAlbumCandidate) string {
	label := strings.TrimSpace(candidate.AlbumTitle)
	if label == "" {
		return ""
	}
	if artist := strings.TrimSpace(candidate.ArtistName); artist != "" {
		label += " by " + artist
	}
	if candidate.Year > 0 {
		label += fmt.Sprintf(" (%d)", candidate.Year)
	}
	if reason := strings.TrimSpace(candidate.Reason); reason != "" {
		label += " - " + reason
	}
	return label
}

func renderDiscoveredAlbumSet(prefix string, candidates []discoveredAlbumCandidate, limit int) string {
	items := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if label := formatDiscoveredAlbumCandidate(candidate); label != "" {
			items = append(items, label)
		}
	}
	return renderRouteBulletList(prefix, items, limit)
}

func discoveredAlbumsToCreativeCandidates(candidates []discoveredAlbumCandidate) []creativeAlbumCandidate {
	out := make([]creativeAlbumCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		name := strings.TrimSpace(candidate.AlbumTitle)
		artist := strings.TrimSpace(candidate.ArtistName)
		if name == "" || artist == "" {
			continue
		}
		out = append(out, creativeAlbumCandidate{
			Name:       name,
			ArtistName: artist,
			Year:       candidate.Year,
		})
	}
	return out
}

func (s *Server) structuredCreativeLibraryCandidates(ctx context.Context, query string, limit int) ([]creativeAlbumCandidate, error) {
	return structuredCreativeLibrarySearchRunner(ctx, s, strings.TrimSpace(query), limit)
}

func structuredCreativeLibrarySearchWithTool(ctx context.Context, s *Server, query string, limit int) ([]creativeAlbumCandidate, error) {
	if s == nil || s.resolver == nil {
		return nil, fmt.Errorf("semantic library search unavailable")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if limit <= 0 {
		limit = 3
	}
	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "semanticAlbumSearch", map[string]interface{}{
		"queryText": query,
		"limit":     limit,
	})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data struct {
			Items struct {
				Matches []semanticAlbumSearchMatch `json:"matches"`
			} `json:"semanticAlbumSearch"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, err
	}
	return semanticMatchesToCreativeCandidates(parsed.Data.Items.Matches), nil
}

func requestedDiscoveryLimit(turn normalizedTurn, fallback int) int {
	if fallback <= 0 {
		fallback = 3
	}
	if strings.TrimSpace(turn.SelectionMode) == "top_n" {
		if count, ok := parseTurnSelectionCount(turn.SelectionValue); ok && count > 0 && count <= 10 {
			return count
		}
	}
	return fallback
}

func (s *Server) handleStructuredGeneralAlbumDiscovery(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	if resolved == nil {
		return "", false
	}
	turn := resolved.Turn
	if turn.Intent != "album_discovery" || turn.FollowupMode != "none" || turn.NeedsClarification {
		return "", false
	}
	if turn.QueryScope == "library" || (turn.LibraryOnly != nil && *turn.LibraryOnly) {
		return "", false
	}
	if action := strings.TrimSpace(turn.ResultAction); action != "" && action != "none" {
		return "", false
	}

	query := strings.TrimSpace(turn.RawMessage)
	if query == "" {
		query = strings.TrimSpace(turn.PromptHint)
	}
	if query == "" {
		return "", false
	}

	limit := requestedDiscoveryLimit(turn, 3)
	candidates, _, err := discoverAlbums(ctx, map[string]interface{}{
		"query": query,
		"limit": limit,
	})
	if err != nil || len(candidates) == 0 {
		return "", false
	}

	sessionID := chatSessionIDFromContext(ctx)
	setLastDiscoveredAlbums(sessionID, query, candidates)
	updatedTurn := turn
	updatedTurn.QueryScope = "general"
	updatedTurn.ResultSetKind = "discovered_albums"
	updatedTurn.PromptHint = query
	setLastActiveFocusFromTurn(sessionID, "discovered_albums", "result_set", updatedTurn)
	if len(candidates) == 1 {
		setLastFocusedResultItem(sessionID, "discovered_albums", normalizedDiscoveredAlbumCandidateKey(candidates[0]))
	}
	return renderDiscoveredAlbumSet("A few records for that", candidates, limit), true
}

func (s *Server) handleStructuredGeneralAlbumDiscoveryTurn(ctx context.Context, turn *Turn) (string, bool) {
	return s.handleStructuredGeneralAlbumDiscovery(ctx, turnToResolvedTurnContext(turn))
}

func buildSpecificAlbumDiscoveryArgs(rawMsg string) (map[string]interface{}, bool) {
	q := strings.TrimSpace(rawMsg)
	if q == "" {
		return nil, false
	}
	lower := strings.ToLower(q)
	if containsLibraryOwnershipCue(lower) {
		return nil, false
	}
	if strings.Contains(lower, "playlist") || strings.Contains(lower, "track") || strings.Contains(lower, "song") {
		return nil, false
	}
	if !(strings.Contains(lower, "album") || strings.Contains(lower, "albums")) {
		return nil, false
	}
	discoveryCue := strings.Contains(lower, "best") ||
		strings.Contains(lower, "top") ||
		strings.Contains(lower, "essential") ||
		strings.Contains(lower, "starter") ||
		strings.Contains(lower, "recommend")
	if !discoveryCue {
		return nil, false
	}
	artistHint := discovery.InferArtistFocus(q)
	if strings.TrimSpace(artistHint) == "" {
		return nil, false
	}

	limit := 5
	if match := regexp.MustCompile(`\b(\d{1,2})\b`).FindStringSubmatch(lower); len(match) == 2 {
		if n, err := strconv.Atoi(match[1]); err == nil && n > 0 && n <= 8 {
			limit = n
		}
	}
	return map[string]interface{}{
		"query": q,
		"limit": limit,
	}, true
}

func unsupportedAlbumRelationshipQueryResponse(lowerMsg string) (string, bool) {
	q := strings.TrimSpace(lowerMsg)
	if q == "" {
		return "", false
	}
	if (strings.Contains(q, "which albums") || strings.Contains(q, "find albums") || strings.Contains(q, "show albums")) &&
		strings.Contains(q, "artists with") &&
		!(strings.Contains(q, "only one album") || strings.Contains(q, "single album") || strings.Contains(q, "exactly one album")) {
		return "I can answer some artist-based album relationship questions, but this one still needs a more specific album-relationship query.", true
	}
	return "", false
}

func (s *Server) handleAlbumRelationshipQuery(ctx context.Context, lowerMsg string) (string, bool) {
	args, label, ok := buildDeterministicAlbumRelationshipArgs(lowerMsg)
	if !ok {
		return "", false
	}

	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "albumRelationshipStats", args)
	if err != nil {
		return "", false
	}

	var parsed struct {
		Data struct {
			Items []struct {
				AlbumName  string `json:"albumName"`
				ArtistName string `json:"artistName"`
				Year       *int   `json:"year"`
			} `json:"albumRelationshipStats"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return "", false
	}
	if len(parsed.Data.Items) == 0 {
		return fmt.Sprintf("%s: none.", label), true
	}

	items := make([]string, 0, len(parsed.Data.Items))
	for _, item := range parsed.Data.Items {
		if strings.TrimSpace(item.AlbumName) == "" {
			continue
		}
		entry := fmt.Sprintf("%s by %s", item.AlbumName, item.ArtistName)
		if item.Year != nil && *item.Year > 0 {
			entry = fmt.Sprintf("%s (%d)", entry, *item.Year)
		}
		items = append(items, entry)
	}
	if len(items) == 0 {
		return "", false
	}
	return renderRouteBulletList(label, items, 8), true
}

func buildDeterministicAlbumRelationshipArgs(lowerMsg string) (map[string]interface{}, string, bool) {
	q := strings.TrimSpace(lowerMsg)
	if q == "" {
		return nil, "", false
	}
	yearsQuery := strings.Contains(q, "played in years") ||
		strings.Contains(q, "played for years") ||
		strings.Contains(q, "played in ages") ||
		strings.Contains(q, "played for ages") ||
		strings.Contains(q, "havent played in years") ||
		strings.Contains(q, "haven't played in years") ||
		strings.Contains(q, "not played in years")
	if (strings.Contains(q, "which albums") || strings.Contains(q, "find albums") || strings.Contains(q, "show albums")) &&
		strings.Contains(q, "artists with") &&
		(strings.Contains(q, "only one album") || strings.Contains(q, "single album") || strings.Contains(q, "exactly one album")) {
		filter := map[string]interface{}{
			"artistExactAlbums": 1,
		}
		label := "Albums in your library by artists with only one album"
		if yearsQuery {
			filter["notPlayedSince"] = "years"
			label += " that you have not played in years"
		}
		return map[string]interface{}{
			"filter": filter,
			"sort":   "name_asc",
			"limit":  50,
		}, label, true
	}
	return nil, "", false
}

func (s *Server) tryCreativeLibraryAlbumsRoute(ctx context.Context, lowerMsg string) (string, bool) {
	if s.resolver == nil {
		return "", false
	}
	if isCreativeThreeAlbumPrompt(lowerMsg) {
		recs := []struct {
			label string
			query string
		}{
			{label: "For focus", query: "focused calm immersive instrumental concentration"},
			{label: "For walking", query: "walking rhythmic propulsive uplifting groove"},
			{label: "For late-night headphones", query: "late-night headphones intimate nocturnal immersive"},
		}
		picks := make([]string, 0, len(recs))
		used := map[string]struct{}{}
		for _, rec := range recs {
			match, ok := s.pickSemanticAlbumMatch(ctx, rec.query, used, 5)
			if !ok {
				continue
			}
			picks = append(picks, fmt.Sprintf("%s: %s", rec.label, match))
		}
		if len(picks) == 0 {
			return "I couldn't find strong library-backed picks for those three moods yet.", true
		}
		return "Using a simple mood heuristic from albums you own: " + strings.Join(picks, ". ") + ".", true
	}
	if strings.Contains(lowerMsg, "weirdest corner") {
		matches, ok := s.semanticAlbumMatches(ctx, "weird experimental avant-garde outsider psychedelic", 5)
		if !ok || len(matches) == 0 {
			return "I couldn't identify a convincing weird corner of your library yet.", true
		}
		items := make([]string, 0, len(matches))
		seen := map[string]struct{}{}
		for _, match := range matches {
			key := strings.ToLower(strings.TrimSpace(match.Name + "::" + match.ArtistName))
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			entry := fmt.Sprintf("%s by %s", match.Name, match.ArtistName)
			if len(match.Explanations) > 0 {
				entry += " [" + match.Explanations[0] + "]"
			}
			items = append(items, entry)
			if len(items) >= 5 {
				break
			}
		}
		if len(items) == 0 {
			return "I couldn't identify a convincing weird corner of your library yet.", true
		}
		return "Using an experimental/avant-garde heuristic, a weird corner of your library looks like: " + strings.Join(items, ", ") + ".", true
	}
	return "", false
}

func buildStructuredCreativeLibraryQuery(turn normalizedTurn) string {
	parts := make([]string, 0, 1+len(turn.StyleHints))
	prompt := strings.TrimSpace(turn.PromptHint)
	lowerPrompt := strings.ToLower(prompt)
	if prompt != "" {
		parts = append(parts, prompt)
	}
	for _, hint := range turn.StyleHints {
		hint = strings.TrimSpace(hint)
		if hint == "" {
			continue
		}
		if lowerPrompt != "" && strings.Contains(lowerPrompt, strings.ToLower(hint)) {
			continue
		}
		parts = append(parts, hint)
	}
	query := compactText(strings.Join(parts, " "), 180)
	query = strings.Trim(query, " .,;:!?")
	return query
}

func (s *Server) handleStructuredCreativeLibraryDiscovery(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	if resolved == nil || s.resolver == nil {
		return "", false
	}
	turn := resolved.Turn
	if turn.Intent != "album_discovery" || turn.QueryScope != "library" || turn.FollowupMode != "none" {
		return "", false
	}
	query := buildStructuredCreativeLibraryQuery(turn)
	if query == "" {
		return "", false
	}
	candidates, err := s.structuredCreativeLibraryCandidates(ctx, query, 3)
	if err != nil {
		return "", false
	}
	if len(candidates) == 0 {
		return "I couldn't find strong library-backed picks for that mood yet. Give me one clearer cue like darker, warmer, more nocturnal, or more rhythmic, or I can look outside your library.", true
	}
	setLastCreativeAlbumSet(chatSessionIDFromContext(ctx), "semantic_structured", query, candidates)
	setLastActiveFocusFromTurn(chatSessionIDFromContext(ctx), "creative_albums", "result_set", turn)
	return renderCreativeAlbumSet("From your library", candidates, 3), true
}

func (s *Server) handleStructuredCreativeLibraryDiscoveryTurn(ctx context.Context, turn *Turn) (string, bool) {
	return s.handleStructuredCreativeLibraryDiscovery(ctx, turnToResolvedTurnContext(turn))
}

type albumLookupCandidate struct {
	Title      string
	ArtistName string
	Year       int
}

func (s *Server) handleStructuredAlbumLibraryLookup(ctx context.Context, rawMsg string, resolved *resolvedTurnContext) (string, bool) {
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
	if !stickyLookup && (strings.TrimSpace(resolved.Turn.Intent) != "album_discovery" || strings.TrimSpace(resolved.Turn.QueryScope) != "library") {
		return "", false
	}
	if !directLookup && !stickyLookup {
		return "", false
	}
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
	title, artist, ok := splitLookupQueryArtist(query)
	if !ok {
		return "", false
	}
	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "albums", map[string]interface{}{
		"queryText":  title,
		"artistName": artist,
		"limit":      10,
	})
	if err != nil {
		return "", false
	}
	candidates, ok := parseAlbumLookupCandidates(raw)
	if !ok {
		return "", false
	}
	match, found := findExactAlbumLookupMatch(title, artist, candidates)
	setLastActiveFocus(chatSessionIDFromContext(ctx), "library_inventory_lookup", "album_mode")
	if !found {
		return fmt.Sprintf("No, I couldn't find %s by %s in your library.", title, artist), true
	}
	if match.Year > 0 {
		return fmt.Sprintf("Yes, you have %s by %s (%d) in your library.", match.Title, match.ArtistName, match.Year), true
	}
	return fmt.Sprintf("Yes, you have %s by %s in your library.", match.Title, match.ArtistName), true
}

func (s *Server) handleStructuredAlbumLibraryLookupTurn(ctx context.Context, turn *Turn) (string, bool) {
	if turn == nil {
		return "", false
	}
	return s.handleStructuredAlbumLibraryLookup(ctx, turn.UserMessage, turnToResolvedTurnContext(turn))
}

type semanticAlbumRouteMatch struct {
	ID           string
	Name         string
	ArtistName   string
	Genre        string
	Year         int
	PlayCount    int
	LastPlayed   string
	Explanations []string
}

func parseAlbumLookupCandidates(raw string) ([]albumLookupCandidate, bool) {
	var parsed struct {
		Data struct {
			Albums []struct {
				Name       string `json:"name"`
				ArtistName string `json:"artistName"`
				Year       *int   `json:"year"`
			} `json:"albums"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, false
	}
	candidates := make([]albumLookupCandidate, 0, len(parsed.Data.Albums))
	for _, item := range parsed.Data.Albums {
		if strings.TrimSpace(item.Name) == "" {
			continue
		}
		candidate := albumLookupCandidate{
			Title:      strings.TrimSpace(item.Name),
			ArtistName: strings.TrimSpace(item.ArtistName),
		}
		if item.Year != nil {
			candidate.Year = *item.Year
		}
		candidates = append(candidates, candidate)
	}
	return candidates, true
}

func findExactAlbumLookupMatch(title, artist string, candidates []albumLookupCandidate) (albumLookupCandidate, bool) {
	titleKey := normalizeReferenceText(title)
	artistKey := normalizeReferenceText(artist)
	if titleKey == "" || artistKey == "" {
		return albumLookupCandidate{}, false
	}
	for _, candidate := range candidates {
		if normalizeReferenceText(candidate.Title) != titleKey {
			continue
		}
		if normalizeReferenceText(candidate.ArtistName) != artistKey {
			continue
		}
		return candidate, true
	}
	return albumLookupCandidate{}, false
}

func (s *Server) pickSemanticAlbumMatch(ctx context.Context, query string, used map[string]struct{}, limit int) (string, bool) {
	matches, ok := s.semanticAlbumMatches(ctx, query, limit)
	if !ok {
		return "", false
	}
	for _, match := range matches {
		key := strings.ToLower(strings.TrimSpace(match.Name + "::" + match.ArtistName))
		if _, exists := used[key]; exists {
			continue
		}
		used[key] = struct{}{}
		entry := fmt.Sprintf("%s by %s", match.Name, match.ArtistName)
		if len(match.Explanations) > 0 {
			entry += " [" + match.Explanations[0] + "]"
		}
		return entry, true
	}
	return "", false
}

func (s *Server) semanticAlbumMatches(ctx context.Context, query string, limit int) ([]semanticAlbumRouteMatch, bool) {
	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "semanticAlbumSearch", map[string]interface{}{
		"queryText": query,
		"limit":     limit,
	})
	if err != nil {
		return nil, false
	}
	var parsed struct {
		Data struct {
			Items struct {
				Matches []struct {
					ID           string   `json:"id"`
					Name         string   `json:"name"`
					ArtistName   string   `json:"artistName"`
					Genre        string   `json:"genre"`
					Year         int      `json:"year"`
					PlayCount    int      `json:"playCount"`
					LastPlayed   string   `json:"lastPlayed"`
					Explanations []string `json:"explanations"`
				} `json:"matches"`
			} `json:"semanticAlbumSearch"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, false
	}
	out := make([]semanticAlbumRouteMatch, 0, len(parsed.Data.Items.Matches))
	for _, item := range parsed.Data.Items.Matches {
		if strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.ArtistName) == "" {
			continue
		}
		out = append(out, semanticAlbumRouteMatch{
			ID:           item.ID,
			Name:         item.Name,
			ArtistName:   item.ArtistName,
			Genre:        item.Genre,
			Year:         item.Year,
			PlayCount:    item.PlayCount,
			LastPlayed:   item.LastPlayed,
			Explanations: item.Explanations,
		})
	}
	return out, len(out) > 0
}

func (s *Server) resolveStructuredDiscoveredAlbumsApplyOutcome(ctx context.Context, resolved *resolvedTurnContext) (resultSetActionResult, bool) {
	ref := resolved.resultReference()
	if resolved == nil || ref.effectiveSetKind() != "discovered_albums" || ref.Action != "preview_apply" {
		return resultSetActionResult{}, false
	}
	sessionID := chatSessionIDFromContext(ctx)
	return handleResultSetAction(ctx, s, sessionID, ref)
}

func renderStructuredDiscoveredAlbumsApply(outcome resultSetActionResult) (ChatResponse, bool) {
	switch outcome.Kind {
	case "discovered_preview_error":
		return ChatResponse{Response: "I couldn't check which of those are still missing from your library right now."}, true
	case "discovered_preview_empty":
		return ChatResponse{Response: "None of those look missing from your library right now."}, true
	case "discovered_preview_apply":
		selected, ok := outcome.Selection.discoveredAlbums()
		if !ok || len(selected) == 0 {
			return ChatResponse{}, false
		}
		if outcome.PendingAction == nil && !strings.EqualFold(strings.TrimSpace(outcome.Selection.Selection), "all") {
			return ChatResponse{Response: fmt.Sprintf("I’m ready to apply library actions for %d discovered album(s).", len(selected))}, true
		}
		return ChatResponse{
			Response:      fmt.Sprintf("I’m ready to apply library actions for %d discovered album(s). Use the approval buttons if you want me to proceed.", len(selected)),
			PendingAction: outcome.PendingAction,
		}, true
	default:
		return ChatResponse{}, false
	}
}

func (s *Server) handleStructuredDiscoveredAlbumsApply(ctx context.Context, resolved *resolvedTurnContext) (string, *PendingAction, bool) {
	outcome, ok := s.resolveStructuredDiscoveredAlbumsApplyOutcome(ctx, resolved)
	if !ok {
		return "", nil, false
	}
	resp, ok := renderStructuredDiscoveredAlbumsApply(outcome)
	if !ok {
		return "", nil, false
	}
	return resp.Response, resp.PendingAction, true
}

func (s *Server) handleStructuredDiscoveredAlbumsApplyTurn(ctx context.Context, turn *Turn) (string, *PendingAction, bool) {
	return s.handleStructuredDiscoveredAlbumsApply(ctx, turnToResolvedTurnContext(turn))
}

func discoveryExecutionHandlers() []serverExecutionHandler {
	return []serverExecutionHandler{
		{
			name: "discovered_albums_availability",
			canHandle: func(turn *Turn) bool {
				request := executionRequestFromTurn(turn)
				return strings.TrimSpace(request.SetKind) == "discovered_albums" &&
					strings.TrimSpace(request.Operation) == "inspect_availability"
			},
			executeWithTurn: func(ctx context.Context, s *Server, _ []agent.Message, turn *Turn) (ChatResponse, bool) {
				outcome, ok := s.resolveStructuredDiscoveredAlbumsAvailabilityOutcome(ctx, turnToResolvedTurnContext(turn))
				if !ok {
					return ChatResponse{}, false
				}
				if resp, ok := renderStructuredDiscoveredAlbumsAvailability(outcome); ok {
					return ChatResponse{Response: resp}, true
				}
				return ChatResponse{}, false
			},
		},
		{
			name: "discovered_albums_apply",
			canHandle: func(turn *Turn) bool {
				request := executionRequestFromTurn(turn)
				return strings.TrimSpace(request.SetKind) == "discovered_albums" &&
					strings.TrimSpace(request.Operation) == "preview_apply"
			},
			executeWithTurn: func(ctx context.Context, s *Server, _ []agent.Message, turn *Turn) (ChatResponse, bool) {
				outcome, ok := s.resolveStructuredDiscoveredAlbumsApplyOutcome(ctx, turnToResolvedTurnContext(turn))
				if !ok {
					return ChatResponse{}, false
				}
				if resp, ok := renderStructuredDiscoveredAlbumsApply(outcome); ok {
					return resp, true
				}
				return ChatResponse{}, false
			},
		},
	}
}

func (s *Server) resolveStructuredDiscoveredAlbumsAvailabilityOutcome(ctx context.Context, resolved *resolvedTurnContext) (resultSetActionResult, bool) {
	ref := resolved.resultReference()
	if resolved == nil || ref.effectiveSetKind() != "discovered_albums" || ref.Action != "inspect_availability" {
		return resultSetActionResult{}, false
	}
	sessionID := chatSessionIDFromContext(ctx)
	return handleResultSetAction(ctx, s, sessionID, ref)
}

func renderStructuredDiscoveredAlbumsAvailability(outcome resultSetActionResult) (string, bool) {
	switch outcome.Kind {
	case "discovered_availability_error":
		return "I couldn't check which of those are still missing from your library right now.", true
	case "discovered_availability_empty":
		return "None of those look missing from your library right now.", true
	case "discovered_availability":
		return renderStructuredDiscoveredAvailability(outcome.Matches, outcome.MissingOnly), true
	default:
		return "", false
	}
}

func (s *Server) handleStructuredDiscoveredAlbumsAvailability(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	outcome, ok := s.resolveStructuredDiscoveredAlbumsAvailabilityOutcome(ctx, resolved)
	if !ok {
		return "", false
	}
	return renderStructuredDiscoveredAlbumsAvailability(outcome)
}

func (s *Server) handleStructuredDiscoveredAlbumsAvailabilityTurn(ctx context.Context, turn *Turn) (string, bool) {
	return s.handleStructuredDiscoveredAlbumsAvailability(ctx, turnToResolvedTurnContext(turn))
}

func selectFocusedDiscoveredCandidate(candidates []discoveredAlbumCandidate, focusedKey string) ([]discoveredAlbumCandidate, string, bool) {
	focusedKey = strings.TrimSpace(focusedKey)
	if focusedKey == "" {
		return nil, "", false
	}
	for _, candidate := range candidates {
		if normalizedDiscoveredAlbumCandidateKey(candidate) != focusedKey {
			continue
		}
		if candidate.Rank > 0 {
			return []discoveredAlbumCandidate{candidate}, strconv.Itoa(candidate.Rank), true
		}
	}
	return nil, "", false
}

func normalizedDiscoveredAlbumCandidateKey(candidate discoveredAlbumCandidate) string {
	parts := []string{
		normalizeSearchTerm(candidate.ArtistName),
		normalizeSearchTerm(candidate.AlbumTitle),
	}
	if candidate.Rank > 0 {
		parts = append([]string{strconv.Itoa(candidate.Rank)}, parts...)
	}
	return strings.Join(parts, "::")
}

func (s *Server) matchSelectedDiscoveredAlbums(ctx context.Context, candidates []discoveredAlbumCandidate, updatedAt time.Time, sourceQuery string) ([]lidarrAlbumMatch, error) {
	client, err := newLidarrClientFromEnv()
	if err != nil {
		return nil, err
	}
	matches, _, err := matchDiscoveredAlbumCandidatesInLidarr(ctx, client, candidates, updatedAt, sourceQuery)
	if err != nil {
		return nil, err
	}
	return matches, nil
}

func (s *Server) matchAndFilterMissingDiscoveredCandidates(ctx context.Context, candidates []discoveredAlbumCandidate, updatedAt time.Time, sourceQuery string) ([]discoveredAlbumCandidate, []lidarrAlbumMatch, error) {
	matches, err := s.matchSelectedDiscoveredAlbums(ctx, candidates, updatedAt, sourceQuery)
	if err != nil {
		return nil, nil, err
	}
	filteredCandidates, filteredMatches := filterDiscoveredMatchesMissingOnly(candidates, matches)
	return filteredCandidates, filteredMatches, nil
}

func filterDiscoveredMatchesMissingOnly(candidates []discoveredAlbumCandidate, matches []lidarrAlbumMatch) ([]discoveredAlbumCandidate, []lidarrAlbumMatch) {
	if len(candidates) == 0 || len(matches) == 0 {
		return nil, nil
	}
	allowed := make(map[int]struct{}, len(matches))
	filteredMatches := make([]lidarrAlbumMatch, 0, len(matches))
	for _, match := range matches {
		if strings.EqualFold(strings.TrimSpace(match.Status), "already_monitored") {
			continue
		}
		allowed[match.Rank] = struct{}{}
		filteredMatches = append(filteredMatches, match)
	}
	if len(filteredMatches) == 0 {
		return nil, nil
	}
	filteredCandidates := make([]discoveredAlbumCandidate, 0, len(filteredMatches))
	for _, candidate := range candidates {
		if _, ok := allowed[candidate.Rank]; ok {
			filteredCandidates = append(filteredCandidates, candidate)
		}
	}
	if len(filteredCandidates) == 0 {
		return nil, nil
	}
	return filteredCandidates, filteredMatches
}

func buildDiscoveredAlbumRankSelection(candidates []discoveredAlbumCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	ranks := make([]int, 0, len(candidates))
	seen := make(map[int]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.Rank <= 0 {
			continue
		}
		if _, ok := seen[candidate.Rank]; ok {
			continue
		}
		seen[candidate.Rank] = struct{}{}
		ranks = append(ranks, candidate.Rank)
	}
	if len(ranks) == 0 {
		return ""
	}
	sort.Ints(ranks)
	out := make([]string, 0, len(ranks))
	for _, rank := range ranks {
		out = append(out, strconv.Itoa(rank))
	}
	return strings.Join(out, ", ")
}

func renderStructuredDiscoveredAvailability(matches []lidarrAlbumMatch, missingOnly bool) string {
	if len(matches) == 0 {
		return ""
	}
	ready := make([]string, 0, len(matches))
	review := make([]string, 0, len(matches))
	for _, item := range matches {
		title := strings.TrimSpace(item.AlbumTitle)
		if title == "" {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(item.Status)) {
		case "can_monitor":
			ready = append(ready, title)
		case "already_monitored":
			if !missingOnly {
				ready = append(ready, title)
			}
		default:
			review = append(review, title)
		}
	}
	parts := make([]string, 0, 2)
	if len(ready) > 0 {
		if missingOnly {
			parts = append(parts, "still missing from your library: "+strings.Join(ready, ", "))
		} else {
			parts = append(parts, "ready to add to your library: "+strings.Join(ready, ", "))
		}
	}
	if len(review) > 0 {
		parts = append(parts, "need review: "+strings.Join(review, ", "))
	}
	if len(parts) == 0 {
		return ""
	}
	if missingOnly {
		return "Missing from your library: " + strings.Join(parts, ". ") + "."
	}
	return "Library check: " + strings.Join(parts, ". ") + "."
}

func extractDiscoveredAlbumSelection(msg string) string {
	lower := strings.ToLower(strings.TrimSpace(msg))
	if lower == "" {
		return "all"
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\bfirst\s+(\d+)\b`),
		regexp.MustCompile(`\bfirst\s+(one|two|three|four|five|six|seven|eight|nine|ten)\b`),
		regexp.MustCompile(`\blast\s+(\d+)\b`),
		regexp.MustCompile(`\blast\s+(one|two|three|four|five|six|seven|eight|nine|ten)\b`),
	}
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(lower); len(match) == 2 {
			count := match[1]
			if n, ok := parseSmallNumberWord(count); ok {
				if strings.Contains(match[0], "last") {
					return fmt.Sprintf("last %d", n)
				}
				return fmt.Sprintf("first %d", n)
			}
			if strings.Contains(match[0], "last") {
				return fmt.Sprintf("last %s", count)
			}
			return fmt.Sprintf("first %s", count)
		}
	}
	if selection := extractDiscoveredAlbumRankList(lower); selection != "" {
		return selection
	}
	if strings.Contains(lower, "those") || strings.Contains(lower, "them") || strings.Contains(lower, "these") {
		return "all"
	}

	return "all"
}

func extractDiscoveredAlbumRankList(lower string) string {
	if match := regexp.MustCompile(`\b(?:album|albums|record|records|result|results|item|items)\s+((?:#?\d+(?:st|nd|rd|th)?|second|third|fourth|fifth|sixth|seventh|eighth|ninth|tenth)(?:\s*(?:,|and)\s*(?:#?\d+(?:st|nd|rd|th)?|second|third|fourth|fifth|sixth|seventh|eighth|ninth|tenth))*)\b`).FindStringSubmatch(lower); len(match) == 2 {
		if normalized := normalizeDiscoveredAlbumRankList(match[1]); normalized != "" {
			return normalized
		}
	}
	if match := regexp.MustCompile(`#\d+(?:st|nd|rd|th)?`).FindString(lower); match != "" {
		if normalized := normalizeDiscoveredAlbumRankList(match); normalized != "" {
			return normalized
		}
	}
	if match := regexp.MustCompile(`\b(the\s+)?(second|third|fourth|fifth|sixth|seventh|eighth|ninth|tenth)\s+(one|album|record|result|item)\b`).FindStringSubmatch(lower); len(match) >= 3 {
		if normalized := normalizeDiscoveredAlbumRankList(match[2]); normalized != "" {
			return normalized
		}
	}
	return ""
}

func normalizeDiscoveredAlbumRankList(raw string) string {
	cleaned := strings.NewReplacer("&", ",", " and ", ",", "\"", "", "'", "").Replace(strings.TrimSpace(raw))
	parts := strings.Split(cleaned, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, "#")
		part = strings.Trim(part, ".!?")
		if part == "" {
			continue
		}
		if n, ok := parseDiscoveryOrdinalWord(part); ok {
			out = append(out, strconv.Itoa(n))
			continue
		}
		for _, suffix := range []string{"st", "nd", "rd", "th"} {
			if strings.HasSuffix(part, suffix) {
				part = strings.TrimSuffix(part, suffix)
				break
			}
		}
		if _, err := strconv.Atoi(part); err == nil {
			out = append(out, part)
		}
	}
	return strings.Join(out, ", ")
}

func parseDiscoveryOrdinalWord(raw string) (int, bool) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "second":
		return 2, true
	case "third":
		return 3, true
	case "fourth":
		return 4, true
	case "fifth":
		return 5, true
	case "sixth":
		return 6, true
	case "seventh":
		return 7, true
	case "eighth":
		return 8, true
	case "ninth":
		return 9, true
	case "tenth":
		return 10, true
	default:
		return 0, false
	}
}
