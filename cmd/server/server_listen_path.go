package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type audioMusePathTrack struct {
	ItemID          string    `json:"item_id"`
	Title           string    `json:"title"`
	Author          string    `json:"author"`
	Album           string    `json:"album"`
	EmbeddingVector []float64 `json:"embedding_vector,omitempty"`
}

type listenSongPathSearchTrack struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	ArtistName string `json:"artistName"`
	AlbumName  string `json:"albumName,omitempty"`
}

type listenSongPathRequest struct {
	StartTrackID  string `json:"startTrackId"`
	EndTrackID    string `json:"endTrackId"`
	MaxSteps      int    `json:"maxSteps"`
	KeepExactSize bool   `json:"keepExactSize"`
}

func (c *audioMuseListenClient) searchPathTracks(ctx context.Context, query string, limit int) ([]audioMusePathTrack, error) {
	params := url.Values{}
	params.Set("search_query", strings.TrimSpace(query))
	params.Set("start", "0")
	params.Set("end", fmt.Sprintf("%d", limit))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/search_tracks?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("sonic analysis service returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tracks []audioMusePathTrack
	if err := json.NewDecoder(resp.Body).Decode(&tracks); err != nil {
		return nil, err
	}
	return tracks, nil
}

func (c *audioMuseListenClient) findSongPath(ctx context.Context, startTrackID, endTrackID string, maxSteps int, keepExactSize bool) ([]audioMusePathTrack, error) {
	params := url.Values{}
	params.Set("start_song_id", strings.TrimSpace(startTrackID))
	params.Set("end_song_id", strings.TrimSpace(endTrackID))
	params.Set("max_steps", fmt.Sprintf("%d", maxSteps))
	if keepExactSize {
		params.Set("path_fix_size", "true")
	} else {
		params.Set("path_fix_size", "false")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/find_path?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err == nil {
			if text := strings.TrimSpace(fmt.Sprintf("%v", payload["error"])); text != "" && text != "<nil>" {
				return nil, fmt.Errorf("%s", text)
			}
		}
		return nil, fmt.Errorf("sonic analysis service returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Path []audioMusePathTrack `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Path, nil
}

func (s *Server) handleListenPathSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	client := newAudioMuseListenClient()
	if client == nil {
		http.Error(w, "sonic analysis is disabled", http.StatusServiceUnavailable)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		http.Error(w, "q is required", http.StatusBadRequest)
		return
	}

	tracks, err := client.searchPathTracks(r.Context(), query, 8)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	message := ""
	if len(tracks) == 0 {
		task, taskErr := client.currentTask(r.Context())
		if taskErr == nil && task != nil && strings.Contains(strings.ToLower(task.TaskType), "analysis") && task.Status == "PROGRESS" {
			message = "Sonic analysis is still running, so Song Path search may stay empty until more of the library is indexed."
		}
	}
	results := make([]listenSongPathSearchTrack, 0, len(tracks))
	for _, track := range tracks {
		results = append(results, listenSongPathSearchTrack{
			ID:         strings.TrimSpace(track.ItemID),
			Title:      strings.TrimSpace(track.Title),
			ArtistName: strings.TrimSpace(track.Author),
			AlbumName:  strings.TrimSpace(track.Album),
		})
	}

	s.sendJSON(w, map[string]interface{}{
		"songPathSearch": map[string]interface{}{
			"tracks":  results,
			"message": message,
		},
	})
}

func (s *Server) handleListenSongPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	client := newAudioMuseListenClient()
	if client == nil {
		http.Error(w, "sonic analysis is disabled", http.StatusServiceUnavailable)
		return
	}

	var req listenSongPathRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	startTrackID := strings.TrimSpace(req.StartTrackID)
	endTrackID := strings.TrimSpace(req.EndTrackID)
	if startTrackID == "" || endTrackID == "" {
		http.Error(w, "startTrackId and endTrackId are required", http.StatusBadRequest)
		return
	}
	maxSteps := req.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 25
	}

	path, err := client.findSongPath(r.Context(), startTrackID, endTrackID, maxSteps, req.KeepExactSize)
	if err != nil {
		if isAudioMuseNoPathError(err) {
			s.sendJSON(w, map[string]interface{}{
				"listenSongPath": map[string]interface{}{
					"path":          []map[string]interface{}{},
					"pathLength":    0,
					"maxSteps":      maxSteps,
					"keepExactSize": req.KeepExactSize,
					"message":       err.Error(),
				},
			})
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	results := make([]map[string]interface{}, 0, len(path))
	for index, track := range path {
		results = append(results, map[string]interface{}{
			"position":   index + 1,
			"id":         strings.TrimSpace(track.ItemID),
			"title":      strings.TrimSpace(track.Title),
			"artistName": strings.TrimSpace(track.Author),
			"albumName":  strings.TrimSpace(track.Album),
		})
	}

	s.sendJSON(w, map[string]interface{}{
		"listenSongPath": map[string]interface{}{
			"path":          results,
			"pathLength":    len(results),
			"maxSteps":      maxSteps,
			"keepExactSize": req.KeepExactSize,
			"message":       "",
		},
	})
}

func isAudioMuseNoPathError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(err.Error())), "no path found")
}
