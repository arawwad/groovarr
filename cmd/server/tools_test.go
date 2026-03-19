package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"groovarr/internal/db"
	"groovarr/internal/discovery"
	"groovarr/internal/similarity"
)

func TestBuildNavidromePlaylistPayloadMergesSavedAndPendingState(t *testing.T) {
	playlist := &navidromePlaylistDetail{
		ID:   "pl-1",
		Name: "Late Night",
		Entries: []navidromePlaylistEntry{
			{ID: "song-1", Title: "Alone in Kyoto", Artist: "Air"},
			{ID: "song-2", Title: "Dayvan Cowboy", Artist: "Boards of Canada"},
		},
	}
	pending := []pendingPlaylistTrack{
		{JobID: "job-1", ArtistName: "Air", TrackTitle: "La Femme d'Argent", State: "pending_fetch", Attempts: 1, PlaylistName: "Late Night"},
		{JobID: "job-2", ArtistName: "Massive Attack", TrackTitle: "Teardrop", State: "waiting_import", Attempts: 2, PlaylistName: "Late Night"},
	}

	payload := buildNavidromePlaylistPayload(playlist, pending)

	if payload["name"] != "Late Night" {
		t.Fatalf("name = %#v", payload["name"])
	}
	counts, ok := payload["counts"].(map[string]int)
	if !ok {
		t.Fatalf("counts type = %T", payload["counts"])
	}
	if counts["saved"] != 2 || counts["pending_fetch"] != 1 || counts["total"] != 4 {
		t.Fatalf("counts = %#v", counts)
	}
	tracks, ok := payload["tracks"].([]map[string]interface{})
	if !ok {
		t.Fatalf("tracks type = %T", payload["tracks"])
	}
	if len(tracks) != 2 {
		t.Fatalf("len(tracks) = %d", len(tracks))
	}
	items, ok := payload["items"].([]map[string]interface{})
	if !ok {
		t.Fatalf("items type = %T", payload["items"])
	}
	if len(items) != 4 {
		t.Fatalf("len(items) = %d", len(items))
	}
	states := []string{
		items[0]["state"].(string),
		items[1]["state"].(string),
		items[2]["state"].(string),
		items[3]["state"].(string),
	}
	wantStates := []string{"saved", "saved", "pending_fetch", "waiting_import"}
	for i := range wantStates {
		if states[i] != wantStates[i] {
			t.Fatalf("states[%d] = %q, want %q", i, states[i], wantStates[i])
		}
	}
}

func TestHandlePlaylistPlanDetailsToolIncludesResolutionSnapshot(t *testing.T) {
	setLastPlannedPlaylist("sess-plan-details", "melancholy jazz for late nights", "Late Nights", []playlistCandidateTrack{
		{Rank: 1, ArtistName: "Miles Davis", TrackTitle: "Blue in Green", Reason: "gentle opener", SourceHint: "planner"},
		{Rank: 2, ArtistName: "Chet Baker", TrackTitle: "Almost Blue", Reason: "keeps the mood suspended"},
	})
	setLastResolvedPlaylist("sess-plan-details", []resolvedPlaylistTrack{
		{Rank: 1, ArtistName: "Miles Davis", TrackTitle: "Blue in Green", Status: "available", SongID: "song-1", MatchedArtist: "Miles Davis", MatchedTitle: "Blue in Green"},
		{Rank: 2, ArtistName: "Chet Baker", TrackTitle: "Almost Blue", Status: "missing"},
	})

	raw, err := executeTool(testChatContext("sess-plan-details", ""), nil, "", "playlistPlanDetails", map[string]interface{}{"selection": "all"})
	if err != nil {
		t.Fatalf("executeTool() error = %v", err)
	}
	var parsed struct {
		Data struct {
			Result struct {
				Prompt       string `json:"prompt"`
				PlaylistName string `json:"playlistName"`
				Selection    string `json:"selection"`
				Counts       struct {
					Planned int `json:"planned"`
				} `json:"counts"`
				ResolutionCounts struct {
					Resolved   int `json:"resolved"`
					Available  int `json:"available"`
					Missing    int `json:"missing"`
					Ambiguous  int `json:"ambiguous"`
					Errors     int `json:"errors"`
					Unresolved int `json:"unresolved"`
				} `json:"resolutionCounts"`
				Tracks []struct {
					Rank       int    `json:"rank"`
					ArtistName string `json:"artistName"`
					TrackTitle string `json:"trackTitle"`
					Reason     string `json:"reason"`
					Status     string `json:"status"`
					SongID     string `json:"songId"`
				} `json:"tracks"`
			} `json:"playlistPlanDetails"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if parsed.Data.Result.Prompt != "melancholy jazz for late nights" {
		t.Fatalf("prompt = %q", parsed.Data.Result.Prompt)
	}
	if parsed.Data.Result.PlaylistName != "Late Nights" {
		t.Fatalf("playlistName = %q", parsed.Data.Result.PlaylistName)
	}
	if parsed.Data.Result.Counts.Planned != 2 {
		t.Fatalf("planned count = %d", parsed.Data.Result.Counts.Planned)
	}
	if parsed.Data.Result.ResolutionCounts.Available != 1 || parsed.Data.Result.ResolutionCounts.Missing != 1 {
		t.Fatalf("resolutionCounts = %+v", parsed.Data.Result.ResolutionCounts)
	}
	if len(parsed.Data.Result.Tracks) != 2 {
		t.Fatalf("len(tracks) = %d", len(parsed.Data.Result.Tracks))
	}
	if parsed.Data.Result.Tracks[0].Reason != "gentle opener" || parsed.Data.Result.Tracks[0].Status != "available" || parsed.Data.Result.Tracks[0].SongID != "song-1" {
		t.Fatalf("first track = %+v", parsed.Data.Result.Tracks[0])
	}
	if parsed.Data.Result.Tracks[1].Status != "missing" {
		t.Fatalf("second track = %+v", parsed.Data.Result.Tracks[1])
	}
}

func TestHandleTextToTrackToolUsesAudioMuseCLAP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/clap/search" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"query": "smoky nocturnal trip-hop",
			"count": 2,
			"results": []map[string]interface{}{
				{"item_id": "song-1", "title": "Teardrop", "author": "Massive Attack", "similarity": 0.91},
				{"item_id": "song-2", "title": "Black Milk", "author": "Massive Attack", "similarity": 0.87},
			},
		})
	}))
	defer server.Close()
	t.Setenv("AUDIOMUSE_URL", server.URL)

	raw, err := executeTool(context.Background(), nil, "", "textToTrack", map[string]interface{}{
		"queryText": "smoky nocturnal trip-hop",
		"limit":     2,
	})
	if err != nil {
		t.Fatalf("executeTool() error = %v", err)
	}
	var parsed struct {
		Data struct {
			Result struct {
				QueryText string `json:"queryText"`
				Count     int    `json:"count"`
				Matches   []struct {
					ID         string  `json:"id"`
					Title      string  `json:"title"`
					ArtistName string  `json:"artistName"`
					Similarity float64 `json:"similarity"`
				} `json:"matches"`
			} `json:"textToTrack"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if parsed.Data.Result.QueryText != "smoky nocturnal trip-hop" {
		t.Fatalf("queryText = %q", parsed.Data.Result.QueryText)
	}
	if parsed.Data.Result.Count != 2 || len(parsed.Data.Result.Matches) != 2 {
		t.Fatalf("result = %+v", parsed.Data.Result)
	}
	if parsed.Data.Result.Matches[0].Title != "Teardrop" || parsed.Data.Result.Matches[0].ArtistName != "Massive Attack" {
		t.Fatalf("first match = %+v", parsed.Data.Result.Matches[0])
	}
}

func TestHandleClusterScenesToolReturnsFilteredScenes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/active_tasks", "/api/last_task":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{})
		case "/api/playlists":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"Indie_Rock_Alternative_Medium_Relaxed_Sad_automatic": []map[string]interface{}{
					{"title": "Teardrop", "author": "Massive Attack"},
					{"title": "Roads", "author": "Portishead"},
				},
				"Bright Pop": []map[string]interface{}{
					{"title": "Dancing Queen", "author": "ABBA"},
				},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("AUDIOMUSE_URL", server.URL)

	raw, err := executeTool(context.Background(), nil, "", "clusterScenes", map[string]interface{}{
		"queryText":    "relaxed",
		"limit":        5,
		"sampleTracks": 2,
	})
	if err != nil {
		t.Fatalf("executeTool() error = %v", err)
	}
	var parsed struct {
		Data struct {
			Result struct {
				Configured bool `json:"configured"`
				Ready      bool `json:"ready"`
				SceneCount int  `json:"sceneCount"`
				Scenes     []struct {
					Key       string `json:"key"`
					Name      string `json:"name"`
					Subtitle  string `json:"subtitle"`
					SongCount int    `json:"songCount"`
				} `json:"scenes"`
			} `json:"clusterScenes"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !parsed.Data.Result.Configured || !parsed.Data.Result.Ready {
		t.Fatalf("result = %+v", parsed.Data.Result)
	}
	if parsed.Data.Result.SceneCount != 1 || len(parsed.Data.Result.Scenes) != 1 {
		t.Fatalf("result = %+v", parsed.Data.Result)
	}
	if parsed.Data.Result.Scenes[0].Key != "Indie_Rock_Alternative_Medium_Relaxed_Sad_automatic" {
		t.Fatalf("scene key = %+v", parsed.Data.Result.Scenes[0])
	}
	if parsed.Data.Result.Scenes[0].Name != "Indie / Rock / Alternative • Mid-Tempo" || parsed.Data.Result.Scenes[0].Subtitle != "Relaxed, Sad" || parsed.Data.Result.Scenes[0].SongCount != 2 {
		t.Fatalf("scene = %+v", parsed.Data.Result.Scenes[0])
	}
}

func TestHandleSceneTracksToolReturnsTracksForResolvedSceneKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/playlists":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"Indie_Rock_Alternative_Medium_Relaxed_Sad_automatic": []map[string]interface{}{
					{"title": "Teardrop", "author": "Massive Attack"},
					{"title": "Roads", "author": "Portishead"},
					{"title": "Nude", "author": "Radiohead"},
				},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("AUDIOMUSE_URL", server.URL)

	raw, err := executeTool(context.Background(), nil, "", "sceneTracks", map[string]interface{}{
		"sceneKey": "Indie_Rock_Alternative_Medium_Relaxed_Sad_automatic",
		"limit":    2,
	})
	if err != nil {
		t.Fatalf("executeTool() error = %v", err)
	}
	var parsed struct {
		Data struct {
			Result struct {
				Resolved    bool `json:"resolved"`
				TrackCount  int  `json:"trackCount"`
				TotalTracks int  `json:"totalTracks"`
				Scene       struct {
					Key      string `json:"key"`
					Name     string `json:"name"`
					Subtitle string `json:"subtitle"`
				} `json:"scene"`
				Tracks []struct {
					Position   int    `json:"position"`
					Title      string `json:"title"`
					ArtistName string `json:"artistName"`
				} `json:"tracks"`
			} `json:"sceneTracks"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !parsed.Data.Result.Resolved {
		t.Fatalf("result = %+v", parsed.Data.Result)
	}
	if parsed.Data.Result.Scene.Key != "Indie_Rock_Alternative_Medium_Relaxed_Sad_automatic" {
		t.Fatalf("scene = %+v", parsed.Data.Result.Scene)
	}
	if parsed.Data.Result.Scene.Name != "Indie / Rock / Alternative • Mid-Tempo" || parsed.Data.Result.Scene.Subtitle != "Relaxed, Sad" {
		t.Fatalf("scene = %+v", parsed.Data.Result.Scene)
	}
	if parsed.Data.Result.TrackCount != 2 || parsed.Data.Result.TotalTracks != 3 || len(parsed.Data.Result.Tracks) != 2 {
		t.Fatalf("result = %+v", parsed.Data.Result)
	}
	if parsed.Data.Result.Tracks[0].Position != 1 || parsed.Data.Result.Tracks[0].Title != "Teardrop" {
		t.Fatalf("tracks = %+v", parsed.Data.Result.Tracks)
	}
}

func TestHandleSceneTracksToolReturnsAmbiguityForDuplicateDisplayNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/playlists":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"Electronic_Rock_Indie_Medium_Danceable_Party_automatic": []map[string]interface{}{
					{"title": "Track One", "author": "Artist One"},
				},
				"Electronic_Rock_Indie_Medium_Relaxed_Danceable_automatic": []map[string]interface{}{
					{"title": "Track Two", "author": "Artist Two"},
				},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("AUDIOMUSE_URL", server.URL)

	raw, err := executeTool(context.Background(), nil, "", "sceneTracks", map[string]interface{}{
		"sceneName": "Electronic / Rock / Indie • Mid-Tempo",
	})
	if err != nil {
		t.Fatalf("executeTool() error = %v", err)
	}
	var parsed struct {
		Data struct {
			Result struct {
				Resolved   bool `json:"resolved"`
				Ambiguous  bool `json:"ambiguous"`
				Candidates []struct {
					Key      string `json:"key"`
					Subtitle string `json:"subtitle"`
				} `json:"candidates"`
			} `json:"sceneTracks"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if parsed.Data.Result.Resolved || !parsed.Data.Result.Ambiguous {
		t.Fatalf("result = %+v", parsed.Data.Result)
	}
	if len(parsed.Data.Result.Candidates) != 2 {
		t.Fatalf("candidates = %+v", parsed.Data.Result.Candidates)
	}
	if parsed.Data.Result.Candidates[0].Key == "" || parsed.Data.Result.Candidates[1].Key == "" {
		t.Fatalf("candidates = %+v", parsed.Data.Result.Candidates)
	}
}

func TestHandleSceneExpandToolAggregatesAdjacentTracks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/playlists":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"Indie_Rock_Alternative_Medium_Relaxed_Sad_automatic": []map[string]interface{}{
					{"title": "Seed One", "author": "Artist A"},
					{"title": "Seed Two", "author": "Artist B"},
				},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("AUDIOMUSE_URL", server.URL)

	originalFetcher := sceneExpandSimilarTracksFetcher
	sceneExpandSimilarTracksFetcher = func(_ context.Context, _ toolRuntime, req similarity.TrackRequest) ([]similarity.TrackResult, string, error) {
		switch req.SeedTrackTitle {
		case "Seed One":
			return []similarity.TrackResult{
				{ID: "cand-1", Title: "Nearby One", ArtistName: "Neighbor A", Score: 0.71, PlayCount: 0},
				{ID: "cand-2", Title: "Nearby Two", ArtistName: "Neighbor B", Score: 0.63, PlayCount: 6},
				{ID: "seed-dup", Title: "Seed Two", ArtistName: "Artist B", Score: 0.95, PlayCount: 3},
			}, similarity.ProviderHybrid, nil
		case "Seed Two":
			return []similarity.TrackResult{
				{ID: "cand-1", Title: "Nearby One", ArtistName: "Neighbor A", Score: 0.69, PlayCount: 0},
				{ID: "cand-3", Title: "Nearby Three", ArtistName: "Neighbor C", Score: 0.6, PlayCount: 1},
			}, similarity.ProviderHybrid, nil
		default:
			t.Fatalf("unexpected seed title %q", req.SeedTrackTitle)
			return nil, "", nil
		}
	}
	defer func() {
		sceneExpandSimilarTracksFetcher = originalFetcher
	}()

	originalScoreFetcher := audioMuseScoreByIDFetcher
	audioMuseScoreByIDFetcher = func(_ context.Context, itemID string) (audioMuseTrackProfile, error) {
		switch itemID {
		case "cand-1":
			energy := 0.05
			return audioMuseTrackProfile{ItemID: itemID, Energy: &energy, OtherFeatures: "relaxed:0.8,sad:0.7"}, nil
		case "cand-2":
			energy := 0.18
			return audioMuseTrackProfile{ItemID: itemID, Energy: &energy, OtherFeatures: "aggressive:0.8"}, nil
		case "cand-3":
			energy := 0.08
			return audioMuseTrackProfile{ItemID: itemID, Energy: &energy, OtherFeatures: "relaxed:0.7"}, nil
		default:
			return audioMuseTrackProfile{}, nil
		}
	}
	defer func() {
		audioMuseScoreByIDFetcher = originalScoreFetcher
	}()

	raw, err := handleSceneExpandTool(context.Background(), toolRuntime{similarity: &similarity.Service{}}, map[string]interface{}{
		"sceneKey":  "Indie_Rock_Alternative_Medium_Relaxed_Sad_automatic",
		"queryText": "calmer and less familiar",
		"limit":     2,
		"seedCount": 2,
		"provider":  "hybrid",
	})
	if err != nil {
		t.Fatalf("handleSceneExpandTool() error = %v", err)
	}
	expanded, ok := raw.payload["sceneExpand"].(map[string]interface{})
	if !ok {
		t.Fatalf("payload = %#v", raw.payload)
	}
	if resolved, _ := expanded["resolved"].(bool); !resolved {
		t.Fatalf("sceneExpand resolved = %#v", expanded["resolved"])
	}
	tracks, ok := expanded["tracks"].([]map[string]interface{})
	if !ok {
		t.Fatalf("tracks type = %T", expanded["tracks"])
	}
	if len(tracks) != 2 {
		t.Fatalf("len(tracks) = %d", len(tracks))
	}
	if tracks[0]["title"] != "Nearby One" {
		t.Fatalf("first track = %#v", tracks[0])
	}
	if tracks[1]["title"] != "Nearby Three" {
		t.Fatalf("second track = %#v", tracks[1])
	}
	if tracks[0]["seedHits"] != 2 {
		t.Fatalf("seedHits = %#v", tracks[0]["seedHits"])
	}
	matchedSeeds, ok := tracks[0]["matchedSeeds"].([]string)
	if !ok || len(matchedSeeds) != 2 {
		t.Fatalf("matchedSeeds = %#v", tracks[0]["matchedSeeds"])
	}
}

func TestHandleDiscoverAlbumsFromSceneToolBuildsGroundedDiscoveryQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/playlists":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"Indie_Rock_Alternative_Medium_Relaxed_Sad_automatic": []map[string]interface{}{
					{"title": "Soldier's Poem", "author": "Muse"},
					{"title": "Bullet Proof... I Wish I Was", "author": "Radiohead"},
					{"title": "In Color", "author": "My Morning Jacket"},
					{"title": "Blue Velvet", "author": "Lana Del Rey"},
				},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("AUDIOMUSE_URL", server.URL)

	originalRunner := discoverAlbumsRequestRunner
	discoverAlbumsRequestRunner = func(_ context.Context, request discovery.Request) ([]discoveredAlbumCandidate, map[string]interface{}, error) {
		if request.Query != "darker and more spacious" {
			t.Fatalf("request.Query = %q", request.Query)
		}
		if request.Limit != 3 {
			t.Fatalf("request.Limit = %d", request.Limit)
		}
		if request.Seed == nil {
			t.Fatal("request.Seed = nil")
		}
		if request.Seed.Type != "scene" {
			t.Fatalf("seed.Type = %q", request.Seed.Type)
		}
		if request.Seed.Name != "Indie / Rock / Alternative • Mid-Tempo" || request.Seed.Subtitle != "Relaxed, Sad" {
			t.Fatalf("seed = %+v", request.Seed)
		}
		if len(request.Seed.RepresentativeArtists) != 4 {
			t.Fatalf("representative artists = %+v", request.Seed.RepresentativeArtists)
		}
		if len(request.Seed.RepresentativeTracks) != 3 {
			t.Fatalf("representative tracks = %+v", request.Seed.RepresentativeTracks)
		}
		return []discoveredAlbumCandidate{
			{Rank: 1, ArtistName: "Low", AlbumTitle: "Things We Lost in the Fire", Year: 2001, Reason: "slow, spacious, emotionally adjacent"},
			{Rank: 2, ArtistName: "Bluetile Lounge", AlbumTitle: "Half-Cut", Year: 1998, Reason: "hushed and nocturnal"},
		}, map[string]interface{}{"query": request.Query, "limit": request.Limit, "seed": request.Seed}, nil
	}
	defer func() {
		discoverAlbumsRequestRunner = originalRunner
	}()

	raw, err := executeTool(context.Background(), nil, "", "discoverAlbumsFromScene", map[string]interface{}{
		"sceneKey":  "Indie_Rock_Alternative_Medium_Relaxed_Sad_automatic",
		"queryText": "darker and more spacious",
		"limit":     3,
	})
	if err != nil {
		t.Fatalf("executeTool() error = %v", err)
	}
	var parsed struct {
		Data struct {
			Result struct {
				Resolved       bool   `json:"resolved"`
				Count          int    `json:"count"`
				DiscoveryQuery string `json:"discoveryQuery"`
				Scene          struct {
					Key      string `json:"key"`
					Name     string `json:"name"`
					Subtitle string `json:"subtitle"`
				} `json:"scene"`
				Candidates []struct {
					ArtistName string `json:"artistName"`
					AlbumTitle string `json:"albumTitle"`
				} `json:"candidates"`
			} `json:"discoverAlbumsFromScene"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !parsed.Data.Result.Resolved || parsed.Data.Result.Count != 2 {
		t.Fatalf("result = %+v", parsed.Data.Result)
	}
	if parsed.Data.Result.Scene.Key != "Indie_Rock_Alternative_Medium_Relaxed_Sad_automatic" {
		t.Fatalf("scene = %+v", parsed.Data.Result.Scene)
	}
	if parsed.Data.Result.Scene.Name != "Indie / Rock / Alternative • Mid-Tempo" || parsed.Data.Result.Scene.Subtitle != "Relaxed, Sad" {
		t.Fatalf("scene = %+v", parsed.Data.Result.Scene)
	}
	if len(parsed.Data.Result.Candidates) != 2 || parsed.Data.Result.Candidates[0].AlbumTitle != "Things We Lost in the Fire" {
		t.Fatalf("candidates = %+v", parsed.Data.Result.Candidates)
	}
	if parsed.Data.Result.DiscoveryQuery != "darker and more spacious" {
		t.Fatalf("discoveryQuery = %q", parsed.Data.Result.DiscoveryQuery)
	}
}

func TestHandleDescribeTrackSoundToolReturnsProfileAndNeighbors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search_tracks":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"item_id": "song-1", "title": "Man Of War", "author": "Radiohead", "album": "OKNOTOK"},
			})
		case "/api/similar_tracks":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"item_id": "song-2", "title": "Lucky", "author": "Radiohead", "album": "OK Computer", "distance": 0.04},
				{"item_id": "song-3", "title": "Subterranean Homesick Alien", "author": "Radiohead", "album": "OK Computer", "distance": 0.05},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("AUDIOMUSE_URL", server.URL)

	originalFetcher := audioMuseScoreByIDFetcher
	audioMuseScoreByIDFetcher = func(_ context.Context, itemID string) (audioMuseTrackProfile, error) {
		if itemID != "song-1" {
			t.Fatalf("itemID = %q", itemID)
		}
		tempo := 156.25
		energy := 0.104
		return audioMuseTrackProfile{
			ItemID:        "song-1",
			Title:         "Man Of War",
			Author:        "Radiohead",
			Album:         "OKNOTOK",
			Tempo:         &tempo,
			Key:           "C",
			Scale:         "minor",
			MoodVector:    "rock:0.575,indie:0.549,alternative:0.532",
			Energy:        &energy,
			OtherFeatures: "relaxed:0.64,sad:0.62,aggressive:0.61",
		}, nil
	}
	defer func() {
		audioMuseScoreByIDFetcher = originalFetcher
	}()

	raw, err := executeTool(context.Background(), nil, "", "describeTrackSound", map[string]interface{}{
		"trackTitle":    "Man Of War",
		"artistName":    "Radiohead",
		"neighborLimit": 2,
	})
	if err != nil {
		t.Fatalf("executeTool() error = %v", err)
	}
	var parsed struct {
		Data struct {
			Result struct {
				Track struct {
					ID         string `json:"id"`
					Title      string `json:"title"`
					ArtistName string `json:"artistName"`
					AlbumName  string `json:"albumName"`
				} `json:"track"`
				Summary struct {
					TempoLabel  string `json:"tempoLabel"`
					EnergyLabel string `json:"energyLabel"`
					ProfileText string `json:"profileText"`
					TopMoods    []struct {
						Label string `json:"label"`
					} `json:"topMoods"`
				} `json:"summary"`
				Neighbors []struct {
					Title      string `json:"title"`
					ArtistName string `json:"artistName"`
				} `json:"neighbors"`
			} `json:"describeTrackSound"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if parsed.Data.Result.Track.Title != "Man Of War" || parsed.Data.Result.Track.ArtistName != "Radiohead" {
		t.Fatalf("track = %+v", parsed.Data.Result.Track)
	}
	if parsed.Data.Result.Summary.TempoLabel != "fast" || parsed.Data.Result.Summary.EnergyLabel != "moderate" {
		t.Fatalf("summary = %+v", parsed.Data.Result.Summary)
	}
	if len(parsed.Data.Result.Summary.TopMoods) == 0 || parsed.Data.Result.Summary.TopMoods[0].Label != "rock" {
		t.Fatalf("top moods = %+v", parsed.Data.Result.Summary.TopMoods)
	}
	if len(parsed.Data.Result.Neighbors) != 2 || parsed.Data.Result.Neighbors[0].Title != "Lucky" {
		t.Fatalf("neighbors = %+v", parsed.Data.Result.Neighbors)
	}
	if !strings.Contains(parsed.Data.Result.Summary.ProfileText, "style tags") {
		t.Fatalf("profileText = %q", parsed.Data.Result.Summary.ProfileText)
	}
}

func TestHandleSongPathToolResolvesTracksAndReturnsPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search_tracks":
			query := r.URL.Query().Get("search_query")
			switch {
			case strings.Contains(query, "Heart-Shaped Box"):
				_ = json.NewEncoder(w).Encode([]map[string]interface{}{
					{"item_id": "start-1", "title": "Heart-Shaped Box", "author": "Nirvana", "album": "In Utero"},
					{"item_id": "start-2", "title": "Heart-Shaped Box", "author": "Nirvana", "album": "Unknown"},
				})
			case strings.Contains(query, "Teardrop"):
				_ = json.NewEncoder(w).Encode([]map[string]interface{}{
					{"item_id": "end-1", "title": "Teardrop", "author": "Massive Attack", "album": "Mezzanine"},
				})
			default:
				t.Fatalf("unexpected search query %q", query)
			}
		case "/api/find_path":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"path": []map[string]interface{}{
					{"item_id": "start-1", "title": "Heart-Shaped Box", "author": "Nirvana", "album": "In Utero"},
					{"item_id": "mid-1", "title": "All Apologies", "author": "Nirvana", "album": "In Utero"},
					{"item_id": "end-1", "title": "Teardrop", "author": "Massive Attack", "album": "Mezzanine"},
				},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("AUDIOMUSE_URL", server.URL)

	raw, err := executeTool(context.Background(), nil, "", "songPath", map[string]interface{}{
		"startTrackTitle": "Heart-Shaped Box",
		"startArtistName": "Nirvana",
		"endTrackTitle":   "Teardrop",
		"endArtistName":   "Massive Attack",
		"maxSteps":        12,
	})
	if err != nil {
		t.Fatalf("executeTool() error = %v", err)
	}
	var parsed struct {
		Data struct {
			Result struct {
				PathLength int `json:"pathLength"`
				Start      struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				} `json:"start"`
				End struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				} `json:"end"`
				Path []struct {
					Position int    `json:"position"`
					Title    string `json:"title"`
				} `json:"path"`
			} `json:"songPath"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if parsed.Data.Result.PathLength != 3 || len(parsed.Data.Result.Path) != 3 {
		t.Fatalf("result = %+v", parsed.Data.Result)
	}
	if parsed.Data.Result.Start.ID != "start-1" || parsed.Data.Result.End.ID != "end-1" {
		t.Fatalf("start/end = %+v / %+v", parsed.Data.Result.Start, parsed.Data.Result.End)
	}
	if parsed.Data.Result.Path[1].Position != 2 || parsed.Data.Result.Path[1].Title != "All Apologies" {
		t.Fatalf("path[1] = %+v", parsed.Data.Result.Path[1])
	}
}

func TestHandleSongPathToolFallsBackToTitleOnlyLookup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search_tracks":
			query := r.URL.Query().Get("search_query")
			switch query {
			case "Heart-Shaped Box by Nirvana":
				_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
			case "Heart-Shaped Box":
				_ = json.NewEncoder(w).Encode([]map[string]interface{}{
					{"item_id": "start-1", "title": "Heart-Shaped Box", "author": "Nirvana", "album": "In Utero"},
				})
			case "Teardrop by Massive Attack":
				_ = json.NewEncoder(w).Encode([]map[string]interface{}{
					{"item_id": "end-1", "title": "Teardrop", "author": "Massive Attack", "album": "Mezzanine"},
				})
			default:
				t.Fatalf("unexpected search query %q", query)
			}
		case "/api/find_path":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"path": []map[string]interface{}{
					{"item_id": "start-1", "title": "Heart-Shaped Box", "author": "Nirvana"},
					{"item_id": "end-1", "title": "Teardrop", "author": "Massive Attack"},
				},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("AUDIOMUSE_URL", server.URL)

	raw, err := executeTool(context.Background(), nil, "", "songPath", map[string]interface{}{
		"startTrackTitle": "Heart-Shaped Box",
		"startArtistName": "Nirvana",
		"endTrackTitle":   "Teardrop",
		"endArtistName":   "Massive Attack",
	})
	if err != nil {
		t.Fatalf("executeTool() error = %v", err)
	}
	if !strings.Contains(raw, `"pathLength":2`) {
		t.Fatalf("raw = %s", raw)
	}
}

func TestNormalizeArtistListeningStatsSortAliases(t *testing.T) {
	tests := map[string]string{
		"plays":       "plays_in_window_desc",
		"recent":      "last_played_desc",
		"name":        "name_asc",
		"albums":      "album_count_desc",
		"total_plays": "total_play_count_desc",
	}
	for input, want := range tests {
		input := input
		got, err := normalizeArtistListeningStatsSort(&input)
		if err != nil {
			t.Fatalf("normalizeArtistListeningStatsSort(%q) error = %v", input, err)
		}
		if got == nil || *got != want {
			t.Fatalf("normalizeArtistListeningStatsSort(%q) = %#v, want %q", input, got, want)
		}
	}
}

func TestNormalizeArtistListeningStatsSortRejectsUnknown(t *testing.T) {
	input := "loudest"
	got, err := normalizeArtistListeningStatsSort(&input)
	if err == nil {
		t.Fatal("normalizeArtistListeningStatsSort() expected error")
	}
	if got != nil {
		t.Fatalf("normalizeArtistListeningStatsSort() = %#v, want nil", got)
	}
	if !strings.Contains(err.Error(), "allowed: plays, recent, name, albums, total_plays") {
		t.Fatalf("normalizeArtistListeningStatsSort() error = %v", err)
	}
}

func TestRepairToolArgsAlbumsDropsSortOrder(t *testing.T) {
	got := repairToolArgs("albums", map[string]interface{}{
		"sortBy":    "playCount",
		"sortOrder": "asc",
	})
	if _, ok := got["sortOrder"]; ok {
		t.Fatalf("repairToolArgs() = %#v, want sortOrder removed", got)
	}
}

func TestRepairToolArgsSemanticAlbumSearchDropsUnsupportedFilters(t *testing.T) {
	got := repairToolArgs("semanticAlbumSearch", map[string]interface{}{
		"queryText":      "like recent listening",
		"notPlayedSince": "2026-01-01",
		"unplayed":       true,
	})
	if _, ok := got["notPlayedSince"]; ok {
		t.Fatalf("repairToolArgs() = %#v, want notPlayedSince removed", got)
	}
	if _, ok := got["unplayed"]; ok {
		t.Fatalf("repairToolArgs() = %#v, want unplayed removed", got)
	}
}

func TestResolveSummaryWindowRejectsMixedWindowAndExplicitRange(t *testing.T) {
	_, _, err := resolveSummaryWindow(map[string]interface{}{
		"window":      "last_month",
		"playedSince": "2026-02-10",
		"playedUntil": "2026-03-10",
	})
	if err == nil || !strings.Contains(err.Error(), "window cannot be combined") {
		t.Fatalf("resolveSummaryWindow() error = %v", err)
	}
}

func TestResolveSummaryWindowRejectsPartialExplicitRange(t *testing.T) {
	_, _, err := resolveSummaryWindow(map[string]interface{}{
		"playedSince": "2026-02-10",
	})
	if err == nil || !strings.Contains(err.Error(), "must both be provided") {
		t.Fatalf("resolveSummaryWindow() error = %v", err)
	}
}

func TestResolveSummaryWindowNormalizesRecentAlias(t *testing.T) {
	start, end, err := resolveSummaryWindow(map[string]interface{}{
		"window": "recent",
	})
	if err != nil {
		t.Fatalf("resolveSummaryWindow() error = %v", err)
	}
	if !end.After(start) {
		t.Fatalf("window = %v to %v, want end after start", start, end)
	}
}

func TestResolveSummaryWindowSupportsThisMonth(t *testing.T) {
	start, end, err := resolveSummaryWindow(map[string]interface{}{
		"window": "this month",
	})
	if err != nil {
		t.Fatalf("resolveSummaryWindow() error = %v", err)
	}
	now := time.Now().UTC()
	if start.Year() != now.Year() || start.Month() != now.Month() || start.Day() != 1 {
		t.Fatalf("start = %v, want first day of current UTC month", start)
	}
	if !end.After(start) {
		t.Fatalf("window = %v to %v, want end after start", start, end)
	}
}

func TestParseTimeRangeRejectsNonIncreasingWindow(t *testing.T) {
	_, _, err := parseTimeRange("2026-03-10", "2026-03-10")
	if err == nil || !strings.Contains(err.Error(), "playedUntil must be later than playedSince") {
		t.Fatalf("parseTimeRange() error = %v", err)
	}
}

func TestNormalizeInactiveSinceRelativeShortcut(t *testing.T) {
	got, err := normalizeInactiveSince("years")
	if err != nil {
		t.Fatalf("normalizeInactiveSince() error = %v", err)
	}
	parsed, err := time.Parse("2006-01-02", got)
	if err != nil {
		t.Fatalf("normalizeInactiveSince() returned %q, parse error = %v", got, err)
	}

	earliest := time.Now().UTC().AddDate(-2, 0, -1)
	latest := time.Now().UTC().AddDate(-1, 11, 1)
	if parsed.Before(earliest) || parsed.After(latest) {
		t.Fatalf("normalizeInactiveSince() = %q, outside expected range", got)
	}
}

func TestValidateMapKeysRejectsUnknownArgs(t *testing.T) {
	err := validateMapKeys(map[string]interface{}{
		"notPlayedSince": "years",
		"surprise":       true,
	}, "notPlayedSince")
	if err == nil {
		t.Fatal("validateMapKeys() expected error for unknown key")
	}
	if !strings.Contains(err.Error(), "surprise") {
		t.Fatalf("validateMapKeys() error = %v, want unknown key in message", err)
	}
}

func TestToolIntArgUsesDefaultForZeroOrNegativeValues(t *testing.T) {
	args := map[string]interface{}{
		"zeroFloat": 0.0,
		"negative":  -3,
		"zeroText":  "0",
	}

	if got := toolIntArg(args, "zeroFloat", 25); got != 25 {
		t.Fatalf("toolIntArg(zeroFloat) = %d, want default 25", got)
	}
	if got := toolIntArg(args, "negative", 10); got != 10 {
		t.Fatalf("toolIntArg(negative) = %d, want default 10", got)
	}
	if got := toolIntArg(args, "zeroText", 5); got != 5 {
		t.Fatalf("toolIntArg(zeroText) = %d, want default 5", got)
	}
}

func TestToolOptStringListArgParsesArraysAndText(t *testing.T) {
	args := map[string]interface{}{
		"artistsArray": []interface{}{" Radiohead ", "The Beatles", "radiohead"},
		"artistsText":  "Pink Floyd, Bjork\nMassive Attack",
	}

	gotArray := toolOptStringListArg(args, "artistsArray")
	if len(gotArray) != 2 || gotArray[0] != "Radiohead" || gotArray[1] != "The Beatles" {
		t.Fatalf("toolOptStringListArg(array) = %#v", gotArray)
	}

	gotText := toolOptStringListArg(args, "artistsText")
	wantText := []string{"Pink Floyd", "Bjork", "Massive Attack"}
	if len(gotText) != len(wantText) {
		t.Fatalf("toolOptStringListArg(text) len = %d, want %d", len(gotText), len(wantText))
	}
	for i := range wantText {
		if gotText[i] != wantText[i] {
			t.Fatalf("toolOptStringListArg(text)[%d] = %q, want %q", i, gotText[i], wantText[i])
		}
	}
}

func TestMapOptStringListParsesArraysAndStrings(t *testing.T) {
	args := map[string]interface{}{
		"artistName":  []interface{}{"Radiohead", " The Beatles ", "radiohead"},
		"artistNames": "Pink Floyd",
	}

	gotArtistName := mapOptStringList(args, "artistName")
	wantArtistName := []string{"Radiohead", "The Beatles"}
	if len(gotArtistName) != len(wantArtistName) {
		t.Fatalf("mapOptStringList(artistName) len = %d, want %d", len(gotArtistName), len(wantArtistName))
	}
	for i := range wantArtistName {
		if gotArtistName[i] != wantArtistName[i] {
			t.Fatalf("mapOptStringList(artistName)[%d] = %q, want %q", i, gotArtistName[i], wantArtistName[i])
		}
	}

	gotArtistNames := mapOptStringList(args, "artistNames")
	if len(gotArtistNames) != 1 || gotArtistNames[0] != "Pink Floyd" {
		t.Fatalf("mapOptStringList(artistNames) = %#v", gotArtistNames)
	}
}

func TestBuildAlbumQuerySupportsArtistNames(t *testing.T) {
	limit, filters, err := buildAlbumQuery(map[string]interface{}{
		"artistName":  "Pink Floyd",
		"artistNames": []interface{}{"Radiohead", "Pink Floyd", "The Beatles"},
		"limit":       12,
		"sortBy":      "rating",
	})
	if err != nil {
		t.Fatalf("buildAlbumQuery() error = %v", err)
	}
	if limit != 12 {
		t.Fatalf("buildAlbumQuery() limit = %d, want 12", limit)
	}
	gotNames, ok := filters["artistNames"].([]string)
	if !ok {
		t.Fatalf("buildAlbumQuery() artistNames filter missing: %#v", filters)
	}
	wantNames := []string{"Pink Floyd", "Radiohead", "The Beatles"}
	if len(gotNames) != len(wantNames) {
		t.Fatalf("buildAlbumQuery() artistNames len = %d, want %d", len(gotNames), len(wantNames))
	}
	for i := range wantNames {
		if gotNames[i] != wantNames[i] {
			t.Fatalf("buildAlbumQuery() artistNames[%d] = %q, want %q", i, gotNames[i], wantNames[i])
		}
	}
	if _, ok := filters["artistName"]; ok {
		t.Fatalf("buildAlbumQuery() unexpected single artistName filter: %#v", filters)
	}
}

func TestBuildAlbumQuerySupportsQueryText(t *testing.T) {
	limit, filters, err := buildAlbumQuery(map[string]interface{}{
		"artistName": "Pink Floyd",
		"queryText":  "The Dark Side of the Moon",
		"limit":      3,
	})
	if err != nil {
		t.Fatalf("buildAlbumQuery() error = %v", err)
	}
	if limit != 3 {
		t.Fatalf("buildAlbumQuery() limit = %d, want 3", limit)
	}
	if got, ok := filters["queryText"].(string); !ok || got != "The Dark Side of the Moon" {
		t.Fatalf("buildAlbumQuery() queryText = %#v", filters["queryText"])
	}
	if got, ok := filters["artistName"].(string); !ok || got != "Pink Floyd" {
		t.Fatalf("buildAlbumQuery() artistName = %#v", filters["artistName"])
	}
}

func TestBuildAlbumQuerySplitsByArtistFromQueryText(t *testing.T) {
	_, filters, err := buildAlbumQuery(map[string]interface{}{
		"queryText": "The Dark Side of the Moon by Pink Floyd",
	})
	if err != nil {
		t.Fatalf("buildAlbumQuery() error = %v", err)
	}
	if got, ok := filters["queryText"].(string); !ok || got != "The Dark Side of the Moon" {
		t.Fatalf("buildAlbumQuery() queryText = %#v", filters["queryText"])
	}
	if got, ok := filters["artistName"].(string); !ok || got != "Pink Floyd" {
		t.Fatalf("buildAlbumQuery() artistName = %#v", filters["artistName"])
	}
}

func TestBuildAlbumQuerySupportsYearRange(t *testing.T) {
	_, filters, err := buildAlbumQuery(map[string]interface{}{
		"artistName": "Bjork",
		"minYear":    1990,
		"maxYear":    1999,
	})
	if err != nil {
		t.Fatalf("buildAlbumQuery() error = %v", err)
	}
	if got, ok := filters["minYear"].(int); !ok || got != 1990 {
		t.Fatalf("buildAlbumQuery() minYear = %#v", filters["minYear"])
	}
	if got, ok := filters["maxYear"].(int); !ok || got != 1999 {
		t.Fatalf("buildAlbumQuery() maxYear = %#v", filters["maxYear"])
	}
}

func TestSplitLookupQueryArtist(t *testing.T) {
	title, artist, ok := splitLookupQueryArtist("Heart-Shaped Box by Nirvana")
	if !ok {
		t.Fatal("splitLookupQueryArtist() = false, want true")
	}
	if title != "Heart-Shaped Box" || artist != "Nirvana" {
		t.Fatalf("splitLookupQueryArtist() = %q, %q", title, artist)
	}
}

func TestRerankSemanticAlbumMatchesPenalizesCompilationLikeAlbums(t *testing.T) {
	year1966 := 1966
	year1994 := 1994
	matches := []db.SimilarAlbum{
		{
			Album: db.Album{
				Name:       "1962-1966",
				ArtistName: "The Beatles",
				Year:       &year1966,
			},
			Similarity: 0.91,
		},
		{
			Album: db.Album{
				Name:       "Selected Ambient Works Volume II",
				ArtistName: "Aphex Twin",
				Year:       &year1994,
			},
			Similarity: 0.89,
		},
	}

	got := rerankSemanticAlbumMatches("nocturnal ambient", matches, 2)
	if len(got) != 2 {
		t.Fatalf("rerankSemanticAlbumMatches() len = %d", len(got))
	}
	if got[0].Name != "Selected Ambient Works Volume II" {
		t.Fatalf("rerankSemanticAlbumMatches() first = %q, want non-compilation album first", got[0].Name)
	}
}

func TestExplainSemanticAlbumMatchUsesMusicBrainzTags(t *testing.T) {
	genre := "ambient, electronic"
	match := db.SimilarAlbum{
		Album: db.Album{
			Name:       "Soon It Will Be Cold Enough",
			ArtistName: "Emancipator",
			Genre:      &genre,
			Metadata: map[string]interface{}{
				"musicbrainz": map[string]interface{}{
					"tags": []interface{}{"atmospheric", "nocturnal", "downtempo"},
				},
			},
		},
		Similarity: 0.92,
	}

	got := explainSemanticAlbumMatch("nocturnal ambient", match, nil, nil, nil, nil)
	if len(got) == 0 {
		t.Fatal("explainSemanticAlbumMatch() returned no explanations")
	}
	if got[0] != "genre matched: ambient" && got[0] != "MusicBrainz tags matched: nocturnal" {
		t.Fatalf("explainSemanticAlbumMatch() first explanation = %q", got[0])
	}
	foundTagMatch := false
	for _, item := range got {
		if strings.Contains(item, "MusicBrainz tags matched: nocturnal") {
			foundTagMatch = true
			break
		}
	}
	if !foundTagMatch {
		t.Fatalf("explainSemanticAlbumMatch() = %#v, want MusicBrainz tag match", got)
	}
}

func TestExplainSemanticAlbumMatchUsesLastFMTagFallback(t *testing.T) {
	match := db.SimilarAlbum{
		Album: db.Album{
			Name:       "Nothing's Real",
			ArtistName: "Shura",
			Metadata: map[string]interface{}{
				"lastfm": map[string]interface{}{
					"tags": []interface{}{"dream pop", "synthpop", "melancholic"},
				},
			},
		},
		Similarity: 0.87,
	}

	got := explainSemanticAlbumMatch("melancholic dream pop", match, nil, nil, nil, nil)
	if len(got) == 0 {
		t.Fatal("explainSemanticAlbumMatch() returned no explanations")
	}
	foundTagMatch := false
	for _, item := range got {
		if strings.Contains(item, "Last.fm tags matched:") &&
			strings.Contains(item, "dream") &&
			strings.Contains(item, "melancholic") {
			foundTagMatch = true
			break
		}
	}
	if !foundTagMatch {
		t.Fatalf("explainSemanticAlbumMatch() = %#v, want Last.fm tag match", got)
	}
}

func TestRerankSemanticAlbumMatchesPrefersMusicBrainzDescriptorOverlap(t *testing.T) {
	yearA := 1994
	yearB := 1976
	matches := []db.SimilarAlbum{
		{
			Album: db.Album{
				Name:       "Station To Station",
				ArtistName: "David Bowie",
				Year:       &yearB,
				Metadata: map[string]interface{}{
					"musicbrainz": map[string]interface{}{
						"tags": []interface{}{"alternative rock", "art rock"},
					},
				},
			},
			Similarity: 0.47,
		},
		{
			Album: db.Album{
				Name:       "Selected Ambient Works Volume II",
				ArtistName: "Aphex Twin",
				Year:       &yearA,
				Metadata: map[string]interface{}{
					"musicbrainz": map[string]interface{}{
						"tags": []interface{}{"ambient", "atmospheric", "dark ambient"},
					},
				},
			},
			Similarity: 0.44,
		},
	}

	got := rerankSemanticAlbumMatches("nocturnal ambient", matches, 2)
	if len(got) != 2 {
		t.Fatalf("rerankSemanticAlbumMatches() len = %d", len(got))
	}
	if got[0].Name != "Selected Ambient Works Volume II" {
		t.Fatalf("rerankSemanticAlbumMatches() first = %q, want descriptor-overlap album first", got[0].Name)
	}
}

func TestRerankSemanticAlbumMatchesDedupesAlbumVariants(t *testing.T) {
	year := 1999
	matches := []db.SimilarAlbum{
		{
			Album: db.Album{
				Name:       "Hours",
				ArtistName: "David Bowie",
				Year:       &year,
				Metadata: map[string]interface{}{
					"musicbrainz": map[string]interface{}{
						"genres": []interface{}{"experimental rock"},
					},
				},
			},
			Similarity: 0.70,
		},
		{
			Album: db.Album{
				Name:       "‘hours…’",
				ArtistName: "David Bowie",
				Year:       &year,
				Metadata: map[string]interface{}{
					"musicbrainz": map[string]interface{}{
						"genres": []interface{}{"experimental rock"},
					},
				},
			},
			Similarity: 0.69,
		},
		{
			Album: db.Album{
				Name:       "Black Tie White Noise (2021 Remaster)",
				ArtistName: "David Bowie",
				Year:       &year,
			},
			Similarity: 0.72,
		},
		{
			Album: db.Album{
				Name:       "Black Tie White Noise",
				ArtistName: "David Bowie",
				Year:       &year,
			},
			Similarity: 0.68,
		},
	}

	got := rerankSemanticAlbumMatches("weird experimental", matches, 4)
	if len(got) != 2 {
		t.Fatalf("rerankSemanticAlbumMatches() len = %d, want 2 canonical albums", len(got))
	}
	if got[0].Name != "Hours" && got[0].Name != "‘hours…’" {
		t.Fatalf("rerankSemanticAlbumMatches() first = %q, want Hours variant", got[0].Name)
	}
	if got[1].Name != "Black Tie White Noise" {
		t.Fatalf("rerankSemanticAlbumMatches() second = %q, want base album over remaster", got[1].Name)
	}
}

func TestMetadataStringSliceFiltersLowSignalValues(t *testing.T) {
	metadata := map[string]interface{}{
		"musicbrainz": map[string]interface{}{
			"tags": []interface{}{"ambient", "5+ Wochen", "discogs/the most popular album released every year from 1950 to 2020", "nocturnal"},
		},
	}
	got := metadataStringSlice(metadata, "musicbrainz", "tags")
	if len(got) != 2 {
		t.Fatalf("metadataStringSlice() len = %d, want 2", len(got))
	}
	if got[0] != "ambient" || got[1] != "nocturnal" {
		t.Fatalf("metadataStringSlice() = %#v", got)
	}
}

func TestRerankSemanticAlbumMatchesPenalizesMoodMismatches(t *testing.T) {
	yearA := 1999
	yearB := 1994
	matches := []db.SimilarAlbum{
		{
			Album: db.Album{
				Name:       "Play",
				ArtistName: "Moby",
				Year:       &yearA,
				Metadata: map[string]interface{}{
					"musicbrainz": map[string]interface{}{
						"tags": []interface{}{"5+ Wochen", "alternative pop/rock"},
					},
				},
			},
			Similarity: 0.93,
		},
		{
			Album: db.Album{
				Name:       "Selected Ambient Works Volume II",
				ArtistName: "Aphex Twin",
				Year:       &yearB,
				Metadata: map[string]interface{}{
					"musicbrainz": map[string]interface{}{
						"tags": []interface{}{"ambient", "nocturnal", "dark ambient"},
					},
				},
			},
			Similarity: 0.88,
		},
	}
	got := rerankSemanticAlbumMatches("nocturnal ambient", matches, 2)
	if len(got) != 2 {
		t.Fatalf("rerankSemanticAlbumMatches() len = %d", len(got))
	}
	if got[0].Name != "Selected Ambient Works Volume II" {
		t.Fatalf("rerankSemanticAlbumMatches() first = %q, want mood-overlap album first", got[0].Name)
	}
}

func TestRerankSemanticAlbumMatchesPrefersDreamPopPhraseOverlap(t *testing.T) {
	yearA := 2009
	yearB := 1999
	matches := []db.SimilarAlbum{
		{
			Album: db.Album{
				Name:       "Life Of Leisure",
				ArtistName: "Washed Out",
				Year:       &yearA,
				Metadata: map[string]interface{}{
					"musicbrainz": map[string]interface{}{
						"tags": []interface{}{"dream pop", "synthpop", "chillwave"},
					},
				},
			},
			Similarity: 0.58,
		},
		{
			Album: db.Album{
				Name:       "Play",
				ArtistName: "Moby",
				Year:       &yearB,
				Metadata: map[string]interface{}{
					"musicbrainz": map[string]interface{}{
						"tags": []interface{}{"alternative pop/rock", "melancholic"},
					},
				},
			},
			Similarity: 0.61,
		},
	}

	got := rerankSemanticAlbumMatches("melancholic dream pop", matches, 2)
	if len(got) != 2 {
		t.Fatalf("rerankSemanticAlbumMatches() len = %d", len(got))
	}
	if got[0].Name != "Life Of Leisure" {
		t.Fatalf("rerankSemanticAlbumMatches() first = %q, want phrase-overlap album first", got[0].Name)
	}
}

func TestRerankSemanticAlbumMatchesPrefersLastFMDescriptorOverlap(t *testing.T) {
	yearA := 2016
	yearB := 1999
	matches := []db.SimilarAlbum{
		{
			Album: db.Album{
				Name:       "Nothing's Real",
				ArtistName: "Shura",
				Year:       &yearA,
				Metadata: map[string]interface{}{
					"lastfm": map[string]interface{}{
						"tags": []interface{}{"dream pop", "synthpop", "melancholic", "night"},
					},
				},
			},
			Similarity: 0.57,
		},
		{
			Album: db.Album{
				Name:       "Play",
				ArtistName: "Moby",
				Year:       &yearB,
				Metadata: map[string]interface{}{
					"musicbrainz": map[string]interface{}{
						"tags": []interface{}{"alternative pop/rock", "electronic"},
					},
				},
			},
			Similarity: 0.61,
		},
	}

	got := rerankSemanticAlbumMatches("melancholic dream pop", matches, 2)
	if len(got) != 2 {
		t.Fatalf("rerankSemanticAlbumMatches() len = %d", len(got))
	}
	if got[0].Name != "Nothing's Real" {
		t.Fatalf("rerankSemanticAlbumMatches() first = %q, want Last.fm overlap album first", got[0].Name)
	}
}

func TestRerankSemanticAlbumMatchesPrefersDreamPopAdjacentPhraseOverlap(t *testing.T) {
	yearA := 2013
	yearB := 1999
	matches := []db.SimilarAlbum{
		{
			Album: db.Album{
				Name:       "Anything in Return",
				ArtistName: "Toro y Moi",
				Year:       &yearA,
				Metadata: map[string]interface{}{
					"musicbrainz": map[string]interface{}{
						"tags": []interface{}{"chillwave", "indie pop", "electronic"},
					},
				},
			},
			Similarity: 0.56,
		},
		{
			Album: db.Album{
				Name:       "Play",
				ArtistName: "Moby",
				Year:       &yearB,
				Metadata: map[string]interface{}{
					"musicbrainz": map[string]interface{}{
						"tags": []interface{}{"melancholic", "ambient", "downtempo"},
					},
				},
			},
			Similarity: 0.61,
		},
	}

	got := rerankSemanticAlbumMatches("melancholic dream pop", matches, 2)
	if len(got) != 2 {
		t.Fatalf("rerankSemanticAlbumMatches() len = %d", len(got))
	}
	if got[0].Name != "Anything in Return" {
		t.Fatalf("rerankSemanticAlbumMatches() first = %q, want adjacent dream-pop phrase album first", got[0].Name)
	}
}

func TestRerankSemanticAlbumMatchesDropsDreamPopTailWhenEnoughPhraseMatchesExist(t *testing.T) {
	yearA := 2009
	yearB := 2013
	yearC := 2015
	yearD := 1999
	matches := []db.SimilarAlbum{
		{
			Album: db.Album{
				Name:       "Life Of Leisure",
				ArtistName: "Washed Out",
				Year:       &yearA,
				Metadata: map[string]interface{}{
					"musicbrainz": map[string]interface{}{
						"tags": []interface{}{"dream pop", "chillwave"},
					},
				},
			},
			Similarity: 0.58,
		},
		{
			Album: db.Album{
				Name:       "Anything in Return",
				ArtistName: "Toro y Moi",
				Year:       &yearB,
				Metadata: map[string]interface{}{
					"musicbrainz": map[string]interface{}{
						"tags": []interface{}{"chillwave", "dreamy"},
					},
				},
			},
			Similarity: 0.57,
		},
		{
			Album: db.Album{
				Name:       "Honeymoon",
				ArtistName: "Lana Del Rey",
				Year:       &yearC,
				Metadata: map[string]interface{}{
					"lastfm": map[string]interface{}{
						"tags": []interface{}{"dream pop", "melancholic"},
					},
				},
			},
			Similarity: 0.56,
		},
		{
			Album: db.Album{
				Name:       "Play",
				ArtistName: "Moby",
				Year:       &yearD,
				Metadata: map[string]interface{}{
					"musicbrainz": map[string]interface{}{
						"tags": []interface{}{"melancholic", "ambient"},
					},
				},
			},
			Similarity: 0.64,
		},
	}

	got := rerankSemanticAlbumMatches("melancholic dream pop", matches, 4)
	if len(got) != 3 {
		t.Fatalf("rerankSemanticAlbumMatches() len = %d, want 3 phrase-overlap results", len(got))
	}
	for _, item := range got {
		if item.Name == "Play" {
			t.Fatalf("rerankSemanticAlbumMatches() unexpectedly kept non-phrase tail: %#v", got)
		}
	}
}

func TestSemanticAlbumHasPhraseOverlap(t *testing.T) {
	match := db.SimilarAlbum{
		Album: db.Album{
			Name:       "Life Of Leisure",
			ArtistName: "Washed Out",
			Metadata: map[string]interface{}{
				"musicbrainz": map[string]interface{}{
					"tags": []interface{}{"dream pop", "chillwave"},
				},
			},
		},
	}
	if !semanticAlbumHasPhraseOverlap("melancholic dream pop", match) {
		t.Fatal("semanticAlbumHasPhraseOverlap() = false, want true")
	}
	if semanticAlbumHasPhraseOverlap("late night walk", db.SimilarAlbum{Album: db.Album{Name: "Play", ArtistName: "Moby"}}) {
		t.Fatal("semanticAlbumHasPhraseOverlap() = true, want false for no descriptor overlap")
	}
}

func TestSemanticDescriptorQueryTermsSuppressesGenericPopInDreamPopQuery(t *testing.T) {
	got := semanticDescriptorQueryTerms("melancholic dream pop")
	want := []string{"melancholic", "dream"}
	if len(got) != len(want) {
		t.Fatalf("semanticDescriptorQueryTerms() len = %d, want %d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("semanticDescriptorQueryTerms()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSemanticPhraseTermsExpandDreamPopToChillwave(t *testing.T) {
	got := semanticPhraseTerms("melancholic dream pop")
	found := false
	for _, term := range got {
		if term == "chillwave" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("semanticPhraseTerms() = %#v, want chillwave expansion", got)
	}
}

func TestNormalizeOptionalYearTreatsZeroAsUnset(t *testing.T) {
	zero := 0
	if got := normalizeOptionalYear(&zero); got != nil {
		t.Fatalf("normalizeOptionalYear(0) = %#v, want nil", got)
	}
	year := 1994
	got := normalizeOptionalYear(&year)
	if got == nil || *got != 1994 {
		t.Fatalf("normalizeOptionalYear(1994) = %#v", got)
	}
}

func TestSelectNavidromePlaylistEntriesMatchesBySelection(t *testing.T) {
	entries := []navidromePlaylistEntry{
		{ID: "1", Title: "Alone in Kyoto", Artist: "Air"},
		{ID: "2", Title: "Dayvan Cowboy", Artist: "Boards of Canada"},
		{ID: "3", Title: "Cherry-coloured Funk", Artist: "Cocteau Twins"},
	}

	indexes, selected, err := selectNavidromePlaylistEntries(entries, "first 2")
	if err != nil {
		t.Fatalf("selectNavidromePlaylistEntries() error = %v", err)
	}
	if len(indexes) != 2 || indexes[0] != 0 || indexes[1] != 1 {
		t.Fatalf("selectNavidromePlaylistEntries() indexes = %#v", indexes)
	}
	if len(selected) != 2 || selected[1].Title != "Dayvan Cowboy" {
		t.Fatalf("selectNavidromePlaylistEntries() selected = %#v", selected)
	}

	indexes, selected, err = selectNavidromePlaylistEntries(entries, "kyoto")
	if err != nil {
		t.Fatalf("selectNavidromePlaylistEntries() substring error = %v", err)
	}
	if len(indexes) != 1 || indexes[0] != 0 || len(selected) != 1 || selected[0].Artist != "Air" {
		t.Fatalf("selectNavidromePlaylistEntries() substring = %#v %#v", indexes, selected)
	}
}

func TestPendingPlaylistTracksAndRemovalUseReconcileJobs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PLAYLIST_RECONCILE_DIR", dir)
	queueDir := t.TempDir()
	t.Setenv("PLAYLIST_QUEUE_DIR", queueDir)

	queue := playlistReconcileQueue{
		Items: []playlistReconcileItem{
			{
				ID:         "track-1",
				QueueFile:  filepath.Join(queueDir, "track-1.txt"),
				ArtistName: "Air",
				TrackTitle: "La Femme d'Argent",
				Playlists:  []string{"Late Night"},
				Attempts:   2,
				UpdatedAt:  time.Now().UTC(),
			},
			{
				ID:         "track-2",
				QueueFile:  filepath.Join(queueDir, "track-2.txt"),
				ArtistName: "Boards of Canada",
				TrackTitle: "Dayvan Cowboy",
				Playlists:  []string{"Late Night"},
				Attempts:   2,
				UpdatedAt:  time.Now().UTC(),
			},
		},
	}
	body, err := json.Marshal(queue)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	path := filepath.Join(dir, "queue.json")
	if err := os.WriteFile(path, body, 0644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	pending, err := pendingPlaylistTracksForPlaylist("Late Night")
	if err != nil {
		t.Fatalf("pendingPlaylistTracksForPlaylist() error = %v", err)
	}
	if len(pending) != 2 || pending[0].State != "pending_fetch" {
		t.Fatalf("pendingPlaylistTracksForPlaylist() = %#v", pending)
	}

	removed, err := removePendingPlaylistTracksForPlaylist("Late Night", "dayvan")
	if err != nil {
		t.Fatalf("removePendingPlaylistTracksForPlaylist() error = %v", err)
	}
	if len(removed) != 1 || removed[0].TrackTitle != "Dayvan Cowboy" {
		t.Fatalf("removePendingPlaylistTracksForPlaylist() = %#v", removed)
	}

	pending, err = pendingPlaylistTracksForPlaylist("Late Night")
	if err != nil {
		t.Fatalf("pendingPlaylistTracksForPlaylist() after removal error = %v", err)
	}
	if len(pending) != 1 || pending[0].TrackTitle != "La Femme d'Argent" {
		t.Fatalf("pendingPlaylistTracksForPlaylist() after removal = %#v", pending)
	}
}

func TestEnqueuePlaylistReconcileTracksUsesGlobalQueueMemberships(t *testing.T) {
	dir := t.TempDir()
	queueDir := t.TempDir()
	t.Setenv("PLAYLIST_RECONCILE_DIR", dir)
	t.Setenv("PLAYLIST_QUEUE_DIR", queueDir)

	queued, queueFile, itemID, err := enqueuePlaylistReconcileTracks("Late Night", []resolvedPlaylistTrack{{
		ArtistName: "Air",
		TrackTitle: "La Femme d'Argent",
		Status:     "missing",
	}})
	if err != nil {
		t.Fatalf("enqueuePlaylistReconcileTracks() error = %v", err)
	}
	if queued != 1 || strings.TrimSpace(queueFile) == "" || strings.TrimSpace(itemID) == "" {
		t.Fatalf("enqueuePlaylistReconcileTracks() = queued=%d queueFile=%q itemID=%q", queued, queueFile, itemID)
	}

	queued, _, _, err = enqueuePlaylistReconcileTracks("After Hours", []resolvedPlaylistTrack{{
		ArtistName: "Air",
		TrackTitle: "La Femme d'Argent",
		Status:     "missing",
	}})
	if err != nil {
		t.Fatalf("enqueuePlaylistReconcileTracks() second call error = %v", err)
	}
	if queued != 0 {
		t.Fatalf("enqueuePlaylistReconcileTracks() second queued = %d, want 0", queued)
	}

	queue, err := loadPlaylistReconcileQueue()
	if err != nil {
		t.Fatalf("loadPlaylistReconcileQueue() error = %v", err)
	}
	if len(queue.Items) != 1 {
		t.Fatalf("loadPlaylistReconcileQueue() items = %#v", queue.Items)
	}
	if got := queue.Items[0].Playlists; len(got) != 2 || got[0] != "After Hours" || got[1] != "Late Night" {
		t.Fatalf("queue item playlists = %#v", got)
	}
}

func TestRunPlaylistReconcilePassDropsItemsThatExceedMaxAttempts(t *testing.T) {
	dir := t.TempDir()
	queueDir := t.TempDir()
	statusDir := t.TempDir()
	t.Setenv("PLAYLIST_RECONCILE_DIR", dir)
	t.Setenv("PLAYLIST_QUEUE_DIR", queueDir)
	t.Setenv("PLAYLIST_QUEUE_STATUS_DIR", statusDir)
	t.Setenv("PLAYLIST_RECONCILE_MAX_ATTEMPTS", "3")

	queueFile := filepath.Join(queueDir, "track-1.txt")
	if err := os.WriteFile(queueFile, []byte("Ella Fitzgerald - The Man I Love\n"), 0644); err != nil {
		t.Fatalf("os.WriteFile(queueFile) error = %v", err)
	}
	donePath := filepath.Join(statusDir, "track-1.txt.done")
	if err := os.WriteFile(donePath, []byte("ok"), 0644); err != nil {
		t.Fatalf("os.WriteFile(donePath) error = %v", err)
	}

	queue := playlistReconcileQueue{
		Items: []playlistReconcileItem{
			{
				ID:         "track-1",
				QueueFile:  queueFile,
				ArtistName: "Ella Fitzgerald",
				TrackTitle: "The Man I Love",
				Playlists:  []string{"Melancholy Jazz"},
				Attempts:   3,
				UpdatedAt:  time.Now().UTC(),
			},
		},
	}
	body, err := json.Marshal(queue)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "queue.json"), body, 0644); err != nil {
		t.Fatalf("os.WriteFile(queue.json) error = %v", err)
	}

	if err := runPlaylistReconcilePass(context.Background(), nil); err != nil {
		t.Fatalf("runPlaylistReconcilePass() error = %v", err)
	}

	got, err := loadPlaylistReconcileQueue()
	if err != nil {
		t.Fatalf("loadPlaylistReconcileQueue() error = %v", err)
	}
	if len(got.Items) != 0 {
		t.Fatalf("loadPlaylistReconcileQueue() items = %#v, want empty queue", got.Items)
	}
	if _, err := os.Stat(queueFile); !os.IsNotExist(err) {
		t.Fatalf("os.Stat(queueFile) err = %v, want not exist", err)
	}
	if _, err := os.Stat(donePath); !os.IsNotExist(err) {
		t.Fatalf("os.Stat(donePath) err = %v, want not exist", err)
	}
}
