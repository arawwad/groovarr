package main

import "testing"

func TestMapAudioMuseMapSamplesRespectsLimitAndCoordinates(t *testing.T) {
	raw := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{
				"item_id":      "track-1",
				"title":        "Alpha",
				"author":       "Artist One",
				"album":        "Album One",
				"embedding_2d": []interface{}{1.25, -0.4},
			},
			map[string]interface{}{
				"id":           "track-2",
				"name":         "Beta",
				"artist_name":  "Artist Two",
				"album_name":   "Album Two",
				"embedding_2d": []interface{}{"2.5", "0.75"},
			},
			map[string]interface{}{
				"item_id": "track-3",
				"title":   "Gamma",
				"author":  "Artist Three",
			},
		},
	}

	got := mapAudioMuseMapSamples(raw, 2)
	if len(got) != 2 {
		t.Fatalf("mapAudioMuseMapSamples() len = %d, want 2", len(got))
	}
	if got[0].ID != "track-1" || got[0].Title != "Alpha" || got[0].ArtistName != "Artist One" || got[0].AlbumName != "Album One" {
		t.Fatalf("first sample = %#v", got[0])
	}
	if got[0].X == nil || got[0].Y == nil || *got[0].X != 1.25 || *got[0].Y != -0.4 {
		t.Fatalf("first sample coordinates = %#v / %#v", got[0].X, got[0].Y)
	}
	if got[1].ID != "track-2" || got[1].Title != "Beta" || got[1].ArtistName != "Artist Two" || got[1].AlbumName != "Album Two" {
		t.Fatalf("second sample = %#v", got[1])
	}
	if got[1].X == nil || got[1].Y == nil || *got[1].X != 2.5 || *got[1].Y != 0.75 {
		t.Fatalf("second sample coordinates = %#v / %#v", got[1].X, got[1].Y)
	}
}

func TestClampListenMapLimit(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  int
	}{
		{name: "min", input: 1, want: 12},
		{name: "middle", input: 64, want: 64},
		{name: "max", input: 1000, want: 240},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampListenMapLimit(tt.input); got != tt.want {
				t.Fatalf("clampListenMapLimit(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
