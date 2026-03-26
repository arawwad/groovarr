package main

import (
	"fmt"
	"regexp"
	"strings"

	"groovarr/internal/discovery"
)

func renderRouteBulletList(prefix string, items []string, limit int) string {
	if len(items) == 0 {
		return prefix + "."
	}
	if limit <= 0 {
		limit = 8
	}
	visible := items
	remaining := 0
	if len(items) > limit {
		visible = items[:limit]
		remaining = len(items) - limit
	}
	lines := make([]string, 0, len(visible)+2)
	lines = append(lines, prefix+":")
	for _, item := range visible {
		lines = append(lines, "- "+item)
	}
	if remaining > 0 {
		lines = append(lines, fmt.Sprintf("- ...and %d more.", remaining))
	}
	return strings.Join(lines, "\n")
}

func containsLibraryOwnershipCue(lowerMsg string) bool {
	cues := []string{
		"my library",
		"in my library",
		"from my library",
		"i own",
		"owned",
		"my albums",
		"my records",
		"my collection",
		"my shelves",
		"from what i have",
		"from what i've got",
		"from what i already have",
		"already have",
	}
	for _, cue := range cues {
		if strings.Contains(lowerMsg, cue) {
			return true
		}
	}
	return false
}

func isLibraryInventoryLookupPrompt(lowerMsg string) bool {
	if lowerMsg == "" || !containsLibraryOwnershipCue(lowerMsg) {
		return false
	}
	for _, prefix := range []string{
		"do i have ",
		"have i got ",
		"can you check if i have ",
		"check if i have ",
	} {
		if strings.HasPrefix(lowerMsg, prefix) {
			return true
		}
	}
	return false
}

func extractLibraryLookupQuery(rawMsg string) string {
	trimmed := strings.TrimSpace(strings.TrimRight(rawMsg, "?!., "))
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{
		"do i have ",
		"have i got ",
		"can you check if i have ",
		"check if i have ",
	} {
		if strings.HasPrefix(lower, prefix) {
			trimmed = strings.TrimSpace(trimmed[len(prefix):])
			lower = strings.ToLower(trimmed)
			break
		}
	}
	for _, suffix := range []string{
		" in my library",
		" from my library",
		" that i own",
		" i own",
	} {
		if strings.HasSuffix(lower, suffix) {
			trimmed = strings.TrimSpace(trimmed[:len(trimmed)-len(suffix)])
			break
		}
	}
	return strings.TrimSpace(trimmed)
}

func isInventoryLookupContinuationPrompt(lowerMsg string) bool {
	lowerMsg = strings.TrimSpace(lowerMsg)
	if lowerMsg == "" {
		return false
	}
	for _, prefix := range []string{
		"what about ",
		"how about ",
		"and what about ",
		"and how about ",
	} {
		if strings.HasPrefix(lowerMsg, prefix) {
			return true
		}
	}
	return false
}

func extractInventoryLookupContinuationQuery(rawMsg string) string {
	trimmed := strings.TrimSpace(strings.TrimRight(rawMsg, "?!., "))
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{
		"what about ",
		"how about ",
		"and what about ",
		"and how about ",
	} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(trimmed[len(prefix):])
		}
	}
	return ""
}

func extractInventoryLookupQueryFromTurn(turn normalizedTurn) string {
	if title := strings.TrimSpace(turn.TrackTitle); title != "" {
		if artist := strings.TrimSpace(turn.ArtistName); artist != "" {
			return title + " by " + artist
		}
		return title
	}
	if target := strings.TrimSpace(turn.TargetName); target != "" {
		return target
	}
	if value := strings.TrimSpace(turn.SelectionValue); value != "" {
		return value
	}
	return ""
}

func isArtistCatalogDepthPrompt(lowerMsg string) bool {
	lowerMsg = strings.ToLower(strings.TrimSpace(lowerMsg))
	if lowerMsg == "" {
		return false
	}
	switch {
	case strings.Contains(lowerMsg, "deepest catalog"):
		return true
	case strings.Contains(lowerMsg, "biggest catalog"):
		return true
	case strings.Contains(lowerMsg, "largest catalog"):
		return true
	case strings.Contains(lowerMsg, "deepest discography"):
		return true
	case strings.Contains(lowerMsg, "biggest discography"):
		return true
	case strings.Contains(lowerMsg, "largest discography"):
		return true
	case strings.Contains(lowerMsg, "most albums"):
		return true
	default:
		return false
	}
}

func creativeArtistFollowupArtist(rawMsg string) (string, bool) {
	trimmed := strings.TrimSpace(rawMsg)
	if trimmed == "" {
		return "", false
	}
	lower := strings.ToLower(trimmed)
	if !(strings.Contains(lower, "album") || strings.Contains(lower, "albums") || strings.Contains(lower, "record") || strings.Contains(lower, "records")) {
		return "", false
	}
	artist := strings.TrimSpace(discovery.InferArtistFocus(trimmed))
	if artist == "" {
		return "", false
	}
	return artist, true
}

func creativeArtistFollowupArtistFromCandidates(rawMsg string, candidates []creativeAlbumCandidate) (string, bool) {
	query := normalizeReferenceText(rawMsg)
	if query == "" || len(candidates) == 0 {
		return "", false
	}
	best := ""
	bestKey := ""
	seen := make(map[string]string, len(candidates))
	for _, candidate := range candidates {
		artist := strings.TrimSpace(candidate.ArtistName)
		key := normalizeReferenceText(artist)
		if artist == "" || key == "" {
			continue
		}
		if _, ok := seen[key]; !ok {
			seen[key] = artist
		}
	}
	for key, artist := range seen {
		if strings.Contains(query, key) {
			if len(key) > len(bestKey) {
				best = artist
				bestKey = key
			}
		}
	}
	if best != "" {
		return best, true
	}
	return creativeArtistFollowupArtist(rawMsg)
}

func creativeArtistFollowupWantsSingle(turn normalizedTurn) bool {
	if strings.TrimSpace(turn.SelectionMode) == "top_n" {
		if count, ok := parseTurnSelectionCount(turn.SelectionValue); ok {
			return count == 1
		}
	}
	lower := strings.ToLower(strings.TrimSpace(turn.RawMessage))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, " one ") ||
		strings.HasPrefix(lower, "one ") ||
		strings.Contains(lower, "1 ") ||
		strings.Contains(lower, " an album") ||
		strings.Contains(lower, " a record") ||
		strings.Contains(lower, "a single album")
}

func isCreativeThreeAlbumPrompt(lowerMsg string) bool {
	return strings.Contains(lowerMsg, "focus") &&
		strings.Contains(lowerMsg, "walking") &&
		(strings.Contains(lowerMsg, "late-night headphones") ||
			(strings.Contains(lowerMsg, "late-night") && strings.Contains(lowerMsg, "headphones")) ||
			(strings.Contains(lowerMsg, "late night") && strings.Contains(lowerMsg, "headphones")))
}

func isSemanticLibraryPrompt(lowerMsg string) bool {
	if lowerMsg == "" {
		return false
	}
	if isCreativeThreeAlbumPrompt(lowerMsg) {
		return true
	}
	cues := []string{
		"vibe",
		"mood",
		"feels like",
		"feel like",
		"sounds like",
		"similar to",
		"weirdest corner",
		"weird corner",
		"nocturnal",
		"atmospheric",
		"dreamy",
		"immersive",
		"spacious",
		"rainy",
		"moody",
		"melancholic",
		"late-night",
		"late night",
	}
	for _, cue := range cues {
		if strings.Contains(lowerMsg, cue) {
			return true
		}
	}
	return false
}

func isArtistAlbumFollowUpPrompt(lowerMsg string) bool {
	if lowerMsg == "" {
		return false
	}
	if strings.Contains(lowerMsg, "played") ||
		strings.Contains(lowerMsg, "listened") ||
		strings.Contains(lowerMsg, "touched") ||
		strings.Contains(lowerMsg, "recently") ||
		strings.Contains(lowerMsg, "most recent") {
		return false
	}
	if strings.Contains(lowerMsg, "revisit") {
		return true
	}
	return strings.Contains(lowerMsg, "album") ||
		strings.Contains(lowerMsg, "albums") ||
		strings.Contains(lowerMsg, "record") ||
		strings.Contains(lowerMsg, "records")
}

func artistAlbumFollowUpRequestedCount(lowerMsg string) (int, bool) {
	lowerMsg = strings.ToLower(strings.TrimSpace(lowerMsg))
	if lowerMsg == "" {
		return 0, false
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\b(?:give|show|pick|suggest|recommend)(?: me)?\s+(\d+|one|two|three|four|five)\b`),
		regexp.MustCompile(`\b(\d+|one|two|three|four|five)\s+(?:albums?|records?)\b`),
	}
	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(lowerMsg)
		if len(matches) < 2 {
			continue
		}
		if count, ok := parseTurnSelectionCount(matches[1]); ok && count > 0 {
			return count, true
		}
	}
	return 0, false
}
