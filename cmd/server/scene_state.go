package main

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

const llmContextSceneTTL = 30 * time.Minute

type sceneSessionItem struct {
	Key          string
	Name         string
	Subtitle     string
	SongCount    int
	SampleTracks []audioMusePlaylistSong
}

type sceneSessionState struct {
	Resolved   *sceneSessionItem
	Candidates []sceneSessionItem
	UpdatedAt  time.Time
}

type sceneSelectionStore struct {
	mu       sync.RWMutex
	sessions map[string]sceneSessionState
}

var lastSceneSelection = newSceneSelectionStore()

func newSceneSelectionStore() *sceneSelectionStore {
	return &sceneSelectionStore{sessions: make(map[string]sceneSessionState)}
}

func (s *sceneSelectionStore) Set(sessionID string, resolved *sceneSessionItem, candidates []sceneSessionItem) {
	s.mu.Lock()
	s.sessions[normalizeChatSessionID(sessionID)] = sceneSessionState{
		Resolved:   cloneSceneSessionItemPtr(resolved),
		Candidates: cloneSceneSessionItems(candidates),
		UpdatedAt:  time.Now().UTC(),
	}
	s.mu.Unlock()
}

func (s *sceneSelectionStore) Get(sessionID string) (sceneSessionState, bool) {
	s.mu.RLock()
	state, ok := s.sessions[normalizeChatSessionID(sessionID)]
	s.mu.RUnlock()
	if !ok {
		return sceneSessionState{}, false
	}
	return sceneSessionState{
		Resolved:   cloneSceneSessionItemPtr(state.Resolved),
		Candidates: cloneSceneSessionItems(state.Candidates),
		UpdatedAt:  state.UpdatedAt,
	}, true
}

func setLastSceneSelection(sessionID string, resolved *sceneSessionItem, candidates []sceneSessionItem) {
	newTurnSessionMemoryWriter(sessionID).SetSceneSelection(resolved, candidates)
}

func getLastSceneSelection(sessionID string) (sceneSessionState, bool) {
	return lastSceneSelection.Get(sessionID)
}

func sceneSessionItemFromPlaylist(scene audioMuseClusterPlaylist) sceneSessionItem {
	sampleCount := minInt(len(scene.Songs), 5)
	return sceneSessionItem{
		Key:          strings.TrimSpace(scene.Key),
		Name:         strings.TrimSpace(scene.Name),
		Subtitle:     strings.TrimSpace(scene.Subtitle),
		SongCount:    scene.SongCount,
		SampleTracks: append([]audioMusePlaylistSong(nil), scene.Songs[:sampleCount]...),
	}
}

func cloneSceneSessionItemPtr(item *sceneSessionItem) *sceneSessionItem {
	if item == nil {
		return nil
	}
	copied := *item
	copied.SampleTracks = append([]audioMusePlaylistSong(nil), item.SampleTracks...)
	return &copied
}

func cloneSceneSessionItems(items []sceneSessionItem) []sceneSessionItem {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]sceneSessionItem, 0, len(items))
	for _, item := range items {
		copied := item
		copied.SampleTracks = append([]audioMusePlaylistSong(nil), item.SampleTracks...)
		cloned = append(cloned, copied)
	}
	return cloned
}

func formatSceneSelectionContext(state sceneSessionState, now time.Time) string {
	if state.UpdatedAt.IsZero() || now.Sub(state.UpdatedAt) > llmContextSceneTTL {
		return ""
	}
	if state.Resolved != nil {
		parts := []string{
			fmt.Sprintf("last_scene: key=%q", strings.TrimSpace(state.Resolved.Key)),
			fmt.Sprintf("name=%q", strings.TrimSpace(state.Resolved.Name)),
			fmt.Sprintf("song_count=%d", state.Resolved.SongCount),
		}
		if subtitle := strings.TrimSpace(state.Resolved.Subtitle); subtitle != "" {
			parts = append(parts, fmt.Sprintf("subtitle=%q", subtitle))
		}
		if sample := formatSceneSampleTracks(state.Resolved.SampleTracks); sample != "" {
			parts = append(parts, fmt.Sprintf("sample=%q", sample))
		}
		return strings.Join(parts, "; ")
	}
	if len(state.Candidates) == 0 {
		return ""
	}
	items := make([]string, 0, len(state.Candidates))
	for _, candidate := range state.Candidates {
		label := strings.TrimSpace(candidate.Name)
		if subtitle := strings.TrimSpace(candidate.Subtitle); subtitle != "" {
			label += " [" + subtitle + "]"
		}
		label += fmt.Sprintf(" (%d tracks)", candidate.SongCount)
		items = append(items, label)
	}
	return fmt.Sprintf("last_scene_options: count=%d; candidates=%q", len(state.Candidates), strings.Join(items, " | "))
}

func formatSceneSampleTracks(tracks []audioMusePlaylistSong) string {
	if len(tracks) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tracks))
	for _, track := range tracks {
		title := strings.TrimSpace(track.Title)
		if title == "" {
			continue
		}
		author := strings.TrimSpace(track.Author)
		if author != "" {
			parts = append(parts, fmt.Sprintf("%s by %s", title, author))
			continue
		}
		parts = append(parts, title)
	}
	return strings.Join(parts, " | ")
}

var sceneTrackCountPattern = regexp.MustCompile(`\b(\d{1,4})\s*(track|tracks|song|songs)\b`)

func resolveSceneCandidateFromMessage(msg string, candidates []sceneSessionItem) (*sceneSessionItem, bool) {
	if len(candidates) == 0 {
		return nil, false
	}
	normalized := normalizeReferenceText(msg)
	if normalized == "" {
		return nil, false
	}
	for _, candidate := range candidates {
		for _, field := range []string{candidate.Key, candidate.Name, candidate.Subtitle} {
			fieldKey := normalizeReferenceText(field)
			if fieldKey != "" && strings.Contains(normalized, fieldKey) {
				resolved := candidate
				return &resolved, true
			}
		}
	}
	if match := sceneTrackCountPattern.FindStringSubmatch(strings.ToLower(msg)); len(match) == 3 {
		count := strings.TrimSpace(match[1])
		if count != "" {
			matches := make([]sceneSessionItem, 0, 2)
			for _, candidate := range candidates {
				if fmt.Sprintf("%d", candidate.SongCount) == count {
					matches = append(matches, candidate)
				}
			}
			if len(matches) == 1 {
				resolved := matches[0]
				return &resolved, true
			}
		}
	}
	switch {
	case strings.Contains(normalized, "first") || strings.Contains(normalized, "1st"):
		resolved := candidates[0]
		return &resolved, true
	case len(candidates) >= 2 && (strings.Contains(normalized, "second") || strings.Contains(normalized, "2nd")):
		resolved := candidates[1]
		return &resolved, true
	case len(candidates) >= 3 && (strings.Contains(normalized, "third") || strings.Contains(normalized, "3rd")):
		resolved := candidates[2]
		return &resolved, true
	case strings.Contains(normalized, "last"):
		resolved := candidates[len(candidates)-1]
		return &resolved, true
	default:
		return nil, false
	}
}
