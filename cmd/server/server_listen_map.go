package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type audioMuseMapSample struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	ArtistName string   `json:"artistName"`
	AlbumName  string   `json:"albumName,omitempty"`
	X          *float64 `json:"x,omitempty"`
	Y          *float64 `json:"y,omitempty"`
}

func (c *audioMuseListenClient) mapProjection(ctx context.Context, percent int) (map[string]interface{}, error) {
	params := url.Values{}
	params.Set("percent", stringInt(percent))
	var payload map[string]interface{}
	if err := c.getJSON(ctx, "/api/map?"+params.Encode(), &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (s *Server) handleListenMap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	client := newAudioMuseListenClient()
	if client == nil {
		s.sendJSON(w, map[string]interface{}{
			"listenMap": map[string]interface{}{
				"configured": false,
				"ready":      false,
				"projection": "none",
				"percent":    20,
				"itemCount":  0,
				"items":      []audioMuseMapSample{},
				"message":    "Sonic analysis is disabled.",
			},
		})
		return
	}

	percent := envInt("AUDIOMUSE_MAP_PREVIEW_PERCENT", 20)
	if raw := strings.TrimSpace(r.URL.Query().Get("percent")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			percent = parsed
		}
	}
	if percent < 1 {
		percent = 1
	}
	if percent > 100 {
		percent = 100
	}
	limit := envInt("AUDIOMUSE_MAP_PREVIEW_LIMIT", 80)
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	limit = clampListenMapLimit(limit)

	mapPayload, err := client.mapProjection(r.Context(), percent)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	task, _ := client.currentTask(r.Context())

	projection := strings.TrimSpace(firstTaskString(mapPayload, "projection"))
	if projection == "" {
		projection = "none"
	}
	items := mapAudioMuseMapSamples(mapPayload, limit)
	ready := len(items) > 0 && projection != "none"
	message := ""
	switch {
	case ready:
		message = "Map points are available for exploration."
	case task != nil && strings.Contains(strings.ToLower(task.TaskType), "analysis") && task.Status == "PROGRESS":
		message = "Sonic analysis is still running, so the map is not populated yet."
	default:
		message = "No map points are available yet."
	}

	s.sendJSON(w, map[string]interface{}{
		"listenMap": map[string]interface{}{
			"configured":  true,
			"ready":       ready,
			"projection":  projection,
			"percent":     percent,
			"sampleLimit": limit,
			"itemCount":   len(items),
			"items":       items,
			"task":        task,
			"message":     message,
		},
	})
}

func mapAudioMuseMapSamples(raw map[string]interface{}, limit int) []audioMuseMapSample {
	list, ok := raw["items"].([]interface{})
	if !ok || len(list) == 0 {
		return []audioMuseMapSample{}
	}
	if limit <= 0 || limit > len(list) {
		limit = len(list)
	}
	results := make([]audioMuseMapSample, 0, limit)
	for _, item := range list[:limit] {
		row, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		sample := audioMuseMapSample{
			ID:         strings.TrimSpace(firstTaskString(row, "item_id", "id")),
			Title:      strings.TrimSpace(firstTaskString(row, "title", "name")),
			ArtistName: strings.TrimSpace(firstTaskString(row, "author", "artist", "artist_name")),
			AlbumName:  strings.TrimSpace(firstTaskString(row, "album", "album_name")),
		}
		if coords, ok := row["embedding_2d"].([]interface{}); ok && len(coords) >= 2 {
			if x, ok := floatValue(coords[0]); ok {
				sample.X = &x
			}
			if y, ok := floatValue(coords[1]); ok {
				sample.Y = &y
			}
		}
		results = append(results, sample)
	}
	return results
}

func clampListenMapLimit(limit int) int {
	if limit < 12 {
		return 12
	}
	if limit > 240 {
		return 240
	}
	return limit
}

func floatValue(raw interface{}) (float64, bool) {
	switch value := raw.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case json.Number:
		parsed, err := value.Float64()
		if err == nil {
			return parsed, true
		}
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err == nil {
			return parsed, true
		}
	default:
		return 0, false
	}
	return 0, false
}

func stringInt(value int) string {
	return strconv.Itoa(value)
}
