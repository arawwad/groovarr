package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

type listenTextSearchRequest struct {
	QueryText string `json:"queryText"`
	Limit     int    `json:"limit"`
}

type listenTextSearchMatch struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	ArtistName string   `json:"artistName"`
	AlbumName  string   `json:"albumName,omitempty"`
	Similarity *float64 `json:"similarity,omitempty"`
}

func (s *Server) handleListenTextSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	client := newAudioMuseListenClient()
	if client == nil {
		s.sendJSON(w, map[string]interface{}{
			"listenTextSearch": map[string]interface{}{
				"configured": false,
				"queryText":  "",
				"count":      0,
				"matches":    []listenTextSearchMatch{},
				"message":    "AudioMuse is not configured.",
			},
		})
		return
	}

	var req listenTextSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	queryText := strings.TrimSpace(req.QueryText)
	if queryText == "" {
		http.Error(w, "queryText is required", http.StatusBadRequest)
		return
	}

	result, err := client.clapSearchTracks(r.Context(), queryText, normalizeListenTextSearchLimit(req.Limit))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	matches := mapAudioMuseCLAPMatches(result.Results)
	message := "No AudioMuse text-to-sound matches found yet."
	if len(matches) > 0 {
		message = "AudioMuse vibe search is ready."
	}
	s.sendJSON(w, map[string]interface{}{
		"listenTextSearch": map[string]interface{}{
			"configured": true,
			"queryText":  queryText,
			"count":      len(matches),
			"matches":    matches,
			"message":    message,
		},
	})
}

func normalizeListenTextSearchLimit(limit int) int {
	if limit <= 0 {
		return 8
	}
	if limit > 20 {
		return 20
	}
	return limit
}

func mapAudioMuseCLAPMatches(results []audioMuseCLAPSearchTrack) []listenTextSearchMatch {
	matches := make([]listenTextSearchMatch, 0, len(results))
	for _, result := range results {
		match := listenTextSearchMatch{
			ID:         strings.TrimSpace(result.ItemID),
			Title:      strings.TrimSpace(result.Title),
			ArtistName: strings.TrimSpace(result.Author),
			AlbumName:  strings.TrimSpace(result.Album),
			Similarity: result.Similarity,
		}
		matches = append(matches, match)
	}
	return matches
}
