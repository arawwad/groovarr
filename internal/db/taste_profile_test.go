package db

import (
	"math"
	"testing"
	"time"
)

func TestComputeReplayAndNoveltyTolerance(t *testing.T) {
	t.Run("empty history", func(t *testing.T) {
		if got := computeReplayAffinity(0, 0); got != 0 {
			t.Fatalf("computeReplayAffinity() = %v, want 0", got)
		}
		if got := computeNoveltyTolerance(0, 0); got != 0 {
			t.Fatalf("computeNoveltyTolerance() = %v, want 0", got)
		}
	})

	t.Run("replay heavy history", func(t *testing.T) {
		replay := computeReplayAffinity(100, 20)
		novelty := computeNoveltyTolerance(100, 20)
		if replay <= novelty {
			t.Fatalf("expected replay affinity %v to exceed novelty tolerance %v", replay, novelty)
		}
		if !approxEqual(replay+novelty, 1, 1e-9) {
			t.Fatalf("expected replay + novelty to equal 1, got %v", replay+novelty)
		}
	})
}

func TestBuildArtistTasteProfile(t *testing.T) {
	now := time.Date(2026, time.March, 14, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-2 * time.Hour)
	stale := now.Add(-20 * 24 * time.Hour)

	features := buildArtistTasteProfile(now, []artistTasteSignal{
		{
			ArtistName:    "Heavy Rotation",
			TotalPlays:    80,
			RecentPlays:   12,
			LastPlayed:    &recent,
			AverageRating: 4.5,
		},
		{
			ArtistName:    "Occasional Listen",
			TotalPlays:    12,
			RecentPlays:   1,
			LastPlayed:    &stale,
			AverageRating: 3.0,
		},
	})

	if len(features) != 2 {
		t.Fatalf("buildArtistTasteProfile() len = %d, want 2", len(features))
	}

	heavy := findArtistFeature(t, features, "Heavy Rotation")
	light := findArtistFeature(t, features, "Occasional Listen")

	if heavy.FamiliarityScore <= light.FamiliarityScore {
		t.Fatalf("expected heavy familiarity %v to exceed light familiarity %v", heavy.FamiliarityScore, light.FamiliarityScore)
	}
	if heavy.FatigueScore <= light.FatigueScore {
		t.Fatalf("expected heavy fatigue %v to exceed light fatigue %v", heavy.FatigueScore, light.FatigueScore)
	}
	if heavy.AverageRating != 4.5 {
		t.Fatalf("expected heavy average rating 4.5, got %v", heavy.AverageRating)
	}
}

func TestBuildAlbumTasteProfile(t *testing.T) {
	now := time.Date(2026, time.March, 14, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-4 * time.Hour)
	stale := now.Add(-30 * 24 * time.Hour)

	features := buildAlbumTasteProfile(now, []albumTasteSignal{
		{
			AlbumID:     "fav",
			AlbumName:   "Favorite",
			ArtistName:  "Artist",
			TotalPlays:  60,
			RecentPlays: 10,
			LastPlayed:  &recent,
			Rating:      5,
		},
		{
			AlbumID:     "archived",
			AlbumName:   "Archived",
			ArtistName:  "Artist",
			TotalPlays:  8,
			RecentPlays: 0,
			LastPlayed:  &stale,
			Rating:      3,
		},
	})

	if len(features) != 2 {
		t.Fatalf("buildAlbumTasteProfile() len = %d, want 2", len(features))
	}

	favorite := findAlbumFeature(t, features, "fav")
	archived := findAlbumFeature(t, features, "archived")

	if favorite.OverexposureScore <= archived.OverexposureScore {
		t.Fatalf("expected favorite overexposure %v to exceed archived overexposure %v", favorite.OverexposureScore, archived.OverexposureScore)
	}
	if favorite.Rating != 5 {
		t.Fatalf("expected favorite rating 5, got %d", favorite.Rating)
	}
}

func findArtistFeature(t *testing.T, features []TasteProfileArtistFeature, artistName string) TasteProfileArtistFeature {
	t.Helper()
	for _, feature := range features {
		if feature.ArtistName == artistName {
			return feature
		}
	}
	t.Fatalf("artist feature %q not found", artistName)
	return TasteProfileArtistFeature{}
}

func findAlbumFeature(t *testing.T, features []TasteProfileAlbumFeature, albumID string) TasteProfileAlbumFeature {
	t.Helper()
	for _, feature := range features {
		if feature.AlbumID == albumID {
			return feature
		}
	}
	t.Fatalf("album feature %q not found", albumID)
	return TasteProfileAlbumFeature{}
}

func approxEqual(left, right, tolerance float64) bool {
	return math.Abs(left-right) <= tolerance
}
