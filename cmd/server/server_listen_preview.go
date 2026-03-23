package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"groovarr/internal/similarity"
)

type listenTrackSearchResult struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	ArtistName string `json:"artistName"`
	AlbumID    string `json:"albumId,omitempty"`
	PlayCount  int    `json:"playCount,omitempty"`
}

type listenPreviewRequest struct {
	SeedTrackID string `json:"seedTrackId"`
	Limit       int    `json:"limit"`
}

func (s *Server) handleListenTrackSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.dbClient == nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		s.sendJSON(w, map[string]interface{}{"tracks": []listenTrackSearchResult{}})
		return
	}
	tracks, err := s.dbClient.GetTracks(r.Context(), 8, true, map[string]interface{}{
		"queryText": query,
		"sortBy":    "lastPlayed",
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	results := make([]listenTrackSearchResult, 0, len(tracks))
	for _, track := range tracks {
		results = append(results, listenTrackSearchResult{
			ID:         track.ID,
			Title:      track.Title,
			ArtistName: track.ArtistName,
			AlbumID:    track.AlbumID,
			PlayCount:  track.PlayCount,
		})
	}
	s.sendJSON(w, map[string]interface{}{"tracks": results})
}

func (s *Server) handleListenPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.similarity == nil {
		http.Error(w, "similarity service unavailable", http.StatusServiceUnavailable)
		return
	}
	var req listenPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.SeedTrackID) == "" {
		http.Error(w, "seedTrackId is required", http.StatusBadRequest)
		return
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 8
	}

	activeContext, err := s.similarity.GetListeningContext(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	current, err := s.similarity.SimilarTracks(r.Context(), similarity.TrackRequest{
		SeedTrackID: req.SeedTrackID,
		Provider:    similarity.ProviderHybrid,
		Limit:       limit,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defaultResponse, err := s.similarity.SimilarTracks(r.Context(), similarity.TrackRequest{
		SeedTrackID:         req.SeedTrackID,
		Provider:            similarity.ProviderHybrid,
		Limit:               limit,
		Mode:                similarity.ModeAdjacent,
		IgnoreStoredContext: true,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.sendJSON(w, map[string]interface{}{
		"listenPreview": map[string]interface{}{
			"context": activeContext,
			"current": current,
			"default": defaultResponse,
		},
	})
}
