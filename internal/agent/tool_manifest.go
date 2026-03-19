package agent

import (
	"fmt"
	"strings"

	"groovarr/internal/toolspec"
)

const (
	toolManifestModeFull   = "full"
	toolManifestModeRouted = "routed"
)

type toolManifestPrompt struct {
	Categories []string
	Content    string
}

type TurnSignals struct {
	Intent                 string
	QueryScope             string
	FollowupMode           string
	LibraryOnly            bool
	HasCreativeAlbumSet    bool
	HasSemanticAlbumSet    bool
	HasDiscoveredAlbums    bool
	HasRecentListening     bool
	HasPendingPlaylistPlan bool
	HasResolvedScene       bool
	HasSongPath            bool
	HasTrackCandidates     bool
	HasArtistCandidates    bool
}

func buildToolManifestPrompt(userMsg string, history []Message, signals *TurnSignals) toolManifestPrompt {
	return buildToolManifestPromptForMode(userMsg, history, signals, toolManifestMode())
}

func buildToolManifestPromptForMode(userMsg string, history []Message, signals *TurnSignals, mode string) toolManifestPrompt {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case toolManifestModeFull:
		categories := toolspec.PromptCategoryNames()
		return toolManifestPrompt{
			Categories: categories,
			Content:    toolspec.RenderPromptCatalog(toolspec.PromptCatalogForCategories(categories)),
		}
	default:
		categories := selectPromptCategories(userMsg, history, signals)
		return toolManifestPrompt{
			Categories: categories,
			Content:    toolspec.RenderPromptCatalog(toolspec.PromptCatalogForCategories(categories)),
		}
	}
}

func toolManifestMode() string {
	switch strings.ToLower(strings.TrimSpace(envString("AGENT_TOOL_MANIFEST_MODE", toolManifestModeRouted))) {
	case toolManifestModeFull:
		return toolManifestModeFull
	default:
		return toolManifestModeRouted
	}
}

func selectPromptCategories(userMsg string, history []Message, signals *TurnSignals) []string {
	selected := make(map[string]struct{}, 8)
	addPromptCategories(selected, defaultPromptCategories())
	addPromptCategories(selected, inferPromptCategories(userMsg))
	addPromptCategories(selected, inferPromptCategoriesFromSignals(signals))
	if shouldRouteFromHistory(userMsg) {
		for _, content := range recentHistoryContents(history, 4) {
			addPromptCategories(selected, inferPromptCategories(content))
		}
	}
	return toolspec.NormalizePromptCategories(categorySetToSlice(selected))
}

func defaultPromptCategories() []string {
	return []string{
		toolspec.CategoryLibraryBrowse,
		toolspec.CategoryListening,
		toolspec.CategorySemanticSearch,
		toolspec.CategorySimilarity,
	}
}

func inferPromptCategories(text string) []string {
	lower := normalizePromptRoutingText(text)
	if lower == "" {
		return nil
	}
	selected := make(map[string]struct{}, 6)

	if mentionsLibraryAnalytics(lower) {
		selected[toolspec.CategoryLibraryAnalytics] = struct{}{}
	}
	if mentionsDiscovery(lower) {
		selected[toolspec.CategoryDiscovery] = struct{}{}
	}
	if mentionsCleanup(lower) {
		selected[toolspec.CategoryCleanup] = struct{}{}
	}
	if strings.Contains(lower, "playlist") {
		selected[toolspec.CategoryPlaylistState] = struct{}{}
		if mentionsPlaylistPlanning(lower) {
			selected[toolspec.CategoryPlaylistPlanning] = struct{}{}
		}
		if mentionsPlaylistActions(lower) {
			selected[toolspec.CategoryPlaylistActions] = struct{}{}
		}
	}

	return categorySetToSlice(selected)
}

func inferPromptCategoriesFromSignals(signals *TurnSignals) []string {
	if signals == nil {
		return nil
	}
	selected := make(map[string]struct{}, 8)
	switch {
	case strings.EqualFold(strings.TrimSpace(signals.Intent), "album_discovery"):
		selected[toolspec.CategoryDiscovery] = struct{}{}
	case strings.EqualFold(strings.TrimSpace(signals.Intent), "track_discovery"):
		selected[toolspec.CategorySemanticSearch] = struct{}{}
		selected[toolspec.CategorySimilarity] = struct{}{}
	case strings.EqualFold(strings.TrimSpace(signals.Intent), "artist_discovery"):
		selected[toolspec.CategorySimilarity] = struct{}{}
	case strings.EqualFold(strings.TrimSpace(signals.Intent), "scene_discovery"):
		selected[toolspec.CategorySimilarity] = struct{}{}
	case strings.EqualFold(strings.TrimSpace(signals.Intent), "stats"):
		selected[toolspec.CategoryLibraryAnalytics] = struct{}{}
	case strings.EqualFold(strings.TrimSpace(signals.Intent), "playlist"):
		selected[toolspec.CategoryPlaylistState] = struct{}{}
		selected[toolspec.CategoryPlaylistPlanning] = struct{}{}
	case strings.EqualFold(strings.TrimSpace(signals.Intent), "listening"):
		selected[toolspec.CategoryListening] = struct{}{}
	}
	if strings.EqualFold(strings.TrimSpace(signals.QueryScope), "library") || signals.LibraryOnly {
		selected[toolspec.CategorySemanticSearch] = struct{}{}
		selected[toolspec.CategoryLibraryBrowse] = struct{}{}
	}
	if signals.HasRecentListening || strings.EqualFold(strings.TrimSpace(signals.QueryScope), "listening") {
		selected[toolspec.CategoryListening] = struct{}{}
	}
	if signals.HasCreativeAlbumSet || signals.HasSemanticAlbumSet {
		selected[toolspec.CategorySemanticSearch] = struct{}{}
		selected[toolspec.CategoryDiscovery] = struct{}{}
	}
	if signals.HasTrackCandidates || signals.HasArtistCandidates {
		selected[toolspec.CategorySimilarity] = struct{}{}
		selected[toolspec.CategorySemanticSearch] = struct{}{}
	}
	if signals.HasPendingPlaylistPlan {
		selected[toolspec.CategoryPlaylistState] = struct{}{}
		selected[toolspec.CategoryPlaylistPlanning] = struct{}{}
	}
	if strings.EqualFold(strings.TrimSpace(signals.FollowupMode), "query_previous_set") || strings.EqualFold(strings.TrimSpace(signals.FollowupMode), "refine_previous") {
		selected[toolspec.CategorySemanticSearch] = struct{}{}
		selected[toolspec.CategoryListening] = struct{}{}
	}
	return categorySetToSlice(selected)
}

func shouldRouteFromHistory(text string) bool {
	lower := normalizePromptRoutingText(text)
	if lower == "" {
		return false
	}
	if len(strings.Fields(lower)) > 14 {
		return false
	}
	cues := []string{
		"from those", "from them", "from that", "those ", "them ", "these ", "that ",
		"the last one", "the last ones", "same playlist", "same artist", "same album",
		"narrow that", "expand that", "revisit today", "what about those", "what about that",
	}
	for _, cue := range cues {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

func mentionsLibraryAnalytics(text string) bool {
	cues := []string{
		"how many", "count", "counts", "stats", "statistics", "breakdown", "distribution",
		"dominant", "most common", "least common", "facet", "facets", "genre breakdown",
		"genres", "decade", "decades", "year breakdown", "years",
	}
	for _, cue := range cues {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return false
}

func mentionsDiscovery(text string) bool {
	if strings.Contains(text, " in my library") || strings.Contains(text, " from my library") {
		return false
	}
	cues := []string{
		"recommend", "recommendation", "discover", "discovery", "suggest", "what should i listen",
		"best ", "top ", "essential ", "records like", "albums like", "like talk talk",
	}
	for _, cue := range cues {
		if strings.Contains(text, cue) {
			return true
		}
	}
	if (strings.Contains(text, "record") || strings.Contains(text, "album")) &&
		(strings.Contains(text, "give me") || strings.Contains(text, "show me") || strings.Contains(text, "find me")) {
		return true
	}
	return false
}

func mentionsCleanup(text string) bool {
	cues := []string{
		"remove ", "delete ", "cleanup", "clean up", "lidarr", "duplicate", "duplicates",
		"stale", "prune", "from my library",
	}
	for _, cue := range cues {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return false
}

func mentionsPlaylistPlanning(text string) bool {
	cues := []string{
		"make ", "create ", "build ", "append ", "add ", "refresh ", "repair ",
		"update ", "extend ",
	}
	for _, cue := range cues {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return false
}

func mentionsPlaylistActions(text string) bool {
	cues := []string{
		"remove ", "queue ", "clear ", "delete ", "add ", "pending ",
	}
	for _, cue := range cues {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return false
}

func recentHistoryContents(history []Message, limit int) []string {
	if len(history) == 0 || limit <= 0 {
		return nil
	}
	if limit > len(history) {
		limit = len(history)
	}
	start := len(history) - limit
	items := make([]string, 0, limit)
	for _, message := range history[start:] {
		content := strings.TrimSpace(message.Content)
		if content != "" {
			items = append(items, content)
		}
	}
	return items
}

func addPromptCategories(target map[string]struct{}, categories []string) {
	for _, category := range categories {
		category = strings.TrimSpace(category)
		if category == "" {
			continue
		}
		target[category] = struct{}{}
	}
}

func categorySetToSlice(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	items := make([]string, 0, len(set))
	for category := range set {
		items = append(items, category)
	}
	return items
}

func normalizePromptRoutingText(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"\n", " ",
		"\t", " ",
		",", " ",
		".", " ",
		"?", " ",
		"!", " ",
		":", " ",
		";", " ",
		"(", " ",
		")", " ",
	)
	return strings.Join(strings.Fields(replacer.Replace(text)), " ")
}

func buildToolManifestContext(prompt toolManifestPrompt) string {
	categories := strings.Join(prompt.Categories, ", ")
	if categories == "" {
		categories = "none"
	}
	return fmt.Sprintf(
		"Tool manifest for this turn.\nLikely tool groups: %s.\nThis may be a routed subset of all tools. Use only the tools listed below; if none fit, ask one concise clarifying question.\n\n%s",
		categories,
		prompt.Content,
	)
}
