package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCompactToolResultForPromptCompactsJSON(t *testing.T) {
	raw := "{\n  \"data\": { \"value\": 1 }\n}\n"
	got := compactToolResultForPrompt(raw, 100)
	if got != `{"data":{"value":1}}` {
		t.Fatalf("compactToolResultForPrompt() = %q", got)
	}
}

func TestCompactToolResultForPromptTruncates(t *testing.T) {
	raw := strings.Repeat("abcdef", 20)
	got := compactToolResultForPrompt(raw, 25)
	if !strings.HasSuffix(got, "... [truncated]") {
		t.Fatalf("compactToolResultForPrompt() = %q, want truncated suffix", got)
	}
}

func TestBuildSystemPromptOmitsFormattedDate(t *testing.T) {
	got := buildSystemPrompt()
	if strings.Contains(got, "Current date: Sunday, March 8, 2026") {
		t.Fatalf("buildSystemPrompt() unexpectedly includes formatted date: %q", got)
	}
}

func TestBuildLegacySystemPromptIncludesFormattedDate(t *testing.T) {
	got := buildLegacySystemPrompt(time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC))
	if !strings.Contains(got, "Current date: Sunday, March 8, 2026") {
		t.Fatalf("buildLegacySystemPrompt() missing formatted date: %q", got)
	}
}

func TestBuildRuntimeContextIncludesFormattedDate(t *testing.T) {
	got := buildRuntimeContext(time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC))
	if got != "Authoritative runtime context:\nCurrent date: Sunday, March 8, 2026" {
		t.Fatalf("buildRuntimeContext() = %q", got)
	}
}

func TestBuildSystemPromptUsesManifestGuidance(t *testing.T) {
	got := buildSystemPrompt()
	disallowed := []string{
		"Tool selection hints:",
		`For direct removal requests like "remove Ulver from my library"`,
		`For conversational playlist requests like "make me a melancholy jazz playlist"`,
	}
	for _, fragment := range disallowed {
		if strings.Contains(got, fragment) {
			t.Fatalf("buildSystemPrompt() unexpectedly contains %q", fragment)
		}
	}
	required := []string{
		"Tool manifest:",
		"Use only the tools listed below. Pick the tool that best matches the user's intent.",
		"Derive the user's intent from the latest message",
		"ask one concise clarifying question instead of guessing",
		"Do not answer library-stat or library-count questions from model memory.",
		"For exact counts, prefer stats or facet tools over counting a capped list.",
		"Reuse prior artists or albums in follow-ups, and prefer multi-value tool args when available.",
		"Preserve the original subject when narrowing prior recommendation or semantic-search results, then add explicit filters.",
		"For decade/year follow-ups on semanticAlbumSearch, keep queryText and add minYear/maxYear.",
		"Recommendations are global by default. Use discoverAlbums unless the user explicitly limits them to their library.",
		`For "best/top/essential <artist>" prompts, use discoverAlbums unless the user says "in my library"; then use albums.`,
		"For library-only vibe recommendations, prefer semanticAlbumSearch over albums or discoverAlbums.",
		"Do not invent tool names, arg names, filter keys, or enum values.",
		"If you cannot identify one best tool with valid arguments, ask a clarifying question.",
		`If the user asks for vague "stats", ask whether they mean library composition or listening over time.`,
		"Clarification examples:",
		`Assistant: {"action":"respond","response":"Do you want artist library stats or artist listening stats over a time window?"}`,
		"Preview before state-changing operations.",
		"discoverAlbums: Discover albums beyond the user's current library.",
		"startArtistRemovalPreview: Prepare a preview for removing an artist from the library.",
		"libraryFacetCounts: Facet counts such as genres, years, or decades.",
		"albums: List albums in the user's library.",
		"args: artistName:string; artistNames:array<string>; queryText:string; genre:string; year:number; unplayed:boolean; notPlayedSince:string; rating:number; ratingBelow:number; sortBy:string; limit:number",
		"schema: filter keys: artistName, artistNames, genre, exactAlbums, minAlbums, maxAlbums, minTotalPlays, maxTotalPlays, inactiveSince, notPlayedSince, playedSince, playedUntil, maxPlaysInWindow",
		"args: field:string*; filter:object; limit:number",
		"schema: filter keys: artistName, playedSince, playedUntil, minPlaysInWindow, maxPlaysInWindow, minAlbums, maxAlbums",
		"schema: field is required; filter keys: genre, artistName, year, minYear, maxYear, unplayed, notPlayedSince",
		`example: {"action":"query","tool":"discoverAlbums","args":{"query":"records like Talk Talk's Laughing Stock but warmer and more spacious","limit":5}}`,
		`example: {"action":"query","tool":"artistListeningStats","args":{"filter":{"playedSince":"2026-02-10","playedUntil":"2026-03-10"},"sort":"plays","limit":10}}`,
		`Assistant: {"action":"query","tool":"artistLibraryStats","args":{"filter":{"artistName":"Pink Floyd"},"sort":"albums","limit":5}}`,
		`Assistant: {"action":"query","tool":"artistLibraryStats","args":{"filter":{"artistNames":["Radiohead","The Beatles"]},"sort":"albums","limit":10}}`,
		`Assistant: {"action":"query","tool":"badlyRatedAlbums","args":{"limit":20,"maxTrackDetails":3}}`,
		`Assistant: {"action":"query","tool":"albums","args":{"artistNames":["Radiohead","The Beatles","Pink Floyd"],"sortBy":"rating","limit":12}}`,
		`Assistant: {"action":"respond","response":"I can prepare a cleanup preview for those albums."}`,
		`Assistant: {"action":"query","tool":"discoverAlbums","args":{"query":"three records for a rainy late-night walk","limit":3}}`,
		`Assistant: {"action":"query","tool":"discoverAlbums","args":{"query":"best 5 Bjork albums","limit":5}}`,
		`Assistant: {"action":"query","tool":"albums","args":{"artistName":"Bjork","sortBy":"rating","limit":5}}`,
		`Assistant: {"action":"query","tool":"semanticAlbumSearch","args":{"queryText":"melancholic dream pop","minYear":1990,"maxYear":1999,"limit":6}}`,
		`example: {"action":"query","tool":"startArtistRemovalPreview","args":{"artistName":"Warpaint"}}`,
		`example: {"action":"query","tool":"semanticAlbumSearch","args":{"queryText":"melancholic dream pop","minYear":1990,"maxYear":1999,"limit":5}}`,
		`badlyRatedAlbums: Find albums in the user's library that contain any badly rated tracks.`,
	}
	for _, fragment := range required {
		if !strings.Contains(got, fragment) {
			t.Fatalf("buildSystemPrompt() missing %q", fragment)
		}
	}
}

func TestBuildSystemPromptIsCompact(t *testing.T) {
	got := buildSystemPrompt()
	if len(got) > 15000 {
		t.Fatalf("buildSystemPrompt() too long: %d chars", len(got))
	}
}

func TestBuildConversationWithRuntimePlacesRuntimeBeforeHistory(t *testing.T) {
	history := []Message{{Role: "assistant", Content: "Earlier context"}}
	got := buildConversationWithRuntime(
		"system prompt",
		"Authoritative runtime context:\nCurrent date: Sunday, March 8, 2026",
		history,
		"latest user message",
	)
	if len(got) != 4 {
		t.Fatalf("len(messages) = %d, want 4", len(got))
	}
	if got[0].Role != "system" || got[0].Content != "system prompt" {
		t.Fatalf("messages[0] = %+v", got[0])
	}
	if got[1].Role != "assistant" || !strings.Contains(got[1].Content, "Current date: Sunday, March 8, 2026") {
		t.Fatalf("messages[1] = %+v", got[1])
	}
	if got[2] != history[0] {
		t.Fatalf("messages[2] = %+v, want %+v", got[2], history[0])
	}
	if got[3].Role != "user" || got[3].Content != "latest user message" {
		t.Fatalf("messages[3] = %+v", got[3])
	}
}

func TestHandleQueryActionToolErrorPushesClarifyInstruction(t *testing.T) {
	exec := &Executor{
		toolExecute: func(context.Context, string, map[string]interface{}) (string, error) {
			return "", fmt.Errorf("unsupported args for tool: timeFrame")
		},
	}
	action := &AgentAction{
		Action: "query",
		Tool:   "artistListeningStats",
		Args:   map[string]interface{}{"filter": map[string]interface{}{"timeFrame": "last_month"}},
	}

	_, nextMessages, done := exec.handleQueryAction(context.Background(), `{"action":"query"}`, action, nil)
	if done {
		t.Fatal("handleQueryAction() unexpectedly finished after tool error")
	}
	if len(nextMessages) != 1 {
		t.Fatalf("len(nextMessages) = %d, want 1", len(nextMessages))
	}
	if !strings.Contains(nextMessages[0].Content, "Otherwise ask one concise clarifying question.") {
		t.Fatalf("tool error follow-up = %q", nextMessages[0].Content)
	}
}

func TestRenderToolResultLeavesArtistsForLLM(t *testing.T) {
	raw := `{"data":{"artists":[{"name":"Ulver"}]}}`
	got, ok := renderToolResult("artists", map[string]interface{}{"artistName": "Ulver"}, raw)
	if ok || got != "" {
		t.Fatalf("renderToolResult() = %q, %v, want no eager render", got, ok)
	}
}

func TestRenderToolResultPreviewResponse(t *testing.T) {
	raw := `{"data":{"startArtistRemovalPreview":{"response":"Ready to remove Ulver."}}}`
	got, ok := renderToolResult("startArtistRemovalPreview", nil, raw)
	if !ok {
		t.Fatal("renderToolResult() did not render preview response")
	}
	if got != "Ready to remove Ulver." {
		t.Fatalf("renderToolResult() = %q", got)
	}
}

func TestRenderToolResultPlaylistAppendPreviewResponse(t *testing.T) {
	raw := `{"data":{"startPlaylistAppendPreview":{"response":"I prepared 2 direct addition(s) and 1 queued addition(s) for \"Late Night\". Use the approval buttons if you want me to apply them."}}}`
	got, ok := renderToolResult("startPlaylistAppendPreview", nil, raw)
	if !ok {
		t.Fatal("renderToolResult() did not render playlist append preview response")
	}
	want := `I prepared 2 direct addition(s) and 1 queued addition(s) for "Late Night". Use the approval buttons if you want me to apply them.`
	if got != want {
		t.Fatalf("renderToolResult() = %q, want %q", got, want)
	}
}

func TestRenderToolResultLeavesAlbumLibraryStatsForLLM(t *testing.T) {
	raw := `{"data":{"albumLibraryStats":[{"albumName":"Ambient 1","artistName":"Brian Eno","year":1978,"lastPlayed":null,"playedInWindow":0}]}}`
	got, ok := renderToolResult("albumLibraryStats", map[string]interface{}{
		"filter": map[string]interface{}{"unplayed": true},
	}, raw)
	if ok || got != "" {
		t.Fatalf("renderToolResult() = %q, %v, want no eager render", got, ok)
	}
}

func TestRenderToolResultLeavesArtistListeningStatsForLLM(t *testing.T) {
	raw := `{"data":{"artistListeningStats":[{"artistName":"Muse","albumCount":4,"playsInWindow":0,"lastPlayed":null}]}}`
	got, ok := renderToolResult("artistListeningStats", map[string]interface{}{
		"filter": map[string]interface{}{"maxPlaysInWindow": 0},
	}, raw)
	if ok || got != "" {
		t.Fatalf("renderToolResult() = %q, %v, want no eager render", got, ok)
	}
}

func TestRenderToolResultLeavesLibraryFacetCountsForLLM(t *testing.T) {
	raw := `{"data":{"libraryFacetCounts":[{"value":"rock","count":42},{"value":"ambient","count":18}]}}`
	got, ok := renderToolResult("libraryFacetCounts", map[string]interface{}{"field": "genre"}, raw)
	if ok || got != "" {
		t.Fatalf("renderToolResult() = %q, %v, want no eager render", got, ok)
	}
}

func TestRenderToolResultLeavesAlbumRelationshipStatsForLLM(t *testing.T) {
	raw := `{"data":{"albumRelationshipStats":[{"albumName":"Warpaint","artistName":"Warpaint","year":2014,"artistAlbumCount":1}]}}`
	got, ok := renderToolResult("albumRelationshipStats", map[string]interface{}{
		"filter": map[string]interface{}{"artistExactAlbums": 1},
	}, raw)
	if ok || got != "" {
		t.Fatalf("renderToolResult() = %q, %v, want no eager render", got, ok)
	}
}

func TestRenderToolResultLeavesSemanticAlbumSearchForLLM(t *testing.T) {
	raw := `{"data":{"semanticAlbumSearch":{"queryText":"nocturnal ambient","matches":[{"name":"Selected Ambient Works Volume II","artistName":"Aphex Twin","year":1994,"similarity":0.91,"explanations":["MusicBrainz tags matched: nocturnal, ambient"]}]}}}`
	got, ok := renderToolResult("semanticAlbumSearch", nil, raw)
	if ok || got != "" {
		t.Fatalf("renderToolResult() = %q, %v, want no eager render", got, ok)
	}
}

func TestRenderToolResultLeavesNavidromePlaylistsForLLM(t *testing.T) {
	raw := `{"data":{"navidromePlaylists":{"playlists":[{"name":"Late Night","songCount":12},{"name":"Focus","songCount":8}]}}}`
	got, ok := renderToolResult("navidromePlaylists", nil, raw)
	if ok || got != "" {
		t.Fatalf("renderToolResult() = %q, %v, want no eager render", got, ok)
	}
}

func TestRenderToolResultLeavesNavidromePlaylistForLLM(t *testing.T) {
	raw := `{"data":{"navidromePlaylist":{"name":"Late Night","tracks":[{"title":"Alone in Kyoto","artistName":"Air"},{"title":"Dayvan Cowboy","artistName":"Boards of Canada"}]}}}`
	got, ok := renderToolResult("navidromePlaylist", nil, raw)
	if ok || got != "" {
		t.Fatalf("renderToolResult() = %q, %v, want no eager render", got, ok)
	}
}

func TestRenderToolResultLeavesNavidromePlaylistStateForLLM(t *testing.T) {
	raw := `{"data":{"navidromePlaylistState":{"name":"Late Night","counts":{"saved":12,"pending_fetch":2,"total":14}}}}`
	got, ok := renderToolResult("navidromePlaylistState", nil, raw)
	if ok || got != "" {
		t.Fatalf("renderToolResult() = %q, %v, want no eager render", got, ok)
	}
}

func TestRenderToolResultAddTrackToNavidromePlaylist(t *testing.T) {
	raw := `{"data":{"addTrackToNavidromePlaylist":{"playlistName":"Late Night","artistName":"Air","trackTitle":"Alone in Kyoto","added":true}}}`
	got, ok := renderToolResult("addTrackToNavidromePlaylist", nil, raw)
	if !ok {
		t.Fatal("renderToolResult() did not render addTrackToNavidromePlaylist")
	}
	want := `Added "Alone in Kyoto" by Air to playlist "Late Night".`
	if got != want {
		t.Fatalf("renderToolResult() = %q, want %q", got, want)
	}
}

func TestRenderToolResultQueueTrackForNavidromePlaylist(t *testing.T) {
	raw := `{"data":{"queueTrackForNavidromePlaylist":{"playlistName":"Late Night","artistName":"Air","trackTitle":"La Femme d'Argent","queued":true}}}`
	got, ok := renderToolResult("queueTrackForNavidromePlaylist", nil, raw)
	if !ok {
		t.Fatal("renderToolResult() did not render queueTrackForNavidromePlaylist")
	}
	want := `Queued "La Femme d'Argent" by Air for playlist "Late Night". Reconcile will add it once it becomes available.`
	if got != want {
		t.Fatalf("renderToolResult() = %q, want %q", got, want)
	}
}

func TestRenderToolResultRemoveTrackFromNavidromePlaylist(t *testing.T) {
	raw := `{"data":{"removeTrackFromNavidromePlaylist":{"playlistName":"Late Night","removed":1,"tracks":["Alone in Kyoto by Air"]}}}`
	got, ok := renderToolResult("removeTrackFromNavidromePlaylist", nil, raw)
	if !ok {
		t.Fatal("renderToolResult() did not render removeTrackFromNavidromePlaylist")
	}
	want := `Removed 1 track(s) from playlist "Late Night": Alone in Kyoto by Air.`
	if got != want {
		t.Fatalf("renderToolResult() = %q, want %q", got, want)
	}
}

func TestRenderToolResultRemovePendingTracksFromNavidromePlaylist(t *testing.T) {
	raw := `{"data":{"removePendingTracksFromNavidromePlaylist":{"playlistName":"Late Night","removed":1,"tracks":["La Femme d'Argent by Air"]}}}`
	got, ok := renderToolResult("removePendingTracksFromNavidromePlaylist", nil, raw)
	if !ok {
		t.Fatal("renderToolResult() did not render removePendingTracksFromNavidromePlaylist")
	}
	want := `Removed 1 pending track(s) from playlist "Late Night": La Femme d'Argent by Air.`
	if got != want {
		t.Fatalf("renderToolResult() = %q, want %q", got, want)
	}
}

func TestBuildToolFollowUpMessageUsesCompactedToolResult(t *testing.T) {
	raw := "{\n  \"data\": { \"value\": 1 }\n}\n"
	got := buildToolFollowUpMessage("albums", raw)
	if !strings.Contains(got, `Tool result for albums: {"data":{"value":1}}`) {
		t.Fatalf("buildToolFollowUpMessage() = %q", got)
	}
}
