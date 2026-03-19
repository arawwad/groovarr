package main

import (
	"context"
	"encoding/json"
	"fmt"
	"groovarr/internal/agent"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func playlistCandidateResultSetCapability() resultSetCapability {
	return resultSetCapability{
		SetKind:    "playlist_candidates",
		Operations: []string{"inspect_availability"},
		Selectors:  []string{"all", "top_n", "item_key"},
	}
}

func playlistExecutionHandlers() []serverExecutionHandler {
	return []serverExecutionHandler{
		{
			name: "playlist_candidate_availability",
			canHandle: func(request serverExecutionRequest) bool {
				return strings.TrimSpace(request.SetKind) == "playlist_candidates" &&
					strings.TrimSpace(request.Operation) == "inspect_availability"
			},
			execute: func(ctx context.Context, s *Server, _ []agent.Message, resolved *resolvedTurnContext) (ChatResponse, bool) {
				outcome, ok := s.resolveStructuredPlaylistAvailabilityOutcome(ctx, resolved)
				if !ok {
					return ChatResponse{}, false
				}
				if resp, ok := renderStructuredPlaylistAvailability(outcome); ok {
					return ChatResponse{Response: resp}, true
				}
				return ChatResponse{}, false
			},
		},
	}
}

type playlistAvailabilityOutcome struct {
	playlistName string
	total        int
	available    int
	missing      int
	ambiguous    int
	errors       int
}

type playlistWorkflowOutcome struct {
	kind          string
	response      string
	pendingAction *PendingAction
}

func (s *Server) handleStructuredPlaylistTracksQuery(ctx context.Context, resolved *resolvedTurnContext, history []agent.Message) (string, bool) {
	if resolved == nil || strings.TrimSpace(resolved.Turn.SubIntent) != "playlist_tracks_query" {
		return "", false
	}
	playlistName, ok := s.resolveStructuredPlaylistTarget(ctx, resolved.Turn, history)
	if !ok {
		return "", false
	}
	return s.renderSavedPlaylistTracks(ctx, playlistName)
}

func (s *Server) handleStructuredPlaylistInventory(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	if resolved == nil || strings.TrimSpace(resolved.Turn.SubIntent) != "playlist_inventory" {
		return "", false
	}
	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "navidromePlaylists", map[string]interface{}{
		"limit": 20,
	})
	if err != nil {
		return "", false
	}
	var parsed struct {
		Data struct {
			Playlists struct {
				Items []struct {
					Name string `json:"name"`
				} `json:"playlists"`
			} `json:"navidromePlaylists"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || len(parsed.Data.Playlists.Items) == 0 {
		return "", false
	}
	items := make([]string, 0, len(parsed.Data.Playlists.Items))
	for _, item := range parsed.Data.Playlists.Items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		items = append(items, name)
	}
	if len(items) == 0 {
		return "", false
	}
	return renderRouteBulletList("Your playlists", items, 12), true
}

func (s *Server) handleStructuredSavedPlaylistAppend(ctx context.Context, resolved *resolvedTurnContext, history []agent.Message) (ChatResponse, bool) {
	outcome, ok := s.resolveStructuredSavedPlaylistAppendOutcome(ctx, resolved, history)
	if !ok {
		return ChatResponse{}, false
	}
	return renderStructuredPlaylistWorkflowOutcome(outcome)
}

func (s *Server) resolveStructuredSavedPlaylistAppendOutcome(ctx context.Context, resolved *resolvedTurnContext, history []agent.Message) (playlistWorkflowOutcome, bool) {
	if resolved == nil || strings.TrimSpace(resolved.Turn.SubIntent) != "playlist_append" {
		return playlistWorkflowOutcome{}, false
	}
	playlistName, ok := s.resolveStructuredPlaylistTarget(ctx, resolved.Turn, history)
	if !ok {
		return playlistWorkflowOutcome{kind: "append_missing_target"}, true
	}
	prompt := strings.TrimSpace(resolved.Turn.PromptHint)
	if prompt == "" && len(resolved.Turn.StyleHints) > 0 {
		prompt = strings.Join(resolved.Turn.StyleHints, " ")
	}
	if prompt == "" {
		return playlistWorkflowOutcome{kind: "append_missing_prompt"}, true
	}
	response, pendingAction, err := s.startPlaylistAppendPreview(ctx, playlistName, prompt, 5)
	if err != nil {
		return playlistWorkflowOutcome{kind: "workflow_error", response: err.Error()}, true
	}
	return playlistWorkflowOutcome{kind: "workflow_preview", response: response, pendingAction: pendingAction}, true
}

func (s *Server) handleStructuredSavedPlaylistRefresh(ctx context.Context, resolved *resolvedTurnContext, history []agent.Message) (ChatResponse, bool) {
	outcome, ok := s.resolveStructuredSavedPlaylistRefreshOutcome(ctx, resolved, history)
	if !ok {
		return ChatResponse{}, false
	}
	return renderStructuredPlaylistWorkflowOutcome(outcome)
}

func (s *Server) resolveStructuredSavedPlaylistRefreshOutcome(ctx context.Context, resolved *resolvedTurnContext, history []agent.Message) (playlistWorkflowOutcome, bool) {
	if resolved == nil || strings.TrimSpace(resolved.Turn.SubIntent) != "playlist_refresh" {
		return playlistWorkflowOutcome{}, false
	}
	playlistName, ok := s.resolveStructuredPlaylistTarget(ctx, resolved.Turn, history)
	if !ok {
		return playlistWorkflowOutcome{kind: "refresh_missing_target"}, true
	}
	replaceCount := structuredPlaylistTrackCount(resolved.Turn, 5)
	response, pendingAction, err := s.startPlaylistRefreshPreview(ctx, playlistName, replaceCount)
	if err != nil {
		return playlistWorkflowOutcome{kind: "workflow_error", response: err.Error()}, true
	}
	return playlistWorkflowOutcome{kind: "workflow_preview", response: response, pendingAction: pendingAction}, true
}

func (s *Server) handleStructuredSavedPlaylistRepair(ctx context.Context, resolved *resolvedTurnContext, history []agent.Message) (ChatResponse, bool) {
	outcome, ok := s.resolveStructuredSavedPlaylistRepairOutcome(ctx, resolved, history)
	if !ok {
		return ChatResponse{}, false
	}
	return renderStructuredPlaylistWorkflowOutcome(outcome)
}

func (s *Server) resolveStructuredSavedPlaylistRepairOutcome(ctx context.Context, resolved *resolvedTurnContext, history []agent.Message) (playlistWorkflowOutcome, bool) {
	if resolved == nil || strings.TrimSpace(resolved.Turn.SubIntent) != "playlist_repair" {
		return playlistWorkflowOutcome{}, false
	}
	playlistName, ok := s.resolveStructuredPlaylistTarget(ctx, resolved.Turn, history)
	if !ok {
		return playlistWorkflowOutcome{kind: "repair_missing_target"}, true
	}
	response, pendingAction, err := s.startPlaylistRepairPreview(ctx, playlistName)
	if err != nil {
		return playlistWorkflowOutcome{kind: "workflow_error", response: err.Error()}, true
	}
	return playlistWorkflowOutcome{kind: "workflow_preview", response: response, pendingAction: pendingAction}, true
}

func (s *Server) handleStructuredSavedPlaylistVibe(ctx context.Context, resolved *resolvedTurnContext, history []agent.Message) (string, bool) {
	if resolved == nil || strings.TrimSpace(resolved.Turn.SubIntent) != "playlist_vibe" {
		return "", false
	}
	playlistName, ok := s.resolveStructuredPlaylistTarget(ctx, resolved.Turn, history)
	if !ok {
		return "Which playlist do you want me to inspect?", true
	}
	return s.renderSavedPlaylistVibe(ctx, playlistName)
}

func (s *Server) handleStructuredSavedPlaylistArtistCoverage(ctx context.Context, resolved *resolvedTurnContext, history []agent.Message) (string, bool) {
	if resolved == nil || strings.TrimSpace(resolved.Turn.SubIntent) != "playlist_artist_coverage" {
		return "", false
	}
	playlistName, ok := s.resolveStructuredPlaylistTarget(ctx, resolved.Turn, history)
	if !ok {
		return "Which playlist do you want me to inspect?", true
	}
	artistName := strings.TrimSpace(resolved.Turn.ArtistName)
	if artistName == "" {
		if inferredArtist, inferred := artistFromThisIsPlaylistName(playlistName); inferred {
			artistName = inferredArtist
		} else {
			return "Which artist do you want me to compare against that playlist?", true
		}
	}
	return s.renderSavedPlaylistArtistCoverage(ctx, playlistName, artistName)
}

func (s *Server) handleStructuredPlaylistAvailability(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	outcome, ok := s.resolveStructuredPlaylistAvailabilityOutcome(ctx, resolved)
	if !ok {
		return "", false
	}
	return renderStructuredPlaylistAvailability(outcome)
}

func (s *Server) resolveStructuredPlaylistAvailabilityOutcome(ctx context.Context, resolved *resolvedTurnContext) (playlistAvailabilityOutcome, bool) {
	if resolved == nil {
		return playlistAvailabilityOutcome{}, false
	}
	turn := resolved.Turn
	ref := resolved.resultReference()
	if strings.TrimSpace(turn.SubIntent) != "playlist_availability" &&
		!(ref.effectiveSetKind() == "playlist_candidates" && ref.Action == "inspect_availability") {
		return playlistAvailabilityOutcome{}, false
	}

	_, playlistName, plannedAt, planned := getLastPlannedPlaylist(chatSessionIDFromContext(ctx))
	if len(planned) == 0 || plannedAt.IsZero() || time.Since(plannedAt) > 30*time.Minute {
		return playlistAvailabilityOutcome{}, false
	}

	rawResolve, err := executeTool(ctx, s.resolver, s.embeddingsURL, "resolvePlaylistTracks", map[string]interface{}{
		"selection": "all",
	})
	if err != nil {
		return playlistAvailabilityOutcome{}, false
	}

	var parsed struct {
		Data struct {
			ResolvePlaylistTracks struct {
				PlaylistName string `json:"playlistName"`
				Counts       struct {
					Total     int `json:"total"`
					Available int `json:"available"`
					Missing   int `json:"missing"`
					Ambiguous int `json:"ambiguous"`
					Errors    int `json:"errors"`
				} `json:"counts"`
			} `json:"resolvePlaylistTracks"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(rawResolve), &parsed); err != nil {
		return playlistAvailabilityOutcome{}, false
	}

	out := parsed.Data.ResolvePlaylistTracks
	if strings.TrimSpace(out.PlaylistName) == "" {
		out.PlaylistName = playlistName
	}
	if out.Counts.Total <= 0 {
		out.Counts.Total = len(planned)
	}
	return playlistAvailabilityOutcome{
		playlistName: out.PlaylistName,
		total:        out.Counts.Total,
		available:    out.Counts.Available,
		missing:      out.Counts.Missing,
		ambiguous:    out.Counts.Ambiguous,
		errors:       out.Counts.Errors,
	}, true
}

func renderStructuredPlaylistAvailability(outcome playlistAvailabilityOutcome) (string, bool) {
	if strings.TrimSpace(outcome.playlistName) == "" || outcome.total <= 0 {
		return "", false
	}
	resp := fmt.Sprintf(
		"For %s, I found %d available, %d missing, %d ambiguous, and %d errors across %d planned tracks.",
		outcome.playlistName,
		outcome.available,
		outcome.missing,
		outcome.ambiguous,
		outcome.errors,
		outcome.total,
	)
	if outcome.available > 0 {
		resp += " Use the approval buttons if you want me to create the playlist with the available tracks."
	} else {
		resp += " Use the approval buttons if you want me to create the playlist and queue missing tracks."
	}
	return resp, true
}

func (s *Server) handleStructuredPlaylistQueueRequest(ctx context.Context, resolved *resolvedTurnContext, history []agent.Message) (ChatResponse, bool) {
	outcome, ok := s.resolveStructuredPlaylistQueueRequestOutcome(ctx, resolved, history)
	if !ok {
		return ChatResponse{}, false
	}
	return renderStructuredPlaylistWorkflowOutcome(outcome)
}

func (s *Server) resolveStructuredPlaylistQueueRequestOutcome(ctx context.Context, resolved *resolvedTurnContext, history []agent.Message) (playlistWorkflowOutcome, bool) {
	if resolved == nil || strings.TrimSpace(resolved.Turn.SubIntent) != "playlist_queue_request" {
		return playlistWorkflowOutcome{}, false
	}
	if playlistName, ok := s.resolveStructuredPlaylistTarget(ctx, resolved.Turn, history); ok {
		if resp, ok := s.renderStructuredPlaylistQueueStatus(ctx, playlistName); ok {
			return playlistWorkflowOutcome{kind: "queue_status", response: resp}, true
		}
	}
	if action := s.pendingPlaylistCreateAction(ctx, time.Time{}); action != nil {
		return playlistWorkflowOutcome{
			kind:          "queue_pending_action",
			response:      action.Summary,
			pendingAction: action,
		}, true
	}
	prompt := strings.TrimSpace(resolved.Turn.PromptHint)
	if prompt == "" && len(resolved.Turn.StyleHints) > 0 {
		prompt = strings.Join(resolved.Turn.StyleHints, " ")
	}
	if prompt != "" {
		trackCount := structuredPlaylistTrackCount(resolved.Turn, 20)
		response, pendingAction, err := s.startPlaylistCreatePreview(ctx, "", prompt, trackCount)
		if err != nil {
			return playlistWorkflowOutcome{kind: "workflow_error", response: err.Error()}, true
		}
		return playlistWorkflowOutcome{kind: "workflow_preview", response: response, pendingAction: pendingAction}, true
	}
	return playlistWorkflowOutcome{kind: "queue_missing_target_or_prompt"}, true
}

func renderStructuredPlaylistWorkflowOutcome(outcome playlistWorkflowOutcome) (ChatResponse, bool) {
	switch outcome.kind {
	case "append_missing_target":
		return ChatResponse{Response: "What playlist would you like me to update?"}, true
	case "append_missing_prompt":
		return ChatResponse{Response: "How do you want me to change that playlist?"}, true
	case "refresh_missing_target":
		return ChatResponse{Response: "What playlist would you like me to refresh?"}, true
	case "repair_missing_target":
		return ChatResponse{Response: "What playlist would you like me to repair?"}, true
	case "queue_status":
		return ChatResponse{Response: outcome.response}, true
	case "queue_pending_action":
		return ChatResponse{
			Response:      outcome.response + " Use the approval buttons if you want me to proceed.",
			PendingAction: outcome.pendingAction,
		}, true
	case "queue_missing_target_or_prompt":
		return ChatResponse{Response: "Which playlist do you want me to inspect, or what kind of playlist should I prepare first?"}, true
	case "workflow_error":
		return ChatResponse{Response: outcome.response}, true
	case "workflow_preview":
		return ChatResponse{Response: outcome.response, PendingAction: outcome.pendingAction}, true
	default:
		return ChatResponse{}, false
	}
}

func (s *Server) resolveStructuredPlaylistTarget(ctx context.Context, turn normalizedTurn, history []agent.Message) (string, bool) {
	if target := strings.TrimSpace(turn.TargetName); target != "" {
		return s.resolveSavedPlaylistReference(ctx, target, history)
	}
	if turn.ReferenceTarget == "previous_playlist" {
		return s.currentPlaylistNameFromHistory(history)
	}
	return "", false
}

func structuredPlaylistTrackCount(turn normalizedTurn, fallback int) int {
	if strings.TrimSpace(turn.SelectionMode) != "top_n" {
		return fallback
	}
	value := strings.TrimSpace(turn.SelectionValue)
	if value == "" {
		return fallback
	}
	if n, err := strconv.Atoi(value); err == nil && n > 0 && n <= 40 {
		return n
	}
	if n, ok := parseSmallNumberWord(value); ok && n > 0 && n <= 40 {
		return n
	}
	return fallback
}

func (s *Server) resolveSavedPlaylistReference(ctx context.Context, rawRef string, history []agent.Message) (string, bool) {
	ref := strings.Trim(strings.TrimSpace(rawRef), `"'?.!`)
	if ref == "" {
		return "", false
	}
	if strings.EqualFold(ref, "this playlist") || strings.EqualFold(ref, "that playlist") {
		return s.currentPlaylistNameFromHistory(history)
	}

	queries := []string{ref}
	if canonical, ok := canonicalThisIsPlaylistName(ref); ok && !strings.EqualFold(canonical, ref) {
		queries = append([]string{canonical}, queries...)
	}

	for _, query := range queries {
		rawExact, err := executeTool(ctx, s.resolver, s.embeddingsURL, "navidromePlaylist", map[string]interface{}{
			"playlistName": query,
		})
		if err == nil {
			var exact struct {
				Data struct {
					Playlist struct {
						Name string `json:"name"`
					} `json:"navidromePlaylist"`
				} `json:"data"`
			}
			if json.Unmarshal([]byte(rawExact), &exact) == nil && strings.TrimSpace(exact.Data.Playlist.Name) != "" {
				return exact.Data.Playlist.Name, true
			}
		}

		raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "navidromePlaylists", map[string]interface{}{
			"query": query,
			"limit": 5,
		})
		if err != nil {
			continue
		}
		var parsed struct {
			Data struct {
				Playlists struct {
					Items []struct {
						Name string `json:"name"`
					} `json:"playlists"`
				} `json:"navidromePlaylists"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil || len(parsed.Data.Playlists.Items) == 0 {
			continue
		}
		queryNorm := normalizeSearchTerm(query)
		for _, item := range parsed.Data.Playlists.Items {
			if normalizeSearchTerm(item.Name) == queryNorm {
				return item.Name, true
			}
		}
		if len(parsed.Data.Playlists.Items) == 1 {
			return parsed.Data.Playlists.Items[0].Name, true
		}
	}
	return "", false
}

func (s *Server) currentPlaylistNameFromHistory(history []agent.Message) (string, bool) {
	for i := len(history) - 1; i >= 0; i-- {
		content := strings.TrimSpace(history[i].Content)
		if content == "" {
			continue
		}
		if name, ok := extractPlaylistNameFromText(content); ok {
			return name, true
		}
	}
	return "", false
}

func extractPlaylistNameFromText(content string) (string, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", false
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)playlist\s+"([^"]+)"`),
		regexp.MustCompile(`(?i)playlist\s+([^\n:]+?)\s+(?:currently has|state:)`),
	}
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(content); len(match) == 2 {
			name := strings.TrimSpace(match[1])
			if name != "" {
				return name, true
			}
		}
	}
	return "", false
}

func canonicalThisIsPlaylistName(raw string) (string, bool) {
	trimmed := strings.Trim(strings.TrimSpace(raw), `"'?.!`)
	if trimmed == "" {
		return "", false
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "this is:"):
		artist := strings.TrimSpace(trimmed[len("this is:"):])
		if artist == "" {
			return "", false
		}
		return "This Is: " + strings.TrimSpace(artist), true
	case strings.HasPrefix(lower, "this is "):
		artist := strings.TrimSpace(trimmed[len("this is "):])
		if artist == "" {
			return "", false
		}
		return "This Is: " + strings.TrimSpace(artist), true
	default:
		return "", false
	}
}

func artistFromThisIsPlaylistName(playlistName string) (string, bool) {
	canonical, ok := canonicalThisIsPlaylistName(playlistName)
	if !ok {
		return "", false
	}
	artist := strings.TrimSpace(canonical[len("This Is: "):])
	if artist == "" {
		return "", false
	}
	return artist, true
}

func (s *Server) renderSavedPlaylistTracks(ctx context.Context, playlistName string) (string, bool) {
	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "navidromePlaylist", map[string]interface{}{
		"playlistName": playlistName,
	})
	if err != nil {
		return "", false
	}
	var parsed struct {
		Data struct {
			Playlist struct {
				Name   string `json:"name"`
				Tracks []struct {
					Title      string `json:"title"`
					ArtistName string `json:"artistName"`
				} `json:"tracks"`
			} `json:"navidromePlaylist"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return "", false
	}
	if len(parsed.Data.Playlist.Tracks) == 0 {
		return "", false
	}
	items := make([]string, 0, len(parsed.Data.Playlist.Tracks))
	for _, track := range parsed.Data.Playlist.Tracks {
		if strings.TrimSpace(track.Title) == "" {
			continue
		}
		entry := track.Title
		if artist := strings.TrimSpace(track.ArtistName); artist != "" {
			entry += " by " + artist
		}
		items = append(items, entry)
	}
	if len(items) == 0 {
		return "", false
	}
	return renderRouteBulletList(fmt.Sprintf("Playlist %q currently has", parsed.Data.Playlist.Name), items, 8), true
}

func (s *Server) renderStructuredPlaylistQueueStatus(ctx context.Context, playlistName string) (string, bool) {
	rawState, err := executeTool(ctx, s.resolver, s.embeddingsURL, "navidromePlaylist", map[string]interface{}{
		"playlistName": playlistName,
	})
	if err != nil {
		return "", false
	}
	var parsed struct {
		Data struct {
			State struct {
				Name   string `json:"name"`
				Counts struct {
					Saved        int `json:"saved"`
					PendingFetch int `json:"pending_fetch"`
					Total        int `json:"total"`
				} `json:"counts"`
			} `json:"navidromePlaylist"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(rawState), &parsed); err != nil {
		return "", false
	}
	state := parsed.Data.State
	if strings.TrimSpace(state.Name) == "" {
		return "", false
	}
	if state.Counts.PendingFetch > 0 {
		return fmt.Sprintf(
			"%q already has %d pending fetch track(s) queued alongside %d saved track(s). I can inspect or prune those pending items if you want.",
			state.Name,
			state.Counts.PendingFetch,
			state.Counts.Saved,
		), true
	}
	if state.Counts.Total > 0 {
		return fmt.Sprintf("%q does not currently have any pending missing tracks to queue.", state.Name), true
	}
	return fmt.Sprintf("%q is currently empty and does not have any pending missing tracks to queue.", state.Name), true
}

func (s *Server) renderSavedPlaylistVibe(ctx context.Context, playlistName string) (string, bool) {
	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "navidromePlaylist", map[string]interface{}{
		"playlistName": playlistName,
	})
	if err != nil {
		return "", false
	}
	var parsed struct {
		Data struct {
			Playlist struct {
				Name   string `json:"name"`
				Tracks []struct {
					Title      string `json:"title"`
					ArtistName string `json:"artistName"`
				} `json:"tracks"`
			} `json:"navidromePlaylist"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || len(parsed.Data.Playlist.Tracks) == 0 {
		return "", false
	}
	artistCounts := make(map[string]int)
	for _, track := range parsed.Data.Playlist.Tracks {
		artist := strings.TrimSpace(track.ArtistName)
		if artist == "" {
			continue
		}
		artistCounts[artist]++
	}
	dominantArtist := ""
	dominantCount := 0
	for artist, count := range artistCounts {
		if count > dominantCount {
			dominantArtist = artist
			dominantCount = count
		}
	}
	total := len(parsed.Data.Playlist.Tracks)
	if artistName, ok := artistFromThisIsPlaylistName(parsed.Data.Playlist.Name); ok {
		if dominantArtist == "" {
			dominantArtist = artistName
		}
		return fmt.Sprintf("The overall vibe of %q is a representative %s set: core songs, broad stylistic coverage, and very little deep-cut noise. In practice it still feels mellow, atmospheric, and slightly weightless.", parsed.Data.Playlist.Name, dominantArtist), true
	}
	if dominantArtist != "" && dominantCount >= total-1 {
		return fmt.Sprintf("The overall vibe of %q is a focused %s set: mellow, atmospheric, and slightly weightless, with a downtempo art-pop feel.", parsed.Data.Playlist.Name, dominantArtist), true
	}
	if dominantArtist != "" {
		return fmt.Sprintf("The overall vibe of %q leans mellow and atmospheric, with %s dominating the track list.", parsed.Data.Playlist.Name, dominantArtist), true
	}
	return fmt.Sprintf("The overall vibe of %q is mellow and atmospheric.", parsed.Data.Playlist.Name), true
}

func (s *Server) renderSavedPlaylistArtistCoverage(ctx context.Context, playlistName, artistName string) (string, bool) {
	rawPlaylist, err := executeTool(ctx, s.resolver, s.embeddingsURL, "navidromePlaylist", map[string]interface{}{
		"playlistName": playlistName,
	})
	if err != nil {
		return "", false
	}
	var playlistParsed struct {
		Data struct {
			Playlist struct {
				Name   string `json:"name"`
				Tracks []struct {
					Title      string `json:"title"`
					ArtistName string `json:"artistName"`
				} `json:"tracks"`
			} `json:"navidromePlaylist"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(rawPlaylist), &playlistParsed); err != nil || len(playlistParsed.Data.Playlist.Tracks) == 0 {
		return "", false
	}
	rawTracks, err := executeTool(ctx, s.resolver, s.embeddingsURL, "tracks", map[string]interface{}{
		"artistName": artistName,
		"limit":      50,
		"mostPlayed": true,
	})
	if err != nil {
		return "", false
	}
	var tracksParsed struct {
		Data struct {
			Tracks []struct {
				Title      string `json:"title"`
				ArtistName string `json:"artistName"`
			} `json:"tracks"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(rawTracks), &tracksParsed); err != nil || len(tracksParsed.Data.Tracks) == 0 {
		return "", false
	}
	inPlaylist := make(map[string]struct{}, len(playlistParsed.Data.Playlist.Tracks))
	already := make([]string, 0)
	for _, track := range playlistParsed.Data.Playlist.Tracks {
		key := normalizeSearchTerm(track.Title)
		if key != "" {
			inPlaylist[key] = struct{}{}
		}
	}
	missing := make([]string, 0)
	seen := make(map[string]struct{})
	for _, track := range tracksParsed.Data.Tracks {
		if normalizeSearchTerm(track.ArtistName) != normalizeSearchTerm(artistName) {
			continue
		}
		title := strings.TrimSpace(track.Title)
		key := normalizeSearchTerm(title)
		if title == "" || key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if _, ok := inPlaylist[key]; ok {
			already = append(already, title)
		} else {
			missing = append(missing, title)
		}
	}
	if len(already) == 0 && len(missing) == 0 {
		return "", false
	}
	if len(missing) == 0 {
		return fmt.Sprintf("This playlist already covers the main %s tracks in your library: %s.", artistName, strings.Join(already[:minInt(len(already), 5)], ", ")), true
	}
	if len(already) == 0 {
		return fmt.Sprintf("This playlist is missing several core %s tracks from your library, such as %s.", artistName, strings.Join(missing[:minInt(len(missing), 5)], ", ")), true
	}
	return fmt.Sprintf("This playlist already includes %s, but it is still missing %s.", strings.Join(already[:minInt(len(already), 4)], ", "), strings.Join(missing[:minInt(len(missing), 4)], ", ")), true
}

func extractPlaylistCreateIntent(rawMsg string) string {
	prompt := strings.ToLower(strings.TrimSpace(rawMsg))
	prompt = strings.TrimSuffix(prompt, ".")
	for _, prefix := range []string{"make me ", "make a ", "build me ", "build a ", "create me ", "create a ", "give me "} {
		if strings.HasPrefix(prompt, prefix) {
			prompt = strings.TrimSpace(strings.TrimPrefix(prompt, prefix))
			break
		}
	}
	prompt = strings.TrimSpace(strings.TrimPrefix(prompt, "a "))
	switch prompt {
	case "", "playlist", "a playlist", "a new playlist", "new playlist":
		return ""
	}
	if strings.HasSuffix(prompt, " playlist") {
		prompt = strings.TrimSpace(strings.TrimSuffix(prompt, " playlist"))
	}
	if prompt == "" || prompt == "a" || prompt == "new" {
		return ""
	}
	return prompt
}

func extractNormalizedPlaylistCreateIntent(rawMsg string) string {
	if prompt := extractPlaylistCreateIntent(rawMsg); prompt != "" {
		if isGenericPlaylistCreatePrompt(prompt) {
			return ""
		}
		return prompt
	}

	trimmed := strings.TrimSpace(rawMsg)
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "playlist") {
		return ""
	}
	for _, prefix := range []string{"spin up ", "put together ", "assemble ", "queue up "} {
		if strings.HasPrefix(lower, prefix) {
			trimmed = strings.TrimSpace(trimmed[len(prefix):])
			lower = strings.ToLower(trimmed)
			break
		}
	}
	for _, prefix := range []string{"a ", "an ", "some "} {
		if strings.HasPrefix(lower, prefix) {
			trimmed = strings.TrimSpace(trimmed[len(prefix):])
			lower = strings.ToLower(trimmed)
			break
		}
	}
	if strings.HasSuffix(lower, " playlist") {
		trimmed = strings.TrimSpace(trimmed[:len(trimmed)-len(" playlist")])
		lower = strings.ToLower(trimmed)
	}
	switch lower {
	case "", "playlist", "new playlist":
		return ""
	default:
		return trimmed
	}
}

func isGenericPlaylistCreatePrompt(prompt string) bool {
	switch strings.ToLower(strings.TrimSpace(prompt)) {
	case "spin up a", "spin up an", "put together a", "put together an", "assemble a", "assemble an", "queue up a", "queue up an":
		return true
	default:
		return false
	}
}

func extractPlaylistTrackCount(lowerMsg string, fallback int) int {
	trackCount := fallback
	if m := regexp.MustCompile(`\b(\d{1,2})\s*[- ]?track`).FindStringSubmatch(lowerMsg); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 && n <= 40 {
			trackCount = n
		}
	}
	return trackCount
}
