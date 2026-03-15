package toolspec

import "strings"

type CategorySpec struct {
	Name        string
	Description string
}

func PromptCategoryCatalog() []CategorySpec {
	return []CategorySpec{
		{Name: CategoryLibraryBrowse, Description: "Library totals plus artist, album, and track lookups."},
		{Name: CategoryListening, Description: "Recent listening windows and artist listening summaries."},
		{Name: CategoryLibraryAnalytics, Description: "Exact grouped counts, distributions, and library composition stats."},
		{Name: CategorySemanticSearch, Description: "Vibe and mood matching inside the owned library."},
		{Name: CategoryDiscovery, Description: "Recommendations beyond the owned library."},
		{Name: CategoryPlaylistPlanning, Description: "Preview creating, extending, refreshing, or repairing playlists."},
		{Name: CategoryPlaylistState, Description: "Inspect saved playlists and pending playlist state."},
		{Name: CategoryPlaylistActions, Description: "Low-level add, remove, queue, or clear actions on playlists."},
		{Name: CategoryCleanup, Description: "Preview cleanup and artist-removal actions."},
		{Name: CategorySimilarity, Description: "Nearest library matches for artists, albums, and tracks."},
	}
}

func PromptCategoryNames() []string {
	catalog := PromptCategoryCatalog()
	names := make([]string, 0, len(catalog))
	for _, category := range catalog {
		names = append(names, category.Name)
	}
	return names
}

func NormalizePromptCategories(categories []string) []string {
	if len(categories) == 0 {
		return PromptCategoryNames()
	}
	seen := make(map[string]struct{}, len(categories))
	for _, category := range categories {
		category = strings.TrimSpace(category)
		if category == "" {
			continue
		}
		seen[category] = struct{}{}
	}
	normalized := make([]string, 0, len(seen))
	for _, category := range PromptCategoryNames() {
		if _, ok := seen[category]; ok {
			normalized = append(normalized, category)
		}
	}
	if len(normalized) == 0 {
		return PromptCategoryNames()
	}
	return normalized
}

func PromptCatalogForCategories(categories []string) []ToolSpec {
	allowed := NormalizePromptCategories(categories)
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, category := range allowed {
		allowedSet[category] = struct{}{}
	}
	filtered := make([]ToolSpec, 0, len(PromptCatalog()))
	for _, spec := range PromptCatalog() {
		if _, ok := allowedSet[spec.Category]; ok {
			filtered = append(filtered, spec)
		}
	}
	return filtered
}
