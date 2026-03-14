package toolspec

import "testing"

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
}
