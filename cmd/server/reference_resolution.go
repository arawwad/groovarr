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

func resolveStructuredReference(memory turnSessionMemory, resolved *resolvedTurnContext) {
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
			resolveFocusedItem(memory, turn, resolved)
			return
		}
	}

	states := collectEligibleReferenceStates(memory, *turn, resolved)
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
	if strings.TrimSpace(turn.ResultSetKind) == "" || turn.ResultSetKind == "none" || !referenceKindAvailable(turn.ResultSetKind, resolved) {
		turn.ResultSetKind = states[0].kind
	}
	resolveFocusedItem(memory, turn, resolved)
}

func collectEligibleReferenceStates(memory turnSessionMemory, turn normalizedTurn, resolved *resolvedTurnContext) []referenceState {
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
	states := collectReferenceStatesForKinds(memory, preferred, resolved)
	if len(states) == 0 && turn.ReferenceTarget == "previous_results" && strings.TrimSpace(turn.ResultSetKind) != "" && turn.ResultSetKind != "none" {
		fallbackTurn := turn
		fallbackTurn.ResultSetKind = ""
		fallbackPreferred := preferredReferenceKinds(fallbackTurn)
		if len(fallbackPreferred) > 0 {
			states = collectReferenceStatesForKinds(memory, fallbackPreferred, resolved)
		}
	}
	return states
}

func collectReferenceStatesForKinds(memory turnSessionMemory, kinds []string, resolved *resolvedTurnContext) []referenceState {
	states := make([]referenceState, 0, len(kinds))
	for _, kind := range kinds {
		updatedAt, ok := latestReferenceTime(memory, kind, resolved)
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
		return []string{"track_candidates", "song_path"}
	case "song_path_summary":
		return []string{"song_path"}
	case "artist_similarity", "artist_starting_album":
		return []string{"artist_candidates"}
	case "creative_refinement", "result_set_most_recent", "result_set_play_recency":
		return []string{"creative_albums", "semantic_albums", "artist_candidates"}
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
			return []string{"creative_albums", "semantic_albums", "discovered_albums", "artist_candidates"}
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

func latestReferenceTime(memory turnSessionMemory, kind string, resolved *resolvedTurnContext) (time.Time, bool) {
	if !referenceKindAvailable(kind, resolved) {
		return time.Time{}, false
	}
	return memory.LatestReferenceTime(kind)
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
	if equivalentReferenceKinds(states[0].kind, states[1].kind) {
		return false
	}
	delta := states[0].updatedAt.Sub(states[1].updatedAt)
	if delta < 0 {
		delta = -delta
	}
	return states[0].kind != states[1].kind && delta <= 2*time.Second
}

func equivalentReferenceKinds(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == right {
		return true
	}
	return (left == "creative_albums" && right == "semantic_albums") ||
		(left == "semantic_albums" && right == "creative_albums")
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

func resolveFocusedItem(memory turnSessionMemory, turn *normalizedTurn, resolved *resolvedTurnContext) {
	if turn == nil || resolved == nil {
		return
	}
	if strings.TrimSpace(turn.ReferenceQualifier) != "last_item" {
		return
	}
	if strings.TrimSpace(resolved.ResolvedReferenceKind) == "" {
		return
	}
	key, ok := memory.ResolveFocusedItem(resolved.ResolvedReferenceKind)
	if !ok {
		return
	}
	resolved.ResolvedItemKey = key
	resolved.ResolvedItemSource = "focused_item"
}

func focusedResultItemTTL(kind string) time.Duration {
	return referenceKindTTL(kind)
}
