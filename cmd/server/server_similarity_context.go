package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type listeningContextRequest struct {
	Mode       string `json:"mode"`
	Mood       string `json:"mood"`
	TTLMinutes int    `json:"ttlMinutes"`
	Source     string `json:"source"`
}

func (s *Server) handleSimilarityContext(w http.ResponseWriter, r *http.Request) {
	if s.similarity == nil {
		http.Error(w, "similarity service unavailable", http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		contextValue, err := s.similarity.GetListeningContext(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.sendJSON(w, map[string]interface{}{"similarityContext": contextValue})
	case http.MethodPost:
		var req listeningContextRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		ttl := time.Duration(req.TTLMinutes) * time.Minute
		if req.TTLMinutes < 0 {
			http.Error(w, "ttlMinutes must be zero or greater", http.StatusBadRequest)
			return
		}
		source := strings.TrimSpace(req.Source)
		if source == "" {
			source = "listen-ui"
		}
		contextValue, err := s.similarity.SetListeningContext(r.Context(), req.Mode, req.Mood, ttl, source)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.sendJSON(w, map[string]interface{}{"similarityContext": contextValue})
	case http.MethodDelete:
		if err := s.similarity.DeleteListeningContext(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.sendJSON(w, map[string]interface{}{"deleted": true})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
