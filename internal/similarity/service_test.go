package similarity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"groovarr/internal/db"

	"github.com/pgvector/pgvector-go"
)

type fakeRepo struct {
	tracksByID          map[string]*db.Track
	tracksByArtistTitle map[string]*db.Track
	tracksByArtist      map[string][]db.Track
	artistsByName       map[string]*db.Artist
	similarTracks       []db.SimilarTrack
	similarArtists      []db.Artist
	tasteSummary        *db.TasteProfileSummary
	artistFeatures      map[string]db.TasteProfileArtistFeature
	albumFeatures       map[string]db.TasteProfileAlbumFeature
	listeningContext    *db.ListeningContext
}

func (f *fakeRepo) GetTrackByID(_ context.Context, id string) (*db.Track, error) {
	return f.tracksByID[id], nil
}

func (f *fakeRepo) GetTrackByArtistTitle(_ context.Context, artistName, title string) (*db.Track, error) {
	return f.tracksByArtistTitle[artistName+"|"+title], nil
}

func (f *fakeRepo) GetTracks(_ context.Context, limit int, _ bool, filters map[string]interface{}) ([]db.Track, error) {
	artistName, _ := filters["artistName"].(string)
	items := f.tracksByArtist[artistName]
	if len(items) > limit {
		return items[:limit], nil
	}
	return items, nil
}

func (f *fakeRepo) FindSimilarTracksByEmbedding(_ context.Context, _ pgvector.Vector, _ int, _, _ *time.Time) ([]db.SimilarTrack, error) {
	return f.similarTracks, nil
}

func (f *fakeRepo) GetArtistByName(_ context.Context, name string) (*db.Artist, error) {
	return f.artistsByName[name], nil
}

func (f *fakeRepo) FindSimilarArtists(_ context.Context, _ pgvector.Vector, _ int) ([]db.Artist, error) {
	return f.similarArtists, nil
}

func (f *fakeRepo) GetTasteProfileSummary(_ context.Context) (*db.TasteProfileSummary, error) {
	return f.tasteSummary, nil
}

func (f *fakeRepo) GetArtistTasteFeatures(_ context.Context, artistNames []string) (map[string]db.TasteProfileArtistFeature, error) {
	out := make(map[string]db.TasteProfileArtistFeature, len(artistNames))
	for _, artistName := range artistNames {
		if item, ok := f.artistFeatures[normalizeKey(artistName)]; ok {
			out[normalizeKey(artistName)] = item
		}
	}
	return out, nil
}

func (f *fakeRepo) GetAlbumTasteFeatures(_ context.Context, albumIDs []string) (map[string]db.TasteProfileAlbumFeature, error) {
	out := make(map[string]db.TasteProfileAlbumFeature, len(albumIDs))
	for _, albumID := range albumIDs {
		if item, ok := f.albumFeatures[albumID]; ok {
			out[albumID] = item
		}
	}
	return out, nil
}

func (f *fakeRepo) GetListeningContext(_ context.Context) (*db.ListeningContext, error) {
	return f.listeningContext, nil
}

func (f *fakeRepo) UpsertListeningContext(_ context.Context, mode, mood string, expiresAt *time.Time, source string) (*db.ListeningContext, error) {
	f.listeningContext = &db.ListeningContext{
		Scope:     "global",
		Mode:      mode,
		Mood:      mood,
		ExpiresAt: expiresAt,
		UpdatedAt: time.Now().UTC(),
		Source:    source,
	}
	return f.listeningContext, nil
}

func (f *fakeRepo) DeleteListeningContext(_ context.Context) error {
	f.listeningContext = nil
	return nil
}

func TestSimilarTracksLocal(t *testing.T) {
	repo := &fakeRepo{
		tracksByID: map[string]*db.Track{
			"seed": {
				ID:         "seed",
				AlbumID:    "album-a",
				Title:      "Seed Track",
				ArtistName: "Seed Artist",
				Embedding:  pgvector.NewVector([]float32{0.1, 0.2, 0.3}),
			},
		},
		similarTracks: []db.SimilarTrack{
			{
				Track: db.Track{
					ID:         "seed",
					Title:      "Seed Track",
					ArtistName: "Seed Artist",
				},
				Similarity: 0.99,
			},
			{
				Track: db.Track{
					ID:         "track-b",
					AlbumID:    "album-b",
					Title:      "Track B",
					ArtistName: "Artist B",
					Rating:     5,
					PlayCount:  8,
				},
				Similarity: 0.88,
			},
		},
	}
	service := NewService(repo, Config{DefaultProvider: ProviderLocal})

	response, err := service.SimilarTracks(context.Background(), TrackRequest{
		SeedTrackID: "seed",
		Provider:    ProviderLocal,
		Limit:       5,
	})
	if err != nil {
		t.Fatalf("SimilarTracks() error = %v", err)
	}
	if response.Provider != ProviderLocal {
		t.Fatalf("provider = %q, want %q", response.Provider, ProviderLocal)
	}
	if len(response.Results) != 1 {
		t.Fatalf("results len = %d, want 1", len(response.Results))
	}
	if response.Results[0].ID != "track-b" {
		t.Fatalf("result id = %q, want track-b", response.Results[0].ID)
	}
	if got := response.Results[0].Sources; len(got) != 1 || got[0] != ProviderLocal {
		t.Fatalf("sources = %#v", got)
	}
}

func TestSimilarTracksHybridMergesAudioMuseCandidates(t *testing.T) {
	audioMuse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"Track B","artist_name":"Artist B","score":0.95},{"title":"Track C","artist_name":"Artist C","score":0.80}]}`))
	}))
	defer audioMuse.Close()

	repo := &fakeRepo{
		tracksByID: map[string]*db.Track{
			"seed": {
				ID:         "seed",
				AlbumID:    "album-a",
				Title:      "Seed Track",
				ArtistName: "Seed Artist",
				Embedding:  pgvector.NewVector([]float32{0.1, 0.2, 0.3}),
			},
		},
		tracksByArtistTitle: map[string]*db.Track{
			"Artist B|Track B": {
				ID:         "track-b",
				AlbumID:    "album-b",
				Title:      "Track B",
				ArtistName: "Artist B",
				Rating:     4,
				PlayCount:  12,
			},
			"Artist C|Track C": {
				ID:         "track-c",
				AlbumID:    "album-c",
				Title:      "Track C",
				ArtistName: "Artist C",
				Rating:     3,
				PlayCount:  4,
			},
		},
		similarTracks: []db.SimilarTrack{
			{
				Track: db.Track{
					ID:         "track-b",
					AlbumID:    "album-b",
					Title:      "Track B",
					ArtistName: "Artist B",
					Rating:     4,
					PlayCount:  12,
				},
				Similarity: 0.70,
			},
		},
	}
	service := NewService(repo, Config{
		DefaultProvider:      ProviderHybrid,
		AudioMuseBaseURL:     audioMuse.URL,
		AudioMuseTracksPath:  "/similarity",
		AudioMuseArtistsPath: "/artists",
		AudioMuseHealthPath:  "/health",
	})

	response, err := service.SimilarTracks(context.Background(), TrackRequest{
		SeedTrackID: "seed",
		Provider:    ProviderHybrid,
		Limit:       5,
	})
	if err != nil {
		t.Fatalf("SimilarTracks() error = %v", err)
	}
	if response.Provider != ProviderHybrid {
		t.Fatalf("provider = %q, want %q", response.Provider, ProviderHybrid)
	}
	if len(response.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(response.Results))
	}
	first := response.Results[0]
	if first.ID != "track-b" {
		t.Fatalf("first result id = %q, want track-b", first.ID)
	}
	if len(first.Sources) != 2 {
		t.Fatalf("first result sources = %#v, want two providers", first.Sources)
	}
	if _, ok := first.SourceScores[ProviderLocal]; !ok {
		t.Fatalf("first result missing local source score: %#v", first.SourceScores)
	}
	if _, ok := first.SourceScores[ProviderAudioMuse]; !ok {
		t.Fatalf("first result missing audiomuse source score: %#v", first.SourceScores)
	}
}

func TestSimilarTracksReranksWithTasteProfile(t *testing.T) {
	repo := &fakeRepo{
		tracksByID: map[string]*db.Track{
			"seed": {
				ID:         "seed",
				AlbumID:    "album-a",
				Title:      "Seed Track",
				ArtistName: "Seed Artist",
				Embedding:  pgvector.NewVector([]float32{0.1, 0.2, 0.3}),
			},
		},
		similarTracks: []db.SimilarTrack{
			{
				Track: db.Track{
					ID:         "track-b",
					AlbumID:    "album-b",
					Title:      "Track B",
					ArtistName: "Favorite Artist",
					Rating:     5,
					PlayCount:  18,
				},
				Similarity: 0.86,
			},
			{
				Track: db.Track{
					ID:         "track-c",
					AlbumID:    "album-c",
					Title:      "Track C",
					ArtistName: "Unknown Artist",
					Rating:     2,
					PlayCount:  1,
				},
				Similarity: 0.89,
			},
		},
		tasteSummary: &db.TasteProfileSummary{
			ReplayAffinityScore:   0.8,
			NoveltyToleranceScore: 0.15,
		},
		artistFeatures: map[string]db.TasteProfileArtistFeature{
			normalizeKey("Favorite Artist"): {
				ArtistName:       "Favorite Artist",
				FamiliarityScore: 0.95,
				FatigueScore:     0.10,
			},
			normalizeKey("Unknown Artist"): {
				ArtistName:       "Unknown Artist",
				FamiliarityScore: 0.05,
				FatigueScore:     0.20,
			},
		},
		albumFeatures: map[string]db.TasteProfileAlbumFeature{
			"album-b": {AlbumID: "album-b", OverexposureScore: 0.10},
			"album-c": {AlbumID: "album-c", OverexposureScore: 0.15},
		},
	}
	service := NewService(repo, Config{DefaultProvider: ProviderLocal})

	response, err := service.SimilarTracks(context.Background(), TrackRequest{
		SeedTrackID: "seed",
		Provider:    ProviderLocal,
		Limit:       5,
	})
	if err != nil {
		t.Fatalf("SimilarTracks() error = %v", err)
	}
	if len(response.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(response.Results))
	}
	if response.Results[0].ID != "track-b" {
		t.Fatalf("expected taste rerank to promote track-b, got %q", response.Results[0].ID)
	}
	if response.Results[0].SourceScores[scoreListeningAffinity] <= response.Results[1].SourceScores[scoreListeningAffinity] {
		t.Fatalf("expected higher listening affinity for promoted result: %#v", response.Results)
	}
}

func TestSimilarTracksAppliesDiversityPenalty(t *testing.T) {
	repo := &fakeRepo{
		tracksByID: map[string]*db.Track{
			"seed": {
				ID:         "seed",
				AlbumID:    "album-a",
				Title:      "Seed Track",
				ArtistName: "Seed Artist",
				Embedding:  pgvector.NewVector([]float32{0.1, 0.2, 0.3}),
			},
		},
		similarTracks: []db.SimilarTrack{
			{Track: db.Track{ID: "track-b1", AlbumID: "album-b", Title: "Track B1", ArtistName: "Artist B"}, Similarity: 0.94},
			{Track: db.Track{ID: "track-b2", AlbumID: "album-b", Title: "Track B2", ArtistName: "Artist B"}, Similarity: 0.92},
			{Track: db.Track{ID: "track-c1", AlbumID: "album-c", Title: "Track C1", ArtistName: "Artist C"}, Similarity: 0.90},
		},
	}
	service := NewService(repo, Config{DefaultProvider: ProviderLocal})

	response, err := service.SimilarTracks(context.Background(), TrackRequest{
		SeedTrackID: "seed",
		Provider:    ProviderLocal,
		Limit:       5,
	})
	if err != nil {
		t.Fatalf("SimilarTracks() error = %v", err)
	}
	if len(response.Results) != 3 {
		t.Fatalf("results len = %d, want 3", len(response.Results))
	}
	if response.Results[1].ID != "track-c1" {
		t.Fatalf("expected diversity penalty to move track-c1 ahead of second Artist B result, got %#v", response.Results)
	}
	if response.Results[2].SourceScores[scoreDiversityPenalty] <= 0 {
		t.Fatalf("expected diversity penalty on trailing repeated artist result, got %#v", response.Results[2].SourceScores)
	}
}

func TestSimilarTracksStoredModeAffectsRanking(t *testing.T) {
	repo := &fakeRepo{
		tracksByID: map[string]*db.Track{
			"seed": {
				ID:         "seed",
				AlbumID:    "album-a",
				Title:      "Seed Track",
				ArtistName: "Seed Artist",
				Embedding:  pgvector.NewVector([]float32{0.1, 0.2, 0.3}),
			},
		},
		similarTracks: []db.SimilarTrack{
			{Track: db.Track{ID: "familiar", AlbumID: "album-f", Title: "Familiar", ArtistName: "Favorite Artist", PlayCount: 18}, Similarity: 0.86},
			{Track: db.Track{ID: "novel", AlbumID: "album-n", Title: "Novel", ArtistName: "Unknown Artist", PlayCount: 1}, Similarity: 0.84},
		},
		tasteSummary: &db.TasteProfileSummary{
			ReplayAffinityScore:   0.85,
			NoveltyToleranceScore: 0.20,
		},
		artistFeatures: map[string]db.TasteProfileArtistFeature{
			normalizeKey("Favorite Artist"): {ArtistName: "Favorite Artist", FamiliarityScore: 0.95, FatigueScore: 0.10},
			normalizeKey("Unknown Artist"):  {ArtistName: "Unknown Artist", FamiliarityScore: 0.05, FatigueScore: 0.10},
		},
		albumFeatures: map[string]db.TasteProfileAlbumFeature{
			"album-f": {AlbumID: "album-f", OverexposureScore: 0.10},
			"album-n": {AlbumID: "album-n", OverexposureScore: 0.10},
		},
		listeningContext: &db.ListeningContext{Scope: "global", Mode: ModeFamiliar},
	}
	service := NewService(repo, Config{DefaultProvider: ProviderLocal})

	response, err := service.SimilarTracks(context.Background(), TrackRequest{
		SeedTrackID: "seed",
		Provider:    ProviderLocal,
		Limit:       5,
	})
	if err != nil {
		t.Fatalf("SimilarTracks() error = %v", err)
	}
	if response.Results[0].ID != "familiar" {
		t.Fatalf("expected familiar mode to promote familiar result, got %#v", response.Results)
	}

	repo.listeningContext = &db.ListeningContext{Scope: "global", Mode: ModeSurprise}
	response, err = service.SimilarTracks(context.Background(), TrackRequest{
		SeedTrackID: "seed",
		Provider:    ProviderLocal,
		Limit:       5,
	})
	if err != nil {
		t.Fatalf("SimilarTracks() error = %v", err)
	}
	if response.Results[0].ID != "novel" {
		t.Fatalf("expected surprise mode to promote novel result, got %#v", response.Results)
	}
}

func TestExplicitContextOverrideBeatsStoredContext(t *testing.T) {
	repo := &fakeRepo{
		listeningContext: &db.ListeningContext{Scope: "global", Mode: ModeFamiliar, Mood: "warm"},
	}
	service := NewService(repo, Config{DefaultProvider: ProviderLocal})

	contextValue, err := service.resolveListeningContext(context.Background(), ModeDeepCut, "night drive", defaultTrackMode(), false)
	if err != nil {
		t.Fatalf("resolveListeningContext() error = %v", err)
	}
	if contextValue.Mode != ModeDeepCut {
		t.Fatalf("mode = %q, want %q", contextValue.Mode, ModeDeepCut)
	}
	if contextValue.Mood != "night drive" {
		t.Fatalf("mood = %q, want %q", contextValue.Mood, "night drive")
	}
}

func TestSimilarSongsByArtistHybrid(t *testing.T) {
	audioMuse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"name":"Artist B","score":0.92},{"name":"Artist C","score":0.81}]}`))
	}))
	defer audioMuse.Close()

	repo := &fakeRepo{
		artistsByName: map[string]*db.Artist{
			"Seed Artist": {
				ID:        "artist-seed",
				Name:      "Seed Artist",
				Embedding: pgvector.NewVector([]float32{0.4, 0.1, 0.2}),
			},
		},
		similarArtists: []db.Artist{
			{ID: "artist-b", Name: "Artist B"},
			{ID: "artist-c", Name: "Artist C"},
		},
		tracksByArtist: map[string][]db.Track{
			"Artist B": {
				{ID: "track-b1", AlbumID: "album-b", Title: "Track B1", ArtistName: "Artist B", PlayCount: 10},
			},
			"Artist C": {
				{ID: "track-c1", AlbumID: "album-c", Title: "Track C1", ArtistName: "Artist C", PlayCount: 7},
			},
		},
	}
	service := NewService(repo, Config{
		DefaultProvider:      ProviderHybrid,
		AudioMuseBaseURL:     audioMuse.URL,
		AudioMuseTracksPath:  "/tracks",
		AudioMuseArtistsPath: "/artists",
		AudioMuseHealthPath:  "/health",
	})

	response, err := service.SimilarSongsByArtist(context.Background(), ArtistSongsRequest{
		SeedArtistName: "Seed Artist",
		Provider:       ProviderHybrid,
		Limit:          5,
	})
	if err != nil {
		t.Fatalf("SimilarSongsByArtist() error = %v", err)
	}
	if response.Provider != ProviderHybrid {
		t.Fatalf("provider = %q, want %q", response.Provider, ProviderHybrid)
	}
	if len(response.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(response.Results))
	}
	if response.Results[0].ID == "" {
		t.Fatal("expected mapped track id")
	}
}

func TestNewServiceEnqueuesInitialAudioMuseAnalysis(t *testing.T) {
	var (
		mu             sync.Mutex
		analysisStarts int
	)
	audioMuse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/active_tasks":
			_, _ = w.Write([]byte(`{}`))
		case "/api/last_task":
			_, _ = w.Write([]byte(`{"status":"NO_PREVIOUS_MAIN_TASK","task_id":null,"task_type":null}`))
		case "/api/analysis/start":
			mu.Lock()
			analysisStarts++
			mu.Unlock()
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if got := int(payload["num_recent_albums"].(float64)); got != 0 {
				t.Errorf("num_recent_albums = %d, want 0", got)
			}
			if got := int(payload["top_n_moods"].(float64)); got != 7 {
				t.Errorf("top_n_moods = %d, want 7", got)
			}
			_, _ = w.Write([]byte(`{"task_id":"bootstrap-task","status":"queued"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer audioMuse.Close()

	service := NewService(&fakeRepo{}, Config{
		DefaultProvider:                ProviderHybrid,
		AudioMuseBaseURL:               audioMuse.URL,
		AudioMuseTracksPath:            "/api/similar_tracks",
		AudioMuseArtistsPath:           "/api/similar_artists",
		AudioMuseHealthPath:            "/",
		AudioMuseBootstrapEnabled:      true,
		AudioMuseBootstrapRecentAlbums: 0,
		AudioMuseBootstrapTopMoods:     7,
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		state := service.getBootstrapState()
		if state.status == audioMuseBootstrapAnalysisQueued {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("bootstrap status = %q, want %q", state.status, audioMuseBootstrapAnalysisQueued)
		}
		time.Sleep(25 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if analysisStarts != 1 {
		t.Fatalf("analysis start calls = %d, want 1", analysisStarts)
	}
}

func TestHealthReportsAudioMuseReadyState(t *testing.T) {
	audioMuse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/active_tasks":
			_, _ = w.Write([]byte(`{}`))
		case "/api/last_task":
			_, _ = w.Write([]byte(`{"status":"SUCCESS","task_id":"done-task","task_type":"main_analysis"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer audioMuse.Close()

	service := NewService(&fakeRepo{}, Config{
		DefaultProvider:      ProviderHybrid,
		AudioMuseBaseURL:     audioMuse.URL,
		AudioMuseTracksPath:  "/api/similar_tracks",
		AudioMuseArtistsPath: "/api/similar_artists",
		AudioMuseHealthPath:  "/",
	})

	health := service.Health(context.Background())
	if !health.AudioMuseReachable {
		t.Fatal("expected AudioMuseReachable to be true")
	}
	if health.AudioMuseLibraryState != audioMuseLibraryStateReady {
		t.Fatalf("AudioMuseLibraryState = %q, want %q", health.AudioMuseLibraryState, audioMuseLibraryStateReady)
	}
}
