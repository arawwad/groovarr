package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"groovarr/internal/agent"
	"groovarr/internal/db"
)

func creativeResultSetCapability(setKind string) resultSetCapability {
	return resultSetCapability{
		SetKind:    strings.TrimSpace(setKind),
		Operations: []string{"filter_by_play_window", "pick_riskier", "pick_safer", "refine_style", "most_recent"},
		Selectors:  []string{"all", "item_key"},
	}
}

func (s *Server) handleUnderplayedAlbums(ctx context.Context, rawMsg string) (string, bool) {
	if s.dbClient == nil {
		return "", false
	}
	lower := strings.ToLower(strings.TrimSpace(rawMsg))
	if !isUnderplayedAlbumPrompt(lower) {
		return "", false
	}

	candidates, err := s.buildUnderplayedAlbumCandidates(ctx, rawMsg, 8)
	if err != nil {
		return "", false
	}
	if len(candidates) == 0 {
		return "I couldn't find convincing underplayed album picks in your library yet.", true
	}

	setLastCreativeAlbumSet(chatSessionIDFromContext(ctx), "underplayed_albums", rawMsg, candidates)
	return renderCreativeAlbumSet("Three underplayed records from your library", candidates, 3), true
}

func (s *Server) handleStructuredCreativeAlbumSetFollowUp(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	outcome, ok := s.resolveStructuredCreativeAlbumSetFollowUpOutcome(ctx, resolved)
	if !ok {
		return "", false
	}
	return renderStructuredCreativeAlbumSetFollowUp(outcome)
}

func (s *Server) handleStructuredCreativeAlbumSetFollowUpTurn(ctx context.Context, turn *Turn) (string, bool) {
	return s.handleStructuredCreativeAlbumSetFollowUp(ctx, turnToResolvedTurnContext(turn))
}

type creativeFollowUpOutcome struct {
	kind       string
	single     *creativeAlbumCandidate
	candidates []creativeAlbumCandidate
	windowFrom time.Time
	windowTo   time.Time
}

func (s *Server) resolveStructuredCreativeAlbumSetFollowUpOutcome(ctx context.Context, resolved *resolvedTurnContext) (creativeFollowUpOutcome, bool) {
	if resolved == nil {
		return creativeFollowUpOutcome{}, false
	}
	sessionID := chatSessionIDFromContext(ctx)
	candidates, mode, ok := creativeCandidatesFromResolvedReference(sessionID, resolved)
	if !ok {
		return creativeFollowUpOutcome{}, false
	}

	switch strings.TrimSpace(resolved.Turn.SubIntent) {
	case "result_set_most_recent":
		latest, ok := mostRecentlyPlayedCreativeCandidate(candidates)
		if !ok {
			return creativeFollowUpOutcome{kind: "most_recent_empty"}, true
		}
		setLastFocusedResultItem(sessionID, resolvedCreativeReferenceKind(resolved), normalizedCreativeAlbumCandidateKey(latest))
		return creativeFollowUpOutcome{kind: "most_recent_single", single: &latest}, true
	case "creative_risk_pick":
		pick, ok := chooseRiskierCreativeCandidate(candidates)
		if !ok {
			return creativeFollowUpOutcome{}, false
		}
		setLastFocusedResultItem(sessionID, resolvedCreativeReferenceKind(resolved), normalizedCreativeAlbumCandidateKey(pick))
		return creativeFollowUpOutcome{kind: "risk_pick", single: &pick}, true
	case "creative_safe_pick":
		pick, ok := chooseSaferCreativeCandidate(candidates)
		if !ok {
			return creativeFollowUpOutcome{}, false
		}
		setLastFocusedResultItem(sessionID, resolvedCreativeReferenceKind(resolved), normalizedCreativeAlbumCandidateKey(pick))
		return creativeFollowUpOutcome{kind: "safe_pick", single: &pick}, true
	case "creative_refinement":
		if len(resolved.Turn.StyleHints) == 0 {
			return creativeFollowUpOutcome{}, false
		}
		refined := s.refineCreativeAlbumCandidates(ctx, strings.Join(resolved.Turn.StyleHints, " "), candidates)
		if len(refined) == 0 {
			return creativeFollowUpOutcome{kind: "refinement_empty"}, true
		}
		setLastCreativeAlbumSet(sessionID, mode, strings.Join(resolved.Turn.StyleHints, " "), refined)
		return creativeFollowUpOutcome{kind: "refinement_set", candidates: refined}, true
	default:
		return creativeFollowUpOutcome{}, false
	}
}

func creativeExecutionHandlers() []serverExecutionHandler {
	return []serverExecutionHandler{
		{
			name: "creative_result_set_listening_followup",
			canHandle: func(turn *Turn) bool {
				request := executionRequestFromTurn(turn)
				setKind := strings.TrimSpace(request.SetKind)
				operation := strings.TrimSpace(request.Operation)
				return (setKind == "creative_albums" || setKind == "semantic_albums") &&
					(operation == "filter_by_play_window" || operation == "most_recent")
			},
			executeWithTurn: func(ctx context.Context, s *Server, _ []agent.Message, turn *Turn) (ChatResponse, bool) {
				outcome, ok := s.resolveAlbumResultSetListeningFollowUpOutcome(ctx, turnToResolvedTurnContext(turn))
				if !ok {
					return ChatResponse{}, false
				}
				if resp, ok := renderAlbumResultSetListeningFollowUp(outcome); ok {
					return ChatResponse{Response: resp}, true
				}
				return ChatResponse{}, false
			},
		},
		{
			name: "creative_result_set_followup",
			canHandle: func(turn *Turn) bool {
				request := executionRequestFromTurn(turn)
				setKind := strings.TrimSpace(request.SetKind)
				operation := strings.TrimSpace(request.Operation)
				if setKind != "creative_albums" && setKind != "semantic_albums" {
					return false
				}
				switch operation {
				case "pick_riskier", "pick_safer", "refine_style":
					return true
				default:
					return false
				}
			},
			executeWithTurn: func(ctx context.Context, s *Server, _ []agent.Message, turn *Turn) (ChatResponse, bool) {
				outcome, ok := s.resolveStructuredCreativeAlbumSetFollowUpOutcome(ctx, turnToResolvedTurnContext(turn))
				if !ok {
					return ChatResponse{}, false
				}
				if resp, ok := renderStructuredCreativeAlbumSetFollowUp(outcome); ok {
					return ChatResponse{Response: resp}, true
				}
				return ChatResponse{}, false
			},
		},
	}
}

func renderStructuredCreativeAlbumSetFollowUp(outcome creativeFollowUpOutcome) (string, bool) {
	switch outcome.kind {
	case "most_recent_empty":
		return "None of those look recently played.", true
	case "most_recent_single":
		if outcome.single == nil {
			return "", false
		}
		return fmt.Sprintf("The one you've touched most recently is %s.", formatCreativeAlbumCandidate(*outcome.single, true)), true
	case "risk_pick":
		if outcome.single == nil {
			return "", false
		}
		return fmt.Sprintf("The riskier pick is %s.", formatCreativeAlbumCandidate(*outcome.single, true)), true
	case "safe_pick":
		if outcome.single == nil {
			return "", false
		}
		return fmt.Sprintf("The safer pick is %s.", formatCreativeAlbumCandidate(*outcome.single, true)), true
	case "refinement_empty":
		return "I couldn't reshape those picks convincingly yet. Give me one clearer cue like warmer, darker, more intimate, or less electronic.", true
	case "refinement_set":
		if len(outcome.candidates) == 0 {
			return "", false
		}
		return renderCreativeAlbumSet("Reshaping those picks", outcome.candidates, 3), true
	default:
		return "", false
	}
}

func describeStructuredRecentListeningInterpretation(state recentListeningState, subIntent string) (string, bool) {
	switch strings.TrimSpace(subIntent) {
	case "listening_interpretation":
		return describeRecentListeningTaste(state), true
	case "artist_dominance":
		return describeRecentListeningDominance(state, ""), true
	default:
		return "", false
	}
}

func isUnderplayedAlbumPrompt(lower string) bool {
	if lower == "" {
		return false
	}
	ownership := containsLibraryOwnershipCue(lower) || strings.Contains(lower, "i own") || strings.Contains(lower, "owned")
	if !ownership {
		return false
	}
	cues := []string{
		"underplay",
		"underplayed",
		"neglect",
		"neglected",
		"overlooked",
		"overlook",
		"forgotten",
	}
	for _, cue := range cues {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

func (s *Server) buildUnderplayedAlbumCandidates(ctx context.Context, rawMsg string, limit int) ([]creativeAlbumCandidate, error) {
	features, err := s.dbClient.ListAlbumTasteFeatures(ctx, 48)
	if err != nil {
		return nil, err
	}

	type rankedCandidate struct {
		candidate creativeAlbumCandidate
		score     float64
	}
	ranked := make([]rankedCandidate, 0, len(features))
	seen := make(map[string]struct{})
	for _, feature := range features {
		candidate, ok := s.enrichCreativeAlbumCandidate(ctx, feature)
		if !ok {
			continue
		}
		key := normalizedCreativeAlbumCandidateKey(candidate)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		score := underplayedCandidateScore(feature, candidate)
		score += 0.18 * creativePreferenceScore(strings.ToLower(strings.TrimSpace(rawMsg)), candidate)
		if score <= 0 {
			continue
		}
		seen[key] = struct{}{}
		ranked = append(ranked, rankedCandidate{candidate: candidate, score: score})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return normalizedCreativeAlbumCandidateKey(ranked[i].candidate) < normalizedCreativeAlbumCandidateKey(ranked[j].candidate)
		}
		return ranked[i].score > ranked[j].score
	})
	if limit <= 0 {
		limit = 5
	}
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	out := make([]creativeAlbumCandidate, 0, len(ranked))
	for _, item := range ranked {
		out = append(out, item.candidate)
	}
	return out, nil
}

func (s *Server) enrichCreativeAlbumCandidate(ctx context.Context, feature db.TasteProfileAlbumFeature) (creativeAlbumCandidate, bool) {
	id := strings.TrimSpace(feature.AlbumID)
	name := strings.TrimSpace(feature.AlbumName)
	artist := strings.TrimSpace(feature.ArtistName)
	if id == "" || name == "" || artist == "" {
		return creativeAlbumCandidate{}, false
	}

	candidate := creativeAlbumCandidate{
		ID:         id,
		Name:       name,
		ArtistName: artist,
		PlayCount:  feature.TotalPlays,
	}
	if feature.LastPlayed != nil {
		candidate.LastPlayed = feature.LastPlayed.UTC().Format(time.RFC3339)
	}

	album, err := s.dbClient.GetAlbumByID(ctx, id)
	if err == nil && album != nil {
		if strings.TrimSpace(album.Name) != "" {
			candidate.Name = strings.TrimSpace(album.Name)
		}
		if strings.TrimSpace(album.ArtistName) != "" {
			candidate.ArtistName = strings.TrimSpace(album.ArtistName)
		}
		candidate.PlayCount = album.PlayCount
		if album.LastPlayed != nil {
			candidate.LastPlayed = album.LastPlayed.UTC().Format(time.RFC3339)
		}
		if album.Year != nil {
			candidate.Year = *album.Year
		}
		if album.Genre != nil {
			candidate.Genre = strings.TrimSpace(*album.Genre)
		}
	}
	return candidate, true
}

func underplayedCandidateScore(feature db.TasteProfileAlbumFeature, candidate creativeAlbumCandidate) float64 {
	score := 0.0
	score += (1 - clampUnit(feature.OverexposureScore)) * 0.25
	if feature.RecentPlays == 0 {
		score += 0.20
	}
	switch {
	case feature.TotalPlays == 0:
		score += 0.12
	case feature.TotalPlays <= 6:
		score += 0.24
	case feature.TotalPlays <= 12:
		score += 0.12
	}
	if lastPlayed, ok := parseCreativeAlbumTime(candidate.LastPlayed); ok {
		age := time.Since(lastPlayed)
		switch {
		case age >= 180*24*time.Hour:
			score += 0.30
		case age >= 90*24*time.Hour:
			score += 0.20
		case age >= 45*24*time.Hour:
			score += 0.10
		case age <= 21*24*time.Hour:
			score -= 0.20
		}
	} else {
		score += 0.18
	}
	if candidate.PlayCount > 18 {
		score -= 0.20
	}
	return score
}

func respondToCreativeAlbumRecencyFollowUp(lower string, candidates []creativeAlbumCandidate) (string, bool) {
	if len(candidates) == 0 {
		return "", false
	}
	if strings.Contains(lower, "most recently") || strings.Contains(lower, "touched most recently") {
		latest, ok := mostRecentlyPlayedCreativeCandidate(candidates)
		if !ok {
			return "None of those look recently played.", true
		}
		return fmt.Sprintf("The one you've touched most recently is %s.", formatCreativeAlbumCandidate(latest, true)), true
	}
	return "", false
}

func filterCreativeCandidatesByLastPlayed(candidates []creativeAlbumCandidate, keep func(time.Time) bool) []creativeAlbumCandidate {
	out := make([]creativeAlbumCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		ts, ok := parseCreativeAlbumTime(candidate.LastPlayed)
		if !ok || !keep(ts) {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func mostRecentlyPlayedCreativeCandidate(candidates []creativeAlbumCandidate) (creativeAlbumCandidate, bool) {
	var (
		best   creativeAlbumCandidate
		bestTS time.Time
		ok     bool
	)
	for _, candidate := range candidates {
		ts, valid := parseCreativeAlbumTime(candidate.LastPlayed)
		if !valid {
			continue
		}
		if !ok || ts.After(bestTS) {
			best = candidate
			bestTS = ts
			ok = true
		}
	}
	return best, ok
}

func chooseRiskierCreativeCandidate(candidates []creativeAlbumCandidate) (creativeAlbumCandidate, bool) {
	if len(candidates) == 0 {
		return creativeAlbumCandidate{}, false
	}
	best := candidates[0]
	bestScore := creativeRiskScore(best)
	for _, candidate := range candidates[1:] {
		score := creativeRiskScore(candidate)
		if score > bestScore {
			best = candidate
			bestScore = score
		}
	}
	return best, true
}

func chooseSaferCreativeCandidate(candidates []creativeAlbumCandidate) (creativeAlbumCandidate, bool) {
	if len(candidates) == 0 {
		return creativeAlbumCandidate{}, false
	}
	best := candidates[0]
	bestScore := creativeRiskScore(best)
	for _, candidate := range candidates[1:] {
		score := creativeRiskScore(candidate)
		if score < bestScore {
			best = candidate
			bestScore = score
		}
	}
	return best, true
}

func creativeCandidatesFromResolvedReference(sessionID string, resolved *resolvedTurnContext) ([]creativeAlbumCandidate, string, bool) {
	if resolved == nil {
		return nil, "", false
	}
	memory := loadTurnSessionMemory(sessionID)
	ref := resolved.resultReference()
	switch ref.effectiveSetKind() {
	case "", "creative_albums":
		candidates, updatedAt, mode, _, ok := memory.CreativeAlbumSet()
		if !ok || len(candidates) == 0 || updatedAt.IsZero() || time.Since(updatedAt) > llmContextCreativeAlbumTTL {
			return nil, "", false
		}
		return candidates, mode, true
	case "semantic_albums":
		matches, updatedAt, queryText, ok := memory.SemanticAlbumSearch()
		if !ok || len(matches) == 0 || updatedAt.IsZero() || time.Since(updatedAt) > llmContextSemanticAlbumTTL {
			return nil, "", false
		}
		mode := "semantic_album_search"
		if strings.TrimSpace(queryText) != "" {
			mode = strings.TrimSpace(queryText)
		}
		return semanticMatchesToCreativeCandidates(matches), mode, true
	default:
		return nil, "", false
	}
}

func resolvedCreativeReferenceKind(resolved *resolvedTurnContext) string {
	if resolved == nil {
		return "creative_albums"
	}
	ref := resolved.resultReference()
	if ref.effectiveSetKind() == "" {
		return "creative_albums"
	}
	return ref.effectiveSetKind()
}

func narrowCreativeCandidatesToFocusedItem(candidates []creativeAlbumCandidate, focusedKey string) []creativeAlbumCandidate {
	focusedKey = strings.TrimSpace(focusedKey)
	if focusedKey == "" || len(candidates) == 0 {
		return candidates
	}
	for _, candidate := range candidates {
		if normalizedCreativeAlbumCandidateKey(candidate) == focusedKey {
			return []creativeAlbumCandidate{candidate}
		}
	}
	return nil
}

func creativeRiskScore(candidate creativeAlbumCandidate) float64 {
	text := strings.ToLower(strings.TrimSpace(candidate.Genre + " " + candidate.Name + " " + candidate.ArtistName))
	score := 0.0
	for _, cue := range []string{"experimental", "dark ambient", "free jazz", "outsider", "avant", "idm", "noise", "drone"} {
		if strings.Contains(text, cue) {
			score += 1.2
		}
	}
	if candidate.PlayCount == 0 {
		score += 0.5
	} else if candidate.PlayCount <= 2 {
		score += 0.25
	}
	return score
}

func (s *Server) refineCreativeAlbumCandidates(ctx context.Context, rawMsg string, candidates []creativeAlbumCandidate) []creativeAlbumCandidate {
	if len(candidates) == 0 {
		return nil
	}
	lower := strings.ToLower(strings.TrimSpace(rawMsg))
	if lower == "" {
		return nil
	}
	if semanticMatches, ok := s.semanticAlbumMatches(ctx, rawMsg, maxInt(len(candidates)*3, 12)); ok {
		byKey := make(map[string]creativeAlbumCandidate, len(candidates))
		for _, candidate := range candidates {
			byKey[normalizedCreativeAlbumCandidateKey(candidate)] = candidate
		}
		refined := make([]creativeAlbumCandidate, 0, len(candidates))
		for _, match := range semanticMatches {
			key := normalizedCreativeAlbumCandidateKey(creativeAlbumCandidate{
				ID:         match.ID,
				Name:       match.Name,
				ArtistName: match.ArtistName,
			})
			if candidate, exists := byKey[key]; exists {
				if match.Year > 0 {
					candidate.Year = match.Year
				}
				if strings.TrimSpace(match.Genre) != "" {
					candidate.Genre = strings.TrimSpace(match.Genre)
				}
				if strings.TrimSpace(match.LastPlayed) != "" {
					candidate.LastPlayed = strings.TrimSpace(match.LastPlayed)
				}
				if match.PlayCount > 0 {
					candidate.PlayCount = match.PlayCount
				}
				refined = append(refined, candidate)
			}
		}
		if len(refined) > 0 {
			return refined
		}
	}

	type scored struct {
		candidate creativeAlbumCandidate
		score     float64
	}
	scoredCandidates := make([]scored, 0, len(candidates))
	for _, candidate := range candidates {
		scoredCandidates = append(scoredCandidates, scored{
			candidate: candidate,
			score:     creativePreferenceScore(lower, candidate),
		})
	}
	sort.SliceStable(scoredCandidates, func(i, j int) bool {
		if scoredCandidates[i].score == scoredCandidates[j].score {
			return normalizedCreativeAlbumCandidateKey(scoredCandidates[i].candidate) < normalizedCreativeAlbumCandidateKey(scoredCandidates[j].candidate)
		}
		return scoredCandidates[i].score > scoredCandidates[j].score
	})
	out := make([]creativeAlbumCandidate, 0, len(scoredCandidates))
	for _, item := range scoredCandidates {
		out = append(out, item.candidate)
	}
	return out
}

func creativePreferenceScore(lower string, candidate creativeAlbumCandidate) float64 {
	text := strings.ToLower(strings.TrimSpace(candidate.Genre + " " + candidate.Name + " " + candidate.ArtistName))
	score := 0.0
	if strings.Contains(lower, "less electronic") {
		if containsAny(text, "electronic", "idm", "synth", "trip hop", "ambient techno", "electronica") {
			score -= 2.0
		} else {
			score += 0.4
		}
	}
	if strings.Contains(lower, "more intimate") {
		if containsAny(text, "folk", "singer-songwriter", "jazz", "chamber", "acoustic", "indie") {
			score += 1.6
		}
	}
	if strings.Contains(lower, "less canonical") {
		if candidate.PlayCount <= 2 {
			score += 1.2
		}
	}
	if strings.Contains(lower, "more lived-in") || strings.Contains(lower, "more lived in") {
		if candidate.PlayCount > 0 {
			score += 1.0
		}
	}
	if strings.Contains(lower, "warmer") {
		if containsAny(text, "jazz", "soul", "chamber pop", "ambient pop", "dream pop") {
			score += 1.0
		}
	}
	if strings.Contains(lower, "darker") || strings.Contains(lower, "nocturnal") {
		if containsAny(text, "dark ambient", "trip hop", "ambient", "dream pop", "experimental") {
			score += 1.0
		}
	}
	return score
}

func describeRecentListeningTaste(state recentListeningState) string {
	if len(state.topArtists) == 0 {
		return "I don't have enough recent listening detail to read your taste from that window yet."
	}
	top := state.topArtists[0]
	secondary := ""
	if len(state.topArtists) > 1 {
		secondary = state.topArtists[1].ArtistName
	}
	switch {
	case len(state.topArtists) > 1 && top.TrackCount >= state.topArtists[1].TrackCount*3:
		if secondary != "" {
			return fmt.Sprintf("Right now your taste looks highly replay-driven around %s, with %s as a distant secondary anchor. It reads focused, introspective, and a bit obsessive rather than broad or novelty-seeking.", top.ArtistName, secondary)
		}
		return fmt.Sprintf("Right now your taste looks highly replay-driven around %s. It reads focused and repeat-heavy rather than broad or novelty-seeking.", top.ArtistName)
	case state.artistsHeard >= 20:
		return fmt.Sprintf("Your recent taste looks broad but still centered on %s. It reads exploratory overall, with a clear pull toward moody, melodic records rather than one narrow lane.", top.ArtistName)
	default:
		return fmt.Sprintf("Your recent taste looks fairly focused, with %s leading the window. It reads like a concentrated phase rather than a scattershot run through the library.", top.ArtistName)
	}
}

func describeRecentListeningDominance(state recentListeningState, lower string) string {
	if len(state.topArtists) == 0 {
		return "I don't have enough recent listening detail to judge dominance in that window yet."
	}
	top := state.topArtists[0]
	if artist := mentionedRecentArtist(lower, state.topArtists); artist != nil {
		if top.TrackCount == 0 {
			return fmt.Sprintf("%s doesn't look dominant in this recent window.", artist.ArtistName)
		}
		if strings.EqualFold(artist.ArtistName, top.ArtistName) {
			second := 0
			if len(state.topArtists) > 1 {
				second = state.topArtists[1].TrackCount
			}
			return fmt.Sprintf("Yes. %s is clearly dominant right now with %d plays in this window, versus %d for the next artist.", artist.ArtistName, artist.TrackCount, second)
		}
		return fmt.Sprintf("Not really. %s has %d plays in this window, while %s leads with %d.", artist.ArtistName, artist.TrackCount, top.ArtistName, top.TrackCount)
	}
	second := 0
	secondName := "the next artist"
	if len(state.topArtists) > 1 {
		second = state.topArtists[1].TrackCount
		secondName = state.topArtists[1].ArtistName
	}
	if top.TrackCount >= second*2 && top.TrackCount >= 20 {
		return fmt.Sprintf("%s is the clear outlier in this window: %d plays versus %d for %s.", top.ArtistName, top.TrackCount, second, secondName)
	}
	return fmt.Sprintf("There is a leader, but not an overwhelming one. %s is on top with %d plays, followed by %s with %d.", top.ArtistName, top.TrackCount, secondName, second)
}

func mentionedRecentArtist(lower string, artists []recentListeningArtistState) *recentListeningArtistState {
	for i := range artists {
		name := normalizeReferenceText(artists[i].ArtistName)
		if name != "" && strings.Contains(lower, name) {
			return &artists[i]
		}
	}
	return nil
}

func renderCreativeAlbumSet(label string, candidates []creativeAlbumCandidate, limit int) string {
	items := make([]string, 0, minInt(len(candidates), limit))
	for _, candidate := range candidates {
		if len(items) >= limit {
			break
		}
		items = append(items, formatCreativeAlbumCandidate(candidate, true))
	}
	return renderRouteBulletList(label, items, limit)
}

func formatCreativeAlbumCandidate(candidate creativeAlbumCandidate, includePlay bool) string {
	label := strings.TrimSpace(candidate.Name)
	if artist := strings.TrimSpace(candidate.ArtistName); artist != "" {
		label += " by " + artist
	}
	if candidate.Year > 0 {
		label += fmt.Sprintf(" (%d)", candidate.Year)
	}
	if !includePlay {
		return label
	}
	if candidate.PlayCount > 0 && strings.TrimSpace(candidate.LastPlayed) != "" {
		return fmt.Sprintf("%s [plays=%d, last played %s]", label, candidate.PlayCount, humanizeSummaryTimestamp(candidate.LastPlayed))
	}
	if strings.TrimSpace(candidate.LastPlayed) != "" {
		return fmt.Sprintf("%s [last played %s]", label, humanizeSummaryTimestamp(candidate.LastPlayed))
	}
	if candidate.PlayCount > 0 {
		return fmt.Sprintf("%s [plays=%d]", label, candidate.PlayCount)
	}
	return label
}

func normalizedCreativeAlbumCandidateKey(candidate creativeAlbumCandidate) string {
	if id := strings.TrimSpace(candidate.ID); id != "" {
		return id
	}
	return normalizeReferenceText(candidate.Name + " " + candidate.ArtistName)
}

func parseCreativeAlbumTime(raw string) (time.Time, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

func containsAny(text string, parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(text, part) {
			return true
		}
	}
	return false
}

func clampUnit(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
