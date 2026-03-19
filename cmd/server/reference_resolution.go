package main

import (
	"sort"
	"strings"
	"time"
)

type referenceState struct {
	kind      string
	updatedAt time.Time
}

func resolveStructuredReference(sessionID string, resolved *resolvedTurnContext) {
	if resolved == nil {
		return
	}
	turn := &resolved.Turn
	if turn.FollowupMode == "none" && turn.ReferenceQualifier == "none" {
		return
	}

	if explicitKind := strings.TrimSpace(turn.ResultSetKind); explicitKind != "" && explicitKind != "none" {
		if referenceKindAvailable(explicitKind, resolved) {
			resolved.ResolvedReferenceKind = explicitKind
			resolved.ResolvedReferenceSource = "explicit_kind"
			resolveFocusedItem(sessionID, turn, resolved)
			return
		}
	}

	states := collectEligibleReferenceStates(sessionID, *turn, resolved)
	if len(states) == 0 {
		return
	}
	sort.SliceStable(states, func(i, j int) bool {
		if states[i].updatedAt.Equal(states[j].updatedAt) {
			return states[i].kind < states[j].kind
		}
		return states[i].updatedAt.After(states[j].updatedAt)
	})

	if shouldClarifyAmbiguousReference(*turn, states) {
		resolved.AmbiguousReference = true
		turn.NeedsClarification = true
		turn.ClarificationFocus = "reference"
		if strings.TrimSpace(turn.ClarificationPrompt) == "" {
			turn.ClarificationPrompt = "Do you mean the latest album results, discovery results, or another recent set?"
		}
		return
	}

	resolved.ResolvedReferenceKind = states[0].kind
	resolved.ResolvedReferenceSource = "resolver"
	if strings.TrimSpace(turn.ResultSetKind) == "" || turn.ResultSetKind == "none" {
		turn.ResultSetKind = states[0].kind
	}
	resolveFocusedItem(sessionID, turn, resolved)
}

func collectEligibleReferenceStates(sessionID string, turn normalizedTurn, resolved *resolvedTurnContext) []referenceState {
	sessionID = normalizeChatSessionID(sessionID)
	preferred := preferredReferenceKinds(turn)
	if len(preferred) == 0 {
		preferred = []string{
			"creative_albums",
			"semantic_albums",
			"discovered_albums",
			"cleanup_candidates",
			"badly_rated_albums",
			"playlist_candidates",
			"recent_listening",
			"scene_candidates",
			"song_path",
			"track_candidates",
			"artist_candidates",
		}
	}
	states := make([]referenceState, 0, len(preferred))
	for _, kind := range preferred {
		updatedAt, ok := latestReferenceTime(sessionID, kind, resolved)
		if !ok {
			continue
		}
		states = append(states, referenceState{kind: kind, updatedAt: updatedAt})
	}
	return states
}

func preferredReferenceKinds(turn normalizedTurn) []string {
	if kind := strings.TrimSpace(turn.ResultSetKind); kind != "" && kind != "none" {
		return []string{kind}
	}
	switch strings.TrimSpace(turn.ReferenceTarget) {
	case "previous_playlist":
		return []string{"playlist_candidates"}
	case "previous_stats":
		return []string{"recent_listening"}
	}
	switch strings.TrimSpace(turn.SubIntent) {
	case "creative_risk_pick", "creative_safe_pick":
		switch strings.TrimSpace(turn.Intent) {
		case "track_discovery":
			return []string{"track_candidates"}
		case "artist_discovery":
			return []string{"artist_candidates"}
		default:
			return []string{"creative_albums", "semantic_albums"}
		}
	case "track_similarity", "track_description":
		return []string{"track_candidates"}
	case "song_path_summary":
		return []string{"song_path"}
	case "artist_similarity", "artist_starting_album":
		return []string{"artist_candidates"}
	case "creative_refinement", "result_set_most_recent", "result_set_play_recency":
		return []string{"creative_albums", "semantic_albums"}
	case "listening_interpretation", "artist_dominance":
		return []string{"recent_listening"}
	case "lidarr_cleanup_apply":
		return []string{"cleanup_candidates"}
	case "badly_rated_cleanup":
		return []string{"badly_rated_albums"}
	}
	if strings.TrimSpace(turn.ReferenceQualifier) == "latest_set" {
		return nil
	}
	if turn.FollowupMode != "none" && turn.ReferenceTarget == "previous_results" {
		switch turn.Intent {
		case "album_discovery", "listening":
			return []string{"creative_albums", "semantic_albums", "discovered_albums"}
		case "track_discovery":
			return []string{"track_candidates"}
		case "artist_discovery":
			return []string{"artist_candidates"}
		case "playlist":
			return []string{"playlist_candidates"}
		case "stats":
			return []string{"recent_listening"}
		}
	}
	return nil
}

func latestReferenceTime(sessionID, kind string, resolved *resolvedTurnContext) (time.Time, bool) {
	switch strings.TrimSpace(kind) {
	case "creative_albums":
		if !resolved.HasCreativeAlbumSet {
			return time.Time{}, false
		}
		_, updatedAt, _, _ := getLastCreativeAlbumSet(sessionID)
		return updatedAt, !updatedAt.IsZero() && time.Since(updatedAt) <= llmContextCreativeAlbumTTL
	case "semantic_albums":
		if !resolved.HasSemanticAlbumSet {
			return time.Time{}, false
		}
		_, updatedAt, _ := getLastSemanticAlbumSearch(sessionID)
		return updatedAt, !updatedAt.IsZero() && time.Since(updatedAt) <= llmContextSemanticAlbumTTL
	case "discovered_albums":
		if !resolved.HasDiscoveredAlbums {
			return time.Time{}, false
		}
		_, updatedAt, _ := getLastDiscoveredAlbums(sessionID)
		return updatedAt, !updatedAt.IsZero() && time.Since(updatedAt) <= llmContextDiscoveredAlbumsTTL
	case "cleanup_candidates":
		if !resolved.HasCleanupCandidates {
			return time.Time{}, false
		}
		_, updatedAt := getLastLidarrCandidates(sessionID)
		return updatedAt, !updatedAt.IsZero() && time.Since(updatedAt) <= llmContextCleanupTTL
	case "badly_rated_albums":
		if !resolved.HasBadlyRatedAlbums {
			return time.Time{}, false
		}
		_, updatedAt := getLastBadlyRatedAlbums(sessionID)
		return updatedAt, !updatedAt.IsZero() && time.Since(updatedAt) <= llmContextBadlyRatedAlbumsTTL
	case "playlist_candidates":
		if !resolved.HasPendingPlaylistPlan {
			return time.Time{}, false
		}
		_, _, updatedAt, candidates := getLastPlannedPlaylist(sessionID)
		return updatedAt, len(candidates) > 0 && !updatedAt.IsZero() && time.Since(updatedAt) <= llmContextPlaylistTTL
	case "recent_listening":
		if !resolved.HasRecentListening {
			return time.Time{}, false
		}
		state, ok := getLastRecentListeningSummary(sessionID)
		return state.updatedAt, ok && !state.updatedAt.IsZero() && time.Since(state.updatedAt) <= llmContextRecentListeningTTL
	case "scene_candidates":
		if !resolved.HasResolvedScene {
			return time.Time{}, false
		}
		state, ok := getLastSceneSelection(sessionID)
		return state.UpdatedAt, ok && !state.UpdatedAt.IsZero() && time.Since(state.UpdatedAt) <= llmContextSceneTTL
	case "song_path":
		if !resolved.HasSongPath {
			return time.Time{}, false
		}
		state, ok := getLastSongPath(sessionID)
		return state.updatedAt, ok && !state.updatedAt.IsZero() && time.Since(state.updatedAt) <= llmContextSongPathTTL
	case "track_candidates":
		if !resolved.HasTrackCandidates {
			return time.Time{}, false
		}
		_, updatedAt, _, _ := getLastTrackCandidateSet(sessionID)
		return updatedAt, !updatedAt.IsZero() && time.Since(updatedAt) <= llmContextRecentListeningTTL
	case "artist_candidates":
		if !resolved.HasArtistCandidates {
			return time.Time{}, false
		}
		_, updatedAt, _ := getLastArtistCandidateSet(sessionID)
		return updatedAt, !updatedAt.IsZero() && time.Since(updatedAt) <= llmContextRecentListeningTTL
	default:
		return time.Time{}, false
	}
}

func shouldClarifyAmbiguousReference(turn normalizedTurn, states []referenceState) bool {
	if len(states) < 2 {
		return false
	}
	if strings.TrimSpace(turn.ResultSetKind) != "" && turn.ResultSetKind != "none" {
		return false
	}
	if strings.TrimSpace(turn.ReferenceTarget) == "previous_playlist" || strings.TrimSpace(turn.ReferenceTarget) == "previous_stats" {
		return false
	}
	delta := states[0].updatedAt.Sub(states[1].updatedAt)
	if delta < 0 {
		delta = -delta
	}
	return states[0].kind != states[1].kind && delta <= 2*time.Second
}

func referenceKindAvailable(kind string, resolved *resolvedTurnContext) bool {
	switch strings.TrimSpace(kind) {
	case "creative_albums":
		return resolved.HasCreativeAlbumSet
	case "semantic_albums":
		return resolved.HasSemanticAlbumSet
	case "discovered_albums":
		return resolved.HasDiscoveredAlbums
	case "cleanup_candidates":
		return resolved.HasCleanupCandidates
	case "badly_rated_albums":
		return resolved.HasBadlyRatedAlbums
	case "playlist_candidates":
		return resolved.HasPendingPlaylistPlan
	case "recent_listening":
		return resolved.HasRecentListening
	case "scene_candidates":
		return resolved.HasResolvedScene
	case "song_path":
		return resolved.HasSongPath
	case "track_candidates":
		return resolved.HasTrackCandidates
	case "artist_candidates":
		return resolved.HasArtistCandidates
	default:
		return false
	}
}

func resolveFocusedItem(sessionID string, turn *normalizedTurn, resolved *resolvedTurnContext) {
	if turn == nil || resolved == nil {
		return
	}
	if strings.TrimSpace(turn.ReferenceQualifier) != "last_item" {
		return
	}
	state, ok := getLastFocusedResultItem(sessionID)
	if !ok || state.updatedAt.IsZero() {
		return
	}
	if strings.TrimSpace(resolved.ResolvedReferenceKind) == "" {
		return
	}
	if state.kind != resolved.ResolvedReferenceKind {
		return
	}
	if ttl := focusedResultItemTTL(state.kind); ttl > 0 && time.Since(state.updatedAt) > ttl {
		return
	}
	resolved.ResolvedItemKey = state.key
	resolved.ResolvedItemSource = "focused_item"
}

func focusedResultItemTTL(kind string) time.Duration {
	switch strings.TrimSpace(kind) {
	case "creative_albums":
		return llmContextCreativeAlbumTTL
	case "semantic_albums":
		return llmContextSemanticAlbumTTL
	case "discovered_albums":
		return llmContextDiscoveredAlbumsTTL
	case "cleanup_candidates":
		return llmContextCleanupTTL
	case "badly_rated_albums":
		return llmContextBadlyRatedAlbumsTTL
	case "playlist_candidates":
		return llmContextPlaylistTTL
	case "recent_listening":
		return llmContextRecentListeningTTL
	case "scene_candidates":
		return llmContextSceneTTL
	case "track_candidates":
		return llmContextRecentListeningTTL
	case "artist_candidates":
		return llmContextRecentListeningTTL
	default:
		return 0
	}
}
