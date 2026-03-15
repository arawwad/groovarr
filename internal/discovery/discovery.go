package discovery

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

type Candidate struct {
	Rank       int    `json:"rank"`
	ArtistName string `json:"artistName"`
	AlbumTitle string `json:"albumTitle"`
	Year       int    `json:"year,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type Request struct {
	Query        string
	ArtistHint   string
	Limit        int
	RequestCount int
	Seed         *SeedContext
}

type SeedContext struct {
	Type                  string
	Name                  string
	Subtitle              string
	RepresentativeArtists []string
	RepresentativeTracks  []string
}

type scoredCandidate struct {
	candidate Candidate
	score     float64
}

func BuildRequest(query string, limit int) (Request, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return Request{}, fmt.Errorf("query is required")
	}

	limit = clampLimit(limit)
	return Request{
		Query:        query,
		ArtistHint:   InferArtistFocus(query),
		Limit:        limit,
		RequestCount: clampRequestCount(limit + 2),
	}, nil
}

func BuildSceneSeededRequest(name, subtitle string, representativeArtists, representativeTracks []string, direction string, limit int) (Request, error) {
	direction = strings.TrimSpace(direction)
	if direction == "" {
		direction = "adjacent studio albums"
	}
	limit = clampLimit(limit)
	return Request{
		Query:        direction,
		ArtistHint:   "",
		Limit:        limit,
		RequestCount: clampRequestCount(limit + 2),
		Seed: &SeedContext{
			Type:                  "scene",
			Name:                  strings.TrimSpace(name),
			Subtitle:              strings.TrimSpace(subtitle),
			RepresentativeArtists: dedupeStrings(representativeArtists),
			RepresentativeTracks:  dedupeStrings(representativeTracks),
		},
	}, nil
}

func BuildPrompts(request Request) (string, string) {
	if request.Seed != nil {
		return BuildSeededPrompts(request)
	}
	systemPrompt := `You are an expert music curator for album discovery.

Return only strict JSON.

Task:
- Interpret the user's discovery request as a generic catalog search unless the prompt explicitly asks for personalization.
- Prefer critically acclaimed, widely respected, representative albums.
- Avoid obvious duplicates, live albums, deluxe editions, compilations, and box sets unless the user explicitly asks for them.
- Keep the output compact and useful for later Lidarr matching.

Output schema:
{
  "albums": [
    {
      "artistName": "Artist",
      "albumTitle": "Album",
      "year": 1973,
      "reason": "Short reason"
    }
  ]
}

Rules:
- Return up to the requested number of albums, not necessarily exactly that many.
- Return a strong ranked list with no filler picks.
- Keep reasons under 18 words.
- Prefer studio albums.
- If the user asks for a specific artist, focus on that artist.
- Reject compilations, greatest-hits sets, live albums, remasters, deluxe editions, anniversary editions, re-recordings, box sets, and soundtrack tie-ins unless the user explicitly asks for them.
- Avoid duplicate titles, obvious outliers, and weak late-career filler when stronger canonical albums exist.
- If the request targets one artist, every result should be by that artist.
- If confidence is low, omit uncertain candidates instead of guessing.
- Never include markdown or prose outside the JSON object.`

	userPrompt := fmt.Sprintf("Find up to %d high-confidence albums for this request: %s", request.RequestCount, request.Query)
	if request.ArtistHint != "" {
		userPrompt += fmt.Sprintf("\nArtist focus: %s", request.ArtistHint)
	}
	return systemPrompt, userPrompt
}

func BuildSeededPrompts(request Request) (string, string) {
	systemPrompt := `You are an expert music curator for album discovery.

Return only strict JSON.

Task:
- Use the structured seed context below as the primary guide for the recommendation space.
- Recommend external studio albums that are adjacent to the seed context, not a restatement of the exact same implied records.
- Prefer critically acclaimed, coherent, high-signal albums.
- Avoid obvious duplicates, live albums, deluxe editions, compilations, and box sets unless the request explicitly asks for them.
- Keep the output compact and useful for later Lidarr matching.

Output schema:
{
  "albums": [
    {
      "artistName": "Artist",
      "albumTitle": "Album",
      "year": 1973,
      "reason": "Short reason"
    }
  ]
}

Rules:
- Return up to the requested number of albums, not necessarily exactly that many.
- Return a strong ranked list with no filler picks.
- Keep reasons under 18 words.
- Prefer studio albums.
- Treat representative artists and tracks as stylistic anchors, not mandatory repeats.
- Prefer adjacent albums and artists when they better fit the seed context than obvious same-artist repeats.
- If confidence is low, omit uncertain candidates instead of guessing.
- Never include markdown or prose outside the JSON object.`

	seed := request.Seed
	lines := []string{
		fmt.Sprintf("Find up to %d high-confidence albums for this request: %s", request.RequestCount, request.Query),
		fmt.Sprintf("Seed type: %s", strings.TrimSpace(seed.Type)),
	}
	if name := strings.TrimSpace(seed.Name); name != "" {
		lines = append(lines, "Seed profile: "+name)
	}
	if subtitle := strings.TrimSpace(seed.Subtitle); subtitle != "" {
		lines = append(lines, "Seed feel: "+subtitle)
	}
	if len(seed.RepresentativeArtists) > 0 {
		lines = append(lines, "Representative artists: "+strings.Join(seed.RepresentativeArtists, ", "))
	}
	if len(seed.RepresentativeTracks) > 0 {
		lines = append(lines, "Representative tracks: "+strings.Join(seed.RepresentativeTracks, "; "))
	}
	return systemPrompt, strings.Join(lines, "\n")
}

func BuildFocusedPrompts(request Request) (string, string) {
	systemPrompt := fmt.Sprintf(`You are an expert music curator for album discovery.

Return only strict JSON matching the existing schema.

Task:
- Return only studio albums by the exact artist named below.
- Do not return albums by related artists, solo members, side projects, compilations, live albums, deluxe editions, remasters, box sets, or soundtrack releases.
- If unsure, omit the album rather than guessing.

Artist:
- %s

Output schema:
{
  "albums": [
    {
      "artistName": "Artist",
      "albumTitle": "Album",
      "year": 1973,
      "reason": "Short reason"
    }
  ]
}`, request.ArtistHint)
	userPrompt := fmt.Sprintf(
		"List up to %d canonical studio albums by %s that best satisfy: %s",
		request.RequestCount,
		request.ArtistHint,
		request.Query,
	)
	return systemPrompt, userPrompt
}

func ParseResponse(raw string) ([]Candidate, error) {
	var parsed struct {
		Albums []Candidate `json:"albums"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse discovery response: %w", err)
	}
	return parsed.Albums, nil
}

func Rank(request Request, albums []Candidate) []Candidate {
	scored := scoreCandidates(request.Query, request.ArtistHint, albums)
	if len(scored) == 0 {
		return nil
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			if scored[i].candidate.Year == scored[j].candidate.Year {
				return scored[i].candidate.AlbumTitle < scored[j].candidate.AlbumTitle
			}
			return scored[i].candidate.Year < scored[j].candidate.Year
		}
		return scored[i].score > scored[j].score
	})

	ranked := make([]Candidate, 0, request.Limit)
	for _, item := range scored {
		item.candidate.Rank = len(ranked) + 1
		ranked = append(ranked, item.candidate)
		if len(ranked) >= request.Limit {
			break
		}
	}
	return ranked
}

func InferArtistFocus(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return ""
	}
	lower := strings.ToLower(q)
	patterns := []string{
		"albums of ",
		"albums by ",
		"album of ",
		"album by ",
		"for ",
	}
	for _, pattern := range patterns {
		idx := strings.Index(lower, pattern)
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(q[idx+len(pattern):])
		rest = trimDiscoveryTail(rest)
		if rest != "" && (pattern != "for " || looksLikeArtistFocus(rest)) {
			return rest
		}
	}
	if strings.Contains(lower, " albums ") {
		if idx := strings.Index(lower, " albums "); idx > 0 {
			suffix := strings.TrimSpace(q[idx+len(" albums "):])
			suffix = trimDiscoveryLead(suffix)
			suffix = trimDiscoveryTail(suffix)
			if suffix != "" {
				return suffix
			}
		}
	}
	if strings.Contains(lower, " albums") {
		if idx := strings.Index(lower, " albums"); idx > 0 {
			prefix := strings.TrimSpace(q[:idx])
			prefix = trimDiscoveryLead(prefix)
			prefix = trimDiscoveryTail(prefix)
			if prefix != "" && !isGenericDiscoveryPhrase(prefix) {
				return prefix
			}
		}
	}
	return ""
}

func SelectCandidates(candidates []Candidate, selection string) ([]Candidate, error) {
	selection = strings.TrimSpace(selection)
	if selection == "" {
		return nil, fmt.Errorf("selection is required")
	}
	lower := strings.ToLower(selection)
	if isAllSelection(lower) {
		return candidates, nil
	}

	if n, ok := parseLeadingCountSelection(lower); ok {
		if n > len(candidates) {
			n = len(candidates)
		}
		if n <= 0 {
			return nil, fmt.Errorf("selection resolved to zero candidates")
		}
		return candidates[:n], nil
	}
	if ranks, ok := parseTrailingCountSelection(lower, len(candidates)); ok {
		return selectCandidatesByRank(candidates, ranks), nil
	}
	if ranks, ok := parseExplicitRankSelection(lower, len(candidates)); ok {
		return selectCandidatesByRank(candidates, ranks), nil
	}

	selected := make([]Candidate, 0, len(candidates))
	selectedByRank := make(map[int]struct{}, len(candidates))
	for _, part := range splitSelectionParts(lower) {
		for _, candidate := range matchSingleSelection(candidates, part) {
			if _, ok := selectedByRank[candidate.Rank]; ok {
				continue
			}
			selectedByRank[candidate.Rank] = struct{}{}
			selected = append(selected, candidate)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("selection did not match any discovered albums")
	}
	return selected, nil
}

func NormalizeTitle(s string) string {
	s = strings.ToLower(strings.TrimSpace(norm.NFD.String(s)))
	var result strings.Builder
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsSpace(r) {
			result.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(result.String()), " ")
}

func TitleSimilarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	if len(a) >= 4 && strings.Contains(b, a) {
		return 0.82
	}
	if len(b) >= 4 && strings.Contains(a, b) {
		return 0.82
	}
	aTokens := tokenSet(a)
	bTokens := tokenSet(b)
	if len(aTokens) == 0 || len(bTokens) == 0 {
		return 0
	}
	intersections := 0
	union := make(map[string]struct{}, len(aTokens)+len(bTokens))
	for token := range aTokens {
		union[token] = struct{}{}
		if _, ok := bTokens[token]; ok {
			intersections++
		}
	}
	for token := range bTokens {
		union[token] = struct{}{}
	}
	return float64(intersections) / float64(len(union))
}

func clampLimit(limit int) int {
	if limit < 1 {
		return 5
	}
	if limit > 8 {
		return 8
	}
	return limit
}

func clampRequestCount(count int) int {
	if count > 12 {
		return 12
	}
	return count
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func scoreCandidates(query, artistHint string, albums []Candidate) []scoredCandidate {
	seen := make(map[string]struct{}, len(albums))
	scored := make([]scoredCandidate, 0, len(albums))
	for _, item := range albums {
		artistName := strings.TrimSpace(item.ArtistName)
		albumTitle := strings.TrimSpace(item.AlbumTitle)
		if artistName == "" || albumTitle == "" {
			continue
		}
		if shouldRejectCandidate(query, artistHint, artistName, albumTitle) {
			continue
		}
		key := NormalizeTitle(artistName) + "::" + NormalizeTitle(albumTitle)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		item.ArtistName = artistName
		item.AlbumTitle = albumTitle
		item.Reason = strings.TrimSpace(item.Reason)
		scored = append(scored, scoredCandidate{
			candidate: item,
			score:     scoreCandidate(query, artistHint, item),
		})
	}
	return scored
}

func isGenericDiscoveryPhrase(v string) bool {
	normalized := strings.ToLower(strings.TrimSpace(v))
	switch normalized {
	case "", "best", "top", "essential", "starter", "great", "what are", "what are best", "what are top", "show me", "recommend":
		return true
	default:
		return false
	}
}

func trimDiscoveryLead(v string) string {
	lower := strings.ToLower(strings.TrimSpace(v))
	leadPatterns := []string{
		"best ",
		"top ",
		"essential ",
		"starter ",
		"great ",
		"five ",
		"4 ",
		"5 ",
		"6 ",
		"7 ",
		"8 ",
	}
	for _, pattern := range leadPatterns {
		if strings.HasPrefix(lower, pattern) {
			v = strings.TrimSpace(v[len(pattern):])
			lower = strings.ToLower(v)
		}
	}
	for len(v) > 0 && (v[0] >= '0' && v[0] <= '9') {
		v = strings.TrimSpace(v[1:])
	}
	return strings.TrimSpace(v)
}

func trimDiscoveryTail(v string) string {
	cutters := []string{
		" in my library",
		" on lidarr",
		" from lidarr",
		" and",
		" please",
	}
	lower := strings.ToLower(v)
	for _, cutter := range cutters {
		if idx := strings.Index(lower, cutter); idx >= 0 {
			v = strings.TrimSpace(v[:idx])
			lower = strings.ToLower(v)
		}
	}
	return strings.Trim(v, " .,!?\"'")
}

func looksLikeArtistFocus(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	tokens := strings.Fields(strings.ToLower(strings.ReplaceAll(v, "-", " ")))
	if len(tokens) == 0 {
		return false
	}
	if len(tokens) >= 4 {
		return false
	}
	moodish := map[string]struct{}{
		"rainy": {}, "late": {}, "night": {}, "walk": {}, "drive": {}, "study": {}, "sleep": {}, "coding": {},
		"workout": {}, "focus": {}, "mood": {}, "moods": {}, "vibe": {}, "vibes": {}, "scene": {}, "background": {},
		"morning": {}, "evening": {}, "afternoon": {}, "party": {}, "dinner": {}, "reading": {}, "gym": {},
	}
	for _, token := range tokens {
		if _, ok := moodish[token]; ok {
			return false
		}
	}
	return true
}

func shouldRejectCandidate(query, artistHint, artistName, albumTitle string) bool {
	if strings.TrimSpace(artistHint) != "" {
		hintNorm := NormalizeTitle(artistHint)
		artistNorm := NormalizeTitle(artistName)
		if !artistMatchesHint(hintNorm, artistNorm) {
			return true
		}
	}

	titleNorm := NormalizeTitle(albumTitle)
	if titleNorm == "" {
		return true
	}

	rejectTerms := []string{
		"greatest hits",
		"best of",
		"anthology",
		"collection",
		"collections",
		"box set",
		"live",
		"deluxe",
		"expanded",
		"anniversary",
		"remaster",
		"re recorded",
		"soundtrack",
		"motion picture",
		"karaoke",
		"instrumental hits",
		"rarities",
	}
	lowerTitle := strings.ToLower(albumTitle)
	lowerQuery := strings.ToLower(query)
	for _, term := range rejectTerms {
		if strings.Contains(lowerTitle, term) && !strings.Contains(lowerQuery, term) {
			return true
		}
	}

	if strings.Contains(lowerTitle, "vol.") || strings.Contains(lowerTitle, "volume ") {
		if !strings.Contains(lowerQuery, "vol.") && !strings.Contains(lowerQuery, "volume") {
			return true
		}
	}

	if shouldRejectQueryEchoCandidate(query, artistHint, artistName, albumTitle) {
		return true
	}

	return false
}

func shouldRejectQueryEchoCandidate(query, artistHint, artistName, albumTitle string) bool {
	if strings.TrimSpace(artistHint) != "" {
		return false
	}

	queryNorm := NormalizeTitle(query)
	titleNorm := NormalizeTitle(albumTitle)
	artistNorm := NormalizeTitle(artistName)
	if queryNorm == "" {
		return false
	}

	if titleNorm != "" {
		if titleNorm == queryNorm {
			return true
		}
		if len(titleNorm) >= 10 && strings.Contains(queryNorm, titleNorm) {
			return true
		}
		if len(titleNorm) >= 14 && TitleSimilarity(queryNorm, titleNorm) >= 0.78 {
			return true
		}
	}

	if artistNorm != "" {
		if len(artistNorm) >= 12 && strings.Contains(queryNorm, artistNorm) {
			return true
		}
		if len(artistNorm) >= 16 && TitleSimilarity(queryNorm, artistNorm) >= 0.80 {
			return true
		}
	}

	if titleNorm != "" && artistNorm != "" {
		combined := strings.TrimSpace(artistNorm + " " + titleNorm)
		if len(combined) >= 18 && TitleSimilarity(queryNorm, combined) >= 0.72 {
			return true
		}
	}

	return false
}

func artistMatchesHint(hintNorm, artistNorm string) bool {
	if hintNorm == "" || artistNorm == "" {
		return false
	}
	if hintNorm == artistNorm {
		return true
	}
	if len(hintNorm) >= 4 && strings.Contains(artistNorm, hintNorm) {
		return true
	}
	if len(artistNorm) >= 4 && strings.Contains(hintNorm, artistNorm) {
		return true
	}
	if TitleSimilarity(hintNorm, artistNorm) >= 0.60 {
		return true
	}
	hintTokens := tokenSet(hintNorm)
	artistTokens := tokenSet(artistNorm)
	if len(hintTokens) == 0 || len(artistTokens) == 0 {
		return false
	}
	matched := 0
	for token := range hintTokens {
		if _, ok := artistTokens[token]; ok {
			matched++
		}
	}
	return matched == len(hintTokens) || matched == len(artistTokens)
}

func scoreCandidate(query, artistHint string, candidate Candidate) float64 {
	score := 0.0

	if strings.TrimSpace(artistHint) != "" {
		score += 2.0 * TitleSimilarity(NormalizeTitle(artistHint), NormalizeTitle(candidate.ArtistName))
	}

	reason := strings.ToLower(strings.TrimSpace(candidate.Reason))
	preferredReasonTerms := []string{
		"iconic",
		"classic",
		"masterpiece",
		"acclaimed",
		"influential",
		"essential",
		"groundbreaking",
		"canonical",
	}
	for _, term := range preferredReasonTerms {
		if strings.Contains(reason, term) {
			score += 0.45
		}
	}

	penaltyTerms := []string{
		"late-career",
		"compilation",
		"solo work",
		"odds and ends",
		"rarities",
	}
	for _, term := range penaltyTerms {
		if strings.Contains(reason, term) {
			score -= 1.2
		}
	}

	if candidate.Year > 0 {
		score += 0.25
		if candidate.Year >= 1965 && candidate.Year <= 1999 {
			score += 0.15
		}
	}

	title := strings.ToLower(candidate.AlbumTitle)
	titlePenaltyTerms := []string{
		"best of",
		"greatest hits",
		"collection",
		"anthology",
		"soundtrack",
		"live",
	}
	for _, term := range titlePenaltyTerms {
		if strings.Contains(title, term) {
			score -= 1.8
		}
	}

	if strings.Contains(strings.ToLower(query), "best") || strings.Contains(strings.ToLower(query), "essential") {
		score += 0.2
	}

	return score
}

func matchSingleSelection(candidates []Candidate, selection string) []Candidate {
	selectionNorm := NormalizeTitle(selection)
	selectionAlbumNorm, selectionArtistNorm := splitAlbumArtistSelection(selectionNorm)

	selected := make([]Candidate, 0)
	for _, candidate := range candidates {
		artistNorm := NormalizeTitle(candidate.ArtistName)
		albumNorm := NormalizeTitle(candidate.AlbumTitle)
		combinedNorm := albumNorm
		if artistNorm != "" {
			combinedNorm = albumNorm + " by " + artistNorm
		}
		switch {
		case selectionAlbumNorm != "" && selectionArtistNorm != "":
			if albumNorm == selectionAlbumNorm && artistNorm == selectionArtistNorm {
				selected = append(selected, candidate)
				continue
			}
			if strings.Contains(combinedNorm, selectionAlbumNorm) && strings.Contains(combinedNorm, selectionArtistNorm) {
				selected = append(selected, candidate)
				continue
			}
		case strings.Contains(artistNorm, selectionNorm) || strings.Contains(albumNorm, selectionNorm) || strings.Contains(combinedNorm, selectionNorm):
			selected = append(selected, candidate)
		}
	}
	return selected
}

func splitSelectionParts(selection string) []string {
	selection = strings.TrimSpace(selection)
	if selection == "" {
		return nil
	}
	if !strings.Contains(selection, ",") {
		return []string{selection}
	}

	rawParts := strings.Split(selection, ",")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		part = strings.TrimSpace(strings.Trim(part, "\"'"))
		if part == "" {
			continue
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return []string{selection}
	}
	return parts
}

func splitAlbumArtistSelection(selectionNorm string) (string, string) {
	if selectionNorm == "" {
		return "", ""
	}
	if strings.Contains(selectionNorm, " by ") {
		parts := strings.SplitN(selectionNorm, " by ", 2)
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	if strings.Contains(selectionNorm, " from ") {
		parts := strings.SplitN(selectionNorm, " from ", 2)
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "", ""
}

func parseLeadingCountSelection(selection string) (int, bool) {
	fields := strings.Fields(selection)
	if len(fields) < 2 {
		return 0, false
	}
	switch fields[0] {
	case "first", "top":
	default:
		return 0, false
	}
	n, err := strconv.Atoi(fields[1])
	if err == nil {
		return n, true
	}
	wordToNum := map[string]int{
		"one":   1,
		"two":   2,
		"three": 3,
		"four":  4,
		"five":  5,
		"six":   6,
		"seven": 7,
		"eight": 8,
	}
	n, ok := wordToNum[fields[1]]
	return n, ok
}

func isAllSelection(selection string) bool {
	switch strings.TrimSpace(selection) {
	case "all", "those", "them", "these", "everything", "all of them", "all of those", "all of these":
		return true
	default:
		return false
	}
}

func parseTrailingCountSelection(selection string, total int) ([]int, bool) {
	normalized := strings.TrimSpace(strings.TrimPrefix(selection, "the "))
	normalized = strings.ReplaceAll(normalized, "final", "last")
	fields := strings.Fields(normalized)
	if len(fields) == 0 || fields[0] != "last" || total <= 0 {
		return nil, false
	}
	if len(fields) == 1 {
		return []int{total}, true
	}
	if isRankLabel(fields[1]) {
		return []int{total}, true
	}
	n, ok := parseRankToken(fields[1])
	if !ok {
		return nil, false
	}
	if n > total {
		n = total
	}
	if n <= 0 {
		return nil, false
	}
	start := total - n + 1
	ranks := make([]int, 0, n)
	for i := start; i <= total; i++ {
		ranks = append(ranks, i)
	}
	return ranks, true
}

func parseExplicitRankSelection(selection string, total int) ([]int, bool) {
	if total <= 0 {
		return nil, false
	}
	normalized := strings.TrimSpace(strings.TrimPrefix(selection, "the "))
	normalized = strings.NewReplacer("&", ",", " and ", ",", "\"", "", "'", "").Replace(normalized)
	parts := strings.Split(normalized, ",")
	ranks := make([]int, 0, len(parts))
	seen := make(map[int]struct{}, len(parts))
	for _, part := range parts {
		part = normalizeRankSelectionPart(part)
		if part == "" {
			continue
		}
		n, ok := parseRankToken(part)
		if !ok || n <= 0 || n > total {
			return nil, false
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		ranks = append(ranks, n)
	}
	if len(ranks) == 0 {
		return nil, false
	}
	return ranks, true
}

func normalizeRankSelectionPart(part string) string {
	part = strings.TrimSpace(part)
	part = strings.Trim(part, ".!?")
	prefixes := []string{
		"album ", "albums ", "record ", "records ", "result ", "results ",
		"item ", "items ", "candidate ", "candidates ", "rank ", "number ", "no ",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(part, prefix) {
			part = strings.TrimSpace(strings.TrimPrefix(part, prefix))
			break
		}
	}
	if isRankLabel(part) {
		return ""
	}
	return strings.TrimPrefix(part, "#")
}

func isRankLabel(part string) bool {
	switch strings.TrimSpace(part) {
	case "album", "albums", "record", "records", "result", "results", "item", "items", "candidate", "candidates":
		return true
	default:
		return false
	}
}

func parseRankToken(raw string) (int, bool) {
	token := strings.TrimSpace(strings.ToLower(raw))
	token = strings.Trim(token, ".!?")
	token = strings.TrimPrefix(token, "#")
	if token == "" {
		return 0, false
	}
	for _, suffix := range []string{"st", "nd", "rd", "th"} {
		if strings.HasSuffix(token, suffix) {
			if n, err := strconv.Atoi(strings.TrimSuffix(token, suffix)); err == nil {
				return n, n > 0
			}
		}
	}
	if n, err := strconv.Atoi(token); err == nil {
		return n, n > 0
	}
	switch token {
	case "one", "first":
		return 1, true
	case "two", "second":
		return 2, true
	case "three", "third":
		return 3, true
	case "four", "fourth":
		return 4, true
	case "five", "fifth":
		return 5, true
	case "six", "sixth":
		return 6, true
	case "seven", "seventh":
		return 7, true
	case "eight", "eighth":
		return 8, true
	case "nine", "ninth":
		return 9, true
	case "ten", "tenth":
		return 10, true
	default:
		return 0, false
	}
}

func selectCandidatesByRank(candidates []Candidate, ranks []int) []Candidate {
	if len(ranks) == 0 {
		return nil
	}
	byRank := make(map[int]Candidate, len(candidates))
	for _, candidate := range candidates {
		byRank[candidate.Rank] = candidate
	}
	selected := make([]Candidate, 0, len(ranks))
	for _, rank := range ranks {
		if candidate, ok := byRank[rank]; ok {
			selected = append(selected, candidate)
		}
	}
	return selected
}

func tokenSet(v string) map[string]struct{} {
	tokens := strings.Fields(v)
	set := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		if token == "" {
			continue
		}
		set[token] = struct{}{}
	}
	return set
}
