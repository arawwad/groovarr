package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"groovarr/internal/similarity"
)

type listenNeighborhoodRequest struct {
	SeedTrackID string `json:"seedTrackId"`
	Limit       int    `json:"limit"`
}

func (s *Server) handleListenNeighborhood(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.similarity == nil || s.dbClient == nil || s.resolver == nil {
		http.Error(w, "exploration service unavailable", http.StatusServiceUnavailable)
		return
	}

	var req listenNeighborhoodRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	seedTrackID := strings.TrimSpace(req.SeedTrackID)
	if seedTrackID == "" {
		http.Error(w, "seedTrackId is required", http.StatusBadRequest)
		return
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 6
	}

	seedTrack, err := s.dbClient.GetTrackByID(r.Context(), seedTrackID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if seedTrack == nil {
		http.Error(w, "seed track not found", http.StatusNotFound)
		return
	}

	artistResponse, err := s.similarity.SimilarArtists(r.Context(), similarity.ArtistRequest{
		SeedArtistName: seedTrack.ArtistName,
		Provider:       similarity.ProviderHybrid,
		Limit:          limit,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	seedAlbumName := ""
	seedAlbumArtist := ""
	if albumID := strings.TrimSpace(seedTrack.AlbumID); albumID != "" {
		album, albumErr := s.dbClient.GetAlbumByID(r.Context(), albumID)
		if albumErr != nil {
			http.Error(w, albumErr.Error(), http.StatusInternalServerError)
			return
		}
		if album != nil {
			seedAlbumName = strings.TrimSpace(album.Name)
			seedAlbumArtist = strings.TrimSpace(album.ArtistName)
		}
	}

	type albumResult struct {
		ID         string  `json:"id"`
		Name       string  `json:"name"`
		ArtistName string  `json:"artistName"`
		Rating     int     `json:"rating,omitempty"`
		PlayCount  int     `json:"playCount,omitempty"`
		LastPlayed *string `json:"lastPlayed,omitempty"`
		Year       *int    `json:"year,omitempty"`
		Genre      *string `json:"genre,omitempty"`
	}

	albumsPayload := make([]albumResult, 0)
	if seedAlbumName != "" {
		albums, albumErr := s.resolver.Query().SimilarAlbums(r.Context(), seedAlbumName, intPtr(limit))
		if albumErr != nil {
			http.Error(w, albumErr.Error(), http.StatusInternalServerError)
			return
		}
		albumsPayload = make([]albumResult, 0, len(albums))
		for _, album := range albums {
			albumsPayload = append(albumsPayload, albumResult{
				ID:         album.ID,
				Name:       album.Name,
				ArtistName: album.ArtistName,
				Rating:     album.Rating,
				PlayCount:  album.PlayCount,
				LastPlayed: album.LastPlayed,
				Year:       album.Year,
				Genre:      album.Genre,
			})
		}
	}

	artistPayload := make([]map[string]interface{}, 0, len(artistResponse.Results))
	for _, artist := range artistResponse.Results {
		artistPayload = append(artistPayload, map[string]interface{}{
			"id":           artist.ID,
			"name":         artist.Name,
			"rating":       artist.Rating,
			"playCount":    artist.PlayCount,
			"score":        artist.Score,
			"sources":      artist.Sources,
			"sourceScores": artist.SourceScores,
		})
	}

	s.sendJSON(w, map[string]interface{}{
		"listenNeighborhood": map[string]interface{}{
			"seed": map[string]interface{}{
				"trackId":         seedTrack.ID,
				"title":           seedTrack.Title,
				"artistName":      seedTrack.ArtistName,
				"albumId":         seedTrack.AlbumID,
				"albumName":       seedAlbumName,
				"albumArtistName": seedAlbumArtist,
			},
			"artists": map[string]interface{}{
				"provider": artistResponse.Provider,
				"results":  artistPayload,
			},
			"albums": map[string]interface{}{
				"seedAlbumName": seedAlbumName,
				"results":       albumsPayload,
			},
		},
	})
}
