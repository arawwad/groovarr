package db

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBuildAlbumEmbeddingDocumentIncludesStableSemanticFields(t *testing.T) {
	year := 2001
	genre := "art pop, electronic"
	doc := buildAlbumEmbeddingDocument(Album{
		Name:       "Vespertine",
		ArtistName: "Björk",
		Year:       &year,
		Genre:      &genre,
		Metadata: map[string]interface{}{
			"musicbrainz": map[string]interface{}{
				"genres":         []string{"art pop", "chamber pop"},
				"tags":           []string{"intimate", "winter"},
				"primary_type":   "Album",
				"disambiguation": "studio album",
			},
			"lastfm": map[string]interface{}{
				"tags": []string{"glitch pop", "microbeats", "winter"},
			},
		},
	}, []string{"Hidden Place", "Cocoon", "Undo"})

	wantFragments := []string{
		"Album: Vespertine",
		"Artist: Björk",
		"Type: album in the user's library",
		"Year: 2001",
		"Decade: 2000s",
		"Genre: art pop, electronic",
		"MusicBrainz genres: art pop, chamber pop",
		"MusicBrainz tags: intimate, winter",
		"Last.fm tags: glitch pop, microbeats, winter",
		"Descriptors: intimate, winter, art pop, chamber pop, glitch pop, microbeats, electronic",
		"Mood and style: intimate, winter, art pop, chamber pop, glitch pop, microbeats, electronic",
		"Release group type: Album",
		"Disambiguation: studio album",
		"Tracks: Hidden Place, Cocoon, Undo",
	}
	for _, fragment := range wantFragments {
		if !containsLine(doc, fragment) {
			t.Fatalf("buildAlbumEmbeddingDocument() missing %q in %q", fragment, doc)
		}
	}
}

func TestBuildAlbumEmbeddingDocumentOmitsMissingOptionalFields(t *testing.T) {
	doc := buildAlbumEmbeddingDocument(Album{
		Name:       "Untrue",
		ArtistName: "Burial",
	}, nil)

	if containsLine(doc, "Year:") {
		t.Fatalf("buildAlbumEmbeddingDocument() unexpectedly included year in %q", doc)
	}
	if containsLine(doc, "Genre:") {
		t.Fatalf("buildAlbumEmbeddingDocument() unexpectedly included genre in %q", doc)
	}
	if containsLine(doc, "Tracks:") {
		t.Fatalf("buildAlbumEmbeddingDocument() unexpectedly included tracks in %q", doc)
	}
	if containsLine(doc, "Descriptors:") {
		t.Fatalf("buildAlbumEmbeddingDocument() unexpectedly included descriptors in %q", doc)
	}
}

func TestMergeAlbumEmbeddingDescriptorsDedupesAndCaps(t *testing.T) {
	got := mergeAlbumEmbeddingDescriptors(
		[]string{"ambient", "nocturnal", "ambient"},
		[]string{"electronic", "ambient pop"},
		[]string{"downtempo", "instrumental", "lush", "late night", "dreamy"},
	)
	want := []string{"ambient", "nocturnal", "electronic", "ambient pop", "downtempo", "instrumental", "lush", "late night"}
	if len(got) != len(want) {
		t.Fatalf("mergeAlbumEmbeddingDescriptors() len = %d, want %d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mergeAlbumEmbeddingDescriptors()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCanonicalAlbumGenresPrefersStructuredMetadata(t *testing.T) {
	genre := "ambient, electronic"
	got := canonicalAlbumGenres(Album{
		Genre: &genre,
		Metadata: map[string]interface{}{
			"musicbrainz": map[string]interface{}{
				"genres": []string{"ambient", "dream pop"},
			},
			"lastfm": map[string]interface{}{
				"tags": []string{"favourite albums", "2010s", "chillwave"},
			},
		},
	})

	want := []string{"ambient", "electronic", "dream pop"}
	if len(got) != len(want) {
		t.Fatalf("canonicalAlbumGenres() len = %d, want %d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("canonicalAlbumGenres()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCanonicalAlbumGenresFallsBackToFilteredLastFMTags(t *testing.T) {
	got := canonicalAlbumGenres(Album{
		Metadata: map[string]interface{}{
			"lastfm": map[string]interface{}{
				"tags": []string{"dream pop", "favorite albums", "2016", "electronic", "british"},
			},
		},
	})

	want := []string{"dream pop", "electronic"}
	if len(got) != len(want) {
		t.Fatalf("canonicalAlbumGenres() len = %d, want %d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("canonicalAlbumGenres()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestShouldPruneSnapshotRequiresFullFetch(t *testing.T) {
	tests := []struct {
		name   string
		total  int
		synced int
		want   bool
	}{
		{name: "full snapshot", total: 198, synced: 198, want: true},
		{name: "partial snapshot", total: 198, synced: 150, want: false},
		{name: "empty full snapshot", total: 0, synced: 0, want: true},
		{name: "oversized mismatch", total: 10, synced: 11, want: false},
	}

	for _, tt := range tests {
		if got := shouldPruneSnapshot(tt.total, tt.synced); got != tt.want {
			t.Fatalf("%s: shouldPruneSnapshot(%d, %d) = %v, want %v", tt.name, tt.total, tt.synced, got, tt.want)
		}
	}
}

func TestHasMusicBrainzMetadataRequiresMatchedStatus(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]interface{}
		want     bool
	}{
		{
			name:     "empty",
			metadata: nil,
			want:     false,
		},
		{
			name: "not found is retriable",
			metadata: map[string]interface{}{
				"musicbrainz": map[string]interface{}{"status": "not_found"},
			},
			want: false,
		},
		{
			name: "matched is terminal",
			metadata: map[string]interface{}{
				"musicbrainz": map[string]interface{}{"status": "matched"},
			},
			want: true,
		},
		{
			name: "wrong shape",
			metadata: map[string]interface{}{
				"musicbrainz": "matched",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		if got := hasMusicBrainzMetadata(tt.metadata); got != tt.want {
			t.Fatalf("%s: hasMusicBrainzMetadata() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestHasLastFMMetadataTreatsMatchedAndNotFoundAsTerminal(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]interface{}
		want     bool
	}{
		{
			name:     "empty",
			metadata: nil,
			want:     false,
		},
		{
			name: "not found is terminal",
			metadata: map[string]interface{}{
				"lastfm": map[string]interface{}{"status": "not_found"},
			},
			want: true,
		},
		{
			name: "matched is terminal",
			metadata: map[string]interface{}{
				"lastfm": map[string]interface{}{"status": "matched"},
			},
			want: true,
		},
		{
			name: "wrong shape",
			metadata: map[string]interface{}{
				"lastfm": "matched",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		if got := hasLastFMMetadata(tt.metadata); got != tt.want {
			t.Fatalf("%s: hasLastFMMetadata() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestEnrichAlbumsWithLastFMContinuesPastRequestFailure(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch requests {
		case 1:
			http.Error(w, "temporary failure", http.StatusBadGateway)
		case 2:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"album": {
					"name": "Second Album",
					"artist": "Second Artist",
					"tags": {
						"tag": [{"name": "shoegaze"}]
					}
				}
			}`))
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	s := &Syncer{
		lastFMEnabled:  true,
		lastFMAPIKey:   "test-key",
		lastFMBaseURL:  server.URL,
		lastFMAlbumCap: 5,
	}
	albums := []Album{
		{Name: "First Album", ArtistName: "First Artist", Metadata: map[string]interface{}{}},
		{Name: "Second Album", ArtistName: "Second Artist", Metadata: map[string]interface{}{}},
	}

	enriched, err := s.enrichAlbumsWithLastFM(context.Background(), albums)
	if err != nil {
		t.Fatalf("enrichAlbumsWithLastFM() error = %v", err)
	}
	if enriched != 1 {
		t.Fatalf("enrichAlbumsWithLastFM() enriched = %d, want 1", enriched)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if got := albums[0].Metadata["lastfm"]; got != nil {
		t.Fatalf("first album lastfm metadata = %#v, want nil", got)
	}
	secondMeta, ok := albums[1].Metadata["lastfm"].(map[string]interface{})
	if !ok {
		t.Fatalf("second album lastfm metadata type = %T, want map[string]interface{}", albums[1].Metadata["lastfm"])
	}
	if got := secondMeta["status"]; got != "matched" {
		t.Fatalf("second album status = %#v, want matched", got)
	}
}

func TestFetchEmbeddingsBatchedWithoutEndpointReturnsEmptyEmbeddings(t *testing.T) {
	s := &Syncer{}
	got, err := s.fetchEmbeddingsBatched(context.Background(), []string{"artist - album"}, 10)
	if err != nil {
		t.Fatalf("fetchEmbeddingsBatched() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("fetchEmbeddingsBatched() len = %d, want 1", len(got))
	}
	if got[0] != nil {
		t.Fatalf("fetchEmbeddingsBatched()[0] = %#v, want nil", got[0])
	}
}

func TestFetchLastFMAlbumMetadataParsesTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("method"); got != "album.getInfo" {
			t.Fatalf("method = %q, want album.getInfo", got)
		}
		if got := r.URL.Query().Get("artist"); got != "Björk" {
			t.Fatalf("artist = %q, want Björk", got)
		}
		if got := r.URL.Query().Get("album"); got != "Vespertine" {
			t.Fatalf("album = %q, want Vespertine", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"album": {
				"name": "Vespertine",
				"artist": "Björk",
				"mbid": "album-mbid",
				"url": "https://last.fm/music/Bj%C3%B6rk/Vespertine",
				"listeners": "1234",
				"playcount": "5678",
				"tags": {
					"tag": [
						{"name": "glitch pop"},
						{"name": "microbeats"},
						{"name": "glitch pop"}
					]
				},
				"wiki": {
					"summary": "icy and intimate",
					"published": "01 Jan 2002, 00:00"
				}
			}
		}`))
	}))
	defer server.Close()

	s := &Syncer{
		lastFMEnabled: true,
		lastFMAPIKey:  "test-key",
		lastFMBaseURL: server.URL,
	}
	meta, err := s.fetchLastFMAlbumMetadata(context.Background(), Album{
		Name:       "Vespertine",
		ArtistName: "Björk",
	})
	if err != nil {
		t.Fatalf("fetchLastFMAlbumMetadata() error = %v", err)
	}
	if got := meta["status"]; got != "matched" {
		t.Fatalf("status = %#v, want matched", got)
	}
	tags, ok := meta["tags"].([]string)
	if !ok {
		t.Fatalf("tags type = %T, want []string", meta["tags"])
	}
	want := []string{"glitch pop", "microbeats"}
	if len(tags) != len(want) {
		t.Fatalf("tags len = %d, want %d (%#v)", len(tags), len(want), tags)
	}
	for i := range want {
		if tags[i] != want[i] {
			t.Fatalf("tags[%d] = %q, want %q", i, tags[i], want[i])
		}
	}
}

func TestExtractLastFMTagsHandlesLooseShapes(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want []string
	}{
		{
			name: "nested list",
			in: map[string]interface{}{
				"tag": []interface{}{
					map[string]interface{}{"name": "dream pop"},
					map[string]interface{}{"name": "electronic"},
				},
			},
			want: []string{"dream pop", "electronic"},
		},
		{
			name: "single string",
			in: map[string]interface{}{
				"tag": "ambient",
			},
			want: []string{"ambient"},
		},
		{
			name: "empty string",
			in: map[string]interface{}{
				"tag": "",
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		got := extractLastFMTags(tt.in)
		if len(got) != len(tt.want) {
			t.Fatalf("%s: len = %d, want %d (%#v)", tt.name, len(got), len(tt.want), got)
		}
		for i := range tt.want {
			if got[i] != tt.want[i] {
				t.Fatalf("%s: got[%d] = %q, want %q", tt.name, i, got[i], tt.want[i])
			}
		}
	}
}

func TestNormalizeMusicBrainzAlbumTitle(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "remaster in parens",
			in:   "Black Tie White Noise (2021 Remaster)",
			want: "Black Tie White Noise",
		},
		{
			name: "bracketed release note",
			in:   "Pin Ups [1990 Rykodisc Remastered CD]",
			want: "Pin Ups",
		},
		{
			name: "plain deluxe suffix",
			in:   "Circuital Deluxe Edition",
			want: "Circuital",
		},
		{
			name: "decorated dash suffix",
			in:   "Discovery - Anniversary Edition",
			want: "Discovery",
		},
		{
			name: "unchanged clean title",
			in:   "Ummagumma",
			want: "Ummagumma",
		},
	}

	for _, tt := range tests {
		if got := normalizeMusicBrainzAlbumTitle(tt.in); got != tt.want {
			t.Fatalf("%s: normalizeMusicBrainzAlbumTitle(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

func containsLine(doc, want string) bool {
	for _, line := range splitLines(doc) {
		if line == want {
			return true
		}
	}
	return false
}

func splitLines(doc string) []string {
	lines := []string{}
	start := 0
	for i := 0; i < len(doc); i++ {
		if doc[i] == '\n' {
			lines = append(lines, doc[start:i])
			start = i + 1
		}
	}
	lines = append(lines, doc[start:])
	return lines
}
