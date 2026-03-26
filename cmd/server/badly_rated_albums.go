package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const llmContextBadlyRatedAlbumsTTL = 30 * time.Minute

type badlyRatedAlbumCandidate struct {
	AlbumID       string
	AlbumName     string
	ArtistName    string
	BadTrackCount int
}

type badlyRatedAlbumsState struct {
	candidates []badlyRatedAlbumCandidate
	updatedAt  time.Time
}

type badlyRatedAlbumMatch struct {
	Candidate badlyRatedAlbumCandidate
	LidarrID  int
	Path      string
}

var lastBadlyRatedAlbums = struct {
	mu       sync.RWMutex
	sessions map[string]badlyRatedAlbumsState
}{
	sessions: make(map[string]badlyRatedAlbumsState),
}

func setLastBadlyRatedAlbums(sessionID string, candidates []badlyRatedAlbumCandidate) {
	newTurnSessionMemoryWriter(sessionID).SetBadlyRatedAlbums(candidates)
}

func getLastBadlyRatedAlbums(sessionID string) ([]badlyRatedAlbumCandidate, time.Time) {
	lastBadlyRatedAlbums.mu.RLock()
	state, ok := lastBadlyRatedAlbums.sessions[normalizeChatSessionID(sessionID)]
	lastBadlyRatedAlbums.mu.RUnlock()
	if !ok {
		return nil, time.Time{}
	}
	cloned := make([]badlyRatedAlbumCandidate, len(state.candidates))
	copy(cloned, state.candidates)
	return cloned, state.updatedAt
}

func formatBadlyRatedAlbumsContext(updatedAt time.Time, candidates []badlyRatedAlbumCandidate, now time.Time) string {
	if updatedAt.IsZero() || now.Sub(updatedAt) > llmContextBadlyRatedAlbumsTTL {
		return ""
	}
	if len(candidates) == 0 {
		return "last_badly_rated_albums: count=0"
	}
	sample := make([]string, 0, minInt(len(candidates), 3))
	for _, candidate := range candidates {
		if len(sample) >= 3 {
			break
		}
		label := strings.TrimSpace(candidate.AlbumName)
		if artist := strings.TrimSpace(candidate.ArtistName); artist != "" {
			label += " by " + artist
		}
		if candidate.BadTrackCount > 0 {
			label += fmt.Sprintf(" (%d bad track", candidate.BadTrackCount)
			if candidate.BadTrackCount != 1 {
				label += "s"
			}
			label += ")"
		}
		sample = append(sample, label)
	}
	return fmt.Sprintf(
		"last_badly_rated_albums: count=%d; sample=%q",
		len(candidates),
		strings.Join(sample, " | "),
	)
}

func recentBadlyRatedAlbumsState(sessionID string, now time.Time) ([]badlyRatedAlbumCandidate, time.Time, bool) {
	candidates, updatedAt, ok := loadTurnSessionMemory(sessionID).BadlyRatedAlbums()
	if !ok || updatedAt.IsZero() || now.Sub(updatedAt) > llmContextBadlyRatedAlbumsTTL {
		return nil, time.Time{}, false
	}
	return candidates, updatedAt, true
}

func selectBadlyRatedAlbums(selection string, candidates []badlyRatedAlbumCandidate) ([]badlyRatedAlbumCandidate, error) {
	selection = strings.ToLower(strings.TrimSpace(selection))
	if selection == "" || selection == "all" || selection == "those" || selection == "them" || selection == "these" || selection == "everything" {
		return append([]badlyRatedAlbumCandidate(nil), candidates...), nil
	}
	if n, ok := parseLeadingCountSelection(selection); ok {
		if n <= 0 {
			return nil, fmt.Errorf("selection resolved to zero albums")
		}
		if n > len(candidates) {
			n = len(candidates)
		}
		return append([]badlyRatedAlbumCandidate(nil), candidates[:n]...), nil
	}
	if positions, ok := parseOrdinalSelectionList(selection); ok {
		selected := make([]badlyRatedAlbumCandidate, 0, len(positions))
		seen := make(map[string]struct{}, len(positions))
		for _, pos := range positions {
			index := pos - 1
			if index < 0 || index >= len(candidates) {
				continue
			}
			candidate := candidates[index]
			key := normalizeSearchTerm(candidate.ArtistName) + "::" + normalizeSearchTerm(candidate.AlbumName)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			selected = append(selected, candidate)
		}
		if len(selected) == 0 {
			return nil, fmt.Errorf("selection %q did not match cached badly rated albums", selection)
		}
		return selected, nil
	}
	needle := strings.TrimSpace(selection)
	if needle != "" {
		selected := make([]badlyRatedAlbumCandidate, 0, len(candidates))
		for _, candidate := range candidates {
			artist := strings.ToLower(strings.TrimSpace(candidate.ArtistName))
			album := strings.ToLower(strings.TrimSpace(candidate.AlbumName))
			if strings.Contains(artist, needle) || strings.Contains(album, needle) {
				selected = append(selected, candidate)
			}
		}
		if len(selected) > 0 {
			return selected, nil
		}
	}
	return nil, fmt.Errorf("selection %q did not match cached badly rated albums", selection)
}

func selectionFromFocusedBadlyRatedAlbum(candidates []badlyRatedAlbumCandidate, focusedKey string) (string, bool) {
	focusedKey = strings.TrimSpace(focusedKey)
	if focusedKey == "" {
		return "", false
	}
	for index, candidate := range candidates {
		if normalizedBadlyRatedAlbumCandidateKey(candidate) != focusedKey {
			continue
		}
		return strconv.Itoa(index + 1), true
	}
	return "", false
}

func normalizedBadlyRatedAlbumCandidateKey(candidate badlyRatedAlbumCandidate) string {
	if albumID := strings.TrimSpace(candidate.AlbumID); albumID != "" {
		return albumID
	}
	return normalizeSearchTerm(candidate.ArtistName) + "::" + normalizeSearchTerm(candidate.AlbumName)
}

func matchBadlyRatedAlbumsInLidarr(candidates []badlyRatedAlbumCandidate, albums []lidarrAlbum) ([]badlyRatedAlbumMatch, []badlyRatedAlbumCandidate, []badlyRatedAlbumCandidate) {
	matchesByKey := make(map[string][]lidarrAlbum)
	for _, album := range albums {
		key := normalizeSearchTerm(album.ArtistName) + "::" + normalizeSearchTerm(album.Title)
		if key == "::" {
			continue
		}
		matchesByKey[key] = append(matchesByKey[key], album)
	}

	matched := make([]badlyRatedAlbumMatch, 0, len(candidates))
	ambiguous := make([]badlyRatedAlbumCandidate, 0)
	missing := make([]badlyRatedAlbumCandidate, 0)
	for _, candidate := range candidates {
		key := normalizeSearchTerm(candidate.ArtistName) + "::" + normalizeSearchTerm(candidate.AlbumName)
		options := matchesByKey[key]
		switch len(options) {
		case 0:
			missing = append(missing, candidate)
		case 1:
			matched = append(matched, badlyRatedAlbumMatch{
				Candidate: candidate,
				LidarrID:  options[0].ID,
				Path:      options[0].Path,
			})
		default:
			ambiguous = append(ambiguous, candidate)
		}
	}

	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].Candidate.ArtistName == matched[j].Candidate.ArtistName {
			return matched[i].Candidate.AlbumName < matched[j].Candidate.AlbumName
		}
		return matched[i].Candidate.ArtistName < matched[j].Candidate.ArtistName
	})
	return matched, ambiguous, missing
}

func (s *Server) buildBadlyRatedAlbumsCleanupPendingAction(ctx context.Context, selection string, minStateAt time.Time) (*PendingAction, int, bool) {
	sessionID := chatSessionIDFromContext(ctx)
	candidates, updatedAt, ok := loadTurnSessionMemory(sessionID).BadlyRatedAlbums()
	if !ok || len(candidates) == 0 || updatedAt.IsZero() || time.Since(updatedAt) > llmContextBadlyRatedAlbumsTTL {
		return nil, 0, false
	}
	if !minStateAt.IsZero() && updatedAt.Before(minStateAt) {
		return nil, 0, false
	}

	selected, err := selectBadlyRatedAlbums(selection, candidates)
	if err != nil || len(selected) == 0 {
		return nil, 0, false
	}

	details := []string{
		fmt.Sprintf("Albums selected: %d", len(selected)),
		"Action: delete from Lidarr with files",
	}
	for _, candidate := range selected {
		details = append(details, fmt.Sprintf("%s by %s (%d bad track(s))", candidate.AlbumName, candidate.ArtistName, candidate.BadTrackCount))
	}

	return s.registerPendingActionForContext(
		ctx,
		"lidarr_badly_rated_cleanup",
		"Delete badly rated albums",
		fmt.Sprintf("Delete %d badly rated album(s) from Lidarr after previewing the matches.", len(selected)),
		details,
		func(ctx context.Context) (string, error) {
			approvalCtx := context.WithValue(ctx, chatSessionKey, normalizeChatSessionID(sessionID))
			return s.executeBadlyRatedAlbumsCleanupApproval(approvalCtx, updatedAt, selection)
		},
	), len(selected), true
}
