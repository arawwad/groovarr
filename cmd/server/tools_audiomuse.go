package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"groovarr/internal/discovery"
	"groovarr/internal/similarity"

	"github.com/jackc/pgx/v5/pgxpool"
)

type audioMuseCLAPSearchTrack struct {
	ItemID     string   `json:"item_id"`
	Title      string   `json:"title"`
	Author     string   `json:"author"`
	Album      string   `json:"album,omitempty"`
	Similarity *float64 `json:"similarity,omitempty"`
}

type audioMuseCLAPSearchResponse struct {
	Query   string                     `json:"query"`
	Results []audioMuseCLAPSearchTrack `json:"results"`
	Count   int                        `json:"count"`
}

type audioMuseTrackProfile struct {
	ItemID        string
	Title         string
	Author        string
	Album         string
	Tempo         *float64
	Key           string
	Scale         string
	MoodVector    string
	Energy        *float64
	OtherFeatures string
}

type audioMuseSimilarTrack struct {
	ItemID   string  `json:"item_id"`
	Title    string  `json:"title"`
	Author   string  `json:"author"`
	Album    string  `json:"album,omitempty"`
	Distance float64 `json:"distance"`
}

var audioMuseScoreByIDFetcher = defaultAudioMuseScoreByIDFetcher
var sceneExpandSimilarTracksFetcher = defaultSceneExpandSimilarTracksFetcher

func (c *audioMuseListenClient) clapSearchTracks(ctx context.Context, query string, limit int) (*audioMuseCLAPSearchResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("sonic analysis is disabled")
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	var result audioMuseCLAPSearchResponse
	if err := c.postJSON(ctx, "/api/clap/search", map[string]interface{}{
		"query": strings.TrimSpace(query),
		"limit": limit,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *audioMuseListenClient) similarTracksByID(ctx context.Context, itemID string, limit int) ([]audioMuseSimilarTrack, error) {
	if c == nil {
		return nil, fmt.Errorf("sonic analysis is disabled")
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 12 {
		limit = 12
	}
	params := url.Values{}
	params.Set("item_id", strings.TrimSpace(itemID))
	params.Set("n", fmt.Sprintf("%d", limit))
	var result []audioMuseSimilarTrack
	if err := c.getJSON(ctx, "/api/similar_tracks?"+params.Encode(), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func handleTextToTrackTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "queryText", "limit"); err != nil {
		return toolResult{}, err
	}
	queryText := strings.TrimSpace(toolStringArg(args, "queryText"))
	if queryText == "" {
		return toolResult{}, fmt.Errorf("queryText is required")
	}
	client := newAudioMuseListenClient()
	if client == nil {
		return toolResult{payload: map[string]interface{}{
			"textToTrack": map[string]interface{}{
				"queryText": queryText,
				"matches":   []map[string]interface{}{},
				"warning":   "Sonic analysis is disabled.",
			},
		}}, nil
	}
	result, err := client.clapSearchTracks(ctx, queryText, clampInt(toolIntArg(args, "limit", 10), 25))
	if err != nil {
		return toolResult{payload: map[string]interface{}{
			"textToTrack": map[string]interface{}{
				"queryText": queryText,
				"matches":   []map[string]interface{}{},
				"warning":   err.Error(),
			},
		}}, nil
	}
	items := make([]map[string]interface{}, 0, len(result.Results))
	for _, match := range result.Results {
		item := map[string]interface{}{
			"id":         strings.TrimSpace(match.ItemID),
			"title":      strings.TrimSpace(match.Title),
			"artistName": strings.TrimSpace(match.Author),
		}
		if album := strings.TrimSpace(match.Album); album != "" {
			item["albumName"] = album
		}
		if match.Similarity != nil {
			item["similarity"] = *match.Similarity
		}
		items = append(items, item)
	}
	return toolResult{payload: map[string]interface{}{
		"textToTrack": map[string]interface{}{
			"queryText": queryText,
			"count":     len(items),
			"matches":   items,
		},
	}}, nil
}

func handleClusterScenesTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "queryText", "limit", "sampleTracks"); err != nil {
		return toolResult{}, err
	}
	queryText := strings.TrimSpace(toolStringArg(args, "queryText"))
	limit := clampInt(toolIntArg(args, "limit", 8), 20)
	sampleTracks := clampInt(toolIntArg(args, "sampleTracks", 3), 6)
	client := newAudioMuseListenClient()
	if client == nil {
		return toolResult{payload: map[string]interface{}{
			"clusterScenes": map[string]interface{}{
				"configured": false,
				"ready":      false,
				"queryText":  queryText,
				"scenes":     []map[string]interface{}{},
				"message":    "Sonic analysis is disabled.",
			},
		}}, nil
	}
	task, taskErr := client.currentTask(ctx)
	playlists, playlistsErr := client.playlists(ctx)
	if taskErr != nil && playlistsErr != nil {
		return toolResult{}, taskErr
	}
	sort.Slice(playlists, func(i, j int) bool {
		if playlists[i].SongCount == playlists[j].SongCount {
			return strings.ToLower(playlists[i].Name) < strings.ToLower(playlists[j].Name)
		}
		return playlists[i].SongCount > playlists[j].SongCount
	})
	filtered := make([]audioMuseClusterPlaylist, 0, len(playlists))
	queryKey := normalizeReferenceText(queryText)
	for _, playlist := range playlists {
		matchesQuery := queryKey == "" ||
			strings.Contains(normalizeReferenceText(playlist.Name), queryKey) ||
			strings.Contains(normalizeReferenceText(playlist.Key), queryKey) ||
			strings.Contains(normalizeReferenceText(playlist.Subtitle), queryKey)
		if !matchesQuery {
			continue
		}
		filtered = append(filtered, playlist)
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	scenes := make([]map[string]interface{}, 0, len(filtered))
	for _, playlist := range filtered {
		sample := make([]map[string]interface{}, 0, minInt(len(playlist.Songs), sampleTracks))
		for _, song := range playlist.Songs {
			if len(sample) >= sampleTracks {
				break
			}
			sample = append(sample, map[string]interface{}{
				"title":      strings.TrimSpace(song.Title),
				"artistName": strings.TrimSpace(song.Author),
			})
		}
		scenes = append(scenes, map[string]interface{}{
			"key":          strings.TrimSpace(playlist.Key),
			"name":         strings.TrimSpace(playlist.Name),
			"subtitle":     strings.TrimSpace(playlist.Subtitle),
			"songCount":    playlist.SongCount,
			"sampleTracks": sample,
		})
	}
	if len(filtered) == 1 {
		item := sceneSessionItemFromPlaylist(filtered[0])
		setLastSceneSelection(chatSessionIDFromContext(ctx), &item, nil)
	} else if len(filtered) > 1 {
		candidates := make([]sceneSessionItem, 0, len(filtered))
		for _, playlist := range filtered {
			candidates = append(candidates, sceneSessionItemFromPlaylist(playlist))
		}
		setLastSceneSelection(chatSessionIDFromContext(ctx), nil, candidates)
	}
	ready := len(playlists) > 0
	message := ""
	switch {
	case len(scenes) > 0 && queryKey != "":
		message = fmt.Sprintf("Loaded %d sonic scene(s) matching %q.", len(scenes), queryText)
	case len(scenes) > 0:
		message = fmt.Sprintf("Loaded %d sonic scene(s).", len(scenes))
	case task != nil && task.Status == "SUCCESS" && task.Details != nil:
		if created, ok := task.Details["num_playlists_created"].(float64); ok && int(created) == 0 {
			message = "Scene clustering completed, but the last run did not produce any saved scenes."
		}
	case ready && queryKey != "":
		message = fmt.Sprintf("No sonic scenes matched %q.", queryText)
	case task != nil && strings.Contains(strings.ToLower(task.TaskType), "analysis") && task.Status == "PROGRESS":
		message = "Sonic analysis is still running, so scenes are not ready yet."
	case task != nil && strings.Contains(strings.ToLower(task.TaskType), "cluster") && task.Status == "PROGRESS":
		message = "Scene clustering is running now."
	default:
		message = "No sonic scenes are available yet."
	}
	return toolResult{payload: map[string]interface{}{
		"clusterScenes": map[string]interface{}{
			"configured":   true,
			"ready":        ready,
			"queryText":    queryText,
			"sceneCount":   len(scenes),
			"totalScenes":  len(playlists),
			"sampleTracks": sampleTracks,
			"task":         task,
			"scenes":       scenes,
			"message":      message,
		},
	}}, nil
}

func handleSceneTracksTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "sceneKey", "sceneName", "limit"); err != nil {
		return toolResult{}, err
	}
	client := newAudioMuseListenClient()
	sceneKey := strings.TrimSpace(toolStringArg(args, "sceneKey"))
	sceneName := strings.TrimSpace(toolStringArg(args, "sceneName"))
	limit := clampInt(toolIntArg(args, "limit", 10), 50)
	if client == nil {
		return toolResult{payload: map[string]interface{}{
			"sceneTracks": map[string]interface{}{
				"configured": false,
				"ready":      false,
				"sceneKey":   sceneKey,
				"sceneName":  sceneName,
				"tracks":     []map[string]interface{}{},
				"message":    "Sonic analysis is disabled.",
			},
		}}, nil
	}
	if sceneKey == "" && sceneName == "" {
		return toolResult{}, fmt.Errorf("sceneKey or sceneName is required")
	}
	playlists, err := client.playlists(ctx)
	if err != nil {
		return toolResult{}, err
	}
	if len(playlists) == 0 {
		return toolResult{payload: map[string]interface{}{
			"sceneTracks": map[string]interface{}{
				"configured": true,
				"ready":      false,
				"sceneKey":   sceneKey,
				"sceneName":  sceneName,
				"tracks":     []map[string]interface{}{},
				"message":    "No sonic scenes are available yet.",
			},
		}}, nil
	}

	matches := selectScenePlaylists(playlists, sceneKey, sceneName)
	payload := map[string]interface{}{
		"configured": true,
		"ready":      true,
		"sceneKey":   sceneKey,
		"sceneName":  sceneName,
	}

	switch len(matches) {
	case 0:
		payload["resolved"] = false
		payload["tracks"] = []map[string]interface{}{}
		payload["message"] = fmt.Sprintf("No sonic scene matched %q.", firstNonEmpty(sceneKey, sceneName))
	case 1:
		scene := matches[0]
		item := sceneSessionItemFromPlaylist(scene)
		setLastSceneSelection(chatSessionIDFromContext(ctx), &item, nil)
		payload["resolved"] = true
		payload["scene"] = map[string]interface{}{
			"key":       strings.TrimSpace(scene.Key),
			"name":      strings.TrimSpace(scene.Name),
			"subtitle":  strings.TrimSpace(scene.Subtitle),
			"songCount": scene.SongCount,
		}
		payload["trackCount"] = minInt(scene.SongCount, limit)
		payload["totalTracks"] = scene.SongCount
		payload["tracks"] = buildSceneTrackItems(scene, limit)
		payload["message"] = fmt.Sprintf("Loaded %d track(s) from %s.", minInt(scene.SongCount, limit), strings.TrimSpace(scene.Name))
	default:
		candidates := make([]map[string]interface{}, 0, len(matches))
		sessionCandidates := make([]sceneSessionItem, 0, len(matches))
		for _, scene := range matches {
			candidates = append(candidates, map[string]interface{}{
				"key":       strings.TrimSpace(scene.Key),
				"name":      strings.TrimSpace(scene.Name),
				"subtitle":  strings.TrimSpace(scene.Subtitle),
				"songCount": scene.SongCount,
			})
			sessionCandidates = append(sessionCandidates, sceneSessionItemFromPlaylist(scene))
		}
		setLastSceneSelection(chatSessionIDFromContext(ctx), nil, sessionCandidates)
		payload["resolved"] = false
		payload["ambiguous"] = true
		payload["tracks"] = []map[string]interface{}{}
		payload["candidates"] = candidates
		payload["message"] = fmt.Sprintf("Multiple sonic scenes matched %q. Use the sceneKey to pick one.", firstNonEmpty(sceneKey, sceneName))
	}

	return toolResult{payload: map[string]interface{}{"sceneTracks": payload}}, nil
}

func handleSceneExpandTool(ctx context.Context, runtime toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "sceneKey", "sceneName", "queryText", "limit", "seedCount", "provider", "excludeRecentDays"); err != nil {
		return toolResult{}, err
	}
	client := newAudioMuseListenClient()
	sceneKey := strings.TrimSpace(toolStringArg(args, "sceneKey"))
	sceneName := strings.TrimSpace(toolStringArg(args, "sceneName"))
	queryText := strings.TrimSpace(toolStringArg(args, "queryText"))
	limit := clampInt(toolIntArg(args, "limit", 10), 30)
	seedCount := clampInt(toolIntArg(args, "seedCount", 3), 6)
	perSeedLimit := maxInt(limit*2, 12)
	provider := strings.TrimSpace(toolStringArg(args, "provider"))
	if provider == "" {
		provider = similarity.ProviderHybrid
	}
	excludeRecentDays := clampInt(toolIntArg(args, "excludeRecentDays", 0), 3650)
	if client == nil {
		return toolResult{payload: map[string]interface{}{
			"sceneExpand": map[string]interface{}{
				"configured": false,
				"ready":      false,
				"sceneKey":   sceneKey,
				"sceneName":  sceneName,
				"queryText":  queryText,
				"tracks":     []map[string]interface{}{},
				"message":    "Sonic analysis is disabled.",
			},
		}}, nil
	}
	if runtime.similarity == nil {
		return toolResult{}, fmt.Errorf("similarity service is unavailable")
	}
	if sceneKey == "" && sceneName == "" {
		return toolResult{}, fmt.Errorf("sceneKey or sceneName is required")
	}
	playlists, err := client.playlists(ctx)
	if err != nil {
		return toolResult{}, err
	}
	if len(playlists) == 0 {
		return toolResult{payload: map[string]interface{}{
			"sceneExpand": map[string]interface{}{
				"configured": true,
				"ready":      false,
				"sceneKey":   sceneKey,
				"sceneName":  sceneName,
				"queryText":  queryText,
				"tracks":     []map[string]interface{}{},
				"message":    "No sonic scenes are available yet.",
			},
		}}, nil
	}

	matches := selectScenePlaylists(playlists, sceneKey, sceneName)
	payload := map[string]interface{}{
		"configured": true,
		"ready":      true,
		"sceneKey":   sceneKey,
		"sceneName":  sceneName,
		"queryText":  queryText,
	}
	switch len(matches) {
	case 0:
		payload["resolved"] = false
		payload["tracks"] = []map[string]interface{}{}
		payload["message"] = fmt.Sprintf("No sonic scene matched %q.", firstNonEmpty(sceneKey, sceneName))
		return toolResult{payload: map[string]interface{}{"sceneExpand": payload}}, nil
	case 1:
	default:
		candidates := make([]map[string]interface{}, 0, len(matches))
		for _, scene := range matches {
			candidates = append(candidates, map[string]interface{}{
				"key":       strings.TrimSpace(scene.Key),
				"name":      strings.TrimSpace(scene.Name),
				"subtitle":  strings.TrimSpace(scene.Subtitle),
				"songCount": scene.SongCount,
			})
		}
		payload["resolved"] = false
		payload["ambiguous"] = true
		payload["tracks"] = []map[string]interface{}{}
		payload["candidates"] = candidates
		payload["message"] = fmt.Sprintf("Multiple sonic scenes matched %q. Use the sceneKey to pick one.", firstNonEmpty(sceneKey, sceneName))
		return toolResult{payload: map[string]interface{}{"sceneExpand": payload}}, nil
	}

	scene := matches[0]
	sceneTrackKeys := make(map[string]struct{}, len(scene.Songs))
	for _, song := range scene.Songs {
		sceneTrackKeys[normalizeSceneTrackKey(song.Title, song.Author)] = struct{}{}
	}
	seeds := scene.Songs
	if len(seeds) > seedCount {
		seeds = seeds[:seedCount]
	}
	type sceneExpandCandidate struct {
		id           string
		albumID      string
		title        string
		artistName   string
		rating       int
		playCount    int
		lastPlayed   string
		baseScore    float64
		finalScore   float64
		seedHits     int
		matchedSeeds []string
		energyLabel  string
		topMoods     []string
		topFeatures  []string
	}
	aggregate := map[string]*sceneExpandCandidate{}
	warnings := make([]string, 0)
	for _, seed := range seeds {
		results, _, fetchErr := sceneExpandSimilarTracksFetcher(ctx, runtime, similarity.TrackRequest{
			SeedTrackTitle:    strings.TrimSpace(seed.Title),
			SeedArtistName:    strings.TrimSpace(seed.Author),
			Provider:          provider,
			Limit:             perSeedLimit,
			ExcludeRecentDays: excludeRecentDays,
		})
		if fetchErr != nil {
			warnings = append(warnings, fmt.Sprintf("seed %q by %q: %v", strings.TrimSpace(seed.Title), strings.TrimSpace(seed.Author), fetchErr))
			continue
		}
		seedLabel := strings.TrimSpace(seed.Title)
		if artist := strings.TrimSpace(seed.Author); artist != "" {
			seedLabel += " by " + artist
		}
		for _, result := range results {
			sceneTrackKey := normalizeSceneTrackKey(result.Title, result.ArtistName)
			if _, exists := sceneTrackKeys[sceneTrackKey]; exists {
				continue
			}
			candidateKey := strings.TrimSpace(result.ID)
			if candidateKey == "" {
				candidateKey = sceneTrackKey
			}
			candidate, exists := aggregate[candidateKey]
			if !exists {
				var lastPlayed interface{}
				if result.LastPlayed != nil {
					lastPlayed = result.LastPlayed.Format(time.RFC3339)
				}
				candidate = &sceneExpandCandidate{
					id:         strings.TrimSpace(result.ID),
					albumID:    strings.TrimSpace(result.AlbumID),
					title:      strings.TrimSpace(result.Title),
					artistName: strings.TrimSpace(result.ArtistName),
					rating:     result.Rating,
					playCount:  result.PlayCount,
				}
				if lastPlayed != nil {
					candidate.lastPlayed = lastPlayed.(string)
				}
				aggregate[candidateKey] = candidate
			}
			candidate.baseScore += result.Score
			candidate.seedHits++
			if !containsString(candidate.matchedSeeds, seedLabel) && len(candidate.matchedSeeds) < 4 {
				candidate.matchedSeeds = append(candidate.matchedSeeds, seedLabel)
			}
		}
	}

	candidates := make([]*sceneExpandCandidate, 0, len(aggregate))
	for _, candidate := range aggregate {
		candidate.finalScore = candidate.baseScore + float64(candidate.seedHits-1)*0.08
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].finalScore == candidates[j].finalScore {
			if candidates[i].seedHits == candidates[j].seedHits {
				return normalizeReferenceText(candidates[i].title) < normalizeReferenceText(candidates[j].title)
			}
			return candidates[i].seedHits > candidates[j].seedHits
		}
		return candidates[i].finalScore > candidates[j].finalScore
	})

	preferences := parseSceneExpandPreferences(queryText)
	if preferences.active() && len(candidates) > 0 {
		shortlist := candidates
		if len(shortlist) > maxInt(limit*3, 12) {
			shortlist = shortlist[:maxInt(limit*3, 12)]
		}
		for _, candidate := range shortlist {
			if strings.TrimSpace(candidate.id) == "" {
				continue
			}
			profile, profileErr := audioMuseScoreByIDFetcher(ctx, candidate.id)
			if profileErr != nil {
				continue
			}
			candidate.energyLabel = describeEnergy(profile.Energy)
			candidate.topMoods = extractLabels(parseRankedAudioMuseScores(profile.MoodVector, 3))
			candidate.topFeatures = extractLabels(parseRankedAudioMuseScores(profile.OtherFeatures, 3))
			candidate.finalScore += sceneExpandPreferenceBonus(preferences, profile, candidate.playCount)
		}
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].finalScore == candidates[j].finalScore {
				if candidates[i].seedHits == candidates[j].seedHits {
					return normalizeReferenceText(candidates[i].title) < normalizeReferenceText(candidates[j].title)
				}
				return candidates[i].seedHits > candidates[j].seedHits
			}
			return candidates[i].finalScore > candidates[j].finalScore
		})
	}

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	items := make([]map[string]interface{}, 0, len(candidates))
	for _, candidate := range candidates {
		item := map[string]interface{}{
			"id":           candidate.id,
			"albumId":      candidate.albumID,
			"title":        candidate.title,
			"artistName":   candidate.artistName,
			"rating":       candidate.rating,
			"playCount":    candidate.playCount,
			"score":        candidate.finalScore,
			"seedHits":     candidate.seedHits,
			"matchedSeeds": candidate.matchedSeeds,
		}
		if candidate.lastPlayed != "" {
			item["lastPlayed"] = candidate.lastPlayed
		}
		if candidate.energyLabel != "" {
			item["energyLabel"] = candidate.energyLabel
		}
		if len(candidate.topMoods) > 0 {
			item["topMoods"] = candidate.topMoods
		}
		if len(candidate.topFeatures) > 0 {
			item["topFeatures"] = candidate.topFeatures
		}
		items = append(items, item)
	}
	payload["resolved"] = true
	payload["scene"] = map[string]interface{}{
		"key":       strings.TrimSpace(scene.Key),
		"name":      strings.TrimSpace(scene.Name),
		"subtitle":  strings.TrimSpace(scene.Subtitle),
		"songCount": scene.SongCount,
	}
	payload["provider"] = provider
	payload["seedCount"] = len(seeds)
	payload["trackCount"] = len(items)
	payload["tracks"] = items
	payload["message"] = fmt.Sprintf("Expanded %s with %d adjacent track(s).", strings.TrimSpace(scene.Name), len(items))
	if len(warnings) > 0 {
		payload["warnings"] = warnings
	}
	return toolResult{payload: map[string]interface{}{"sceneExpand": payload}}, nil
}

func handleDiscoverAlbumsFromSceneTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "sceneKey", "sceneName", "queryText", "limit"); err != nil {
		return toolResult{}, err
	}
	client := newAudioMuseListenClient()
	sceneKey := strings.TrimSpace(toolStringArg(args, "sceneKey"))
	sceneName := strings.TrimSpace(toolStringArg(args, "sceneName"))
	queryText := strings.TrimSpace(toolStringArg(args, "queryText"))
	limit := clampInt(toolIntArg(args, "limit", 5), 8)
	if client == nil {
		return toolResult{payload: map[string]interface{}{
			"discoverAlbumsFromScene": map[string]interface{}{
				"configured": false,
				"ready":      false,
				"sceneKey":   sceneKey,
				"sceneName":  sceneName,
				"queryText":  queryText,
				"candidates": []discoveredAlbumCandidate{},
				"message":    "Sonic analysis is disabled.",
			},
		}}, nil
	}
	if sceneKey == "" && sceneName == "" {
		return toolResult{}, fmt.Errorf("sceneKey or sceneName is required")
	}
	playlists, err := client.playlists(ctx)
	if err != nil {
		return toolResult{}, err
	}
	if len(playlists) == 0 {
		return toolResult{payload: map[string]interface{}{
			"discoverAlbumsFromScene": map[string]interface{}{
				"configured": false,
				"ready":      false,
				"sceneKey":   sceneKey,
				"sceneName":  sceneName,
				"queryText":  queryText,
				"candidates": []discoveredAlbumCandidate{},
				"message":    "No sonic scenes are available yet.",
			},
		}}, nil
	}
	matches := selectScenePlaylists(playlists, sceneKey, sceneName)
	payload := map[string]interface{}{
		"configured": true,
		"ready":      true,
		"sceneKey":   sceneKey,
		"sceneName":  sceneName,
		"queryText":  queryText,
	}
	switch len(matches) {
	case 0:
		payload["resolved"] = false
		payload["candidates"] = []discoveredAlbumCandidate{}
		payload["message"] = fmt.Sprintf("No sonic scene matched %q.", firstNonEmpty(sceneKey, sceneName))
		return toolResult{payload: map[string]interface{}{"discoverAlbumsFromScene": payload}}, nil
	case 1:
	default:
		candidates := make([]map[string]interface{}, 0, len(matches))
		for _, scene := range matches {
			candidates = append(candidates, map[string]interface{}{
				"key":       strings.TrimSpace(scene.Key),
				"name":      strings.TrimSpace(scene.Name),
				"subtitle":  strings.TrimSpace(scene.Subtitle),
				"songCount": scene.SongCount,
			})
		}
		payload["resolved"] = false
		payload["ambiguous"] = true
		payload["candidates"] = []discoveredAlbumCandidate{}
		payload["sceneCandidates"] = candidates
		payload["message"] = fmt.Sprintf("Multiple sonic scenes matched %q. Use the sceneKey to pick one.", firstNonEmpty(sceneKey, sceneName))
		return toolResult{payload: map[string]interface{}{"discoverAlbumsFromScene": payload}}, nil
	}
	scene := matches[0]
	item := sceneSessionItemFromPlaylist(scene)
	setLastSceneSelection(chatSessionIDFromContext(ctx), &item, nil)
	request, err := discovery.BuildSceneSeededRequest(
		scene.Name,
		scene.Subtitle,
		sceneRepresentativeArtists(scene, 4),
		sceneRepresentativeTracks(scene, 3),
		queryText,
		limit,
	)
	if err != nil {
		return toolResult{}, err
	}
	candidates, meta, err := discoverAlbumsRequestRunner(ctx, request)
	if err != nil {
		return toolResult{}, err
	}
	payload["resolved"] = true
	payload["scene"] = map[string]interface{}{
		"key":       strings.TrimSpace(scene.Key),
		"name":      strings.TrimSpace(scene.Name),
		"subtitle":  strings.TrimSpace(scene.Subtitle),
		"songCount": scene.SongCount,
	}
	payload["discoveryQuery"] = request.Query
	payload["discoverySeed"] = request.Seed
	payload["query"] = meta["query"]
	payload["count"] = len(candidates)
	payload["candidates"] = candidates
	payload["message"] = fmt.Sprintf("Discovered %d album candidate(s) from %s.", len(candidates), strings.TrimSpace(scene.Name))
	return toolResult{payload: map[string]interface{}{"discoverAlbumsFromScene": payload}}, nil
}

func handleDescribeTrackSoundTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "trackId", "trackTitle", "artistName", "neighborLimit"); err != nil {
		return toolResult{}, err
	}
	client := newAudioMuseListenClient()
	if client == nil {
		return toolResult{}, fmt.Errorf("sonic analysis is disabled")
	}
	track, err := resolveAudioMusePathTrack(
		ctx,
		client,
		toolStringArg(args, "trackId"),
		toolStringArg(args, "trackTitle"),
		toolStringArg(args, "artistName"),
	)
	if err != nil {
		return toolResult{}, fmt.Errorf("resolve track: %w", err)
	}
	profile, err := audioMuseScoreByIDFetcher(ctx, track.ItemID)
	if err != nil {
		return toolResult{}, fmt.Errorf("load sonic analysis score: %w", err)
	}
	neighbors, neighborErr := client.similarTracksByID(ctx, track.ItemID, clampInt(toolIntArg(args, "neighborLimit", 5), 8))
	moods := parseRankedAudioMuseScores(profile.MoodVector, 4)
	features := parseRankedAudioMuseScores(profile.OtherFeatures, 4)
	neighborItems := make([]map[string]interface{}, 0, len(neighbors))
	for _, neighbor := range neighbors {
		neighborItems = append(neighborItems, map[string]interface{}{
			"id":         strings.TrimSpace(neighbor.ItemID),
			"title":      strings.TrimSpace(neighbor.Title),
			"artistName": strings.TrimSpace(neighbor.Author),
			"albumName":  strings.TrimSpace(neighbor.Album),
			"distance":   neighbor.Distance,
		})
	}

	trackPayload := map[string]interface{}{
		"id":         strings.TrimSpace(profile.ItemID),
		"title":      firstNonEmpty(profile.Title, track.Title),
		"artistName": firstNonEmpty(profile.Author, track.Author),
		"albumName":  firstNonEmpty(profile.Album, track.Album),
	}
	summary := map[string]interface{}{
		"tempoBPM":    maybeFloat(profile.Tempo),
		"tempoLabel":  describeTempo(profile.Tempo),
		"energy":      maybeFloat(profile.Energy),
		"energyLabel": describeEnergy(profile.Energy),
		"key":         strings.TrimSpace(profile.Key),
		"scale":       strings.TrimSpace(profile.Scale),
		"topMoods":    moods,
		"topFeatures": features,
		"profileText": buildTrackSoundProfileText(profile, moods, features),
	}
	payload := map[string]interface{}{
		"describeTrackSound": map[string]interface{}{
			"track":     trackPayload,
			"summary":   summary,
			"neighbors": neighborItems,
			"message":   "Track sound profile loaded.",
		},
	}
	if neighborErr != nil {
		payload["describeTrackSound"].(map[string]interface{})["warning"] = neighborErr.Error()
	}
	return toolResult{payload: payload}, nil
}

func selectScenePlaylists(playlists []audioMuseClusterPlaylist, sceneKey, sceneName string) []audioMuseClusterPlaylist {
	sceneKey = strings.TrimSpace(sceneKey)
	sceneName = strings.TrimSpace(sceneName)
	if sceneKey != "" {
		keyNorm := normalizeReferenceText(sceneKey)
		exact := make([]audioMuseClusterPlaylist, 0, 1)
		partial := make([]audioMuseClusterPlaylist, 0, len(playlists))
		for _, playlist := range playlists {
			playlistKey := normalizeReferenceText(playlist.Key)
			if playlistKey == keyNorm {
				exact = append(exact, playlist)
				continue
			}
			if strings.Contains(playlistKey, keyNorm) {
				partial = append(partial, playlist)
			}
		}
		if len(exact) > 0 {
			return exact
		}
		if len(partial) > 0 {
			return partial
		}
	}
	if sceneName == "" {
		return nil
	}
	nameNorm := normalizeReferenceText(sceneName)
	exact := make([]audioMuseClusterPlaylist, 0, 1)
	partial := make([]audioMuseClusterPlaylist, 0, len(playlists))
	for _, playlist := range playlists {
		playlistName := normalizeReferenceText(playlist.Name)
		playlistSubtitle := normalizeReferenceText(playlist.Subtitle)
		if playlistName == nameNorm || strings.TrimSpace(playlistSubtitle) == nameNorm {
			exact = append(exact, playlist)
			continue
		}
		if strings.Contains(playlistName, nameNorm) || (playlistSubtitle != "" && strings.Contains(playlistSubtitle, nameNorm)) {
			partial = append(partial, playlist)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return partial
}

func buildSceneTrackItems(scene audioMuseClusterPlaylist, limit int) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, minInt(len(scene.Songs), limit))
	for index, song := range scene.Songs {
		if len(items) >= limit {
			break
		}
		items = append(items, map[string]interface{}{
			"position":   index + 1,
			"title":      strings.TrimSpace(song.Title),
			"artistName": strings.TrimSpace(song.Author),
		})
	}
	return items
}

func normalizeSceneTrackKey(title, artist string) string {
	return normalizeReferenceText(strings.TrimSpace(title) + " " + strings.TrimSpace(artist))
}

func sceneRepresentativeArtists(scene audioMuseClusterPlaylist, limit int) []string {
	seen := map[string]struct{}{}
	artists := make([]string, 0, minInt(len(scene.Songs), limit))
	for _, song := range scene.Songs {
		artist := strings.TrimSpace(song.Author)
		if artist == "" {
			continue
		}
		key := normalizeReferenceText(artist)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		artists = append(artists, artist)
		if len(artists) >= limit {
			break
		}
	}
	return artists
}

func sceneRepresentativeTracks(scene audioMuseClusterPlaylist, limit int) []string {
	tracks := make([]string, 0, minInt(len(scene.Songs), limit))
	for _, song := range scene.Songs {
		if len(tracks) >= limit {
			break
		}
		title := strings.TrimSpace(song.Title)
		if title == "" {
			continue
		}
		if artist := strings.TrimSpace(song.Author); artist != "" {
			tracks = append(tracks, title+" by "+artist)
			continue
		}
		tracks = append(tracks, title)
	}
	return tracks
}

func containsString(values []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	for _, value := range values {
		if strings.TrimSpace(value) == needle {
			return true
		}
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type sceneExpandPreferences struct {
	lowEnergy  bool
	highEnergy bool
	sad        bool
	relaxed    bool
	danceable  bool
	novel      bool
	familiar   bool
}

func parseSceneExpandPreferences(queryText string) sceneExpandPreferences {
	lower := normalizeReferenceText(queryText)
	return sceneExpandPreferences{
		lowEnergy:  strings.Contains(lower, "calmer") || strings.Contains(lower, "calm") || strings.Contains(lower, "quieter") || strings.Contains(lower, "softer") || strings.Contains(lower, "gentler"),
		highEnergy: strings.Contains(lower, "louder") || strings.Contains(lower, "harder") || strings.Contains(lower, "more energy") || strings.Contains(lower, "more energetic"),
		sad:        strings.Contains(lower, "sad") || strings.Contains(lower, "sadder") || strings.Contains(lower, "melanch") || strings.Contains(lower, "darker") || strings.Contains(lower, "bleak"),
		relaxed:    strings.Contains(lower, "relaxed") || strings.Contains(lower, "dreamier") || strings.Contains(lower, "dreamy") || strings.Contains(lower, "floatier"),
		danceable:  strings.Contains(lower, "dance") || strings.Contains(lower, "groove") || strings.Contains(lower, "club"),
		novel:      strings.Contains(lower, "less familiar") || strings.Contains(lower, "less known") || strings.Contains(lower, "unplayed") || strings.Contains(lower, "novel") || strings.Contains(lower, "newer to me"),
		familiar:   strings.Contains(lower, "more familiar") || strings.Contains(lower, "safer") || strings.Contains(lower, "known") || strings.Contains(lower, "popular"),
	}
}

func (p sceneExpandPreferences) active() bool {
	return p.lowEnergy || p.highEnergy || p.sad || p.relaxed || p.danceable || p.novel || p.familiar
}

func sceneExpandPreferenceBonus(preferences sceneExpandPreferences, profile audioMuseTrackProfile, playCount int) float64 {
	bonus := 0.0
	if preferences.lowEnergy && profile.Energy != nil {
		switch {
		case *profile.Energy < 0.08:
			bonus += 0.12
		case *profile.Energy < 0.14:
			bonus += 0.05
		default:
			bonus -= 0.04
		}
	}
	if preferences.highEnergy && profile.Energy != nil {
		switch {
		case *profile.Energy > 0.18:
			bonus += 0.12
		case *profile.Energy > 0.11:
			bonus += 0.05
		default:
			bonus -= 0.04
		}
	}
	if preferences.sad && audioMuseProfileMentions(profile, "sad") {
		bonus += 0.08
	}
	if preferences.relaxed && audioMuseProfileMentions(profile, "relaxed") {
		bonus += 0.08
	}
	if preferences.danceable && audioMuseProfileMentions(profile, "danceable") {
		bonus += 0.08
	}
	if preferences.novel {
		switch {
		case playCount == 0:
			bonus += 0.10
		case playCount <= 2:
			bonus += 0.05
		case playCount >= 8:
			bonus -= 0.04
		}
	}
	if preferences.familiar {
		switch {
		case playCount >= 8:
			bonus += 0.10
		case playCount >= 3:
			bonus += 0.05
		case playCount == 0:
			bonus -= 0.04
		}
	}
	return bonus
}

func audioMuseProfileMentions(profile audioMuseTrackProfile, term string) bool {
	needle := normalizeReferenceText(term)
	return strings.Contains(normalizeReferenceText(profile.MoodVector), needle) || strings.Contains(normalizeReferenceText(profile.OtherFeatures), needle)
}

func defaultSceneExpandSimilarTracksFetcher(ctx context.Context, runtime toolRuntime, req similarity.TrackRequest) ([]similarity.TrackResult, string, error) {
	if runtime.similarity == nil {
		return nil, "", fmt.Errorf("similarity service is unavailable")
	}
	response, err := runtime.similarity.SimilarTracks(ctx, req)
	if err != nil {
		return nil, "", err
	}
	return response.Results, response.Provider, nil
}

func handleSongPathTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "startTrackId", "startTrackTitle", "startArtistName", "endTrackId", "endTrackTitle", "endArtistName", "maxSteps", "keepExactSize"); err != nil {
		return toolResult{}, err
	}
	client := newAudioMuseListenClient()
	if client == nil {
		return toolResult{}, fmt.Errorf("sonic analysis is disabled")
	}
	startTrack, err := resolveAudioMusePathTrack(
		ctx,
		client,
		toolStringArg(args, "startTrackId"),
		toolStringArg(args, "startTrackTitle"),
		toolStringArg(args, "startArtistName"),
	)
	if err != nil {
		return toolResult{}, fmt.Errorf("resolve start track: %w", err)
	}
	endTrack, err := resolveAudioMusePathTrack(
		ctx,
		client,
		toolStringArg(args, "endTrackId"),
		toolStringArg(args, "endTrackTitle"),
		toolStringArg(args, "endArtistName"),
	)
	if err != nil {
		return toolResult{}, fmt.Errorf("resolve end track: %w", err)
	}
	maxSteps := clampInt(toolIntArg(args, "maxSteps", 25), 200)
	keepExactSize := toolBoolArg(args, "keepExactSize", false)
	path, err := client.findSongPath(ctx, startTrack.ItemID, endTrack.ItemID, maxSteps, keepExactSize)
	if err != nil {
		return toolResult{}, err
	}
	items := make([]map[string]interface{}, 0, len(path))
	pathState := make([]songPathTrack, 0, len(path))
	for index, track := range path {
		item := songPathTrack{
			ID:         strings.TrimSpace(track.ItemID),
			Title:      strings.TrimSpace(track.Title),
			ArtistName: strings.TrimSpace(track.Author),
			AlbumName:  strings.TrimSpace(track.Album),
			Position:   index + 1,
		}
		items = append(items, map[string]interface{}{
			"position":   item.Position,
			"id":         item.ID,
			"title":      item.Title,
			"artistName": item.ArtistName,
			"albumName":  item.AlbumName,
		})
		pathState = append(pathState, item)
	}
	setLastSongPath(chatSessionIDFromContext(ctx),
		songPathTrack{
			ID:         strings.TrimSpace(startTrack.ItemID),
			Title:      strings.TrimSpace(startTrack.Title),
			ArtistName: strings.TrimSpace(startTrack.Author),
			AlbumName:  strings.TrimSpace(startTrack.Album),
			Position:   1,
		},
		songPathTrack{
			ID:         strings.TrimSpace(endTrack.ItemID),
			Title:      strings.TrimSpace(endTrack.Title),
			ArtistName: strings.TrimSpace(endTrack.Author),
			AlbumName:  strings.TrimSpace(endTrack.Album),
			Position:   len(pathState),
		},
		pathState,
		maxSteps,
		keepExactSize,
	)
	return toolResult{payload: map[string]interface{}{
		"songPath": map[string]interface{}{
			"start": map[string]interface{}{
				"id":         strings.TrimSpace(startTrack.ItemID),
				"title":      strings.TrimSpace(startTrack.Title),
				"artistName": strings.TrimSpace(startTrack.Author),
				"albumName":  strings.TrimSpace(startTrack.Album),
			},
			"end": map[string]interface{}{
				"id":         strings.TrimSpace(endTrack.ItemID),
				"title":      strings.TrimSpace(endTrack.Title),
				"artistName": strings.TrimSpace(endTrack.Author),
				"albumName":  strings.TrimSpace(endTrack.Album),
			},
			"path":          items,
			"pathLength":    len(items),
			"maxSteps":      maxSteps,
			"keepExactSize": keepExactSize,
		},
	}}, nil
}

func defaultAudioMuseScoreByIDFetcher(ctx context.Context, itemID string) (audioMuseTrackProfile, error) {
	databaseURL := audioMuseDatabaseURL()
	if databaseURL == "" {
		return audioMuseTrackProfile{}, fmt.Errorf("sonic analysis score database is not configured")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return audioMuseTrackProfile{}, err
	}
	defer pool.Close()

	var (
		profile       audioMuseTrackProfile
		tempo         sql.NullFloat64
		energy        sql.NullFloat64
		album         sql.NullString
		key           sql.NullString
		scale         sql.NullString
		moodVector    sql.NullString
		otherFeatures sql.NullString
	)
	err = pool.QueryRow(ctx, `
		SELECT item_id, title, author, album, tempo, key, scale, mood_vector, energy, other_features
		FROM score
		WHERE item_id = $1
	`, strings.TrimSpace(itemID)).Scan(
		&profile.ItemID,
		&profile.Title,
		&profile.Author,
		&album,
		&tempo,
		&key,
		&scale,
		&moodVector,
		&energy,
		&otherFeatures,
	)
	if err != nil {
		return audioMuseTrackProfile{}, err
	}
	profile.Album = strings.TrimSpace(album.String)
	profile.Key = strings.TrimSpace(key.String)
	profile.Scale = strings.TrimSpace(scale.String)
	profile.MoodVector = strings.TrimSpace(moodVector.String)
	profile.OtherFeatures = strings.TrimSpace(otherFeatures.String)
	if tempo.Valid {
		profile.Tempo = &tempo.Float64
	}
	if energy.Valid {
		profile.Energy = &energy.Float64
	}
	return profile, nil
}

func audioMuseDatabaseURL() string {
	if raw := strings.TrimSpace(os.Getenv("AUDIOMUSE_DATABASE_URL")); raw != "" {
		return raw
	}
	user := strings.TrimSpace(os.Getenv("AUDIOMUSE_POSTGRES_USER"))
	if user == "" {
		user = "audiomuse"
	}
	password := strings.TrimSpace(os.Getenv("AUDIOMUSE_POSTGRES_PASSWORD"))
	if password == "" {
		password = "audiomusepassword"
	}
	host := strings.TrimSpace(os.Getenv("AUDIOMUSE_POSTGRES_HOST"))
	if host == "" {
		host = "audiomuse-postgres"
	}
	port := strings.TrimSpace(os.Getenv("AUDIOMUSE_POSTGRES_PORT"))
	if port == "" {
		port = "5432"
	}
	database := strings.TrimSpace(os.Getenv("AUDIOMUSE_POSTGRES_DB"))
	if database == "" {
		database = "audiomusedb"
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable&pool_max_conns=2&pool_min_conns=0", user, password, host, port, database)
}

func parseRankedAudioMuseScores(raw string, limit int) []map[string]interface{} {
	type scoredLabel struct {
		label string
		score float64
	}
	parts := strings.Split(strings.TrimSpace(raw), ",")
	items := make([]scoredLabel, 0, len(parts))
	for _, part := range parts {
		key, value, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok {
			continue
		}
		score, err := parseToolFloat(value)
		if err != nil {
			continue
		}
		items = append(items, scoredLabel{
			label: strings.TrimSpace(key),
			score: score,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].score > items[j].score
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	result := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]interface{}{
			"label": item.label,
			"score": item.score,
		})
	}
	return result
}

func parseToolFloat(raw string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(raw), 64)
}

func buildTrackSoundProfileText(profile audioMuseTrackProfile, moods, features []map[string]interface{}) string {
	parts := make([]string, 0, 4)
	if tempo := describeTempo(profile.Tempo); tempo != "" {
		parts = append(parts, tempo+" tempo")
	}
	if energy := describeEnergy(profile.Energy); energy != "" {
		parts = append(parts, energy+" energy")
	}
	if labels := extractLabels(moods); len(labels) > 0 {
		parts = append(parts, "style tags: "+strings.Join(labels, ", "))
	}
	if labels := extractLabels(features); len(labels) > 0 {
		parts = append(parts, "feel tags: "+strings.Join(labels, ", "))
	}
	if len(parts) == 0 {
		return "No track sound profile details are available yet."
	}
	return strings.Join(parts, "; ")
}

func describeTempo(value *float64) string {
	if value == nil {
		return ""
	}
	switch {
	case *value < 85:
		return "slow"
	case *value < 125:
		return "medium"
	default:
		return "fast"
	}
}

func describeEnergy(value *float64) string {
	if value == nil {
		return ""
	}
	switch {
	case *value < 0.06:
		return "low"
	case *value < 0.12:
		return "moderate"
	default:
		return "high"
	}
}

func extractLabels(items []map[string]interface{}) []string {
	labels := make([]string, 0, len(items))
	for _, item := range items {
		label := strings.TrimSpace(fmt.Sprintf("%v", item["label"]))
		if label == "" || label == "<nil>" {
			continue
		}
		labels = append(labels, label)
	}
	return labels
}

func maybeFloat(value *float64) interface{} {
	if value == nil {
		return nil
	}
	return *value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			return text
		}
	}
	return ""
}

func resolveAudioMusePathTrack(ctx context.Context, client *audioMuseListenClient, trackID, title, artistName string) (audioMusePathTrack, error) {
	trackID = strings.TrimSpace(trackID)
	title = strings.TrimSpace(title)
	artistName = strings.TrimSpace(artistName)
	if trackID != "" {
		return audioMusePathTrack{
			ItemID: trackID,
			Title:  title,
			Author: artistName,
		}, nil
	}
	if title == "" {
		return audioMusePathTrack{}, fmt.Errorf("track title is required when track ID is not provided")
	}
	query := title
	if artistName != "" {
		query += " by " + artistName
	}
	candidates, err := client.searchPathTracks(ctx, query, 8)
	if err != nil {
		return audioMusePathTrack{}, err
	}
	if len(candidates) == 0 && artistName != "" {
		candidates, err = client.searchPathTracks(ctx, title, 8)
		if err != nil {
			return audioMusePathTrack{}, err
		}
	}
	if len(candidates) == 0 {
		return audioMusePathTrack{}, fmt.Errorf("no indexed path track matched %q", query)
	}
	best, ambiguous := selectBestAudioMusePathTrack(candidates, title, artistName)
	if ambiguous {
		return audioMusePathTrack{}, fmt.Errorf("multiple indexed path tracks matched %q: %s", query, describeAudioMusePathTracks(candidates))
	}
	return best, nil
}

func selectBestAudioMusePathTrack(candidates []audioMusePathTrack, title, artistName string) (audioMusePathTrack, bool) {
	type scoredTrack struct {
		track audioMusePathTrack
		score int
	}
	titleKey := normalizeReferenceText(title)
	artistKey := normalizeReferenceText(artistName)
	scored := make([]scoredTrack, 0, len(candidates))
	for _, candidate := range candidates {
		score := 0
		candidateTitle := normalizeReferenceText(candidate.Title)
		candidateArtist := normalizeReferenceText(candidate.Author)
		switch {
		case titleKey != "" && candidateTitle == titleKey:
			score += 6
		case titleKey != "" && (strings.Contains(candidateTitle, titleKey) || strings.Contains(titleKey, candidateTitle)):
			score += 3
		}
		switch {
		case artistKey != "" && candidateArtist == artistKey:
			score += 6
		case artistKey != "" && (strings.Contains(candidateArtist, artistKey) || strings.Contains(artistKey, candidateArtist)):
			score += 3
		}
		if titleKey != "" && artistKey != "" && candidateTitle == titleKey && candidateArtist == artistKey {
			score += 4
		}
		scored = append(scored, scoredTrack{track: candidate, score: score})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	if len(scored) == 1 {
		return scored[0].track, false
	}
	if scored[0].score == 0 {
		return scored[0].track, false
	}
	if scored[0].score == scored[1].score {
		topTitle := normalizeReferenceText(scored[0].track.Title)
		topArtist := normalizeReferenceText(scored[0].track.Author)
		if topTitle != "" && topArtist != "" {
			allEquivalent := true
			for _, candidate := range scored {
				if candidate.score != scored[0].score {
					break
				}
				if normalizeReferenceText(candidate.track.Title) != topTitle || normalizeReferenceText(candidate.track.Author) != topArtist {
					allEquivalent = false
					break
				}
			}
			if allEquivalent {
				return scored[0].track, false
			}
		}
	}
	return scored[0].track, scored[0].score == scored[1].score
}

func describeAudioMusePathTracks(candidates []audioMusePathTrack) string {
	items := make([]string, 0, minInt(len(candidates), 3))
	for _, candidate := range candidates {
		if len(items) >= 3 {
			break
		}
		label := strings.TrimSpace(candidate.Title)
		if artist := strings.TrimSpace(candidate.Author); artist != "" {
			label += " by " + artist
		}
		items = append(items, label)
	}
	return strings.Join(items, "; ")
}
