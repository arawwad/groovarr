package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeListenTextSearchLimit(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  int
	}{
		{name: "default", input: 0, want: 8},
		{name: "passthrough", input: 6, want: 6},
		{name: "max", input: 99, want: 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeListenTextSearchLimit(tt.input); got != tt.want {
				t.Fatalf("normalizeListenTextSearchLimit(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestMapAudioMuseCLAPMatches(t *testing.T) {
	similarity := 0.932
	got := mapAudioMuseCLAPMatches([]audioMuseCLAPSearchTrack{
		{ItemID: " track-1 ", Title: " Alpha ", Author: " Artist One ", Album: " Album One ", Similarity: &similarity},
		{ItemID: "track-2", Title: "Beta", Author: "Artist Two"},
	})
	if len(got) != 2 {
		t.Fatalf("mapAudioMuseCLAPMatches() len = %d, want 2", len(got))
	}
	if got[0].ID != "track-1" || got[0].Title != "Alpha" || got[0].ArtistName != "Artist One" || got[0].AlbumName != "Album One" {
		t.Fatalf("first match = %#v", got[0])
	}
	if got[0].Similarity == nil || *got[0].Similarity != similarity {
		t.Fatalf("first match similarity = %#v", got[0].Similarity)
	}
	if got[1].AlbumName != "" {
		t.Fatalf("second match album = %q, want empty", got[1].AlbumName)
	}
}

func TestHandleListenTextSearch(t *testing.T) {
	audioMuse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/clap/search" {
			t.Fatalf("path = %s, want /api/clap/search", r.URL.Path)
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["query"] != "smoky and nocturnal" {
			t.Fatalf("query = %#v", payload["query"])
		}
		if payload["limit"] != float64(5) {
			t.Fatalf("limit = %#v", payload["limit"])
		}
		_ = json.NewEncoder(w).Encode(audioMuseCLAPSearchResponse{
			Query: "smoky and nocturnal",
			Results: []audioMuseCLAPSearchTrack{
				{ItemID: "track-9", Title: "Teardrop", Author: "Massive Attack", Album: "Mezzanine"},
			},
			Count: 1,
		})
	}))
	defer audioMuse.Close()

	t.Setenv("AUDIOMUSE_URL", audioMuse.URL)
	server := &Server{}
	body := bytes.NewBufferString(`{"queryText":"smoky and nocturnal","limit":5}`)
	req := httptest.NewRequest(http.MethodPost, "/api/listen/text-search", body)
	rr := httptest.NewRecorder()

	server.handleListenTextSearch(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var payload map[string]map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	result := payload["listenTextSearch"]
	if result["queryText"] != "smoky and nocturnal" {
		t.Fatalf("queryText = %#v", result["queryText"])
	}
	if result["count"] != float64(1) {
		t.Fatalf("count = %#v", result["count"])
	}
	matches, ok := result["matches"].([]interface{})
	if !ok || len(matches) != 1 {
		t.Fatalf("matches = %#v", result["matches"])
	}
}
