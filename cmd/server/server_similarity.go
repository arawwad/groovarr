package main

import (
	"encoding/json"
	"net/http"

	"groovarr/internal/similarity"
)

func (s *Server) handleSimilarityHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.similarity == nil {
		s.sendJSON(w, map[string]interface{}{
			"similarity": map[string]interface{}{
				"defaultProvider":    similarity.ProviderLocal,
				"availableProviders": []string{similarity.ProviderLocal},
			},
		})
		return
	}
	s.sendJSON(w, map[string]interface{}{"similarity": s.similarity.Health(r.Context())})
}

func (s *Server) handleSimilarityTracks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.similarity == nil {
		http.Error(w, "similarity service unavailable", http.StatusServiceUnavailable)
		return
	}
	var req similarity.TrackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	response, err := s.similarity.SimilarTracks(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.sendJSON(w, map[string]interface{}{"similarityTracks": response})
}

func (s *Server) handleSimilaritySongsByArtist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.similarity == nil {
		http.Error(w, "similarity service unavailable", http.StatusServiceUnavailable)
		return
	}
	var req similarity.ArtistSongsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	response, err := s.similarity.SimilarSongsByArtist(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.sendJSON(w, map[string]interface{}{"similaritySongsByArtist": response})
}

func (s *Server) handleSimilarityArtists(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.similarity == nil {
		http.Error(w, "similarity service unavailable", http.StatusServiceUnavailable)
		return
	}
	var req similarity.ArtistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	response, err := s.similarity.SimilarArtists(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.sendJSON(w, map[string]interface{}{"similarityArtists": response})
}
