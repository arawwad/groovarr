package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"groovarr/graph"
	"groovarr/internal/db"
	"groovarr/internal/similarity"
	"groovarr/internal/toolspec"

	"github.com/pgvector/pgvector-go"
	"github.com/rs/zerolog/log"
)

var compilationYearRangePattern = regexp.MustCompile(`\b(19|20)\d{2}\s*[–-]\s*(19|20)\d{2}\b`)

type toolRuntime struct {
	resolver      *graph.Resolver
	similarity    *similarity.Service
	embeddingsURL string
}

type toolResult struct {
	payload     map[string]interface{}
	cacheKey    string
	rawResponse string
	onSuccess   func(context.Context)
}

type toolHandler func(ctx context.Context, runtime toolRuntime, args map[string]interface{}) (toolResult, error)

var toolHandlers = map[string]toolHandler{
	"addOrQueueTrackToNavidromePlaylist":       handleAddOrQueueTrackToNavidromePlaylistTool,
	"albums":                                   handleAlbumsTool,
	"albumRelationshipStats":                   handleAlbumRelationshipStatsTool,
	"albumLibraryStats":                        handleAlbumLibraryStatsTool,
	"applyDiscoveredAlbums":                    handleApplyDiscoveredAlbumsTool,
	"applyLidarrCleanup":                       handleApplyLidarrCleanupTool,
	"artistLibraryStats":                       handleArtistLibraryStatsTool,
	"artistListeningStats":                     handleArtistListeningStatsTool,
	"artists":                                  handleArtistsTool,
	"badlyRatedAlbums":                         handleBadlyRatedAlbumsTool,
	"addTrackToNavidromePlaylist":              handleAddTrackToNavidromePlaylistTool,
	"createDiscoveredPlaylist":                 handleCreateDiscoveredPlaylistTool,
	"clusterScenes":                            handleClusterScenesTool,
	"describeTrackSound":                       handleDescribeTrackSoundTool,
	"discoverAlbums":                           handleDiscoverAlbumsTool,
	"discoverAlbumsFromScene":                  handleDiscoverAlbumsFromSceneTool,
	"libraryFacetCounts":                       handleLibraryFacetCountsTool,
	"libraryStats":                             handleLibraryStatsTool,
	"lidarrCleanupCandidates":                  handleLidarrCleanupCandidatesTool,
	"matchDiscoveredAlbumsInLidarr":            handleMatchDiscoveredAlbumsInLidarrTool,
	"navidromePlaylist":                        handleNavidromePlaylistTool,
	"navidromePlaylistState":                   handleNavidromePlaylistStateTool,
	"navidromePlaylists":                       handleNavidromePlaylistsTool,
	"planDiscoverPlaylist":                     handlePlanDiscoverPlaylistTool,
	"playlistPlanDetails":                      handlePlaylistPlanDetailsTool,
	"queueMissingPlaylistTracks":               handleQueueMissingPlaylistTracksTool,
	"queueTrackForNavidromePlaylist":           handleQueueTrackForNavidromePlaylistTool,
	"recentListeningSummary":                   handleRecentListeningSummaryTool,
	"removePendingTracksFromNavidromePlaylist": handleRemovePendingTracksFromNavidromePlaylistTool,
	"removeTrackFromNavidromePlaylist":         handleRemoveTrackFromNavidromePlaylistTool,
	"removeArtistFromLibrary":                  handleRemoveArtistFromLibraryTool,
	"resolvePlaylistTracks":                    handleResolvePlaylistTracksTool,
	"sceneExpand":                              handleSceneExpandTool,
	"sceneTracks":                              handleSceneTracksTool,
	"semanticAlbumSearch":                      handleSemanticAlbumSearchTool,
	"semanticTrackSearch":                      handleSemanticTrackSearchTool,
	"similarAlbums":                            handleSimilarAlbumsTool,
	"similarArtists":                           handleSimilarArtistsTool,
	"similarTracks":                            handleSimilarTracksTool,
	"songPath":                                 handleSongPathTool,
	"textToTrack":                              handleTextToTrackTool,
	"tracks":                                   handleTracksTool,
}

func executeTool(ctx context.Context, resolver *graph.Resolver, embeddingsURL string, tool string, args map[string]interface{}) (string, error) {
	return executeToolWithSimilarity(ctx, resolver, nil, embeddingsURL, tool, args)
}

func executeToolWithSimilarity(ctx context.Context, resolver *graph.Resolver, similarityService *similarity.Service, embeddingsURL string, tool string, args map[string]interface{}) (string, error) {
	if args == nil {
		args = map[string]interface{}{}
	}
	args = repairToolArgs(tool, args)

	handler, ok := toolHandlers[tool]
	if !ok {
		return "", fmt.Errorf("unsupported tool: %s", tool)
	}

	result, err := handler(ctx, toolRuntime{resolver: resolver, similarity: similarityService, embeddingsURL: embeddingsURL}, args)
	if err != nil {
		return "", err
	}
	if result.rawResponse != "" {
		return result.rawResponse, nil
	}

	out, err := json.Marshal(map[string]interface{}{"data": result.payload})
	if err != nil {
		return "", err
	}
	if result.cacheKey != "" {
		setRecentListeningSummaryCache(result.cacheKey, string(out))
	}
	if result.onSuccess != nil {
		result.onSuccess(ctx)
	}
	return string(out), nil
}

func repairToolArgs(tool string, args map[string]interface{}) map[string]interface{} {
	if len(args) == 0 {
		return args
	}
	repaired := make(map[string]interface{}, len(args))
	for key, value := range args {
		repaired[key] = value
	}
	switch tool {
	case "albums":
		delete(repaired, "sortOrder")
		if sortBy, ok := repaired["sortBy"].(string); ok {
			switch strings.ToLower(strings.TrimSpace(sortBy)) {
			case "playcount", "play_count", "plays", "least_played", "underplayed":
				delete(repaired, "sortBy")
			}
		}
	case "semanticAlbumSearch":
		for _, key := range []string{"notPlayedSince", "unplayed", "playedSince", "playedUntil", "sortBy", "rating"} {
			delete(repaired, key)
		}
	}
	return repaired
}

func handleLibraryStatsTool(ctx context.Context, runtime toolRuntime, _ map[string]interface{}) (toolResult, error) {
	stats, err := runtime.resolver.Query().LibraryStats(ctx)
	if err != nil {
		return toolResult{}, err
	}
	return toolResult{
		payload: map[string]interface{}{
			"libraryStats": map[string]int{
				"totalAlbums":    stats.TotalAlbums,
				"totalArtists":   stats.TotalArtists,
				"unplayedAlbums": stats.UnplayedAlbums,
				"ratedAlbums":    stats.RatedAlbums,
			},
		},
	}, nil
}

func handleArtistsTool(ctx context.Context, runtime toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "limit", "minPlayCount", "artistName"); err != nil {
		return toolResult{}, err
	}

	artists, err := runtime.resolver.Query().Artists(
		ctx,
		intPtr(toolIntArg(args, "limit", 10)),
		toolOptIntArg(args, "minPlayCount"),
		toolOptStringArg(args, "artistName"),
	)
	if err != nil {
		return toolResult{}, err
	}

	items := make([]map[string]interface{}, 0, len(artists))
	for _, artist := range artists {
		items = append(items, map[string]interface{}{
			"id":        artist.ID,
			"name":      artist.Name,
			"rating":    artist.Rating,
			"playCount": artist.PlayCount,
		})
	}
	return toolResult{payload: map[string]interface{}{"artists": items}}, nil
}

func handleArtistLibraryStatsTool(ctx context.Context, runtime toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "filter", "sort", "limit"); err != nil {
		return toolResult{}, err
	}
	stats, err := fetchArtistLibraryStats(ctx, runtime, args)
	if err != nil {
		return toolResult{}, err
	}
	return toolResult{payload: map[string]interface{}{"artistLibraryStats": stats}}, nil
}

func handleArtistListeningStatsTool(ctx context.Context, runtime toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "filter", "sort", "limit"); err != nil {
		return toolResult{}, err
	}
	stats, err := fetchArtistListeningStats(ctx, runtime, args)
	if err != nil {
		return toolResult{}, err
	}
	return toolResult{payload: map[string]interface{}{"artistListeningStats": stats}}, nil
}

func handleBadlyRatedAlbumsTool(ctx context.Context, runtime toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "limit", "maxTrackDetails"); err != nil {
		return toolResult{}, err
	}
	limit := toolIntArg(args, "limit", 20)
	if limit > 100 {
		limit = 100
	}
	maxTrackDetails := toolIntArg(args, "maxTrackDetails", 3)
	if maxTrackDetails > 10 {
		maxTrackDetails = 10
	}

	albums, err := runtime.resolver.DB.GetAlbumsWithBadlyRatedTracks(ctx, limit, maxTrackDetails)
	if err != nil {
		return toolResult{}, err
	}

	items := make([]map[string]interface{}, 0, len(albums))
	candidates := make([]badlyRatedAlbumCandidate, 0, len(albums))
	for _, album := range albums {
		badTracks := make([]map[string]interface{}, 0, len(album.BadTracks))
		for _, track := range album.BadTracks {
			badTracks = append(badTracks, map[string]interface{}{
				"trackId": track.TrackID,
				"title":   track.Title,
				"rating":  track.Rating,
			})
		}
		items = append(items, map[string]interface{}{
			"albumId":       album.AlbumID,
			"albumName":     album.AlbumName,
			"artistName":    album.ArtistName,
			"badTrackCount": album.BadTrackCount,
			"badTracks":     badTracks,
		})
		candidates = append(candidates, badlyRatedAlbumCandidate{
			AlbumID:       album.AlbumID,
			AlbumName:     album.AlbumName,
			ArtistName:    album.ArtistName,
			BadTrackCount: album.BadTrackCount,
		})
	}

	return toolResult{
		payload: map[string]interface{}{"badlyRatedAlbums": items},
		onSuccess: func(ctx context.Context) {
			setLastBadlyRatedAlbums(chatSessionIDFromContext(ctx), candidates)
		},
	}, nil
}

func handleLibraryFacetCountsTool(ctx context.Context, runtime toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "field", "filter", "limit"); err != nil {
		return toolResult{}, err
	}
	counts, err := fetchLibraryFacetCounts(ctx, runtime, args)
	if err != nil {
		return toolResult{}, err
	}
	return toolResult{payload: map[string]interface{}{"libraryFacetCounts": counts}}, nil
}

func handleAlbumRelationshipStatsTool(ctx context.Context, runtime toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "filter", "sort", "limit"); err != nil {
		return toolResult{}, err
	}
	stats, err := fetchAlbumRelationshipStats(ctx, runtime, args)
	if err != nil {
		return toolResult{}, err
	}
	return toolResult{payload: map[string]interface{}{"albumRelationshipStats": stats}}, nil
}

func handleAlbumLibraryStatsTool(ctx context.Context, runtime toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "filter", "sort", "limit"); err != nil {
		return toolResult{}, err
	}
	stats, err := fetchAlbumLibraryStats(ctx, runtime, args)
	if err != nil {
		return toolResult{}, err
	}
	return toolResult{payload: map[string]interface{}{"albumLibraryStats": stats}}, nil
}

func fetchArtistLibraryStats(ctx context.Context, runtime toolRuntime, args map[string]interface{}) ([]map[string]interface{}, error) {
	filter := db.ArtistLibraryStatsFilter{}
	if rawFilter, ok := toolOptMapArg(args, "filter"); ok {
		if err := validateMapKeys(rawFilter, toolspec.ArtistLibraryStatsFilterKeys...); err != nil {
			return nil, err
		}
		inactiveSince, err := resolveInactiveSinceFilter(rawFilter)
		if err != nil {
			return nil, err
		}
		artistNames := uniqueNonEmptyStrings(append(
			mapOptStringList(rawFilter, "artistName"),
			mapOptStringList(rawFilter, "artistNames")...,
		))
		switch len(artistNames) {
		case 1:
			filter.ArtistName = &artistNames[0]
		default:
			filter.ArtistNames = artistNames
		}
		filter.Genre = mapOptString(rawFilter, "genre")
		filter.ExactAlbums = mapOptInt(rawFilter, "exactAlbums")
		filter.MinAlbums = mapOptInt(rawFilter, "minAlbums")
		filter.MaxAlbums = mapOptInt(rawFilter, "maxAlbums")
		filter.MinTotalPlays = mapOptInt(rawFilter, "minTotalPlays")
		filter.MaxTotalPlays = mapOptInt(rawFilter, "maxTotalPlays")
		filter.MaxPlaysInWindow = mapOptInt(rawFilter, "maxPlaysInWindow")
		if inactiveSince != nil {
			parsed, err := parseTimeArg(*inactiveSince)
			if err != nil {
				return nil, err
			}
			filter.InactiveSince = &parsed
		}
		if playedSince := mapOptString(rawFilter, "playedSince"); playedSince != nil {
			parsed, err := parseTimeArg(*playedSince)
			if err != nil {
				return nil, err
			}
			filter.PlayedSince = &parsed
		}
		if playedUntil := mapOptString(rawFilter, "playedUntil"); playedUntil != nil {
			parsed, err := parseTimeArg(*playedUntil)
			if err != nil {
				return nil, err
			}
			filter.PlayedUntil = &parsed
		}
	}
	sortBy := toolStringArg(args, "sort")
	limit := clampInt(toolIntArg(args, "limit", 25), 100)
	stats, err := runtime.resolver.DB.GetArtistLibraryStats(ctx, limit, filter, sortBy)
	if err != nil {
		return nil, err
	}

	items := make([]map[string]interface{}, 0, len(stats))
	for _, item := range stats {
		items = append(items, map[string]interface{}{
			"artistName":         item.ArtistName,
			"albumCount":         item.AlbumCount,
			"totalPlayCount":     item.TotalPlayCount,
			"lastPlayed":         item.LastPlayed,
			"unplayedAlbumCount": item.UnplayedAlbumCount,
			"playedInWindow":     item.PlayedInWindow,
		})
	}
	return items, nil
}

func fetchAlbumLibraryStats(ctx context.Context, runtime toolRuntime, args map[string]interface{}) ([]map[string]interface{}, error) {
	var filter *graph.AlbumLibraryStatsFilter
	if rawFilter, ok := toolOptMapArg(args, "filter"); ok {
		if err := validateMapKeys(rawFilter, toolspec.AlbumLibraryStatsFilterKeys...); err != nil {
			return nil, err
		}
		inactiveSince, err := resolveInactiveSinceFilter(rawFilter)
		if err != nil {
			return nil, err
		}
		filter = &graph.AlbumLibraryStatsFilter{
			ArtistName:       mapOptString(rawFilter, "artistName"),
			Genre:            mapOptString(rawFilter, "genre"),
			Year:             mapOptInt(rawFilter, "year"),
			MinYear:          mapOptInt(rawFilter, "minYear"),
			MaxYear:          mapOptInt(rawFilter, "maxYear"),
			MinTotalPlays:    mapOptInt(rawFilter, "minTotalPlays"),
			MaxTotalPlays:    mapOptInt(rawFilter, "maxTotalPlays"),
			MinRating:        mapOptInt(rawFilter, "minRating"),
			MaxRating:        mapOptInt(rawFilter, "maxRating"),
			InactiveSince:    inactiveSince,
			PlayedSince:      mapOptString(rawFilter, "playedSince"),
			PlayedUntil:      mapOptString(rawFilter, "playedUntil"),
			MaxPlaysInWindow: mapOptInt(rawFilter, "maxPlaysInWindow"),
			Unplayed:         mapOptBool(rawFilter, "unplayed"),
		}
	}
	sortBy := toolOptStringArg(args, "sort")
	limit := intPtr(clampInt(toolIntArg(args, "limit", 25), 100))
	stats, err := runtime.resolver.Query().AlbumLibraryStats(ctx, filter, sortBy, limit)
	if err != nil {
		return nil, err
	}

	items := make([]map[string]interface{}, 0, len(stats))
	for _, item := range stats {
		items = append(items, map[string]interface{}{
			"albumName":      item.AlbumName,
			"artistName":     item.ArtistName,
			"year":           item.Year,
			"genre":          item.Genre,
			"rating":         item.Rating,
			"totalPlayCount": item.TotalPlayCount,
			"lastPlayed":     item.LastPlayed,
			"playedInWindow": item.PlayedInWindow,
		})
	}
	return items, nil
}

func fetchArtistListeningStats(ctx context.Context, runtime toolRuntime, args map[string]interface{}) ([]map[string]interface{}, error) {
	var filter *graph.ArtistListeningStatsFilter
	if rawFilter, ok := toolOptMapArg(args, "filter"); ok {
		if err := validateMapKeys(rawFilter, toolspec.ArtistListeningStatsFilterKeys...); err != nil {
			return nil, err
		}
		filter = &graph.ArtistListeningStatsFilter{
			ArtistName:       mapOptString(rawFilter, "artistName"),
			PlayedSince:      mapOptString(rawFilter, "playedSince"),
			PlayedUntil:      mapOptString(rawFilter, "playedUntil"),
			MinPlaysInWindow: mapOptInt(rawFilter, "minPlaysInWindow"),
			MaxPlaysInWindow: mapOptInt(rawFilter, "maxPlaysInWindow"),
			MinAlbums:        mapOptInt(rawFilter, "minAlbums"),
			MaxAlbums:        mapOptInt(rawFilter, "maxAlbums"),
		}
	}
	sortBy, err := normalizeArtistListeningStatsSort(toolOptStringArg(args, "sort"))
	if err != nil {
		return nil, err
	}
	limit := intPtr(clampInt(toolIntArg(args, "limit", 25), 100))
	stats, err := runtime.resolver.Query().ArtistListeningStats(ctx, filter, sortBy, limit)
	if err != nil {
		return nil, err
	}

	items := make([]map[string]interface{}, 0, len(stats))
	for _, item := range stats {
		items = append(items, map[string]interface{}{
			"artistName":     item.ArtistName,
			"albumCount":     item.AlbumCount,
			"totalPlayCount": item.TotalPlayCount,
			"playsInWindow":  item.PlaysInWindow,
			"lastPlayed":     item.LastPlayed,
		})
	}
	return items, nil
}

func normalizeArtistListeningStatsSort(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	value := strings.ToLower(strings.TrimSpace(*raw))
	if value == "" {
		return nil, nil
	}
	switch value {
	case "plays", "plays_in_window_desc":
		normalized := "plays_in_window_desc"
		return &normalized, nil
	case "recent", "last_played_desc":
		normalized := "last_played_desc"
		return &normalized, nil
	case "name", "name_asc":
		normalized := "name_asc"
		return &normalized, nil
	case "albums", "album_count_desc":
		normalized := "album_count_desc"
		return &normalized, nil
	case "total_plays", "total_play_count_desc":
		normalized := "total_play_count_desc"
		return &normalized, nil
	default:
		return nil, fmt.Errorf("invalid sort %q (allowed: plays, recent, name, albums, total_plays)", strings.TrimSpace(*raw))
	}
}

func fetchLibraryFacetCounts(ctx context.Context, runtime toolRuntime, args map[string]interface{}) ([]map[string]interface{}, error) {
	field := strings.ToLower(strings.TrimSpace(toolStringArg(args, "field")))
	if field == "" {
		return nil, fmt.Errorf("field is required")
	}

	var filter *graph.LibraryFacetFilter
	if rawFilter, ok := toolOptMapArg(args, "filter"); ok {
		if err := validateMapKeys(rawFilter, toolspec.LibraryFacetCountsFilterKeys...); err != nil {
			return nil, err
		}
		filter = &graph.LibraryFacetFilter{
			Genre:          mapOptString(rawFilter, "genre"),
			ArtistName:     mapOptString(rawFilter, "artistName"),
			Year:           mapOptInt(rawFilter, "year"),
			MinYear:        mapOptInt(rawFilter, "minYear"),
			MaxYear:        mapOptInt(rawFilter, "maxYear"),
			Unplayed:       mapOptBool(rawFilter, "unplayed"),
			NotPlayedSince: mapOptString(rawFilter, "notPlayedSince"),
		}
	}

	limit := intPtr(clampInt(toolIntArg(args, "limit", 10), 50))
	counts, err := runtime.resolver.Query().LibraryFacetCounts(ctx, field, filter, limit)
	if err != nil {
		return nil, err
	}

	items := make([]map[string]interface{}, 0, len(counts))
	for _, item := range counts {
		items = append(items, map[string]interface{}{
			"value": item.Value,
			"count": item.Count,
		})
	}
	return items, nil
}

func fetchAlbumRelationshipStats(ctx context.Context, runtime toolRuntime, args map[string]interface{}) ([]map[string]interface{}, error) {
	var filter *graph.AlbumRelationshipStatsFilter
	if rawFilter, ok := toolOptMapArg(args, "filter"); ok {
		if err := validateMapKeys(rawFilter, toolspec.AlbumRelationshipStatsFilterKeys...); err != nil {
			return nil, err
		}
		inactiveSince, err := resolveInactiveSinceFilter(rawFilter)
		if err != nil {
			return nil, err
		}
		filter = &graph.AlbumRelationshipStatsFilter{
			ArtistExactAlbums: mapOptInt(rawFilter, "artistExactAlbums"),
			ArtistMinAlbums:   mapOptInt(rawFilter, "artistMinAlbums"),
			ArtistMaxAlbums:   mapOptInt(rawFilter, "artistMaxAlbums"),
			Genre:             mapOptString(rawFilter, "genre"),
			Unplayed:          mapOptBool(rawFilter, "unplayed"),
			NotPlayedSince:    inactiveSince,
		}
	}
	sortBy := toolOptStringArg(args, "sort")
	limit := intPtr(clampInt(toolIntArg(args, "limit", 25), 100))
	stats, err := runtime.resolver.Query().AlbumRelationshipStats(ctx, filter, sortBy, limit)
	if err != nil {
		return nil, err
	}

	items := make([]map[string]interface{}, 0, len(stats))
	for _, item := range stats {
		items = append(items, map[string]interface{}{
			"albumName":        item.AlbumName,
			"artistName":       item.ArtistName,
			"year":             item.Year,
			"artistAlbumCount": item.ArtistAlbumCount,
		})
	}
	return items, nil
}

func handleAlbumsTool(ctx context.Context, runtime toolRuntime, args map[string]interface{}) (toolResult, error) {
	limit, filters, err := buildAlbumQuery(args)
	if err != nil {
		return toolResult{}, err
	}

	albums, err := runtime.resolver.DB.GetAlbums(ctx, limit, filters)
	if err != nil {
		return toolResult{}, err
	}

	items := make([]map[string]interface{}, 0, len(albums))
	for _, album := range albums {
		items = append(items, map[string]interface{}{
			"id":         album.ID,
			"name":       album.Name,
			"artistName": album.ArtistName,
			"rating":     album.Rating,
			"playCount":  album.PlayCount,
			"lastPlayed": album.LastPlayed,
			"year":       album.Year,
			"genre":      album.Genre,
		})
	}
	return toolResult{payload: map[string]interface{}{"albums": items}}, nil
}

func handleTracksTool(ctx context.Context, runtime toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "limit", "mostPlayed", "playedSince", "playedUntil", "onlyPlayed", "window", "artistName", "queryText", "sortBy"); err != nil {
		return toolResult{}, err
	}
	limitVal := toolIntArg(args, "limit", 20)
	mostPlayed := true
	if rawMostPlayed := toolOptBoolArg(args, "mostPlayed"); rawMostPlayed != nil {
		mostPlayed = *rawMostPlayed
	}
	playedSince := toolOptStringArg(args, "playedSince")
	playedUntil := toolOptStringArg(args, "playedUntil")
	onlyPlayed := toolOptBoolArg(args, "onlyPlayed")
	filters := map[string]interface{}{}
	artistName := strings.TrimSpace(toolStringArg(args, "artistName"))
	queryText := strings.TrimSpace(toolStringArg(args, "queryText"))
	if queryText != "" {
		if titlePart, artistPart, ok := splitLookupQueryArtist(queryText); ok {
			queryText = titlePart
			if artistName == "" {
				artistName = artistPart
			}
		}
	}
	if artistName != "" {
		filters["artistName"] = artistName
	}
	if queryText != "" {
		filters["queryText"] = queryText
	}
	if sortBy := toolStringArg(args, "sortBy"); strings.TrimSpace(sortBy) != "" {
		filters["sortBy"] = strings.TrimSpace(sortBy)
	}

	if window := normalizedWindowArg(args); window != "" {
		since, until, err := resolveNamedWindow(window)
		if err != nil {
			return toolResult{}, err
		}
		sinceVal := since.Format(time.RFC3339)
		untilVal := until.Format(time.RFC3339)
		playedSince = &sinceVal
		playedUntil = &untilVal
		trueVal := true
		onlyPlayed = &trueVal
		limitVal = clampInt(limitVal, 30)
	}

	if playedSince != nil && *playedSince != "" {
		parsed, err := parseTrackTimeArg(*playedSince)
		if err != nil {
			return toolResult{}, err
		}
		filters["playedSince"] = parsed
	}
	if playedUntil != nil && *playedUntil != "" {
		parsed, err := parseTrackTimeArg(*playedUntil)
		if err != nil {
			return toolResult{}, err
		}
		filters["playedUntil"] = parsed
	}
	if onlyPlayed != nil {
		filters["onlyPlayed"] = *onlyPlayed
	}

	tracks, err := runtime.resolver.DB.GetTracks(ctx, limitVal, mostPlayed, filters)
	if err != nil {
		return toolResult{}, err
	}

	items := make([]map[string]interface{}, 0, len(tracks))
	for _, track := range tracks {
		items = append(items, map[string]interface{}{
			"id":         track.ID,
			"title":      track.Title,
			"artistName": track.ArtistName,
			"rating":     track.Rating,
			"playCount":  track.PlayCount,
			"lastPlayed": track.LastPlayed,
		})
	}
	return toolResult{payload: map[string]interface{}{"tracks": items}}, nil
}

func parseTrackTimeArg(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("time value is required")
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time value %q", value)
}

func handleRecentListeningSummaryTool(ctx context.Context, runtime toolRuntime, args map[string]interface{}) (toolResult, error) {
	start, end, err := resolveSummaryWindow(args)
	if err != nil {
		return toolResult{}, err
	}

	trackLimit := clampInt(toolIntArg(args, "trackLimit", 20), 30)
	artistLimit := clampInt(toolIntArg(args, "artistLimit", 8), 12)
	cacheKey := buildRecentListeningSummaryCacheKey(start, end, trackLimit, artistLimit)
	if cached, ok := getRecentListeningSummaryCache(cacheKey); ok {
		return toolResult{rawResponse: cached}, nil
	}

	summary, err := runtime.resolver.DB.GetListeningSummary(ctx, start, end, trackLimit, artistLimit)
	if err != nil {
		return toolResult{}, err
	}

	artists := make([]map[string]interface{}, 0, len(summary.TopArtists))
	for _, artist := range summary.TopArtists {
		artists = append(artists, map[string]interface{}{
			"artistName": artist.ArtistName,
			"trackCount": artist.TrackCount,
		})
	}

	tracks := make([]map[string]interface{}, 0, len(summary.TopTracks))
	for _, track := range summary.TopTracks {
		tracks = append(tracks, map[string]interface{}{
			"id":         track.ID,
			"title":      track.Title,
			"artistName": track.ArtistName,
			"playCount":  track.PlayCount,
			"lastPlayed": track.LastPlayed,
		})
	}

	dataSource := "events"
	var note string
	if summary.TotalPlays == 0 {
		metaTracks, ok := buildMetadataFallbackTracks(ctx, runtime, start, end, trackLimit)
		if ok {
			tracks = metaTracks
			summary.TracksHeard = len(metaTracks)
			summary.ArtistsHeard = countDistinctArtists(metaTracks)
			dataSource = "track_metadata_fallback"
			note = "No scrobble events in this window; using track metadata (play_count/last_played)."
		}
	}

	payload := map[string]interface{}{
		"dataSource": dataSource,
		"recentListeningSummary": map[string]interface{}{
			"windowStart":  summary.WindowStart.Format(time.RFC3339),
			"windowEnd":    summary.WindowEnd.Format(time.RFC3339),
			"tracksHeard":  summary.TracksHeard,
			"totalPlays":   summary.TotalPlays,
			"artistsHeard": summary.ArtistsHeard,
			"topArtists":   artists,
			"topTracks":    tracks,
		},
	}
	if note != "" {
		payload["note"] = note
	}
	artistState := make([]recentListeningArtistState, 0, len(summary.TopArtists))
	for _, artist := range summary.TopArtists {
		if strings.TrimSpace(artist.ArtistName) == "" {
			continue
		}
		artistState = append(artistState, recentListeningArtistState{
			ArtistName: artist.ArtistName,
			TrackCount: artist.TrackCount,
		})
	}
	trackState := make([]recentListeningTrackState, 0, len(summary.TopTracks))
	for _, track := range summary.TopTracks {
		lastPlayed := ""
		if track.LastPlayed != nil {
			lastPlayed = track.LastPlayed.Format(time.RFC3339)
		}
		trackState = append(trackState, recentListeningTrackState{
			ID:         track.ID,
			Title:      track.Title,
			ArtistName: track.ArtistName,
			PlayCount:  track.PlayCount,
			LastPlayed: lastPlayed,
		})
	}
	state := recentListeningState{
		windowStart:  summary.WindowStart.Format(time.RFC3339),
		windowEnd:    summary.WindowEnd.Format(time.RFC3339),
		totalPlays:   summary.TotalPlays,
		tracksHeard:  summary.TracksHeard,
		artistsHeard: summary.ArtistsHeard,
		topArtists:   artistState,
		topTracks:    trackState,
	}
	return toolResult{
		payload:  payload,
		cacheKey: cacheKey,
		onSuccess: func(ctx context.Context) {
			setLastRecentListeningSummary(chatSessionIDFromContext(ctx), state)
		},
	}, nil
}

func handleSemanticTrackSearchTool(ctx context.Context, runtime toolRuntime, args map[string]interface{}) (toolResult, error) {
	queryText := strings.TrimSpace(toolStringArg(args, "queryText"))
	if queryText == "" {
		return toolResult{}, fmt.Errorf("queryText is required")
	}
	if strings.TrimSpace(runtime.embeddingsURL) == "" {
		return semanticTrackSearchUnavailableResult(queryText, args, "Semantic track search is unavailable because EMBEDDINGS_ENDPOINT is not configured."), nil
	}

	limit := clampInt(toolIntArg(args, "limit", 10), 25)
	start, end, err := resolveSemanticSearchWindow(args)
	if err != nil {
		return toolResult{}, err
	}

	embedding, err := fetchSingleEmbedding(ctx, runtime.embeddingsURL, queryText)
	if err != nil {
		log.Warn().Err(err).Str("query", queryText).Msg("Semantic track search unavailable")
		return semanticTrackSearchUnavailableResult(queryText, args, "Semantic track search is temporarily unavailable because the embeddings service could not be reached."), nil
	}
	matches, err := runtime.resolver.DB.FindSimilarTracksByEmbedding(ctx, embedding, limit, start, end)
	if err != nil {
		return toolResult{}, err
	}

	items := make([]map[string]interface{}, 0, len(matches))
	for _, match := range matches {
		var lastPlayed interface{}
		if match.LastPlayed != nil {
			lastPlayed = match.LastPlayed.Format(time.RFC3339)
		}
		items = append(items, map[string]interface{}{
			"id":         match.ID,
			"title":      match.Title,
			"artistName": match.ArtistName,
			"playCount":  match.PlayCount,
			"lastPlayed": lastPlayed,
			"similarity": match.Similarity,
		})
	}

	result := map[string]interface{}{
		"queryText": queryText,
		"matches":   items,
	}
	if start != nil && end != nil {
		result["windowStart"] = start.Format(time.RFC3339)
		result["windowEnd"] = end.Format(time.RFC3339)
	}
	return toolResult{payload: map[string]interface{}{"semanticTrackSearch": result}}, nil
}

func handleSemanticAlbumSearchTool(ctx context.Context, runtime toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "queryText", "artistName", "genre", "minYear", "maxYear", "limit"); err != nil {
		return toolResult{}, err
	}

	queryText := strings.TrimSpace(toolStringArg(args, "queryText"))
	if queryText == "" {
		return toolResult{}, fmt.Errorf("queryText is required")
	}
	if strings.TrimSpace(runtime.embeddingsURL) == "" {
		return semanticAlbumSearchUnavailableResult(queryText, args, "Semantic album search is unavailable because EMBEDDINGS_ENDPOINT is not configured."), nil
	}

	embedding, err := fetchSingleEmbedding(ctx, runtime.embeddingsURL, queryText)
	if err != nil {
		log.Warn().Err(err).Str("query", queryText).Msg("Semantic album search unavailable")
		return semanticAlbumSearchUnavailableResult(queryText, args, "Semantic album search is temporarily unavailable because the embeddings service could not be reached."), nil
	}

	limit := clampInt(toolIntArg(args, "limit", 10), 25)
	artistName := toolOptStringArg(args, "artistName")
	genre := toolOptStringArg(args, "genre")
	minYear := normalizeOptionalYear(toolOptIntArg(args, "minYear"))
	maxYear := normalizeOptionalYear(toolOptIntArg(args, "maxYear"))

	fetchLimit := limit * 4
	if fetchLimit < 12 {
		fetchLimit = 12
	}
	queryTerms := semanticDescriptorQueryTerms(queryText)
	if isMoodSemanticQuery(queryTerms) || len(semanticPhraseTerms(queryText)) > 0 {
		if fetchLimit < 24 {
			fetchLimit = 24
		}
	}
	if fetchLimit > 60 {
		fetchLimit = 60
	}
	matches, err := runtime.resolver.DB.FindSimilarAlbumsByEmbedding(ctx, embedding, fetchLimit, artistName, genre, minYear, maxYear)
	if err != nil {
		return toolResult{}, err
	}
	matches = rerankSemanticAlbumMatches(queryText, matches, limit)
	if (minYear != nil || maxYear != nil) && len(semanticPhraseTerms(queryText)) > 0 {
		overlapOnly := make([]db.SimilarAlbum, 0, len(matches))
		for _, match := range matches {
			if semanticAlbumHasPhraseOverlap(queryText, match) {
				overlapOnly = append(overlapOnly, match)
			}
		}
		matches = overlapOnly
	}

	items := make([]map[string]interface{}, 0, len(matches))
	sessionMatches := make([]semanticAlbumSearchMatch, 0, len(matches))
	for _, match := range matches {
		var lastPlayed interface{}
		lastPlayedRaw := ""
		if match.LastPlayed != nil {
			lastPlayed = match.LastPlayed.Format(time.RFC3339)
			lastPlayedRaw = match.LastPlayed.Format(time.RFC3339)
		}
		explanations := explainSemanticAlbumMatch(queryText, match, artistName, genre, minYear, maxYear)
		items = append(items, map[string]interface{}{
			"id":           match.ID,
			"name":         match.Name,
			"artistName":   match.ArtistName,
			"rating":       match.Rating,
			"playCount":    match.PlayCount,
			"lastPlayed":   lastPlayed,
			"year":         match.Year,
			"genre":        match.Genre,
			"similarity":   match.Similarity,
			"explanations": explanations,
		})
		sessionMatches = append(sessionMatches, semanticAlbumSearchMatch{
			ID:         match.ID,
			Name:       match.Name,
			ArtistName: match.ArtistName,
			Genre: func() string {
				if match.Genre == nil {
					return ""
				}
				return strings.TrimSpace(*match.Genre)
			}(),
			PlayCount:  match.PlayCount,
			LastPlayed: lastPlayedRaw,
		})
		if match.Year != nil {
			sessionMatches[len(sessionMatches)-1].Year = *match.Year
		}
	}
	setLastSemanticAlbumSearch(chatSessionIDFromContext(ctx), queryText, sessionMatches)
	setLastCreativeAlbumSet(chatSessionIDFromContext(ctx), "semantic_album_search", queryText, semanticMatchesToCreativeCandidates(sessionMatches))

	result := map[string]interface{}{
		"queryText": queryText,
		"matches":   items,
	}
	if artistName != nil {
		result["artistName"] = *artistName
	}
	if genre != nil {
		result["genre"] = *genre
	}
	if minYear != nil {
		result["minYear"] = *minYear
	}
	if maxYear != nil {
		result["maxYear"] = *maxYear
	}
	return toolResult{payload: map[string]interface{}{"semanticAlbumSearch": result}}, nil
}

func semanticMatchesToCreativeCandidates(matches []semanticAlbumSearchMatch) []creativeAlbumCandidate {
	out := make([]creativeAlbumCandidate, 0, len(matches))
	for _, match := range matches {
		out = append(out, creativeAlbumCandidate{
			ID:         strings.TrimSpace(match.ID),
			Name:       strings.TrimSpace(match.Name),
			ArtistName: strings.TrimSpace(match.ArtistName),
			Genre:      strings.TrimSpace(match.Genre),
			Year:       match.Year,
			PlayCount:  match.PlayCount,
			LastPlayed: strings.TrimSpace(match.LastPlayed),
		})
	}
	return out
}

func explainSemanticAlbumMatch(queryText string, match db.SimilarAlbum, artistName, genre *string, minYear, maxYear *int) []string {
	queryTerms := semanticDescriptorQueryTerms(queryText)
	explanations := make([]string, 0, 4)

	if artistName != nil && strings.TrimSpace(*artistName) != "" {
		explanations = append(explanations, fmt.Sprintf("artist filter: %s", strings.TrimSpace(*artistName)))
	}
	if genre != nil && strings.TrimSpace(*genre) != "" {
		explanations = append(explanations, fmt.Sprintf("genre filter: %s", strings.TrimSpace(*genre)))
	}
	if minYear != nil || maxYear != nil {
		switch {
		case minYear != nil && maxYear != nil:
			explanations = append(explanations, fmt.Sprintf("year filter: %d-%d", *minYear, *maxYear))
		case minYear != nil:
			explanations = append(explanations, fmt.Sprintf("year filter: since %d", *minYear))
		case maxYear != nil:
			explanations = append(explanations, fmt.Sprintf("year filter: through %d", *maxYear))
		}
	}

	if genreMatch := semanticTermsFromGenre(queryTerms, match.Genre); genreMatch != "" {
		explanations = append(explanations, "genre matched: "+genreMatch)
	}

	mbGenres := metadataStringSlice(match.Metadata, "musicbrainz", "genres")
	if matched := semanticTermsFromSlice(queryTerms, mbGenres); matched != "" {
		explanations = append(explanations, "MusicBrainz genres matched: "+matched)
	}

	mbTags := metadataStringSlice(match.Metadata, "musicbrainz", "tags")
	if matched := semanticTermsFromSlice(queryTerms, mbTags); matched != "" {
		explanations = append(explanations, "MusicBrainz tags matched: "+matched)
	}

	lastfmTags := metadataStringSlice(match.Metadata, "lastfm", "tags")
	if matched := semanticTermsFromSlice(queryTerms, lastfmTags); matched != "" {
		explanations = append(explanations, "Last.fm tags matched: "+matched)
	}

	if len(explanations) == 0 {
		if len(mbTags) > 0 {
			explanations = append(explanations, "MusicBrainz tags: "+joinFirstN(mbTags, 2))
		} else if len(lastfmTags) > 0 {
			explanations = append(explanations, "Last.fm tags: "+joinFirstN(lastfmTags, 2))
		} else if len(mbGenres) > 0 {
			explanations = append(explanations, "MusicBrainz genres: "+joinFirstN(mbGenres, 2))
		} else if match.Genre != nil && strings.TrimSpace(*match.Genre) != "" {
			explanations = append(explanations, "genre: "+strings.TrimSpace(*match.Genre))
		}
	}

	return dedupeStrings(explanations)
}

func normalizeOptionalYear(value *int) *int {
	if value == nil || *value <= 0 {
		return nil
	}
	return value
}

func rerankSemanticAlbumMatches(queryText string, matches []db.SimilarAlbum, limit int) []db.SimilarAlbum {
	if len(matches) <= 1 {
		return matches
	}

	queryTerms := semanticDescriptorQueryTerms(queryText)
	expandedTerms := expandSemanticQueryTerms(queryTerms)
	phraseTerms := semanticPhraseTerms(queryText)
	type scoredAlbum struct {
		item          db.SimilarAlbum
		score         float64
		phraseOverlap bool
	}
	scored := make([]scoredAlbum, 0, len(matches))
	for _, match := range matches {
		score := match.Similarity
		title := strings.ToLower(strings.TrimSpace(match.Name))
		artist := strings.ToLower(strings.TrimSpace(match.ArtistName))
		genreValues := splitGenreValues(match.Genre)
		genre := strings.ToLower(strings.Join(genreValues, ", "))
		mbGenres := metadataStringSlice(match.Metadata, "musicbrainz", "genres")
		mbTags := metadataStringSlice(match.Metadata, "musicbrainz", "tags")
		lastfmTags := metadataStringSlice(match.Metadata, "lastfm", "tags")
		phraseGenreScore := semanticDescriptorScore(phraseTerms, genreValues, 0.04, 0.1)
		phraseMBGenreScore := semanticDescriptorScore(phraseTerms, mbGenres, 0.055, 0.12)
		phraseMBTagScore := semanticDescriptorScore(phraseTerms, mbTags, 0.065, 0.14)
		phraseLastFMTagScore := semanticDescriptorScore(phraseTerms, lastfmTags, 0.06, 0.13)
		phraseScore := phraseGenreScore + phraseMBGenreScore + phraseMBTagScore + phraseLastFMTagScore
		hasPhraseDescriptorOverlap := phraseScore > 0
		if isCompilationLikeAlbum(title) {
			score -= 0.06
		}
		score -= semanticAlbumVariantPenalty(match.Name)
		if genre != "" || len(mbGenres) > 0 || len(mbTags) > 0 || len(lastfmTags) > 0 {
			score += 0.015
		}
		if match.Year != nil && *match.Year > 0 {
			score += 0.01
		}
		if match.Rating > 0 {
			score += 0.005
		}
		if match.PlayCount > 0 {
			score += 0.003
		}
		for _, term := range queryTerms {
			if term == "" {
				continue
			}
			if strings.Contains(title, term) {
				score += 0.01
			}
			if strings.Contains(artist, term) {
				score += 0.006
			}
			if genre != "" && strings.Contains(genre, term) {
				score += 0.018
			}
		}
		genreDescriptorScore := semanticDescriptorScore(expandedTerms, genreValues, 0.018, 0.04)
		mbGenreScore := semanticDescriptorScore(expandedTerms, mbGenres, 0.024, 0.06)
		mbTagScore := semanticDescriptorScore(expandedTerms, mbTags, 0.03, 0.09)
		lastfmTagScore := semanticDescriptorScore(expandedTerms, lastfmTags, 0.028, 0.085)
		score += genreDescriptorScore + mbGenreScore + mbTagScore + lastfmTagScore + phraseScore
		hasDescriptorOverlap := genreDescriptorScore > 0 || mbGenreScore > 0 || mbTagScore > 0 || lastfmTagScore > 0
		hasPhraseOverlap := phraseScore > 0
		if len(expandedTerms) > 0 && len(mbTags) > 0 && mbTagScore == 0 {
			score -= 0.012
		}
		if len(expandedTerms) > 0 && len(lastfmTags) > 0 && lastfmTagScore == 0 {
			score -= 0.01
		}
		if len(phraseTerms) > 0 && !hasPhraseOverlap {
			score -= 0.22
		}
		if len(phraseTerms) > 0 && hasPhraseDescriptorOverlap {
			score += 0.025
		}
		if hasDescriptorOverlap || hasPhraseOverlap {
			score += 0.018
		} else if isMoodSemanticQuery(queryTerms) {
			score -= 0.12
		}
		scored = append(scored, scoredAlbum{item: match, score: score, phraseOverlap: hasPhraseDescriptorOverlap})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			if scored[i].item.PlayCount == scored[j].item.PlayCount {
				return strings.ToLower(scored[i].item.Name) < strings.ToLower(scored[j].item.Name)
			}
			return scored[i].item.PlayCount > scored[j].item.PlayCount
		}
		return scored[i].score > scored[j].score
	})

	if limit <= 0 || limit > len(scored) {
		limit = len(scored)
	}
	phraseOverlapCount := 0
	for _, candidate := range scored {
		if candidate.phraseOverlap {
			phraseOverlapCount++
		}
	}
	requirePhraseOverlap := len(phraseTerms) > 0 && phraseOverlapCount >= 3
	out := make([]db.SimilarAlbum, 0, limit)
	seen := make(map[string]struct{}, len(scored))
	for _, candidate := range scored {
		if requirePhraseOverlap && !candidate.phraseOverlap {
			continue
		}
		key := semanticAlbumCanonicalKey(candidate.item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, candidate.item)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func semanticAlbumHasPhraseOverlap(queryText string, match db.SimilarAlbum) bool {
	phraseTerms := semanticPhraseTerms(queryText)
	if len(phraseTerms) == 0 {
		return false
	}
	genreValues := splitGenreValues(match.Genre)
	if semanticDescriptorScore(phraseTerms, genreValues, 0.04, 0.1) > 0 {
		return true
	}
	if semanticDescriptorScore(phraseTerms, metadataStringSlice(match.Metadata, "musicbrainz", "genres"), 0.055, 0.12) > 0 {
		return true
	}
	if semanticDescriptorScore(phraseTerms, metadataStringSlice(match.Metadata, "musicbrainz", "tags"), 0.065, 0.14) > 0 {
		return true
	}
	return semanticDescriptorScore(phraseTerms, metadataStringSlice(match.Metadata, "lastfm", "tags"), 0.06, 0.13) > 0
}

func semanticAlbumVariantPenalty(name string) float64 {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return 0
	}
	penalty := 0.0
	cues := []struct {
		needle string
		value  float64
	}{
		{"deluxe", 0.05},
		{"super deluxe", 0.06},
		{"remaster", 0.05},
		{"remastered", 0.05},
		{"expanded", 0.04},
		{"anniversary", 0.04},
		{"bonus", 0.03},
		{"disc ", 0.03},
		{" cd", 0.02},
		{"hd remastered", 0.05},
		{"special edition", 0.04},
	}
	for _, cue := range cues {
		if strings.Contains(lower, cue.needle) {
			penalty += cue.value
		}
	}
	if strings.Contains(lower, "(") || strings.Contains(lower, "[") || strings.Contains(lower, "{") {
		penalty += 0.015
	}
	if penalty > 0.14 {
		return 0.14
	}
	return penalty
}

func semanticAlbumCanonicalKey(match db.SimilarAlbum) string {
	return canonicalizeSemanticAlbumText(match.ArtistName) + "::" + canonicalizeSemanticAlbumText(match.Name)
}

func canonicalizeSemanticAlbumText(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return ""
	}
	var b strings.Builder
	depthParen := 0
	depthBracket := 0
	depthBrace := 0
	for _, r := range lower {
		switch r {
		case '(':
			depthParen++
			continue
		case ')':
			if depthParen > 0 {
				depthParen--
			}
			continue
		case '[':
			depthBracket++
			continue
		case ']':
			if depthBracket > 0 {
				depthBracket--
			}
			continue
		case '{':
			depthBrace++
			continue
		case '}':
			if depthBrace > 0 {
				depthBrace--
			}
			continue
		}
		if depthParen > 0 || depthBracket > 0 || depthBrace > 0 {
			continue
		}
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	clean := strings.Join(strings.Fields(b.String()), " ")
	replacements := []struct {
		old string
		new string
	}{
		{" deluxe edition ", " "},
		{" deluxe ", " "},
		{" super deluxe ", " "},
		{" remaster ", " "},
		{" remastered ", " "},
		{" expanded edition ", " "},
		{" expanded ", " "},
		{" anniversary edition ", " "},
		{" anniversary ", " "},
		{" special edition ", " "},
		{" bonus edition ", " "},
		{" edition ", " "},
		{" disc 1 ", " "},
		{" disc 2 ", " "},
		{" disc 3 ", " "},
		{" disc 4 ", " "},
		{" cd 1 ", " "},
		{" cd 2 ", " "},
		{" mono ", " "},
		{" stereo ", " "},
	}
	clean = " " + clean + " "
	for _, replacement := range replacements {
		clean = strings.ReplaceAll(clean, replacement.old, replacement.new)
	}
	clean = strings.Join(strings.Fields(clean), " ")
	return clean
}

var semanticCueExpansions = map[string][]string{
	"nocturnal":   {"night", "midnight", "dark", "ambient", "atmospheric", "dream", "dreamy"},
	"night":       {"nocturnal", "midnight", "dark", "ambient", "atmospheric"},
	"late-night":  {"nocturnal", "night", "midnight", "after-hours", "atmospheric"},
	"warm":        {"organic", "soul", "soulful", "folk", "acoustic", "lush"},
	"spacious":    {"space", "ambient", "atmospheric", "lush", "expansive"},
	"intimate":    {"soft", "acoustic", "chamber", "delicate", "quiet", "close"},
	"aggressive":  {"heavy", "loud", "intense", "industrial", "hard", "punk"},
	"abrasive":    {"harsh", "industrial", "noise", "aggressive", "heavy"},
	"weird":       {"experimental", "avant", "avant-garde", "psychedelic", "abstract", "eccentric"},
	"strange":     {"experimental", "avant", "abstract", "weird", "psychedelic"},
	"melancholic": {"sad", "melancholy", "somber", "dark", "moody"},
	"rainy":       {"melancholic", "ambient", "soft", "moody", "atmospheric"},
	"meditative":  {"ambient", "calm", "drone", "minimal", "quiet"},
	"cinematic":   {"soundtrack", "orchestral", "epic", "atmospheric", "dramatic"},
}

var semanticPhraseExpansions = map[string][]string{
	"dark ambient": {"dark ambient", "ambient", "drone", "nocturnal", "atmospheric"},
	"dream pop":    {"dream pop", "dreampop", "dreamy", "ethereal", "shoegaze", "chillwave"},
	"late night":   {"late night", "nocturnal", "midnight", "after-hours", "atmospheric", "downtempo"},
}

var semanticPhraseSuppressedTerms = map[string][]string{
	"dream pop": {"pop"},
}

func tokenizeQueryText(queryText string) []string {
	parts := strings.Fields(strings.ToLower(queryText))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, ".,!?\"'()[]{}")
		if len(part) < 3 {
			continue
		}
		out = append(out, part)
	}
	return out
}

func semanticDescriptorQueryTerms(queryText string) []string {
	terms := tokenizeQueryText(queryText)
	if len(terms) == 0 {
		return nil
	}

	suppressed := make(map[string]struct{})
	lower := strings.ToLower(strings.ReplaceAll(queryText, "-", " "))
	for phrase, termsToSuppress := range semanticPhraseSuppressedTerms {
		if !strings.Contains(lower, phrase) {
			continue
		}
		for _, term := range termsToSuppress {
			term = strings.TrimSpace(strings.ToLower(term))
			if term != "" {
				suppressed[term] = struct{}{}
			}
		}
	}
	if len(suppressed) == 0 {
		return terms
	}

	filtered := make([]string, 0, len(terms))
	for _, term := range terms {
		if _, ok := suppressed[term]; ok {
			continue
		}
		filtered = append(filtered, term)
	}
	if len(filtered) == 0 {
		return terms
	}
	return filtered
}

func expandSemanticQueryTerms(queryTerms []string) []string {
	if len(queryTerms) == 0 {
		return nil
	}
	out := make([]string, 0, len(queryTerms)*2)
	for _, term := range queryTerms {
		term = strings.TrimSpace(strings.ToLower(term))
		if term == "" {
			continue
		}
		out = append(out, term)
		if expansions, ok := semanticCueExpansions[term]; ok {
			out = append(out, expansions...)
		}
	}
	return dedupeStrings(out)
}

func semanticPhraseTerms(queryText string) []string {
	lower := strings.ToLower(strings.ReplaceAll(queryText, "-", " "))
	out := make([]string, 0, 6)
	for phrase, expansions := range semanticPhraseExpansions {
		if !strings.Contains(lower, phrase) {
			continue
		}
		out = append(out, phrase)
		out = append(out, expansions...)
	}
	return dedupeStrings(out)
}

func splitGenreValues(genre *string) []string {
	if genre == nil {
		return nil
	}
	parts := strings.Split(strings.TrimSpace(*genre), ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func semanticTermsFromGenre(queryTerms []string, genre *string) string {
	if genre == nil {
		return ""
	}
	parts := strings.Split(strings.TrimSpace(*genre), ",")
	return semanticTermsFromSlice(queryTerms, parts)
}

func semanticTermsFromSlice(queryTerms, values []string) string {
	if len(values) == 0 {
		return ""
	}
	matches := make([]string, 0, len(values))
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		lower := strings.ToLower(clean)
		for _, term := range queryTerms {
			if term != "" && strings.Contains(lower, term) {
				matches = append(matches, clean)
				break
			}
		}
	}
	if len(matches) == 0 {
		return ""
	}
	return joinFirstN(dedupeStrings(matches), 2)
}

func metadataStringSlice(metadata map[string]interface{}, section, key string) []string {
	sectionMap, ok := metadata[section].(map[string]interface{})
	if !ok {
		return nil
	}
	raw, ok := sectionMap[key]
	if !ok {
		return nil
	}
	switch items := raw.(type) {
	case []string:
		out := make([]string, 0, len(items))
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item != "" && !isLowSignalSemanticValue(item) {
				out = append(out, item)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(items))
		for _, item := range items {
			text, _ := item.(string)
			text = strings.TrimSpace(text)
			if text != "" && !isLowSignalSemanticValue(text) {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func joinFirstN(values []string, n int) string {
	if len(values) == 0 {
		return ""
	}
	if n <= 0 || n > len(values) {
		n = len(values)
	}
	return strings.Join(values[:n], ", ")
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func semanticDescriptorScore(expandedTerms, values []string, weight, cap float64) float64 {
	if len(expandedTerms) == 0 || len(values) == 0 {
		return 0
	}
	matches := 0
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		for _, term := range expandedTerms {
			if term != "" && strings.Contains(value, strings.ToLower(term)) {
				matches++
				break
			}
		}
	}
	score := float64(matches) * weight
	if score > cap {
		return cap
	}
	return score
}

func isMoodSemanticQuery(queryTerms []string) bool {
	if len(queryTerms) == 0 {
		return false
	}
	for _, term := range queryTerms {
		if _, ok := semanticCueExpansions[term]; ok {
			return true
		}
	}
	return false
}

func isLowSignalSemanticValue(value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return true
	}
	if len(v) < 3 {
		return true
	}
	noisePhrases := []string{
		"5+ wochen",
		"discogs/",
		"discogs\\",
		"the most popular album released every year",
		"setlist",
		"rym",
		"1001 albums",
		"best ever albums",
	}
	for _, phrase := range noisePhrases {
		if strings.Contains(v, phrase) {
			return true
		}
	}
	hasLetter := false
	for _, r := range v {
		if unicode.IsLetter(r) {
			hasLetter = true
			break
		}
	}
	if !hasLetter {
		return true
	}
	digitCount := 0
	for _, r := range v {
		if unicode.IsDigit(r) {
			digitCount++
		}
	}
	if digitCount >= len([]rune(v))/2 {
		return true
	}
	return false
}

func isCompilationLikeAlbum(title string) bool {
	title = strings.ToLower(strings.TrimSpace(title))
	if title == "" {
		return false
	}
	cues := []string{
		"greatest hits",
		"best of",
		"anthology",
		"collection",
		"essential",
	}
	for _, cue := range cues {
		if strings.Contains(title, cue) {
			return true
		}
	}
	return compilationYearRangePattern.MatchString(title)
}

func handleDiscoverAlbumsTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	candidates, meta, err := discoverAlbums(ctx, args)
	if err != nil {
		return toolResult{}, err
	}
	return toolResult{
		payload: map[string]interface{}{
			"discoverAlbums": map[string]interface{}{
				"query":      meta["query"],
				"limit":      meta["limit"],
				"count":      len(candidates),
				"candidates": candidates,
				"note":       "Discovery results only. No library changes will happen unless you ask explicitly.",
			},
		},
	}, nil
}

func handlePlanDiscoverPlaylistTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	candidates, meta, err := planDiscoverPlaylist(ctx, args)
	if err != nil {
		return toolResult{}, err
	}
	return toolResult{
		payload: map[string]interface{}{
			"planDiscoverPlaylist": map[string]interface{}{
				"prompt":           meta["prompt"],
				"normalizedIntent": meta["normalizedIntent"],
				"playlistName":     meta["playlistName"],
				"trackCount":       meta["trackCount"],
				"candidates":       candidates,
				"note":             "This step is read-only. You can ask me to resolve tracks in Navidrome next.",
			},
		},
	}, nil
}

func handlePlaylistPlanDetailsTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "selection"); err != nil {
		return toolResult{}, err
	}
	sessionID := chatSessionIDFromContext(ctx)
	prompt, playlistName, plannedAt, candidates := getLastPlannedPlaylist(sessionID)
	selected, err := selectPlaylistCandidates(candidates, toolStringArg(args, "selection"))
	if err != nil {
		return toolResult{}, err
	}
	resolvedAt, resolved := getLastResolvedPlaylist(sessionID)
	resolvedByKey := make(map[string]resolvedPlaylistTrack, len(resolved))
	for _, item := range resolved {
		key := plannedPlaylistTrackKey(item.Rank, item.ArtistName, item.TrackTitle)
		resolvedByKey[key] = item
	}

	items := make([]map[string]interface{}, 0, len(selected))
	resolvedCounts := map[string]int{
		"resolved":   0,
		"available":  0,
		"missing":    0,
		"ambiguous":  0,
		"errors":     0,
		"unresolved": 0,
	}
	for _, candidate := range selected {
		item := map[string]interface{}{
			"rank":       candidate.Rank,
			"artistName": candidate.ArtistName,
			"trackTitle": candidate.TrackTitle,
		}
		if strings.TrimSpace(candidate.Reason) != "" {
			item["reason"] = candidate.Reason
		}
		if strings.TrimSpace(candidate.SourceHint) != "" {
			item["sourceHint"] = candidate.SourceHint
		}
		if resolvedItem, ok := resolvedByKey[plannedPlaylistTrackKey(candidate.Rank, candidate.ArtistName, candidate.TrackTitle)]; ok {
			item["status"] = resolvedItem.Status
			if strings.TrimSpace(resolvedItem.SongID) != "" {
				item["songId"] = resolvedItem.SongID
			}
			if strings.TrimSpace(resolvedItem.MatchedArtist) != "" {
				item["matchedArtist"] = resolvedItem.MatchedArtist
			}
			if strings.TrimSpace(resolvedItem.MatchedTitle) != "" {
				item["matchedTitle"] = resolvedItem.MatchedTitle
			}
			if resolvedItem.MatchCount > 0 {
				item["matchCount"] = resolvedItem.MatchCount
			}
			if strings.TrimSpace(resolvedItem.Detail) != "" {
				item["detail"] = resolvedItem.Detail
			}
			resolvedCounts["resolved"]++
			switch resolvedItem.Status {
			case "available":
				resolvedCounts["available"]++
			case "missing":
				resolvedCounts["missing"]++
			case "ambiguous":
				resolvedCounts["ambiguous"]++
			default:
				resolvedCounts["errors"]++
			}
		} else {
			item["status"] = "planned"
			resolvedCounts["unresolved"]++
		}
		items = append(items, item)
	}

	payload := map[string]interface{}{
		"prompt":       prompt,
		"playlistName": playlistName,
		"plannedAt":    plannedAt.Format(time.RFC3339),
		"selection":    strings.TrimSpace(toolStringArg(args, "selection")),
		"counts": map[string]int{
			"planned": len(selected),
		},
		"tracks": items,
		"note":   "This step is read-only. Use it to inspect the current playlist plan before resolving, creating, or queueing anything.",
	}
	if resolvedCounts["resolved"] > 0 {
		payload["resolutionCounts"] = resolvedCounts
		payload["resolvedAt"] = resolvedAt.Format(time.RFC3339)
	}

	return toolResult{
		payload: map[string]interface{}{
			"playlistPlanDetails": payload,
		},
	}, nil
}

func plannedPlaylistTrackKey(rank int, artistName, trackTitle string) string {
	return fmt.Sprintf("%d|%s|%s", rank, normalizeSearchTerm(artistName), normalizeSearchTerm(trackTitle))
}

func handleNavidromePlaylistsTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "limit", "query"); err != nil {
		return toolResult{}, err
	}
	client, err := newNavidromeClientFromEnv()
	if err != nil {
		return toolResult{}, err
	}
	playlists, err := client.GetPlaylists(ctx)
	if err != nil {
		return toolResult{}, err
	}

	query := strings.TrimSpace(strings.ToLower(toolStringArg(args, "query")))
	if query != "" {
		filtered := make([]navidromePlaylist, 0, len(playlists))
		for _, playlist := range playlists {
			if strings.Contains(strings.ToLower(strings.TrimSpace(playlist.Name)), query) {
				filtered = append(filtered, playlist)
			}
		}
		playlists = filtered
	}
	limit := toolIntArg(args, "limit", 25)
	if limit > 0 && len(playlists) > limit {
		playlists = playlists[:limit]
	}

	items := make([]map[string]interface{}, 0, len(playlists))
	for _, playlist := range playlists {
		items = append(items, map[string]interface{}{
			"id":        playlist.ID,
			"name":      playlist.Name,
			"songCount": playlist.SongCount,
		})
	}
	return toolResult{payload: map[string]interface{}{
		"navidromePlaylists": map[string]interface{}{
			"playlists": items,
			"query":     strings.TrimSpace(toolStringArg(args, "query")),
		},
	}}, nil
}

func handleNavidromePlaylistTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "playlistId", "playlistName"); err != nil {
		return toolResult{}, err
	}
	client, err := newNavidromeClientFromEnv()
	if err != nil {
		return toolResult{}, err
	}
	playlist, err := resolveNavidromePlaylist(ctx, client, toolStringArg(args, "playlistId"), toolStringArg(args, "playlistName"))
	if err != nil {
		return toolResult{}, err
	}
	pending, err := pendingPlaylistTracksForPlaylist(playlist.Name)
	if err != nil {
		return toolResult{}, err
	}
	return toolResult{payload: map[string]interface{}{
		"navidromePlaylist": buildNavidromePlaylistPayload(playlist, pending),
	}}, nil
}

func handleNavidromePlaylistStateTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "playlistId", "playlistName"); err != nil {
		return toolResult{}, err
	}
	client, err := newNavidromeClientFromEnv()
	if err != nil {
		return toolResult{}, err
	}
	playlist, err := resolveNavidromePlaylist(ctx, client, toolStringArg(args, "playlistId"), toolStringArg(args, "playlistName"))
	if err != nil {
		return toolResult{}, err
	}
	pending, err := pendingPlaylistTracksForPlaylist(playlist.Name)
	if err != nil {
		return toolResult{}, err
	}
	return toolResult{payload: map[string]interface{}{
		"navidromePlaylistState": buildNavidromePlaylistPayload(playlist, pending),
	}}, nil
}

func buildNavidromePlaylistPayload(playlist *navidromePlaylistDetail, pending []pendingPlaylistTrack) map[string]interface{} {
	tracks := make([]map[string]interface{}, 0, len(playlist.Entries))
	items := make([]map[string]interface{}, 0, len(playlist.Entries)+len(pending))
	for index, entry := range playlist.Entries {
		track := map[string]interface{}{
			"index":      index,
			"id":         entry.ID,
			"title":      entry.Title,
			"artistName": entry.Artist,
		}
		tracks = append(tracks, track)
		items = append(items, map[string]interface{}{
			"state":      "saved",
			"index":      index,
			"id":         entry.ID,
			"title":      entry.Title,
			"artistName": entry.Artist,
		})
	}
	pendingFetch := 0
	for _, item := range pending {
		if item.State == "pending_fetch" {
			pendingFetch++
		}
		items = append(items, map[string]interface{}{
			"state":        item.State,
			"jobId":        item.JobID,
			"rank":         item.Rank,
			"title":        item.TrackTitle,
			"artistName":   item.ArtistName,
			"attempts":     item.Attempts,
			"lastError":    item.LastError,
			"playlistName": item.PlaylistName,
		})
	}
	return map[string]interface{}{
		"id":        playlist.ID,
		"name":      playlist.Name,
		"songCount": len(playlist.Entries),
		"counts": map[string]int{
			"saved":         len(playlist.Entries),
			"pending_fetch": pendingFetch,
			"total":         len(playlist.Entries) + len(pending),
		},
		"tracks": tracks,
		"items":  items,
	}
}

func handleAddTrackToNavidromePlaylistTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "playlistId", "playlistName", "artistName", "trackTitle"); err != nil {
		return toolResult{}, err
	}
	artistName := strings.TrimSpace(toolStringArg(args, "artistName"))
	trackTitle := strings.TrimSpace(toolStringArg(args, "trackTitle"))
	if artistName == "" || trackTitle == "" {
		return toolResult{}, fmt.Errorf("artistName and trackTitle are required")
	}
	client, err := newNavidromeClientFromEnv()
	if err != nil {
		return toolResult{}, err
	}
	playlist, err := resolveNavidromePlaylist(ctx, client, toolStringArg(args, "playlistId"), toolStringArg(args, "playlistName"))
	if err != nil {
		return toolResult{}, err
	}
	matches, err := client.SearchTrackByArtistTitle(ctx, artistName, trackTitle)
	if err != nil {
		return toolResult{}, err
	}
	if len(matches) == 0 {
		return toolResult{}, fmt.Errorf("no Navidrome track matched %q by %q", trackTitle, artistName)
	}
	songID := strings.TrimSpace(matches[0].ID)
	if songID == "" {
		return toolResult{}, fmt.Errorf("matched track had no id")
	}
	existingIDs, err := client.GetPlaylistSongIDs(ctx, playlist.ID)
	if err != nil {
		return toolResult{}, err
	}
	for _, existingID := range existingIDs {
		if existingID == songID {
			return toolResult{payload: map[string]interface{}{
				"addTrackToNavidromePlaylist": map[string]interface{}{
					"playlistId":   playlist.ID,
					"playlistName": playlist.Name,
					"artistName":   matches[0].Artist,
					"trackTitle":   matches[0].Title,
					"added":        false,
					"reason":       "already_present",
				},
			}}, nil
		}
	}
	if err := client.UpdatePlaylistAddSongs(ctx, playlist.ID, []string{songID}); err != nil {
		return toolResult{}, err
	}
	return toolResult{payload: map[string]interface{}{
		"addTrackToNavidromePlaylist": map[string]interface{}{
			"playlistId":   playlist.ID,
			"playlistName": playlist.Name,
			"artistName":   matches[0].Artist,
			"trackTitle":   matches[0].Title,
			"added":        true,
		},
	}}, nil
}

func handleAddOrQueueTrackToNavidromePlaylistTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "playlistId", "playlistName", "artistName", "trackTitle"); err != nil {
		return toolResult{}, err
	}
	artistName := strings.TrimSpace(toolStringArg(args, "artistName"))
	trackTitle := strings.TrimSpace(toolStringArg(args, "trackTitle"))
	if artistName == "" || trackTitle == "" {
		return toolResult{}, fmt.Errorf("artistName and trackTitle are required")
	}
	client, err := newNavidromeClientFromEnv()
	if err != nil {
		return toolResult{}, err
	}
	playlist, err := resolveNavidromePlaylist(ctx, client, toolStringArg(args, "playlistId"), toolStringArg(args, "playlistName"))
	if err != nil {
		return toolResult{}, err
	}

	basePayload := map[string]interface{}{
		"playlistId":   playlist.ID,
		"playlistName": playlist.Name,
		"artistName":   artistName,
		"trackTitle":   trackTitle,
	}

	matches, err := client.SearchTrackByArtistTitle(ctx, artistName, trackTitle)
	if err != nil {
		return toolResult{}, err
	}
	if len(matches) == 0 {
		pending, err := pendingPlaylistTracksForPlaylist(playlist.Name)
		if err != nil {
			return toolResult{}, err
		}
		targetArtist := normalizeSearchTerm(artistName)
		targetTitle := normalizeSearchTerm(trackTitle)
		for _, item := range pending {
			if normalizeSearchTerm(item.ArtistName) == targetArtist && normalizeSearchTerm(item.TrackTitle) == targetTitle {
				payload := cloneToolPayload(basePayload)
				payload["mode"] = "already_queued"
				payload["queueFile"] = item.QueueFile
				payload["reconcileJobFile"] = item.JobID
				payload["state"] = item.State
				return toolResult{payload: map[string]interface{}{
					"addOrQueueTrackToNavidromePlaylist": payload,
				}}, nil
			}
		}
		queued, queueFile, itemID, err := enqueuePlaylistReconcileTracks(playlist.Name, []resolvedPlaylistTrack{{
			Rank:       1,
			ArtistName: artistName,
			TrackTitle: trackTitle,
			Status:     "missing",
		}})
		if err != nil {
			return toolResult{}, err
		}
		if queued > 0 {
			triggerPlaylistReconcile()
		}
		payload := cloneToolPayload(basePayload)
		payload["mode"] = "queued"
		payload["queued"] = queued > 0
		payload["queueFile"] = queueFile
		payload["reconcileJobFile"] = itemID
		return toolResult{payload: map[string]interface{}{
			"addOrQueueTrackToNavidromePlaylist": payload,
		}}, nil
	}
	if len(matches) > 1 {
		candidates := make([]map[string]interface{}, 0, minInt(len(matches), 3))
		for _, match := range matches[:minInt(len(matches), 3)] {
			candidates = append(candidates, map[string]interface{}{
				"songId":     strings.TrimSpace(match.ID),
				"artistName": strings.TrimSpace(match.Artist),
				"trackTitle": strings.TrimSpace(match.Title),
			})
		}
		payload := cloneToolPayload(basePayload)
		payload["mode"] = "ambiguous"
		payload["matchCount"] = len(matches)
		payload["matches"] = candidates
		return toolResult{payload: map[string]interface{}{
			"addOrQueueTrackToNavidromePlaylist": payload,
		}}, nil
	}

	songID := strings.TrimSpace(matches[0].ID)
	if songID == "" {
		return toolResult{}, fmt.Errorf("matched track had no id")
	}
	payload := cloneToolPayload(basePayload)
	payload["artistName"] = matches[0].Artist
	payload["trackTitle"] = matches[0].Title
	payload["songId"] = songID
	existingIDs, err := client.GetPlaylistSongIDs(ctx, playlist.ID)
	if err != nil {
		return toolResult{}, err
	}
	for _, existingID := range existingIDs {
		if existingID == songID {
			payload["mode"] = "already_present"
			payload["added"] = false
			return toolResult{payload: map[string]interface{}{
				"addOrQueueTrackToNavidromePlaylist": payload,
			}}, nil
		}
	}
	if err := client.UpdatePlaylistAddSongs(ctx, playlist.ID, []string{songID}); err != nil {
		return toolResult{}, err
	}
	payload["mode"] = "added"
	payload["added"] = true
	return toolResult{payload: map[string]interface{}{
		"addOrQueueTrackToNavidromePlaylist": payload,
	}}, nil
}

func handleQueueTrackForNavidromePlaylistTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "playlistId", "playlistName", "artistName", "trackTitle"); err != nil {
		return toolResult{}, err
	}
	artistName := strings.TrimSpace(toolStringArg(args, "artistName"))
	trackTitle := strings.TrimSpace(toolStringArg(args, "trackTitle"))
	if artistName == "" || trackTitle == "" {
		return toolResult{}, fmt.Errorf("artistName and trackTitle are required")
	}
	client, err := newNavidromeClientFromEnv()
	if err != nil {
		return toolResult{}, err
	}
	playlist, err := resolveNavidromePlaylist(ctx, client, toolStringArg(args, "playlistId"), toolStringArg(args, "playlistName"))
	if err != nil {
		return toolResult{}, err
	}
	queued, queueFile, itemID, err := enqueuePlaylistReconcileTracks(playlist.Name, []resolvedPlaylistTrack{{
		Rank:       1,
		ArtistName: artistName,
		TrackTitle: trackTitle,
		Status:     "missing",
	}})
	if err != nil {
		return toolResult{}, err
	}
	if queued > 0 {
		triggerPlaylistReconcile()
	}

	return toolResult{payload: map[string]interface{}{
		"queueTrackForNavidromePlaylist": map[string]interface{}{
			"playlistId":       playlist.ID,
			"playlistName":     playlist.Name,
			"artistName":       artistName,
			"trackTitle":       trackTitle,
			"queueFile":        queueFile,
			"reconcileJobFile": itemID,
			"queued":           true,
		},
	}}, nil
}

func cloneToolPayload(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func handleRemoveTrackFromNavidromePlaylistTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "playlistId", "playlistName", "selection"); err != nil {
		return toolResult{}, err
	}
	selection := strings.TrimSpace(toolStringArg(args, "selection"))
	if selection == "" {
		return toolResult{}, fmt.Errorf("selection is required")
	}
	client, err := newNavidromeClientFromEnv()
	if err != nil {
		return toolResult{}, err
	}
	playlist, err := resolveNavidromePlaylist(ctx, client, toolStringArg(args, "playlistId"), toolStringArg(args, "playlistName"))
	if err != nil {
		return toolResult{}, err
	}
	indexes, removed, err := selectNavidromePlaylistEntries(playlist.Entries, selection)
	if err != nil {
		return toolResult{}, err
	}
	if err := client.UpdatePlaylistRemoveIndexes(ctx, playlist.ID, indexes); err != nil {
		return toolResult{}, err
	}
	items := make([]string, 0, len(removed))
	for _, entry := range removed {
		label := strings.TrimSpace(entry.Title)
		if strings.TrimSpace(entry.Artist) != "" {
			label += " by " + strings.TrimSpace(entry.Artist)
		}
		items = append(items, label)
	}
	return toolResult{payload: map[string]interface{}{
		"removeTrackFromNavidromePlaylist": map[string]interface{}{
			"playlistId":   playlist.ID,
			"playlistName": playlist.Name,
			"selection":    selection,
			"removed":      len(removed),
			"tracks":       items,
		},
	}}, nil
}

func handleRemovePendingTracksFromNavidromePlaylistTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "playlistId", "playlistName", "selection"); err != nil {
		return toolResult{}, err
	}
	selection := strings.TrimSpace(toolStringArg(args, "selection"))
	if selection == "" {
		return toolResult{}, fmt.Errorf("selection is required")
	}
	client, err := newNavidromeClientFromEnv()
	if err != nil {
		return toolResult{}, err
	}
	playlist, err := resolveNavidromePlaylist(ctx, client, toolStringArg(args, "playlistId"), toolStringArg(args, "playlistName"))
	if err != nil {
		return toolResult{}, err
	}
	removed, err := removePendingPlaylistTracksForPlaylist(playlist.Name, selection)
	if err != nil {
		return toolResult{}, err
	}
	items := make([]string, 0, len(removed))
	for _, item := range removed {
		label := strings.TrimSpace(item.TrackTitle)
		if strings.TrimSpace(item.ArtistName) != "" {
			label += " by " + strings.TrimSpace(item.ArtistName)
		}
		items = append(items, label)
	}
	return toolResult{payload: map[string]interface{}{
		"removePendingTracksFromNavidromePlaylist": map[string]interface{}{
			"playlistId":   playlist.ID,
			"playlistName": playlist.Name,
			"selection":    selection,
			"removed":      len(removed),
			"tracks":       items,
		},
	}}, nil
}

func handleResolvePlaylistTracksTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	resolved, meta, err := resolvePlaylistTracks(ctx, args)
	if err != nil {
		return toolResult{}, err
	}
	return toolResult{
		payload: map[string]interface{}{
			"resolvePlaylistTracks": map[string]interface{}{
				"playlistName": meta["playlistName"],
				"plannedAt":    meta["plannedAt"],
				"resolvedAt":   meta["resolvedAt"],
				"selection":    meta["selection"],
				"counts":       meta["counts"],
				"tracks":       resolved,
				"note":         "Create/update playlist and queue operations require explicit confirmation.",
			},
		},
	}, nil
}

func resolveNavidromePlaylist(ctx context.Context, client *navidromeClient, playlistID, playlistName string) (*navidromePlaylistDetail, error) {
	playlistID = strings.TrimSpace(playlistID)
	playlistName = strings.TrimSpace(playlistName)
	switch {
	case playlistID != "":
		playlist, err := client.GetPlaylist(ctx, playlistID)
		if err != nil {
			return nil, err
		}
		if playlist == nil {
			return nil, fmt.Errorf("playlist %q was not found in Navidrome", playlistID)
		}
		return playlist, nil
	case playlistName != "":
		ref, err := client.GetPlaylistByName(ctx, playlistName)
		if err != nil {
			return nil, err
		}
		if ref == nil {
			return nil, fmt.Errorf("playlist %q was not found in Navidrome", playlistName)
		}
		playlist, err := client.GetPlaylist(ctx, ref.ID)
		if err != nil {
			return nil, err
		}
		if playlist == nil {
			return nil, fmt.Errorf("playlist %q was not found in Navidrome", playlistName)
		}
		return playlist, nil
	default:
		return nil, fmt.Errorf("playlistId or playlistName is required")
	}
}

func selectNavidromePlaylistEntries(entries []navidromePlaylistEntry, selection string) ([]int, []navidromePlaylistEntry, error) {
	if len(entries) == 0 {
		return nil, nil, fmt.Errorf("playlist has no tracks")
	}
	selection = strings.TrimSpace(selection)
	if selection == "" {
		return nil, nil, fmt.Errorf("selection is required")
	}
	lower := strings.ToLower(selection)
	if n, ok := parseLeadingCountSelection(lower); ok {
		if n > len(entries) {
			n = len(entries)
		}
		if n <= 0 {
			return nil, nil, fmt.Errorf("selection resolved to zero tracks")
		}
		indexes := make([]int, 0, n)
		selected := make([]navidromePlaylistEntry, 0, n)
		for i := 0; i < n; i++ {
			indexes = append(indexes, i)
			selected = append(selected, entries[i])
		}
		return indexes, selected, nil
	}
	needle := normalizeSearchTerm(lower)
	indexes := make([]int, 0)
	selected := make([]navidromePlaylistEntry, 0)
	for i, entry := range entries {
		title := normalizeSearchTerm(entry.Title)
		artist := normalizeSearchTerm(entry.Artist)
		if strings.Contains(title, needle) || strings.Contains(artist, needle) {
			indexes = append(indexes, i)
			selected = append(selected, entry)
		}
	}
	if len(indexes) == 0 {
		return nil, nil, fmt.Errorf("selection did not match any playlist tracks")
	}
	return indexes, selected, nil
}

func handleQueueMissingPlaylistTracksTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	out, err := queueMissingPlaylistTracks(ctx, args)
	if err != nil {
		return toolResult{}, err
	}
	return toolResult{
		payload: map[string]interface{}{
			"queueMissingPlaylistTracks": map[string]interface{}{
				"queueFile":        out["queueFile"],
				"queued":           out["queued"],
				"selection":        out["selection"],
				"reconcileJobFile": out["reconcileJobFile"],
				"note":             "Tracks were queued for the download agent. Reconcile loop will periodically try to add newly available tracks to the target playlist.",
			},
		},
	}, nil
}

func handleCreateDiscoveredPlaylistTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	out, err := createDiscoveredPlaylist(ctx, args)
	if err != nil {
		return toolResult{}, err
	}
	return toolResult{payload: map[string]interface{}{"createDiscoveredPlaylist": out}}, nil
}

func handleMatchDiscoveredAlbumsInLidarrTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "selection", "limit"); err != nil {
		return toolResult{}, err
	}
	if strings.TrimSpace(toolStringArg(args, "selection")) == "" {
		return toolResult{}, fmt.Errorf("matchDiscoveredAlbumsInLidarr requires selection; for example: {\"selection\":\"Nevermind\"} or {\"selection\":\"first 3\"}")
	}

	client, err := newLidarrClientFromEnv()
	if err != nil {
		return toolResult{}, err
	}
	matches, meta, err := matchDiscoveredAlbumsInLidarr(ctx, client, args)
	if err != nil {
		return toolResult{}, err
	}

	return toolResult{
		payload: map[string]interface{}{
			"matchDiscoveredAlbumsInLidarr": map[string]interface{}{
				"query":          meta["query"],
				"selectionCount": meta["selectionCount"],
				"cachedAt":       meta["cachedAt"],
				"matches":        matches,
				"note":           "This is a preview only. I have not changed your library yet.",
			},
		},
	}, nil
}

func handleApplyDiscoveredAlbumsTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "selection", "dryRun", "confirm"); err != nil {
		return toolResult{}, err
	}
	if strings.TrimSpace(toolStringArg(args, "selection")) == "" {
		return toolResult{}, fmt.Errorf("applyDiscoveredAlbums requires selection; for example: {\"selection\":\"Nevermind\",\"dryRun\":true}")
	}

	client, err := newLidarrClientFromEnv()
	if err != nil {
		return toolResult{}, err
	}
	results, mode, err := applyDiscoveredAlbums(ctx, client, args)
	if err != nil {
		return toolResult{}, err
	}

	okCount := 0
	failCount := 0
	for _, item := range results {
		switch item.Status {
		case "ok", "dry_run":
			okCount++
		case "error", "not_found", "ambiguous":
			failCount++
		}
	}

	return toolResult{
		payload: map[string]interface{}{
			"applyDiscoveredAlbums": map[string]interface{}{
				"mode":      mode,
				"okCount":   okCount,
				"failCount": failCount,
				"results":   results,
				"note":      "Real changes require dryRun=false and confirm=true.",
			},
		},
	}, nil
}

func handleLidarrCleanupCandidatesTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	client, err := newLidarrClientFromEnv()
	if err != nil {
		return toolResult{}, err
	}
	candidates, summary, filters, err := buildLidarrCleanupCandidates(ctx, client, args)
	if err != nil {
		return toolResult{}, err
	}
	setLastLidarrCandidates(chatSessionIDFromContext(ctx), candidates)

	return toolResult{
		payload: map[string]interface{}{
			"lidarrCleanupCandidates": map[string]interface{}{
				"filters":    filters,
				"count":      len(candidates),
				"summary":    summary,
				"candidates": candidates,
				"note":       "Review candidates before applying actions to your library. You can reference by selection (e.g. first 3, artist name, title) or explicit albumIds.",
			},
		},
	}, nil
}

func handleApplyLidarrCleanupTool(ctx context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	client, err := newLidarrClientFromEnv()
	if err != nil {
		return toolResult{}, err
	}
	results, mode, err := applyLidarrCleanup(ctx, client, args)
	if err != nil {
		return toolResult{}, err
	}

	okCount := 0
	failCount := 0
	for _, result := range results {
		if result.Status == "ok" || result.Status == "dry_run" {
			okCount++
			continue
		}
		failCount++
	}

	return toolResult{
		payload: map[string]interface{}{
			"applyLidarrCleanup": map[string]interface{}{
				"mode":      mode,
				"okCount":   okCount,
				"failCount": failCount,
				"results":   results,
			},
		},
	}, nil
}

func handleSimilarArtistsTool(ctx context.Context, runtime toolRuntime, args map[string]interface{}) (toolResult, error) {
	seed := toolStringArg(args, "seedArtist")
	if seed == "" {
		return toolResult{}, fmt.Errorf("seedArtist is required")
	}
	if runtime.similarity != nil {
		response, err := runtime.similarity.SimilarArtists(ctx, similarity.ArtistRequest{
			SeedArtistName: seed,
			Provider:       toolStringArg(args, "provider"),
			Limit:          toolIntArg(args, "limit", 5),
		})
		if err != nil {
			return toolResult{}, err
		}
		items := make([]map[string]interface{}, 0, len(response.Results))
		for _, artist := range response.Results {
			items = append(items, map[string]interface{}{
				"id":           artist.ID,
				"name":         artist.Name,
				"rating":       artist.Rating,
				"playCount":    artist.PlayCount,
				"score":        artist.Score,
				"sourceScores": artist.SourceScores,
				"sources":      artist.Sources,
			})
		}
		return toolResult{payload: map[string]interface{}{
			"similarArtists": map[string]interface{}{
				"provider": response.Provider,
				"seed":     response.Seed,
				"results":  items,
			},
		}}, nil
	}

	artists, err := runtime.resolver.Query().SimilarArtists(ctx, seed, intPtr(toolIntArg(args, "limit", 5)))
	if err != nil {
		return toolResult{}, err
	}

	items := make([]map[string]interface{}, 0, len(artists))
	for _, artist := range artists {
		items = append(items, map[string]interface{}{
			"id":        artist.ID,
			"name":      artist.Name,
			"rating":    artist.Rating,
			"playCount": artist.PlayCount,
		})
	}
	return toolResult{payload: map[string]interface{}{"similarArtists": items}}, nil
}

func handleSimilarTracksTool(ctx context.Context, runtime toolRuntime, args map[string]interface{}) (toolResult, error) {
	if runtime.similarity == nil {
		return toolResult{}, fmt.Errorf("similarity service is unavailable")
	}
	response, err := runtime.similarity.SimilarTracks(ctx, similarity.TrackRequest{
		SeedTrackID:       toolStringArg(args, "seedTrackId"),
		SeedTrackTitle:    toolStringArg(args, "seedTrackTitle"),
		SeedArtistName:    toolStringArg(args, "seedArtistName"),
		Provider:          toolStringArg(args, "provider"),
		Limit:             toolIntArg(args, "limit", 10),
		ExcludeRecentDays: toolIntArg(args, "excludeRecentDays", 0),
		ExcludeSeedArtist: toolBoolArg(args, "excludeSeedArtist", false),
	})
	if err != nil {
		return toolResult{}, err
	}
	items := make([]map[string]interface{}, 0, len(response.Results))
	for _, track := range response.Results {
		var lastPlayed interface{}
		if track.LastPlayed != nil {
			lastPlayed = track.LastPlayed.Format(time.RFC3339)
		}
		items = append(items, map[string]interface{}{
			"id":           track.ID,
			"albumId":      track.AlbumID,
			"title":        track.Title,
			"artistName":   track.ArtistName,
			"rating":       track.Rating,
			"playCount":    track.PlayCount,
			"lastPlayed":   lastPlayed,
			"score":        track.Score,
			"sourceScores": track.SourceScores,
			"sources":      track.Sources,
		})
	}
	return toolResult{payload: map[string]interface{}{
		"similarTracks": map[string]interface{}{
			"provider": response.Provider,
			"seed":     response.Seed,
			"results":  items,
		},
	}}, nil
}

func handleSimilarAlbumsTool(ctx context.Context, runtime toolRuntime, args map[string]interface{}) (toolResult, error) {
	seed := toolStringArg(args, "seedAlbum")
	if seed == "" {
		return toolResult{}, fmt.Errorf("seedAlbum is required")
	}

	albums, err := runtime.resolver.Query().SimilarAlbums(ctx, seed, intPtr(toolIntArg(args, "limit", 5)))
	if err != nil {
		return toolResult{}, err
	}

	items := make([]map[string]interface{}, 0, len(albums))
	for _, album := range albums {
		items = append(items, map[string]interface{}{
			"id":         album.ID,
			"name":       album.Name,
			"artistName": album.ArtistName,
			"rating":     album.Rating,
			"playCount":  album.PlayCount,
			"lastPlayed": album.LastPlayed,
			"year":       album.Year,
			"genre":      album.Genre,
		})
	}
	return toolResult{payload: map[string]interface{}{"similarAlbums": items}}, nil
}

func handleRemoveArtistFromLibraryTool(_ context.Context, _ toolRuntime, args map[string]interface{}) (toolResult, error) {
	if err := validateToolArgs(args, "artistName", "confirm"); err != nil {
		return toolResult{}, err
	}

	artistName := toolStringArg(args, "artistName")
	if artistName == "" {
		return toolResult{}, fmt.Errorf("artistName is required")
	}
	confirm := false
	if val := toolOptBoolArg(args, "confirm"); val != nil {
		confirm = *val
	}
	if !confirm {
		return toolResult{}, fmt.Errorf("confirm=true is required to remove an artist from your library")
	}
	return toolResult{}, fmt.Errorf("artist removal requires the server-managed approval flow")
}

func buildAlbumQuery(args map[string]interface{}) (int, map[string]interface{}, error) {
	if err := validateToolArgs(args, "limit", "rating", "ratingBelow", "unplayed", "genre", "year", "minYear", "maxYear", "artistName", "artistNames", "queryText", "sortBy", "notPlayedSince"); err != nil {
		return 0, nil, err
	}
	limit := toolIntArg(args, "limit", 10)
	filters := make(map[string]interface{})

	if rating := toolOptIntArg(args, "rating"); rating != nil {
		filters["rating"] = *rating
	}
	if ratingBelow := toolOptIntArg(args, "ratingBelow"); ratingBelow != nil {
		filters["ratingBelow"] = *ratingBelow
	}
	if unplayed := toolOptBoolArg(args, "unplayed"); unplayed != nil {
		filters["unplayed"] = *unplayed
	}
	if genre := toolOptStringArg(args, "genre"); genre != nil {
		filters["genre"] = *genre
	}
	if year := toolOptIntArg(args, "year"); year != nil {
		filters["year"] = *year
	}
	if minYear := normalizeOptionalYear(toolOptIntArg(args, "minYear")); minYear != nil {
		filters["minYear"] = *minYear
	}
	if maxYear := normalizeOptionalYear(toolOptIntArg(args, "maxYear")); maxYear != nil {
		filters["maxYear"] = *maxYear
	}
	artistNames := toolOptStringListArg(args, "artistNames")
	if artistName := toolOptStringArg(args, "artistName"); artistName != nil {
		artistNames = append([]string{*artistName}, artistNames...)
	}
	if queryText := toolOptStringArg(args, "queryText"); queryText != nil && strings.TrimSpace(*queryText) != "" {
		trimmed := strings.TrimSpace(*queryText)
		if titlePart, artistPart, ok := splitLookupQueryArtist(trimmed); ok {
			trimmed = titlePart
			artistNames = append(artistNames, artistPart)
		}
		filters["queryText"] = trimmed
	}
	artistNames = uniqueNonEmptyStrings(artistNames)
	switch len(artistNames) {
	case 1:
		filters["artistName"] = artistNames[0]
	default:
		filters["artistNames"] = artistNames
	}
	if sortBy := strings.ToLower(strings.TrimSpace(toolStringArg(args, "sortBy"))); sortBy != "" {
		filters["sortBy"] = sortBy
	}
	if notPlayedSince := toolOptStringArg(args, "notPlayedSince"); notPlayedSince != nil && strings.TrimSpace(*notPlayedSince) != "" {
		cutoff, err := parseTimeArg(*notPlayedSince)
		if err != nil {
			return 0, nil, err
		}
		filters["notPlayedSince"] = cutoff
	}

	return limit, filters, nil
}

func splitLookupQueryArtist(raw string) (string, string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", false
	}
	lower := strings.ToLower(trimmed)
	idx := strings.LastIndex(lower, " by ")
	if idx <= 0 {
		return "", "", false
	}
	titlePart := strings.TrimSpace(trimmed[:idx])
	artistPart := strings.TrimSpace(trimmed[idx+4:])
	if titlePart == "" || artistPart == "" {
		return "", "", false
	}
	return titlePart, artistPart, true
}

func normalizedWindowArg(args map[string]interface{}) string {
	return canonicalWindowName(toolStringArg(args, "window"))
}

func resolveNamedWindow(window string) (time.Time, time.Time, error) {
	start, end, ok := resolveWindow(window)
	if !ok {
		return time.Time{}, time.Time{}, fmt.Errorf("unsupported window: %s", window)
	}
	return start, end, nil
}

func resolveSummaryWindow(args map[string]interface{}) (time.Time, time.Time, error) {
	window := normalizedWindowArg(args)
	startRaw := strings.TrimSpace(toolStringArg(args, "playedSince"))
	endRaw := strings.TrimSpace(toolStringArg(args, "playedUntil"))
	if window != "" {
		if startRaw != "" || endRaw != "" {
			return time.Time{}, time.Time{}, fmt.Errorf("window cannot be combined with playedSince/playedUntil")
		}
		return resolveNamedWindow(window)
	}
	if startRaw == "" && endRaw == "" {
		return resolveNamedWindow("last_month")
	}
	if startRaw == "" || endRaw == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("playedSince and playedUntil must both be provided when window is omitted")
	}
	return parseTimeRange(startRaw, endRaw)
}

func resolveSemanticSearchWindow(args map[string]interface{}) (*time.Time, *time.Time, error) {
	if window := normalizedWindowArg(args); window != "" {
		start, end, err := resolveNamedWindow(window)
		if err != nil {
			return nil, nil, err
		}
		return &start, &end, nil
	}

	startRaw := toolStringArg(args, "playedSince")
	endRaw := toolStringArg(args, "playedUntil")
	if startRaw == "" || endRaw == "" {
		return nil, nil, nil
	}

	start, end, err := parseTimeRange(startRaw, endRaw)
	if err != nil {
		return nil, nil, err
	}
	return &start, &end, nil
}

func parseTimeRange(startRaw, endRaw string) (time.Time, time.Time, error) {
	var start time.Time
	var end time.Time
	var err error

	if startRaw != "" {
		start, err = parseTimeArg(startRaw)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if endRaw != "" {
		end, err = parseTimeArg(endRaw)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if !start.IsZero() && !end.IsZero() && !end.After(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("playedUntil must be later than playedSince")
	}
	return start, end, nil
}

func clampInt(value, max int) int {
	if value > max {
		return max
	}
	return value
}

func buildMetadataFallbackTracks(ctx context.Context, runtime toolRuntime, start, end time.Time, trackLimit int) ([]map[string]interface{}, bool) {
	sinceVal := start.Format(time.RFC3339)
	untilVal := end.Format(time.RFC3339)
	mostPlayed := true
	onlyPlayed := true
	metaTracks, err := runtime.resolver.Query().Tracks(
		ctx,
		intPtr(trackLimit),
		&mostPlayed,
		&sinceVal,
		&untilVal,
		&onlyPlayed,
	)
	if err != nil || len(metaTracks) == 0 {
		return nil, false
	}

	items := make([]map[string]interface{}, 0, len(metaTracks))
	for _, track := range metaTracks {
		items = append(items, map[string]interface{}{
			"id":         track.ID,
			"title":      track.Title,
			"artistName": track.ArtistName,
			"playCount":  track.PlayCount,
			"lastPlayed": track.LastPlayed,
		})
	}
	return items, true
}

func countDistinctArtists(tracks []map[string]interface{}) int {
	artists := make(map[string]struct{})
	for _, track := range tracks {
		name, _ := track["artistName"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		artists[name] = struct{}{}
	}
	return len(artists)
}

func executeGraphQL(ctx context.Context, resolver *graph.Resolver, query string) (string, error) {
	q := strings.TrimSpace(query)
	res := make(map[string]interface{})

	switch {
	case strings.Contains(q, "libraryStats"):
		stats, err := resolver.Query().LibraryStats(ctx)
		if err != nil {
			return fmt.Sprintf(`{"errors": [{"message": "%s"}]}`, err), nil
		}
		res["libraryStats"] = map[string]int{
			"totalAlbums":    stats.TotalAlbums,
			"totalArtists":   stats.TotalArtists,
			"unplayedAlbums": stats.UnplayedAlbums,
			"ratedAlbums":    stats.RatedAlbums,
		}
	case strings.Contains(q, "similarArtists"):
		seed := parseStringArg(q, "seedArtist")
		limit := parseIntArg(q, "limit", 5)
		if seed == "" {
			return `{"errors": [{"message": "seedArtist required"}]}`, nil
		}
		artists, err := resolver.Query().SimilarArtists(ctx, seed, &limit)
		if err != nil {
			return fmt.Sprintf(`{"errors": [{"message": "%s"}]}`, err), nil
		}
		var ares []map[string]interface{}
		for _, a := range artists {
			ares = append(ares, map[string]interface{}{
				"id":        a.ID,
				"name":      a.Name,
				"rating":    a.Rating,
				"playCount": a.PlayCount,
			})
		}
		res["similarArtists"] = ares
	case strings.Contains(q, "similarAlbums"):
		seed := parseStringArg(q, "seedAlbum")
		limit := parseIntArg(q, "limit", 5)
		if seed == "" {
			return `{"errors": [{"message": "seedAlbum required"}]}`, nil
		}
		albums, err := resolver.Query().SimilarAlbums(ctx, seed, &limit)
		if err != nil {
			return fmt.Sprintf(`{"errors": [{"message": "%s"}]}`, err), nil
		}
		var ares []map[string]interface{}
		for _, a := range albums {
			ares = append(ares, map[string]interface{}{
				"id":         a.ID,
				"name":       a.Name,
				"artistName": a.ArtistName,
				"rating":     a.Rating,
				"playCount":  a.PlayCount,
				"lastPlayed": a.LastPlayed,
				"year":       a.Year,
				"genre":      a.Genre,
			})
		}
		res["similarAlbums"] = ares
	case strings.Contains(q, "artists"):
		artists, err := resolver.Query().Artists(ctx, intPtr(10), nil, nil)
		if err != nil {
			return fmt.Sprintf(`{"errors": [{"message": "%s"}]}`, err), nil
		}
		var result []map[string]interface{}
		for _, a := range artists {
			result = append(result, map[string]interface{}{
				"id":        a.ID,
				"name":      a.Name,
				"rating":    a.Rating,
				"playCount": a.PlayCount,
			})
		}
		res["artists"] = result
	case strings.Contains(q, "albums"):
		albums, err := resolver.Query().Albums(ctx, intPtr(10), nil, nil, nil, nil, nil, nil, nil)
		if err != nil {
			return fmt.Sprintf(`{"errors": [{"message": "%s"}]}`, err), nil
		}
		var result []map[string]interface{}
		for _, a := range albums {
			result = append(result, map[string]interface{}{
				"id":         a.ID,
				"name":       a.Name,
				"artistName": a.ArtistName,
				"rating":     a.Rating,
				"playCount":  a.PlayCount,
				"lastPlayed": a.LastPlayed,
				"year":       a.Year,
				"genre":      a.Genre,
			})
		}
		res["albums"] = result
	case strings.Contains(q, "tracks"):
		tracks, err := resolver.Query().Tracks(ctx, intPtr(10), nil, nil, nil, nil)
		if err != nil {
			return fmt.Sprintf(`{"errors": [{"message": "%s"}]}`, err), nil
		}
		var result []map[string]interface{}
		for _, t := range tracks {
			result = append(result, map[string]interface{}{
				"id":         t.ID,
				"title":      t.Title,
				"artistName": t.ArtistName,
				"playCount":  t.PlayCount,
				"lastPlayed": t.LastPlayed,
			})
		}
		res["tracks"] = result
	default:
		return `{"errors": [{"message": "unsupported query"}]}`, nil
	}

	out, err := json.Marshal(map[string]interface{}{"data": res})
	if err != nil {
		return `{"errors": [{"message": "marshal error"}]}`, nil
	}
	return string(out), nil
}

func parseStringArg(q, arg string) string {
	re := regexp.MustCompile(arg + `\s*:\s*"([^"]+)"`)
	m := re.FindStringSubmatch(q)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

func parseIntArg(q, arg string, defaultVal int) int {
	re := regexp.MustCompile(arg + `\s*:\s*(\d+)`)
	m := re.FindStringSubmatch(q)
	if len(m) > 1 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			return v
		}
	}
	return defaultVal
}

func intPtr(i int) *int {
	return &i
}

func toolIntArg(args map[string]interface{}, key string, defaultVal int) int {
	v, ok := args[key]
	if !ok || v == nil {
		return defaultVal
	}
	switch val := v.(type) {
	case float64:
		if int(val) <= 0 {
			return defaultVal
		}
		return int(val)
	case int:
		if val <= 0 {
			return defaultVal
		}
		return val
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(val))
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultVal
}

func validateToolArgs(args map[string]interface{}, allowedKeys ...string) error {
	return validateMapKeys(args, allowedKeys...)
}

func validateMapKeys(args map[string]interface{}, allowedKeys ...string) error {
	if len(args) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(allowedKeys))
	for _, key := range allowedKeys {
		allowed[key] = struct{}{}
	}
	unknown := make([]string, 0)
	for key := range args {
		if _, ok := allowed[key]; ok {
			continue
		}
		unknown = append(unknown, key)
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	sort.Strings(allowedKeys)
	return fmt.Errorf("unsupported args for tool: %s (allowed: %s)", strings.Join(unknown, ", "), strings.Join(allowedKeys, ", "))
}

func resolveInactiveSinceFilter(rawFilter map[string]interface{}) (*string, error) {
	for _, key := range []string{"inactiveSince", "notPlayedSince"} {
		value := mapOptString(rawFilter, key)
		if value == nil {
			continue
		}
		normalized, err := normalizeInactiveSince(*value)
		if err != nil {
			return nil, err
		}
		return &normalized, nil
	}
	return nil, nil
}

func normalizeInactiveSince(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("inactiveSince cannot be empty")
	}

	lower := strings.ToLower(trimmed)
	switch lower {
	case "years", "in years", "for years", "a long time", "long time", "ages":
		return time.Now().UTC().AddDate(-2, 0, 0).Format("2006-01-02"), nil
	case "months", "in months", "for months":
		return time.Now().UTC().AddDate(0, -6, 0).Format("2006-01-02"), nil
	}

	if start, _, ok := resolveWindow(lower); ok {
		return start.Format(time.RFC3339), nil
	}

	parsed, err := parseTimeArg(trimmed)
	if err != nil {
		return "", err
	}
	return parsed.Format(time.RFC3339), nil
}

func toolOptIntArg(args map[string]interface{}, key string) *int {
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	out := toolIntArg(args, key, 0)
	return &out
}

func toolOptBoolArg(args map[string]interface{}, key string) *bool {
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}

	var out bool
	switch val := v.(type) {
	case bool:
		out = val
	case string:
		lower := strings.ToLower(strings.TrimSpace(val))
		out = lower == "true" || lower == "1" || lower == "yes"
	default:
		return nil
	}
	return &out
}

func toolBoolArg(args map[string]interface{}, key string, defaultVal bool) bool {
	if value := toolOptBoolArg(args, key); value != nil {
		return *value
	}
	return defaultVal
}

func toolStringArg(args map[string]interface{}, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return fmt.Sprintf("%v", v)
}

func toolOptStringArg(args map[string]interface{}, key string) *string {
	v := toolStringArg(args, key)
	if v == "" {
		return nil
	}
	return &v
}

func toolOptStringListArg(args map[string]interface{}, key string) []string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}

	var values []string
	switch typed := raw.(type) {
	case []string:
		values = append(values, typed...)
	case []interface{}:
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprintf("%v", item))
			if text != "" {
				values = append(values, text)
			}
		}
	case string:
		for _, item := range strings.FieldsFunc(typed, func(r rune) bool {
			return r == ',' || r == '\n'
		}) {
			text := strings.TrimSpace(item)
			if text != "" {
				values = append(values, text)
			}
		}
	}

	return uniqueNonEmptyStrings(values)
}

func uniqueNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func toolOptMapArg(args map[string]interface{}, key string) (map[string]interface{}, bool) {
	v, ok := args[key]
	if !ok || v == nil {
		return nil, false
	}
	m, ok := v.(map[string]interface{})
	return m, ok
}

func mapOptString(args map[string]interface{}, key string) *string {
	if args == nil {
		return nil
	}
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	if s, ok := v.(string); ok {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		return &s
	}
	s := strings.TrimSpace(fmt.Sprintf("%v", v))
	if s == "" {
		return nil
	}
	return &s
}

func mapOptStringList(args map[string]interface{}, key string) []string {
	if args == nil {
		return nil
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}

	var values []string
	switch typed := raw.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed != "" {
			values = append(values, trimmed)
		}
	case []string:
		values = append(values, typed...)
	case []interface{}:
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprintf("%v", item))
			if text != "" {
				values = append(values, text)
			}
		}
	default:
		text := strings.TrimSpace(fmt.Sprintf("%v", raw))
		if text != "" {
			values = append(values, text)
		}
	}
	return uniqueNonEmptyStrings(values)
}

func mapOptInt(args map[string]interface{}, key string) *int {
	if args == nil {
		return nil
	}
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	switch val := v.(type) {
	case int:
		return &val
	case float64:
		out := int(val)
		return &out
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil {
			return nil
		}
		return &n
	default:
		return nil
	}
}

func mapOptBool(args map[string]interface{}, key string) *bool {
	if args == nil {
		return nil
	}
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	switch val := v.(type) {
	case bool:
		return &val
	case string:
		lower := strings.ToLower(strings.TrimSpace(val))
		out := lower == "true" || lower == "1" || lower == "yes"
		return &out
	default:
		return nil
	}
}

func resolveWindow(window string) (time.Time, time.Time, bool) {
	now := time.Now().UTC()
	switch canonicalWindowName(window) {
	case "last_day", "day", "1d", "today":
		return now.Add(-24 * time.Hour), now, true
	case "this_week":
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		offset := (int(start.Weekday()) + 6) % 7
		return start.AddDate(0, 0, -offset), now, true
	case "last_week", "week", "7d":
		return now.AddDate(0, 0, -7), now, true
	case "this_month":
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC), now, true
	case "last_month", "month", "30d":
		return now.AddDate(0, -1, 0), now, true
	case "this_year":
		return time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, time.UTC), now, true
	case "last_year", "year", "365d":
		return now.AddDate(-1, 0, 0), now, true
	default:
		return time.Time{}, time.Time{}, false
	}
}

func canonicalWindowName(raw string) string {
	window := strings.ToLower(strings.TrimSpace(raw))
	window = strings.ReplaceAll(window, " ", "_")
	switch window {
	case "recent", "recently", "lately":
		return "last_month"
	case "past_week":
		return "last_week"
	case "past_month":
		return "last_month"
	case "past_year":
		return "last_year"
	case "current_week":
		return "this_week"
	case "current_month":
		return "this_month"
	case "current_year":
		return "this_year"
	default:
		return window
	}
}

func parseTimeArg(raw string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid time value %q: expected RFC3339 or YYYY-MM-DD", raw)
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func fetchSingleEmbedding(ctx context.Context, endpoint, text string) (pgvector.Vector, error) {
	body, err := json.Marshal(map[string]interface{}{"input": []string{text}})
	if err != nil {
		return pgvector.Vector{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return pgvector.Vector{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return pgvector.Vector{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return pgvector.Vector{}, fmt.Errorf("embeddings endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var parsed embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return pgvector.Vector{}, err
	}
	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return pgvector.Vector{}, fmt.Errorf("empty embedding response")
	}
	return pgvector.NewVector(parsed.Data[0].Embedding), nil
}

func semanticTrackSearchUnavailableResult(queryText string, args map[string]interface{}, warning string) toolResult {
	result := map[string]interface{}{
		"queryText": queryText,
		"matches":   []map[string]interface{}{},
		"warning":   warning,
	}
	start, end, err := resolveSemanticSearchWindow(args)
	if err == nil {
		if start != nil && end != nil {
			result["windowStart"] = start.Format(time.RFC3339)
			result["windowEnd"] = end.Format(time.RFC3339)
		}
	}
	return toolResult{payload: map[string]interface{}{"semanticTrackSearch": result}}
}

func semanticAlbumSearchUnavailableResult(queryText string, args map[string]interface{}, warning string) toolResult {
	result := map[string]interface{}{
		"queryText": queryText,
		"matches":   []map[string]interface{}{},
		"warning":   warning,
	}
	if artistName := strings.TrimSpace(toolStringArg(args, "artistName")); artistName != "" {
		result["artistName"] = artistName
	}
	if genre := strings.TrimSpace(toolStringArg(args, "genre")); genre != "" {
		result["genre"] = genre
	}
	if minYear := toolOptIntArg(args, "minYear"); minYear != nil {
		result["minYear"] = *minYear
	}
	if maxYear := toolOptIntArg(args, "maxYear"); maxYear != nil {
		result["maxYear"] = *maxYear
	}
	return toolResult{payload: map[string]interface{}{"semanticAlbumSearch": result}}
}

func buildRecentListeningSummaryCacheKey(start, end time.Time, trackLimit, artistLimit int) string {
	return fmt.Sprintf(
		"%d:%d:%d:%d",
		start.UTC().Unix(),
		end.UTC().Unix(),
		trackLimit,
		artistLimit,
	)
}

func getRecentListeningSummaryCache(key string) (string, bool) {
	now := time.Now()
	recentListeningSummaryCache.mu.RLock()
	entry, ok := recentListeningSummaryCache.entries[key]
	recentListeningSummaryCache.mu.RUnlock()
	if !ok {
		return "", false
	}
	if now.After(entry.expires) {
		recentListeningSummaryCache.mu.Lock()
		delete(recentListeningSummaryCache.entries, key)
		recentListeningSummaryCache.mu.Unlock()
		return "", false
	}
	return entry.value, true
}

func setRecentListeningSummaryCache(key, value string) {
	recentListeningSummaryCache.mu.Lock()
	recentListeningSummaryCache.entries[key] = cachedToolResult{
		value:   value,
		expires: time.Now().Add(recentListeningSummaryTTL()),
	}
	recentListeningSummaryCache.mu.Unlock()
}
