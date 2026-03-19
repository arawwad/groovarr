package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"groovarr/internal/discovery"
)

type resultSetSelection struct {
	Selection string
	Value     any
	FocusKey  string
}

type resultSetActionResult struct {
	Kind          string
	PendingAction *PendingAction
	Selection     resultSetSelection
	Matches       []lidarrAlbumMatch
	Scene         *sceneSessionItem
	Count         int
	EmptyReason   string
	MissingOnly   bool
}

func (s resultSetSelection) discoveredAlbums() ([]discoveredAlbumCandidate, bool) {
	items, ok := s.Value.([]discoveredAlbumCandidate)
	return items, ok
}

func (s resultSetSelection) cleanupCandidates() ([]lidarrCleanupCandidate, bool) {
	items, ok := s.Value.([]lidarrCleanupCandidate)
	return items, ok
}

func (s resultSetSelection) badlyRatedAlbums() ([]badlyRatedAlbumCandidate, bool) {
	items, ok := s.Value.([]badlyRatedAlbumCandidate)
	return items, ok
}

func (s resultSetSelection) sceneCandidate() (*sceneSessionItem, bool) {
	item, ok := s.Value.(*sceneSessionItem)
	return item, ok
}

type resultSetAdapter interface {
	Kind() string
	Capability() resultSetCapability
	ResolveSelection(sessionID string, ref resolvedResultReference) (resultSetSelection, bool)
	HandleAction(ctx context.Context, s *Server, sessionID string, ref resolvedResultReference, selection resultSetSelection) (resultSetActionResult, bool)
}

var resultSetAdapters = map[string]resultSetAdapter{
	"discovered_albums":  discoveredAlbumsAdapter{},
	"cleanup_candidates": cleanupCandidatesAdapter{},
	"badly_rated_albums": badlyRatedAlbumsAdapter{},
	"scene_candidates":   sceneCandidatesAdapter{},
}

func currentAdapterResultSetCapabilities() []resultSetCapability {
	capabilities := make([]resultSetCapability, 0, len(resultSetAdapters))
	for _, adapter := range resultSetAdapters {
		capabilities = append(capabilities, adapter.Capability())
	}
	sort.Slice(capabilities, func(i, j int) bool {
		return capabilities[i].SetKind < capabilities[j].SetKind
	})
	return capabilities
}

func resolveResultSetSelection(sessionID string, ref resolvedResultReference) (resultSetSelection, bool) {
	adapter, ok := resultSetAdapters[ref.effectiveSetKind()]
	if !ok {
		return resultSetSelection{}, false
	}
	return adapter.ResolveSelection(sessionID, ref)
}

func handleResultSetAction(ctx context.Context, s *Server, sessionID string, ref resolvedResultReference) (resultSetActionResult, bool) {
	adapter, ok := resultSetAdapters[ref.effectiveSetKind()]
	if !ok {
		return resultSetActionResult{}, false
	}
	selection, selected := adapter.ResolveSelection(sessionID, ref)
	outcome, handled := adapter.HandleAction(ctx, s, sessionID, ref, selection)
	if handled {
		return outcome, true
	}
	if !selected {
		return resultSetActionResult{}, false
	}
	return resultSetActionResult{}, false
}

type discoveredAlbumsAdapter struct{}

func (discoveredAlbumsAdapter) Kind() string { return "discovered_albums" }
func (discoveredAlbumsAdapter) Capability() resultSetCapability {
	return resultSetCapability{
		SetKind:    "discovered_albums",
		Operations: []string{"inspect_availability", "preview_apply"},
		Selectors:  []string{"all", "top_n", "ordinal", "explicit_names", "missing_only", "item_key"},
	}
}

func (discoveredAlbumsAdapter) ResolveSelection(sessionID string, ref resolvedResultReference) (resultSetSelection, bool) {
	candidates, updatedAt, _ := getLastDiscoveredAlbums(sessionID)
	if len(candidates) == 0 || updatedAt.IsZero() || time.Since(updatedAt) > llmContextDiscoveredAlbumsTTL {
		return resultSetSelection{}, false
	}
	if selected, selection, ok := selectFocusedDiscoveredCandidate(candidates, ref.ResolvedItemKey); ok {
		out := resultSetSelection{Selection: selection, Value: selected}
		if len(selected) == 1 {
			out.FocusKey = normalizedDiscoveredAlbumCandidateKey(selected[0])
		}
		return out, true
	}
	if ref.Selection.Mode == "missing_only" {
		return resultSetSelection{Selection: "all", Value: candidates}, true
	}
	selection, ok := buildDiscoveredAlbumSelection(ref.resultReference, len(candidates))
	if !ok {
		return resultSetSelection{}, false
	}
	if strings.EqualFold(strings.TrimSpace(selection), "all") {
		return resultSetSelection{Selection: "all", Value: candidates}, true
	}
	selected, err := discovery.SelectCandidates(candidates, selection)
	if err != nil {
		return resultSetSelection{}, false
	}
	out := resultSetSelection{Selection: selection, Value: selected}
	if len(selected) == 1 {
		out.FocusKey = normalizedDiscoveredAlbumCandidateKey(selected[0])
	}
	return out, true
}

func (discoveredAlbumsAdapter) HandleAction(ctx context.Context, s *Server, sessionID string, ref resolvedResultReference, selection resultSetSelection) (resultSetActionResult, bool) {
	candidates, updatedAt, sourceQuery := getLastDiscoveredAlbums(sessionID)
	if len(candidates) == 0 || updatedAt.IsZero() || time.Since(updatedAt) > llmContextDiscoveredAlbumsTTL {
		return resultSetActionResult{}, false
	}
	selected, ok := selection.discoveredAlbums()
	if !ok || len(selected) == 0 {
		return resultSetActionResult{}, false
	}
	switch ref.Action {
	case "inspect_availability":
		matches, err := s.matchSelectedDiscoveredAlbums(ctx, selected, updatedAt, sourceQuery)
		if err != nil {
			if ref.Selection.Mode == "missing_only" {
				return resultSetActionResult{Kind: "discovered_availability_error", MissingOnly: true}, true
			}
			return resultSetActionResult{}, false
		}
		if len(matches) == 0 {
			return resultSetActionResult{}, false
		}
		if ref.Selection.Mode == "missing_only" {
			filtered, filteredMatches := filterDiscoveredMatchesMissingOnly(selected, matches)
			if len(filtered) == 0 {
				return resultSetActionResult{Kind: "discovered_availability_empty", MissingOnly: true}, true
			}
			if len(filtered) == 1 {
				setLastFocusedResultItem(sessionID, "discovered_albums", normalizedDiscoveredAlbumCandidateKey(filtered[0]))
			}
			return resultSetActionResult{
				Kind:        "discovered_availability",
				Selection:   resultSetSelection{Selection: buildDiscoveredAlbumRankSelection(filtered), Value: filtered},
				Matches:     filteredMatches,
				MissingOnly: true,
			}, true
		}
		if selection.FocusKey != "" {
			setLastFocusedResultItem(sessionID, "discovered_albums", selection.FocusKey)
		}
		return resultSetActionResult{
			Kind:      "discovered_availability",
			Selection: selection,
			Matches:   matches,
		}, true
	case "preview_apply":
		actionSelection := selection.Selection
		if ref.Selection.Mode == "missing_only" {
			filtered, _, err := s.matchAndFilterMissingDiscoveredCandidates(ctx, candidates, updatedAt, sourceQuery)
			if err != nil {
				return resultSetActionResult{Kind: "discovered_preview_error", MissingOnly: true}, true
			}
			if len(filtered) == 0 {
				return resultSetActionResult{Kind: "discovered_preview_empty", MissingOnly: true}, true
			}
			selected = filtered
			actionSelection = buildDiscoveredAlbumRankSelection(filtered)
			if actionSelection == "" {
				return resultSetActionResult{}, false
			}
		}
		if selection.FocusKey != "" {
			setLastFocusedResultItem(sessionID, "discovered_albums", selection.FocusKey)
		}
		resp, pendingAction, err := s.startDiscoveredAlbumsApplyPreview(ctx, actionSelection)
		if err != nil || strings.TrimSpace(resp) == "" {
			return resultSetActionResult{}, false
		}
		return resultSetActionResult{
			Kind:          "discovered_preview_apply",
			PendingAction: pendingAction,
			Selection:     resultSetSelection{Selection: actionSelection, Value: selected, FocusKey: selection.FocusKey},
		}, true
	default:
		return resultSetActionResult{}, false
	}
}

type cleanupCandidatesAdapter struct{}

func (cleanupCandidatesAdapter) Kind() string { return "cleanup_candidates" }
func (cleanupCandidatesAdapter) Capability() resultSetCapability {
	return resultSetCapability{
		SetKind:    "cleanup_candidates",
		Operations: []string{"preview_apply"},
		Selectors:  []string{"all", "top_n", "ordinal", "explicit_names", "item_key"},
	}
}

func (cleanupCandidatesAdapter) ResolveSelection(sessionID string, ref resolvedResultReference) (resultSetSelection, bool) {
	candidates, updatedAt := getLastLidarrCandidates(sessionID)
	if len(candidates) == 0 || updatedAt.IsZero() || time.Since(updatedAt) > llmContextCleanupTTL {
		return resultSetSelection{}, false
	}
	selection := buildCleanupSelection(ref, "cleanup_candidates", candidates, nil)
	selected, err := selectCleanupCandidates(selection, candidates)
	if err != nil {
		return resultSetSelection{}, false
	}
	out := resultSetSelection{Selection: selection, Value: selected}
	if len(selected) == 1 {
		out.FocusKey = normalizedCleanupCandidateKey(selected[0])
	}
	return out, true
}

func (cleanupCandidatesAdapter) HandleAction(ctx context.Context, s *Server, sessionID string, ref resolvedResultReference, selection resultSetSelection) (resultSetActionResult, bool) {
	if ref.Action != "preview_apply" {
		return resultSetActionResult{}, false
	}
	if selection.FocusKey != "" {
		setLastFocusedResultItem(sessionID, "cleanup_candidates", selection.FocusKey)
	}
	resp, pendingAction, err := s.startLidarrCleanupApplyPreview(ctx, "", selection.Selection)
	if err != nil || strings.TrimSpace(resp) == "" {
		return resultSetActionResult{}, false
	}
	return resultSetActionResult{
		Kind:          "cleanup_preview_apply",
		PendingAction: pendingAction,
		Selection:     selection,
	}, true
}

type badlyRatedAlbumsAdapter struct{}

func (badlyRatedAlbumsAdapter) Kind() string { return "badly_rated_albums" }
func (badlyRatedAlbumsAdapter) Capability() resultSetCapability {
	return resultSetCapability{
		SetKind:    "badly_rated_albums",
		Operations: []string{"preview_apply"},
		Selectors:  []string{"all", "top_n", "ordinal", "explicit_names", "item_key"},
	}
}

func (badlyRatedAlbumsAdapter) ResolveSelection(sessionID string, ref resolvedResultReference) (resultSetSelection, bool) {
	candidates, updatedAt := getLastBadlyRatedAlbums(sessionID)
	if len(candidates) == 0 || updatedAt.IsZero() || time.Since(updatedAt) > llmContextBadlyRatedAlbumsTTL {
		return resultSetSelection{}, false
	}
	selection := buildCleanupSelection(ref, "badly_rated_albums", nil, candidates)
	selected, err := selectBadlyRatedAlbums(selection, candidates)
	if err != nil {
		return resultSetSelection{}, false
	}
	out := resultSetSelection{Selection: selection, Value: selected}
	if len(selected) == 1 {
		out.FocusKey = normalizedBadlyRatedAlbumCandidateKey(selected[0])
	}
	return out, true
}

func (badlyRatedAlbumsAdapter) HandleAction(ctx context.Context, s *Server, sessionID string, ref resolvedResultReference, selection resultSetSelection) (resultSetActionResult, bool) {
	if ref.Action != "preview_apply" {
		return resultSetActionResult{}, false
	}
	selectionLabel := strings.TrimSpace(selection.Selection)
	if selectionLabel == "" {
		selectionLabel = "all"
	}
	action, selectedCount, ok := s.buildBadlyRatedAlbumsCleanupPendingAction(ctx, selectionLabel, time.Time{})
	if ok {
		if selection.FocusKey != "" {
			setLastFocusedResultItem(sessionID, "badly_rated_albums", selection.FocusKey)
		}
		return resultSetActionResult{
			Kind:          "badly_rated_preview_apply",
			PendingAction: action,
			Count:         selectedCount,
		}, true
	}
	if candidates, _, recent := recentBadlyRatedAlbumsState(sessionID, time.Now().UTC()); recent {
		if len(candidates) > 0 {
			return resultSetActionResult{}, false
		}
	} else {
		memory, found := s.latestChatSessionMemory(sessionID)
		if !found || !recentRequestLooksLikeBadlyRatedAlbums(memory) {
			return resultSetActionResult{}, false
		}
	}
	if selectionLabel == "" || strings.EqualFold(selectionLabel, "all") {
		return resultSetActionResult{Kind: "badly_rated_empty_all"}, true
	}
	return resultSetActionResult{Kind: "badly_rated_empty_selection"}, true
}

type sceneCandidatesAdapter struct{}

func (sceneCandidatesAdapter) Kind() string { return "scene_candidates" }
func (sceneCandidatesAdapter) Capability() resultSetCapability {
	return resultSetCapability{
		SetKind:    "scene_candidates",
		Operations: []string{"select_candidate"},
		Selectors:  []string{"ordinal", "explicit_names", "count_match"},
	}
}

func (sceneCandidatesAdapter) ResolveSelection(sessionID string, ref resolvedResultReference) (resultSetSelection, bool) {
	state, ok := getLastSceneSelection(sessionID)
	if !ok || state.UpdatedAt.IsZero() || time.Since(state.UpdatedAt) > llmContextSceneTTL {
		return resultSetSelection{}, false
	}
	candidates := state.Candidates
	if len(candidates) == 0 && state.Resolved != nil {
		candidates = []sceneSessionItem{*state.Resolved}
	}
	if len(candidates) == 0 {
		return resultSetSelection{}, false
	}
	selected, ok := resolveSceneCandidateFromReference(ref, candidates)
	if !ok || selected == nil {
		return resultSetSelection{}, false
	}
	return resultSetSelection{
		Selection: buildSceneSelectionLabel(ref),
		Value:     selected,
	}, true
}

func (sceneCandidatesAdapter) HandleAction(_ context.Context, _ *Server, sessionID string, ref resolvedResultReference, selection resultSetSelection) (resultSetActionResult, bool) {
	if ref.Action != "select_candidate" {
		return resultSetActionResult{}, false
	}
	selected, ok := selection.sceneCandidate()
	if !ok || selected == nil {
		return resultSetActionResult{}, false
	}
	setLastSceneSelection(sessionID, selected, nil)
	return resultSetActionResult{Kind: "scene_select", Scene: selected}, true
}

func buildDiscoveredAlbumSelection(ref resultReference, total int) (string, bool) {
	if total <= 0 {
		return "", false
	}
	switch ref.Selection.Mode {
	case "", "none", "all":
		return "all", true
	case "top_n":
		n, ok := parseSmallNumberWord(ref.Selection.Value)
		if !ok {
			if parsed, err := strconv.Atoi(strings.TrimSpace(ref.Selection.Value)); err == nil {
				n = parsed
				ok = true
			}
		}
		if !ok || n <= 0 {
			return "", false
		}
		if n > total {
			n = total
		}
		return fmt.Sprintf("first %d", n), true
	case "ordinal":
		normalized := normalizeDiscoveredAlbumRankList(ref.Selection.Value)
		return normalized, normalized != ""
	case "explicit_names":
		value := strings.TrimSpace(ref.Selection.Value)
		return value, value != ""
	case "missing_only":
		return "", false
	default:
		return "", false
	}
}

func buildCleanupSelection(
	ref resolvedResultReference,
	resultSetKind string,
	cleanupCandidates []lidarrCleanupCandidate,
	badlyRatedCandidates []badlyRatedAlbumCandidate,
) string {
	if ref.ResolvedItemKey != "" {
		switch strings.TrimSpace(resultSetKind) {
		case "cleanup_candidates":
			if selection, ok := selectionFromFocusedCleanupCandidate(cleanupCandidates, ref.ResolvedItemKey); ok {
				return selection
			}
		case "badly_rated_albums":
			if selection, ok := selectionFromFocusedBadlyRatedAlbum(badlyRatedCandidates, ref.ResolvedItemKey); ok {
				return selection
			}
		}
	}
	switch ref.Selection.Mode {
	case "", "none", "all":
		return "all"
	case "top_n":
		if count, ok := parseTurnSelectionCount(ref.Selection.Value); ok {
			return fmt.Sprintf("first %d", count)
		}
	case "ordinal", "explicit_names":
		if value := strings.TrimSpace(ref.Selection.Value); value != "" {
			return value
		}
	}
	return "all"
}

func buildSceneSelectionLabel(ref resolvedResultReference) string {
	if ref.Selection.Value != "" {
		return ref.Selection.Value
	}
	return ref.Selection.Mode
}

func buildDiscoveredAlbumSelectionFromTurn(turn normalizedTurn, total int) (string, bool) {
	return buildDiscoveredAlbumSelection(turn.resultReference(), total)
}

func buildCleanupSelectionFromResolved(
	resolved *resolvedTurnContext,
	resultSetKind string,
	cleanupCandidates []lidarrCleanupCandidate,
	badlyRatedCandidates []badlyRatedAlbumCandidate,
) string {
	if resolved == nil {
		return "all"
	}
	return buildCleanupSelection(resolved.resultReference(), resultSetKind, cleanupCandidates, badlyRatedCandidates)
}

func selectDiscoveredCandidatesFromResolved(resolved *resolvedTurnContext, candidates []discoveredAlbumCandidate) ([]discoveredAlbumCandidate, string, bool) {
	if resolved == nil || len(candidates) == 0 {
		return nil, "", false
	}
	ref := resolved.resultReference()
	if selected, selection, ok := selectFocusedDiscoveredCandidate(candidates, ref.ResolvedItemKey); ok {
		return selected, selection, true
	}
	if ref.Selection.Mode == "missing_only" {
		return candidates, "all", true
	}
	selection, ok := buildDiscoveredAlbumSelection(ref.resultReference, len(candidates))
	if !ok {
		return nil, "", false
	}
	if strings.EqualFold(strings.TrimSpace(selection), "all") {
		return candidates, "all", true
	}
	selected, err := discovery.SelectCandidates(candidates, selection)
	if err != nil {
		return nil, "", false
	}
	return selected, selection, true
}
