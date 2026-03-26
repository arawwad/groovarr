package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"groovarr/graph"
	"groovarr/internal/agent"
	"groovarr/internal/discovery"
	"groovarr/internal/similarity"
)

func boolPtr(v bool) *bool { return &v }

func TestBuildDeterministicAlbumLibraryStatsArgs(t *testing.T) {
	args, label, ok := buildDeterministicAlbumLibraryStatsArgs("show albums in my library i havent played in years")
	if !ok {
		t.Fatal("expected album library stats query to be detected")
	}
	if label == "" {
		t.Fatal("expected non-empty label")
	}
	filter, ok := args["filter"].(map[string]interface{})
	if !ok {
		t.Fatal("expected filter map")
	}
	if got := filter["notPlayedSince"]; got != "years" {
		t.Fatalf("notPlayedSince = %v, want years", got)
	}
}

func TestExtractArtistAlbumCountNamesCombined(t *testing.T) {
	names, ok := extractArtistAlbumCountNames("How many albums do Radiohead and The Beatles have in my library combined?")
	if !ok {
		t.Fatal("expected combined artist album count query to be detected")
	}
	want := []string{"Radiohead", "The Beatles"}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Fatalf("names = %#v, want %#v", names, want)
	}
}

func TestExtractArtistAlbumCountNamesSingleSubjectAfterDo(t *testing.T) {
	names, ok := extractArtistAlbumCountNames("How many albums do Radiohead have in my library?")
	if !ok {
		t.Fatal("expected single-artist count query to be detected")
	}
	want := []string{"Radiohead"}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Fatalf("names = %#v, want %#v", names, want)
	}
}

func TestExtractArtistAlbumCountNamesSingleSubjectBeforeAlbums(t *testing.T) {
	names, ok := extractArtistAlbumCountNames("How many Pink Floyd albums are in my library?")
	if !ok {
		t.Fatal("expected artist-before-albums count query to be detected")
	}
	want := []string{"Pink Floyd"}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Fatalf("names = %#v, want %#v", names, want)
	}
}

func TestExtractLibraryLookupQuery(t *testing.T) {
	got := extractLibraryLookupQuery("Do I have Heart-Shaped Box by Nirvana in my library?")
	if got != "Heart-Shaped Box by Nirvana" {
		t.Fatalf("extractLibraryLookupQuery() = %q", got)
	}
}

func TestExtractInventoryLookupContinuationQuery(t *testing.T) {
	got := extractInventoryLookupContinuationQuery("What about The Bends by Radiohead?")
	if got != "The Bends by Radiohead" {
		t.Fatalf("extractInventoryLookupContinuationQuery() = %q", got)
	}
}

func TestIsArtistAlbumFollowUpPromptRecognizesRevisitWithoutAlbumWord(t *testing.T) {
	if !isArtistAlbumFollowUpPrompt("from those, give me two to revisit tonight") {
		t.Fatal("expected revisit follow-up to bind to prior artist candidates")
	}
	if isArtistAlbumFollowUpPrompt("which of those have i played recently") {
		t.Fatal("did not expect recent-listening follow-up to be treated as artist album revisit")
	}
}

func TestArtistAlbumFollowUpRequestedCount(t *testing.T) {
	if count, ok := artistAlbumFollowUpRequestedCount("from those, give me two to revisit tonight"); !ok || count != 2 {
		t.Fatalf("artistAlbumFollowUpRequestedCount() = %d, %v, want 2, true", count, ok)
	}
	if count, ok := artistAlbumFollowUpRequestedCount("pick 3 albums from those"); !ok || count != 3 {
		t.Fatalf("artistAlbumFollowUpRequestedCount() = %d, %v, want 3, true", count, ok)
	}
	if count, ok := artistAlbumFollowUpRequestedCount("give me the second one"); ok || count != 0 {
		t.Fatalf("artistAlbumFollowUpRequestedCount() = %d, %v, want 0, false", count, ok)
	}
}

func TestFindExactTrackLookupMatch(t *testing.T) {
	match, ok := findExactTrackLookupMatch("Heart-Shaped Box", "Nirvana", []trackCandidate{
		{Title: "Heart-Shaped Box", ArtistName: "Nirvana"},
		{Title: "Heart-Shaped Box", ArtistName: "Something Else"},
	})
	if !ok {
		t.Fatal("expected exact track match to resolve")
	}
	if match.ArtistName != "Nirvana" {
		t.Fatalf("match = %#v", match)
	}
}

func TestFindExactAlbumLookupMatch(t *testing.T) {
	match, ok := findExactAlbumLookupMatch("The Dark Side of the Moon", "Pink Floyd", []albumLookupCandidate{
		{Title: "The Dark Side of the Moon", ArtistName: "Pink Floyd", Year: 1973},
		{Title: "The Dark Side of the Moon", ArtistName: "Unknown"},
	})
	if !ok {
		t.Fatal("expected exact album match to resolve")
	}
	if match.ArtistName != "Pink Floyd" || match.Year != 1973 {
		t.Fatalf("match = %#v", match)
	}
}

func TestParseArtistAlbumCandidates(t *testing.T) {
	candidates, ok := parseArtistAlbumCandidates(`{"data":{"albums":[{"name":"Honeymoon","artistName":"Lana Del Rey","year":2015,"genre":"dream pop","playCount":2,"lastPlayed":"2026-03-01T12:00:00Z"}]}}`)
	if !ok {
		t.Fatal("expected parseArtistAlbumCandidates to succeed")
	}
	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1", len(candidates))
	}
	if candidates[0].Name != "Honeymoon" || candidates[0].ArtistName != "Lana Del Rey" {
		t.Fatalf("candidate = %#v", candidates[0])
	}
	if candidates[0].Year != 2015 || candidates[0].PlayCount != 2 || candidates[0].LastPlayed != "2026-03-01T12:00:00Z" {
		t.Fatalf("candidate metadata = %#v", candidates[0])
	}
}

func TestFindMatchingArtistLibraryStat(t *testing.T) {
	stats := []artistAlbumStat{
		{ArtistName: "Radiohead", AlbumCount: 16},
		{ArtistName: "The Beatles", AlbumCount: 11},
	}
	used := make(map[int]struct{})
	if got := findMatchingArtistLibraryStat("Beatles", stats, used); got != 1 {
		t.Fatalf("findMatchingArtistLibraryStat() = %d, want 1", got)
	}
}

func TestHandleStructuredBadlyRatedCleanupWhenLatestResultIsEmpty(t *testing.T) {
	lastBadlyRatedAlbums.mu.Lock()
	lastBadlyRatedAlbums.sessions = make(map[string]badlyRatedAlbumsState)
	lastBadlyRatedAlbums.mu.Unlock()
	setLastBadlyRatedAlbums("session-bad-empty", nil)

	srv := &Server{}
	ctx := context.WithValue(context.Background(), chatSessionKey, "session-bad-empty")
	resp, _, ok := srv.handleStructuredBadlyRatedCleanup(ctx, &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:        "other",
			SubIntent:     "badly_rated_cleanup",
			ResultSetKind: "badly_rated_albums",
			ResultAction:  "preview_apply",
			SelectionMode: "all",
		},
		HasBadlyRatedAlbums: true,
	})
	if !ok {
		t.Fatal("expected deterministic badly rated cleanup follow-up response")
	}
	if !strings.Contains(resp, "aren't any recently identified badly rated albums") {
		t.Fatalf("response = %q", resp)
	}
}

func TestRecentBadlyRatedAlbumsStateRejectsExpiredState(t *testing.T) {
	lastBadlyRatedAlbums.mu.Lock()
	lastBadlyRatedAlbums.sessions = map[string]badlyRatedAlbumsState{
		normalizeChatSessionID("expired-bad"): {
			candidates: nil,
			updatedAt:  time.Now().UTC().Add(-llmContextBadlyRatedAlbumsTTL - time.Minute),
		},
	}
	lastBadlyRatedAlbums.mu.Unlock()
	if _, _, ok := recentBadlyRatedAlbumsState("expired-bad", time.Now().UTC()); ok {
		t.Fatal("expected expired badly rated state to be ignored")
	}
}

func TestHandleStructuredBadlyRatedCleanupFallsBackToMemory(t *testing.T) {
	srv := &Server{
		chatMemory: map[string]chatSessionMemory{
			normalizeChatSessionID("session-bad-memory"): {
				UpdatedAt:          time.Now().UTC(),
				ActiveRequest:      "show badly rated albums in my library",
				RecentUserRequests: []string{"show badly rated albums in my library"},
			},
		},
	}
	ctx := context.WithValue(context.Background(), chatSessionKey, "session-bad-memory")
	resp, _, ok := srv.handleStructuredBadlyRatedCleanup(ctx, &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:        "other",
			SubIntent:     "badly_rated_cleanup",
			ResultSetKind: "badly_rated_albums",
			ResultAction:  "preview_apply",
			SelectionMode: "all",
		},
	})
	if !ok {
		t.Fatal("expected memory-backed deterministic badly rated cleanup follow-up response")
	}
	if !strings.Contains(resp, "aren't any recently identified badly rated albums") {
		t.Fatalf("response = %q", resp)
	}
}

func TestNeedsBroadAlbumDiscoveryClarification(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{msg: "best albums", want: true},
		{msg: "what should i listen to", want: true},
		{msg: "show my best albums", want: false},
		{msg: "best jazz albums", want: false},
		{msg: "best pink floyd albums", want: false},
		{msg: "albums to begin with", want: true},
	}

	for _, tc := range tests {
		got := needsBroadAlbumDiscoveryClarification(tc.msg)
		if got != tc.want {
			t.Fatalf("needsBroadAlbumDiscoveryClarification(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

func TestBuildSpecificAlbumDiscoveryArgs(t *testing.T) {
	args, ok := buildSpecificAlbumDiscoveryArgs("Best 5 Bjork albums")
	if !ok {
		t.Fatal("expected specific artist album discovery query to be detected")
	}
	if got := args["query"]; got != "Best 5 Bjork albums" {
		t.Fatalf("query = %v, want original query", got)
	}
	if got := args["limit"]; got != 5 {
		t.Fatalf("limit = %v, want 5", got)
	}
}

func TestSanitizeNormalizedTurnTrackDiscoveryDefaults(t *testing.T) {
	got := sanitizeNormalizedTurn("find me a song like windowlicker", normalizedTurn{
		Intent:     "track_discovery",
		SubIntent:  "track_similarity",
		TrackTitle: "Windowlicker",
	})
	if got.QueryScope != "library" {
		t.Fatalf("QueryScope = %q, want library", got.QueryScope)
	}
	if got.SubIntent != "track_similarity" {
		t.Fatalf("SubIntent = %q", got.SubIntent)
	}
	if got.TrackTitle != "Windowlicker" {
		t.Fatalf("TrackTitle = %q", got.TrackTitle)
	}
}

func TestResolveTurnContextTrackCandidates(t *testing.T) {
	setLastTrackCandidateSet("track-ref", "similar_tracks", "Windowlicker", []trackCandidate{
		{ID: "trk-1", Title: "Track One", ArtistName: "Artist One"},
	})
	resolved := resolveTurnContext("track-ref", normalizedTurn{
		Intent:          "track_discovery",
		SubIntent:       "track_description",
		FollowupMode:    "query_previous_set",
		ReferenceTarget: "previous_results",
	})
	if !resolved.HasTrackCandidates {
		t.Fatal("expected track candidates in context")
	}
	if resolved.Turn.ResultSetKind != "track_candidates" {
		t.Fatalf("ResultSetKind = %q, want track_candidates", resolved.Turn.ResultSetKind)
	}
	if resolved.MissingReferenceContext {
		t.Fatal("did not expect missing reference context")
	}
}

func TestSanitizeOrchestrationDecisionBiasesTrackRouting(t *testing.T) {
	decision := sanitizeOrchestrationDecision(orchestrationDecision{
		NextStage: "responder",
	}, &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:    "track_discovery",
			SubIntent: "track_search",
		},
	})
	if decision.NextStage != "deterministic" {
		t.Fatalf("NextStage = %q, want deterministic", decision.NextStage)
	}
	if decision.DeterministicMode != "normalized_first" {
		t.Fatalf("DeterministicMode = %q, want normalized_first", decision.DeterministicMode)
	}
}

func TestSanitizeOrchestrationDecisionRoutesCompareToResolver(t *testing.T) {
	decision := sanitizeOrchestrationDecision(orchestrationDecision{
		NextStage: "deterministic",
	}, &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:          "track_discovery",
			FollowupMode:    "refine_previous",
			ReferenceTarget: "previous_results",
			ResultAction:    "compare",
		},
	})
	if decision.NextStage != "resolver" {
		t.Fatalf("NextStage = %q, want resolver", decision.NextStage)
	}
	if decision.DeterministicMode != "none" {
		t.Fatalf("DeterministicMode = %q, want none", decision.DeterministicMode)
	}
}

func TestSanitizeOrchestrationDecisionBiasesResolvedFollowupRouting(t *testing.T) {
	decision := sanitizeOrchestrationDecision(orchestrationDecision{
		NextStage: "clarify",
	}, &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:          "listening",
			FollowupMode:    "query_previous_set",
			ReferenceTarget: "previous_results",
			ResultSetKind:   "artist_candidates",
		},
		ResolvedReferenceKind: "artist_candidates",
	})
	if decision.NextStage != "deterministic" {
		t.Fatalf("NextStage = %q, want deterministic", decision.NextStage)
	}
	if decision.DeterministicMode != "normalized_first" {
		t.Fatalf("DeterministicMode = %q, want normalized_first", decision.DeterministicMode)
	}
}

func TestSanitizeOrchestrationDecisionBiasesFirstTurnCreativeLibraryDiscovery(t *testing.T) {
	decision := sanitizeOrchestrationDecision(orchestrationDecision{
		NextStage: "resolver",
	}, &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:       "album_discovery",
			SubIntent:    "creative_refinement",
			FollowupMode: "none",
			QueryScope:   "library",
			LibraryOnly:  boolPtr(true),
			StyleHints:   []string{"melancholic", "dream pop"},
		},
	})
	if decision.NextStage != "deterministic" {
		t.Fatalf("NextStage = %q, want deterministic", decision.NextStage)
	}
	if decision.DeterministicMode != "normalized_first" {
		t.Fatalf("DeterministicMode = %q, want normalized_first", decision.DeterministicMode)
	}
}

func TestSanitizeOrchestrationDecisionBiasesFirstTurnGeneralAlbumDiscovery(t *testing.T) {
	decision := sanitizeOrchestrationDecision(orchestrationDecision{
		NextStage: "responder",
	}, &resolvedTurnContext{
		Turn: normalizedTurn{
			RawMessage:    "What should I put on for a rainy late-night walk?",
			Intent:        "album_discovery",
			SubIntent:     "creative_refinement",
			FollowupMode:  "none",
			QueryScope:    "general",
			PromptHint:    "rainy late-night walk",
			StyleHints:    []string{"rainy", "late-night"},
			ResultAction:  "none",
			SelectionMode: "none",
		},
	})
	if decision.NextStage != "deterministic" {
		t.Fatalf("NextStage = %q, want deterministic", decision.NextStage)
	}
	if decision.DeterministicMode != "normalized_first" {
		t.Fatalf("DeterministicMode = %q, want normalized_first", decision.DeterministicMode)
	}
}

func TestSanitizeOrchestrationDecisionBiasesArtistCandidateRevisitFollowUp(t *testing.T) {
	decision := sanitizeOrchestrationDecision(orchestrationDecision{
		NextStage: "resolver",
	}, &resolvedTurnContext{
		Turn: normalizedTurn{
			RawMessage:      "From those, give me two to revisit tonight.",
			Intent:          "artist_discovery",
			SubIntent:       "listening_summary",
			FollowupMode:    "refine_previous",
			ReferenceTarget: "previous_results",
			ResultSetKind:   "artist_candidates",
			SelectionMode:   "top_n",
			SelectionValue:  "2",
		},
		ResolvedReferenceKind: "artist_candidates",
	})
	if decision.NextStage != "deterministic" {
		t.Fatalf("NextStage = %q, want deterministic", decision.NextStage)
	}
	if decision.DeterministicMode != "normalized_first" {
		t.Fatalf("DeterministicMode = %q, want normalized_first", decision.DeterministicMode)
	}
}

func TestSanitizeOrchestrationDecisionBiasesMisnormalizedArtistCandidateSelection(t *testing.T) {
	decision := sanitizeOrchestrationDecision(orchestrationDecision{
		NextStage: "resolver",
	}, &resolvedTurnContext{
		Turn: normalizedTurn{
			RawMessage:      "From those, give me two to revisit tonight.",
			Intent:          "track_discovery",
			SubIntent:       "result_set_most_recent",
			FollowupMode:    "query_previous_set",
			ReferenceTarget: "previous_results",
			ResultSetKind:   "artist_candidates",
			SelectionMode:   "top_n",
			SelectionValue:  "2",
		},
		ResolvedReferenceKind: "artist_candidates",
	})
	if decision.NextStage != "deterministic" {
		t.Fatalf("NextStage = %q, want deterministic", decision.NextStage)
	}
	if decision.DeterministicMode != "normalized_first" {
		t.Fatalf("DeterministicMode = %q, want normalized_first", decision.DeterministicMode)
	}
}

func TestSelectTrackCandidatesByOrdinal(t *testing.T) {
	selected := selectTrackCandidatesByOrdinal([]trackCandidate{
		{Title: "One"},
		{Title: "Two"},
		{Title: "Three"},
	}, "2")
	if len(selected) != 1 || selected[0].Title != "Two" {
		t.Fatalf("selected = %#v", selected)
	}
}

func TestResolveTrackSeedPrefersSelectedFollowupCandidate(t *testing.T) {
	setLastTrackCandidateSet("track-seed", "similar_tracks", "Windowlicker", []trackCandidate{
		{ID: "trk-1", Title: "Sheep", ArtistName: "Pink Floyd"},
		{ID: "trk-2", Title: "Wrecked", ArtistName: "Imagine Dragons"},
	})
	seed, ok := resolveTrackSeed(context.WithValue(context.Background(), chatSessionKey, "track-seed"), &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:          "track_discovery",
			SubIntent:       "track_description",
			FollowupMode:    "refine_previous",
			ReferenceTarget: "previous_results",
			ResultSetKind:   "track_candidates",
			SelectionMode:   "ordinal",
			SelectionValue:  "2",
			TrackTitle:      "Windowlicker",
			ArtistName:      "Aphex Twin",
		},
		HasTrackCandidates: true,
	})
	if !ok {
		t.Fatal("expected track seed")
	}
	if seed.Title != "Wrecked" || seed.ArtistName != "Imagine Dragons" {
		t.Fatalf("seed = %#v", seed)
	}
}

func TestChooseRiskierTrackCandidate(t *testing.T) {
	pick := chooseRiskierTrackCandidate([]trackCandidate{
		{Title: "A", PlayCount: 4, Score: 0.9},
		{Title: "B", PlayCount: 0, Score: 0.85},
		{Title: "C", PlayCount: 1, Score: 0.7},
	})
	if pick.Title != "B" {
		t.Fatalf("pick = %#v", pick)
	}
}

func TestChooseSaferArtistCandidate(t *testing.T) {
	pick := chooseSaferArtistCandidate([]artistCandidate{
		{Name: "A", PlayCount: 1, Rating: 2},
		{Name: "B", PlayCount: 4, Rating: 1},
		{Name: "C", PlayCount: 3, Rating: 5},
	})
	if pick.Name != "B" {
		t.Fatalf("pick = %#v", pick)
	}
}

func TestResolveTrackSeedPrefersRiskierQualifierForCompositeFollowup(t *testing.T) {
	setLastTrackCandidateSet("track-composite", "similar_tracks", "Windowlicker", []trackCandidate{
		{ID: "trk-1", Title: "Sheep", ArtistName: "Pink Floyd", PlayCount: 4, Score: 0.9},
		{ID: "trk-2", Title: "Wrecked", ArtistName: "Imagine Dragons", PlayCount: 0, Score: 0.85},
		{ID: "trk-3", Title: "Thief", ArtistName: "Imagine Dragons", PlayCount: 1, Score: 0.7},
	})
	seed, ok := resolveTrackSeed(context.WithValue(context.Background(), chatSessionKey, "track-composite"), &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:             "track_discovery",
			SubIntent:          "track_description",
			FollowupMode:       "refine_previous",
			ReferenceTarget:    "previous_results",
			ReferenceQualifier: "riskier",
			ResultSetKind:      "track_candidates",
		},
		HasTrackCandidates: true,
	})
	if !ok {
		t.Fatal("expected track seed")
	}
	if seed.Title != "Wrecked" {
		t.Fatalf("seed = %#v", seed)
	}
}

func TestResolveTrackSeedUsesFocusedSongPathItemForCompositeFollowup(t *testing.T) {
	setLastSongPath("track-song-path-seed",
		songPathTrack{ID: "start", Title: "Windowlicker", ArtistName: "Aphex Twin"},
		songPathTrack{ID: "end", Title: "Teardrop", ArtistName: "Massive Attack"},
		[]songPathTrack{
			{ID: "start", Title: "Windowlicker", ArtistName: "Aphex Twin"},
			{ID: "mid", Title: "Angel", ArtistName: "Massive Attack"},
			{ID: "end", Title: "Teardrop", ArtistName: "Massive Attack"},
		},
		3,
		true,
	)
	setLastFocusedResultItem("track-song-path-seed", "song_path", normalizedSongPathTrackKey(songPathTrack{
		ID:         "mid",
		Title:      "Angel",
		ArtistName: "Massive Attack",
	}))
	seed, ok := resolveTrackSeed(context.WithValue(context.Background(), chatSessionKey, "track-song-path-seed"), &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:             "track_discovery",
			SubIntent:          "track_similarity",
			FollowupMode:       "refine_previous",
			ReferenceTarget:    "previous_results",
			ReferenceQualifier: "last_item",
			ResultSetKind:      "song_path",
		},
		ResolvedReferenceKind: "song_path",
		ResolvedItemKey:       normalizedSongPathTrackKey(songPathTrack{ID: "mid", Title: "Angel", ArtistName: "Massive Attack"}),
		HasSongPath:           true,
	})
	if !ok {
		t.Fatal("expected track seed from song path")
	}
	if seed.ID != "mid" || seed.Title != "Angel" {
		t.Fatalf("seed = %#v", seed)
	}
}

func TestResolveArtistSeedPrefersRiskierQualifierForCompositeFollowup(t *testing.T) {
	setLastArtistCandidateSet("artist-composite", "Radiohead", []artistCandidate{
		{Name: "Queen", PlayCount: 6, Rating: 4},
		{Name: "MØ", PlayCount: 0, Rating: 0},
		{Name: "Björk", PlayCount: 3, Rating: 5},
	})
	seed, ok := resolveArtistSeed(context.WithValue(context.Background(), chatSessionKey, "artist-composite"), &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:             "artist_discovery",
			SubIntent:          "artist_starting_album",
			FollowupMode:       "refine_previous",
			ReferenceTarget:    "previous_results",
			ReferenceQualifier: "riskier",
			ResultSetKind:      "artist_candidates",
		},
		HasArtistCandidates: true,
	})
	if !ok {
		t.Fatal("expected artist seed")
	}
	if seed.Name != "MØ" {
		t.Fatalf("seed = %#v", seed)
	}
}

func TestSanitizeNormalizedTurnInfersFollowupFromReferenceQualifier(t *testing.T) {
	turn := sanitizeNormalizedTurn("Take the less expected one and show me a strong starting record I already own.", normalizedTurn{
		Intent:             "artist_discovery",
		SubIntent:          "artist_starting_album",
		QueryScope:         "library",
		ReferenceQualifier: "riskier",
		ReferenceTarget:    "none",
		FollowupMode:       "none",
	})
	if turn.ReferenceTarget != "previous_results" {
		t.Fatalf("reference target = %q", turn.ReferenceTarget)
	}
	if turn.FollowupMode != "refine_previous" {
		t.Fatalf("followup mode = %q", turn.FollowupMode)
	}
}

func TestResolveTurnContextArtistStartingAlbumUsesPriorArtistCandidates(t *testing.T) {
	setLastArtistCandidateSet("artist-starting-auto", "Radiohead", []artistCandidate{
		{Name: "Khruangbin", PlayCount: 0, Rating: 1},
		{Name: "Björk", PlayCount: 2, Rating: 5},
	})
	resolved := resolveTurnContext("artist-starting-auto", normalizedTurn{
		Intent:              "artist_discovery",
		SubIntent:           "artist_starting_album",
		QueryScope:          "library",
		NeedsClarification:  true,
		ClarificationFocus:  "reference",
		ClarificationPrompt: "Which artist would you like to find a starting record for?",
	})
	if resolved.Turn.ReferenceTarget != "previous_results" {
		t.Fatalf("reference target = %q", resolved.Turn.ReferenceTarget)
	}
	if resolved.Turn.FollowupMode != "refine_previous" {
		t.Fatalf("followup mode = %q", resolved.Turn.FollowupMode)
	}
	if resolved.Turn.ResultSetKind != "artist_candidates" {
		t.Fatalf("result set kind = %q", resolved.Turn.ResultSetKind)
	}
	if resolved.ResolvedReferenceKind != "artist_candidates" {
		t.Fatalf("resolved reference kind = %q", resolved.ResolvedReferenceKind)
	}
	if resolved.Turn.NeedsClarification {
		t.Fatal("expected clarification to be cleared once prior artist candidates were resolved")
	}
}

func TestResolveTurnContextListeningFollowUpUsesPriorArtistCandidates(t *testing.T) {
	setLastArtistCandidateSet("artist-followup-auto", "top artists last month", []artistCandidate{
		{Name: "Radiohead", PlayCount: 12},
		{Name: "Pink Floyd", PlayCount: 8},
	})
	resolved := resolveTurnContext("artist-followup-auto", normalizedTurn{
		Intent:              "listening",
		SubIntent:           "result_set_play_recency",
		FollowupMode:        "query_previous_set",
		ReferenceTarget:     "previous_results",
		NeedsClarification:  true,
		ClarificationFocus:  "reference",
		ClarificationPrompt: "Which earlier results do you mean?",
		TimeWindow:          "last_month",
	})
	if resolved.Turn.ResultSetKind != "artist_candidates" {
		t.Fatalf("result set kind = %q, want artist_candidates", resolved.Turn.ResultSetKind)
	}
	if resolved.ResolvedReferenceKind != "artist_candidates" {
		t.Fatalf("resolved reference kind = %q, want artist_candidates", resolved.ResolvedReferenceKind)
	}
	if resolved.Turn.NeedsClarification {
		t.Fatal("expected clarification to be cleared once prior artist candidates were resolved")
	}
}

func TestResolveTurnContextSongPathSummaryUsesPriorPath(t *testing.T) {
	setLastSongPath("song-path-summary", songPathTrack{
		ID:         "start-1",
		Title:      "Heart-Shaped Box",
		ArtistName: "Nirvana",
	}, songPathTrack{
		ID:         "end-1",
		Title:      "Teardrop",
		ArtistName: "Massive Attack",
	}, []songPathTrack{
		{ID: "start-1", Title: "Heart-Shaped Box", ArtistName: "Nirvana", Position: 1},
		{ID: "mid-1", Title: "Pagan Poetry", ArtistName: "Björk", Position: 2},
		{ID: "end-1", Title: "Teardrop", ArtistName: "Massive Attack", Position: 3},
	}, 18, false)
	resolved := resolveTurnContext("song-path-summary", normalizedTurn{
		Intent:              "general_chat",
		SubIntent:           "song_path_summary",
		NeedsClarification:  true,
		ClarificationFocus:  "reference",
		ClarificationPrompt: "Could you provide more context about the path you're referring to?",
	})
	if resolved.Turn.Intent != "track_discovery" {
		t.Fatalf("intent = %q", resolved.Turn.Intent)
	}
	if resolved.Turn.ReferenceTarget != "previous_results" {
		t.Fatalf("reference target = %q", resolved.Turn.ReferenceTarget)
	}
	if resolved.Turn.ResultSetKind != "song_path" {
		t.Fatalf("result set kind = %q", resolved.Turn.ResultSetKind)
	}
	if resolved.ResolvedReferenceKind != "song_path" {
		t.Fatalf("resolved reference kind = %q", resolved.ResolvedReferenceKind)
	}
	if resolved.Turn.NeedsClarification {
		t.Fatal("expected clarification to be cleared once prior song path was resolved")
	}
}

func TestRenderStructuredSceneOverview(t *testing.T) {
	raw := `{"data":{"clusterScenes":{"message":"Loaded 2 sonic scene(s).","scenes":[{"name":"Indie / Rock","subtitle":"Relaxed, Sad","songCount":31,"sampleTracks":[{"title":"Soldier's Poem","artistName":"Muse"},{"title":"Bullet Proof... I Wish I Was","artistName":"Radiohead"}]},{"name":"Electronic / Rock","subtitle":"Danceable, Party","songCount":27,"sampleTracks":[{"title":"Mount Hopeless","artistName":"Melody’s Echo Chamber"}]}]}}}`
	resp, ok := renderStructuredSceneOverview(raw)
	if !ok {
		t.Fatal("expected rendered scene overview")
	}
	if !strings.Contains(resp, "I split your library into 2 sound neighborhoods") {
		t.Fatalf("response = %q", resp)
	}
	if !strings.Contains(resp, "Indie / Rock [Relaxed, Sad] (31 tracks)") {
		t.Fatalf("response = %q", resp)
	}
}

func TestSanitizeNormalizedTurnKeepsSceneDiscoveryIntent(t *testing.T) {
	turn := sanitizeNormalizedTurn("Split what I own into a few sound neighborhoods.", normalizedTurn{
		Intent:      "scene_discovery",
		SubIntent:   "scene_overview",
		QueryScope:  "unknown",
		LibraryOnly: boolPtr(true),
	})
	if turn.Intent != "scene_discovery" {
		t.Fatalf("intent = %q", turn.Intent)
	}
	if turn.QueryScope != "library" {
		t.Fatalf("query scope = %q", turn.QueryScope)
	}
}

func TestSanitizeNormalizedTurnKeepsCompareSelection(t *testing.T) {
	turn := sanitizeNormalizedTurn("Compare the safer one to the first.", normalizedTurn{
		Intent:                "track_discovery",
		ResultAction:          "compare",
		ReferenceQualifier:    "safer",
		CompareSelectionMode:  "ordinal",
		CompareSelectionValue: "1",
	})
	if turn.ResultAction != "compare" {
		t.Fatalf("result action = %q", turn.ResultAction)
	}
	if turn.CompareSelectionMode != "ordinal" {
		t.Fatalf("compare selection mode = %q", turn.CompareSelectionMode)
	}
	if turn.CompareSelectionValue != "1" {
		t.Fatalf("compare selection value = %q", turn.CompareSelectionValue)
	}
}

func TestSanitizeNormalizedTurnPromotesCompareSubIntent(t *testing.T) {
	turn := sanitizeNormalizedTurn("Compare the safer one to the first.", normalizedTurn{
		Intent:                "other",
		SubIntent:             "compare",
		FollowupMode:          "refine_previous",
		ReferenceTarget:       "previous_results",
		ReferenceQualifier:    "safer",
		CompareSelectionMode:  "ordinal",
		CompareSelectionValue: "1",
	})
	if turn.ResultAction != "compare" {
		t.Fatalf("result action = %q", turn.ResultAction)
	}
	if turn.SubIntent != "" {
		t.Fatalf("subintent = %q", turn.SubIntent)
	}
}

func TestResolveTrackComparisonPair(t *testing.T) {
	primary, secondary, sameItem, ok := resolveTrackComparisonPair(resolvedResultReference{}, normalizedTurn{
		ReferenceQualifier:    "safer",
		CompareSelectionMode:  "ordinal",
		CompareSelectionValue: "1",
	}, []trackCandidate{
		{ID: "trk-1", Title: "Sheep", ArtistName: "Pink Floyd", PlayCount: 1, Score: 0.91},
		{ID: "trk-2", Title: "Wrecked", ArtistName: "Imagine Dragons", PlayCount: 7, Score: 0.84},
		{ID: "trk-3", Title: "Thief", ArtistName: "Imagine Dragons", PlayCount: 0, Score: 0.72},
	})
	if !ok {
		t.Fatal("expected comparison pair")
	}
	if sameItem {
		t.Fatal("did not expect same-item comparison")
	}
	if primary.Title != "Wrecked" {
		t.Fatalf("primary = %#v", primary)
	}
	if secondary.Title != "Sheep" {
		t.Fatalf("secondary = %#v", secondary)
	}
}

func TestResolveArtistComparisonPair(t *testing.T) {
	primary, secondary, sameItem, ok := resolveArtistComparisonPair(resolvedResultReference{}, normalizedTurn{
		ReferenceQualifier:    "riskier",
		CompareSelectionMode:  "ordinal",
		CompareSelectionValue: "1",
	}, []artistCandidate{
		{Name: "Queen", PlayCount: 6, Rating: 4},
		{Name: "MØ", PlayCount: 0, Rating: 0},
		{Name: "Björk", PlayCount: 3, Rating: 5},
	})
	if !ok {
		t.Fatal("expected comparison pair")
	}
	if sameItem {
		t.Fatal("did not expect same-item comparison")
	}
	if primary.Name != "MØ" {
		t.Fatalf("primary = %#v", primary)
	}
	if secondary.Name != "Queen" {
		t.Fatalf("secondary = %#v", secondary)
	}
}

func TestResolveTrackComparisonPairPrefersExplicitPrimarySelection(t *testing.T) {
	primary, secondary, sameItem, ok := resolveTrackComparisonPair(resolvedResultReference{
		resultReference: resultReference{
			Selection: resultSelection{Mode: "ordinal", Value: "2"},
		},
	}, normalizedTurn{
		CompareSelectionMode:  "ordinal",
		CompareSelectionValue: "1",
	}, []trackCandidate{
		{ID: "trk-1", Title: "Sheep", ArtistName: "Pink Floyd", PlayCount: 1, Score: 0.91},
		{ID: "trk-2", Title: "Wrecked", ArtistName: "Imagine Dragons", PlayCount: 7, Score: 0.84},
		{ID: "trk-3", Title: "Thief", ArtistName: "Imagine Dragons", PlayCount: 0, Score: 0.72},
	})
	if !ok {
		t.Fatal("expected comparison pair")
	}
	if sameItem {
		t.Fatal("did not expect same-item comparison")
	}
	if primary.Title != "Wrecked" {
		t.Fatalf("primary = %#v", primary)
	}
	if secondary.Title != "Sheep" {
		t.Fatalf("secondary = %#v", secondary)
	}
}

func TestResolveTrackComparisonPairPrefersFocusedPrimaryItem(t *testing.T) {
	primary, secondary, sameItem, ok := resolveTrackComparisonPair(resolvedResultReference{
		ResolvedItemKey: normalizedTrackCandidateKey(trackCandidate{
			ID:         "trk-3",
			Title:      "Thief",
			ArtistName: "Imagine Dragons",
		}),
	}, normalizedTurn{
		CompareSelectionMode:  "ordinal",
		CompareSelectionValue: "1",
	}, []trackCandidate{
		{ID: "trk-1", Title: "Sheep", ArtistName: "Pink Floyd", PlayCount: 1, Score: 0.91},
		{ID: "trk-2", Title: "Wrecked", ArtistName: "Imagine Dragons", PlayCount: 7, Score: 0.84},
		{ID: "trk-3", Title: "Thief", ArtistName: "Imagine Dragons", PlayCount: 0, Score: 0.72},
	})
	if !ok {
		t.Fatal("expected comparison pair")
	}
	if sameItem {
		t.Fatal("did not expect same-item comparison")
	}
	if primary.Title != "Thief" {
		t.Fatalf("primary = %#v", primary)
	}
	if secondary.Title != "Sheep" {
		t.Fatalf("secondary = %#v", secondary)
	}
}

func TestResolveArtistComparisonPairPrefersExplicitPrimarySelection(t *testing.T) {
	primary, secondary, sameItem, ok := resolveArtistComparisonPair(resolvedResultReference{
		resultReference: resultReference{
			Selection: resultSelection{Mode: "ordinal", Value: "3"},
		},
	}, normalizedTurn{
		CompareSelectionMode:  "ordinal",
		CompareSelectionValue: "1",
	}, []artistCandidate{
		{Name: "Queen", PlayCount: 6, Rating: 4},
		{Name: "MØ", PlayCount: 0, Rating: 0},
		{Name: "Björk", PlayCount: 3, Rating: 5},
	})
	if !ok {
		t.Fatal("expected comparison pair")
	}
	if sameItem {
		t.Fatal("did not expect same-item comparison")
	}
	if primary.Name != "Björk" {
		t.Fatalf("primary = %#v", primary)
	}
	if secondary.Name != "Queen" {
		t.Fatalf("secondary = %#v", secondary)
	}
}

func TestResolveArtistComparisonPairPrefersFocusedPrimaryItem(t *testing.T) {
	primary, secondary, sameItem, ok := resolveArtistComparisonPair(resolvedResultReference{
		ResolvedItemKey: normalizedArtistCandidateKey(artistCandidate{Name: "Björk"}),
	}, normalizedTurn{
		CompareSelectionMode:  "ordinal",
		CompareSelectionValue: "1",
	}, []artistCandidate{
		{Name: "Queen", PlayCount: 6, Rating: 4},
		{Name: "MØ", PlayCount: 0, Rating: 0},
		{Name: "Björk", PlayCount: 3, Rating: 5},
	})
	if !ok {
		t.Fatal("expected comparison pair")
	}
	if sameItem {
		t.Fatal("did not expect same-item comparison")
	}
	if primary.Name != "Björk" {
		t.Fatalf("primary = %#v", primary)
	}
	if secondary.Name != "Queen" {
		t.Fatalf("secondary = %#v", secondary)
	}
}

func TestResolveTurnContextSceneCandidatesUseSceneDiscoveryIntent(t *testing.T) {
	setLastSceneSelection("scene-intent", nil, []sceneSessionItem{{Name: "Indie / Rock", SongCount: 31}})
	resolved := resolveTurnContext("scene-intent", normalizedTurn{
		Intent:          "scene_discovery",
		FollowupMode:    "refine_previous",
		ReferenceTarget: "previous_results",
		ResultAction:    "select_candidate",
		SelectionMode:   "count_match",
		SelectionValue:  "31",
	})
	if resolved.Turn.ResultSetKind != "scene_candidates" {
		t.Fatalf("result set kind = %q", resolved.Turn.ResultSetKind)
	}
	if resolved.ResolvedReferenceKind != "scene_candidates" {
		t.Fatalf("resolved reference kind = %q", resolved.ResolvedReferenceKind)
	}
}

func TestArtistDiscoveryScopeClarificationTarget(t *testing.T) {
	artist, ok := artistDiscoveryScopeClarificationTarget("Best 5 Bjork albums")
	if !ok {
		t.Fatal("expected artist-scope clarification to be detected")
	}
	if artist != "Bjork" {
		t.Fatalf("artist = %q, want Bjork", artist)
	}
}

func TestArtistDiscoveryScopeClarificationTargetRejectsOwnedQuery(t *testing.T) {
	artist, ok := artistDiscoveryScopeClarificationTarget("Best 5 Bjork albums in my library")
	if ok || artist != "" {
		t.Fatalf("expected owned query to skip clarification, got (%q, %v)", artist, ok)
	}
}

func TestBuildSpecificAlbumDiscoveryArgsRejectsLibraryOwnedQueries(t *testing.T) {
	if args, ok := buildSpecificAlbumDiscoveryArgs("show nocturnal albums in my library"); ok || args != nil {
		t.Fatalf("expected library-owned semantic query to be rejected, got (%v, %v)", args, ok)
	}
}

func TestUnsupportedAlbumRelationshipQueryResponse(t *testing.T) {
	resp, ok := unsupportedAlbumRelationshipQueryResponse("which albums in my library are by artists with at least 3 albums")
	if !ok {
		t.Fatal("expected unsupported relationship query to be detected")
	}
	if resp == "" {
		t.Fatal("expected non-empty response")
	}
}

func TestBuildDeterministicAlbumRelationshipArgs(t *testing.T) {
	args, label, ok := buildDeterministicAlbumRelationshipArgs("which albums in my library are by artists with only one album")
	if !ok {
		t.Fatal("expected album relationship query to be detected")
	}
	if label == "" {
		t.Fatal("expected non-empty label")
	}
	filter, ok := args["filter"].(map[string]interface{})
	if !ok {
		t.Fatal("expected filter map")
	}
	if got := filter["artistExactAlbums"]; got != 1 {
		t.Fatalf("artistExactAlbums = %v, want 1", got)
	}
}

func TestBuildDeterministicAlbumRelationshipArgsWithNotPlayedYears(t *testing.T) {
	args, label, ok := buildDeterministicAlbumRelationshipArgs("which albums in my library are by artists with only one album that i havent played in years")
	if !ok {
		t.Fatal("expected compound album relationship query to be detected")
	}
	if label == "" || !containsIgnoreCase(label, "not played in years") {
		t.Fatalf("expected label to mention years inactivity, got %q", label)
	}
	filter, ok := args["filter"].(map[string]interface{})
	if !ok {
		t.Fatal("expected filter map")
	}
	if got := filter["artistExactAlbums"]; got != 1 {
		t.Fatalf("artistExactAlbums = %v, want 1", got)
	}
	if got := filter["notPlayedSince"]; got != "years" {
		t.Fatalf("notPlayedSince = %v, want years", got)
	}
}

func TestBuildDeterministicAlbumRelationshipArgsWithContraction(t *testing.T) {
	args, _, ok := buildDeterministicAlbumRelationshipArgs("which albums in my library are by artists with only one album that i haven't played in years")
	if !ok {
		t.Fatal("expected compound album relationship query with contraction to be detected")
	}
	filter, ok := args["filter"].(map[string]interface{})
	if !ok {
		t.Fatal("expected filter map")
	}
	if got := filter["notPlayedSince"]; got != "years" {
		t.Fatalf("notPlayedSince = %v, want years", got)
	}
}

func TestBuildDeterministicLibraryFacetArgs(t *testing.T) {
	args, label, ok := buildDeterministicLibraryFacetArgs("what genres dominate my library")
	if !ok {
		t.Fatal("expected genre facet query to be detected")
	}
	if label == "" {
		t.Fatal("expected non-empty label")
	}
	if got := args["field"]; got != "genre" {
		t.Fatalf("field = %v, want genre", got)
	}
}

func TestIsSingleAlbumArtistQuery(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{msg: "which artists in my library have only one album", want: true},
		{msg: "artists with exactly one album", want: true},
		{msg: "show artists with a single album", want: true},
		{msg: "which albums in my library are by artists with only one album", want: false},
		{msg: "which albums do i have", want: false},
		{msg: "which artists do i have", want: false},
	}

	for _, tc := range tests {
		got := isSingleAlbumArtistQuery(tc.msg)
		if got != tc.want {
			t.Fatalf("isSingleAlbumArtistQuery(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

func TestExtractMinAlbumsQueryValue(t *testing.T) {
	tests := []struct {
		msg  string
		want int
		ok   bool
	}{
		{msg: "which artists have at least 3 albums", want: 3, ok: true},
		{msg: "artists with at least three albums", want: 3, ok: true},
		{msg: "artists with one album", want: 0, ok: false},
	}

	for _, tc := range tests {
		got, ok := extractMinAlbumsQueryValue(tc.msg)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("extractMinAlbumsQueryValue(%q) = (%d, %v), want (%d, %v)", tc.msg, got, ok, tc.want, tc.ok)
		}
	}
}

func TestIsArtistListeningStatsQuery(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{msg: "which artists did i listen to most this month", want: true},
		{msg: "which artists in my library have no plays this year", want: true},
		{msg: "what did i listen to this month", want: false},
		{msg: "which albums did i play most this month", want: false},
	}

	for _, tc := range tests {
		got := isArtistListeningStatsQuery(tc.msg)
		if got != tc.want {
			t.Fatalf("isArtistListeningStatsQuery(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

func TestBuildDeterministicArtistListeningStatsArgsWithMinAlbums(t *testing.T) {
	args, label, ok := buildDeterministicArtistListeningStatsArgs("which artists did i listen to most this month and have at least 3 albums")
	if !ok {
		t.Fatal("expected listening stats query to be detected")
	}
	if !containsIgnoreCase(label, "at least 3 albums") {
		t.Fatalf("expected label to mention album constraint, got %q", label)
	}
	filter, ok := args["filter"].(map[string]interface{})
	if !ok {
		t.Fatal("expected filter map")
	}
	if got := filter["minAlbums"]; got != 3 {
		t.Fatalf("minAlbums = %v, want 3", got)
	}
	if got := filter["minPlaysInWindow"]; got != 1 {
		t.Fatalf("minPlaysInWindow = %v, want 1", got)
	}
}

func TestBuildDeterministicArtistListeningStatsArgsNoPlaysThisYearWithMinAlbums(t *testing.T) {
	args, label, ok := buildDeterministicArtistListeningStatsArgs("which artists in my library have no plays this year and at least 3 albums")
	if !ok {
		t.Fatal("expected no-plays listening stats query to be detected")
	}
	if !containsIgnoreCase(label, "no plays") || !containsIgnoreCase(label, "at least 3 albums") {
		t.Fatalf("label = %q, want no-plays and min-albums cues", label)
	}
	filter, ok := args["filter"].(map[string]interface{})
	if !ok {
		t.Fatal("expected filter map")
	}
	if got := filter["minAlbums"]; got != 3 {
		t.Fatalf("minAlbums = %v, want 3", got)
	}
	if got := filter["maxPlaysInWindow"]; got != 0 {
		t.Fatalf("maxPlaysInWindow = %v, want 0", got)
	}
	if _, ok := filter["playedSince"]; !ok {
		t.Fatal("expected playedSince filter")
	}
	if _, ok := filter["playedUntil"]; !ok {
		t.Fatal("expected playedUntil filter")
	}
}

func TestBuildDeterministicArtistLibraryStatsArgsWithNoPlaysThisYear(t *testing.T) {
	args, label, ok := buildDeterministicArtistLibraryStatsArgs("which artists in my library have at least 3 albums and no plays this year")
	if !ok {
		t.Fatal("expected artist library stats query to be detected")
	}
	if !containsIgnoreCase(label, "at least 3 albums") || !containsIgnoreCase(label, "no plays since") {
		t.Fatalf("label = %q, want min-albums and no-plays cues", label)
	}
	filter, ok := args["filter"].(map[string]interface{})
	if !ok {
		t.Fatal("expected filter map")
	}
	if got := filter["minAlbums"]; got != 3 {
		t.Fatalf("minAlbums = %v, want 3", got)
	}
	if got := filter["maxPlaysInWindow"]; got != 0 {
		t.Fatalf("maxPlaysInWindow = %v, want 0", got)
	}
	if _, ok := filter["playedSince"]; !ok {
		t.Fatal("expected playedSince filter")
	}
	if _, ok := filter["playedUntil"]; !ok {
		t.Fatal("expected playedUntil filter")
	}
}

func TestBuildDeterministicAlbumLibraryStatsArgsWithContraction(t *testing.T) {
	args, label, ok := buildDeterministicAlbumLibraryStatsArgs("show albums in my library i haven't played in years")
	if !ok {
		t.Fatal("expected album library stats query with contraction to be detected")
	}
	if label == "" {
		t.Fatal("expected non-empty label")
	}
	filter, ok := args["filter"].(map[string]interface{})
	if !ok {
		t.Fatal("expected filter map")
	}
	if got := filter["notPlayedSince"]; got != "years" {
		t.Fatalf("notPlayedSince = %v, want years", got)
	}
}

func TestResolveListeningPeriodTodayStartsAtLocalMidnight(t *testing.T) {
	start, end, label, ok := resolveListeningPeriod("what did i listen to today")
	if !ok {
		t.Fatal("expected today listening period to resolve")
	}
	if label != "today" {
		t.Fatalf("label = %q, want today", label)
	}
	if !(start.Hour() == 0 && start.Minute() == 0 && start.Second() == 0) {
		t.Fatalf("start = %v, want local midnight", start)
	}
	if !end.After(start) {
		t.Fatalf("end = %v, want after start %v", end, start)
	}
}

func TestResolveListeningPeriodLatelyDefaultsToLastMonth(t *testing.T) {
	start, end, label, ok := resolveListeningPeriod("what do i keep replaying lately")
	if !ok {
		t.Fatal("expected lately listening period to resolve")
	}
	if label != "in the last month" {
		t.Fatalf("label = %q, want in the last month", label)
	}
	if !end.After(start) {
		t.Fatalf("end = %v, want after start %v", end, start)
	}
	if start.After(end.AddDate(0, -1, 1)) {
		t.Fatalf("start = %v, want roughly one month before %v", start, end)
	}
}

func TestIsCreativeThreeAlbumPrompt(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{msg: "give me one album for focus, one for walking, and one for late-night headphones from my library", want: true},
		{msg: "one album for focus and one for walking from my library", want: false},
		{msg: "late night headphones album from my library", want: false},
	}
	for _, tc := range tests {
		got := isCreativeThreeAlbumPrompt(tc.msg)
		if got != tc.want {
			t.Fatalf("isCreativeThreeAlbumPrompt(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

func TestIsSemanticLibraryPrompt(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{msg: "show nocturnal albums in my library", want: true},
		{msg: "show nocturnal albums from my collection", want: true},
		{msg: "show nocturnal albums from what i already have", want: true},
		{msg: "what in my library feels rainy and spacious", want: true},
		{msg: "give me one album for focus, one for walking, and one for late-night headphones from my library", want: true},
		{msg: "how many albums are in my library", want: false},
		{msg: "what did i listen to this month", want: false},
	}
	for _, tc := range tests {
		got := isSemanticLibraryPrompt(tc.msg)
		if got != tc.want {
			t.Fatalf("isSemanticLibraryPrompt(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

func TestContainsLibraryOwnershipCue(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{msg: "find my albums for tonight", want: true},
		{msg: "show my records for a rainy walk", want: true},
		{msg: "pull from my collection", want: true},
		{msg: "use what i already have", want: true},
		{msg: "find albums like Air", want: false},
	}
	for _, tc := range tests {
		got := containsLibraryOwnershipCue(strings.ToLower(tc.msg))
		if got != tc.want {
			t.Fatalf("containsLibraryOwnershipCue(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

func TestTryEmbeddingsUnavailableSemanticLibraryQuery(t *testing.T) {
	srv := &Server{}

	resp, ok := srv.tryEmbeddingsUnavailableSemanticLibraryQuery("show nocturnal albums in my library")
	if !ok {
		t.Fatal("expected semantic library prompt to be intercepted when embeddings are unavailable")
	}
	if !strings.Contains(resp, "EMBEDDINGS_ENDPOINT is not configured") {
		t.Fatalf("response = %q, want explicit embeddings warning", resp)
	}

	if resp, ok := srv.tryEmbeddingsUnavailableSemanticLibraryQuery("how many albums are in my library"); ok || resp != "" {
		t.Fatalf("non-semantic library query should not be intercepted, got (%q, %v)", resp, ok)
	}

	srv.embeddingsURL = "http://embeddings:80"
	if resp, ok := srv.tryEmbeddingsUnavailableSemanticLibraryQuery("show nocturnal albums in my library"); ok || resp != "" {
		t.Fatalf("configured embeddings should not trigger unavailable response, got (%q, %v)", resp, ok)
	}
}

func TestBuildDeterministicAlbumRelationshipArgsFindAlbums(t *testing.T) {
	args, _, ok := buildDeterministicAlbumRelationshipArgs("find albums in my library by artists with only one album")
	if !ok {
		t.Fatal("expected find albums relationship query to be detected")
	}
	filter, ok := args["filter"].(map[string]interface{})
	if !ok {
		t.Fatal("expected filter map")
	}
	if got := filter["artistExactAlbums"]; got != 1 {
		t.Fatalf("artistExactAlbums = %v, want 1", got)
	}
}

func containsIgnoreCase(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

func TestExtractDiscoveredAlbumSelection(t *testing.T) {
	tests := []struct {
		msg  string
		want string
	}{
		{msg: "can we add those to library?", want: "all"},
		{msg: "add the first two to library", want: "first 2"},
		{msg: "monitor the first 3 in lidarr", want: "first 3"},
		{msg: "add the last one to library", want: "last 1"},
		{msg: "monitor albums 2 and 4 in lidarr", want: "2, 4"},
		{msg: "add #3 to library", want: "3"},
	}

	for _, tc := range tests {
		got := extractDiscoveredAlbumSelection(tc.msg)
		if got != tc.want {
			t.Fatalf("extractDiscoveredAlbumSelection(%q) = %q, want %q", tc.msg, got, tc.want)
		}
	}
}

func TestWantsDiscoveredAlbumApproval(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{msg: "can we add those to library?", want: true},
		{msg: "could you add these to my library", want: true},
		{msg: "which of those are already in my library?", want: false},
		{msg: "are those available in my library?", want: false},
		{msg: "best 3 pink floyd albums", want: false},
	}

	for _, tc := range tests {
		got := wantsDiscoveredAlbumApproval(tc.msg)
		if got != tc.want {
			t.Fatalf("wantsDiscoveredAlbumApproval(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

func TestExtractPlaylistNameFromTextFindsQuotedPlaylist(t *testing.T) {
	got, ok := extractPlaylistNameFromText("Playlist \"This Is: Air\" currently has:\n- La Femme d’argent by Air")
	if !ok || got != "This Is: Air" {
		t.Fatalf("extractPlaylistNameFromText() = %q, %v", got, ok)
	}
}

func TestCanonicalThisIsPlaylistName(t *testing.T) {
	tests := []struct {
		raw  string
		want string
		ok   bool
	}{
		{raw: "this is air", want: "This Is: air", ok: true},
		{raw: "This Is: Air", want: "This Is: Air", ok: true},
		{raw: "\"This Is Air\"", want: "This Is: Air", ok: true},
		{raw: "Melancholy Jazz", want: "", ok: false},
	}

	for _, tc := range tests {
		got, ok := canonicalThisIsPlaylistName(tc.raw)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("canonicalThisIsPlaylistName(%q) = (%q, %v), want (%q, %v)", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}

func TestArtistFromThisIsPlaylistName(t *testing.T) {
	got, ok := artistFromThisIsPlaylistName("This Is: Air")
	if !ok || got != "Air" {
		t.Fatalf("artistFromThisIsPlaylistName() = %q, %v", got, ok)
	}
}

func TestExtractPlaylistCreateIntent(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "Make me a melancholy jazz playlist for late nights.", want: "melancholy jazz playlist for late nights"},
		{raw: "Build a rainy-day playlist", want: "rainy-day"},
		{raw: "Make me a playlist", want: ""},
	}

	for _, tc := range tests {
		if got := extractPlaylistCreateIntent(tc.raw); got != tc.want {
			t.Fatalf("extractPlaylistCreateIntent(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestSanitizeNormalizedTurnDefaultsAmbiguousRecentListening(t *testing.T) {
	turn := sanitizeNormalizedTurn("What has really been carrying my listening lately?", normalizedTurn{
		Intent:              "listening",
		QueryScope:          "listening",
		TimeWindow:          "ambiguous_recent",
		NeedsClarification:  true,
		ClarificationFocus:  "time_window",
		ClarificationPrompt: "How far back would you like to look?",
	})
	if turn.NeedsClarification {
		t.Fatalf("NeedsClarification = true, want false")
	}
	if turn.ClarificationFocus != "none" {
		t.Fatalf("ClarificationFocus = %q, want none", turn.ClarificationFocus)
	}
	if turn.ClarificationPrompt != "" {
		t.Fatalf("ClarificationPrompt = %q, want empty", turn.ClarificationPrompt)
	}
	if turn.SubIntent != "listening_summary" {
		t.Fatalf("SubIntent = %q, want listening_summary", turn.SubIntent)
	}
}

func TestSanitizeNormalizedTurnClarifiesAmbiguousArtistStatsScope(t *testing.T) {
	turn := sanitizeNormalizedTurn("Give me artist stats.", normalizedTurn{
		Intent:     "stats",
		SubIntent:  "library_top_artists",
		QueryScope: "general",
	})
	if !turn.NeedsClarification {
		t.Fatal("expected clarification to be required")
	}
	if turn.ClarificationFocus != "scope" {
		t.Fatalf("ClarificationFocus = %q, want scope", turn.ClarificationFocus)
	}
	if turn.ClarificationPrompt != "Do you want library stats or listening stats?" {
		t.Fatalf("ClarificationPrompt = %q", turn.ClarificationPrompt)
	}
}

func TestSanitizeNormalizedTurnDefaultsResultSetPlayRecency(t *testing.T) {
	turn := sanitizeNormalizedTurn("From those, which ones have I actually touched this year?", normalizedTurn{
		Intent:          "listening",
		FollowupMode:    "query_previous_set",
		ReferenceTarget: "previous_results",
		TimeWindow:      "this_year",
	})
	if turn.SubIntent != "result_set_play_recency" {
		t.Fatalf("SubIntent = %q, want result_set_play_recency", turn.SubIntent)
	}
}

func TestSanitizeNormalizedTurnNormalizesStyleHints(t *testing.T) {
	turn := sanitizeNormalizedTurn("Make that less polished and more frayed.", normalizedTurn{
		Intent:     "album_discovery",
		SubIntent:  "creative_refinement",
		StyleHints: []string{" Less Polished ", "more frayed", "more frayed"},
	})
	if len(turn.StyleHints) != 2 {
		t.Fatalf("StyleHints len = %d, want 2 (%#v)", len(turn.StyleHints), turn.StyleHints)
	}
	if turn.StyleHints[0] != "less polished" || turn.StyleHints[1] != "more frayed" {
		t.Fatalf("StyleHints = %#v", turn.StyleHints)
	}
}

func TestSanitizeNormalizedTurnDefaultsDiscoveredWorkflowContract(t *testing.T) {
	turn := sanitizeNormalizedTurn("Can we add those to my library?", normalizedTurn{
		Intent:          "album_discovery",
		FollowupMode:    "query_previous_set",
		ReferenceTarget: "previous_results",
		ResultAction:    "preview_apply",
	})
	if turn.ResultSetKind != "discovered_albums" {
		t.Fatalf("ResultSetKind = %q, want discovered_albums", turn.ResultSetKind)
	}
	if turn.SelectionMode != "all" {
		t.Fatalf("SelectionMode = %q, want all", turn.SelectionMode)
	}
}

func TestSanitizeNormalizedTurnCoercesDiscoveredWorkflowIntentAndSelection(t *testing.T) {
	turn := sanitizeNormalizedTurn("Which of those are already in my library?", normalizedTurn{
		Intent:          "listening",
		FollowupMode:    "query_previous_set",
		ReferenceTarget: "previous_results",
		ResultSetKind:   "discovered_albums",
		ResultAction:    "inspect_availability",
		SelectionMode:   "explicit_names",
	})
	if turn.Intent != "album_discovery" {
		t.Fatalf("Intent = %q, want album_discovery", turn.Intent)
	}
	if turn.SelectionMode != "all" {
		t.Fatalf("SelectionMode = %q, want all", turn.SelectionMode)
	}
}

func TestHandleAlbumResultSetListeningFollowUpFiltersByWindow(t *testing.T) {
	lastCreativeAlbumSet.mu.Lock()
	lastCreativeAlbumSet.sessions = make(map[string]creativeAlbumSetState)
	lastCreativeAlbumSet.mu.Unlock()

	setLastCreativeAlbumSet("session-creative-recency", "semantic", "moody commute", []creativeAlbumCandidate{
		{
			Name:       "Sheer Heart Attack",
			ArtistName: "Queen",
			LastPlayed: time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339),
		},
		{
			Name:       "That's All",
			ArtistName: "Mel Tormé",
			LastPlayed: time.Now().UTC().AddDate(-2, 0, 0).Format(time.RFC3339),
		},
	})

	srv := &Server{}
	ctx := context.WithValue(context.Background(), chatSessionKey, "session-creative-recency")
	resp, ok := srv.handleAlbumResultSetListeningFollowUp(ctx, &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:          "listening",
			FollowupMode:    "query_previous_set",
			ReferenceTarget: "previous_results",
			TimeWindow:      "this_year",
			SubIntent:       "result_set_play_recency",
		},
	})
	if !ok {
		t.Fatal("expected previous-set listening follow-up to resolve")
	}
	if !containsIgnoreCase(resp, "Sheer Heart Attack") {
		t.Fatalf("response = %q, want matching album from this year", resp)
	}
	if containsIgnoreCase(resp, "That's All") {
		t.Fatalf("response = %q, did not expect stale album in this-year response", resp)
	}
}

func TestTryTurnIntentRouteHandlesAlbumDiscoveryRecencyFollowUp(t *testing.T) {
	lastCreativeAlbumSet.mu.Lock()
	lastCreativeAlbumSet.sessions = make(map[string]creativeAlbumSetState)
	lastCreativeAlbumSet.mu.Unlock()

	setLastCreativeAlbumSet("session-album-discovery-recency", "semantic", "moody commute", []creativeAlbumCandidate{
		{
			Name:       "Sheer Heart Attack",
			ArtistName: "Queen",
			LastPlayed: time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339),
		},
		{
			Name:       "That's All",
			ArtistName: "Mel Tormé",
			LastPlayed: time.Now().UTC().AddDate(-2, 0, 0).Format(time.RFC3339),
		},
	})

	srv := &Server{}
	ctx := context.WithValue(context.Background(), chatSessionKey, "session-album-discovery-recency")
	resp, ok := srv.tryTurnIntentRoute(ctx, &Turn{
		SessionID:   "session-album-discovery-recency",
		UserMessage: "Which of those have I played recently?",
		Normalized: TurnNormalized{
			Intent:          "album_discovery",
			SubIntent:       "result_set_play_recency",
			FollowupMode:    "query_previous_set",
			QueryScope:      "library",
			TimeWindow:      "this_year",
			ReferenceTarget: "previous_results",
		},
		Reference: TurnReference{
			HasCreativeAlbumSet: true,
		},
	}, nil)
	if !ok {
		t.Fatal("expected album_discovery follow-up to resolve")
	}
	if !containsIgnoreCase(resp.Response, "Sheer Heart Attack") {
		t.Fatalf("response = %q, want matching album from this year", resp.Response)
	}
	if containsIgnoreCase(resp.Response, "That's All") {
		t.Fatalf("response = %q, did not expect stale album in this-year response", resp.Response)
	}
}

func TestTryTurnIntentRouteHandlesListeningRecencyFollowUpViaConversationObject(t *testing.T) {
	lastCreativeAlbumSet.mu.Lock()
	lastCreativeAlbumSet.sessions = make(map[string]creativeAlbumSetState)
	lastCreativeAlbumSet.mu.Unlock()

	setLastCreativeAlbumSet("session-listening-object-recency", "semantic", "moody commute", []creativeAlbumCandidate{
		{
			Name:       "Sheer Heart Attack",
			ArtistName: "Queen",
			LastPlayed: time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339),
		},
		{
			Name:       "That's All",
			ArtistName: "Mel Tormé",
			LastPlayed: time.Now().UTC().AddDate(-2, 0, 0).Format(time.RFC3339),
		},
	})

	srv := &Server{}
	ctx := context.WithValue(context.Background(), chatSessionKey, "session-listening-object-recency")
	resp, ok := srv.tryTurnIntentRoute(ctx, &Turn{
		SessionID:   "session-listening-object-recency",
		UserMessage: "Which of those have I played recently?",
		Normalized: TurnNormalized{
			Intent:          "listening",
			SubIntent:       "result_set_play_recency",
			ConversationOp:  "inspect",
			FollowupMode:    "query_previous_set",
			QueryScope:      "library",
			TimeWindow:      "this_year",
			ReferenceTarget: "previous_results",
		},
		Reference: TurnReference{
			ObjectType:          "result_set",
			ObjectKind:          "creative_albums",
			ObjectStatus:        "result_set",
			ObjectIntent:        "album_discovery",
			ObjectTarget:        "previous_results",
			HasCreativeAlbumSet: true,
		},
	}, nil)
	if !ok {
		t.Fatal("expected listening follow-up to resolve through conversation object")
	}
	if !containsIgnoreCase(resp.Response, "Sheer Heart Attack") {
		t.Fatalf("response = %q, want matching album from this year", resp.Response)
	}
	if containsIgnoreCase(resp.Response, "That's All") {
		t.Fatalf("response = %q, did not expect stale album in this-year response", resp.Response)
	}
}

func TestTryTurnIntentRouteHandlesAlbumDiscoveryRecencyFollowUpWithUnavailableRequestedSet(t *testing.T) {
	lastCreativeAlbumSet.mu.Lock()
	lastCreativeAlbumSet.sessions = make(map[string]creativeAlbumSetState)
	lastCreativeAlbumSet.mu.Unlock()

	setLastCreativeAlbumSet("session-album-discovery-recency-fallback", "semantic", "moody commute", []creativeAlbumCandidate{
		{
			Name:       "Sheer Heart Attack",
			ArtistName: "Queen",
			LastPlayed: time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339),
		},
		{
			Name:       "That's All",
			ArtistName: "Mel Tormé",
			LastPlayed: time.Now().UTC().AddDate(-2, 0, 0).Format(time.RFC3339),
		},
	})

	srv := &Server{}
	ctx := context.WithValue(context.Background(), chatSessionKey, "session-album-discovery-recency-fallback")
	resp, ok := srv.tryTurnIntentRoute(ctx, &Turn{
		SessionID:   "session-album-discovery-recency-fallback",
		UserMessage: "Which of those have I played recently?",
		Normalized: TurnNormalized{
			Intent:          "album_discovery",
			SubIntent:       "result_set_play_recency",
			FollowupMode:    "query_previous_set",
			QueryScope:      "library",
			TimeWindow:      "this_year",
			ResultSetKind:   "discovered_albums",
			ReferenceTarget: "previous_results",
		},
	}, nil)
	if !ok {
		t.Fatal("expected album_discovery follow-up to resolve")
	}
	if !containsIgnoreCase(resp.Response, "Sheer Heart Attack") {
		t.Fatalf("response = %q, want matching album from this year", resp.Response)
	}
	if containsIgnoreCase(resp.Response, "That's All") {
		t.Fatalf("response = %q, did not expect stale album in this-year response", resp.Response)
	}
}

func TestDescribeStructuredRecentListeningInterpretationUsesSubIntent(t *testing.T) {
	resp, ok := describeStructuredRecentListeningInterpretation(recentListeningState{
		artistsHeard: 4,
		topArtists: []recentListeningArtistState{
			{ArtistName: "Radiohead", TrackCount: 20},
			{ArtistName: "Air", TrackCount: 5},
		},
	}, "listening_interpretation")
	if !ok {
		t.Fatal("expected structured interpretation to resolve")
	}
	if !containsIgnoreCase(resp, "taste looks") {
		t.Fatalf("response = %q", resp)
	}
}

func TestHandleStructuredCreativeAlbumSetFollowUpUsesSubIntent(t *testing.T) {
	lastCreativeAlbumSet.mu.Lock()
	lastCreativeAlbumSet.sessions = make(map[string]creativeAlbumSetState)
	lastCreativeAlbumSet.mu.Unlock()

	setLastCreativeAlbumSet("session-creative-risk", "semantic", "moody commute", []creativeAlbumCandidate{
		{Name: "Safe Pick", ArtistName: "Artist One", PlayCount: 6},
		{Name: "Risk Pick", ArtistName: "Artist Two", Genre: "experimental drone", PlayCount: 0},
	})

	srv := &Server{}
	ctx := context.WithValue(context.Background(), chatSessionKey, "session-creative-risk")
	resp, ok := srv.handleStructuredCreativeAlbumSetFollowUp(ctx, &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:       "album_discovery",
			FollowupMode: "refine_previous",
			SubIntent:    "creative_risk_pick",
		},
	})
	if !ok {
		t.Fatal("expected structured creative follow-up to resolve")
	}
	if !containsIgnoreCase(resp, "Risk Pick") {
		t.Fatalf("response = %q", resp)
	}
}

func TestResolveTurnContextInfersDiscoveredResultSetKind(t *testing.T) {
	lastAlbumDiscovery = discovery.NewStore()
	setLastDiscoveredAlbums("session-discovered-contract", "dream pop", []discoveredAlbumCandidate{
		{Rank: 1, ArtistName: "Air", AlbumTitle: "Moon Safari"},
	})

	resolved := resolveTurnContext("session-discovered-contract", normalizedTurn{
		Intent:          "album_discovery",
		FollowupMode:    "query_previous_set",
		ReferenceTarget: "previous_results",
		ResultAction:    "inspect_availability",
	})
	if resolved.Turn.ResultSetKind != "discovered_albums" {
		t.Fatalf("ResultSetKind = %q, want discovered_albums", resolved.Turn.ResultSetKind)
	}
}

func TestBuildDiscoveredAlbumSelectionFromTurn(t *testing.T) {
	tests := []struct {
		name  string
		turn  normalizedTurn
		total int
		want  string
		ok    bool
	}{
		{
			name:  "all",
			turn:  normalizedTurn{SelectionMode: "all"},
			total: 4,
			want:  "all",
			ok:    true,
		},
		{
			name:  "top n",
			turn:  normalizedTurn{SelectionMode: "top_n", SelectionValue: "2"},
			total: 4,
			want:  "first 2",
			ok:    true,
		},
		{
			name:  "ordinal",
			turn:  normalizedTurn{SelectionMode: "ordinal", SelectionValue: "2, 4"},
			total: 5,
			want:  "2, 4",
			ok:    true,
		},
		{
			name:  "explicit names",
			turn:  normalizedTurn{SelectionMode: "explicit_names", SelectionValue: "Moon Safari by Air"},
			total: 5,
			want:  "Moon Safari by Air",
			ok:    true,
		},
		{
			name:  "missing only maps to full selection until workflow filtering is added",
			turn:  normalizedTurn{SelectionMode: "missing_only"},
			total: 5,
			want:  "",
			ok:    false,
		},
	}

	for _, tc := range tests {
		got, ok := buildDiscoveredAlbumSelectionFromTurn(tc.turn, tc.total)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("%s: buildDiscoveredAlbumSelectionFromTurn() = (%q, %v), want (%q, %v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestSelectDiscoveredCandidatesFromResolvedUsesFocusedItem(t *testing.T) {
	candidates := []discoveredAlbumCandidate{
		{Rank: 1, ArtistName: "Air", AlbumTitle: "Moon Safari"},
		{Rank: 2, ArtistName: "Broadcast", AlbumTitle: "Tender Buttons"},
	}
	selected, selection, ok := selectDiscoveredCandidatesFromResolved(&resolvedTurnContext{
		Turn: normalizedTurn{
			ResultSetKind:      "discovered_albums",
			ReferenceQualifier: "last_item",
		},
		ResolvedReferenceKind: "discovered_albums",
		ResolvedItemKey:       normalizedDiscoveredAlbumCandidateKey(candidates[1]),
	}, candidates)
	if !ok {
		t.Fatal("expected focused discovered item selection to resolve")
	}
	if selection != "2" {
		t.Fatalf("selection = %q, want %q", selection, "2")
	}
	if len(selected) != 1 || selected[0].AlbumTitle != "Tender Buttons" {
		t.Fatalf("selected = %#v", selected)
	}
}

func TestFilterDiscoveredMatchesMissingOnly(t *testing.T) {
	candidates := []discoveredAlbumCandidate{
		{Rank: 1, ArtistName: "Air", AlbumTitle: "Moon Safari"},
		{Rank: 2, ArtistName: "Moby", AlbumTitle: "Play"},
		{Rank: 3, ArtistName: "Röyksopp", AlbumTitle: "Melody A.M."},
	}
	matches := []lidarrAlbumMatch{
		{Rank: 1, AlbumTitle: "Moon Safari", Status: "already_monitored"},
		{Rank: 2, AlbumTitle: "Play", Status: "can_monitor"},
		{Rank: 3, AlbumTitle: "Melody A.M.", Status: "album_not_found"},
	}
	filteredCandidates, filteredMatches := filterDiscoveredMatchesMissingOnly(candidates, matches)
	if len(filteredCandidates) != 2 || len(filteredMatches) != 2 {
		t.Fatalf("filtered = %d candidates, %d matches; want 2 and 2", len(filteredCandidates), len(filteredMatches))
	}
	if filteredCandidates[0].Rank != 2 || filteredCandidates[1].Rank != 3 {
		t.Fatalf("filtered candidate ranks = %d, %d; want 2, 3", filteredCandidates[0].Rank, filteredCandidates[1].Rank)
	}
}

func TestBuildDiscoveredAlbumRankSelection(t *testing.T) {
	selection := buildDiscoveredAlbumRankSelection([]discoveredAlbumCandidate{
		{Rank: 4, ArtistName: "Röyksopp", AlbumTitle: "Melody A.M."},
		{Rank: 2, ArtistName: "Moby", AlbumTitle: "Play"},
		{Rank: 4, ArtistName: "Röyksopp", AlbumTitle: "Melody A.M."},
	})
	if selection != "2, 4" {
		t.Fatalf("selection = %q, want %q", selection, "2, 4")
	}
}

func TestSanitizeNormalizedTurnKeepsSceneStructuredSelection(t *testing.T) {
	turn := sanitizeNormalizedTurn("Use the one with 31 tracks.", normalizedTurn{
		Intent:          "other",
		FollowupMode:    "query_previous_set",
		ReferenceTarget: "previous_results",
		ResultSetKind:   "scene_candidates",
		ResultAction:    "select_candidate",
		SelectionMode:   "count_match",
		SelectionValue:  "31",
	})
	if turn.ResultSetKind != "scene_candidates" {
		t.Fatalf("ResultSetKind = %q, want scene_candidates", turn.ResultSetKind)
	}
	if turn.ResultAction != "select_candidate" {
		t.Fatalf("ResultAction = %q, want select_candidate", turn.ResultAction)
	}
	if turn.SelectionMode != "count_match" || turn.SelectionValue != "31" {
		t.Fatalf("selection = (%q, %q), want (count_match, 31)", turn.SelectionMode, turn.SelectionValue)
	}
}

func TestResolveSceneCandidateFromTurn(t *testing.T) {
	candidates := []sceneSessionItem{
		{Name: "Indie / Rock / Alternative • Mid-Tempo", Subtitle: "Relaxed, Sad", SongCount: 31},
		{Name: "Indie / Rock / Alternative • Mid-Tempo", Subtitle: "Sad, Happy", SongCount: 23},
	}
	resolved, ok := resolveSceneCandidateFromReference(resolvedResultReference{
		resultReference: resultReference{
			SetKind: "scene_candidates",
			Action:  "select_candidate",
			Selection: resultSelection{
				Mode:  "count_match",
				Value: "31",
			},
		},
	}, candidates)
	if !ok || resolved == nil {
		t.Fatal("expected scene selection to resolve")
	}
	if resolved.SongCount != 31 {
		t.Fatalf("resolved song count = %d, want 31", resolved.SongCount)
	}
}

func TestResolveTurnContextInfersPlaylistCandidates(t *testing.T) {
	setLastPlannedPlaylist("session-playlist-availability", "late night ambient", "Late Nights", []playlistCandidateTrack{
		{ArtistName: "Air", TrackTitle: "La Femme d'Argent"},
	})
	resolved := resolveTurnContext("session-playlist-availability", normalizedTurn{
		Intent:          "playlist",
		FollowupMode:    "query_previous_set",
		ReferenceTarget: "previous_playlist",
		ResultAction:    "inspect_availability",
	})
	if resolved.Turn.ResultSetKind != "playlist_candidates" {
		t.Fatalf("ResultSetKind = %q, want playlist_candidates", resolved.Turn.ResultSetKind)
	}
}

func TestResolveTurnContextInfersCleanupCandidates(t *testing.T) {
	setLastLidarrCandidates("session-cleanup-apply", []lidarrCleanupCandidate{
		{AlbumID: 1, ArtistName: "Air", Title: "Moon Safari"},
	})
	resolved := resolveTurnContext("session-cleanup-apply", normalizedTurn{
		Intent:          "other",
		SubIntent:       "lidarr_cleanup_apply",
		FollowupMode:    "query_previous_set",
		ReferenceTarget: "previous_results",
		ResultAction:    "preview_apply",
	})
	if resolved.Turn.ResultSetKind != "cleanup_candidates" {
		t.Fatalf("ResultSetKind = %q, want cleanup_candidates", resolved.Turn.ResultSetKind)
	}
}

func TestResolveTurnContextInfersBadlyRatedAlbums(t *testing.T) {
	setLastBadlyRatedAlbums("session-badly-rated-apply", []badlyRatedAlbumCandidate{
		{AlbumID: "1", ArtistName: "Air", AlbumName: "Moon Safari"},
	})
	resolved := resolveTurnContext("session-badly-rated-apply", normalizedTurn{
		Intent:          "other",
		SubIntent:       "badly_rated_cleanup",
		FollowupMode:    "query_previous_set",
		ReferenceTarget: "previous_results",
		ResultAction:    "preview_apply",
	})
	if resolved.Turn.ResultSetKind != "badly_rated_albums" {
		t.Fatalf("ResultSetKind = %q, want badly_rated_albums", resolved.Turn.ResultSetKind)
	}
}

func TestResolveTurnContextUsesActiveFocusForEmptyBadlyRatedFollowUp(t *testing.T) {
	setLastBadlyRatedAlbums("session-badly-rated-empty-focus", nil)

	resolved := resolveTurnContext("session-badly-rated-empty-focus", normalizedTurn{
		Intent:          "playlist",
		SubIntent:       "playlist_append",
		FollowupMode:    "query_previous_set",
		ReferenceTarget: "previous_results",
		SelectionMode:   "top_n",
		SelectionValue:  "two",
	})

	if !resolved.HasActiveFocus {
		t.Fatal("expected active focus to be available")
	}
	if resolved.ActiveFocusKind != "badly_rated_albums" {
		t.Fatalf("ActiveFocusKind = %q, want badly_rated_albums", resolved.ActiveFocusKind)
	}
	if resolved.Turn.Intent != "other" {
		t.Fatalf("Intent = %q, want other", resolved.Turn.Intent)
	}
	if resolved.Turn.SubIntent != "badly_rated_cleanup" {
		t.Fatalf("SubIntent = %q, want badly_rated_cleanup", resolved.Turn.SubIntent)
	}
	if resolved.Turn.ResultAction != "preview_apply" {
		t.Fatalf("ResultAction = %q, want preview_apply", resolved.Turn.ResultAction)
	}
	if resolved.Turn.ResultSetKind != "badly_rated_albums" {
		t.Fatalf("ResultSetKind = %q, want badly_rated_albums", resolved.Turn.ResultSetKind)
	}
	if resolved.ResolvedReferenceKind != "badly_rated_albums" {
		t.Fatalf("ResolvedReferenceKind = %q, want badly_rated_albums", resolved.ResolvedReferenceKind)
	}
	if resolved.MissingReferenceContext {
		t.Fatal("expected active focus to satisfy reference context")
	}
}

func TestResolveTurnContextOverridesWrongActionForEmptyBadlyRatedObject(t *testing.T) {
	setLastBadlyRatedAlbums("session-badly-rated-empty-action", nil)

	resolved := resolveTurnContext("session-badly-rated-empty-action", normalizedTurn{
		RawMessage:      "Pick two worth saving.",
		Intent:          "other",
		SubIntent:       "badly_rated_cleanup",
		ConversationOp:  "select",
		FollowupMode:    "refine_previous",
		QueryScope:      "library",
		LibraryOnly:     boolPtr(true),
		ReferenceTarget: "previous_results",
		ResultSetKind:   "badly_rated_albums",
		ResultAction:    "select_candidate",
		SelectionMode:   "top_n",
		SelectionValue:  "2",
	})

	if resolved.Turn.ConversationOp != "apply" {
		t.Fatalf("ConversationOp = %q, want apply", resolved.Turn.ConversationOp)
	}
	if resolved.Turn.ResultAction != "preview_apply" {
		t.Fatalf("ResultAction = %q, want preview_apply", resolved.Turn.ResultAction)
	}
	if resolved.Turn.ResultSetKind != "badly_rated_albums" {
		t.Fatalf("ResultSetKind = %q, want badly_rated_albums", resolved.Turn.ResultSetKind)
	}
	if resolved.ResolvedReferenceKind != "badly_rated_albums" {
		t.Fatalf("ResolvedReferenceKind = %q, want badly_rated_albums", resolved.ResolvedReferenceKind)
	}
}

func TestApplyPreviousTurnClarificationCarryoverKeepsStatsDimension(t *testing.T) {
	sessionID := "session-stats-clarification-carryover"

	lastNormalizedTurn.mu.Lock()
	lastNormalizedTurn.sessions = make(map[string]normalizedTurnState)
	lastNormalizedTurn.mu.Unlock()

	setLastNormalizedTurn(sessionID, normalizedTurn{
		Intent:              "stats",
		SubIntent:           "library_top_artists",
		QueryScope:          "stats",
		NeedsClarification:  true,
		ClarificationFocus:  "scope",
		ClarificationPrompt: "Do you mean library stats or listening stats?",
	})

	resolved := applyPreviousTurnClarificationCarryover(sessionID, resolvedTurnContext{
		Turn: sanitizeNormalizedTurn("Listening over the last month.", normalizedTurn{
			Intent:     "listening",
			SubIntent:  "listening_summary",
			QueryScope: "listening",
			TimeWindow: "last_month",
		}),
	})

	if resolved.Turn.Intent != "stats" {
		t.Fatalf("Intent = %q, want stats", resolved.Turn.Intent)
	}
	if resolved.Turn.QueryScope != "listening" {
		t.Fatalf("QueryScope = %q, want listening", resolved.Turn.QueryScope)
	}
	if resolved.Turn.SubIntent != "library_top_artists" {
		t.Fatalf("SubIntent = %q, want library_top_artists", resolved.Turn.SubIntent)
	}
	if resolved.Turn.NeedsClarification {
		t.Fatal("expected clarification to be cleared")
	}
}

func TestResolveTurnContextInfersArtistFromArtistCandidateConversationObject(t *testing.T) {
	sessionID := "session-artist-object-artist-name"
	setLastArtistCandidateSet(sessionID, "top artists last month", []artistCandidate{
		{Name: "Radiohead", PlayCount: 12, Score: 9},
		{Name: "Massive Attack", PlayCount: 7, Score: 4},
	})
	setLastActiveFocusFromTurn(sessionID, "artist_candidates", "listening_stats", normalizedTurn{
		Intent:     "stats",
		SubIntent:  "library_top_artists",
		QueryScope: "listening",
		PromptHint: "top artists last month",
	})

	resolved := resolveTurnContext(sessionID, normalizedTurn{
		RawMessage:   "Then give me one Radiohead album to revisit tonight.",
		Intent:       "artist_discovery",
		FollowupMode: "query_previous_set",
		QueryScope:   "unknown",
	})

	if !resolved.HasConversationObject {
		t.Fatal("expected conversation object to be available")
	}
	if resolved.Turn.ArtistName != "Radiohead" {
		t.Fatalf("ArtistName = %q, want Radiohead", resolved.Turn.ArtistName)
	}
	if resolved.Turn.ResultSetKind != "artist_candidates" {
		t.Fatalf("ResultSetKind = %q, want artist_candidates", resolved.Turn.ResultSetKind)
	}
	if resolved.Turn.FollowupMode != "query_previous_set" {
		t.Fatalf("FollowupMode = %q, want query_previous_set", resolved.Turn.FollowupMode)
	}
	if resolved.Turn.ReferenceTarget != "previous_results" {
		t.Fatalf("ReferenceTarget = %q, want previous_results", resolved.Turn.ReferenceTarget)
	}
	if resolved.Turn.ConversationOp == "constrain" {
		t.Fatalf("ConversationOp = %q, want non-constrain follow-up", resolved.Turn.ConversationOp)
	}
}

func TestResolveTurnContextDoesNotInheritPromptHintAcrossFreshPivot(t *testing.T) {
	sessionID := "session-fresh-pivot-no-prompt-inherit"
	setLastArtistCandidateSet(sessionID, "top artists last month", []artistCandidate{
		{Name: "Radiohead", PlayCount: 12, Score: 9},
		{Name: "Massive Attack", PlayCount: 7, Score: 4},
	})
	setLastActiveFocusFromTurn(sessionID, "artist_candidates", "listening_stats", normalizedTurn{
		Intent:     "stats",
		SubIntent:  "library_top_artists",
		QueryScope: "listening",
		TimeWindow: "last_month",
		PromptHint: "last_month",
	})

	resolved := resolveTurnContext(sessionID, sanitizeNormalizedTurn("Forget stats for a second. Find me two records in my library for a predawn drive.", normalizedTurn{
		RawMessage:      "Forget stats for a second. Find me two records in my library for a predawn drive.",
		Intent:          "album_discovery",
		SubIntent:       "creative_refinement",
		QueryScope:      "library",
		LibraryOnly:     boolPtr(true),
		SelectionMode:   "top_n",
		SelectionValue:  "2",
		StyleHints:      []string{"predawn", "drive"},
		FollowupMode:    "none",
		ReferenceTarget: "none",
		PromptHint:      "",
	}))

	if resolved.Turn.PromptHint != "" {
		t.Fatalf("PromptHint = %q, want empty for fresh pivot", resolved.Turn.PromptHint)
	}
	if resolved.Turn.TimeWindow != "none" {
		t.Fatalf("TimeWindow = %q, want none for fresh pivot", resolved.Turn.TimeWindow)
	}
	if resolved.Turn.ArtistName != "" {
		t.Fatalf("ArtistName = %q, want empty for fresh pivot", resolved.Turn.ArtistName)
	}
}

func TestResolveTurnContextInfersConversationConstraintFromActiveObject(t *testing.T) {
	sessionID := "session-conversation-object-constraint"
	setLastCreativeAlbumSet(sessionID, "semantic_album_search", "rainy late-night walk", []creativeAlbumCandidate{
		{Name: "Play", ArtistName: "Moby"},
		{Name: "The Campfire Headphase", ArtistName: "Boards of Canada"},
	})
	setLastActiveFocusFromTurn(sessionID, "creative_albums", "result_set", normalizedTurn{
		Intent:     "album_discovery",
		QueryScope: "general",
		PromptHint: "rainy late-night walk",
	})

	resolved := resolveTurnContext(sessionID, normalizedTurn{
		RawMessage:   "Actually keep it to my library.",
		Intent:       "album_discovery",
		QueryScope:   "library",
		FollowupMode: "none",
	})

	if !resolved.HasConversationObject {
		t.Fatal("expected conversation object to be available")
	}
	if resolved.Turn.ConversationOp != "constrain" {
		t.Fatalf("ConversationOp = %q, want constrain", resolved.Turn.ConversationOp)
	}
	if resolved.Turn.FollowupMode != "refine_previous" {
		t.Fatalf("FollowupMode = %q, want refine_previous", resolved.Turn.FollowupMode)
	}
	if resolved.Turn.ReferenceTarget != "previous_results" {
		t.Fatalf("ReferenceTarget = %q, want previous_results", resolved.Turn.ReferenceTarget)
	}
	if resolved.Turn.ResultSetKind != "creative_albums" {
		t.Fatalf("ResultSetKind = %q, want creative_albums", resolved.Turn.ResultSetKind)
	}
}

func TestResolveTurnContextInfersConversationConstraintFromDiscoveredAlbumsObject(t *testing.T) {
	sessionID := "session-discovered-conversation-object-constraint"
	setLastDiscoveredAlbums(sessionID, "rainy late-night walk", []discoveredAlbumCandidate{
		{Rank: 1, AlbumTitle: "Play", ArtistName: "Moby", Year: 1999},
		{Rank: 2, AlbumTitle: "Moon Safari", ArtistName: "Air", Year: 1998},
	})

	resolved := resolveTurnContext(sessionID, normalizedTurn{
		RawMessage:   "Actually keep it to my library.",
		Intent:       "album_discovery",
		QueryScope:   "library",
		FollowupMode: "none",
	})

	if !resolved.HasConversationObject {
		t.Fatal("expected conversation object to be available")
	}
	if resolved.Turn.ConversationOp != "constrain" {
		t.Fatalf("ConversationOp = %q, want constrain", resolved.Turn.ConversationOp)
	}
	if resolved.Turn.FollowupMode != "refine_previous" {
		t.Fatalf("FollowupMode = %q, want refine_previous", resolved.Turn.FollowupMode)
	}
	if resolved.Turn.ReferenceTarget != "previous_results" {
		t.Fatalf("ReferenceTarget = %q, want previous_results", resolved.Turn.ReferenceTarget)
	}
	if resolved.Turn.ResultSetKind != "discovered_albums" {
		t.Fatalf("ResultSetKind = %q, want discovered_albums", resolved.Turn.ResultSetKind)
	}
}

func TestResolveTurnContextOverridesMismatchedAlbumSetKindFromCreativeFocus(t *testing.T) {
	sessionID := "session-creative-artist-followup-mismatch"
	setLastCreativeAlbumSet(sessionID, "semantic_structured", "melancholic dream pop", []creativeAlbumCandidate{
		{Name: "Honeymoon", ArtistName: "Lana Del Rey"},
		{Name: "Warpaint", ArtistName: "Warpaint"},
	})
	libraryOnly := true
	setLastActiveFocusFromTurn(sessionID, "creative_albums", "result_set", normalizedTurn{
		QueryScope:  "library",
		LibraryOnly: &libraryOnly,
	})

	resolved := resolveTurnContext(sessionID, normalizedTurn{
		RawMessage:      "Then give me one Lana Del Rey album I should revisit tonight.",
		Intent:          "album_discovery",
		SubIntent:       "artist_starting_album",
		QueryScope:      "library",
		LibraryOnly:     &libraryOnly,
		FollowupMode:    "refine_previous",
		ReferenceTarget: "previous_results",
		ResultSetKind:   "discovered_albums",
		ResultAction:    "select_candidate",
		SelectionMode:   "explicit_names",
		SelectionValue:  "Honeymoon by Lana Del Rey",
		ArtistName:      "Lana Del Rey",
	})

	if resolved.Turn.ResultSetKind != "creative_albums" {
		t.Fatalf("ResultSetKind = %q, want creative_albums", resolved.Turn.ResultSetKind)
	}
	if resolved.ResolvedReferenceKind != "creative_albums" {
		t.Fatalf("ResolvedReferenceKind = %q, want creative_albums", resolved.ResolvedReferenceKind)
	}
}

func TestTryNormalizedIntentRouteHandlesCreativeArtistFilteredFollowUp(t *testing.T) {
	sessionID := "route-creative-artist-followup"
	setLastCreativeAlbumSet(sessionID, "semantic_structured", "rainy late-night walk", []creativeAlbumCandidate{
		{Name: "Kid A", ArtistName: "Radiohead", PlayCount: 12, LastPlayed: "2026-03-01T12:00:00Z"},
		{Name: "Amnesiac", ArtistName: "Radiohead", PlayCount: 4, LastPlayed: "2026-02-01T12:00:00Z"},
		{Name: "Moon Safari", ArtistName: "Air", PlayCount: 2, LastPlayed: "2026-03-05T12:00:00Z"},
	})

	srv := &Server{}
	ctx := context.WithValue(context.Background(), chatSessionKey, sessionID)
	resolved := &resolvedTurnContext{
		Turn: normalizedTurn{
			RawMessage:      "Then give me one Radiohead album I should revisit tonight.",
			Intent:          "album_discovery",
			FollowupMode:    "query_previous_set",
			ReferenceTarget: "previous_results",
			ResultSetKind:   "creative_albums",
			ArtistName:      "Radiohead",
			SelectionMode:   "top_n",
			SelectionValue:  "1",
		},
		ResolvedReferenceKind: "creative_albums",
	}

	resp, ok := srv.tryNormalizedIntentRoute(ctx, "Then give me one Radiohead album I should revisit tonight.", nil, resolved)
	if !ok {
		t.Fatal("tryNormalizedIntentRoute() = false, want true")
	}
	if !strings.Contains(resp.Response, "Kid A by Radiohead") {
		t.Fatalf("response = %q", resp.Response)
	}
	if strings.Contains(resp.Response, "Moon Safari") {
		t.Fatalf("response leaked non-Radiohead candidate: %q", resp.Response)
	}
}

func TestTryNormalizedIntentRouteHandlesConversationConstraintFollowUp(t *testing.T) {
	sessionID := "route-conversation-object-constraint"
	setLastCreativeAlbumSet(sessionID, "semantic_album_search", "rainy late-night walk", []creativeAlbumCandidate{
		{Name: "Play", ArtistName: "Moby"},
		{Name: "The Campfire Headphase", ArtistName: "Boards of Canada"},
	})
	setLastActiveFocusFromTurn(sessionID, "creative_albums", "result_set", normalizedTurn{
		Intent:     "album_discovery",
		QueryScope: "general",
		PromptHint: "rainy late-night walk",
	})

	srv := &Server{}
	ctx := context.WithValue(context.Background(), chatSessionKey, sessionID)
	resolved := resolveTurnContext(sessionID, normalizedTurn{
		RawMessage:   "Actually keep it to my library.",
		Intent:       "album_discovery",
		QueryScope:   "library",
		FollowupMode: "none",
	})

	resp, ok := srv.tryNormalizedIntentRoute(ctx, "Actually keep it to my library.", nil, &resolved)
	if !ok {
		t.Fatal("tryNormalizedIntentRoute() = false, want true")
	}
	if !strings.Contains(resp.Response, "From your library") {
		t.Fatalf("response = %q", resp.Response)
	}
	if !strings.Contains(resp.Response, "Play by Moby") {
		t.Fatalf("response = %q", resp.Response)
	}
}

func TestTryTurnIntentRouteHandlesStructuredGeneralAlbumDiscovery(t *testing.T) {
	originalRunner := discoverAlbumsRequestRunner
	discoverAlbumsRequestRunner = func(_ context.Context, request discovery.Request) ([]discoveredAlbumCandidate, map[string]interface{}, error) {
		if request.Query != "Give me three records for a rainy late-night walk." {
			t.Fatalf("request.Query = %q", request.Query)
		}
		if request.Limit != 3 {
			t.Fatalf("request.Limit = %d, want 3", request.Limit)
		}
		return []discoveredAlbumCandidate{
			{Rank: 1, AlbumTitle: "Play", ArtistName: "Moby", Year: 1999, Reason: "nocturnal trip-hop pulse"},
			{Rank: 2, AlbumTitle: "Moon Safari", ArtistName: "Air", Year: 1998, Reason: "soft, drifting night glide"},
			{Rank: 3, AlbumTitle: "Dummy", ArtistName: "Portishead", Year: 1994, Reason: "rain-streaked late-night mood"},
		}, map[string]interface{}{}, nil
	}
	defer func() {
		discoverAlbumsRequestRunner = originalRunner
	}()

	srv := &Server{}
	sessionID := "route-general-album-discovery"
	ctx := context.WithValue(context.Background(), chatSessionKey, sessionID)
	resp, ok := srv.tryTurnIntentRoute(ctx, &Turn{
		SessionID:   sessionID,
		UserMessage: "Give me three records for a rainy late-night walk.",
		Normalized: TurnNormalized{
			Intent:         "album_discovery",
			QueryScope:     "general",
			SelectionMode:  "top_n",
			SelectionValue: "3",
		},
	}, nil)
	if !ok {
		t.Fatal("expected structured general album discovery to resolve")
	}
	if !strings.Contains(resp.Response, "Play by Moby") {
		t.Fatalf("response = %q", resp.Response)
	}
	memory := loadTurnSessionMemory(sessionID)
	focus, ok := memory.ActiveFocus()
	if !ok {
		t.Fatal("expected active focus after discovery")
	}
	if focus.kind != "discovered_albums" {
		t.Fatalf("focus.kind = %q, want discovered_albums", focus.kind)
	}
	if focus.queryScope != "general" {
		t.Fatalf("focus.queryScope = %q, want general", focus.queryScope)
	}
}

func TestTryNormalizedIntentRouteHandlesCreativeArtistFollowUpWithMismatchedRequestedSet(t *testing.T) {
	sessionID := "route-creative-artist-followup-mismatch"
	setLastCreativeAlbumSet(sessionID, "semantic_structured", "melancholic dream pop", []creativeAlbumCandidate{
		{Name: "Honeymoon", ArtistName: "Lana Del Rey", PlayCount: 2, LastPlayed: "2026-03-01T12:00:00Z"},
		{Name: "Warpaint", ArtistName: "Warpaint", PlayCount: 6, LastPlayed: "2026-02-01T12:00:00Z"},
	})

	srv := &Server{}
	ctx := context.WithValue(context.Background(), chatSessionKey, sessionID)
	resolved := &resolvedTurnContext{
		Turn: normalizedTurn{
			RawMessage:      "Then give me one Lana Del Rey album I should revisit tonight.",
			Intent:          "album_discovery",
			SubIntent:       "artist_starting_album",
			FollowupMode:    "refine_previous",
			ReferenceTarget: "previous_results",
			ResultSetKind:   "discovered_albums",
			ResultAction:    "select_candidate",
			SelectionMode:   "explicit_names",
			SelectionValue:  "Honeymoon by Lana Del Rey",
			ArtistName:      "Lana Del Rey",
			QueryScope:      "library",
		},
		HasActiveFocus:          true,
		ActiveFocusKind:         "creative_albums",
		ActiveFocusStatus:       "result_set",
		ResolvedReferenceKind:   "creative_albums",
		ResolvedReferenceSource: "active_focus",
	}

	resp, ok := srv.tryNormalizedIntentRoute(ctx, "Then give me one Lana Del Rey album I should revisit tonight.", nil, resolved)
	if !ok {
		t.Fatal("tryNormalizedIntentRoute() = false, want true")
	}
	if !strings.Contains(resp.Response, "Honeymoon by Lana Del Rey") {
		t.Fatalf("response = %q", resp.Response)
	}
	if strings.Contains(resp.Response, "Warpaint") {
		t.Fatalf("response leaked non-Lana candidate: %q", resp.Response)
	}
}

func TestTryNormalizedIntentRouteHandlesArtistCatalogDepthFollowUp(t *testing.T) {
	sessionID := "route-artist-catalog-depth"
	setLastArtistCandidateSet(sessionID, "top artists last month", []artistCandidate{
		{Name: "Radiohead", PlayCount: 12, Score: 9},
		{Name: "Massive Attack", PlayCount: 7, Score: 4},
		{Name: "Portishead", PlayCount: 5, Score: 4},
	})

	srv := &Server{}
	ctx := context.WithValue(context.Background(), chatSessionKey, sessionID)
	resolved := &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:          "stats",
			SubIntent:       "artist_catalog_depth",
			FollowupMode:    "query_previous_set",
			ReferenceTarget: "previous_results",
			QueryScope:      "library",
			ResultSetKind:   "artist_candidates",
		},
		ResolvedReferenceKind: "artist_candidates",
	}

	resp, ok := srv.tryNormalizedIntentRoute(ctx, "Who has the deepest catalog?", nil, resolved)
	if !ok {
		t.Fatal("tryNormalizedIntentRoute() = false, want true")
	}
	if !strings.Contains(resp.Response, "Radiohead has the deepest catalog in your library with 9 albums") {
		t.Fatalf("response = %q", resp.Response)
	}
}

func TestResolveTurnContextBindsLatestReferenceSet(t *testing.T) {
	lastCreativeAlbumSet.mu.Lock()
	lastCreativeAlbumSet.sessions[normalizeChatSessionID("session-latest-ref")] = creativeAlbumSetState{
		mode:      "underplayed_albums",
		queryText: "underplayed",
		updatedAt: time.Now().UTC().Add(-2 * time.Minute),
		candidates: []creativeAlbumCandidate{
			{Name: "Older Pick", ArtistName: "Artist A"},
		},
	}
	lastCreativeAlbumSet.mu.Unlock()
	lastSemanticAlbumSearch.mu.Lock()
	lastSemanticAlbumSearch.sessions[normalizeChatSessionID("session-latest-ref")] = semanticAlbumSearchState{
		queryText: "dreamy albums",
		updatedAt: time.Now().UTC(),
		matches: []semanticAlbumSearchMatch{
			{Name: "Newer Pick", ArtistName: "Artist B"},
		},
	}
	lastSemanticAlbumSearch.mu.Unlock()

	resolved := resolveTurnContext("session-latest-ref", normalizedTurn{
		Intent:             "album_discovery",
		FollowupMode:       "query_previous_set",
		ReferenceTarget:    "previous_results",
		ReferenceQualifier: "latest_set",
	})
	if resolved.ResolvedReferenceKind != "semantic_albums" {
		t.Fatalf("ResolvedReferenceKind = %q, want semantic_albums", resolved.ResolvedReferenceKind)
	}
	if resolved.Turn.ResultSetKind != "semantic_albums" {
		t.Fatalf("ResultSetKind = %q, want semantic_albums", resolved.Turn.ResultSetKind)
	}
}

func TestResolveTurnContextFallsBackWhenRequestedSetUnavailable(t *testing.T) {
	lastCreativeAlbumSet.mu.Lock()
	lastCreativeAlbumSet.sessions[normalizeChatSessionID("session-fallback-ref")] = creativeAlbumSetState{
		mode:      "underplayed_albums",
		queryText: "underplayed",
		updatedAt: time.Now().UTC().Add(-2 * time.Minute),
		candidates: []creativeAlbumCandidate{
			{Name: "Older Pick", ArtistName: "Artist A"},
		},
	}
	lastCreativeAlbumSet.mu.Unlock()
	lastSemanticAlbumSearch.mu.Lock()
	lastSemanticAlbumSearch.sessions[normalizeChatSessionID("session-fallback-ref")] = semanticAlbumSearchState{
		queryText: "dreamy albums",
		updatedAt: time.Now().UTC(),
		matches: []semanticAlbumSearchMatch{
			{Name: "Newer Pick", ArtistName: "Artist B"},
		},
	}
	lastSemanticAlbumSearch.mu.Unlock()

	resolved := resolveTurnContext("session-fallback-ref", normalizedTurn{
		Intent:          "album_discovery",
		SubIntent:       "result_set_play_recency",
		FollowupMode:    "query_previous_set",
		ReferenceTarget: "previous_results",
		ResultSetKind:   "discovered_albums",
		TimeWindow:      "this_year",
	})
	if resolved.ResolvedReferenceKind != "semantic_albums" {
		t.Fatalf("ResolvedReferenceKind = %q, want semantic_albums", resolved.ResolvedReferenceKind)
	}
	if resolved.Turn.ResultSetKind != "semantic_albums" {
		t.Fatalf("ResultSetKind = %q, want semantic_albums", resolved.Turn.ResultSetKind)
	}
}

func TestResolveTurnContextBindsLastFocusedItem(t *testing.T) {
	lastCreativeAlbumSet.mu.Lock()
	lastCreativeAlbumSet.sessions[normalizeChatSessionID("session-focused-item")] = creativeAlbumSetState{
		mode:      "underplayed_albums",
		queryText: "underplayed",
		updatedAt: time.Now().UTC(),
		candidates: []creativeAlbumCandidate{
			{Name: "Chosen Record", ArtistName: "Artist A"},
		},
	}
	lastCreativeAlbumSet.mu.Unlock()
	setLastFocusedResultItem("session-focused-item", "creative_albums", normalizedCreativeAlbumCandidateKey(creativeAlbumCandidate{
		Name:       "Chosen Record",
		ArtistName: "Artist A",
	}))

	resolved := resolveTurnContext("session-focused-item", normalizedTurn{
		Intent:             "listening",
		SubIntent:          "result_set_play_recency",
		FollowupMode:       "query_previous_set",
		ReferenceTarget:    "previous_results",
		ReferenceQualifier: "last_item",
		TimeWindow:         "this_year",
	})
	if resolved.ResolvedReferenceKind != "creative_albums" {
		t.Fatalf("ResolvedReferenceKind = %q, want creative_albums", resolved.ResolvedReferenceKind)
	}
	if resolved.ResolvedItemKey == "" {
		t.Fatal("expected focused item key to resolve")
	}
}

func TestResolveTurnContextDoesNotClarifyCreativeAndSemanticAlbumSets(t *testing.T) {
	lastCreativeAlbumSet.mu.Lock()
	lastCreativeAlbumSet.sessions[normalizeChatSessionID("session-equivalent-album-sets")] = creativeAlbumSetState{
		mode:      "semantic",
		queryText: "dream pop",
		updatedAt: time.Now().UTC(),
		candidates: []creativeAlbumCandidate{
			{Name: "Souvlaki", ArtistName: "Slowdive"},
		},
	}
	lastCreativeAlbumSet.mu.Unlock()
	lastSemanticAlbumSearch.mu.Lock()
	lastSemanticAlbumSearch.sessions[normalizeChatSessionID("session-equivalent-album-sets")] = semanticAlbumSearchState{
		queryText: "dream pop",
		updatedAt: time.Now().UTC(),
		matches: []semanticAlbumSearchMatch{
			{Name: "Souvlaki", ArtistName: "Slowdive"},
		},
	}
	lastSemanticAlbumSearch.mu.Unlock()

	resolved := resolveTurnContext("session-equivalent-album-sets", normalizedTurn{
		Intent:              "listening",
		SubIntent:           "result_set_play_recency",
		FollowupMode:        "query_previous_set",
		ReferenceTarget:     "previous_results",
		ReferenceQualifier:  "latest_set",
		NeedsClarification:  true,
		ClarificationFocus:  "reference",
		ClarificationPrompt: "Do you mean the latest album results, discovery results, or another recent set?",
		TimeWindow:          "ambiguous_recent",
	})
	if resolved.AmbiguousReference {
		t.Fatal("did not expect equivalent album result sets to remain ambiguous")
	}
	if resolved.Turn.NeedsClarification {
		t.Fatal("expected clarification to clear for equivalent album result sets")
	}
}

func TestBuildCleanupSelectionFromTurn(t *testing.T) {
	tests := []struct {
		name string
		turn normalizedTurn
		want string
	}{
		{name: "all", turn: normalizedTurn{SelectionMode: "all"}, want: "all"},
		{name: "top_n", turn: normalizedTurn{SelectionMode: "top_n", SelectionValue: "3"}, want: "first 3"},
		{name: "ordinal", turn: normalizedTurn{SelectionMode: "ordinal", SelectionValue: "2,4"}, want: "2,4"},
		{name: "explicit", turn: normalizedTurn{SelectionMode: "explicit_names", SelectionValue: "Moon Safari"}, want: "Moon Safari"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildCleanupSelectionFromTurn(tc.turn); got != tc.want {
				t.Fatalf("buildCleanupSelectionFromTurn() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildCleanupSelectionFromResolvedUsesFocusedItem(t *testing.T) {
	cleanupCandidates := []lidarrCleanupCandidate{
		{AlbumID: 11, ArtistName: "Air", Title: "Moon Safari"},
		{AlbumID: 22, ArtistName: "Broadcast", Title: "Tender Buttons"},
	}
	resolved := &resolvedTurnContext{
		Turn:               normalizedTurn{SelectionMode: "all"},
		ResolvedItemKey:    normalizedCleanupCandidateKey(cleanupCandidates[1]),
		ResolvedItemSource: "focused_item",
	}
	if got := buildCleanupSelectionFromResolved(resolved, "cleanup_candidates", cleanupCandidates, nil); got != "2" {
		t.Fatalf("buildCleanupSelectionFromResolved() = %q, want %q", got, "2")
	}
}

func TestBuildBadlyRatedCleanupSelectionFromResolvedUsesFocusedItem(t *testing.T) {
	badlyRated := []badlyRatedAlbumCandidate{
		{AlbumID: "11", ArtistName: "Air", AlbumName: "Moon Safari"},
		{AlbumID: "22", ArtistName: "Broadcast", AlbumName: "Tender Buttons"},
	}
	resolved := &resolvedTurnContext{
		Turn:               normalizedTurn{SelectionMode: "all"},
		ResolvedItemKey:    normalizedBadlyRatedAlbumCandidateKey(badlyRated[1]),
		ResolvedItemSource: "focused_item",
	}
	if got := buildCleanupSelectionFromResolved(resolved, "badly_rated_albums", nil, badlyRated); got != "2" {
		t.Fatalf("buildCleanupSelectionFromResolved() = %q, want %q", got, "2")
	}
}

func TestResolveFocusedItemRespectsKindTTL(t *testing.T) {
	setLastDiscoveredAlbums("session-focused-discovered-stale", "dream pop", []discoveredAlbumCandidate{
		{Rank: 1, ArtistName: "Air", AlbumTitle: "Moon Safari"},
	})
	lastFocusedResultItem.mu.Lock()
	lastFocusedResultItem.sessions[normalizeChatSessionID("session-focused-discovered-stale")] = focusedResultItemState{
		kind:      "cleanup_candidates",
		key:       "11",
		updatedAt: time.Now().UTC().Add(-llmContextCleanupTTL - time.Minute),
	}
	lastFocusedResultItem.mu.Unlock()

	resolved := resolveTurnContext("session-focused-discovered-stale", normalizedTurn{
		Intent:             "other",
		SubIntent:          "lidarr_cleanup_apply",
		FollowupMode:       "query_previous_set",
		ReferenceTarget:    "previous_results",
		ReferenceQualifier: "last_item",
		ResultSetKind:      "cleanup_candidates",
		ResultAction:       "preview_apply",
	})
	if resolved.ResolvedItemKey != "" {
		t.Fatalf("ResolvedItemKey = %q, want empty for stale focused item", resolved.ResolvedItemKey)
	}
}

func TestBuildServerTurnRequestUsesResolvedReferenceContract(t *testing.T) {
	libraryOnly := true
	req := buildServerTurnRequest(&resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:              "album_discovery",
			SubIntent:           "creative_refinement",
			FollowupMode:        "refine_previous",
			QueryScope:          "library",
			TimeWindow:          "last_month",
			Confidence:          "high",
			LibraryOnly:         &libraryOnly,
			NeedsClarification:  false,
			ClarificationFocus:  "none",
			ClarificationPrompt: "",
			StyleHints:          []string{"darker", "less polished"},
			TargetName:          "Late Nights",
			ArtistName:          "Air",
			PromptHint:          "colder tracks",
			ReferenceTarget:     "previous_results",
			ReferenceQualifier:  "last_item",
			ResultSetKind:       "semantic_albums",
			ResultAction:        "inspect_availability",
			SelectionMode:       "ordinal",
			SelectionValue:      "2",
		},
		ResolvedReferenceKind:   "discovered_albums",
		ResolvedReferenceSource: "resolver",
		ResolvedItemKey:         "2::air::moon safari",
		ResolvedItemSource:      "focused_item",
		HasDiscoveredAlbums:     true,
	})
	if req.Reference.RequestedSet != "semantic_albums" {
		t.Fatalf("RequestedSet = %q, want semantic_albums", req.Reference.RequestedSet)
	}
	if req.Reference.ResolvedSet != "discovered_albums" {
		t.Fatalf("ResolvedSet = %q, want discovered_albums", req.Reference.ResolvedSet)
	}
	if req.Reference.ResolvedItemKey != "2::air::moon safari" {
		t.Fatalf("ResolvedItemKey = %q", req.Reference.ResolvedItemKey)
	}
	if req.Workflow.Action != "inspect_availability" || req.Workflow.SelectionMode != "ordinal" || req.Workflow.SelectionValue != "2" {
		t.Fatalf("workflow = %#v", req.Workflow)
	}
	if !req.Session.HasDiscoveredAlbums {
		t.Fatal("expected discovered albums flag")
	}
}

func TestBuildResultSetResolverRequestIncludesCapabilities(t *testing.T) {
	req := buildResultSetResolverRequest(&resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:          "album_discovery",
			ReferenceTarget: "previous_results",
			ResultSetKind:   "discovered_albums",
			ResultAction:    "preview_apply",
			SelectionMode:   "ordinal",
			SelectionValue:  "2",
		},
		ResolvedReferenceKind: "discovered_albums",
	})
	if req.Turn.Intent != "album_discovery" {
		t.Fatalf("intent = %q", req.Turn.Intent)
	}
	if len(req.Capabilities) == 0 {
		t.Fatal("expected capabilities")
	}
	found := false
	for _, capability := range req.Capabilities {
		if capability.SetKind != "discovered_albums" {
			continue
		}
		found = true
		if len(capability.Operations) == 0 {
			t.Fatal("expected discovered album operations")
		}
	}
	if !found {
		t.Fatal("expected discovered_albums capability")
	}
}

func TestCurrentResultSetCapabilitiesIncludesPlaylistCandidates(t *testing.T) {
	capabilities := currentResultSetCapabilities()
	for _, capability := range capabilities {
		if capability.SetKind != "playlist_candidates" {
			continue
		}
		if len(capability.Operations) == 0 {
			t.Fatalf("playlist_candidates operations = %#v", capability.Operations)
		}
		foundAvailability := false
		foundRefine := false
		for _, op := range capability.Operations {
			switch op {
			case "inspect_availability":
				foundAvailability = true
			case "refine_style":
				foundRefine = true
			}
		}
		if !foundAvailability || !foundRefine {
			t.Fatalf("playlist_candidates operations = %#v, want inspect_availability and refine_style", capability.Operations)
		}
		return
	}
	t.Fatal("expected playlist_candidates capability")
}

func TestBuildServerExecutionRequestUsesResolverDecision(t *testing.T) {
	req := buildServerExecutionRequest(&resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:         "playlist",
			TargetName:     "Late Nights",
			ArtistName:     "Air",
			PromptHint:     "colder tracks",
			TimeWindow:     "this_month",
			ResultAction:   "inspect_availability",
			SelectionMode:  "all",
			SelectionValue: "",
		},
		ResolvedReferenceKind: "playlist_candidates",
		ResolvedItemKey:       "track:123",
	}, resultSetResolverDecision{
		SetKind:        "playlist_candidates",
		Operation:      "inspect_availability",
		SelectionMode:  "top_n",
		SelectionValue: "12",
		ItemKey:        "track:456",
	})
	if req.Domain != "playlist" {
		t.Fatalf("Domain = %q", req.Domain)
	}
	if req.SetKind != "playlist_candidates" {
		t.Fatalf("SetKind = %q", req.SetKind)
	}
	if req.Operation != "inspect_availability" {
		t.Fatalf("Operation = %q", req.Operation)
	}
	if req.SelectionMode != "top_n" || req.SelectionValue != "12" {
		t.Fatalf("selection = (%q, %q)", req.SelectionMode, req.SelectionValue)
	}
	if req.ItemKey != "track:456" {
		t.Fatalf("ItemKey = %q", req.ItemKey)
	}
	if req.TargetName != "Late Nights" || req.ArtistName != "Air" || req.PromptHint != "colder tracks" || req.TimeWindow != "this_month" {
		t.Fatalf("request = %#v", req)
	}
}

func TestBuildStructuredCreativeLibraryQueryUsesPromptAndStyleHints(t *testing.T) {
	got := buildStructuredCreativeLibraryQuery(normalizedTurn{
		Intent:     "album_discovery",
		QueryScope: "library",
		PromptHint: "for a wet-city commute. Keep it moody, not sleepy.",
		StyleHints: []string{"moody", "not sleepy"},
		Confidence: "high",
	})
	want := "for a wet-city commute. Keep it moody, not sleepy"
	if got != want {
		t.Fatalf("buildStructuredCreativeLibraryQuery() = %q, want %q", got, want)
	}
}

func TestNormalizedTimeWindowLabel(t *testing.T) {
	if got := normalizedTimeWindowLabel("this_month"); got != "this month" {
		t.Fatalf("normalizedTimeWindowLabel(this_month) = %q", got)
	}
	if got := normalizedTimeWindowLabel("last_month"); got != "in the last month" {
		t.Fatalf("normalizedTimeWindowLabel(last_month) = %q", got)
	}
}

func TestRenderNormalizedArtistDominance(t *testing.T) {
	items := []artistListeningStatItem{
		{ArtistName: "Radiohead", AlbumCount: 11, PlaysInWindow: 205},
		{ArtistName: "Pink Floyd", AlbumCount: 16, PlaysInWindow: 59},
	}
	got := renderNormalizedArtistDominance(items, normalizedTurn{
		SubIntent:  "artist_dominance",
		TimeWindow: "this_month",
	})
	if !strings.Contains(got, "Radiohead is ahead this month with 205 plays") {
		t.Fatalf("renderNormalizedArtistDominance() = %q", got)
	}
}

func TestSanitizeResolverDecisionFallsBackToSupportedCapability(t *testing.T) {
	request := resultSetResolverRequest{
		Turn: serverTurnRequest{
			Intent: "album_discovery",
			Reference: serverTurnReference{
				RequestedSet: "discovered_albums",
				ResolvedSet:  "discovered_albums",
			},
			Workflow: serverTurnWorkflow{
				Action:         "preview_apply",
				SelectionMode:  "ordinal",
				SelectionValue: "2",
			},
			Confidence: "high",
		},
		Capabilities: currentResultSetCapabilities(),
	}
	fallback := resultSetResolverDecision{
		SetKind:        "discovered_albums",
		Operation:      "preview_apply",
		SelectionMode:  "ordinal",
		SelectionValue: "2",
		Confidence:     "high",
		Reason:         "structured_passthrough",
	}

	got := sanitizeResolverDecision(resultSetResolverDecision{
		SetKind:        "playlist_candidates",
		Operation:      "delete_everything",
		SelectionMode:  "wildcard",
		SelectionValue: "oops",
		Confidence:     "high",
	}, request, fallback)

	if got.SetKind != "discovered_albums" {
		t.Fatalf("SetKind = %q, want discovered_albums", got.SetKind)
	}
	if got.Operation != "preview_apply" {
		t.Fatalf("Operation = %q, want preview_apply", got.Operation)
	}
	if got.SelectionMode != "ordinal" || got.SelectionValue != "2" {
		t.Fatalf("selection = (%q, %q), want (ordinal, 2)", got.SelectionMode, got.SelectionValue)
	}
}

func TestSanitizeResolverDecisionPreservesCompareAction(t *testing.T) {
	request := resultSetResolverRequest{
		Turn: serverTurnRequest{
			Intent: "track_discovery",
			Reference: serverTurnReference{
				RequestedSet: "track_candidates",
				ResolvedSet:  "track_candidates",
				Qualifier:    "safer",
			},
			Workflow: serverTurnWorkflow{
				Action:                "compare",
				SelectionMode:         "all",
				CompareSelectionMode:  "ordinal",
				CompareSelectionValue: "1",
			},
			Confidence: "high",
		},
		Capabilities: currentResultSetCapabilities(),
	}
	fallback := resultSetResolverDecision{
		SetKind:               "track_candidates",
		Operation:             "compare",
		SelectionMode:         "all",
		CompareSelectionMode:  "ordinal",
		CompareSelectionValue: "1",
		Confidence:            "high",
		Reason:                "structured_passthrough",
	}

	got := sanitizeResolverDecision(resultSetResolverDecision{
		SetKind:        "track_candidates",
		Operation:      "describe_item",
		SelectionMode:  "ordinal",
		SelectionValue: "1",
		Confidence:     "high",
	}, request, fallback)

	if got.Operation != "compare" {
		t.Fatalf("Operation = %q, want compare", got.Operation)
	}
	if got.CompareSelectionMode != "ordinal" || got.CompareSelectionValue != "1" {
		t.Fatalf("compare selection = (%q, %q)", got.CompareSelectionMode, got.CompareSelectionValue)
	}
}

func TestSanitizeResolverDecisionPrefersItemKeySelectorWhenSupported(t *testing.T) {
	request := resultSetResolverRequest{
		Turn: serverTurnRequest{
			Intent: "album_discovery",
			Reference: serverTurnReference{
				RequestedSet:    "discovered_albums",
				ResolvedSet:     "discovered_albums",
				ResolvedItemKey: "1::air::moon safari",
			},
			Workflow: serverTurnWorkflow{
				Action: "inspect_availability",
			},
			Confidence: "high",
		},
		Capabilities: currentResultSetCapabilities(),
	}
	fallback := resultSetResolverDecision{
		SetKind:       "discovered_albums",
		ItemKey:       "1::air::moon safari",
		Operation:     "inspect_availability",
		SelectionMode: "all",
		Confidence:    "high",
		Reason:        "structured_passthrough",
	}

	got := sanitizeResolverDecision(resultSetResolverDecision{
		SetKind:    "discovered_albums",
		ItemKey:    "1::air::moon safari",
		Operation:  "inspect_availability",
		Confidence: "high",
	}, request, fallback)

	if got.SelectionMode != "item_key" {
		t.Fatalf("SelectionMode = %q, want item_key", got.SelectionMode)
	}
	if got.ItemKey != "1::air::moon safari" {
		t.Fatalf("ItemKey = %q", got.ItemKey)
	}
	if got.SelectionValue != "" {
		t.Fatalf("SelectionValue = %q, want empty", got.SelectionValue)
	}
}

func TestTryNormalizedIntentRouteHandlesTrackCandidateCompare(t *testing.T) {
	sessionID := "route-track-compare"
	setLastTrackCandidateSet(sessionID, "similar_tracks", "Windowlicker", []trackCandidate{
		{ID: "1", Title: "Matador", ArtistName: "Arctic Monkeys", PlayCount: 1, Score: 0.92},
		{ID: "2", Title: "Doll", ArtistName: "Foo Fighters", PlayCount: 8, Score: 0.83},
		{ID: "3", Title: "Gold", ArtistName: "Imagine Dragons", PlayCount: 3, Score: 0.82},
	})

	srv := &Server{}
	ctx := context.WithValue(context.Background(), chatSessionKey, sessionID)
	resolved := &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:                "track_discovery",
			SubIntent:             "compare",
			FollowupMode:          "refine_previous",
			ReferenceTarget:       "previous_results",
			ReferenceQualifier:    "safer",
			ResultSetKind:         "track_candidates",
			ResultAction:          "compare",
			CompareSelectionMode:  "ordinal",
			CompareSelectionValue: "1",
		},
		ResolvedReferenceKind: "track_candidates",
	}

	resp, ok := srv.tryNormalizedIntentRoute(ctx, "Compare the safer one to the first.", nil, resolved)
	if !ok {
		t.Fatal("tryNormalizedIntentRoute() = false, want true")
	}
	if !strings.Contains(resp.Response, "Selected anchor: Doll by Foo Fighters") {
		t.Fatalf("response = %q", resp.Response)
	}
	if !strings.Contains(resp.Response, "comparison target: Matador by Arctic Monkeys") {
		t.Fatalf("response = %q", resp.Response)
	}
}

func TestTryNormalizedIntentRouteHandlesTrackCandidateCompareWithExplicitPrimarySelection(t *testing.T) {
	sessionID := "route-track-compare-primary-selection"
	setLastTrackCandidateSet(sessionID, "similar_tracks", "Windowlicker", []trackCandidate{
		{ID: "1", Title: "Matador", ArtistName: "Arctic Monkeys", PlayCount: 1, Score: 0.92},
		{ID: "2", Title: "Doll", ArtistName: "Foo Fighters", PlayCount: 8, Score: 0.83},
		{ID: "3", Title: "Gold", ArtistName: "Imagine Dragons", PlayCount: 3, Score: 0.82},
	})

	srv := &Server{}
	ctx := context.WithValue(context.Background(), chatSessionKey, sessionID)
	resolved := &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:                "track_discovery",
			SubIntent:             "compare",
			FollowupMode:          "refine_previous",
			ReferenceTarget:       "previous_results",
			ResultSetKind:         "track_candidates",
			ResultAction:          "compare",
			SelectionMode:         "ordinal",
			SelectionValue:        "2",
			CompareSelectionMode:  "ordinal",
			CompareSelectionValue: "1",
		},
		ResolvedReferenceKind: "track_candidates",
	}

	resp, ok := srv.tryNormalizedIntentRoute(ctx, "Compare the second one to the first.", nil, resolved)
	if !ok {
		t.Fatal("tryNormalizedIntentRoute() = false, want true")
	}
	if !strings.Contains(resp.Response, "Selected anchor: Doll by Foo Fighters") {
		t.Fatalf("response = %q", resp.Response)
	}
	if !strings.Contains(resp.Response, "comparison target: Matador by Arctic Monkeys") {
		t.Fatalf("response = %q", resp.Response)
	}
}

func TestTryNormalizedIntentRouteHandlesTrackCandidateCompareWhenPrimaryMatchesComparisonTarget(t *testing.T) {
	sessionID := "route-track-compare-same"
	setLastTrackCandidateSet(sessionID, "similar_tracks", "Windowlicker", []trackCandidate{
		{ID: "1", Title: "Balaclava", ArtistName: "Arctic Monkeys", PlayCount: 0, Score: 0.91},
		{ID: "2", Title: "Doll", ArtistName: "Foo Fighters", PlayCount: 0, Score: 0.83},
		{ID: "3", Title: "Gold", ArtistName: "Imagine Dragons", PlayCount: 0, Score: 0.82},
	})

	srv := &Server{}
	ctx := context.WithValue(context.Background(), chatSessionKey, sessionID)
	resolved := &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:                "track_discovery",
			FollowupMode:          "refine_previous",
			ReferenceTarget:       "previous_results",
			ReferenceQualifier:    "safer",
			ResultSetKind:         "track_candidates",
			ResultAction:          "compare",
			SelectionMode:         "all",
			CompareSelectionMode:  "ordinal",
			CompareSelectionValue: "1",
		},
		ResolvedReferenceKind: "track_candidates",
	}

	resp, ok := srv.tryNormalizedIntentRoute(ctx, "Compare the safer one to the first.", nil, resolved)
	if !ok {
		t.Fatal("tryNormalizedIntentRoute() = false, want true")
	}
	if !strings.Contains(resp.Response, "already the first result") {
		t.Fatalf("response = %q", resp.Response)
	}
}

func TestTryNormalizedIntentRouteHandlesArtistCandidateCompare(t *testing.T) {
	sessionID := "route-artist-compare"
	setLastArtistCandidateSet(sessionID, "Radiohead", []artistCandidate{
		{ID: "1", Name: "Blur", PlayCount: 2, Rating: 6, Score: 0.91},
		{ID: "2", Name: "Coldplay", PlayCount: 9, Rating: 8, Score: 0.87},
		{ID: "3", Name: "Elbow", PlayCount: 4, Rating: 7, Score: 0.84},
	})

	srv := &Server{}
	ctx := context.WithValue(context.Background(), chatSessionKey, sessionID)
	resolved := &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:                "artist_discovery",
			SubIntent:             "compare",
			FollowupMode:          "refine_previous",
			ReferenceTarget:       "previous_results",
			ReferenceQualifier:    "safer",
			ResultSetKind:         "artist_candidates",
			ResultAction:          "compare",
			CompareSelectionMode:  "ordinal",
			CompareSelectionValue: "1",
		},
		ResolvedReferenceKind: "artist_candidates",
	}

	resp, ok := srv.tryNormalizedIntentRoute(ctx, "Compare the safer one to the first.", nil, resolved)
	if !ok {
		t.Fatal("tryNormalizedIntentRoute() = false, want true")
	}
	if !strings.Contains(resp.Response, "Selected anchor: Coldplay") {
		t.Fatalf("response = %q", resp.Response)
	}
	if !strings.Contains(resp.Response, "comparison target: Blur") {
		t.Fatalf("response = %q", resp.Response)
	}
}

func TestTryNormalizedIntentRouteHandlesArtistCandidateCompareWithFocusedPrimaryItem(t *testing.T) {
	sessionID := "route-artist-compare-focused"
	setLastArtistCandidateSet(sessionID, "Radiohead", []artistCandidate{
		{ID: "1", Name: "Blur", PlayCount: 2, Rating: 6, Score: 0.91},
		{ID: "2", Name: "Coldplay", PlayCount: 9, Rating: 8, Score: 0.87},
		{ID: "3", Name: "Elbow", PlayCount: 4, Rating: 7, Score: 0.84},
	})
	setLastFocusedResultItem(sessionID, "artist_candidates", normalizedArtistCandidateKey(artistCandidate{ID: "3", Name: "Elbow"}))

	srv := &Server{}
	ctx := context.WithValue(context.Background(), chatSessionKey, sessionID)
	resolved := &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:                "artist_discovery",
			SubIntent:             "compare",
			FollowupMode:          "refine_previous",
			ReferenceTarget:       "previous_results",
			ReferenceQualifier:    "last_item",
			ResultSetKind:         "artist_candidates",
			ResultAction:          "compare",
			CompareSelectionMode:  "ordinal",
			CompareSelectionValue: "1",
		},
		ResolvedReferenceKind: "artist_candidates",
		ResolvedItemKey:       normalizedArtistCandidateKey(artistCandidate{ID: "3", Name: "Elbow"}),
	}

	resp, ok := srv.tryNormalizedIntentRoute(ctx, "Compare that one to the first.", nil, resolved)
	if !ok {
		t.Fatal("tryNormalizedIntentRoute() = false, want true")
	}
	if !strings.Contains(resp.Response, "Selected anchor: Elbow") {
		t.Fatalf("response = %q", resp.Response)
	}
	if !strings.Contains(resp.Response, "comparison target: Blur") {
		t.Fatalf("response = %q", resp.Response)
	}
}

func TestTryTurnIntentRouteHandlesTrackCandidateCompareViaConversationObject(t *testing.T) {
	sessionID := "route-track-object-compare"
	setLastTrackCandidateSet(sessionID, "similar_tracks", "Windowlicker", []trackCandidate{
		{ID: "1", Title: "Balaclava", ArtistName: "Arctic Monkeys", Score: 0.91},
		{ID: "2", Title: "Doll", ArtistName: "Foo Fighters", Score: 0.83},
	})

	srv := &Server{}
	ctx := context.WithValue(context.Background(), chatSessionKey, sessionID)
	resp, ok := srv.tryTurnIntentRoute(ctx, &Turn{
		SessionID:   sessionID,
		UserMessage: "Compare the second one to the first.",
		Normalized: TurnNormalized{
			Intent:                "other",
			SubIntent:             "compare",
			ConversationOp:        "compare",
			FollowupMode:          "query_previous_set",
			QueryScope:            "library",
			ResultAction:          "compare",
			SelectionMode:         "ordinal",
			SelectionValue:        "2",
			CompareSelectionMode:  "ordinal",
			CompareSelectionValue: "1",
			ReferenceTarget:       "previous_results",
		},
		Reference: TurnReference{
			ObjectType:         "result_set",
			ObjectKind:         "track_candidates",
			ObjectStatus:       "result_set",
			ObjectIntent:       "track_discovery",
			ObjectTarget:       "previous_results",
			HasTrackCandidates: true,
		},
	}, nil)
	if !ok {
		t.Fatal("expected conversation object track compare to resolve")
	}
	if !strings.Contains(resp.Response, "Selected anchor: Doll by Foo Fighters") {
		t.Fatalf("response = %q", resp.Response)
	}
	if !strings.Contains(resp.Response, "comparison target: Balaclava by Arctic Monkeys") {
		t.Fatalf("response = %q", resp.Response)
	}
}

func TestTryTurnIntentRouteHandlesArtistCandidateCompareViaConversationObject(t *testing.T) {
	sessionID := "route-artist-object-compare"
	setLastArtistCandidateSet(sessionID, "Radiohead", []artistCandidate{
		{ID: "1", Name: "Blur", Score: 0.91},
		{ID: "2", Name: "Coldplay", Score: 0.87},
	})

	srv := &Server{}
	ctx := context.WithValue(context.Background(), chatSessionKey, sessionID)
	resp, ok := srv.tryTurnIntentRoute(ctx, &Turn{
		SessionID:   sessionID,
		UserMessage: "Compare the second one to the first.",
		Normalized: TurnNormalized{
			Intent:                "general_chat",
			SubIntent:             "compare",
			ConversationOp:        "compare",
			FollowupMode:          "query_previous_set",
			QueryScope:            "library",
			ResultAction:          "compare",
			SelectionMode:         "ordinal",
			SelectionValue:        "2",
			CompareSelectionMode:  "ordinal",
			CompareSelectionValue: "1",
			ReferenceTarget:       "previous_results",
		},
		Reference: TurnReference{
			ObjectType:          "result_set",
			ObjectKind:          "artist_candidates",
			ObjectStatus:        "result_set",
			ObjectIntent:        "artist_discovery",
			ObjectTarget:        "previous_results",
			HasArtistCandidates: true,
		},
	}, nil)
	if !ok {
		t.Fatal("expected conversation object artist compare to resolve")
	}
	if !strings.Contains(resp.Response, "Selected anchor: Coldplay") {
		t.Fatalf("response = %q", resp.Response)
	}
	if !strings.Contains(resp.Response, "comparison target: Blur") {
		t.Fatalf("response = %q", resp.Response)
	}
}

func TestTryTurnIntentRouteHandlesArtistCandidateRevisitViaConversationObjectWithListeningIntent(t *testing.T) {
	originalToolRunner := executeToolWithSimilarityImpl
	executeToolWithSimilarityImpl = func(_ context.Context, _ *graph.Resolver, _ *similarity.Service, _ string, tool string, args map[string]interface{}) (string, error) {
		if tool != "albums" {
			t.Fatalf("tool = %q, want albums", tool)
		}
		artistName := strings.TrimSpace(args["artistName"].(string))
		switch artistName {
		case "Radiohead":
			return `{"data":{"albums":[{"name":"OK Computer OKNOTOK","artistName":"Radiohead","year":1997}]}}`, nil
		case "Pink Floyd":
			return `{"data":{"albums":[{"name":"The Wall","artistName":"Pink Floyd","year":1979}]}}`, nil
		default:
			t.Fatalf("unexpected artist lookup args: %#v", args)
			return "", nil
		}
	}
	defer func() {
		executeToolWithSimilarityImpl = originalToolRunner
	}()

	sessionID := "route-artist-object-revisit-listening"
	setLastArtistCandidateSet(sessionID, "top artists last month", []artistCandidate{
		{ID: "1", Name: "Radiohead", Score: 0.91},
		{ID: "2", Name: "Pink Floyd", Score: 0.87},
	})

	srv := &Server{resolver: &graph.Resolver{}}
	ctx := context.WithValue(context.Background(), chatSessionKey, sessionID)
	resp, ok := srv.tryTurnIntentRoute(ctx, &Turn{
		SessionID:   sessionID,
		UserMessage: "From those, give me two to revisit tonight.",
		Normalized: TurnNormalized{
			Intent:          "listening",
			SubIntent:       "listening_summary",
			ConversationOp:  "select",
			FollowupMode:    "query_previous_set",
			QueryScope:      "library",
			SelectionMode:   "top_n",
			SelectionValue:  "2",
			ReferenceTarget: "previous_results",
		},
		Reference: TurnReference{
			ObjectType:          "result_set",
			ObjectKind:          "artist_candidates",
			ObjectStatus:        "result_set",
			ObjectIntent:        "stats",
			ObjectTarget:        "previous_results",
			HasArtistCandidates: true,
		},
	}, nil)
	if !ok {
		t.Fatal("expected conversation object artist revisit to resolve")
	}
	if !strings.Contains(resp.Response, "OK Computer OKNOTOK by Radiohead") {
		t.Fatalf("response = %q", resp.Response)
	}
	if !strings.Contains(resp.Response, "The Wall by Pink Floyd") {
		t.Fatalf("response = %q", resp.Response)
	}
}

func TestSelectAlbumIDsFromCandidatesSupportsOrdinalSelection(t *testing.T) {
	candidates := []lidarrCleanupCandidate{
		{AlbumID: 11, Title: "A"},
		{AlbumID: 22, Title: "B"},
		{AlbumID: 33, Title: "C"},
	}
	ids, err := selectAlbumIDsFromCandidates("2,3", candidates)
	if err != nil {
		t.Fatalf("selectAlbumIDsFromCandidates() error = %v", err)
	}
	if len(ids) != 2 || ids[0] != 22 || ids[1] != 33 {
		t.Fatalf("selectAlbumIDsFromCandidates() = %#v, want [22 33]", ids)
	}
}

func TestResolveStructuredPlaylistTargetFromHistory(t *testing.T) {
	srv := &Server{}
	history := []agent.Message{
		{Role: "assistant", Content: "Playlist \"Late Nights\" currently has:\n- Nude by Radiohead"},
	}
	name, ok := srv.resolveStructuredPlaylistTarget(context.Background(), normalizedTurn{
		Intent:          "playlist",
		SubIntent:       "playlist_tracks_query",
		ReferenceTarget: "previous_playlist",
	}, history)
	if !ok || name != "Late Nights" {
		t.Fatalf("resolveStructuredPlaylistTarget() = (%q, %v), want (%q, true)", name, ok, "Late Nights")
	}
}

func TestTryNormalizedPlaylistCreateSkipsInventorySubIntent(t *testing.T) {
	srv := &Server{}
	resp, ok := srv.tryNormalizedPlaylistCreate(context.Background(), "What playlists do I have?", &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:    "playlist",
			SubIntent: "playlist_inventory",
		},
	})
	if ok || resp.Response != "" {
		t.Fatalf("tryNormalizedPlaylistCreate() = (%#v, %v), want no route", resp, ok)
	}
}

func TestSanitizeNormalizedTurnNormalizesPreviousPlaylistReference(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		turn normalizedTurn
	}{
		{
			name: "refresh",
			msg:  "Refresh that playlist.",
			turn: normalizedTurn{
				Intent:          "playlist",
				SubIntent:       "playlist_refresh",
				FollowupMode:    "query_previous_set",
				ReferenceTarget: "none",
			},
		},
		{
			name: "queue request",
			msg:  "Queue the missing tracks from that playlist.",
			turn: normalizedTurn{
				Intent:          "playlist",
				SubIntent:       "playlist_queue_request",
				FollowupMode:    "query_previous_set",
				ReferenceTarget: "none",
			},
		},
		{
			name: "vibe followup",
			msg:  "What's the vibe of that playlist?",
			turn: normalizedTurn{
				Intent:          "playlist",
				SubIntent:       "playlist_vibe",
				FollowupMode:    "query_previous_set",
				ReferenceTarget: "none",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			turn := sanitizeNormalizedTurn(tc.msg, tc.turn)
			if turn.ReferenceTarget != "previous_playlist" {
				t.Fatalf("ReferenceTarget = %q, want previous_playlist", turn.ReferenceTarget)
			}
		})
	}
}

func TestSanitizeNormalizedTurnDefaultsRecentResultSetWindow(t *testing.T) {
	turn := sanitizeNormalizedTurn("Which of those have I played recently?", normalizedTurn{
		Intent:          "album_discovery",
		SubIntent:       "result_set_play_recency",
		FollowupMode:    "query_previous_set",
		ReferenceTarget: "previous_results",
	})
	if turn.TimeWindow != "last_month" {
		t.Fatalf("TimeWindow = %q, want last_month", turn.TimeWindow)
	}
}

func TestShouldAttemptPlaylistCreateTurn(t *testing.T) {
	if !shouldAttemptPlaylistCreateTurn(&Turn{
		UserMessage: "Create a melancholy jazz playlist for late nights.",
		Normalized: TurnNormalized{
			Intent:    "playlist",
			SubIntent: "playlist_vibe",
		},
	}) {
		t.Fatal("expected playlist create route to be attempted")
	}
	if shouldAttemptPlaylistCreateTurn(&Turn{
		UserMessage: "What's the vibe of that playlist?",
		Normalized: TurnNormalized{
			Intent:          "playlist",
			SubIntent:       "playlist_vibe",
			FollowupMode:    "query_previous_set",
			ReferenceTarget: "previous_playlist",
		},
	}) {
		t.Fatal("did not expect saved playlist vibe follow-up to route as create")
	}
	if !shouldAttemptPlaylistCreateTurn(&Turn{
		UserMessage: "Make me a 12-track playlist for a foggy midnight drive.",
		Normalized: TurnNormalized{
			Intent:       "playlist",
			SubIntent:    "playlist_append",
			FollowupMode: "none",
		},
	}) {
		t.Fatal("expected target-less playlist_append normalization to still route as create")
	}
	if !shouldAttemptPlaylistCreateTurn(&Turn{
		UserMessage: "Make me a 12-track playlist for a foggy midnight drive.",
		Normalized: TurnNormalized{
			Intent:       "playlist",
			SubIntent:    "playlist_append",
			FollowupMode: "none",
			TargetName:   "midnight drive",
		},
	}) {
		t.Fatal("expected create wording to override noisy playlist target extraction")
	}
}

func TestPlaylistDraftFocusTurnCarriesPlaylistObjectMetadata(t *testing.T) {
	turn := &Turn{
		UserMessage: "Make me a 12-track playlist for a foggy midnight drive.",
		Normalized: TurnNormalized{
			Intent:          "playlist",
			SubIntent:       "playlist_vibe",
			StyleHints:      []string{"foggy", "midnight", "drive"},
			FollowupMode:    "none",
			QueryScope:      "general",
			ResultAction:    "none",
			SelectionMode:   "none",
			ReferenceTarget: "none",
			Confidence:      "high",
		},
	}

	got := playlistDraftFocusTurn(turn, "12-track playlist for a foggy midnight drive")
	if got.Intent != "playlist" {
		t.Fatalf("Intent = %q, want playlist", got.Intent)
	}
	if got.SubIntent != "playlist_vibe" {
		t.Fatalf("SubIntent = %q, want playlist_vibe", got.SubIntent)
	}
	if got.ResultSetKind != "playlist_candidates" {
		t.Fatalf("ResultSetKind = %q, want playlist_candidates", got.ResultSetKind)
	}
	if got.ReferenceTarget != "previous_playlist" {
		t.Fatalf("ReferenceTarget = %q, want previous_playlist", got.ReferenceTarget)
	}
	if got.PromptHint != "12-track playlist for a foggy midnight drive" {
		t.Fatalf("PromptHint = %q", got.PromptHint)
	}
}

func TestSanitizeOrchestrationDecisionBiasesFirstTurnPlaylistCreateDespiteNoisySubIntent(t *testing.T) {
	decision := sanitizeOrchestrationDecision(orchestrationDecision{
		NextStage: "responder",
	}, &resolvedTurnContext{
		Turn: normalizedTurn{
			Intent:       "playlist",
			SubIntent:    "playlist_append",
			FollowupMode: "none",
			QueryScope:   "general",
			PromptHint:   "12-track foggy midnight drive",
			StyleHints:   []string{"foggy", "midnight", "drive"},
			Confidence:   "high",
		},
	})
	if decision.NextStage != "deterministic" {
		t.Fatalf("NextStage = %q, want deterministic", decision.NextStage)
	}
	if decision.DeterministicMode != "normalized_first" {
		t.Fatalf("DeterministicMode = %q, want normalized_first", decision.DeterministicMode)
	}
}

func TestStructuredPlaylistTrackCount(t *testing.T) {
	count := structuredPlaylistTrackCount(normalizedTurn{
		SelectionMode:  "top_n",
		SelectionValue: "7",
	}, 5)
	if count != 7 {
		t.Fatalf("structuredPlaylistTrackCount() = %d, want 7", count)
	}
}
