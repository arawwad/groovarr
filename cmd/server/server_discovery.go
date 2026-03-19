package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"groovarr/internal/discovery"
)

func (s *Server) tryDeterministicBroadDiscoveryClarification(lowerMsg string) (string, bool) {
	if !needsBroadAlbumDiscoveryClarification(lowerMsg) {
		return "", false
	}
	return "Do you want the best albums in your library, or recommendations narrowed by artist, genre, era, or mood?", true
}

func (s *Server) tryDeterministicArtistDiscoveryScopeClarification(rawMsg string) (string, bool) {
	artistName, ok := artistDiscoveryScopeClarificationTarget(rawMsg)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("Do you want the best %s albums in general, or only from your library?", artistName), true
}

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

func (s *Server) tryDeterministicSpecificAlbumDiscovery(ctx context.Context, rawMsg string) (string, bool) {
	args, ok := buildDeterministicSpecificAlbumDiscoveryArgs(rawMsg)
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

func buildDeterministicSpecificAlbumDiscoveryArgs(rawMsg string) (map[string]interface{}, bool) {
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

func tryDeterministicUnsupportedAlbumRelationshipQuery(lowerMsg string) (string, bool) {
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

func (s *Server) tryDeterministicAlbumRelationshipQuery(ctx context.Context, lowerMsg string) (string, bool) {
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

func (s *Server) tryDeterministicCreativeLibraryAlbums(ctx context.Context, lowerMsg string) (string, bool) {
	if !containsLibraryOwnershipCue(lowerMsg) {
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

func (s *Server) tryDeterministicDiscoveredAlbumsApply(ctx context.Context, msg string) (string, *PendingAction, bool) {
	sessionID := chatSessionIDFromContext(ctx)
	candidates, updatedAt, _ := getLastDiscoveredAlbums(sessionID)
	if len(candidates) == 0 || updatedAt.IsZero() || time.Since(updatedAt) > 30*time.Minute {
		return "", nil, false
	}
	if !wantsDiscoveredAlbumApproval(msg) {
		return "", nil, false
	}

	selection := extractDiscoveredAlbumSelection(msg)
	resp, pendingAction, err := s.startDiscoveredAlbumsApplyPreview(ctx, selection)
	if err != nil {
		return "", nil, false
	}
	if strings.TrimSpace(resp) == "" {
		return "", nil, false
	}
	if pendingAction == nil && !strings.EqualFold(strings.TrimSpace(selection), "all") {
		return resp, nil, true
	}
	return resp, pendingAction, pendingAction != nil
}

func (s *Server) tryDeterministicDiscoveredAlbumsAvailability(ctx context.Context, msg string) (string, bool) {
	sessionID := chatSessionIDFromContext(ctx)
	candidates, updatedAt, _ := getLastDiscoveredAlbums(sessionID)
	if len(candidates) == 0 || updatedAt.IsZero() || time.Since(updatedAt) > 30*time.Minute {
		return "", false
	}
	lower := strings.ToLower(strings.TrimSpace(msg))
	if lower == "" {
		return "", false
	}
	if !(strings.Contains(lower, "those") || strings.Contains(lower, "them") || strings.Contains(lower, "these")) {
		return "", false
	}
	if !(strings.Contains(lower, "already in my library") ||
		strings.Contains(lower, "in my library") ||
		strings.Contains(lower, "available") ||
		strings.Contains(lower, "already have")) {
		return "", false
	}

	selection := fmt.Sprintf("first %d", len(candidates))
	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "matchDiscoveredAlbumsInLidarr", map[string]interface{}{
		"selection": selection,
	})
	if err != nil {
		return "", false
	}

	var parsed struct {
		Data struct {
			Match struct {
				Matches []struct {
					AlbumTitle string `json:"albumTitle"`
					Status     string `json:"status"`
				} `json:"matches"`
			} `json:"matchDiscoveredAlbumsInLidarr"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return "", false
	}
	if len(parsed.Data.Match.Matches) == 0 {
		return "", false
	}

	ready := make([]string, 0, len(parsed.Data.Match.Matches))
	review := make([]string, 0, len(parsed.Data.Match.Matches))
	for _, item := range parsed.Data.Match.Matches {
		title := strings.TrimSpace(item.AlbumTitle)
		if title == "" {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(item.Status)) {
		case "can_monitor", "already_monitored":
			ready = append(ready, title)
		default:
			review = append(review, title)
		}
	}
	parts := make([]string, 0, 2)
	if len(ready) > 0 {
		parts = append(parts, "ready to add to your library: "+strings.Join(ready, ", "))
	}
	if len(review) > 0 {
		parts = append(parts, "need review: "+strings.Join(review, ", "))
	}
	if len(parts) == 0 {
		return "", false
	}
	return "Library check: " + strings.Join(parts, ". ") + ".", true
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

func (s *Server) tryDeterministicArtistRemoval(ctx context.Context, rawMsg string) (string, *PendingAction, bool) {
	target := extractArtistRemovalTarget(rawMsg)
	if target == "" {
		return "", nil, false
	}

	response, pendingAction, err := s.startArtistRemovalPreview(ctx, target)
	if err != nil {
		return err.Error(), nil, true
	}
	return response, pendingAction, true
}

func extractArtistRemovalTarget(rawMsg string) string {
	msg := strings.TrimSpace(rawMsg)
	if msg == "" {
		return ""
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)^can we remove\s+(.+?)\s+from my library$`),
		regexp.MustCompile(`(?i)^could you remove\s+(.+?)\s+from my library$`),
		regexp.MustCompile(`(?i)^please remove\s+(.+?)\s+from my library$`),
		regexp.MustCompile(`(?i)^remove artist\s+(.+?)\s+from my library$`),
		regexp.MustCompile(`(?i)^delete artist\s+(.+?)\s+from my library$`),
		regexp.MustCompile(`(?i)^remove\s+(.+?)\s+from my library$`),
		regexp.MustCompile(`(?i)^delete\s+(.+?)\s+from my library$`),
		regexp.MustCompile(`(?i)^remove artist\s+(.+)$`),
		regexp.MustCompile(`(?i)^delete artist\s+(.+)$`),
	}
	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(msg)
		if len(matches) != 2 {
			continue
		}
		target := strings.TrimSpace(matches[1])
		target = strings.Trim(target, "\"'.,!? ")
		if target != "" {
			return target
		}
	}
	return ""
}
