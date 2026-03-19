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

	"groovarr/internal/agent"
	"groovarr/internal/discovery"
	"groovarr/internal/lidarr"

	"github.com/rs/zerolog/log"
)

type discoveredAlbumCandidate = discovery.Candidate

type lidarrAlbumMatch struct {
	Rank            int     `json:"rank"`
	ArtistName      string  `json:"artistName"`
	AlbumTitle      string  `json:"albumTitle"`
	Status          string  `json:"status"`
	AlbumID         int     `json:"albumId,omitempty"`
	ArtistID        int     `json:"artistId,omitempty"`
	MatchedTitle    string  `json:"matchedTitle,omitempty"`
	MatchedArtist   string  `json:"matchedArtist,omitempty"`
	Monitored       bool    `json:"monitored,omitempty"`
	Rating          float64 `json:"rating,omitempty"`
	ReleaseDate     string  `json:"releaseDate,omitempty"`
	SuggestedAction string  `json:"suggestedAction,omitempty"`
	Detail          string  `json:"detail,omitempty"`
}

type applyDiscoveredAlbumItem struct {
	Rank       int    `json:"rank"`
	ArtistName string `json:"artistName"`
	AlbumTitle string `json:"albumTitle"`
	Status     string `json:"status"`
	AlbumID    int    `json:"albumId,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

type lidarrArtistSearchResult = lidarr.ArtistSearchResult
type lidarrArtist = lidarr.Artist
type lidarrRootFolder = lidarr.RootFolder
type lidarrAlbumSearchResult = lidarr.AlbumSearchResult

const (
	discoveryProviderGroq = "groq"
	discoveryProviderHF   = "hf"
)

type discoveryModelCandidate struct {
	provider string
	model    string
}

var lastAlbumDiscovery = discovery.NewStore()
var discoverAlbumsRequestRunner = discoverAlbumsWithRequest

type discoveredAlbumApplyOptions struct {
	dryRun  bool
	confirm bool
}

func setLastDiscoveredAlbums(sessionID, query string, candidates []discoveredAlbumCandidate) {
	lastAlbumDiscovery.Set(normalizeChatSessionID(sessionID), query, candidates)
}

func getLastDiscoveredAlbums(sessionID string) ([]discoveredAlbumCandidate, time.Time, string) {
	candidates, updatedAt, query, ok := lastAlbumDiscovery.Get(normalizeChatSessionID(sessionID))
	if !ok {
		return nil, time.Time{}, ""
	}
	return candidates, updatedAt, query
}

func discoverAlbums(ctx context.Context, args map[string]interface{}) ([]discoveredAlbumCandidate, map[string]interface{}, error) {
	request, err := discovery.BuildRequest(toolStringArg(args, "query"), toolIntArg(args, "limit", 5))
	if err != nil {
		return nil, nil, err
	}
	return discoverAlbumsRequestRunner(ctx, request)
}

func discoverAlbumsWithRequest(ctx context.Context, request discovery.Request) ([]discoveredAlbumCandidate, map[string]interface{}, error) {
	groqKey := strings.TrimSpace(os.Getenv("GROQ_API_KEY"))
	huggingFaceKey := firstEnv("HUGGINGFACE_API_KEY", "HF_API_KEY", "HF_TOKEN")
	if groqKey == "" && strings.TrimSpace(huggingFaceKey) == "" {
		return nil, nil, fmt.Errorf("album discovery is not configured (set GROQ_API_KEY or HUGGINGFACE_API_KEY)")
	}

	systemPrompt, userPrompt := discovery.BuildPrompts(request)
	candidates, lastErr := runDiscoveryPass(ctx, groqKey, huggingFaceKey, request, systemPrompt, userPrompt)
	if len(candidates) == 0 && request.ArtistHint != "" {
		focusedSystemPrompt, focusedUserPrompt := discovery.BuildFocusedPrompts(request)
		candidates, lastErr = runDiscoveryPass(ctx, groqKey, huggingFaceKey, request, focusedSystemPrompt, focusedUserPrompt)
	}
	if len(candidates) == 0 {
		if lastErr != nil {
			return nil, nil, fmt.Errorf("album discovery failed: %w", lastErr)
		}
		return nil, nil, fmt.Errorf("album discovery returned no usable candidates")
	}

	setLastDiscoveredAlbums(chatSessionIDFromContext(ctx), request.Query, candidates)
	return candidates, map[string]interface{}{
		"query":        request.Query,
		"limit":        request.Limit,
		"artistFocus":  request.ArtistHint,
		"requestCount": request.RequestCount,
		"seed":         request.Seed,
	}, nil
}

func runDiscoveryPass(ctx context.Context, groqKey, huggingFaceKey string, request discovery.Request, systemPrompt, userPrompt string) ([]discoveredAlbumCandidate, error) {
	var lastErr error
	for _, candidate := range discoveryModelFallbacks() {
		raw, err := callDiscoveryJSON(ctx, candidate.provider, candidate.model, groqKey, huggingFaceKey, systemPrompt, userPrompt, 900)
		if err != nil {
			lastErr = err
			log.Warn().
				Err(err).
				Str("provider", candidate.provider).
				Str("model", candidate.model).
				Str("query", request.Query).
				Msg("Album discovery request failed")
			continue
		}

		parsed, err := discovery.ParseResponse(raw)
		if err != nil {
			lastErr = err
			log.Warn().
				Err(err).
				Str("provider", candidate.provider).
				Str("model", candidate.model).
				Str("query", request.Query).
				Msg("Album discovery response parse failed")
			continue
		}

		candidates := discovery.Rank(request, parsed)
		if len(candidates) == 0 {
			lastErr = fmt.Errorf("%s model %s returned no usable album candidates", candidate.provider, candidate.model)
			log.Warn().
				Str("provider", candidate.provider).
				Str("model", candidate.model).
				Str("query", request.Query).
				Msg("Album discovery produced no usable candidates")
			continue
		}
		return candidates, nil
	}

	return nil, lastErr
}

func discoveryModelFallbacks() []discoveryModelCandidate {
	seen := make(map[string]struct{})
	out := make([]discoveryModelCandidate, 0, 6)
	add := func(raw string) {
		provider, model := resolveDiscoveryModelProvider(raw)
		if model == "" {
			return
		}
		key := provider + ":" + model
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, discoveryModelCandidate{provider: provider, model: model})
	}

	add(os.Getenv("DISCOVERY_MODEL"))
	add(os.Getenv("HF_DISCOVERY_MODEL"))
	add(os.Getenv("GROQ_DISCOVERY_MODEL"))
	add(os.Getenv("DEFAULT_CHAT_MODEL"))
	add(os.Getenv("GROQ_MODEL"))
	if len(out) == 0 {
		add(agent.DefaultGroqModel)
	}
	return out
}

func resolveDiscoveryModelProvider(model string) (string, string) {
	trimmed := strings.TrimSpace(model)
	if strings.HasPrefix(trimmed, "hf:") {
		return discoveryProviderHF, strings.TrimSpace(strings.TrimPrefix(trimmed, "hf:"))
	}
	if trimmed == "" {
		return discoveryProviderGroq, ""
	}
	return discoveryProviderGroq, trimmed
}

func matchDiscoveredAlbumsInLidarr(ctx context.Context, client *lidarrClient, args map[string]interface{}) ([]lidarrAlbumMatch, map[string]interface{}, error) {
	candidates, updatedAt, sourceQuery, err := resolveDiscoveredAlbumSelection(ctx, args)
	if err != nil {
		return nil, nil, err
	}
	return matchDiscoveredAlbumCandidatesInLidarr(ctx, client, candidates, updatedAt, sourceQuery)
}

func matchDiscoveredAlbumCandidatesInLidarr(ctx context.Context, client *lidarrClient, candidates []discoveredAlbumCandidate, updatedAt time.Time, sourceQuery string) ([]lidarrAlbumMatch, map[string]interface{}, error) {
	results := make([]lidarrAlbumMatch, 0, len(candidates))
	for _, candidate := range candidates {
		results = append(results, matchDiscoveredAlbumCandidateInLidarr(ctx, client, candidate))
	}

	meta := map[string]interface{}{
		"query":          sourceQuery,
		"selectionCount": len(candidates),
		"cachedAt":       updatedAt.Format(time.RFC3339),
	}
	return results, meta, nil
}

func applyDiscoveredAlbums(ctx context.Context, client *lidarrClient, args map[string]interface{}) ([]applyDiscoveredAlbumItem, string, error) {
	candidates, _, _, err := resolveDiscoveredAlbumSelection(ctx, args)
	if err != nil {
		return nil, "", err
	}
	return applyDiscoveredAlbumCandidates(ctx, client, candidates, args)
}

func applyDiscoveredAlbumCandidates(ctx context.Context, client *lidarrClient, candidates []discoveredAlbumCandidate, args map[string]interface{}) ([]applyDiscoveredAlbumItem, string, error) {
	options, err := parseDiscoveredAlbumApplyOptions(args)
	if err != nil {
		return nil, "", err
	}

	mode := "dry_run"
	if !options.dryRun {
		mode = "applied"
	}

	items := make([]applyDiscoveredAlbumItem, 0, len(candidates))
	for _, candidate := range candidates {
		items = append(items, applyDiscoveredAlbumCandidate(ctx, client, candidate, options))
	}

	return items, mode, nil
}

func parseDiscoveredAlbumApplyOptions(args map[string]interface{}) (discoveredAlbumApplyOptions, error) {
	options := discoveredAlbumApplyOptions{dryRun: true}
	if val := toolOptBoolArg(args, "dryRun"); val != nil {
		options.dryRun = *val
	}
	if val := toolOptBoolArg(args, "confirm"); val != nil {
		options.confirm = *val
	}
	if !options.dryRun && !options.confirm {
		return discoveredAlbumApplyOptions{}, fmt.Errorf("confirm=true is required when dryRun=false")
	}
	return options, nil
}

func matchDiscoveredAlbumCandidateInLidarr(ctx context.Context, client *lidarrClient, candidate discoveredAlbumCandidate) lidarrAlbumMatch {
	match := newLidarrAlbumMatch(candidate)

	bestArtist, err := lookupBestArtistForDiscoveryCandidate(ctx, client, candidate)
	if err != nil {
		return withLookupError(match, err)
	}
	if bestArtist == nil {
		return withArtistNotFound(match)
	}

	match.ArtistID = bestArtist.ID
	bestAlbum, ambiguous, err := lookupBestAlbumForDiscoveryCandidate(ctx, client, candidate, bestArtist.ID)
	if err != nil {
		return withLookupError(match, err)
	}
	if bestAlbum == nil {
		return withAlbumNotFound(match)
	}

	return populateMatchedAlbum(match, bestAlbum, ambiguous)
}

func newLidarrAlbumMatch(candidate discoveredAlbumCandidate) lidarrAlbumMatch {
	return lidarrAlbumMatch{
		Rank:            candidate.Rank,
		ArtistName:      candidate.ArtistName,
		AlbumTitle:      candidate.AlbumTitle,
		Status:          "no_match",
		SuggestedAction: "skip",
	}
}

func lookupBestArtistForDiscoveryCandidate(ctx context.Context, client *lidarrClient, candidate discoveredAlbumCandidate) (*lidarrArtistSearchResult, error) {
	artistResults, err := client.SearchArtist(ctx, candidate.ArtistName)
	if err != nil {
		return nil, err
	}
	return lidarr.BestArtistResult(artistResults, candidate.ArtistName), nil
}

func lookupBestAlbumForDiscoveryCandidate(ctx context.Context, client *lidarrClient, candidate discoveredAlbumCandidate, artistID int) (*lidarrAlbumSearchResult, bool, error) {
	albumResults, err := client.SearchAlbumsByArtist(ctx, artistID, candidate.AlbumTitle)
	if err != nil {
		return nil, false, err
	}
	if len(albumResults) == 0 {
		albumResults, err = client.SearchAlbumLookup(ctx, candidate.ArtistName, candidate.AlbumTitle)
		if err != nil {
			return nil, false, err
		}
	}
	bestAlbum, ambiguous := lidarr.BestAlbumResult(albumResults)
	return bestAlbum, ambiguous, nil
}

func withLookupError(match lidarrAlbumMatch, err error) lidarrAlbumMatch {
	match.Status = "lookup_error"
	match.Detail = err.Error()
	return match
}

func withArtistNotFound(match lidarrAlbumMatch) lidarrAlbumMatch {
	match.Status = "artist_not_found"
	match.Detail = "artist lookup returned no strong match"
	match.SuggestedAction = "manual_review"
	return match
}

func withAlbumNotFound(match lidarrAlbumMatch) lidarrAlbumMatch {
	match.Status = "album_not_found"
	match.Detail = "album lookup returned no strong match"
	match.SuggestedAction = "manual_review"
	return match
}

func populateMatchedAlbum(match lidarrAlbumMatch, album *lidarrAlbumSearchResult, ambiguous bool) lidarrAlbumMatch {
	if ambiguous {
		match.Status = "ambiguous"
		match.Detail = "multiple close library matches"
		match.SuggestedAction = "manual_review"
	} else if album.Monitored {
		match.Status = "already_monitored"
		match.SuggestedAction = "search_existing"
	} else {
		match.Status = "can_monitor"
		match.SuggestedAction = "monitor_and_search"
	}

	match.AlbumID = album.ID
	match.ArtistID = album.ArtistID
	match.MatchedTitle = strings.TrimSpace(album.Title)
	match.MatchedArtist = strings.TrimSpace(album.ArtistName)
	match.Monitored = album.Monitored
	match.Rating = album.Ratings.Value
	match.ReleaseDate = strings.TrimSpace(album.ReleaseDate)
	return match
}

func applyDiscoveredAlbumCandidate(ctx context.Context, client *lidarrClient, candidate discoveredAlbumCandidate, options discoveredAlbumApplyOptions) applyDiscoveredAlbumItem {
	item := newApplyDiscoveredAlbumItem(candidate)

	artist, artistAdded, err := client.EnsureArtistPresent(ctx, candidate.ArtistName, options.dryRun)
	if err != nil {
		return withApplyError(item, err)
	}

	album, ambiguous, err := client.FindAlbumForArtist(ctx, artist.ID, candidate.ArtistName, candidate.AlbumTitle)
	if err != nil {
		return withApplyError(item, err)
	}
	if album == nil {
		return withApplyNotFound(item)
	}
	if ambiguous {
		return withApplyAmbiguous(item)
	}

	item.AlbumID = album.ID
	if options.dryRun {
		return withApplyDryRun(item, album.Monitored, artistAdded)
	}

	if err := client.MonitorAlbumByID(ctx, album); err != nil {
		return withApplyError(item, err)
	}
	if err := client.AlbumSearch(ctx, album.ID); err != nil {
		item.Status = "partial"
		item.Detail = "album monitored but search trigger failed"
		return item
	}
	return withApplySuccess(item, album.Monitored)
}

func newApplyDiscoveredAlbumItem(candidate discoveredAlbumCandidate) applyDiscoveredAlbumItem {
	return applyDiscoveredAlbumItem{
		Rank:       candidate.Rank,
		ArtistName: candidate.ArtistName,
		AlbumTitle: candidate.AlbumTitle,
		Status:     "skipped",
	}
}

func withApplyError(item applyDiscoveredAlbumItem, err error) applyDiscoveredAlbumItem {
	item.Status = "error"
	item.Detail = lidarr.SanitizeApplyError(err)
	return item
}

func withApplyNotFound(item applyDiscoveredAlbumItem) applyDiscoveredAlbumItem {
	item.Status = "not_found"
	item.Detail = "album not found in library metadata"
	return item
}

func withApplyAmbiguous(item applyDiscoveredAlbumItem) applyDiscoveredAlbumItem {
	item.Status = "ambiguous"
	item.Detail = "multiple close album matches"
	return item
}

func withApplyDryRun(item applyDiscoveredAlbumItem, albumMonitored, artistAdded bool) applyDiscoveredAlbumItem {
	item.Status = "dry_run"
	switch {
	case albumMonitored:
		item.Detail = "already monitored"
	case artistAdded:
		item.Detail = "would add artist, monitor album, and trigger search"
	default:
		item.Detail = "would monitor album and trigger search"
	}
	return item
}

func withApplySuccess(item applyDiscoveredAlbumItem, albumMonitored bool) applyDiscoveredAlbumItem {
	item.Status = "ok"
	if albumMonitored {
		item.Detail = "already monitored; search triggered"
	} else {
		item.Detail = "album monitored and search triggered"
	}
	return item
}

func resolveDiscoveredAlbumSelection(ctx context.Context, args map[string]interface{}) ([]discoveredAlbumCandidate, time.Time, string, error) {
	candidates, updatedAt, sourceQuery := getLastDiscoveredAlbums(chatSessionIDFromContext(ctx))
	if len(candidates) == 0 {
		return nil, time.Time{}, "", fmt.Errorf("no discovered albums cached yet; run discoverAlbums first")
	}
	if time.Since(updatedAt) > 30*time.Minute {
		return nil, time.Time{}, "", fmt.Errorf("cached discovered albums are stale; run discoverAlbums again")
	}

	if raw, ok := args["selection"]; ok && raw != nil {
		selected, err := discovery.SelectCandidates(candidates, fmt.Sprintf("%v", raw))
		if err != nil {
			return nil, time.Time{}, "", err
		}
		return selected, updatedAt, sourceQuery, nil
	}

	limit := toolIntArg(args, "limit", len(candidates))
	if limit > len(candidates) {
		limit = len(candidates)
	}
	return candidates[:limit], updatedAt, sourceQuery, nil
}

func callDiscoveryJSON(ctx context.Context, provider, model, groqKey, huggingFaceKey, systemPrompt, userPrompt string, maxTokens int) (string, error) {
	body, err := json.Marshal(map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"max_completion_tokens": maxTokens,
		"response_format": map[string]string{
			"type": "json_object",
		},
	})
	if err != nil {
		return "", err
	}

	var (
		apiKey   string
		endpoint string
		label    string
	)
	switch provider {
	case discoveryProviderHF:
		apiKey = strings.TrimSpace(huggingFaceKey)
		endpoint = firstEnv("HUGGINGFACE_CHAT_COMPLETIONS_URL", "HF_CHAT_COMPLETIONS_URL")
		if endpoint == "" {
			endpoint = "https://router.huggingface.co/v1/chat/completions"
		}
		label = "Hugging Face"
	case discoveryProviderGroq:
		apiKey = strings.TrimSpace(groqKey)
		endpoint = firstEnv("GROQ_CHAT_COMPLETIONS_URL")
		if endpoint == "" {
			endpoint = "https://api.groq.com/openai/v1/chat/completions"
		}
		label = "Groq"
	default:
		return "", fmt.Errorf("unsupported discovery model provider: %s", provider)
	}
	if apiKey == "" {
		return "", fmt.Errorf("%s discovery is not configured", label)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("%s API returned %d: %s", label, resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if len(payload.Choices) == 0 {
		return "", fmt.Errorf("%s returned empty choices", label)
	}
	return payload.Choices[0].Message.Content, nil
}

func callGroqJSON(ctx context.Context, apiKey, model, systemPrompt, userPrompt string, maxTokens int) (string, error) {
	return callDiscoveryJSON(ctx, discoveryProviderGroq, model, apiKey, "", systemPrompt, userPrompt, maxTokens)
}

func urlQueryEscape(raw string) string {
	replacer := strings.NewReplacer(
		"%", "%25",
		" ", "%20",
		"\"", "%22",
		"#", "%23",
		"&", "%26",
		"+", "%2B",
		"/", "%2F",
		":", "%3A",
		";", "%3B",
		"=", "%3D",
		"?", "%3F",
	)
	return replacer.Replace(strings.TrimSpace(raw))
}
