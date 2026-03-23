package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const llmContextDiscoveredAlbumsTTL = 30 * time.Minute
const llmContextCleanupTTL = 20 * time.Minute
const llmContextPlaylistTTL = 30 * time.Minute
const llmContextSemanticAlbumTTL = 30 * time.Minute
const llmContextCreativeAlbumTTL = 30 * time.Minute
const llmContextRecentListeningTTL = 30 * time.Minute
const llmContextSongPathTTL = 30 * time.Minute

func (s *Server) buildLLMSessionContext(sessionID string) string {
	sessionID = normalizeChatSessionID(sessionID)
	now := time.Now().UTC()
	turnMemory := loadTurnSessionMemory(sessionID)

	sections := make([]string, 0, 5)
	if memory, ok := s.latestChatSessionMemory(sessionID); ok {
		if section := formatStructuredChatMemory(memory, now); section != "" {
			sections = append(sections, section)
		}
	}
	if actionSection := formatPendingActionContext(s.latestPendingAction(sessionID), now); actionSection != "" {
		sections = append(sections, actionSection)
	}
	if prompt, playlistName, plannedAt, candidates, resolvedAt, resolved, ok := turnMemory.PlaylistContext(); ok {
		if section := formatPlaylistContext(prompt, playlistName, plannedAt, candidates, resolvedAt, resolved, now); section != "" {
			sections = append(sections, section)
		}
	}
	if matches, updatedAt, queryText, ok := turnMemory.SemanticAlbumSearch(); ok && updatedAt != (time.Time{}) && strings.TrimSpace(queryText) != "" {
		if section := formatSemanticAlbumSearchContext(queryText, updatedAt, matches, now); section != "" {
			sections = append(sections, section)
		}
	}
	if candidates, updatedAt, mode, queryText, ok := turnMemory.CreativeAlbumSet(); ok {
		if section := formatCreativeAlbumSetContext(mode, queryText, updatedAt, candidates, now); section != "" {
			sections = append(sections, section)
		}
	}
	if candidates, updatedAt, query, ok := turnMemory.DiscoveredAlbums(); ok {
		if section := formatDiscoveredAlbumsContext(query, updatedAt, candidates, now); section != "" {
			sections = append(sections, section)
		}
	}
	if state, ok := turnMemory.RecentListeningSummary(); ok {
		if section := formatRecentListeningContext(state, now); section != "" {
			sections = append(sections, section)
		}
	}
	if state, ok := turnMemory.SceneSelection(); ok {
		if section := formatSceneSelectionContext(state, now); section != "" {
			sections = append(sections, section)
		}
	}
	if state, ok := turnMemory.SongPath(); ok {
		if section := formatSongPathContext(state, now); section != "" {
			sections = append(sections, section)
		}
	}
	if candidates, updatedAt, mode, queryText, ok := turnMemory.TrackCandidateSet(); ok {
		if section := formatTrackCandidateContext(mode, queryText, updatedAt, candidates, now); section != "" {
			sections = append(sections, section)
		}
	}
	if candidates, updatedAt, queryText, ok := turnMemory.ArtistCandidateSet(); ok {
		if section := formatArtistCandidateContext(queryText, updatedAt, candidates, now); section != "" {
			sections = append(sections, section)
		}
	}
	if section := formatFocusedResultItemContext(turnMemory, now); section != "" {
		sections = append(sections, section)
	}
	if candidates, updatedAt, ok := turnMemory.CleanupCandidates(); ok {
		if section := formatLidarrCleanupContext(updatedAt, candidates, now); section != "" {
			sections = append(sections, section)
		}
	}
	if candidates, updatedAt, ok := turnMemory.BadlyRatedAlbums(); ok {
		if section := formatBadlyRatedAlbumsContext(updatedAt, candidates, now); section != "" {
			sections = append(sections, section)
		}
	}
	if len(sections) == 0 {
		return ""
	}
	return "Server session state (authoritative cached facts for this chat):\n" + strings.Join(sections, "\n")
}

func formatPendingActionContext(action *PendingAction, now time.Time) string {
	if action == nil {
		return ""
	}
	expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(action.ExpiresAt))
	if err != nil || now.After(expiresAt) {
		return ""
	}
	parts := []string{
		fmt.Sprintf("pending_action: kind=%s", strings.TrimSpace(action.Kind)),
		fmt.Sprintf("title=%q", strings.TrimSpace(action.Title)),
		fmt.Sprintf("summary=%q", strings.TrimSpace(action.Summary)),
	}
	if len(action.Details) > 0 {
		parts = append(parts, fmt.Sprintf("details=%q", strings.Join(action.Details, " | ")))
	}
	return strings.Join(parts, "; ")
}

func formatDiscoveredAlbumsContext(query string, updatedAt time.Time, candidates []discoveredAlbumCandidate, now time.Time) string {
	if len(candidates) == 0 || updatedAt.IsZero() || now.Sub(updatedAt) > llmContextDiscoveredAlbumsTTL {
		return ""
	}
	sample := make([]string, 0, minInt(len(candidates), 3))
	for _, candidate := range candidates {
		if len(sample) >= 3 {
			break
		}
		name := strings.TrimSpace(candidate.AlbumTitle)
		if artist := strings.TrimSpace(candidate.ArtistName); artist != "" {
			name += " by " + artist
		}
		if candidate.Year > 0 {
			name += fmt.Sprintf(" (%d)", candidate.Year)
		}
		sample = append(sample, name)
	}
	return fmt.Sprintf(
		"last_discovered_albums: query=%q; count=%d; sample=%q",
		strings.TrimSpace(query),
		len(candidates),
		strings.Join(sample, " | "),
	)
}

func formatSemanticAlbumSearchContext(queryText string, updatedAt time.Time, matches []semanticAlbumSearchMatch, now time.Time) string {
	if updatedAt.IsZero() || now.Sub(updatedAt) > llmContextSemanticAlbumTTL || strings.TrimSpace(queryText) == "" {
		return ""
	}

	sample := make([]string, 0, minInt(len(matches), 4))
	recent := make([]string, 0, 3)
	for _, match := range matches {
		label := strings.TrimSpace(match.Name)
		if label == "" {
			continue
		}
		if artist := strings.TrimSpace(match.ArtistName); artist != "" {
			label += " by " + artist
		}
		if match.Year > 0 {
			label += fmt.Sprintf(" (%d)", match.Year)
		}
		if len(sample) < 4 {
			sample = append(sample, label)
		}
		if len(recent) >= 3 {
			continue
		}
		if strings.TrimSpace(match.LastPlayed) == "" && match.PlayCount <= 0 {
			continue
		}
		detail := label
		if match.PlayCount > 0 {
			detail += fmt.Sprintf(" [plays=%d", match.PlayCount)
			if strings.TrimSpace(match.LastPlayed) != "" {
				detail += fmt.Sprintf(", last_played=%s", strings.TrimSpace(match.LastPlayed))
			}
			detail += "]"
		} else if strings.TrimSpace(match.LastPlayed) != "" {
			detail += fmt.Sprintf(" [last_played=%s]", strings.TrimSpace(match.LastPlayed))
		}
		recent = append(recent, detail)
	}

	parts := []string{
		fmt.Sprintf("last_semantic_album_search: query=%q", strings.TrimSpace(queryText)),
		fmt.Sprintf("count=%d", len(matches)),
	}
	if len(sample) > 0 {
		parts = append(parts, fmt.Sprintf("sample=%q", strings.Join(sample, " | ")))
	}
	if len(recent) > 0 {
		parts = append(parts, fmt.Sprintf("recent_matches=%q", strings.Join(recent, " | ")))
	}
	return strings.Join(parts, "; ")
}

func formatCreativeAlbumSetContext(mode, queryText string, updatedAt time.Time, candidates []creativeAlbumCandidate, now time.Time) string {
	if len(candidates) == 0 || updatedAt.IsZero() || now.Sub(updatedAt) > llmContextCreativeAlbumTTL {
		return ""
	}
	sample := make([]string, 0, minInt(len(candidates), 4))
	recent := make([]string, 0, 3)
	for _, candidate := range candidates {
		label := strings.TrimSpace(candidate.Name)
		if label == "" {
			continue
		}
		if artist := strings.TrimSpace(candidate.ArtistName); artist != "" {
			label += " by " + artist
		}
		if candidate.Year > 0 {
			label += fmt.Sprintf(" (%d)", candidate.Year)
		}
		if len(sample) < 4 {
			sample = append(sample, label)
		}
		if len(recent) >= 3 {
			continue
		}
		if candidate.PlayCount <= 0 && strings.TrimSpace(candidate.LastPlayed) == "" {
			continue
		}
		detail := label
		if candidate.PlayCount > 0 {
			detail += fmt.Sprintf(" [plays=%d", candidate.PlayCount)
			if strings.TrimSpace(candidate.LastPlayed) != "" {
				detail += fmt.Sprintf(", last_played=%s", strings.TrimSpace(candidate.LastPlayed))
			}
			detail += "]"
		} else if strings.TrimSpace(candidate.LastPlayed) != "" {
			detail += fmt.Sprintf(" [last_played=%s]", strings.TrimSpace(candidate.LastPlayed))
		}
		recent = append(recent, detail)
	}
	parts := []string{
		fmt.Sprintf("last_creative_album_set: mode=%q", strings.TrimSpace(mode)),
		fmt.Sprintf("count=%d", len(candidates)),
	}
	if strings.TrimSpace(queryText) != "" {
		parts = append(parts, fmt.Sprintf("query=%q", strings.TrimSpace(queryText)))
	}
	if len(sample) > 0 {
		parts = append(parts, fmt.Sprintf("sample=%q", strings.Join(sample, " | ")))
	}
	if len(recent) > 0 {
		parts = append(parts, fmt.Sprintf("play_context=%q", strings.Join(recent, " | ")))
	}
	return strings.Join(parts, "; ")
}

func formatRecentListeningContext(state recentListeningState, now time.Time) string {
	if state.updatedAt.IsZero() || now.Sub(state.updatedAt) > llmContextRecentListeningTTL {
		return ""
	}
	parts := []string{
		fmt.Sprintf("last_recent_listening: total_plays=%d", state.totalPlays),
		fmt.Sprintf("tracks_heard=%d", state.tracksHeard),
		fmt.Sprintf("artists_heard=%d", state.artistsHeard),
	}
	if strings.TrimSpace(state.windowStart) != "" && strings.TrimSpace(state.windowEnd) != "" {
		parts = append(parts, fmt.Sprintf("window=%s..%s", strings.TrimSpace(state.windowStart), strings.TrimSpace(state.windowEnd)))
	}
	if len(state.topArtists) > 0 {
		items := make([]string, 0, minInt(len(state.topArtists), 5))
		for _, item := range state.topArtists {
			if strings.TrimSpace(item.ArtistName) == "" {
				continue
			}
			items = append(items, fmt.Sprintf("%s:%d", item.ArtistName, item.TrackCount))
			if len(items) >= 5 {
				break
			}
		}
		if len(items) > 0 {
			parts = append(parts, fmt.Sprintf("top_artists=%q", strings.Join(items, " | ")))
		}
	}
	if len(state.topTracks) > 0 {
		items := make([]string, 0, minInt(len(state.topTracks), 3))
		for _, item := range state.topTracks {
			title := strings.TrimSpace(item.Title)
			if title == "" {
				continue
			}
			entry := title
			if artist := strings.TrimSpace(item.ArtistName); artist != "" {
				entry += " by " + artist
			}
			if item.PlayCount > 0 {
				entry += fmt.Sprintf(" [%d]", item.PlayCount)
			}
			items = append(items, entry)
			if len(items) >= 3 {
				break
			}
		}
		if len(items) > 0 {
			parts = append(parts, fmt.Sprintf("top_tracks=%q", strings.Join(items, " | ")))
		}
	}
	return strings.Join(parts, "; ")
}

func formatSongPathContext(state songPathState, now time.Time) string {
	if state.updatedAt.IsZero() || now.Sub(state.updatedAt) > llmContextSongPathTTL || len(state.path) == 0 {
		return ""
	}
	middle := state.path[len(state.path)/2]
	parts := []string{
		fmt.Sprintf("last_song_path: count=%d", len(state.path)),
		fmt.Sprintf("start=%q", formatSongPathTrack(state.start)),
		fmt.Sprintf("end=%q", formatSongPathTrack(state.end)),
		fmt.Sprintf("middle=%q", formatSongPathTrack(middle)),
	}
	if len(state.path) > 2 {
		sample := []string{formatSongPathTrack(state.path[0]), formatSongPathTrack(middle), formatSongPathTrack(state.path[len(state.path)-1])}
		parts = append(parts, fmt.Sprintf("sample=%q", strings.Join(sample, " | ")))
	}
	return strings.Join(parts, "; ")
}

func formatSongPathTrack(track songPathTrack) string {
	label := strings.TrimSpace(track.Title)
	if artist := strings.TrimSpace(track.ArtistName); artist != "" {
		label += " by " + artist
	}
	if album := strings.TrimSpace(track.AlbumName); album != "" {
		label += " [" + album + "]"
	}
	return strings.TrimSpace(label)
}

func formatTrackCandidateContext(mode, queryText string, updatedAt time.Time, candidates []trackCandidate, now time.Time) string {
	if len(candidates) == 0 || updatedAt.IsZero() || now.Sub(updatedAt) > llmContextRecentListeningTTL {
		return ""
	}
	sample := make([]string, 0, minInt(len(candidates), 4))
	for _, candidate := range candidates {
		if len(sample) >= 4 {
			break
		}
		label := formatTrackCandidate(candidate)
		if label == "" {
			continue
		}
		sample = append(sample, label)
	}
	parts := []string{
		fmt.Sprintf("last_track_candidates: count=%d", len(candidates)),
		fmt.Sprintf("mode=%q", strings.TrimSpace(mode)),
	}
	if strings.TrimSpace(queryText) != "" {
		parts = append(parts, fmt.Sprintf("query=%q", strings.TrimSpace(queryText)))
	}
	if len(sample) > 0 {
		parts = append(parts, fmt.Sprintf("sample=%q", strings.Join(sample, " | ")))
	}
	return strings.Join(parts, "; ")
}

func formatArtistCandidateContext(queryText string, updatedAt time.Time, candidates []artistCandidate, now time.Time) string {
	if len(candidates) == 0 || updatedAt.IsZero() || now.Sub(updatedAt) > llmContextRecentListeningTTL {
		return ""
	}
	sample := make([]string, 0, minInt(len(candidates), 4))
	for _, candidate := range candidates {
		if len(sample) >= 4 {
			break
		}
		label := strings.TrimSpace(candidate.Name)
		if label == "" {
			continue
		}
		sample = append(sample, label)
	}
	parts := []string{
		fmt.Sprintf("last_artist_candidates: count=%d", len(candidates)),
	}
	if strings.TrimSpace(queryText) != "" {
		parts = append(parts, fmt.Sprintf("query=%q", strings.TrimSpace(queryText)))
	}
	if len(sample) > 0 {
		parts = append(parts, fmt.Sprintf("sample=%q", strings.Join(sample, " | ")))
	}
	return strings.Join(parts, "; ")
}

func formatFocusedResultItemContext(memory turnSessionMemory, now time.Time) string {
	state, ok := memory.FocusedResultItem()
	if !ok {
		return ""
	}
	kind := strings.TrimSpace(state.kind)
	if kind == "" || strings.TrimSpace(state.key) == "" || state.updatedAt.IsZero() {
		return ""
	}
	ttl := focusedResultItemTTL(kind)
	if ttl <= 0 || now.Sub(state.updatedAt) > ttl {
		return ""
	}
	label := memory.FocusedResultItemLabel()
	parts := []string{
		fmt.Sprintf("focused_result_item: kind=%q", kind),
		fmt.Sprintf("key=%q", strings.TrimSpace(state.key)),
	}
	if label != "" {
		parts = append(parts, fmt.Sprintf("label=%q", label))
	}
	return strings.Join(parts, "; ")
}

func formatLidarrCleanupContext(updatedAt time.Time, candidates []lidarrCleanupCandidate, now time.Time) string {
	if len(candidates) == 0 || updatedAt.IsZero() || now.Sub(updatedAt) > llmContextCleanupTTL {
		return ""
	}
	reasonCounts := make(map[string]int)
	sample := make([]string, 0, minInt(len(candidates), 3))
	for _, candidate := range candidates {
		reason := strings.TrimSpace(candidate.Reason)
		if reason != "" {
			reasonCounts[reason]++
		}
		if len(sample) < 3 {
			label := strings.TrimSpace(candidate.Title)
			if artist := strings.TrimSpace(candidate.ArtistName); artist != "" {
				label += " by " + artist
			}
			sample = append(sample, label)
		}
	}
	reasons := make([]string, 0, len(reasonCounts))
	for reason, count := range reasonCounts {
		reasons = append(reasons, fmt.Sprintf("%s:%d", reason, count))
	}
	sort.Strings(reasons)
	return fmt.Sprintf(
		"last_cleanup_preview: count=%d; recommended_action=%s; reasons=%q; sample=%q",
		len(candidates),
		recommendedLidarrCleanupAction(candidates),
		strings.Join(reasons, ", "),
		strings.Join(sample, " | "),
	)
}

func formatPlaylistContext(prompt, playlistName string, plannedAt time.Time, candidates []playlistCandidateTrack, resolvedAt time.Time, resolved []resolvedPlaylistTrack, now time.Time) string {
	if len(candidates) == 0 || plannedAt.IsZero() || now.Sub(plannedAt) > llmContextPlaylistTTL {
		return ""
	}

	name := strings.TrimSpace(playlistName)
	if name == "" {
		name = "Discover: Mixed"
	}
	sample := make([]string, 0, minInt(len(candidates), 3))
	for _, candidate := range candidates {
		if len(sample) >= 3 {
			break
		}
		sample = append(sample, fmt.Sprintf("%s - %s", strings.TrimSpace(candidate.ArtistName), strings.TrimSpace(candidate.TrackTitle)))
	}

	parts := []string{
		fmt.Sprintf("last_playlist_plan: name=%q", name),
		fmt.Sprintf("prompt=%q", strings.TrimSpace(prompt)),
		fmt.Sprintf("planned_tracks=%d", len(candidates)),
		fmt.Sprintf("sample=%q", strings.Join(sample, " | ")),
	}

	if !resolvedAt.IsZero() && now.Sub(resolvedAt) <= llmContextPlaylistTTL && len(resolved) > 0 {
		available := 0
		missing := 0
		ambiguous := 0
		errors := 0
		for _, item := range resolved {
			switch item.Status {
			case "available":
				available++
			case "missing":
				missing++
			case "ambiguous":
				ambiguous++
			default:
				errors++
			}
		}
		parts = append(parts, fmt.Sprintf("resolved_counts=%d/%d/%d/%d", available, missing, ambiguous, errors))
	}

	return strings.Join(parts, "; ")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
