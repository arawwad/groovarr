package toolspec

import (
	"strings"
	"testing"
)

func TestPromptCatalogUsesSharedFilterSchemaStrings(t *testing.T) {
	catalog := PromptCatalog()
	byName := make(map[string]ToolSpec, len(catalog))
	for _, spec := range catalog {
		byName[spec.Name] = spec
	}

	if got := byName["artistListeningStats"].Schema; got != filterKeySchema(ArtistListeningStatsFilterKeys) {
		t.Fatalf("artistListeningStats schema = %q", got)
	}
	if got := byName["artistLibraryStats"].Schema; got != filterKeySchema(ArtistLibraryStatsFilterKeys) {
		t.Fatalf("artistLibraryStats schema = %q", got)
	}
	if got := byName["albumLibraryStats"].Schema; got != filterKeySchema(AlbumLibraryStatsFilterKeys) {
		t.Fatalf("albumLibraryStats schema = %q", got)
	}
	if got := byName["albumRelationshipStats"].Schema; got != filterKeySchema(AlbumRelationshipStatsFilterKeys) {
		t.Fatalf("albumRelationshipStats schema = %q", got)
	}
	if got := byName["libraryFacetCounts"].Schema; got != requiredFieldFilterKeySchema(LibraryFacetCountsFilterKeys) {
		t.Fatalf("libraryFacetCounts schema = %q", got)
	}
	if _, ok := byName["startPlaylistCreatePreview"]; !ok {
		t.Fatal("startPlaylistCreatePreview missing from prompt catalog")
	}
	if _, ok := byName["startPlaylistRefreshPreview"]; !ok {
		t.Fatal("startPlaylistRefreshPreview missing from prompt catalog")
	}
	if _, ok := byName["startPlaylistRepairPreview"]; !ok {
		t.Fatal("startPlaylistRepairPreview missing from prompt catalog")
	}
	if _, ok := byName["textToTrack"]; !ok {
		t.Fatal("textToTrack missing from prompt catalog")
	}
	if _, ok := byName["songPath"]; !ok {
		t.Fatal("songPath missing from prompt catalog")
	}
	if _, ok := byName["describeTrackSound"]; !ok {
		t.Fatal("describeTrackSound missing from prompt catalog")
	}
	if _, ok := byName["clusterScenes"]; !ok {
		t.Fatal("clusterScenes missing from prompt catalog")
	}
	if got := byName["startPlaylistCreatePreview"].Category; got != CategoryPlaylistPlanning {
		t.Fatalf("startPlaylistCreatePreview category = %q", got)
	}
	if got := byName["navidromePlaylist"].Category; got != CategoryPlaylistState {
		t.Fatalf("navidromePlaylist category = %q", got)
	}
	if got := byName["addOrQueueTrackToNavidromePlaylist"].Category; got != CategoryPlaylistActions {
		t.Fatalf("addOrQueueTrackToNavidromePlaylist category = %q", got)
	}
	if got := byName["removeTrackFromNavidromePlaylist"].Category; got != CategoryPlaylistActions {
		t.Fatalf("removeTrackFromNavidromePlaylist category = %q", got)
	}
	if got := byName["textToTrack"].Category; got != CategorySemanticSearch {
		t.Fatalf("textToTrack category = %q", got)
	}
	if got := byName["songPath"].Category; got != CategorySimilarity {
		t.Fatalf("songPath category = %q", got)
	}
	if got := byName["describeTrackSound"].Category; got != CategorySimilarity {
		t.Fatalf("describeTrackSound category = %q", got)
	}
	if got := byName["clusterScenes"].Category; got != CategorySimilarity {
		t.Fatalf("clusterScenes category = %q", got)
	}
}

func TestRenderPromptCatalogGroupsPlaylistTools(t *testing.T) {
	rendered := RenderPromptCatalog(PromptCatalog())
	required := []string{
		"Playlist Planning:\n- startPlaylistCreatePreview:",
		"Playlist State:\n- navidromePlaylists:",
		"\n- navidromePlaylist:",
		"Playlist Actions:\n- addOrQueueTrackToNavidromePlaylist:",
		"\n- addTrackToNavidromePlaylist:",
	}
	for _, fragment := range required {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("RenderPromptCatalog() missing %q", fragment)
		}
	}
	if strings.Contains(rendered, "\n- navidromePlaylistState:") {
		t.Fatal("RenderPromptCatalog() unexpectedly exposed navidromePlaylistState")
	}
}

func TestPromptCatalogForCategoriesFiltersByCategory(t *testing.T) {
	filtered := PromptCatalogForCategories([]string{CategoryDiscovery, CategoryCleanup})
	if len(filtered) == 0 {
		t.Fatal("PromptCatalogForCategories() returned no specs")
	}
	for _, spec := range filtered {
		if spec.Category != CategoryDiscovery && spec.Category != CategoryCleanup {
			t.Fatalf("unexpected category %q in filtered catalog", spec.Category)
		}
	}
}

func TestRenderPromptCategorySummaryListsGroups(t *testing.T) {
	rendered := RenderPromptCategorySummary(PromptCategoryCatalog())
	required := []string{
		"Tool groups:",
		"Library Browse: Library totals plus artist, album, and track lookups.",
		"Playlist Actions: Low-level add, remove, queue, or clear actions on playlists.",
		"Similarity: Nearest library matches for artists, albums, and tracks.",
	}
	for _, fragment := range required {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("RenderPromptCategorySummary() missing %q", fragment)
		}
	}
}
