package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type artistAlbumStat struct {
	ArtistName string
	AlbumCount int
}

func (s *Server) handleArtistAlbumCount(ctx context.Context, rawMsg string) (string, bool) {
	artistNames, ok := extractArtistAlbumCountNames(rawMsg)
	if !ok {
		return "", false
	}

	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "artistLibraryStats", map[string]interface{}{
		"filter": map[string]interface{}{
			"artistNames": artistNames,
		},
		"sort":  "album_count_desc",
		"limit": maxInt(len(artistNames)*3, 10),
	})
	if err != nil {
		return "", false
	}

	var parsed struct {
		Data struct {
			Items []struct {
				ArtistName string `json:"artistName"`
				AlbumCount int    `json:"albumCount"`
			} `json:"artistLibraryStats"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || len(parsed.Data.Items) == 0 {
		return "", false
	}

	stats := make([]artistAlbumStat, 0, len(parsed.Data.Items))
	for _, item := range parsed.Data.Items {
		if strings.TrimSpace(item.ArtistName) == "" {
			continue
		}
		stats = append(stats, artistAlbumStat{
			ArtistName: strings.TrimSpace(item.ArtistName),
			AlbumCount: item.AlbumCount,
		})
	}
	if len(stats) == 0 {
		return "", false
	}

	matched := make([]artistAlbumStat, 0, len(artistNames))
	used := make(map[int]struct{})
	missing := make([]string, 0, len(artistNames))
	for _, requested := range artistNames {
		index := findMatchingArtistLibraryStat(requested, stats, used)
		if index < 0 {
			missing = append(missing, strings.TrimSpace(requested))
			continue
		}
		used[index] = struct{}{}
		matched = append(matched, stats[index])
	}
	if len(matched) == 0 {
		return "", false
	}

	total := 0
	details := make([]string, 0, len(matched))
	for _, item := range matched {
		total += item.AlbumCount
		details = append(details, fmt.Sprintf("%s (%d)", item.ArtistName, item.AlbumCount))
	}

	response := ""
	if len(matched) == 1 {
		response = fmt.Sprintf("You have %d albums by %s in your library.", total, matched[0].ArtistName)
	} else {
		response = fmt.Sprintf("You have %d albums combined by %s in your library.", total, strings.Join(details, " and "))
	}
	if len(missing) > 0 {
		response += " I couldn't match " + strings.Join(quoteStrings(missing), " and ") + " to an artist in your library."
	}
	return response, true
}

func (s *Server) handleArtistListeningStatsQuery(ctx context.Context, lowerMsg string) (string, bool) {
	args, label, ok := buildDeterministicArtistListeningStatsArgs(lowerMsg)
	if !ok {
		return "", false
	}

	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "artistListeningStats", args)
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

	items := make([]string, 0, len(parsed.Data.Items))
	for _, item := range parsed.Data.Items {
		if strings.TrimSpace(item.ArtistName) == "" {
			continue
		}
		items = append(items, fmt.Sprintf("%s (%d plays, %d albums)", item.ArtistName, item.PlaysInWindow, item.AlbumCount))
	}
	if len(items) == 0 {
		return "", false
	}
	return renderRouteBulletList(label, items, 8), true
}

func (s *Server) handleLibraryFacetQuery(ctx context.Context, lowerMsg string) (string, bool) {
	args, label, ok := buildDeterministicLibraryFacetArgs(lowerMsg)
	if !ok {
		return "", false
	}

	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "libraryFacetCounts", args)
	if err != nil {
		return "", false
	}

	var parsed struct {
		Data struct {
			Items []struct {
				Value string `json:"value"`
				Count int    `json:"count"`
			} `json:"libraryFacetCounts"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || len(parsed.Data.Items) == 0 {
		return "", false
	}

	items := make([]string, 0, len(parsed.Data.Items))
	for _, item := range parsed.Data.Items {
		if strings.TrimSpace(item.Value) == "" {
			continue
		}
		items = append(items, fmt.Sprintf("%s (%d)", item.Value, item.Count))
	}
	if len(items) == 0 {
		return "", false
	}
	return renderRouteBulletList(label, items, 8), true
}

func (s *Server) tryNormalizedTopLibraryArtists(ctx context.Context, turn normalizedTurn) (string, bool) {
	if s.resolver == nil || strings.TrimSpace(turn.SubIntent) != "library_top_artists" {
		return "", false
	}
	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "artistLibraryStats", map[string]interface{}{
		"filter": map[string]interface{}{},
		"sort":   "album_count_desc",
		"limit":  8,
	})
	if err != nil {
		return "", false
	}

	var parsed struct {
		Data struct {
			Items []struct {
				ArtistName string `json:"artistName"`
				AlbumCount int    `json:"albumCount"`
			} `json:"artistLibraryStats"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || len(parsed.Data.Items) == 0 {
		return "", false
	}

	items := make([]string, 0, len(parsed.Data.Items))
	for _, item := range parsed.Data.Items {
		if strings.TrimSpace(item.ArtistName) == "" {
			continue
		}
		items = append(items, fmt.Sprintf("%s (%d albums)", item.ArtistName, item.AlbumCount))
	}
	if len(items) == 0 {
		return "", false
	}
	return renderRouteBulletList("Artists with the biggest footprint in your library", items, 8), true
}

func buildDeterministicLibraryFacetArgs(lowerMsg string) (map[string]interface{}, string, bool) {
	q := strings.TrimSpace(lowerMsg)
	if q == "" {
		return nil, "", false
	}
	if !(strings.Contains(q, "my library") || strings.Contains(q, "in my library") || strings.Contains(q, "i own")) {
		return nil, "", false
	}

	switch {
	case strings.Contains(q, "genre") && (strings.Contains(q, "dominate") || strings.Contains(q, "most common") || strings.Contains(q, "top genre")):
		return map[string]interface{}{"field": "genre", "limit": 10}, "Genres that dominate your library", true
	case strings.Contains(q, "decade") && (strings.Contains(q, "dominate") || strings.Contains(q, "most common") || strings.Contains(q, "top decade")):
		return map[string]interface{}{"field": "decade", "limit": 10}, "Decades that dominate your library", true
	case strings.Contains(q, "year") && (strings.Contains(q, "dominate") || strings.Contains(q, "most common") || strings.Contains(q, "top year")):
		return map[string]interface{}{"field": "year", "limit": 10}, "Years that dominate your library", true
	default:
		return nil, "", false
	}
}

var artistAlbumCountPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^how many albums do (.+?) have in my library(?:\s+(?:combined|altogether|in total|total))?[?!.\s]*$`),
	regexp.MustCompile(`(?i)^how many (.+?) albums are in my library[?!.\s]*$`),
	regexp.MustCompile(`(?i)^how many albums by (.+?) are in my library[?!.\s]*$`),
}

func extractArtistAlbumCountNames(rawMsg string) ([]string, bool) {
	trimmed := strings.TrimSpace(rawMsg)
	if trimmed == "" {
		return nil, false
	}
	subject := ""
	for _, pattern := range artistAlbumCountPatterns {
		match := pattern.FindStringSubmatch(trimmed)
		if len(match) == 2 {
			subject = strings.TrimSpace(match[1])
			break
		}
	}
	if subject == "" {
		return nil, false
	}
	replacer := strings.NewReplacer(", and ", ",", " and ", ",", " & ", ",", ";", ",")
	parts := strings.Split(replacer.Replace(subject), ",")
	names := uniqueNonEmptyStrings(parts)
	if len(names) == 0 {
		return nil, false
	}
	return names, true
}

func findMatchingArtistLibraryStat(requested string, stats []artistAlbumStat, used map[int]struct{}) int {
	requestedKey := normalizeReferenceText(requested)
	if requestedKey == "" {
		return -1
	}

	bestIndex := -1
	bestScore := -1
	for index, item := range stats {
		if _, ok := used[index]; ok {
			continue
		}
		artistKey := normalizeReferenceText(item.ArtistName)
		if artistKey == "" {
			continue
		}
		score := -1
		switch {
		case artistKey == requestedKey:
			score = 4
		case strings.Contains(artistKey, requestedKey):
			score = 3
		case strings.Contains(requestedKey, artistKey):
			score = 2
		case strings.Contains(artistKey, "the "+requestedKey):
			score = 1
		}
		if score > bestScore {
			bestIndex = index
			bestScore = score
		}
	}
	return bestIndex
}

func quoteStrings(items []string) []string {
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		quoted = append(quoted, fmt.Sprintf("%q", item))
	}
	return quoted
}

func buildDeterministicArtistListeningStatsArgs(lowerMsg string) (map[string]interface{}, string, bool) {
	if !isArtistListeningStatsQuery(lowerMsg) {
		return nil, "", false
	}

	start, end, label, ok := resolveListeningPeriod(lowerMsg)
	if !ok {
		return nil, "", false
	}

	filter := map[string]interface{}{
		"playedSince": start.Format(time.RFC3339),
		"playedUntil": end.Format(time.RFC3339),
	}
	responseLabel := fmt.Sprintf("Artists you played most %s", label)
	if strings.Contains(lowerMsg, "most") || strings.Contains(lowerMsg, "top artists") || strings.Contains(lowerMsg, "dominated") {
		filter["minPlaysInWindow"] = 1
	}
	if minAlbums, ok := extractMinAlbumsQueryValue(lowerMsg); ok {
		filter["minAlbums"] = minAlbums
		responseLabel += fmt.Sprintf(" and have at least %d albums", minAlbums)
	}
	if strings.Contains(lowerMsg, "no plays") || strings.Contains(lowerMsg, "zero plays") || strings.Contains(lowerMsg, "ignored") {
		filter["maxPlaysInWindow"] = 0
		responseLabel = fmt.Sprintf("Artists in your library with no plays %s", label)
		if minAlbums, ok := filter["minAlbums"].(int); ok && minAlbums > 0 {
			responseLabel += fmt.Sprintf(" and at least %d albums", minAlbums)
		}
	}

	return map[string]interface{}{
		"filter": filter,
		"sort":   "plays_in_window_desc",
		"limit":  25,
	}, responseLabel, true
}

func isArtistListeningStatsQuery(lowerMsg string) bool {
	if !strings.Contains(lowerMsg, "artist") {
		return false
	}
	if _, _, _, ok := resolveListeningPeriod(lowerMsg); !ok {
		return false
	}
	return strings.Contains(lowerMsg, "listen") ||
		strings.Contains(lowerMsg, "played") ||
		strings.Contains(lowerMsg, "plays") ||
		strings.Contains(lowerMsg, "dominated") ||
		strings.Contains(lowerMsg, "top artists")
}

func (s *Server) handleArtistLibraryStatsQuery(ctx context.Context, lowerMsg string) (string, bool) {
	args, label, ok := buildDeterministicArtistLibraryStatsArgs(lowerMsg)
	if !ok {
		return "", false
	}

	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "artistLibraryStats", args)
	if err != nil {
		return "", false
	}

	var parsed struct {
		Data struct {
			Items []struct {
				ArtistName string `json:"artistName"`
				AlbumCount int    `json:"albumCount"`
			} `json:"artistLibraryStats"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || len(parsed.Data.Items) == 0 {
		return "", false
	}

	names := make([]string, 0, len(parsed.Data.Items))
	for _, item := range parsed.Data.Items {
		if strings.TrimSpace(item.ArtistName) == "" {
			continue
		}
		names = append(names, item.ArtistName)
	}
	if len(names) == 0 {
		return "", false
	}
	return renderRouteBulletList(label, names, 8), true
}

func isSingleAlbumArtistQuery(lowerMsg string) bool {
	q := strings.TrimSpace(lowerMsg)
	if q == "" {
		return false
	}
	if strings.Contains(q, "which albums") || strings.Contains(q, "show albums") || strings.Contains(q, "albums in my library") {
		return false
	}
	if !strings.Contains(q, "artist") || !strings.Contains(q, "album") {
		return false
	}
	artistSubjectCues := []string{
		"which artists",
		"artists with",
		"show artists",
		"list artists",
	}
	matchedSubject := false
	for _, cue := range artistSubjectCues {
		if strings.Contains(q, cue) {
			matchedSubject = true
			break
		}
	}
	if !matchedSubject {
		return false
	}
	countCues := []string{
		"only one album",
		"just one album",
		"exactly one album",
		"single album",
		"1 album",
	}
	for _, cue := range countCues {
		if strings.Contains(q, cue) {
			return true
		}
	}
	return false
}

func buildDeterministicArtistLibraryStatsArgs(lowerMsg string) (map[string]interface{}, string, bool) {
	if !(strings.Contains(lowerMsg, "my library") || strings.Contains(lowerMsg, "in my library")) {
		return nil, "", false
	}
	if isSingleAlbumArtistQuery(lowerMsg) {
		return map[string]interface{}{
			"filter": map[string]interface{}{
				"exactAlbums": 1,
			},
			"sort":  "album_count_asc",
			"limit": 50,
		}, "Artists in your library with only one album", true
	}

	minAlbums, ok := extractMinAlbumsQueryValue(lowerMsg)
	if !ok {
		return nil, "", false
	}
	if !(strings.Contains(lowerMsg, "artist") && strings.Contains(lowerMsg, "album")) {
		return nil, "", false
	}

	filter := map[string]interface{}{
		"minAlbums": minAlbums,
	}
	label := fmt.Sprintf("Artists in your library with at least %d albums", minAlbums)
	if strings.Contains(lowerMsg, "no plays this year") || strings.Contains(lowerMsg, "no plays this yr") {
		now := time.Now().UTC()
		start := time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
		end := start.AddDate(1, 0, 0)
		filter["playedSince"] = start.Format(time.RFC3339)
		filter["playedUntil"] = end.Format(time.RFC3339)
		filter["maxPlaysInWindow"] = 0
		label += fmt.Sprintf(" and no plays since %s", start.Format("January 2, 2006"))
	}

	return map[string]interface{}{
		"filter": filter,
		"sort":   "album_count_asc",
		"limit":  50,
	}, label, true
}

func (s *Server) handleAlbumLibraryStatsQuery(ctx context.Context, lowerMsg string) (string, bool) {
	args, label, ok := buildDeterministicAlbumLibraryStatsArgs(lowerMsg)
	if !ok {
		return "", false
	}

	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "albumLibraryStats", args)
	if err != nil {
		return "", false
	}

	var parsed struct {
		Data struct {
			Items []struct {
				AlbumName  string  `json:"albumName"`
				ArtistName string  `json:"artistName"`
				Year       *int    `json:"year"`
				PlayCount  int     `json:"playCount"`
				LastPlayed *string `json:"lastPlayed"`
			} `json:"albumLibraryStats"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || len(parsed.Data.Items) == 0 {
		return "", false
	}

	items := make([]string, 0, len(parsed.Data.Items))
	for _, item := range parsed.Data.Items {
		if strings.TrimSpace(item.AlbumName) == "" {
			continue
		}
		labelText := item.AlbumName
		if strings.TrimSpace(item.ArtistName) != "" {
			labelText += " by " + item.ArtistName
		}
		if item.Year != nil && *item.Year > 0 {
			labelText += fmt.Sprintf(" (%d)", *item.Year)
		}
		items = append(items, labelText)
	}
	if len(items) == 0 {
		return "", false
	}
	return fmt.Sprintf("%s: %s.", label, strings.Join(items, ", ")), true
}

func buildDeterministicAlbumLibraryStatsArgs(lowerMsg string) (map[string]interface{}, string, bool) {
	if !(strings.Contains(lowerMsg, "my library") || strings.Contains(lowerMsg, "in my library")) {
		return nil, "", false
	}
	if !(strings.Contains(lowerMsg, "album") || strings.Contains(lowerMsg, "albums")) {
		return nil, "", false
	}
	if !(strings.Contains(lowerMsg, "played in years") ||
		strings.Contains(lowerMsg, "played for years") ||
		strings.Contains(lowerMsg, "played in ages") ||
		strings.Contains(lowerMsg, "played for ages") ||
		strings.Contains(lowerMsg, "havent played in years") ||
		strings.Contains(lowerMsg, "haven't played in years") ||
		strings.Contains(lowerMsg, "not played in years")) {
		return nil, "", false
	}

	return map[string]interface{}{
		"filter": map[string]interface{}{
			"notPlayedSince": "years",
		},
		"sort":  "last_played_asc",
		"limit": 10,
	}, "Albums in your library not played in years", true
}

func extractMinAlbumsQueryValue(lowerMsg string) (int, bool) {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`at least\s+(\d+)\s+albums?`),
		regexp.MustCompile(`(\d+)\s+albums?\s+or more`),
		regexp.MustCompile(`at least\s+(one|two|three|four|five|six|seven|eight|nine|ten)\s+albums?`),
	}
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(lowerMsg); len(match) == 2 {
			if n, ok := parseSmallNumberWord(match[1]); ok {
				return n, true
			}
			n, err := strconv.Atoi(match[1])
			if err == nil && n > 0 {
				return n, true
			}
		}
	}
	return 0, false
}

func parseSmallNumberWord(raw string) (int, bool) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "one":
		return 1, true
	case "two":
		return 2, true
	case "three":
		return 3, true
	case "four":
		return 4, true
	case "five":
		return 5, true
	case "six":
		return 6, true
	case "seven":
		return 7, true
	case "eight":
		return 8, true
	case "nine":
		return 9, true
	case "ten":
		return 10, true
	default:
		return 0, false
	}
}
