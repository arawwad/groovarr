package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type audioMuseListenClient struct {
	baseURL string
	client  *http.Client
}

type audioMuseTaskView struct {
	TaskID        string                 `json:"taskId,omitempty"`
	TaskType      string                 `json:"taskType,omitempty"`
	Status        string                 `json:"status,omitempty"`
	Progress      interface{}            `json:"progress,omitempty"`
	StatusMessage string                 `json:"statusMessage,omitempty"`
	Details       map[string]interface{} `json:"details,omitempty"`
}

type audioMusePlaylistSong struct {
	Title  string `json:"title"`
	Author string `json:"author"`
}

type audioMuseClusterPlaylist struct {
	Key       string                  `json:"key,omitempty"`
	Name      string                  `json:"name"`
	Subtitle  string                  `json:"subtitle,omitempty"`
	SongCount int                     `json:"songCount"`
	Songs     []audioMusePlaylistSong `json:"songs"`
}

type audioMuseStartResponse struct {
	TaskID   string `json:"task_id"`
	TaskType string `json:"task_type"`
	Message  string `json:"message"`
}

func newAudioMuseListenClient() *audioMuseListenClient {
	if !sonicAnalysisEnabled() {
		return nil
	}
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("AUDIOMUSE_URL")), "/")
	if baseURL == "" {
		return nil
	}
	return &audioMuseListenClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 8 * time.Second},
	}
}

func sonicAnalysisEnabled() bool {
	raw := strings.TrimSpace(os.Getenv("SONIC_ANALYSIS_ENABLED"))
	if raw == "" {
		return true
	}
	switch strings.ToLower(raw) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func (c *audioMuseListenClient) getJSON(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("sonic analysis service returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *audioMuseListenClient) postJSON(ctx context.Context, path string, payload interface{}, out interface{}) error {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("sonic analysis service returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *audioMuseListenClient) currentTask(ctx context.Context) (*audioMuseTaskView, error) {
	var active map[string]interface{}
	if err := c.getJSON(ctx, "/api/active_tasks", &active); err == nil && len(active) > 0 {
		return mapAudioMuseTask(active), nil
	}
	var last map[string]interface{}
	if err := c.getJSON(ctx, "/api/last_task", &last); err != nil {
		return nil, err
	}
	if len(last) == 0 {
		return nil, nil
	}
	return mapAudioMuseTask(last), nil
}

func (c *audioMuseListenClient) playlists(ctx context.Context) ([]audioMuseClusterPlaylist, error) {
	var raw map[string][]audioMusePlaylistSong
	if err := c.getJSON(ctx, "/api/playlists", &raw); err != nil {
		return nil, err
	}
	playlists := make([]audioMuseClusterPlaylist, 0, len(raw))
	for name, songs := range raw {
		displayName, subtitle := formatClusterSceneName(name)
		playlists = append(playlists, audioMuseClusterPlaylist{
			Key:       strings.TrimSpace(name),
			Name:      displayName,
			Subtitle:  subtitle,
			SongCount: len(songs),
			Songs:     songs,
		})
	}
	return playlists, nil
}

func formatClusterSceneName(raw string) (string, string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "Untitled Scene", ""
	}
	parts := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == '_' || r == '-' || r == '/'
	})
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		token := normalizeSceneToken(part)
		if token == "" || token == "Automatic" {
			continue
		}
		tokens = append(tokens, token)
	}
	if len(tokens) == 0 {
		return "Untitled Scene", ""
	}

	tempoIndex := -1
	for index, token := range tokens {
		if isSceneTempoToken(token) {
			tempoIndex = index
			break
		}
	}

	styleTokens := dedupeSceneTokens(tokens)
	tempoToken := ""
	moodTokens := []string{}
	if tempoIndex >= 0 {
		styleTokens = dedupeSceneTokens(tokens[:tempoIndex])
		tempoToken = formatSceneTempoToken(tokens[tempoIndex])
		moodTokens = dedupeSceneTokens(tokens[tempoIndex+1:])
	}
	if len(styleTokens) == 0 {
		styleTokens = dedupeSceneTokens(tokens)
	}

	nameParts := make([]string, 0, 3)
	if len(styleTokens) > 0 {
		nameParts = append(nameParts, strings.Join(styleTokens, " / "))
	}
	if tempoToken != "" {
		nameParts = append(nameParts, tempoToken)
	}
	displayName := strings.Join(nameParts, " • ")
	if displayName == "" {
		displayName = strings.Join(tokens, " / ")
	}

	subtitle := ""
	if len(moodTokens) > 0 {
		subtitle = strings.Join(moodTokens, ", ")
	}
	return displayName, subtitle
}

func normalizeSceneToken(raw string) string {
	token := strings.TrimSpace(raw)
	if token == "" {
		return ""
	}
	if len(token) <= 4 && token == strings.ToUpper(token) {
		return token
	}
	lower := strings.ToLower(token)
	words := strings.Fields(lower)
	if len(words) == 0 {
		words = []string{lower}
	}
	for index, word := range words {
		switch word {
		case "rnb":
			words[index] = "R&B"
		case "hiphop":
			words[index] = "Hip-Hop"
		default:
			words[index] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}

func isSceneTempoToken(token string) bool {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "slow", "medium", "fast":
		return true
	default:
		return false
	}
}

func formatSceneTempoToken(token string) string {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "slow":
		return "Slow Tempo"
	case "medium":
		return "Mid-Tempo"
	case "fast":
		return "Fast Tempo"
	default:
		return ""
	}
}

func dedupeSceneTokens(tokens []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(tokens))
	for _, token := range tokens {
		normalized := strings.TrimSpace(token)
		if normalized == "" {
			continue
		}
		key := strings.ToLower(normalized)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func (c *audioMuseListenClient) config(ctx context.Context) (map[string]interface{}, error) {
	var raw map[string]interface{}
	if err := c.getJSON(ctx, "/api/config", &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *audioMuseListenClient) startClustering(ctx context.Context, payload map[string]interface{}) (*audioMuseStartResponse, error) {
	var result audioMuseStartResponse
	if err := c.postJSON(ctx, "/api/clustering/start", payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func mapAudioMuseTask(raw map[string]interface{}) *audioMuseTaskView {
	if len(raw) == 0 {
		return nil
	}
	task := &audioMuseTaskView{
		TaskID:   firstTaskString(raw, "task_id", "taskId"),
		TaskType: firstTaskString(raw, "task_type", "task_type_from_db", "taskType"),
		Status:   strings.ToUpper(firstTaskString(raw, "status", "state")),
		Progress: raw["progress"],
	}
	if details, ok := raw["details"].(map[string]interface{}); ok {
		task.Details = details
		if message := firstTaskString(details, "status_message", "message"); message != "" {
			task.StatusMessage = message
		}
	}
	if task.StatusMessage == "" {
		task.StatusMessage = firstTaskString(raw, "message")
	}
	return task
}

func firstTaskString(raw map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			if text := strings.TrimSpace(fmt.Sprintf("%v", value)); text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func (s *Server) handleListenClusters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	client := newAudioMuseListenClient()
	if client == nil {
		s.sendJSON(w, map[string]interface{}{
			"listenClusters": map[string]interface{}{
				"configured": false,
				"ready":      false,
				"playlists":  []audioMuseClusterPlaylist{},
				"message":    "Sonic analysis is disabled.",
			},
		})
		return
	}

	task, taskErr := client.currentTask(r.Context())
	playlists, playlistsErr := client.playlists(r.Context())
	if taskErr != nil && playlistsErr != nil {
		http.Error(w, taskErr.Error(), http.StatusBadGateway)
		return
	}
	if len(playlists) > 0 {
		if renamed, err := syncScenePlaylistsInNavidrome(r.Context(), playlists); err != nil {
			log.Warn().Err(err).Msg("Failed to normalize sonic scene playlist names in Navidrome")
		} else if renamed > 0 {
			log.Info().Int("renamed", renamed).Msg("Normalized sonic scene playlist names in Navidrome")
		}
	}

	message := ""
	ready := len(playlists) > 0
	canStart := true
	switch {
	case ready:
		message = fmt.Sprintf("Loaded %d sonic scene playlist(s).", len(playlists))
		canStart = false
	case task != nil && strings.Contains(strings.ToLower(task.TaskType), "analysis") && task.Status == "PROGRESS":
		message = "Sonic analysis is still running, so scene playlists are not ready yet."
		canStart = false
	case task != nil && strings.Contains(strings.ToLower(task.TaskType), "cluster") && task.Status == "PROGRESS":
		message = "Scene clustering is running now."
		canStart = false
	default:
		message = "No sonic scene playlists are available yet."
	}

	s.sendJSON(w, map[string]interface{}{
		"listenClusters": map[string]interface{}{
			"configured": true,
			"ready":      ready,
			"canStart":   canStart,
			"task":       task,
			"playlists":  playlists,
			"message":    message,
		},
	})
}

func (s *Server) handleListenClustersStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	client := newAudioMuseListenClient()
	if client == nil {
		http.Error(w, "sonic analysis is disabled", http.StatusServiceUnavailable)
		return
	}
	config, err := client.config(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	result, err := client.startClustering(r.Context(), config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.sendJSON(w, map[string]interface{}{
		"listenClustersStart": map[string]interface{}{
			"taskId":   strings.TrimSpace(result.TaskID),
			"taskType": strings.TrimSpace(result.TaskType),
			"message":  strings.TrimSpace(result.Message),
		},
	})
}
