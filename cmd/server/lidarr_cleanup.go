package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"groovarr/internal/lidarr"
)

type lidarrClient = lidarr.Client
type lidarrAlbum = lidarr.Album
type lidarrWantedRecord = lidarr.WantedRecord
type lidarrCleanupCandidate = lidarr.CleanupCandidate
type lidarrCleanupApplyItem = lidarr.CleanupApplyItem

type lidarrCleanupState struct {
	candidates []lidarrCleanupCandidate
	updatedAt  time.Time
}

var lastLidarrCandidates = struct {
	mu       sync.RWMutex
	sessions map[string]lidarrCleanupState
}{
	sessions: make(map[string]lidarrCleanupState),
}

func newLidarrClientFromEnv() (*lidarrClient, error) {
	return lidarr.NewFromEnv()
}

func buildLidarrCleanupCandidates(ctx context.Context, c *lidarrClient, args map[string]interface{}) ([]lidarrCleanupCandidate, map[string]int, map[string]interface{}, error) {
	scope := strings.ToLower(strings.TrimSpace(toolStringArg(args, "scope")))
	if scope == "" {
		scope = "missing_files"
	}
	limit := toolIntArg(args, "limit", 25)
	if limit < 1 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	artistFilter := strings.ToLower(strings.TrimSpace(toolStringArg(args, "artist")))
	pathContains := strings.ToLower(strings.TrimSpace(toolStringArg(args, "pathContains")))
	olderThanDays := toolIntArg(args, "olderThanDays", 0)

	matches := func(artistName, path string, addedAt time.Time) bool {
		if artistFilter != "" && !strings.Contains(strings.ToLower(artistName), artistFilter) {
			return false
		}
		if pathContains != "" && !strings.Contains(strings.ToLower(path), pathContains) {
			return false
		}
		if olderThanDays > 0 && !addedAt.IsZero() {
			if time.Since(addedAt) < time.Duration(olderThanDays)*24*time.Hour {
				return false
			}
		}
		return true
	}

	parseAdded := func(raw string) time.Time {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return time.Time{}
		}
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			return t
		}
		if t, err := time.Parse("2006-01-02", raw); err == nil {
			return t
		}
		return time.Time{}
	}

	seen := make(map[int]struct{})
	candidates := make([]lidarrCleanupCandidate, 0, limit)
	summary := map[string]int{}
	addCandidate := func(item lidarrCleanupCandidate) {
		if item.AlbumID <= 0 {
			return
		}
		if _, ok := seen[item.AlbumID]; ok {
			return
		}
		seen[item.AlbumID] = struct{}{}
		candidates = append(candidates, item)
		summary[item.Reason]++
	}

	switch scope {
	case "missing_files":
		recs, err := c.GetWantedMissing(ctx, limit*3)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, r := range recs {
			if len(candidates) >= limit {
				break
			}
			if !matches(r.ArtistName, "", time.Time{}) {
				continue
			}
			addCandidate(lidarrCleanupCandidate{
				AlbumID:           r.AlbumID,
				ArtistName:        r.ArtistName,
				Title:             r.Title,
				Monitored:         r.Monitored,
				Reason:            "Missing files in Lidarr wanted list",
				RecommendedAction: "search_missing",
			})
		}
	case "low_quality":
		recs, err := c.GetWantedCutoff(ctx, limit*3)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, r := range recs {
			if len(candidates) >= limit {
				break
			}
			if !matches(r.ArtistName, "", time.Time{}) {
				continue
			}
			addCandidate(lidarrCleanupCandidate{
				AlbumID:           r.AlbumID,
				ArtistName:        r.ArtistName,
				Title:             r.Title,
				Monitored:         r.Monitored,
				Reason:            "Below quality cutoff in Lidarr wanted list",
				RecommendedAction: "search_missing",
			})
		}
	case "unmonitored", "unwanted_path", "duplicates":
		albums, err := c.GetAlbums(ctx)
		if err != nil {
			return nil, nil, nil, err
		}

		sort.SliceStable(albums, func(i, j int) bool {
			return albums[i].ID < albums[j].ID
		})

		switch scope {
		case "unmonitored":
			for _, a := range albums {
				if len(candidates) >= limit {
					break
				}
				addedAt := parseAdded(a.Added)
				if a.Monitored || !matches(a.ArtistName, a.Path, addedAt) {
					continue
				}
				addCandidate(lidarrCleanupCandidate{
					AlbumID:           a.ID,
					ArtistName:        a.ArtistName,
					Title:             a.Title,
					Monitored:         a.Monitored,
					Path:              a.Path,
					Reason:            "Album is unmonitored",
					RecommendedAction: "delete",
				})
			}
		case "unwanted_path":
			if pathContains == "" {
				return nil, nil, nil, fmt.Errorf("pathContains is required for scope=unwanted_path")
			}
			for _, a := range albums {
				if len(candidates) >= limit {
					break
				}
				addedAt := parseAdded(a.Added)
				if !matches(a.ArtistName, a.Path, addedAt) {
					continue
				}
				addCandidate(lidarrCleanupCandidate{
					AlbumID:           a.ID,
					ArtistName:        a.ArtistName,
					Title:             a.Title,
					Monitored:         a.Monitored,
					Path:              a.Path,
					Reason:            "Album path matches unwanted_path filter",
					RecommendedAction: "delete",
				})
			}
		case "duplicates":
			groups := make(map[string][]lidarrAlbum)
			for _, a := range albums {
				key := strings.ToLower(strings.TrimSpace(a.ArtistName + "::" + a.Title))
				groups[key] = append(groups[key], a)
			}
			for _, g := range groups {
				if len(g) < 2 {
					continue
				}
				for i := 1; i < len(g); i++ {
					if len(candidates) >= limit {
						break
					}
					a := g[i]
					addedAt := parseAdded(a.Added)
					if !matches(a.ArtistName, a.Path, addedAt) {
						continue
					}
					addCandidate(lidarrCleanupCandidate{
						AlbumID:           a.ID,
						ArtistName:        a.ArtistName,
						Title:             a.Title,
						Monitored:         a.Monitored,
						Path:              a.Path,
						Reason:            "Potential duplicate album entry",
						RecommendedAction: "unmonitor",
					})
				}
				if len(candidates) >= limit {
					break
				}
			}
		}
	default:
		return nil, nil, nil, fmt.Errorf("unsupported scope: %s", scope)
	}

	filters := map[string]interface{}{
		"scope":         scope,
		"limit":         limit,
		"artist":        toolStringArg(args, "artist"),
		"pathContains":  toolStringArg(args, "pathContains"),
		"olderThanDays": olderThanDays,
	}
	return candidates, summary, filters, nil
}

func parseAlbumIDsArg(args map[string]interface{}, key string) ([]int, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil, fmt.Errorf("%s is required", key)
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s must be an array", key)
	}
	ids := make([]int, 0, len(arr))
	seen := make(map[int]struct{})
	for _, it := range arr {
		var id int
		switch v := it.(type) {
		case float64:
			id = int(v)
		case int:
			id = v
		case string:
			p, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return nil, fmt.Errorf("invalid album id: %v", v)
			}
			id = p
		default:
			return nil, fmt.Errorf("invalid album id type: %T", it)
		}
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no valid album ids provided")
	}
	return ids, nil
}

func applyLidarrCleanup(ctx context.Context, c *lidarrClient, args map[string]interface{}) ([]lidarrCleanupApplyItem, string, error) {
	action := strings.ToLower(strings.TrimSpace(toolStringArg(args, "action")))
	if action == "" {
		return nil, "", fmt.Errorf("action is required")
	}
	albumIDs, err := resolveAlbumIDsForApply(ctx, args)
	if err != nil {
		return nil, "", err
	}
	dryRun := true
	if v := toolOptBoolArg(args, "dryRun"); v != nil {
		dryRun = *v
	}
	confirm := false
	if v := toolOptBoolArg(args, "confirm"); v != nil {
		confirm = *v
	}
	if !dryRun && !confirm {
		return nil, "", fmt.Errorf("confirm=true is required when dryRun=false")
	}

	if dryRun {
		items := make([]lidarrCleanupApplyItem, 0, len(albumIDs))
		for _, id := range albumIDs {
			items = append(items, lidarrCleanupApplyItem{AlbumID: id, Status: "dry_run", Detail: "No change applied"})
		}
		return items, "dry_run", nil
	}

	items := make([]lidarrCleanupApplyItem, 0, len(albumIDs))
	switch action {
	case "unmonitor":
		err := c.UnmonitorAlbums(ctx, albumIDs)
		for _, id := range albumIDs {
			if err != nil {
				items = append(items, lidarrCleanupApplyItem{AlbumID: id, Status: "failed", Detail: err.Error()})
			} else {
				items = append(items, lidarrCleanupApplyItem{AlbumID: id, Status: "ok"})
			}
		}
	case "delete":
		for _, id := range albumIDs {
			if err := c.DeleteAlbum(ctx, id); err != nil {
				items = append(items, lidarrCleanupApplyItem{AlbumID: id, Status: "failed", Detail: err.Error()})
				continue
			}
			items = append(items, lidarrCleanupApplyItem{AlbumID: id, Status: "ok"})
		}
	case "search_missing":
		for _, id := range albumIDs {
			if err := c.AlbumSearch(ctx, id); err != nil {
				items = append(items, lidarrCleanupApplyItem{AlbumID: id, Status: "failed", Detail: err.Error()})
				continue
			}
			items = append(items, lidarrCleanupApplyItem{AlbumID: id, Status: "ok"})
		}
	case "refresh_metadata":
		for _, id := range albumIDs {
			if err := c.RefreshAlbum(ctx, id); err != nil {
				items = append(items, lidarrCleanupApplyItem{AlbumID: id, Status: "failed", Detail: err.Error()})
				continue
			}
			items = append(items, lidarrCleanupApplyItem{AlbumID: id, Status: "ok"})
		}
	default:
		return nil, "", fmt.Errorf("unsupported action: %s", action)
	}

	return items, "applied", nil
}

func setLastLidarrCandidates(sessionID string, candidates []lidarrCleanupCandidate) {
	newTurnSessionMemoryWriter(sessionID).SetCleanupCandidates(candidates)
}

func getLastLidarrCandidates(sessionID string) ([]lidarrCleanupCandidate, time.Time) {
	lastLidarrCandidates.mu.RLock()
	state, ok := lastLidarrCandidates.sessions[normalizeChatSessionID(sessionID)]
	lastLidarrCandidates.mu.RUnlock()
	if !ok {
		return nil, time.Time{}
	}
	cloned := make([]lidarrCleanupCandidate, len(state.candidates))
	copy(cloned, state.candidates)
	return cloned, state.updatedAt
}

func resolveAlbumIDsForApply(ctx context.Context, args map[string]interface{}) ([]int, error) {
	if _, ok := args["albumIds"]; ok && args["albumIds"] != nil {
		return parseAlbumIDsArg(args, "albumIds")
	}

	selection := strings.TrimSpace(toolStringArg(args, "selection"))
	if selection == "" {
		return nil, fmt.Errorf("albumIds or selection is required")
	}

	candidates, updatedAt, ok := loadTurnSessionMemory(chatSessionIDFromContext(ctx)).CleanupCandidates()
	if !ok || len(candidates) == 0 {
		return nil, fmt.Errorf("no cached cleanup candidates available; run lidarrCleanupCandidates first")
	}
	if time.Since(updatedAt) > 20*time.Minute {
		return nil, fmt.Errorf("cached cleanup candidates are stale; run lidarrCleanupCandidates again")
	}
	return selectAlbumIDsFromCandidates(selection, candidates)
}

func selectAlbumIDsFromCandidates(selection string, candidates []lidarrCleanupCandidate) ([]int, error) {
	sel := strings.ToLower(strings.TrimSpace(selection))
	if sel == "" {
		return nil, fmt.Errorf("selection is required")
	}
	normalized := strings.NewReplacer("_", " ", "-", " ").Replace(sel)

	if sel == "all" || sel == "everything" {
		ids := make([]int, 0, len(candidates))
		for _, c := range candidates {
			ids = append(ids, c.AlbumID)
		}
		return ids, nil
	}

	if strings.HasPrefix(normalized, "first ") || strings.HasPrefix(normalized, "top ") {
		parts := strings.Fields(normalized)
		if len(parts) >= 2 {
			n, err := strconv.Atoi(parts[1])
			if err == nil && n > 0 {
				if n > len(candidates) {
					n = len(candidates)
				}
				ids := make([]int, 0, n)
				for i := 0; i < n; i++ {
					ids = append(ids, candidates[i].AlbumID)
				}
				return ids, nil
			}
		}
	}

	if positions, ok := parseOrdinalSelectionList(sel); ok {
		ids := make([]int, 0, len(positions))
		seen := make(map[int]struct{}, len(positions))
		for _, pos := range positions {
			index := pos - 1
			if index < 0 || index >= len(candidates) {
				continue
			}
			id := candidates[index].AlbumID
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("selection %q did not match cached candidates", selection)
		}
		return ids, nil
	}

	needle := normalized
	needle = strings.TrimPrefix(needle, "artist:")
	needle = strings.TrimPrefix(needle, "title:")
	needle = strings.TrimSpace(needle)

	ids := make([]int, 0, len(candidates))
	seen := make(map[int]struct{})
	for _, c := range candidates {
		artist := strings.ToLower(c.ArtistName)
		title := strings.ToLower(c.Title)
		if strings.Contains(artist, needle) || strings.Contains(title, needle) {
			if _, ok := seen[c.AlbumID]; ok {
				continue
			}
			seen[c.AlbumID] = struct{}{}
			ids = append(ids, c.AlbumID)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("selection %q did not match cached candidates", selection)
	}
	return ids, nil
}

func selectCleanupCandidates(selection string, candidates []lidarrCleanupCandidate) ([]lidarrCleanupCandidate, error) {
	ids, err := selectAlbumIDsFromCandidates(selection, candidates)
	if err != nil {
		return nil, err
	}
	selected := make([]lidarrCleanupCandidate, 0, len(ids))
	allowed := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		allowed[id] = struct{}{}
	}
	for _, candidate := range candidates {
		if _, ok := allowed[candidate.AlbumID]; !ok {
			continue
		}
		selected = append(selected, candidate)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("selection %q did not match cached candidates", selection)
	}
	return selected, nil
}

func selectionFromFocusedCleanupCandidate(candidates []lidarrCleanupCandidate, focusedKey string) (string, bool) {
	focusedKey = strings.TrimSpace(focusedKey)
	if focusedKey == "" {
		return "", false
	}
	for index, candidate := range candidates {
		if normalizedCleanupCandidateKey(candidate) != focusedKey {
			continue
		}
		return strconv.Itoa(index + 1), true
	}
	return "", false
}

func normalizedCleanupCandidateKey(candidate lidarrCleanupCandidate) string {
	if candidate.AlbumID > 0 {
		return strconv.Itoa(candidate.AlbumID)
	}
	return normalizeSearchTerm(candidate.ArtistName) + "::" + normalizeSearchTerm(candidate.Title)
}

func parseOrdinalSelectionList(raw string) ([]int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	parts := strings.Split(raw, ",")
	positions := make([]int, 0, len(parts))
	seen := make(map[int]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n <= 0 {
			return nil, false
		}
		if _, exists := seen[n]; exists {
			continue
		}
		seen[n] = struct{}{}
		positions = append(positions, n)
	}
	if len(positions) == 0 {
		return nil, false
	}
	return positions, true
}
