package main

import (
	"strings"
	"testing"
	"time"
)

func TestFormatPendingActionContext(t *testing.T) {
	now := time.Date(2026, time.March, 7, 16, 0, 0, 0, time.UTC)
	action := &PendingAction{
		Kind:      "artist_remove",
		Title:     "Remove artist",
		Summary:   `Remove "M83" from Lidarr.`,
		Details:   []string{"Artist: M83"},
		ExpiresAt: now.Add(10 * time.Minute).Format(time.RFC3339),
	}

	got := formatPendingActionContext(action, now)
	if !strings.Contains(got, `kind=artist_remove`) {
		t.Fatalf("formatPendingActionContext() = %q, want kind", got)
	}
	if !strings.Contains(got, `summary="Remove \"M83\" from Lidarr."`) {
		t.Fatalf("formatPendingActionContext() = %q, want summary", got)
	}
}

func TestFormatDiscoveredAlbumsContextSkipsStale(t *testing.T) {
	now := time.Date(2026, time.March, 7, 16, 0, 0, 0, time.UTC)
	got := formatDiscoveredAlbumsContext(
		"best pink floyd albums",
		now.Add(-llmContextDiscoveredAlbumsTTL-time.Minute),
		[]discoveredAlbumCandidate{{AlbumTitle: "The Wall", ArtistName: "Pink Floyd", Year: 1979}},
		now,
	)
	if got != "" {
		t.Fatalf("formatDiscoveredAlbumsContext() = %q, want empty for stale data", got)
	}
}

func TestFormatLidarrCleanupContextIncludesCounts(t *testing.T) {
	now := time.Date(2026, time.March, 7, 16, 0, 0, 0, time.UTC)
	got := formatLidarrCleanupContext(
		now.Add(-5*time.Minute),
		[]lidarrCleanupCandidate{
			{Title: "Moon Safari", ArtistName: "Air", Reason: "missing_files", RecommendedAction: "search_missing"},
			{Title: "Talkie Walkie", ArtistName: "Air", Reason: "missing_files", RecommendedAction: "search_missing"},
		},
		now,
	)
	if !strings.Contains(got, "count=2") {
		t.Fatalf("formatLidarrCleanupContext() = %q, want count", got)
	}
	if !strings.Contains(got, "recommended_action=search_missing") {
		t.Fatalf("formatLidarrCleanupContext() = %q, want action", got)
	}
}

func TestFormatPlaylistContextIncludesResolvedCounts(t *testing.T) {
	now := time.Date(2026, time.March, 7, 16, 0, 0, 0, time.UTC)
	got := formatPlaylistContext(
		"late night ambient",
		"Late Night Ambient",
		now.Add(-5*time.Minute),
		[]playlistCandidateTrack{
			{ArtistName: "Air", TrackTitle: "La Femme d'Argent"},
			{ArtistName: "Boards of Canada", TrackTitle: "Dayvan Cowboy"},
		},
		now.Add(-2*time.Minute),
		[]resolvedPlaylistTrack{
			{Status: "available"},
			{Status: "missing"},
		},
		now,
	)
	if !strings.Contains(got, `name="Late Night Ambient"`) {
		t.Fatalf("formatPlaylistContext() = %q, want playlist name", got)
	}
	if !strings.Contains(got, "resolved_counts=1/1/0/0") {
		t.Fatalf("formatPlaylistContext() = %q, want resolved counts", got)
	}
}

func TestFormatSemanticAlbumSearchContextIncludesRecentMatches(t *testing.T) {
	now := time.Date(2026, time.March, 19, 2, 45, 0, 0, time.UTC)
	got := formatSemanticAlbumSearchContext(
		"melancholic dream pop",
		now.Add(-5*time.Minute),
		[]semanticAlbumSearchMatch{
			{Name: "Life Of Leisure", ArtistName: "Washed Out", Year: 2009},
			{Name: "Lust for Life", ArtistName: "Lana Del Rey", Year: 2017, PlayCount: 1, LastPlayed: "2026-02-25T20:41:45Z"},
			{Name: "Ultraviolence", ArtistName: "Lana Del Rey", Year: 2014, PlayCount: 3, LastPlayed: "2026-03-06T12:28:13Z"},
		},
		now,
	)
	required := []string{
		`last_semantic_album_search: query="melancholic dream pop"`,
		`count=3`,
		`sample="Life Of Leisure by Washed Out (2009) | Lust for Life by Lana Del Rey (2017) | Ultraviolence by Lana Del Rey (2014)"`,
		`recent_matches="Lust for Life by Lana Del Rey (2017) [plays=1, last_played=2026-02-25T20:41:45Z] | Ultraviolence by Lana Del Rey (2014) [plays=3, last_played=2026-03-06T12:28:13Z]"`,
	}
	for _, fragment := range required {
		if !strings.Contains(got, fragment) {
			t.Fatalf("formatSemanticAlbumSearchContext() = %q, missing %q", got, fragment)
		}
	}
}

func TestBuildLLMSessionContextIncludesBadlyRatedAlbums(t *testing.T) {
	lastBadlyRatedAlbums.mu.Lock()
	lastBadlyRatedAlbums.sessions = make(map[string]badlyRatedAlbumsState)
	lastBadlyRatedAlbums.mu.Unlock()

	setLastBadlyRatedAlbums("session-bad", []badlyRatedAlbumCandidate{
		{AlbumName: "Dummy", ArtistName: "Artist", BadTrackCount: 2},
	})

	srv := &Server{}
	got := srv.buildLLMSessionContext("session-bad")
	if !strings.Contains(got, "last_badly_rated_albums") {
		t.Fatalf("buildLLMSessionContext() = %q", got)
	}
}

func TestBuildLLMSessionContextIncludesSemanticAlbumSearch(t *testing.T) {
	lastSemanticAlbumSearch.mu.Lock()
	lastSemanticAlbumSearch.sessions = make(map[string]semanticAlbumSearchState)
	lastSemanticAlbumSearch.mu.Unlock()

	setLastSemanticAlbumSearch("session-semantic", "melancholic dream pop", []semanticAlbumSearchMatch{
		{Name: "Ultraviolence", ArtistName: "Lana Del Rey", Year: 2014, PlayCount: 3, LastPlayed: "2026-03-06T12:28:13Z"},
	})

	srv := &Server{}
	got := srv.buildLLMSessionContext("session-semantic")
	if !strings.Contains(got, "last_semantic_album_search") {
		t.Fatalf("buildLLMSessionContext() = %q", got)
	}
	if !strings.Contains(got, "Ultraviolence by Lana Del Rey (2014)") {
		t.Fatalf("buildLLMSessionContext() = %q", got)
	}
}

func TestBuildLLMSessionContextIncludesCreativeAlbumSet(t *testing.T) {
	lastCreativeAlbumSet.mu.Lock()
	lastCreativeAlbumSet.sessions = make(map[string]creativeAlbumSetState)
	lastCreativeAlbumSet.mu.Unlock()

	setLastCreativeAlbumSet("session-creative", "underplayed_albums", "surprise me with underplayed albums", []creativeAlbumCandidate{
		{Name: "Teachings in Silence", ArtistName: "Ulver", Year: 2002, PlayCount: 2, LastPlayed: "2026-03-04T22:08:38Z"},
	})

	srv := &Server{}
	got := srv.buildLLMSessionContext("session-creative")
	if !strings.Contains(got, "last_creative_album_set") {
		t.Fatalf("buildLLMSessionContext() = %q", got)
	}
	if !strings.Contains(got, "active_conversation_object") {
		t.Fatalf("buildLLMSessionContext() = %q", got)
	}
	if !strings.Contains(got, "Teachings in Silence by Ulver (2002)") {
		t.Fatalf("buildLLMSessionContext() = %q", got)
	}
}

func TestBuildLLMSessionContextIncludesRecentListening(t *testing.T) {
	lastRecentListening.mu.Lock()
	lastRecentListening.sessions = make(map[string]recentListeningState)
	lastRecentListening.mu.Unlock()

	setLastRecentListeningSummary("session-listening", recentListeningState{
		windowStart:  "2026-02-19T03:18:39Z",
		windowEnd:    "2026-03-19T03:18:39Z",
		totalPlays:   478,
		tracksHeard:  304,
		artistsHeard: 36,
		topArtists: []recentListeningArtistState{
			{ArtistName: "Radiohead", TrackCount: 206},
			{ArtistName: "Pink Floyd", TrackCount: 61},
		},
		topTracks: []recentListeningTrackState{
			{Title: "Man Of War", ArtistName: "Radiohead", PlayCount: 12},
		},
	})

	srv := &Server{}
	got := srv.buildLLMSessionContext("session-listening")
	if !strings.Contains(got, "last_recent_listening") {
		t.Fatalf("buildLLMSessionContext() = %q", got)
	}
	if !strings.Contains(got, `top_artists="Radiohead:206 | Pink Floyd:61"`) {
		t.Fatalf("buildLLMSessionContext() = %q", got)
	}
}

func TestBuildLLMSessionContextIncludesTrackAndArtistCandidates(t *testing.T) {
	lastTrackCandidateSet.mu.Lock()
	lastTrackCandidateSet.sessions = make(map[string]trackCandidateSetState)
	lastTrackCandidateSet.mu.Unlock()
	lastArtistCandidateSet.mu.Lock()
	lastArtistCandidateSet.sessions = make(map[string]artistCandidateSetState)
	lastArtistCandidateSet.mu.Unlock()

	setLastTrackCandidateSet("session-track-candidates", "similar_tracks", "Windowlicker", []trackCandidate{
		{ID: "t1", Title: "Doll", ArtistName: "Foo Fighters"},
		{ID: "t2", Title: "Gold", ArtistName: "Imagine Dragons"},
	})
	setLastArtistCandidateSet("session-track-candidates", "Radiohead", []artistCandidate{
		{ID: "a1", Name: "Blur"},
		{ID: "a2", Name: "Elbow"},
	})

	srv := &Server{}
	got := srv.buildLLMSessionContext("session-track-candidates")
	if !strings.Contains(got, "last_track_candidates") {
		t.Fatalf("buildLLMSessionContext() = %q", got)
	}
	if !strings.Contains(got, "Doll by Foo Fighters") {
		t.Fatalf("buildLLMSessionContext() = %q", got)
	}
	if !strings.Contains(got, "last_artist_candidates") {
		t.Fatalf("buildLLMSessionContext() = %q", got)
	}
	if !strings.Contains(got, `sample="Blur | Elbow"`) {
		t.Fatalf("buildLLMSessionContext() = %q", got)
	}
}

func TestBuildLLMSessionContextIncludesFocusedResultItem(t *testing.T) {
	lastTrackCandidateSet.mu.Lock()
	lastTrackCandidateSet.sessions = make(map[string]trackCandidateSetState)
	lastTrackCandidateSet.mu.Unlock()
	lastFocusedResultItem.mu.Lock()
	lastFocusedResultItem.sessions = make(map[string]focusedResultItemState)
	lastFocusedResultItem.mu.Unlock()

	setLastTrackCandidateSet("session-focused", "similar_tracks", "Windowlicker", []trackCandidate{
		{ID: "t1", Title: "Doll", ArtistName: "Foo Fighters"},
	})
	setLastFocusedResultItem("session-focused", "track_candidates", normalizedTrackCandidateKey(trackCandidate{
		ID:         "t1",
		Title:      "Doll",
		ArtistName: "Foo Fighters",
	}))

	srv := &Server{}
	got := srv.buildLLMSessionContext("session-focused")
	if !strings.Contains(got, "focused_result_item") {
		t.Fatalf("buildLLMSessionContext() = %q", got)
	}
	if !strings.Contains(got, `label="Doll by Foo Fighters"`) {
		t.Fatalf("buildLLMSessionContext() = %q", got)
	}
}

func TestFormatStructuredChatMemoryIncludesFacts(t *testing.T) {
	now := time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC)
	got := formatStructuredChatMemory(chatSessionMemory{
		UpdatedAt:          now.Add(-5 * time.Minute),
		ActiveRequest:      "Find me some melancholic dream pop albums in my library.",
		RecentUserRequests: []string{"Find me some melancholic dream pop albums in my library.", "Narrow that to the 90s."},
		CurrentPlaylist:    "Late Night",
		LastDiscoveryQuery: "albums for a rainy late-night walk",
	}, now)
	required := []string{
		`active_request="Find me some melancholic dream pop albums in my library."`,
		`recent_user_requests="Find me some melancholic dream pop albums in my library. | Narrow that to the 90s."`,
		`current_playlist="Late Night"`,
		`last_discovery_query="albums for a rainy late-night walk"`,
	}
	for _, fragment := range required {
		if !strings.Contains(got, fragment) {
			t.Fatalf("formatStructuredChatMemory() = %q, missing %q", got, fragment)
		}
	}
}

func TestPlaylistDiscoveryStateIsSessionScoped(t *testing.T) {
	lastPlaylistDiscovery.mu.Lock()
	lastPlaylistDiscovery.sessions = make(map[string]playlistDiscoveryState)
	lastPlaylistDiscovery.mu.Unlock()

	setLastPlannedPlaylist("session-a", "ambient prompt", "Ambient Mix", []playlistCandidateTrack{
		{ArtistName: "Air", TrackTitle: "Alone in Kyoto"},
	})
	setLastPlannedPlaylist("session-b", "jazz prompt", "Jazz Mix", []playlistCandidateTrack{
		{ArtistName: "Alice Coltrane", TrackTitle: "Journey in Satchidananda"},
	})

	_, playlistA, _, candidatesA := getLastPlannedPlaylist("session-a")
	_, playlistB, _, candidatesB := getLastPlannedPlaylist("session-b")

	if playlistA != "Ambient Mix" || len(candidatesA) != 1 || candidatesA[0].ArtistName != "Air" {
		t.Fatalf("session-a playlist state = %q %+v", playlistA, candidatesA)
	}
	if playlistB != "Jazz Mix" || len(candidatesB) != 1 || candidatesB[0].ArtistName != "Alice Coltrane" {
		t.Fatalf("session-b playlist state = %q %+v", playlistB, candidatesB)
	}
}
