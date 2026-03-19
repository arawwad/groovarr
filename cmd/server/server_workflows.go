package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"groovarr/internal/discovery"

	"github.com/rs/zerolog/log"
)

type playlistPreviewData struct {
	Response          string                   `json:"response"`
	PendingAction     *PendingAction           `json:"pendingAction,omitempty"`
	PlaylistName      string                   `json:"playlistName,omitempty"`
	Mode              string                   `json:"mode,omitempty"`
	SourceSummary     map[string]interface{}   `json:"sourceSummary,omitempty"`
	ConstraintSummary map[string]interface{}   `json:"constraintSummary,omitempty"`
	Counts            map[string]int           `json:"counts,omitempty"`
	Tracks            []map[string]interface{} `json:"tracks,omitempty"`
	Notes             []string                 `json:"notes,omitempty"`
}

type playlistRepairIssue struct {
	Index  int
	Entry  navidromePlaylistEntry
	Reason string
}

func (s *Server) startArtistRemovalPreview(ctx context.Context, target string) (string, *PendingAction, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", nil, fmt.Errorf("artistName is required")
	}

	client, err := newLidarrClientFromEnv()
	if err != nil {
		return fmt.Sprintf("I couldn't remove %q from Lidarr: %v", target, err), nil, nil
	}

	artist, err := client.FindExistingArtist(ctx, target)
	if err != nil {
		return fmt.Sprintf("I couldn't check Lidarr for %q: %v", target, err), nil, nil
	}
	if artist == nil || artist.ID == 0 {
		return fmt.Sprintf("I couldn't find %q in Lidarr.", target), nil, nil
	}

	action := s.registerPendingActionForContext(
		ctx,
		"artist_remove",
		"Remove artist",
		fmt.Sprintf("Remove %q from Lidarr.", artist.ArtistName),
		[]string{fmt.Sprintf("Artist: %s", artist.ArtistName)},
		func(runCtx context.Context) (string, error) {
			if err := client.DeleteArtist(runCtx, artist.ID); err != nil {
				return "", fmt.Errorf("couldn't remove %q from Lidarr: %w", artist.ArtistName, err)
			}
			localArtist, localErr := s.resolver.DB.GetArtistByName(runCtx, artist.ArtistName)
			if localErr == nil && localArtist != nil && strings.TrimSpace(localArtist.ID) != "" {
				if err := s.resolver.DB.DeleteArtist(runCtx, localArtist.ID); err != nil {
					log.Warn().Err(err).Str("artist", artist.ArtistName).Msg("Failed to remove local artist cache after Lidarr delete")
				}
			}
			s.publishEvent("library", fmt.Sprintf("Removed artist %q from Lidarr.", artist.ArtistName))
			return fmt.Sprintf("Removed %q from Lidarr.", artist.ArtistName), nil
		},
	)
	return fmt.Sprintf("I found %q in Lidarr. Use the approval buttons if you want me to remove it.", artist.ArtistName), action, nil
}

func (s *Server) startDiscoveredAlbumsApplyPreview(ctx context.Context, selection string) (string, *PendingAction, error) {
	sessionID := chatSessionIDFromContext(ctx)
	action, selectedCount, ok := s.buildDiscoveredAlbumsPendingAction(ctx, selection, time.Time{})
	if !ok {
		candidates, updatedAt, _ := getLastDiscoveredAlbums(sessionID)
		if len(candidates) == 0 || updatedAt.IsZero() || time.Since(updatedAt) > 30*time.Minute {
			return "I don't have a recent discovered album list in this chat yet. Ask me to discover albums first.", nil, nil
		}
		if strings.TrimSpace(selection) != "" && !strings.EqualFold(strings.TrimSpace(selection), "all") {
			return fmt.Sprintf("I couldn't match %q against the most recent discovered albums in this chat.", selection), nil, nil
		}
		return "I couldn't start the album apply preview from the current discovery state.", nil, nil
	}
	return fmt.Sprintf("I’m ready to apply library actions for %d discovered album(s). Use the approval buttons if you want me to proceed.", selectedCount), action, nil
}

func (s *Server) startLidarrCleanupApplyPreview(ctx context.Context, action, selection string) (string, *PendingAction, error) {
	sessionID := chatSessionIDFromContext(ctx)
	pendingAction, selectedCount, resolvedAction, ok := s.buildLidarrCleanupPendingAction(ctx, action, selection, time.Time{})
	if !ok {
		candidates, updatedAt := getLastLidarrCandidates(sessionID)
		if len(candidates) == 0 || updatedAt.IsZero() || time.Since(updatedAt) > 20*time.Minute {
			return "I don't have a recent cleanup preview in this chat yet. Ask me to preview library cleanup first.", nil, nil
		}
		if strings.TrimSpace(selection) != "" && !strings.EqualFold(strings.TrimSpace(selection), "all") {
			return fmt.Sprintf("I couldn't match %q against the most recent cleanup candidates in this chat.", selection), nil, nil
		}
		return "I couldn't start the cleanup apply preview from the current cleanup state.", nil, nil
	}
	return fmt.Sprintf("I’m ready to apply cleanup action %q to %d album(s). Use the approval buttons if you want me to proceed.", resolvedAction, selectedCount), pendingAction, nil
}

func (s *Server) executeBadlyRatedAlbumsCleanupApproval(ctx context.Context, updatedAt time.Time, selection string) (string, error) {
	candidates, cachedAt := getLastBadlyRatedAlbums(chatSessionIDFromContext(ctx))
	if len(candidates) == 0 || cachedAt.IsZero() {
		return "", fmt.Errorf("badly rated album preview is no longer available")
	}
	if cachedAt.UnixNano() != updatedAt.UnixNano() {
		return "", fmt.Errorf("badly rated album preview changed")
	}
	if time.Since(cachedAt) > llmContextBadlyRatedAlbumsTTL {
		return "", fmt.Errorf("badly rated album preview expired")
	}
	selected, err := selectBadlyRatedAlbums(selection, candidates)
	if err != nil {
		return "", err
	}

	dedupeKey := fmt.Sprintf("%d|%s", cachedAt.UnixNano(), selection)
	resp, handled, err := s.runWorkflowWithDedupe(ctx, "lidarr_badly_rated_cleanup", dedupeKey, func(runCtx context.Context) (string, bool, error) {
		client, err := newLidarrClientFromEnv()
		if err != nil {
			return "", false, err
		}
		lidarrAlbums, err := client.GetAlbums(runCtx)
		if err != nil {
			return "", false, err
		}
		matched, ambiguous, missing := matchBadlyRatedAlbumsInLidarr(selected, lidarrAlbums)
		if len(matched) == 0 {
			return "I couldn't find exact Lidarr album matches for those badly rated albums, so I didn't delete anything.", true, nil
		}

		okCount := 0
		failures := make([]string, 0)
		for _, item := range matched {
			if err := client.DeleteAlbum(runCtx, item.LidarrID); err != nil {
				failures = append(failures, fmt.Sprintf("%s by %s (%v)", item.Candidate.AlbumName, item.Candidate.ArtistName, err))
				continue
			}
			okCount++
		}

		parts := []string{
			fmt.Sprintf("Deleted %d badly rated album(s) from Lidarr.", okCount),
		}
		if len(failures) > 0 {
			parts = append(parts, fmt.Sprintf("%d failed: %s", len(failures), strings.Join(failures, "; ")))
		}
		if len(missing) > 0 {
			parts = append(parts, fmt.Sprintf("%d not found in Lidarr", len(missing)))
		}
		if len(ambiguous) > 0 {
			parts = append(parts, fmt.Sprintf("%d ambiguous and skipped", len(ambiguous)))
		}
		s.publishEvent("lidarr", fmt.Sprintf("Badly rated album cleanup complete: %d deleted, %d failed.", okCount, len(failures)))
		return strings.Join(parts, " "), true, nil
	})
	if err != nil {
		log.Warn().Err(err).Str("workflow", "lidarr_badly_rated_cleanup").Msg("Workflow execution failed")
		return "", err
	}
	if !handled {
		return "", fmt.Errorf("badly rated album cleanup workflow did not apply")
	}
	return resp, nil
}

func makePlaylistPreviewTracks(resolved []resolvedPlaylistTrack) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(resolved))
	for _, item := range resolved {
		items = append(items, map[string]interface{}{
			"rank":          item.Rank,
			"artistName":    item.ArtistName,
			"trackTitle":    item.TrackTitle,
			"status":        item.Status,
			"songId":        item.SongID,
			"matchedArtist": item.MatchedArtist,
			"matchedTitle":  item.MatchedTitle,
			"matchCount":    item.MatchCount,
			"detail":        item.Detail,
		})
	}
	return items
}

func makePlaylistReplacePreviewTracks(entries []navidromePlaylistEntry, resolved []resolvedPlaylistTrack) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(entries)+len(resolved))
	for _, entry := range entries {
		items = append(items, map[string]interface{}{
			"artistName": entry.Artist,
			"trackTitle": entry.Title,
			"status":     "replace_candidate",
		})
	}
	return append(items, makePlaylistPreviewTracks(resolved)...)
}

func makePlaylistRepairPreviewTracks(issues []playlistRepairIssue, resolved []resolvedPlaylistTrack) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(issues)+len(resolved))
	for _, issue := range issues {
		items = append(items, map[string]interface{}{
			"artistName": issue.Entry.Artist,
			"trackTitle": issue.Entry.Title,
			"status":     "repair_candidate",
			"detail":     issue.Reason,
		})
	}
	return append(items, makePlaylistPreviewTracks(resolved)...)
}

func playlistSampleLabels(entries []navidromePlaylistEntry, limit int) []string {
	if limit <= 0 {
		limit = 6
	}
	labels := make([]string, 0, limit)
	for _, entry := range entries {
		title := strings.TrimSpace(entry.Title)
		artist := strings.TrimSpace(entry.Artist)
		if title == "" && artist == "" {
			continue
		}
		if artist != "" && title != "" {
			labels = append(labels, fmt.Sprintf("%s - %s", artist, title))
		} else if artist != "" {
			labels = append(labels, artist)
		} else {
			labels = append(labels, title)
		}
		if len(labels) >= limit {
			break
		}
	}
	return labels
}

func buildPlaylistContextPrompt(prefix string, entries []navidromePlaylistEntry, replacements int) string {
	samples := playlistSampleLabels(entries, 6)
	sampleText := strings.Join(samples, "; ")
	if sampleText == "" {
		sampleText = "keep the current playlist character intact"
	}
	return fmt.Sprintf("%s Use this playlist as context: %s. Generate %d replacement tracks that fit naturally and avoid duplicates.", prefix, sampleText, replacements)
}

func buildPlaylistAppendPrompt(playlistName, prompt string, entries []navidromePlaylistEntry, additions int) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "append tracks that fit this playlist naturally"
	}
	if additions <= 0 {
		additions = 5
	}
	samples := playlistSampleLabels(entries, 4)
	if len(samples) == 0 {
		return prompt
	}
	return fmt.Sprintf(
		"%s Append to playlist %q after this recent run: %s. Keep continuity with that tail and avoid artist clumping across the next %d tracks.",
		prompt,
		playlistName,
		strings.Join(samples, "; "),
		additions,
	)
}

func buildPlaylistRefreshPrompt(playlistName string, entries []navidromePlaylistEntry, replaceIndexes []int, removalEntries []navidromePlaylistEntry) string {
	base := buildPlaylistContextPrompt(
		fmt.Sprintf("Refresh playlist %q while keeping its overall character and sequencing momentum intact.", playlistName),
		entries,
		len(removalEntries),
	)
	if len(replaceIndexes) == 0 || len(removalEntries) == 0 {
		return base
	}
	slotNotes := make([]string, 0, len(replaceIndexes))
	for i, idx := range replaceIndexes {
		if i >= len(removalEntries) || idx < 0 || idx >= len(entries) {
			continue
		}
		note := fmt.Sprintf("slot %d replaces %s - %s", idx+1, removalEntries[i].Artist, removalEntries[i].Title)
		if idx > 0 {
			prev := entries[idx-1]
			note += fmt.Sprintf(", after %s - %s", prev.Artist, prev.Title)
		}
		if idx+1 < len(entries) {
			next := entries[idx+1]
			note += fmt.Sprintf(", before %s - %s", next.Artist, next.Title)
		}
		slotNotes = append(slotNotes, note)
	}
	if len(slotNotes) == 0 {
		return base
	}
	return fmt.Sprintf("%s Treat these as in-sequence replacement slots: %s.", base, strings.Join(slotNotes, "; "))
}

func selectPlaylistRefreshEntries(entries []navidromePlaylistEntry, replaceCount int) ([]int, []navidromePlaylistEntry) {
	if replaceCount <= 0 {
		return nil, nil
	}
	type refreshCandidate struct {
		index int
		entry navidromePlaylistEntry
		score int
	}
	artistCounts := make(map[string]int, len(entries))
	trackCounts := make(map[string]int, len(entries))
	for _, entry := range entries {
		if artistKey := normalizeSearchTerm(entry.Artist); artistKey != "" {
			artistCounts[artistKey]++
		}
		if trackKey := normalizedPlaylistTrackKey(entry.Artist, entry.Title); trackKey != "" {
			trackCounts[trackKey]++
		}
	}
	candidates := make([]refreshCandidate, 0, len(entries))
	for i, entry := range entries {
		score := 0
		artistKey := normalizeSearchTerm(entry.Artist)
		trackKey := normalizedPlaylistTrackKey(entry.Artist, entry.Title)
		if n := artistCounts[artistKey]; artistKey != "" && n > 1 {
			score += (n - 1) * 3
		}
		if n := trackCounts[trackKey]; trackKey != "" && n > 1 {
			score += (n - 1) * 6
		}
		if i > 0 && artistKey != "" && normalizeSearchTerm(entries[i-1].Artist) == artistKey {
			score += 4
		}
		if i+1 < len(entries) && artistKey != "" && normalizeSearchTerm(entries[i+1].Artist) == artistKey {
			score += 4
		}
		if i > 1 && artistKey != "" && normalizeSearchTerm(entries[i-2].Artist) == artistKey {
			score += 2
		}
		if i+2 < len(entries) && artistKey != "" && normalizeSearchTerm(entries[i+2].Artist) == artistKey {
			score += 2
		}
		if runLength := playlistArtistRunLength(entries, i); runLength > 1 {
			score += (runLength - 1) * 3
		}
		if i > 0 && trackKey != "" && normalizedPlaylistTrackKey(entries[i-1].Artist, entries[i-1].Title) == trackKey {
			score += 8
		}
		if i+1 < len(entries) && trackKey != "" && normalizedPlaylistTrackKey(entries[i+1].Artist, entries[i+1].Title) == trackKey {
			score += 8
		}
		if len(entries) > 4 && i >= len(entries)/2 {
			score++
		}
		if len(entries) > 6 && i >= (len(entries)*3)/4 {
			score++
		}
		switch i {
		case 0:
			score -= 5
		case 1:
			score -= 2
		}
		if i == len(entries)-1 {
			score -= 2
		}
		if score <= 0 {
			continue
		}
		candidates = append(candidates, refreshCandidate{index: i, entry: entry, score: score})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].index > candidates[j].index
		}
		return candidates[i].score > candidates[j].score
	})
	indexes := make([]int, 0, replaceCount)
	selected := make([]navidromePlaylistEntry, 0, replaceCount)
	chosen := make(map[int]struct{}, replaceCount)
	for _, candidate := range candidates {
		if len(indexes) >= replaceCount {
			break
		}
		chosen[candidate.index] = struct{}{}
		indexes = append(indexes, candidate.index)
		selected = append(selected, candidate.entry)
	}
	if len(indexes) < replaceCount {
		for i := len(entries) - 2; i >= 2 && len(indexes) < replaceCount; i-- {
			if _, ok := chosen[i]; ok {
				continue
			}
			if _, ok := chosen[i-1]; ok {
				continue
			}
			chosen[i] = struct{}{}
			indexes = append(indexes, i)
		}
	}
	sort.Ints(indexes)
	ordered := make([]navidromePlaylistEntry, 0, len(indexes))
	for _, idx := range indexes {
		ordered = append(ordered, entries[idx])
	}
	return indexes, ordered
}

func buildOrderedPlaylistSongIDs(entries []navidromePlaylistEntry, replaceIndexes []int, replacementSongIDs []string) ([]string, bool) {
	if len(replaceIndexes) == 0 {
		return nil, false
	}
	replacements := make(map[int]string, len(replaceIndexes))
	removed := make(map[int]struct{}, len(replaceIndexes))
	for i, idx := range replaceIndexes {
		removed[idx] = struct{}{}
		if i >= len(replacementSongIDs) {
			break
		}
		songID := strings.TrimSpace(replacementSongIDs[i])
		if songID == "" {
			return nil, false
		}
		replacements[idx] = songID
	}
	finalIDs := make([]string, 0, len(entries)-len(replaceIndexes)+len(replacements))
	for i, entry := range entries {
		if replacementID, ok := replacements[i]; ok {
			finalIDs = append(finalIDs, replacementID)
			continue
		}
		if _, ok := removed[i]; ok {
			continue
		}
		existingID := strings.TrimSpace(entry.ID)
		if existingID == "" {
			return nil, false
		}
		finalIDs = append(finalIDs, existingID)
	}
	return finalIDs, true
}

func playlistEntryIndexes(entries []navidromePlaylistEntry) []int {
	indexes := make([]int, 0, len(entries))
	for i := range entries {
		indexes = append(indexes, i)
	}
	return indexes
}

func recentPlaylistArtistKeys(entries []navidromePlaylistEntry, limit int) []string {
	if limit <= 0 {
		limit = 3
	}
	keys := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)
	for i := len(entries) - 1; i >= 0 && len(keys) < limit; i-- {
		key := normalizeSearchTerm(entries[i].Artist)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for i, j := 0, len(keys)-1; i < j; i, j = i+1, j-1 {
		keys[i], keys[j] = keys[j], keys[i]
	}
	return keys
}

func playlistArtistWindow(entries []navidromePlaylistEntry, endExclusive, limit int) []string {
	if limit <= 0 {
		limit = 4
	}
	if endExclusive > len(entries) {
		endExclusive = len(entries)
	}
	if endExclusive < 0 {
		endExclusive = 0
	}
	keys := make([]string, 0, limit)
	for i := endExclusive - 1; i >= 0 && len(keys) < limit; i-- {
		key := normalizeSearchTerm(entries[i].Artist)
		if key == "" {
			continue
		}
		keys = append(keys, key)
	}
	for i, j := 0, len(keys)-1; i < j; i, j = i+1, j-1 {
		keys[i], keys[j] = keys[j], keys[i]
	}
	return keys
}

func playlistNextArtistKey(entries []navidromePlaylistEntry, start int, skipped map[int]struct{}) string {
	if start < 0 {
		start = 0
	}
	for i := start; i < len(entries); i++ {
		if _, ok := skipped[i]; ok {
			continue
		}
		if key := normalizeSearchTerm(entries[i].Artist); key != "" {
			return key
		}
	}
	return ""
}

func playlistArtistRunLength(entries []navidromePlaylistEntry, index int) int {
	if index < 0 || index >= len(entries) {
		return 0
	}
	artistKey := normalizeSearchTerm(entries[index].Artist)
	if artistKey == "" {
		return 0
	}
	run := 1
	for i := index - 1; i >= 0; i-- {
		if normalizeSearchTerm(entries[i].Artist) != artistKey {
			break
		}
		run++
	}
	for i := index + 1; i < len(entries); i++ {
		if normalizeSearchTerm(entries[i].Artist) != artistKey {
			break
		}
		run++
	}
	return run
}

func sequencePlaylistAvailableAdditions(resolved []resolvedPlaylistTrack, availableSongIDs []string, contextEntries []navidromePlaylistEntry) ([]string, []resolvedPlaylistTrack) {
	if len(availableSongIDs) <= 1 {
		ordered := make([]resolvedPlaylistTrack, 0, len(resolved))
		allowed := make(map[string]struct{}, len(availableSongIDs))
		for _, id := range availableSongIDs {
			allowed[strings.TrimSpace(id)] = struct{}{}
		}
		for _, item := range resolved {
			if _, ok := allowed[strings.TrimSpace(item.SongID)]; ok {
				ordered = append(ordered, item)
			}
		}
		return append([]string(nil), availableSongIDs...), ordered
	}
	type additionCandidate struct {
		item        resolvedPlaylistTrack
		songID      string
		artistKey   string
		originalIdx int
	}
	allowed := make(map[string]struct{}, len(availableSongIDs))
	for _, id := range availableSongIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			allowed[trimmed] = struct{}{}
		}
	}
	candidates := make([]additionCandidate, 0, len(availableSongIDs))
	for idx, item := range resolved {
		songID := strings.TrimSpace(item.SongID)
		if _, ok := allowed[songID]; !ok {
			continue
		}
		artistKey := normalizeSearchTerm(item.ArtistName)
		if artistKey == "" {
			artistKey = normalizeSearchTerm(item.MatchedArtist)
		}
		candidates = append(candidates, additionCandidate{
			item:        item,
			songID:      songID,
			artistKey:   artistKey,
			originalIdx: idx,
		})
	}
	recentArtists := recentPlaylistArtistKeys(contextEntries, 4)
	selectedArtistCounts := make(map[string]int, len(candidates))
	remainingArtistCounts := make(map[string]int, len(candidates))
	for _, candidate := range candidates {
		if candidate.artistKey != "" {
			remainingArtistCounts[candidate.artistKey]++
		}
	}
	orderedIDs := make([]string, 0, len(candidates))
	orderedTracks := make([]resolvedPlaylistTrack, 0, len(candidates))
	for len(candidates) > 0 {
		bestIdx := 0
		bestScore := 1 << 30
		for i, candidate := range candidates {
			score := candidate.originalIdx * 4
			if len(recentArtists) > 0 && candidate.artistKey != "" {
				last := recentArtists[len(recentArtists)-1]
				if candidate.artistKey == last {
					score += 200
				}
				if len(recentArtists) > 1 && candidate.artistKey == recentArtists[len(recentArtists)-2] {
					score += 80
				}
				for j := 0; j < len(recentArtists)-2; j++ {
					if candidate.artistKey == recentArtists[j] {
						score += 25
						break
					}
				}
			}
			if candidate.artistKey != "" {
				score += selectedArtistCounts[candidate.artistKey] * 90
				if remaining := remainingArtistCounts[candidate.artistKey]; remaining > 1 {
					score += (remaining - 1) * 12
				}
			}
			if score < bestScore {
				bestScore = score
				bestIdx = i
			}
		}
		chosen := candidates[bestIdx]
		orderedIDs = append(orderedIDs, chosen.songID)
		orderedTracks = append(orderedTracks, chosen.item)
		if chosen.artistKey != "" {
			selectedArtistCounts[chosen.artistKey]++
			remainingArtistCounts[chosen.artistKey]--
			recentArtists = append(recentArtists, chosen.artistKey)
			if len(recentArtists) > 4 {
				recentArtists = recentArtists[len(recentArtists)-4:]
			}
		}
		candidates = append(candidates[:bestIdx], candidates[bestIdx+1:]...)
	}
	return orderedIDs, orderedTracks
}

func sequencePlaylistSlotReplacements(resolved []resolvedPlaylistTrack, availableSongIDs []string, entries []navidromePlaylistEntry, replaceIndexes []int) ([]string, []resolvedPlaylistTrack) {
	if len(replaceIndexes) == 0 || len(availableSongIDs) <= 1 {
		return sequencePlaylistAvailableAdditions(resolved, availableSongIDs, entries)
	}
	type replacementCandidate struct {
		item        resolvedPlaylistTrack
		songID      string
		artistKey   string
		originalIdx int
	}
	allowed := make(map[string]struct{}, len(availableSongIDs))
	for _, id := range availableSongIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			allowed[trimmed] = struct{}{}
		}
	}
	candidates := make([]replacementCandidate, 0, len(availableSongIDs))
	for idx, item := range resolved {
		songID := strings.TrimSpace(item.SongID)
		if _, ok := allowed[songID]; !ok {
			continue
		}
		artistKey := normalizeSearchTerm(item.ArtistName)
		if artistKey == "" {
			artistKey = normalizeSearchTerm(item.MatchedArtist)
		}
		candidates = append(candidates, replacementCandidate{
			item:        item,
			songID:      songID,
			artistKey:   artistKey,
			originalIdx: idx,
		})
	}
	if len(candidates) <= 1 {
		return sequencePlaylistAvailableAdditions(resolved, availableSongIDs, entries)
	}
	skipped := make(map[int]struct{}, len(replaceIndexes))
	for _, idx := range replaceIndexes {
		skipped[idx] = struct{}{}
	}
	orderedIDs := make([]string, 0, len(candidates))
	orderedTracks := make([]resolvedPlaylistTrack, 0, len(candidates))
	selectedArtistCounts := make(map[string]int, len(candidates))
	remainingArtistCounts := make(map[string]int, len(candidates))
	for _, candidate := range candidates {
		if candidate.artistKey != "" {
			remainingArtistCounts[candidate.artistKey]++
		}
	}
	for slotIdx, replaceIndex := range replaceIndexes {
		if len(candidates) == 0 {
			break
		}
		bestIdx := 0
		bestScore := 1 << 30
		prevArtists := playlistArtistWindow(entries, replaceIndex, 3)
		nextArtist := playlistNextArtistKey(entries, replaceIndex+1, skipped)
		if slotIdx > 0 {
			if lastArtist := normalizeSearchTerm(orderedTracks[len(orderedTracks)-1].ArtistName); lastArtist != "" {
				prevArtists = append(prevArtists, lastArtist)
			}
		}
		for i, candidate := range candidates {
			score := candidate.originalIdx * 4
			if candidate.artistKey != "" {
				if len(prevArtists) > 0 {
					last := prevArtists[len(prevArtists)-1]
					if candidate.artistKey == last {
						score += 200
					}
					if len(prevArtists) > 1 && candidate.artistKey == prevArtists[len(prevArtists)-2] {
						score += 80
					}
					for j := 0; j < len(prevArtists)-2; j++ {
						if candidate.artistKey == prevArtists[j] {
							score += 25
							break
						}
					}
				}
				if nextArtist != "" && candidate.artistKey == nextArtist {
					score += 70
				}
				score += selectedArtistCounts[candidate.artistKey] * 90
				if remaining := remainingArtistCounts[candidate.artistKey]; remaining > 1 {
					score += (remaining - 1) * 12
				}
			}
			if score < bestScore {
				bestScore = score
				bestIdx = i
			}
		}
		chosen := candidates[bestIdx]
		orderedIDs = append(orderedIDs, chosen.songID)
		orderedTracks = append(orderedTracks, chosen.item)
		if chosen.artistKey != "" {
			selectedArtistCounts[chosen.artistKey]++
			remainingArtistCounts[chosen.artistKey]--
		}
		candidates = append(candidates[:bestIdx], candidates[bestIdx+1:]...)
	}
	return orderedIDs, orderedTracks
}

func combinePlaylistPreviewTracks(orderedAvailable []resolvedPlaylistTrack, resolved []resolvedPlaylistTrack) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(resolved))
	availableIDs := make(map[string]struct{}, len(orderedAvailable))
	for _, item := range orderedAvailable {
		availableIDs[strings.TrimSpace(item.SongID)] = struct{}{}
	}
	items = append(items, makePlaylistPreviewTracks(orderedAvailable)...)
	for _, item := range resolved {
		if _, ok := availableIDs[strings.TrimSpace(item.SongID)]; ok {
			continue
		}
		items = append(items, makePlaylistPreviewTracks([]resolvedPlaylistTrack{item})...)
	}
	return items
}

func buildPlaylistRepairPrompt(playlistName string, entries []navidromePlaylistEntry, issues []playlistRepairIssue) string {
	replacements := len(issues)
	if replacements <= 0 {
		replacements = 1
	}
	base := buildPlaylistContextPrompt(
		fmt.Sprintf("Repair playlist %q by fixing broken or duplicate entries while keeping the same feel and sequence shape.", playlistName),
		entries,
		replacements,
	)
	slotNotes := make([]string, 0, len(issues))
	for _, issue := range issues {
		if issue.Index < 0 || issue.Index >= len(entries) {
			continue
		}
		note := fmt.Sprintf("slot %d repairs %s - %s because %s", issue.Index+1, issue.Entry.Artist, issue.Entry.Title, issue.Reason)
		if issue.Index > 0 {
			prev := entries[issue.Index-1]
			note += fmt.Sprintf(", after %s - %s", prev.Artist, prev.Title)
		}
		if issue.Index+1 < len(entries) {
			next := entries[issue.Index+1]
			note += fmt.Sprintf(", before %s - %s", next.Artist, next.Title)
		}
		slotNotes = append(slotNotes, note)
	}
	if len(slotNotes) == 0 {
		return base
	}
	return fmt.Sprintf("%s Treat these as in-sequence repair slots: %s.", base, strings.Join(slotNotes, "; "))
}

func selectPlaylistRepairIssues(entries []navidromePlaylistEntry) []playlistRepairIssue {
	seenIDs := make(map[string]struct{}, len(entries))
	seenTracks := make(map[string]struct{}, len(entries))
	issues := make([]playlistRepairIssue, 0)
	for i, entry := range entries {
		id := strings.TrimSpace(entry.ID)
		artist := strings.TrimSpace(entry.Artist)
		title := strings.TrimSpace(entry.Title)
		key := normalizedPlaylistTrackKey(entry.Artist, entry.Title)
		switch {
		case id == "" && key == "":
			issues = append(issues, playlistRepairIssue{
				Index:  i,
				Entry:  entry,
				Reason: "the entry is missing both a track id and usable artist/title metadata",
			})
			continue
		case id == "":
			issues = append(issues, playlistRepairIssue{
				Index:  i,
				Entry:  entry,
				Reason: "the entry is missing a track id",
			})
			continue
		case artist == "" || title == "":
			issues = append(issues, playlistRepairIssue{
				Index:  i,
				Entry:  entry,
				Reason: "the entry has incomplete artist/title metadata",
			})
			continue
		}
		if _, ok := seenIDs[id]; ok {
			issues = append(issues, playlistRepairIssue{
				Index:  i,
				Entry:  entry,
				Reason: "it duplicates an earlier saved track id in the playlist",
			})
			continue
		}
		seenIDs[id] = struct{}{}
		if key == "" {
			continue
		}
		if _, ok := seenTracks[key]; ok {
			issues = append(issues, playlistRepairIssue{
				Index:  i,
				Entry:  entry,
				Reason: "it duplicates an earlier saved artist/title pair in the playlist",
			})
			continue
		}
		seenTracks[key] = struct{}{}
	}
	return issues
}

func playlistRepairIssueIndexes(issues []playlistRepairIssue) []int {
	indexes := make([]int, 0, len(issues))
	for _, issue := range issues {
		indexes = append(indexes, issue.Index)
	}
	return indexes
}

func playlistRepairIssueEntries(issues []playlistRepairIssue) []navidromePlaylistEntry {
	entries := make([]navidromePlaylistEntry, 0, len(issues))
	for _, issue := range issues {
		entries = append(entries, issue.Entry)
	}
	return entries
}

func playlistRepairReasonCounts(issues []playlistRepairIssue) map[string]int {
	counts := make(map[string]int)
	for _, issue := range issues {
		counts[issue.Reason]++
	}
	return counts
}

func collectPlaylistCandidateSets(
	resolved []resolvedPlaylistTrack,
	existingSongIDs map[string]struct{},
	existingTrackKeys map[string]struct{},
	pendingKeys map[string]struct{},
) ([]string, []resolvedPlaylistTrack, int, int, int) {
	availableSongIDs := make([]string, 0, len(resolved))
	availableSeen := make(map[string]struct{}, len(resolved))
	missingTracks := make([]resolvedPlaylistTrack, 0, len(resolved))
	missingSeen := make(map[string]struct{}, len(resolved))
	skippedExisting := 0
	ambiguous := 0
	errors := 0
	for _, item := range resolved {
		key := normalizedPlaylistTrackKey(item.ArtistName, item.TrackTitle)
		switch item.Status {
		case "available":
			if strings.TrimSpace(item.SongID) == "" {
				continue
			}
			if _, ok := existingSongIDs[item.SongID]; ok {
				skippedExisting++
				continue
			}
			if key != "" {
				if _, ok := existingTrackKeys[key]; ok {
					skippedExisting++
					continue
				}
				if _, ok := pendingKeys[key]; ok {
					skippedExisting++
					continue
				}
			}
			if _, ok := availableSeen[item.SongID]; ok {
				continue
			}
			availableSeen[item.SongID] = struct{}{}
			availableSongIDs = append(availableSongIDs, item.SongID)
		case "missing":
			if key == "" {
				continue
			}
			if _, ok := existingTrackKeys[key]; ok {
				skippedExisting++
				continue
			}
			if _, ok := pendingKeys[key]; ok {
				skippedExisting++
				continue
			}
			if _, ok := missingSeen[key]; ok {
				continue
			}
			missingSeen[key] = struct{}{}
			missingTracks = append(missingTracks, item)
		case "ambiguous":
			ambiguous++
		default:
			errors++
		}
	}
	return availableSongIDs, missingTracks, skippedExisting, ambiguous, errors
}

func buildPlaylistEntrySets(entries []navidromePlaylistEntry) (map[string]struct{}, map[string]struct{}) {
	existingSongIDs := make(map[string]struct{}, len(entries))
	existingTrackKeys := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if id := strings.TrimSpace(entry.ID); id != "" {
			existingSongIDs[id] = struct{}{}
		}
		if key := normalizedPlaylistTrackKey(entry.Artist, entry.Title); key != "" {
			existingTrackKeys[key] = struct{}{}
		}
	}
	return existingSongIDs, existingTrackKeys
}

func (s *Server) buildPlaylistCreatePreview(ctx context.Context, playlistName, prompt string, trackCount int) (playlistPreviewData, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return playlistPreviewData{}, fmt.Errorf("prompt is required")
	}
	if trackCount <= 0 {
		trackCount = 20
	}
	if trackCount > 40 {
		trackCount = 40
	}
	client, err := newNavidromeClientFromEnv()
	if err != nil {
		return playlistPreviewData{}, err
	}
	playlistName = strings.TrimSpace(playlistName)
	existingPlaylist, err := client.GetPlaylistByName(ctx, playlistName)
	if err != nil {
		return playlistPreviewData{}, err
	}
	var existingDetail *navidromePlaylistDetail
	if existingPlaylist != nil {
		existingDetail, err = client.GetPlaylist(ctx, existingPlaylist.ID)
		if err != nil {
			return playlistPreviewData{}, err
		}
	}

	planPrompt := prompt
	if existingDetail != nil && len(existingDetail.Entries) > 0 {
		planPrompt = buildPlaylistAppendPrompt(existingDetail.Name, prompt, existingDetail.Entries, trackCount)
	}
	candidates, meta, err := planDiscoverPlaylist(ctx, map[string]interface{}{
		"prompt":       planPrompt,
		"trackCount":   trackCount,
		"playlistName": playlistName,
		"personalized": true,
	})
	if err != nil {
		return playlistPreviewData{}, err
	}
	resolved, err := resolvePlaylistCandidates(ctx, client, candidates)
	if err != nil {
		return playlistPreviewData{}, err
	}

	existingSongIDs := map[string]struct{}{}
	existingTrackKeys := map[string]struct{}{}
	pendingKeys := map[string]struct{}{}
	contextEntries := []navidromePlaylistEntry(nil)
	if existingDetail != nil {
		existingSongIDs, existingTrackKeys = buildPlaylistEntrySets(existingDetail.Entries)
		pendingKeys, err = pendingPlaylistTrackKeysForPlaylist(existingDetail.Name)
		if err != nil {
			return playlistPreviewData{}, err
		}
		contextEntries = existingDetail.Entries
	}
	availableSongIDs, missingTracks, skippedExisting, ambiguous, errors := collectPlaylistCandidateSets(resolved, existingSongIDs, existingTrackKeys, pendingKeys)
	orderedSongIDs, orderedAvailableTracks := sequencePlaylistAvailableAdditions(resolved, availableSongIDs, contextEntries)
	if len(orderedSongIDs) > 0 {
		availableSongIDs = orderedSongIDs
	}
	plannedName := strings.TrimSpace(fmt.Sprintf("%v", meta["playlistName"]))
	if plannedName == "" {
		plannedName = playlistName
	}
	if plannedName == "" {
		plannedName = "Discover: Mixed"
	}
	if len(availableSongIDs) == 0 && len(missingTracks) == 0 {
		return playlistPreviewData{
			Response:     fmt.Sprintf("I couldn’t find any usable playlist additions for %q. The proposed tracks were already present, already pending, or unresolved.", plannedName),
			PlaylistName: plannedName,
			Mode:         "create",
			SourceSummary: map[string]interface{}{
				"prompt":           prompt,
				"normalizedIntent": meta["normalizedIntent"],
			},
			ConstraintSummary: map[string]interface{}{},
			Counts: map[string]int{
				"planned":         len(candidates),
				"availableNow":    0,
				"missing":         0,
				"ambiguous":       ambiguous,
				"skippedExisting": skippedExisting,
				"toApply":         0,
				"errors":          errors,
			},
			Tracks: makePlaylistPreviewTracks(resolved),
		}, nil
	}

	details := []string{
		fmt.Sprintf("Playlist: %s", plannedName),
		fmt.Sprintf("Planned tracks: %d", len(candidates)),
		fmt.Sprintf("Ready to add now: %d", len(availableSongIDs)),
		fmt.Sprintf("Queue for fetch: %d", len(missingTracks)),
		fmt.Sprintf("Prompt: %s", prompt),
	}
	if len(availableSongIDs) > 1 {
		details = append(details, "Sequence: anti-clumped direct additions")
	}
	if skippedExisting > 0 {
		details = append(details, fmt.Sprintf("Skipped existing/pending: %d", skippedExisting))
	}
	if ambiguous > 0 {
		details = append(details, fmt.Sprintf("Ambiguous: %d", ambiguous))
	}
	action := s.registerPendingActionForContext(
		ctx,
		"playlist_create",
		"Create playlist",
		fmt.Sprintf("Create or update playlist %q with %d direct track(s) and queue %d missing match(es) if needed.", plannedName, len(availableSongIDs), len(missingTracks)),
		details,
		func(runCtx context.Context) (string, error) {
			client, err := newNavidromeClientFromEnv()
			if err != nil {
				return "", err
			}
			actionName, added, _, err := upsertPlaylistSongs(runCtx, client, plannedName, availableSongIDs)
			if err != nil {
				return "", err
			}
			queued := 0
			if len(missingTracks) > 0 {
				queued, _, _, err = enqueuePlaylistReconcileTracks(plannedName, missingTracks)
				if err != nil {
					return "", err
				}
			}
			response := fmt.Sprintf("Playlist %q %s. Added %d track(s) from your library.", plannedName, actionName, added)
			if queued > 0 {
				response += fmt.Sprintf(" Queued %d missing track(s) for the download agent.", queued)
			}
			if skippedExisting > 0 {
				response += fmt.Sprintf(" Skipped %d track(s) that were already present or pending.", skippedExisting)
			}
			s.publishEvent("playlist", fmt.Sprintf("Playlist %q create preview applied: %d added, %d queued.", plannedName, added, queued))
			return response, nil
		},
	)
	notes := []string{"This uses the existing plan, resolve, and playlist apply flow underneath."}
	if existingDetail != nil {
		notes = append(notes, "An existing playlist with this name will be updated instead of replaced.")
		notes = append(notes, "Direct additions are re-sequenced against the existing playlist tail to reduce artist clumping.")
	} else if len(availableSongIDs) > 1 {
		notes = append(notes, "Direct additions are re-sequenced to reduce artist clumping before approval.")
	}
	return playlistPreviewData{
		Response:      fmt.Sprintf("I prepared %d direct track(s) and %d queued track(s) for %q. Use the approval buttons if you want me to create or update it.", len(availableSongIDs), len(missingTracks), plannedName),
		PendingAction: action,
		PlaylistName:  plannedName,
		Mode:          "create",
		SourceSummary: map[string]interface{}{
			"prompt":           prompt,
			"normalizedIntent": meta["normalizedIntent"],
			"personalized":     true,
		},
		ConstraintSummary: map[string]interface{}{},
		Counts: map[string]int{
			"planned":         len(candidates),
			"availableNow":    len(availableSongIDs),
			"missing":         len(missingTracks),
			"ambiguous":       ambiguous,
			"skippedExisting": skippedExisting,
			"toApply":         len(availableSongIDs) + len(missingTracks),
			"errors":          errors,
		},
		Tracks: combinePlaylistPreviewTracks(orderedAvailableTracks, resolved),
		Notes:  notes,
	}, nil
}

func (s *Server) buildPlaylistAppendPreview(ctx context.Context, playlistName, prompt string, trackCount int) (playlistPreviewData, error) {
	playlistName = strings.TrimSpace(playlistName)
	prompt = strings.TrimSpace(prompt)
	if playlistName == "" {
		return playlistPreviewData{}, fmt.Errorf("playlistName is required")
	}
	if prompt == "" {
		return playlistPreviewData{}, fmt.Errorf("prompt is required")
	}
	if trackCount <= 0 {
		trackCount = 5
	}
	if trackCount > 20 {
		trackCount = 20
	}

	client, err := newNavidromeClientFromEnv()
	if err != nil {
		return playlistPreviewData{}, err
	}
	playlist, err := resolveNavidromePlaylist(ctx, client, "", playlistName)
	if err != nil {
		return playlistPreviewData{}, err
	}

	planPrompt := buildPlaylistAppendPrompt(playlist.Name, prompt, playlist.Entries, trackCount)
	candidates, meta, err := planDiscoverPlaylist(ctx, map[string]interface{}{
		"prompt":       planPrompt,
		"trackCount":   trackCount,
		"playlistName": playlist.Name,
		"personalized": true,
	})
	if err != nil {
		return playlistPreviewData{}, err
	}
	resolved, err := resolvePlaylistCandidates(ctx, client, candidates)
	if err != nil {
		return playlistPreviewData{}, err
	}

	existingSongIDs, existingTrackKeys := buildPlaylistEntrySets(playlist.Entries)
	pendingKeys, err := pendingPlaylistTrackKeysForPlaylist(playlist.Name)
	if err != nil {
		return playlistPreviewData{}, err
	}
	availableSongIDs, missingTracks, skippedExisting, ambiguous, errors := collectPlaylistCandidateSets(resolved, existingSongIDs, existingTrackKeys, pendingKeys)
	availableSongIDs, orderedAvailableTracks := sequencePlaylistAvailableAdditions(resolved, availableSongIDs, playlist.Entries)

	if len(availableSongIDs) == 0 && len(missingTracks) == 0 {
		return playlistPreviewData{
			Response:          fmt.Sprintf("I couldn’t find any new track additions for %q. The proposed tracks were already present, already pending, or unresolved.", playlist.Name),
			PlaylistName:      playlist.Name,
			Mode:              "append",
			SourceSummary:     map[string]interface{}{"prompt": prompt, "normalizedIntent": meta["normalizedIntent"]},
			ConstraintSummary: map[string]interface{}{},
			Counts: map[string]int{
				"planned":         len(candidates),
				"availableNow":    0,
				"missing":         0,
				"ambiguous":       ambiguous,
				"skippedExisting": skippedExisting,
				"toApply":         0,
				"errors":          errors,
			},
			Tracks: makePlaylistPreviewTracks(resolved),
		}, nil
	}

	details := []string{
		fmt.Sprintf("Playlist: %s", playlist.Name),
		fmt.Sprintf("Planned additions: %d", len(candidates)),
		fmt.Sprintf("Ready to add now: %d", len(availableSongIDs)),
		fmt.Sprintf("Queue for fetch: %d", len(missingTracks)),
	}
	if len(availableSongIDs) > 1 {
		details = append(details, "Sequence: anti-clumped direct additions")
	}
	if skippedExisting > 0 {
		details = append(details, fmt.Sprintf("Skipped existing/pending: %d", skippedExisting))
	}
	if ambiguous > 0 {
		details = append(details, fmt.Sprintf("Ambiguous: %d", ambiguous))
	}
	if prompt != "" {
		details = append(details, fmt.Sprintf("Prompt: %s", prompt))
	}

	action := s.registerPendingActionForContext(
		ctx,
		"playlist_append",
		"Update playlist",
		fmt.Sprintf("Add %d new track(s) to playlist %q and queue missing matches if needed.", len(availableSongIDs)+len(missingTracks), playlist.Name),
		details,
		func(runCtx context.Context) (string, error) {
			client, err := newNavidromeClientFromEnv()
			if err != nil {
				return "", err
			}
			added := 0
			if len(availableSongIDs) > 0 {
				if _, added, _, err = upsertPlaylistSongs(runCtx, client, playlist.Name, availableSongIDs); err != nil {
					return "", err
				}
			}
			queued := 0
			if len(missingTracks) > 0 {
				var enqueueErr error
				queued, _, _, enqueueErr = enqueuePlaylistReconcileTracks(playlist.Name, missingTracks)
				if enqueueErr != nil {
					return "", enqueueErr
				}
				if queued > 0 {
					triggerPlaylistReconcile()
				}
			}

			response := fmt.Sprintf("Playlist %q updated. Added %d new track(s) from your library.", playlist.Name, added)
			if skippedExisting > 0 {
				response += fmt.Sprintf(" Skipped %d track(s) that were already in the playlist or already pending.", skippedExisting)
			}
			if queued > 0 {
				response += fmt.Sprintf(" Queued %d missing track(s) for the download agent.", queued)
			}
			if ambiguous > 0 {
				response += fmt.Sprintf(" %d track(s) were too ambiguous to add automatically.", ambiguous)
			}
			s.publishEvent("playlist", fmt.Sprintf("Playlist %q append applied: %d added, %d queued.", playlist.Name, added, queued))
			return response, nil
		},
	)

	return playlistPreviewData{
		Response:          fmt.Sprintf("I prepared %d direct addition(s) and %d queued addition(s) for %q. Use the approval buttons if you want me to apply them.", len(availableSongIDs), len(missingTracks), playlist.Name),
		PendingAction:     action,
		PlaylistName:      playlist.Name,
		Mode:              "append",
		SourceSummary:     map[string]interface{}{"prompt": prompt, "normalizedIntent": meta["normalizedIntent"]},
		ConstraintSummary: map[string]interface{}{},
		Counts: map[string]int{
			"planned":         len(candidates),
			"availableNow":    len(availableSongIDs),
			"missing":         len(missingTracks),
			"ambiguous":       ambiguous,
			"skippedExisting": skippedExisting,
			"toApply":         len(availableSongIDs) + len(missingTracks),
			"errors":          errors,
		},
		Tracks: combinePlaylistPreviewTracks(orderedAvailableTracks, resolved),
		Notes: []string{
			"Existing tracks and pending fetch items are filtered out before approval.",
			"Direct additions are re-sequenced against the current playlist tail to reduce artist clumping.",
		},
	}, nil
}

func (s *Server) buildPlaylistRefreshPreview(ctx context.Context, playlistName string, replaceCount int) (playlistPreviewData, error) {
	playlistName = strings.TrimSpace(playlistName)
	if playlistName == "" {
		return playlistPreviewData{}, fmt.Errorf("playlistName is required")
	}
	if replaceCount <= 0 {
		replaceCount = 5
	}
	client, err := newNavidromeClientFromEnv()
	if err != nil {
		return playlistPreviewData{}, err
	}
	playlist, err := resolveNavidromePlaylist(ctx, client, "", playlistName)
	if err != nil {
		return playlistPreviewData{}, err
	}
	if len(playlist.Entries) == 0 {
		return playlistPreviewData{
			Response:          fmt.Sprintf("Playlist %q is empty, so there is nothing to refresh yet.", playlist.Name),
			PlaylistName:      playlist.Name,
			Mode:              "refresh",
			ConstraintSummary: map[string]interface{}{},
		}, nil
	}
	if replaceCount > len(playlist.Entries) {
		replaceCount = len(playlist.Entries)
	}
	removeIndexes, removalEntries := selectPlaylistRefreshEntries(playlist.Entries, replaceCount)
	if len(removeIndexes) == 0 {
		return playlistPreviewData{
			Response:          fmt.Sprintf("I didn’t find any safe refresh candidates in %q yet.", playlist.Name),
			PlaylistName:      playlist.Name,
			Mode:              "refresh",
			ConstraintSummary: map[string]interface{}{},
		}, nil
	}
	prompt := buildPlaylistRefreshPrompt(playlist.Name, playlist.Entries, removeIndexes, removalEntries)
	candidates, _, err := planDiscoverPlaylist(ctx, map[string]interface{}{
		"prompt":       prompt,
		"trackCount":   len(removalEntries),
		"playlistName": playlist.Name,
		"personalized": true,
	})
	if err != nil {
		return playlistPreviewData{}, err
	}
	resolved, err := resolvePlaylistCandidates(ctx, client, candidates)
	if err != nil {
		return playlistPreviewData{}, err
	}
	existingSongIDs, existingTrackKeys := buildPlaylistEntrySets(playlist.Entries)
	pendingKeys, err := pendingPlaylistTrackKeysForPlaylist(playlist.Name)
	if err != nil {
		return playlistPreviewData{}, err
	}
	availableSongIDs, missingTracks, skippedExisting, ambiguous, errors := collectPlaylistCandidateSets(resolved, existingSongIDs, existingTrackKeys, pendingKeys)
	availableSongIDs, _ = sequencePlaylistSlotReplacements(resolved, availableSongIDs, playlist.Entries, removeIndexes)
	applyCount := len(availableSongIDs)
	if applyCount > len(removeIndexes) {
		applyCount = len(removeIndexes)
	}
	if applyCount <= 0 {
		return playlistPreviewData{
			Response:          fmt.Sprintf("I found refresh candidates for %q, but none of the replacements are safely available in your library right now.", playlist.Name),
			PlaylistName:      playlist.Name,
			Mode:              "refresh",
			SourceSummary:     map[string]interface{}{"prompt": prompt},
			ConstraintSummary: map[string]interface{}{},
			Counts: map[string]int{
				"planned":         len(candidates),
				"availableNow":    0,
				"missing":         len(missingTracks),
				"ambiguous":       ambiguous,
				"skippedExisting": skippedExisting,
				"toApply":         0,
				"errors":          errors,
			},
			Tracks: makePlaylistReplacePreviewTracks(removalEntries, resolved),
		}, nil
	}
	appliedIndexes := append([]int(nil), removeIndexes[:applyCount]...)
	appliedEntries := append([]navidromePlaylistEntry(nil), removalEntries[:applyCount]...)
	appliedSongIDs := append([]string(nil), availableSongIDs[:applyCount]...)
	finalSongIDs, preserveOrder := buildOrderedPlaylistSongIDs(playlist.Entries, appliedIndexes, appliedSongIDs)
	details := []string{
		fmt.Sprintf("Playlist: %s", playlist.Name),
		fmt.Sprintf("Replace candidates: %d", len(removalEntries)),
		fmt.Sprintf("Ready replacements: %d", applyCount),
	}
	if applyCount > 1 {
		details = append(details, "Sequence: neighbor-aware replacements")
	}
	if preserveOrder {
		details = append(details, "Order: preserve replacement slots")
	} else {
		details = append(details, "Order: fallback to append replacements")
	}
	action := s.registerPendingActionForContext(
		ctx,
		"playlist_refresh",
		"Refresh playlist",
		fmt.Sprintf("Refresh playlist %q by replacing %d track(s).", playlist.Name, applyCount),
		details,
		func(runCtx context.Context) (string, error) {
			client, err := newNavidromeClientFromEnv()
			if err != nil {
				return "", err
			}
			if preserveOrder {
				if err := client.UpdatePlaylistRemoveIndexes(runCtx, playlist.ID, playlistEntryIndexes(playlist.Entries)); err != nil {
					return "", err
				}
				if err := client.UpdatePlaylistAddSongs(runCtx, playlist.ID, finalSongIDs); err != nil {
					return "", err
				}
			} else {
				if err := client.UpdatePlaylistRemoveIndexes(runCtx, playlist.ID, appliedIndexes); err != nil {
					return "", err
				}
				if err := client.UpdatePlaylistAddSongs(runCtx, playlist.ID, appliedSongIDs); err != nil {
					return "", err
				}
			}
			s.publishEvent("playlist", fmt.Sprintf("Playlist %q refresh applied: %d replaced.", playlist.Name, applyCount))
			if preserveOrder {
				return fmt.Sprintf("Playlist %q refreshed. Replaced %d track(s) with new in-library matches while preserving their slots in the sequence.", playlist.Name, applyCount), nil
			}
			return fmt.Sprintf("Playlist %q refreshed. Replaced %d track(s) with new in-library matches.", playlist.Name, applyCount), nil
		},
	)
	notes := []string{"Refresh replaces only tracks that have direct in-library replacements available now."}
	if applyCount > 1 {
		notes = append(notes, "Replacement tracks are sequenced against their local neighbors to avoid immediate artist clumping.")
	}
	if preserveOrder {
		notes = append(notes, "Approved refreshes rebuild the playlist so replacement tracks stay in their original slots.")
	} else {
		notes = append(notes, "This refresh will fall back to appending replacements because the current playlist entries could not be rebuilt position-for-position.")
	}
	if len(missingTracks) > 0 {
		notes = append(notes, fmt.Sprintf("%d additional replacement idea(s) were missing and were not queued automatically.", len(missingTracks)))
	}
	return playlistPreviewData{
		Response:          fmt.Sprintf("I prepared %d safe replacement(s) for %q. Use the approval buttons if you want me to refresh it.", applyCount, playlist.Name),
		PendingAction:     action,
		PlaylistName:      playlist.Name,
		Mode:              "refresh",
		SourceSummary:     map[string]interface{}{"prompt": prompt},
		ConstraintSummary: map[string]interface{}{},
		Counts: map[string]int{
			"planned":         len(candidates),
			"availableNow":    len(availableSongIDs),
			"missing":         len(missingTracks),
			"ambiguous":       ambiguous,
			"skippedExisting": skippedExisting,
			"toApply":         applyCount,
			"errors":          errors,
		},
		Tracks: makePlaylistReplacePreviewTracks(appliedEntries, resolved),
		Notes:  notes,
	}, nil
}

func (s *Server) buildPlaylistRepairPreview(ctx context.Context, playlistName string) (playlistPreviewData, error) {
	playlistName = strings.TrimSpace(playlistName)
	if playlistName == "" {
		return playlistPreviewData{}, fmt.Errorf("playlistName is required")
	}
	client, err := newNavidromeClientFromEnv()
	if err != nil {
		return playlistPreviewData{}, err
	}
	playlist, err := resolveNavidromePlaylist(ctx, client, "", playlistName)
	if err != nil {
		return playlistPreviewData{}, err
	}
	issues := selectPlaylistRepairIssues(playlist.Entries)
	if len(issues) == 0 {
		return playlistPreviewData{
			Response:          fmt.Sprintf("I didn’t find broken or duplicate saved tracks in %q, so there is nothing obvious to repair right now.", playlist.Name),
			PlaylistName:      playlist.Name,
			Mode:              "repair",
			ConstraintSummary: map[string]interface{}{},
		}, nil
	}
	removeIndexes := playlistRepairIssueIndexes(issues)
	reasonCounts := playlistRepairReasonCounts(issues)
	prompt := buildPlaylistRepairPrompt(playlist.Name, playlist.Entries, issues)
	candidates, _, err := planDiscoverPlaylist(ctx, map[string]interface{}{
		"prompt":       prompt,
		"trackCount":   len(issues),
		"playlistName": playlist.Name,
		"personalized": true,
	})
	if err != nil {
		return playlistPreviewData{}, err
	}
	resolved, err := resolvePlaylistCandidates(ctx, client, candidates)
	if err != nil {
		return playlistPreviewData{}, err
	}
	existingSongIDs, existingTrackKeys := buildPlaylistEntrySets(playlist.Entries)
	pendingKeys, err := pendingPlaylistTrackKeysForPlaylist(playlist.Name)
	if err != nil {
		return playlistPreviewData{}, err
	}
	availableSongIDs, missingTracks, skippedExisting, ambiguous, errors := collectPlaylistCandidateSets(resolved, existingSongIDs, existingTrackKeys, pendingKeys)
	availableSongIDs, _ = sequencePlaylistSlotReplacements(resolved, availableSongIDs, playlist.Entries, removeIndexes)
	applyCount := len(availableSongIDs)
	if applyCount > len(removeIndexes) {
		applyCount = len(removeIndexes)
	}
	removeOnlyCount := len(removeIndexes) - applyCount
	finalSongIDs, preserveOrder := buildOrderedPlaylistSongIDs(playlist.Entries, removeIndexes, availableSongIDs[:applyCount])
	if applyCount <= 0 && removeOnlyCount <= 0 {
		return playlistPreviewData{
			Response:          fmt.Sprintf("I found repairable entries in %q, but I couldn't find safe in-library replacements yet.", playlist.Name),
			PlaylistName:      playlist.Name,
			Mode:              "repair",
			SourceSummary:     map[string]interface{}{"prompt": prompt},
			ConstraintSummary: map[string]interface{}{},
			Counts: map[string]int{
				"planned":         len(candidates),
				"availableNow":    0,
				"missing":         len(missingTracks),
				"ambiguous":       ambiguous,
				"skippedExisting": skippedExisting,
				"toApply":         0,
				"errors":          errors,
			},
			Tracks: makePlaylistRepairPreviewTracks(issues, resolved),
		}, nil
	}
	if !preserveOrder {
		return playlistPreviewData{
			Response:          fmt.Sprintf("I found repairable entries in %q, but I couldn't rebuild a safe repaired sequence yet.", playlist.Name),
			PlaylistName:      playlist.Name,
			Mode:              "repair",
			SourceSummary:     map[string]interface{}{"prompt": prompt},
			ConstraintSummary: map[string]interface{}{},
			Counts: map[string]int{
				"planned":         len(candidates),
				"availableNow":    len(availableSongIDs),
				"missing":         len(missingTracks),
				"ambiguous":       ambiguous,
				"skippedExisting": skippedExisting,
				"toApply":         0,
				"errors":          errors,
			},
			Tracks: makePlaylistRepairPreviewTracks(issues, resolved),
		}, nil
	}
	details := []string{
		fmt.Sprintf("Playlist: %s", playlist.Name),
		fmt.Sprintf("Repair candidates: %d", len(issues)),
		fmt.Sprintf("Ready replacements: %d", applyCount),
	}
	if removeOnlyCount > 0 {
		details = append(details, fmt.Sprintf("Cleanup-only removals: %d", removeOnlyCount))
	}
	if applyCount > 1 {
		details = append(details, "Sequence: neighbor-aware replacements")
	}
	for reason, count := range reasonCounts {
		details = append(details, fmt.Sprintf("Issue: %s (%d)", reason, count))
	}
	details = append(details, "Order: preserve repair slots")
	action := s.registerPendingActionForContext(
		ctx,
		"playlist_repair",
		"Repair playlist",
		fmt.Sprintf("Repair playlist %q by replacing %d broken or duplicate track(s) and removing %d irreparable track(s) if needed.", playlist.Name, applyCount, removeOnlyCount),
		details,
		func(runCtx context.Context) (string, error) {
			client, err := newNavidromeClientFromEnv()
			if err != nil {
				return "", err
			}
			if err := client.UpdatePlaylistRemoveIndexes(runCtx, playlist.ID, playlistEntryIndexes(playlist.Entries)); err != nil {
				return "", err
			}
			if err := client.UpdatePlaylistAddSongs(runCtx, playlist.ID, finalSongIDs); err != nil {
				return "", err
			}
			s.publishEvent("playlist", fmt.Sprintf("Playlist %q repair applied: %d broken or duplicate track(s) replaced, %d removed.", playlist.Name, applyCount, removeOnlyCount))
			if removeOnlyCount > 0 {
				return fmt.Sprintf("Playlist %q repaired. Replaced %d broken or duplicate track(s) and removed %d irreparable track(s) while preserving the remaining sequence.", playlist.Name, applyCount, removeOnlyCount), nil
			}
			return fmt.Sprintf("Playlist %q repaired. Replaced %d broken or duplicate track(s) while preserving their slots in the sequence.", playlist.Name, applyCount), nil
		},
	)
	notes := []string{"Repair targets obviously broken or duplicate saved entries before changing anything else."}
	if applyCount > 1 {
		notes = append(notes, "Replacement tracks are sequenced against their local neighbors to avoid immediate artist clumping.")
	}
	notes = append(notes, "Approved repairs rebuild the playlist so replacements stay in their original slots.")
	if removeOnlyCount > 0 {
		notes = append(notes, "Entries without safe in-library replacements will be removed instead of blocking the entire repair.")
	}
	if len(missingTracks) > 0 {
		notes = append(notes, fmt.Sprintf("%d additional repair replacement idea(s) were missing and not queued automatically.", len(missingTracks)))
	}
	return playlistPreviewData{
		Response:          fmt.Sprintf("I prepared %d repair replacement(s) for %q. Use the approval buttons if you want me to apply them.", applyCount, playlist.Name),
		PendingAction:     action,
		PlaylistName:      playlist.Name,
		Mode:              "repair",
		SourceSummary:     map[string]interface{}{"prompt": prompt},
		ConstraintSummary: map[string]interface{}{},
		Counts: map[string]int{
			"planned":         len(candidates),
			"availableNow":    len(availableSongIDs),
			"missing":         len(missingTracks),
			"ambiguous":       ambiguous,
			"skippedExisting": skippedExisting,
			"toApply":         applyCount + removeOnlyCount,
			"errors":          errors,
		},
		Tracks: makePlaylistRepairPreviewTracks(issues, resolved),
		Notes:  notes,
	}, nil
}

func (s *Server) startPlaylistAppendPreview(ctx context.Context, playlistName, prompt string, trackCount int) (string, *PendingAction, error) {
	preview, err := s.buildPlaylistAppendPreview(ctx, playlistName, prompt, trackCount)
	if err != nil {
		return "", nil, err
	}
	return preview.Response, preview.PendingAction, nil
}

func (s *Server) startPlaylistCreatePreview(ctx context.Context, playlistName, prompt string, trackCount int) (string, *PendingAction, error) {
	preview, err := s.buildPlaylistCreatePreview(ctx, playlistName, prompt, trackCount)
	if err != nil {
		return "", nil, err
	}
	return preview.Response, preview.PendingAction, nil
}

func (s *Server) startPlaylistRefreshPreview(ctx context.Context, playlistName string, replaceCount int) (string, *PendingAction, error) {
	preview, err := s.buildPlaylistRefreshPreview(ctx, playlistName, replaceCount)
	if err != nil {
		return "", nil, err
	}
	return preview.Response, preview.PendingAction, nil
}

func (s *Server) startPlaylistRepairPreview(ctx context.Context, playlistName string) (string, *PendingAction, error) {
	preview, err := s.buildPlaylistRepairPreview(ctx, playlistName)
	if err != nil {
		return "", nil, err
	}
	return preview.Response, preview.PendingAction, nil
}

func recommendedLidarrCleanupAction(candidates []lidarrCleanupCandidate) string {
	counts := map[string]int{}
	for _, c := range candidates {
		action := strings.TrimSpace(strings.ToLower(c.RecommendedAction))
		if action == "" {
			continue
		}
		counts[action]++
	}
	bestAction := ""
	bestCount := 0
	for action, n := range counts {
		if n > bestCount {
			bestCount = n
			bestAction = action
		}
	}
	if bestAction != "" {
		return bestAction
	}
	return "search_missing"
}

func (s *Server) executeLidarrCleanupApproval(ctx context.Context, updatedAt time.Time, action, selection string) (string, error) {
	candidates, cachedAt := getLastLidarrCandidates(chatSessionIDFromContext(ctx))
	if len(candidates) == 0 || cachedAt.IsZero() {
		return "", fmt.Errorf("cleanup preview is no longer available")
	}
	if cachedAt.UnixNano() != updatedAt.UnixNano() {
		return "", fmt.Errorf("cleanup preview changed")
	}
	if time.Since(cachedAt) > 20*time.Minute {
		return "", fmt.Errorf("cleanup preview expired")
	}
	if selection == "" {
		selection = "all"
	}
	if action == "" {
		action = recommendedLidarrCleanupAction(candidates)
	}
	dedupeKey := fmt.Sprintf("%d|%s|%s", cachedAt.UnixNano(), action, selection)
	resp, handled, err := s.runWorkflowWithDedupe(ctx, "lidarr_cleanup_apply", dedupeKey, func(runCtx context.Context) (string, bool, error) {
		client, err := newLidarrClientFromEnv()
		if err != nil {
			return "", false, err
		}

		s.publishEvent("lidarr", fmt.Sprintf("Library cleanup confirmed. Applying action '%s' on selection '%s'.", action, selection))
		results, mode, err := applyLidarrCleanup(runCtx, client, map[string]interface{}{
			"action":    action,
			"selection": selection,
			"dryRun":    false,
			"confirm":   true,
		})
		if err != nil {
			s.publishEvent("lidarr", "Library cleanup failed.")
			return "", false, err
		}
		if mode != "applied" {
			return "", false, nil
		}

		okCount := 0
		failCount := 0
		for _, r := range results {
			if r.Status == "ok" {
				okCount++
			} else {
				failCount++
			}
		}

		s.publishEvent("lidarr", fmt.Sprintf("Library cleanup complete: %d successful, %d failed.", okCount, failCount))
		return fmt.Sprintf(
			"Applied library cleanup action '%s' on %d album(s) (%s): %d successful, %d failed.",
			action,
			len(results),
			selection,
			okCount,
			failCount,
		), true, nil
	})
	if err != nil {
		log.Warn().Err(err).Str("workflow", "lidarr_cleanup_apply").Msg("Workflow execution failed")
		return "", err
	}
	if !handled {
		return "", fmt.Errorf("cleanup workflow did not apply")
	}
	return resp, nil
}

func (s *Server) executeDiscoveredAlbumsApproval(ctx context.Context, discoveredAt time.Time, selection string) (string, error) {
	candidates, cachedAt, sourceQuery := getLastDiscoveredAlbums(chatSessionIDFromContext(ctx))
	if len(candidates) == 0 || cachedAt.IsZero() {
		return "", fmt.Errorf("discovered album preview is no longer available")
	}
	if cachedAt.UnixNano() != discoveredAt.UnixNano() {
		return "", fmt.Errorf("discovered album preview changed")
	}
	if time.Since(cachedAt) > 30*time.Minute {
		return "", fmt.Errorf("discovered album preview expired")
	}
	if selection == "" {
		selection = "all"
	}
	selected := candidates
	if !strings.EqualFold(strings.TrimSpace(selection), "all") {
		var err error
		selected, err = discovery.SelectCandidates(candidates, selection)
		if err != nil {
			return "", err
		}
	}

	dedupeKey := fmt.Sprintf("%d|%s", cachedAt.UnixNano(), selection)
	resp, handled, err := s.runWorkflowWithDedupe(ctx, "lidarr_discovery_apply", dedupeKey, func(runCtx context.Context) (string, bool, error) {
		client, err := newLidarrClientFromEnv()
		if err != nil {
			return "", false, err
		}

		s.publishEvent("lidarr", fmt.Sprintf("Discovery ready. Matching %s in your library.", selection))
		matches, _, err := matchDiscoveredAlbumCandidatesInLidarr(runCtx, client, selected, cachedAt, sourceQuery)
		if err != nil {
			s.publishEvent("lidarr", "Library match step failed.")
			return "", false, err
		}

		s.publishEvent("lidarr", "Applying add/search actions in your library.")
		items, mode, err := applyDiscoveredAlbumCandidates(runCtx, client, selected, map[string]interface{}{
			"selection": selection,
			"dryRun":    false,
			"confirm":   true,
		})
		if err != nil {
			s.publishEvent("lidarr", "Library apply step failed.")
			return "", false, err
		}
		if mode != "applied" || len(items) == 0 {
			return "", false, nil
		}

		ready := 0
		already := 0
		for _, m := range matches {
			switch m.Status {
			case "can_monitor":
				ready++
			case "already_monitored":
				already++
			}
		}

		okCount := 0
		partialCount := 0
		failCount := 0
		for _, item := range items {
			switch item.Status {
			case "ok":
				okCount++
			case "partial":
				partialCount++
			case "error", "not_found", "ambiguous":
				failCount++
			}
		}

		out := fmt.Sprintf(
			"Applied library actions for %d discovered albums (%s): %d successful, %d partial, %d failed.",
			len(items),
			selection,
			okCount,
			partialCount,
			failCount,
		)
		if ready > 0 || already > 0 {
			out += fmt.Sprintf(" Match summary: %d ready to add, %d already in your library.", ready, already)
		}
		if strings.TrimSpace(sourceQuery) != "" {
			out += fmt.Sprintf(" Source: %q.", sourceQuery)
		}
		s.publishEvent("lidarr", fmt.Sprintf("Library apply complete: %d successful, %d partial, %d failed.", okCount, partialCount, failCount))
		return out, true, nil
	})
	if err != nil {
		log.Warn().Err(err).Str("workflow", "lidarr_discovery_apply").Msg("Workflow execution failed")
		return "", err
	}
	if !handled {
		return "", fmt.Errorf("discovered album workflow did not apply")
	}
	return resp, nil
}

func (s *Server) executePlaylistCreateApproval(ctx context.Context, plannedAt time.Time) (string, error) {
	_, _, cachedAt, planned := getLastPlannedPlaylist(chatSessionIDFromContext(ctx))
	if len(planned) == 0 || cachedAt.IsZero() {
		return "", fmt.Errorf("playlist plan is no longer available")
	}
	if cachedAt.UnixNano() != plannedAt.UnixNano() {
		return "", fmt.Errorf("playlist plan changed")
	}
	dedupeKey := fmt.Sprintf("%d", cachedAt.UnixNano())
	resp, handled, err := s.runWorkflowWithDedupe(ctx, "playlist_create", dedupeKey, func(runCtx context.Context) (string, bool, error) {
		triggerPlaylistReconcile()
		s.publishEvent("playlist", "Plan ready. Resolving tracks in your library.")

		rawResolve, err := executeTool(runCtx, s.resolver, s.embeddingsURL, "resolvePlaylistTracks", map[string]interface{}{
			"selection": "all",
		})
		if err != nil {
			s.publishEvent("playlist", "Resolve step failed.")
			return "", false, err
		}
		s.publishEvent("playlist", "Resolved tracks. Creating playlist in Navidrome.")

		rawCreate, err := executeTool(runCtx, s.resolver, s.embeddingsURL, "createDiscoveredPlaylist", map[string]interface{}{
			"selection": "all",
			"confirm":   true,
		})
		if err != nil {
			s.publishEvent("playlist", "Playlist create/update step failed.")
			return "", false, err
		}

		var resolved struct {
			Data struct {
				ResolvePlaylistTracks struct {
					Counts struct {
						Available int `json:"available"`
						Missing   int `json:"missing"`
						Ambiguous int `json:"ambiguous"`
					} `json:"counts"`
				} `json:"resolvePlaylistTracks"`
			} `json:"data"`
		}
		_ = json.Unmarshal([]byte(rawResolve), &resolved)

		var created struct {
			Data struct {
				CreateDiscoveredPlaylist struct {
					Action       string `json:"action"`
					PlaylistName string `json:"playlistName"`
					Added        int    `json:"added"`
					Existing     int    `json:"existing"`
				} `json:"createDiscoveredPlaylist"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(rawCreate), &created); err != nil {
			return "", false, err
		}

		out := created.Data.CreateDiscoveredPlaylist
		if strings.TrimSpace(out.PlaylistName) == "" {
			return "", false, nil
		}

		actionText := "updated"
		if out.Action == "created" {
			actionText = "created"
		}
		outResp := fmt.Sprintf(
			"Playlist '%s' %s. Added %d tracks from your library",
			out.PlaylistName,
			actionText,
			out.Added,
		)
		if out.Existing > 0 {
			outResp += fmt.Sprintf(" (%d were already in the playlist)", out.Existing)
		}
		outResp += "."

		counts := resolved.Data.ResolvePlaylistTracks.Counts
		if counts.Missing > 0 || counts.Ambiguous > 0 {
			outResp += fmt.Sprintf(
				" I also found %d missing and %d ambiguous tracks while resolving.",
				counts.Missing,
				counts.Ambiguous,
			)
		}
		s.publishEvent("playlist", fmt.Sprintf("Playlist '%s' %s with %d added track(s).", out.PlaylistName, actionText, out.Added))

		if counts.Missing > 0 {
			s.publishEvent("playlist", "Queueing missing tracks for the download agent.")
			rawQueue, err := executeTool(runCtx, s.resolver, s.embeddingsURL, "queueMissingPlaylistTracks", map[string]interface{}{
				"selection": "all",
				"confirm":   true,
			})
			if err != nil {
				s.publishEvent("playlist", "Auto-queue failed for missing tracks.")
				outResp += " I could not auto-queue missing tracks."
				return outResp, true, nil
			}
			var queued struct {
				Data struct {
					QueueMissingPlaylistTracks struct {
						Queued    int    `json:"queued"`
						QueueFile string `json:"queueFile"`
					} `json:"queueMissingPlaylistTracks"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(rawQueue), &queued); err != nil {
				s.publishEvent("playlist", "Auto-queue parsing failed for missing tracks.")
				outResp += " I could not auto-queue missing tracks."
				return outResp, true, nil
			}
			if queued.Data.QueueMissingPlaylistTracks.Queued > 0 {
				triggerPlaylistReconcile()
				s.publishEvent("playlist", fmt.Sprintf("Queued %d missing track(s) for the download agent.", queued.Data.QueueMissingPlaylistTracks.Queued))
				outResp += fmt.Sprintf(
					" Auto-queued %d missing tracks for the download agent.",
					queued.Data.QueueMissingPlaylistTracks.Queued,
				)
			}
		}

		return outResp, true, nil
	})
	if err != nil {
		log.Warn().Err(err).Str("workflow", "playlist_create").Msg("Workflow execution failed")
		return "", err
	}
	if !handled {
		return "", fmt.Errorf("playlist workflow did not apply")
	}
	return resp, nil
}
