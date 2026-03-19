package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"groovarr/internal/agent"

	"github.com/rs/zerolog/log"
)

type normalizedTurn struct {
	Intent                string   `json:"intent"`
	SubIntent             string   `json:"subIntent,omitempty"`
	StyleHints            []string `json:"styleHints,omitempty"`
	FollowupMode          string   `json:"followupMode"`
	QueryScope            string   `json:"queryScope"`
	LibraryOnly           *bool    `json:"libraryOnly,omitempty"`
	TimeWindow            string   `json:"timeWindow"`
	ResultSetKind         string   `json:"resultSetKind,omitempty"`
	ResultAction          string   `json:"resultAction,omitempty"`
	SelectionMode         string   `json:"selectionMode,omitempty"`
	SelectionValue        string   `json:"selectionValue,omitempty"`
	CompareSelectionMode  string   `json:"compareSelectionMode,omitempty"`
	CompareSelectionValue string   `json:"compareSelectionValue,omitempty"`
	TargetName            string   `json:"targetName,omitempty"`
	ArtistName            string   `json:"artistName,omitempty"`
	TrackTitle            string   `json:"trackTitle,omitempty"`
	PromptHint            string   `json:"promptHint,omitempty"`
	NeedsClarification    bool     `json:"needsClarification"`
	ClarificationFocus    string   `json:"clarificationFocus"`
	ReferenceTarget       string   `json:"referenceTarget"`
	ReferenceQualifier    string   `json:"referenceQualifier,omitempty"`
	Confidence            string   `json:"confidence"`
	ClarificationPrompt   string   `json:"clarificationPrompt,omitempty"`
}

type resolvedTurnContext struct {
	Turn                    normalizedTurn
	ResolvedReferenceKind   string
	ResolvedReferenceSource string
	ResolvedItemKey         string
	ResolvedItemSource      string
	HasCreativeAlbumSet     bool
	HasSemanticAlbumSet     bool
	HasDiscoveredAlbums     bool
	HasCleanupCandidates    bool
	HasBadlyRatedAlbums     bool
	HasRecentListening      bool
	HasPendingPlaylistPlan  bool
	HasResolvedScene        bool
	HasSongPath             bool
	HasTrackCandidates      bool
	HasArtistCandidates     bool
	AmbiguousReference      bool
	MissingReferenceContext bool
}

type groqTurnNormalizer struct {
	apiKey string
	model  string
}

func newGroqTurnNormalizer(apiKey, defaultModel string) chatTurnNormalizer {
	if !envBool("CHAT_NORMALIZER_ENABLED", true) {
		return nil
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil
	}
	model := strings.TrimSpace(os.Getenv("CHAT_NORMALIZER_MODEL"))
	if model == "" {
		model = strings.TrimSpace(defaultModel)
	}
	if model == "" {
		model = agent.DefaultGroqModel
	}
	return &groqTurnNormalizer{
		apiKey: apiKey,
		model:  model,
	}
}

func (n *groqTurnNormalizer) NormalizeTurn(ctx context.Context, msg string, history []agent.Message, sessionContext string) (normalizedTurn, error) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return normalizedTurn{}, nil
	}
	return n.normalizeWithPrompt(ctx, msg, history, sessionContext, buildTurnNormalizerSystemPrompt(false))
}

func buildTurnNormalizerSystemPrompt(referenceRecovery bool) string {
	systemPrompt := `You normalize user turns for a music assistant into strict JSON only.

Return exactly this schema:
{
  "intent": "album_discovery|track_discovery|artist_discovery|scene_discovery|listening|stats|playlist|general_chat|other",
  "subIntent": "short snake_case string or empty",
  "styleHints": ["short style cue", "optional second cue"],
  "followupMode": "none|refine_previous|query_previous_set|pivot",
  "queryScope": "general|library|listening|stats|playlist|unknown",
  "libraryOnly": true,
  "timeWindow": "none|last_month|this_month|this_year|explicit|ambiguous_recent",
  "resultSetKind": "none|creative_albums|semantic_albums|discovered_albums|cleanup_candidates|badly_rated_albums|playlist_candidates|recent_listening|scene_candidates|song_path|track_candidates|artist_candidates",
  "resultAction": "none|inspect_availability|preview_apply|apply_confirmed|compare|filter_by_play_window|pick_riskier|refine_style|select_candidate|describe_item",
  "selectionMode": "none|all|top_n|ordinal|explicit_names|missing_only|count_match",
  "selectionValue": "compact selection payload or empty",
  "compareSelectionMode": "none|all|top_n|ordinal|explicit_names|item_key",
  "compareSelectionValue": "compact secondary selection payload or empty",
  "targetName": "explicit playlist or target name when present",
  "artistName": "explicit artist name when present",
  "trackTitle": "explicit track title when present",
  "promptHint": "short append/refine prompt when present",
  "needsClarification": false,
  "clarificationFocus": "none|scope|time_window|target_type|reference|other",
  "referenceTarget": "none|previous_results|previous_taste|previous_playlist|previous_stats",
  "referenceQualifier": "none|latest_set|last_item|safer|riskier",
  "confidence": "low|medium|high",
  "clarificationPrompt": "one concise question or empty"
}

Rules:
- Be conservative. Do not invent constraints the user did not imply.
- Use album_discovery for recommendations, best albums by an artist, mood-based album finding, similarity album finding, and underplayed owned albums.
- Use track_discovery when the user explicitly wants a song/track match, nearest tracks, or a sonic description search for tracks they own.
- Use artist_discovery when the user explicitly wants nearest/similar artists in their library.
- Use scene_discovery when the user wants sonic scenes, sound neighborhoods, sonic pockets, or cluster-style grouping of their library.
- Use listening for recent listening summaries or referential questions about a prior album/result set.
- Use stats for top artists, dominance, comparative summaries, or library composition/stats.
- Use playlist only for explicit playlist creation, editing, repair, refresh, or availability follow-ups.
- Use general_chat for greetings, thanks, or simple casual conversation.
- Prefer these subIntent values when they fit:
  - listening_summary
  - listening_interpretation
  - result_set_play_recency
  - result_set_most_recent
  - artist_dominance
  - library_top_artists
  - creative_refinement
  - creative_risk_pick
  - creative_safe_pick
  - scene_overview
  - playlist_inventory
  - playlist_tracks_query
  - playlist_availability
  - playlist_append
  - playlist_refresh
  - playlist_repair
  - playlist_vibe
  - playlist_artist_coverage
  - playlist_queue_request
  - track_search
  - track_similarity
  - track_description
  - song_path_summary
  - artist_similarity
  - artist_starting_album
  - lidarr_cleanup_apply
  - badly_rated_cleanup
  - artist_remove
- Only set libraryOnly true when the user explicitly signals owned/library/shelves/already-have scope, including phrases like my albums, my records, my collection, my shelves, or from what I already have.
- Use last_month for lately/recently. Use this_month or this_year only when explicitly stated.
- Set needsClarification true only when missing detail materially changes the route or scope.
- When needsClarification is true, provide one short clarificationPrompt tailored to the user turn.
- If the user refers to earlier results with words like those/them/that pattern, use followupMode query_previous_set or refine_previous.
- For playlist follow-ups like "this playlist" or "that playlist", prefer referenceTarget=previous_playlist.
- Use referenceQualifier=latest_set for phrases like the last set, last batch, or previous batch.
- Use referenceQualifier=last_item for phrases like that one or the last one when the user is referring to a single prior item.
- Use referenceQualifier=safer or riskier when the user contrasts prior options that way.
- Use subIntent=result_set_play_recency when the user asks which prior results were played or touched within a time window.
- Use subIntent=result_set_most_recent when the user asks which prior result was most recently played or touched.
- Use subIntent=listening_interpretation for taste/phase/leaning questions about recent listening.
- Use subIntent=artist_dominance for who is leading or separating from the pack in listening/stats.
- Use subIntent=library_top_artists for broad library-footprint prompts such as heavy hitters or biggest names on the shelves.
- Use subIntent=creative_refinement for requests like less polished, darker, warmer, more intimate, less electronic.
- When using creative_refinement, populate styleHints with 1-4 short cues extracted from the user request.
- Use subIntent=creative_risk_pick when the user asks for the riskier, bolder, or braver option from prior results.
- Use subIntent=creative_safe_pick when the user asks for the safer, more familiar, or less risky option from prior results.
- Use subIntent=scene_overview when the user asks to split their library into scenes, clusters, sonic pockets, sound neighborhoods, or sonic regions.
- For combined follow-ups like "pick the less expected one and tell me what it sounds like", use the final action as subIntent (for example track_description or artist_starting_album) and use referenceQualifier=riskier or safer to express which prior candidate to act on.
- Use subIntent=track_search when the user asks for a track or song matching a sound, mood, texture, or sonic description they own.
- Use subIntent=track_similarity when the user asks for the closest, nearest, cousin, neighbor, or similar track to a specific track.
- Use subIntent=track_description when the user asks what a specific track sounds like or asks to describe one specific prior track result.
- Use subIntent=song_path_summary when the user asks about the feel, middle stretch, bridge, or character of a previously returned song path.
- Use subIntent=artist_similarity when the user asks which artist in their library is nearest or closest to a named artist.
- Use subIntent=artist_starting_album when the user asks for a starting record after choosing a similar artist.
- Combined follow-up examples:
  - "Take the less expected one and tell me what it sounds like." -> intent=track_discovery, subIntent=track_description, followupMode=refine_previous, referenceTarget=previous_results, referenceQualifier=riskier, resultSetKind=track_candidates.
  - "Take the safer one and tell me what it sounds like." -> intent=track_discovery, subIntent=track_description, followupMode=refine_previous, referenceTarget=previous_results, referenceQualifier=safer, resultSetKind=track_candidates.
  - "Take the less expected one and show me a strong starting record I already own." -> intent=artist_discovery, subIntent=artist_starting_album, followupMode=refine_previous, queryScope=library, libraryOnly=true, referenceTarget=previous_results, referenceQualifier=riskier, resultSetKind=artist_candidates.
  - "Take the safer one and show me a starting record I own." -> intent=artist_discovery, subIntent=artist_starting_album, followupMode=refine_previous, queryScope=library, libraryOnly=true, referenceTarget=previous_results, referenceQualifier=safer, resultSetKind=artist_candidates.
  - "Compare the safer one to the first." after prior track results -> intent=track_discovery, resultAction=compare, followupMode=refine_previous, referenceTarget=previous_results, referenceQualifier=safer, resultSetKind=track_candidates, compareSelectionMode=ordinal, compareSelectionValue="1".
  - "Compare the less expected one to the first." after prior artist results -> intent=artist_discovery, resultAction=compare, followupMode=refine_previous, referenceTarget=previous_results, referenceQualifier=riskier, resultSetKind=artist_candidates, compareSelectionMode=ordinal, compareSelectionValue="1".
- Use resultSetKind=discovered_albums when a follow-up is about the last discovered album list in this chat.
- If resultSetKind=discovered_albums, use intent=album_discovery.
- Use resultSetKind=scene_candidates when the user is choosing among previously listed sonic scenes.
- Use resultSetKind=song_path when a follow-up refers to a previously returned path or bridge between songs.
- Use resultSetKind=track_candidates when a follow-up is about the last listed tracks in this chat.
- Use resultSetKind=artist_candidates when a follow-up is about the last listed artists in this chat.
- Use subIntent=playlist_inventory when the user asks what playlists they have or to list saved playlists.
- Use subIntent=playlist_tracks_query when the user asks what tracks are in a playlist.
- Use subIntent=playlist_availability when the user asks how many planned playlist tracks are available, missing, or resolvable.
- Use subIntent=playlist_append for requests to add, append, extend, or reshape an existing playlist.
- Use subIntent=playlist_refresh for requests to refresh an existing playlist.
- Use subIntent=playlist_repair for requests to repair or fix an existing playlist.
- Use subIntent=playlist_vibe when the user asks about the overall vibe, feel, or character of a playlist.
- Use subIntent=playlist_artist_coverage when the user asks whether a playlist covers an artist's key or representative tracks.
- Use subIntent=playlist_queue_request when the user asks to queue or download missing tracks for a playlist or playlist plan.
- Use subIntent=lidarr_cleanup_apply when the user asks to apply, run, or carry out a recent library cleanup preview.
- Use subIntent=badly_rated_cleanup when the user asks to remove or delete recently identified badly rated albums.
- Use subIntent=artist_remove when the user asks to remove or delete an artist from Lidarr or from their library.
- Use resultAction=inspect_availability for follow-ups like which of those are already in my library, available, already monitored, or missing.
- Use resultAction=preview_apply for add/monitor/import/queue follow-ups on discovered albums that should start a preview, not direct mutation.
- Use resultSetKind=cleanup_candidates when a follow-up is about the last library cleanup preview in this chat.
- Use resultSetKind=badly_rated_albums when a follow-up is about the last badly rated album list in this chat.
- Use resultAction=select_candidate when the user chooses one item from previously listed scene candidates.
- Use resultAction=describe_item when the user asks what one chosen prior item sounds like.
- Use resultAction=compare when the user wants one prior result compared against another prior result.
- Use selectionMode=all for those/them/these, top_n for first N, ordinal for specific ranks like 2 and 4, explicit_names for named albums/artists, and missing_only only when the user explicitly says only missing/unowned ones.
- For compare follow-ups like "compare the safer one to the first", use referenceQualifier for the primary pick and compareSelectionMode / compareSelectionValue for the comparison target.
- Use selectionMode=count_match when the user refers to a scene by a numeric track count, such as "the one with 31 tracks".
- When a playlist name is explicitly present, populate targetName with that exact name.
- When the user explicitly names an artist for playlist_artist_coverage, populate artistName.
- When the user explicitly names an artist for artist_similarity, artist_starting_album, or track_similarity, populate artistName.
- When the user explicitly names a track for track_similarity or track_description, populate trackTitle exactly as given.
- When subIntent=artist_remove, populate artistName with the artist to remove.
- When subIntent=playlist_append, populate promptHint with the short modification request if present.
- When subIntent=playlist_queue_request and the user describes a new playlist idea, populate promptHint with that playlist prompt and use selectionMode=top_n if they specify a track count.
- Keep selectionValue compact. Examples: "2", "2,4", "Moon Safari by Air".
- Return JSON only. No markdown.`
	if referenceRecovery {
		systemPrompt += `

Additional recovery rules:
- This is a recovery pass for a likely referential follow-up. Prefer binding to the most recent authoritative result set in session context instead of asking for clarification when the user says things like safer one, less expected one, first, second, last, compare, or describe that one.
- When recent track_candidates exist, prefer track_discovery for these follow-ups.
- When recent artist_candidates exist, prefer artist_discovery for these follow-ups.
- For "compare the safer one to the first" style turns, use resultAction=compare, referenceTarget=previous_results, followupMode=refine_previous, referenceQualifier=safer or riskier, and compareSelectionMode / compareSelectionValue for the second selector.
- Only keep needsClarification=true if session context truly lacks a plausible recent result set.`
	}
	return systemPrompt
}

func (n *groqTurnNormalizer) normalizeWithPrompt(ctx context.Context, msg string, history []agent.Message, sessionContext, systemPrompt string) (normalizedTurn, error) {
	userPrompt := fmt.Sprintf(
		"Latest user message:\n%s\n\nRecent chat history:\n%s\n\nServer session context:\n%s",
		msg,
		renderNormalizerHistory(history),
		renderNormalizerSessionContext(sessionContext),
	)

	timeoutMS := envInt("CHAT_NORMALIZER_TIMEOUT_MS", 4000)
	if timeoutMS < 500 {
		timeoutMS = 500
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()

	raw, err := callGroqJSON(callCtx, n.apiKey, n.model, systemPrompt, userPrompt, envInt("CHAT_NORMALIZER_MAX_TOKENS", 300))
	if err != nil {
		return normalizedTurn{}, err
	}

	var parsed normalizedTurn
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return normalizedTurn{}, fmt.Errorf("failed to parse normalizer response: %w", err)
	}
	return parsed, nil
}

func (s *Server) normalizeResolvedTurn(ctx context.Context, sessionID, msg string, history []agent.Message, sessionContext string) (*resolvedTurnContext, error) {
	if s.normalizer == nil {
		return nil, nil
	}
	turn, err := s.normalizer.NormalizeTurn(ctx, msg, history, sessionContext)
	if err != nil {
		return nil, err
	}
	resolved := resolveTurnContext(sessionID, sanitizeNormalizedTurn(msg, turn))
	if retry, ok := s.retryReferenceRecovery(ctx, msg, history, sessionContext, resolved); ok {
		resolved = retry
	}
	setLastNormalizedTurn(sessionID, resolved.Turn)
	return &resolved, nil
}

func (s *Server) retryReferenceRecovery(ctx context.Context, msg string, history []agent.Message, sessionContext string, resolved resolvedTurnContext) (resolvedTurnContext, bool) {
	normalizer, ok := s.normalizer.(*groqTurnNormalizer)
	if !ok {
		return resolvedTurnContext{}, false
	}
	if !resolved.Turn.NeedsClarification || strings.TrimSpace(resolved.Turn.ClarificationFocus) != "reference" {
		return resolvedTurnContext{}, false
	}
	if strings.TrimSpace(resolved.Turn.Confidence) != "low" {
		return resolvedTurnContext{}, false
	}
	if !(resolved.HasTrackCandidates || resolved.HasArtistCandidates) {
		return resolvedTurnContext{}, false
	}
	if retryResolved, ok := s.retryComparativeFollowupRecovery(ctx, msg, history, sessionContext, resolved, normalizer); ok {
		return retryResolved, true
	}
	retryTurn, err := normalizer.normalizeWithPrompt(ctx, msg, history, sessionContext, buildTurnNormalizerSystemPrompt(true))
	if err != nil {
		return resolvedTurnContext{}, false
	}
	retryResolved := resolveTurnContext(chatSessionIDFromContext(ctx), sanitizeNormalizedTurn(msg, retryTurn))
	if retryResolved.Turn.NeedsClarification {
		return resolvedTurnContext{}, false
	}
	return retryResolved, true
}

func (s *Server) retryComparativeFollowupRecovery(ctx context.Context, msg string, history []agent.Message, sessionContext string, resolved resolvedTurnContext, normalizer *groqTurnNormalizer) (resolvedTurnContext, bool) {
	prevTurn, _, ok := getLastNormalizedTurn(chatSessionIDFromContext(ctx))
	if !ok {
		return resolvedTurnContext{}, false
	}
	if prevTurn.Intent != "track_discovery" && prevTurn.Intent != "artist_discovery" {
		return resolvedTurnContext{}, false
	}
	systemPrompt := buildTurnNormalizerSystemPrompt(true) + `

Additional comparison-followup rules:
- The previous normalized turn is authoritative context for the result family.
- If the previous turn was track_discovery and the user asks to compare one prior option against another, keep intent=track_discovery and use resultAction=compare.
- If the previous turn was artist_discovery and the user asks to compare one prior option against another, keep intent=artist_discovery and use resultAction=compare.
- Use referenceQualifier for the primary pick like safer or riskier.
- Use compareSelectionMode / compareSelectionValue for the other target like first, second, or last.
- Do not ask for clarification if the previous normalized turn plus current session context makes the compare target family clear.`
	prevJSON, err := json.Marshal(prevTurn)
	if err != nil {
		return resolvedTurnContext{}, false
	}
	userPrompt := fmt.Sprintf(
		"Latest user message:\n%s\n\nPrevious normalized turn:\n%s\n\nRecent chat history:\n%s\n\nServer session context:\n%s",
		strings.TrimSpace(msg),
		string(prevJSON),
		renderNormalizerHistory(history),
		renderNormalizerSessionContext(sessionContext),
	)
	timeoutMS := envInt("CHAT_NORMALIZER_TIMEOUT_MS", 4000)
	if timeoutMS < 500 {
		timeoutMS = 500
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()
	raw, err := callGroqJSON(callCtx, normalizer.apiKey, normalizer.model, systemPrompt, userPrompt, envInt("CHAT_NORMALIZER_MAX_TOKENS", 300))
	if err != nil {
		return resolvedTurnContext{}, false
	}
	var retryTurn normalizedTurn
	if err := json.Unmarshal([]byte(raw), &retryTurn); err != nil {
		return resolvedTurnContext{}, false
	}
	retryResolved := resolveTurnContext(chatSessionIDFromContext(ctx), sanitizeNormalizedTurn(msg, retryTurn))
	if retryResolved.Turn.NeedsClarification {
		return resolvedTurnContext{}, false
	}
	return retryResolved, true
}

func sanitizeNormalizedTurn(msg string, turn normalizedTurn) normalizedTurn {
	turn.Intent = normalizeEnum(strings.ToLower(strings.TrimSpace(turn.Intent)), "other",
		"album_discovery", "track_discovery", "artist_discovery", "scene_discovery", "listening", "stats", "playlist", "general_chat", "other",
	)
	turn.SubIntent = normalizeSnakeCase(turn.SubIntent)
	turn.StyleHints = normalizeStyleHints(turn.StyleHints)
	turn.FollowupMode = normalizeEnum(strings.ToLower(strings.TrimSpace(turn.FollowupMode)), "none",
		"none", "refine_previous", "query_previous_set", "pivot",
	)
	turn.QueryScope = normalizeEnum(strings.ToLower(strings.TrimSpace(turn.QueryScope)), "unknown",
		"general", "library", "listening", "stats", "playlist", "unknown",
	)
	turn.TimeWindow = normalizeEnum(strings.ToLower(strings.TrimSpace(turn.TimeWindow)), "none",
		"none", "last_month", "this_month", "this_year", "explicit", "ambiguous_recent",
	)
	turn.ResultSetKind = normalizeEnum(strings.ToLower(strings.TrimSpace(turn.ResultSetKind)), "none",
		"none", "creative_albums", "semantic_albums", "discovered_albums", "cleanup_candidates", "badly_rated_albums", "playlist_candidates", "recent_listening", "scene_candidates", "song_path", "track_candidates", "artist_candidates",
	)
	turn.ResultAction = normalizeEnum(strings.ToLower(strings.TrimSpace(turn.ResultAction)), "none",
		"none", "inspect_availability", "preview_apply", "apply_confirmed", "compare", "filter_by_play_window", "pick_riskier", "refine_style", "select_candidate", "describe_item",
	)
	turn.SelectionMode = normalizeEnum(strings.ToLower(strings.TrimSpace(turn.SelectionMode)), "none",
		"none", "all", "top_n", "ordinal", "explicit_names", "missing_only", "count_match",
	)
	turn.SelectionValue = compactText(strings.TrimSpace(turn.SelectionValue), 180)
	turn.CompareSelectionMode = normalizeEnum(strings.ToLower(strings.TrimSpace(turn.CompareSelectionMode)), "none",
		"none", "all", "top_n", "ordinal", "explicit_names", "item_key",
	)
	turn.CompareSelectionValue = compactText(strings.TrimSpace(turn.CompareSelectionValue), 180)
	turn.TargetName = compactText(strings.TrimSpace(turn.TargetName), 180)
	turn.ArtistName = compactText(strings.TrimSpace(turn.ArtistName), 160)
	turn.TrackTitle = compactText(strings.TrimSpace(turn.TrackTitle), 180)
	turn.PromptHint = compactText(strings.TrimSpace(turn.PromptHint), 220)
	turn.ClarificationFocus = normalizeEnum(strings.ToLower(strings.TrimSpace(turn.ClarificationFocus)), "none",
		"none", "scope", "time_window", "target_type", "reference", "other",
	)
	turn.ReferenceTarget = normalizeEnum(strings.ToLower(strings.TrimSpace(turn.ReferenceTarget)), "none",
		"none", "previous_results", "previous_taste", "previous_playlist", "previous_stats",
	)
	turn.ReferenceQualifier = normalizeEnum(strings.ToLower(strings.TrimSpace(turn.ReferenceQualifier)), "none",
		"none", "latest_set", "last_item", "safer", "riskier",
	)
	turn.Confidence = normalizeEnum(strings.ToLower(strings.TrimSpace(turn.Confidence)), "medium",
		"low", "medium", "high",
	)
	turn.ClarificationPrompt = compactText(strings.TrimSpace(turn.ClarificationPrompt), 220)

	if turn.LibraryOnly != nil && *turn.LibraryOnly {
		turn.QueryScope = "library"
	}
	if turn.ReferenceQualifier != "none" && turn.ReferenceTarget == "none" {
		turn.ReferenceTarget = "previous_results"
	}
	if turn.ReferenceQualifier != "none" && turn.FollowupMode == "none" {
		turn.FollowupMode = "refine_previous"
	}
	if turn.ReferenceTarget == "none" && turn.FollowupMode != "none" {
		turn.ReferenceTarget = "previous_results"
	}
	if turn.Intent == "playlist" &&
		(turn.SubIntent == "playlist_append" ||
			turn.SubIntent == "playlist_refresh" ||
			turn.SubIntent == "playlist_repair" ||
			turn.SubIntent == "playlist_tracks_query" ||
			turn.SubIntent == "playlist_vibe" ||
			turn.SubIntent == "playlist_artist_coverage" ||
			turn.SubIntent == "playlist_queue_request") &&
		turn.ReferenceTarget == "previous_results" {
		turn.ReferenceTarget = "previous_playlist"
	}
	if turn.ResultSetKind == "discovered_albums" {
		turn.Intent = "album_discovery"
	}
	if turn.ResultSetKind == "track_candidates" {
		turn.Intent = "track_discovery"
	}
	if turn.ResultSetKind == "artist_candidates" {
		turn.Intent = "artist_discovery"
	}
	if turn.ResultSetKind == "cleanup_candidates" || turn.ResultSetKind == "badly_rated_albums" {
		turn.Intent = "other"
	}
	if turn.ResultSetKind == "scene_candidates" {
		turn.Intent = "scene_discovery"
	}
	if turn.ResultAction != "none" && turn.SelectionMode == "none" && turn.FollowupMode != "none" {
		turn.SelectionMode = "all"
	}
	if (turn.SelectionMode == "explicit_names" || turn.SelectionMode == "count_match") && turn.SelectionValue == "" {
		turn.SelectionMode = "all"
	}
	if turn.SelectionMode == "all" {
		turn.SelectionValue = ""
	}
	if turn.SelectionMode == "none" {
		turn.SelectionValue = ""
	}
	if turn.CompareSelectionMode == "all" {
		turn.CompareSelectionValue = ""
	}
	if turn.CompareSelectionMode == "none" {
		turn.CompareSelectionValue = ""
	}

	inferDefaultQueryScope(&turn)

	if turn.SubIntent == "" {
		switch {
		case turn.Intent == "listening" && turn.FollowupMode == "query_previous_set" && turn.ReferenceTarget == "previous_results" && turn.TimeWindow != "none":
			turn.SubIntent = "result_set_play_recency"
		case turn.Intent == "listening" && turn.FollowupMode == "none" && turn.TimeWindow != "none":
			turn.SubIntent = "listening_summary"
		case (turn.Intent == "album_discovery" || turn.Intent == "listening") && turn.ReferenceQualifier == "riskier":
			turn.SubIntent = "creative_risk_pick"
		case (turn.Intent == "album_discovery" || turn.Intent == "listening") && turn.ReferenceQualifier == "safer":
			turn.SubIntent = "creative_safe_pick"
		case turn.Intent == "track_discovery" && turn.TrackTitle != "" && turn.FollowupMode == "none":
			turn.SubIntent = "track_description"
		}
	}
	if turn.SubIntent == "compare" && turn.ResultAction == "none" {
		turn.ResultAction = "compare"
		turn.SubIntent = ""
	}
	if turn.Intent == "album_discovery" && turn.ResultAction != "none" && turn.ResultSetKind == "none" && turn.ReferenceTarget == "previous_results" {
		turn.ResultSetKind = "discovered_albums"
	}
	if turn.Intent == "track_discovery" && turn.ResultSetKind == "none" && turn.ReferenceTarget == "previous_results" {
		turn.ResultSetKind = "track_candidates"
	}
	if turn.Intent == "artist_discovery" && turn.ResultSetKind == "none" && turn.ReferenceTarget == "previous_results" {
		turn.ResultSetKind = "artist_candidates"
	}
	if turn.Intent == "scene_discovery" && turn.ResultSetKind == "none" && turn.ReferenceTarget == "previous_results" {
		turn.ResultSetKind = "scene_candidates"
	}
	if turn.ResultAction == "compare" && turn.Intent == "other" && turn.ReferenceTarget == "previous_results" {
		if turn.ResultSetKind == "track_candidates" {
			turn.Intent = "track_discovery"
		}
		if turn.ResultSetKind == "artist_candidates" {
			turn.Intent = "artist_discovery"
		}
	}
	if turn.Intent == "track_discovery" && turn.ResultSetKind == "none" && turn.FollowupMode != "none" &&
		(turn.SubIntent == "creative_risk_pick" || turn.SubIntent == "creative_safe_pick") && turn.ReferenceTarget == "previous_results" {
		turn.ResultSetKind = "track_candidates"
	}
	if turn.Intent == "artist_discovery" && turn.ResultSetKind == "none" && turn.FollowupMode != "none" &&
		(turn.SubIntent == "creative_risk_pick" || turn.SubIntent == "creative_safe_pick") && turn.ReferenceTarget == "previous_results" {
		turn.ResultSetKind = "artist_candidates"
	}
	if turn.SubIntent == "lidarr_cleanup_apply" && turn.ResultSetKind == "none" && turn.ReferenceTarget == "previous_results" {
		turn.ResultSetKind = "cleanup_candidates"
	}
	if turn.SubIntent == "badly_rated_cleanup" && turn.ResultSetKind == "none" && turn.ReferenceTarget == "previous_results" {
		turn.ResultSetKind = "badly_rated_albums"
	}
	if turn.ResultAction == "compare" && turn.CompareSelectionMode == "none" && turn.SelectionMode == "none" && turn.ReferenceQualifier == "none" {
		turn.NeedsClarification = true
		turn.ClarificationFocus = "reference"
		if turn.ClarificationPrompt == "" {
			turn.ClarificationPrompt = "Which two earlier results do you want me to compare?"
		}
	}
	inferDefaultQueryScope(&turn)

	if turn.TimeWindow == "ambiguous_recent" && (turn.Intent == "listening" || turn.Intent == "stats") && turn.ClarificationFocus == "time_window" {
		turn.NeedsClarification = false
		turn.ClarificationFocus = "none"
		turn.ClarificationPrompt = ""
	}

	if !turn.NeedsClarification {
		turn.ClarificationFocus = "none"
		turn.ClarificationPrompt = ""
	}
	if turn.NeedsClarification && turn.ClarificationFocus == "none" {
		turn.ClarificationFocus = "other"
	}

	return turn
}

func inferDefaultQueryScope(turn *normalizedTurn) {
	if turn == nil || turn.QueryScope != "unknown" {
		return
	}
	switch turn.Intent {
	case "playlist":
		turn.QueryScope = "playlist"
	case "listening":
		turn.QueryScope = "listening"
	case "stats":
		turn.QueryScope = "stats"
	case "scene_discovery", "track_discovery", "artist_discovery":
		turn.QueryScope = "library"
	case "album_discovery":
		if turn.LibraryOnly != nil && *turn.LibraryOnly {
			turn.QueryScope = "library"
		} else {
			turn.QueryScope = "general"
		}
	}
}

func resolveTurnContext(sessionID string, turn normalizedTurn) resolvedTurnContext {
	sessionID = normalizeChatSessionID(sessionID)
	resolved := resolvedTurnContext{Turn: turn}
	if candidates, _, _, _ := getLastCreativeAlbumSet(sessionID); len(candidates) > 0 {
		resolved.HasCreativeAlbumSet = true
	}
	if matches, _, _ := getLastSemanticAlbumSearch(sessionID); len(matches) > 0 {
		resolved.HasSemanticAlbumSet = true
	}
	if candidates, _, _ := getLastDiscoveredAlbums(sessionID); len(candidates) > 0 {
		resolved.HasDiscoveredAlbums = true
	}
	if candidates, _ := getLastLidarrCandidates(sessionID); len(candidates) > 0 {
		resolved.HasCleanupCandidates = true
	}
	if candidates, _ := getLastBadlyRatedAlbums(sessionID); len(candidates) > 0 {
		resolved.HasBadlyRatedAlbums = true
	}
	if _, ok := getLastRecentListeningSummary(sessionID); ok {
		resolved.HasRecentListening = true
	}
	if _, _, _, candidates := getLastPlannedPlaylist(sessionID); len(candidates) > 0 {
		resolved.HasPendingPlaylistPlan = true
	}
	if sceneState, ok := getLastSceneSelection(sessionID); ok && (sceneState.Resolved != nil || len(sceneState.Candidates) > 0) {
		resolved.HasResolvedScene = true
	}
	if state, ok := getLastSongPath(sessionID); ok && len(state.path) > 0 {
		resolved.HasSongPath = true
	}
	if candidates, _, _, _ := getLastTrackCandidateSet(sessionID); len(candidates) > 0 {
		resolved.HasTrackCandidates = true
	}
	if candidates, _, _ := getLastArtistCandidateSet(sessionID); len(candidates) > 0 {
		resolved.HasArtistCandidates = true
	}
	if resolved.Turn.Intent == "album_discovery" && resolved.Turn.ResultAction != "" && resolved.Turn.ResultAction != "none" && (resolved.Turn.ResultSetKind == "" || resolved.Turn.ResultSetKind == "none") && resolved.HasDiscoveredAlbums {
		resolved.Turn.ResultSetKind = "discovered_albums"
	}
	if resolved.Turn.Intent == "playlist" && resolved.Turn.ResultAction == "inspect_availability" && (resolved.Turn.ResultSetKind == "" || resolved.Turn.ResultSetKind == "none") && resolved.HasPendingPlaylistPlan {
		resolved.Turn.ResultSetKind = "playlist_candidates"
	}
	if (resolved.Turn.SubIntent == "lidarr_cleanup_apply" || resolved.Turn.ResultAction == "preview_apply") &&
		(resolved.Turn.ResultSetKind == "" || resolved.Turn.ResultSetKind == "none") && resolved.HasCleanupCandidates {
		resolved.Turn.ResultSetKind = "cleanup_candidates"
	}
	if (resolved.Turn.SubIntent == "badly_rated_cleanup" || resolved.Turn.ResultAction == "preview_apply") &&
		(resolved.Turn.ResultSetKind == "" || resolved.Turn.ResultSetKind == "none") && resolved.HasBadlyRatedAlbums {
		resolved.Turn.ResultSetKind = "badly_rated_albums"
	}
	if resolved.Turn.ResultAction == "select_candidate" && (resolved.Turn.ResultSetKind == "" || resolved.Turn.ResultSetKind == "none") && resolved.HasResolvedScene {
		resolved.Turn.ResultSetKind = "scene_candidates"
	}
	if resolved.Turn.SubIntent == "song_path_summary" && resolved.HasSongPath {
		if resolved.Turn.ReferenceTarget == "" || resolved.Turn.ReferenceTarget == "none" {
			resolved.Turn.ReferenceTarget = "previous_results"
		}
		if resolved.Turn.FollowupMode == "" || resolved.Turn.FollowupMode == "none" {
			resolved.Turn.FollowupMode = "refine_previous"
		}
		if resolved.Turn.ResultSetKind == "" || resolved.Turn.ResultSetKind == "none" {
			resolved.Turn.ResultSetKind = "song_path"
		}
		if resolved.Turn.Intent == "general_chat" || resolved.Turn.Intent == "other" {
			resolved.Turn.Intent = "track_discovery"
		}
	}
	if resolved.Turn.Intent == "track_discovery" && (resolved.Turn.ResultSetKind == "" || resolved.Turn.ResultSetKind == "none") && resolved.HasTrackCandidates && resolved.Turn.FollowupMode != "none" {
		resolved.Turn.ResultSetKind = "track_candidates"
	}
	if resolved.Turn.Intent == "artist_discovery" && (resolved.Turn.ResultSetKind == "" || resolved.Turn.ResultSetKind == "none") && resolved.HasArtistCandidates && resolved.Turn.FollowupMode != "none" {
		resolved.Turn.ResultSetKind = "artist_candidates"
	}
	if resolved.Turn.ResultAction == "compare" && resolved.Turn.ReferenceTarget == "previous_results" {
		if (resolved.Turn.ResultSetKind == "" || resolved.Turn.ResultSetKind == "none") && resolved.HasTrackCandidates && !resolved.HasArtistCandidates {
			resolved.Turn.ResultSetKind = "track_candidates"
			if resolved.Turn.Intent == "other" || resolved.Turn.Intent == "general_chat" {
				resolved.Turn.Intent = "track_discovery"
			}
		}
		if (resolved.Turn.ResultSetKind == "" || resolved.Turn.ResultSetKind == "none") && resolved.HasArtistCandidates && !resolved.HasTrackCandidates {
			resolved.Turn.ResultSetKind = "artist_candidates"
			if resolved.Turn.Intent == "other" || resolved.Turn.Intent == "general_chat" {
				resolved.Turn.Intent = "artist_discovery"
			}
		}
	}
	if resolved.Turn.SubIntent == "track_description" && strings.TrimSpace(resolved.Turn.TrackTitle) == "" && resolved.HasTrackCandidates {
		if resolved.Turn.ReferenceTarget == "" || resolved.Turn.ReferenceTarget == "none" {
			resolved.Turn.ReferenceTarget = "previous_results"
		}
		if resolved.Turn.FollowupMode == "" || resolved.Turn.FollowupMode == "none" {
			resolved.Turn.FollowupMode = "refine_previous"
		}
		if resolved.Turn.ResultSetKind == "" || resolved.Turn.ResultSetKind == "none" {
			resolved.Turn.ResultSetKind = "track_candidates"
		}
	}
	if resolved.Turn.SubIntent == "artist_starting_album" && strings.TrimSpace(resolved.Turn.ArtistName) == "" && resolved.HasArtistCandidates {
		if resolved.Turn.ReferenceTarget == "" || resolved.Turn.ReferenceTarget == "none" {
			resolved.Turn.ReferenceTarget = "previous_results"
		}
		if resolved.Turn.FollowupMode == "" || resolved.Turn.FollowupMode == "none" {
			resolved.Turn.FollowupMode = "refine_previous"
		}
		if resolved.Turn.ResultSetKind == "" || resolved.Turn.ResultSetKind == "none" {
			resolved.Turn.ResultSetKind = "artist_candidates"
		}
	}
	resolveStructuredReference(sessionID, &resolved)
	if resolved.Turn.ClarificationFocus == "reference" && resolved.Turn.ReferenceTarget == "previous_results" &&
		!resolved.MissingReferenceContext && !resolved.AmbiguousReference && resolved.ResolvedReferenceKind != "" {
		resolved.Turn.NeedsClarification = false
		resolved.Turn.ClarificationFocus = "none"
		resolved.Turn.ClarificationPrompt = ""
	}
	if turn.FollowupMode != "none" && !resolved.hasReferenceContext() {
		resolved.MissingReferenceContext = true
		resolved.Turn.NeedsClarification = true
		resolved.Turn.ClarificationFocus = "reference"
		resolved.Turn.ReferenceTarget = "previous_results"
		if strings.TrimSpace(resolved.Turn.ClarificationPrompt) == "" {
			resolved.Turn.ClarificationPrompt = "Which earlier results do you mean?"
		}
	}
	return resolved
}

func (r resolvedTurnContext) hasReferenceContext() bool {
	return r.HasCreativeAlbumSet || r.HasSemanticAlbumSet || r.HasDiscoveredAlbums || r.HasCleanupCandidates || r.HasBadlyRatedAlbums || r.HasRecentListening || r.HasPendingPlaylistPlan || r.HasResolvedScene || r.HasSongPath || r.HasTrackCandidates || r.HasArtistCandidates
}

func (s *Server) tryNormalizedClarification(msg string, resolved *resolvedTurnContext) (string, bool) {
	if resolved == nil {
		return "", false
	}
	turn := resolved.Turn
	if !turn.NeedsClarification {
		return "", false
	}
	if prompt := strings.TrimSpace(turn.ClarificationPrompt); prompt != "" {
		return prompt, true
	}
	switch turn.ClarificationFocus {
	case "reference":
		return "Which earlier results do you mean?", true
	case "scope":
		if turn.Intent == "stats" {
			return "Do you want library stats or listening stats?", true
		}
		if turn.Intent == "album_discovery" {
			return "Do you want that from your library, or as general recommendations?", true
		}
	case "target_type":
		if turn.Intent == "playlist" {
			return "What kind of playlist do you want me to make?", true
		}
	case "time_window":
		return "What time window do you want me to use?", true
	}
	return "", false
}

func buildNormalizedTurnContext(resolved *resolvedTurnContext) string {
	if resolved == nil {
		return ""
	}
	return "server_turn_request: " + renderServerTurnRequest(resolved)
}

func buildAgentTurnSignals(resolved *resolvedTurnContext) *agent.TurnSignals {
	if resolved == nil {
		return nil
	}
	signals := &agent.TurnSignals{
		Intent:                 strings.TrimSpace(resolved.Turn.Intent),
		QueryScope:             strings.TrimSpace(resolved.Turn.QueryScope),
		FollowupMode:           strings.TrimSpace(resolved.Turn.FollowupMode),
		HasCreativeAlbumSet:    resolved.HasCreativeAlbumSet,
		HasSemanticAlbumSet:    resolved.HasSemanticAlbumSet,
		HasDiscoveredAlbums:    resolved.HasDiscoveredAlbums,
		HasRecentListening:     resolved.HasRecentListening,
		HasPendingPlaylistPlan: resolved.HasPendingPlaylistPlan,
		HasResolvedScene:       resolved.HasResolvedScene,
		HasSongPath:            resolved.HasSongPath,
		HasTrackCandidates:     resolved.HasTrackCandidates,
		HasArtistCandidates:    resolved.HasArtistCandidates,
	}
	if resolved.Turn.LibraryOnly != nil {
		signals.LibraryOnly = *resolved.Turn.LibraryOnly
	}
	return signals
}

func renderNormalizerHistory(history []agent.Message) string {
	if len(history) == 0 {
		return "none"
	}
	lines := make([]string, 0, minInt(len(history), 6))
	start := 0
	if len(history) > 6 {
		start = len(history) - 6
	}
	for _, msg := range history[start:] {
		role := strings.TrimSpace(msg.Role)
		content := strings.TrimSpace(msg.Content)
		if role == "" || content == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s", role, compactText(content, 180)))
	}
	if len(lines) == 0 {
		return "none"
	}
	return strings.Join(lines, "\n")
}

func renderNormalizerSessionContext(sessionContext string) string {
	sessionContext = compactText(strings.TrimSpace(sessionContext), 900)
	if sessionContext == "" {
		return "none"
	}
	return sessionContext
}

func compactText(raw string, maxChars int) string {
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if raw == "" || maxChars <= 0 {
		return raw
	}
	runes := []rune(raw)
	if len(runes) <= maxChars {
		return raw
	}
	return string(runes[:maxChars]) + "..."
}

func normalizeStyleHints(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, hint := range raw {
		hint = strings.ToLower(compactText(strings.TrimSpace(hint), 48))
		hint = strings.Join(strings.Fields(hint), " ")
		if hint == "" {
			continue
		}
		if _, ok := seen[hint]; ok {
			continue
		}
		seen[hint] = struct{}{}
		out = append(out, hint)
		if len(out) >= 4 {
			break
		}
	}
	return out
}

func normalizeEnum(value, fallback string, allowed ...string) string {
	for _, candidate := range allowed {
		if value == candidate {
			return value
		}
	}
	return fallback
}

func normalizeSnakeCase(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

func (s *Server) maybeNormalizeTurn(ctx context.Context, sessionID, msg string, history []agent.Message, sessionContext string) *resolvedTurnContext {
	resolved, err := s.normalizeResolvedTurn(ctx, sessionID, msg, history, sessionContext)
	if err != nil {
		log.Warn().Err(err).Str("request_id", chatRequestIDFromContext(ctx)).Msg("Chat normalizer failed")
		return nil
	}
	if resolved == nil {
		logChatPipelineStage(ctx, "normalizer_skipped", map[string]string{
			"message": msg,
		})
		return nil
	}
	logChatPipelineStage(ctx, "normalizer", map[string]string{
		"message":    msg,
		"normalized": buildNormalizedTurnContext(resolved),
	})
	return resolved
}
