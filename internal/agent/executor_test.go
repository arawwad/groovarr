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
		"Tool manifest:",
		"albums: List albums in the user's library.",
		"discoverAlbums: Discover albums beyond the user's current library.",
		"args: field:string*; filter:object; limit:number",
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
		"Derive the user's intent from the latest message",
		"ask one concise clarifying question instead of guessing",
		"Use only the tools listed in the tool manifest for this turn.",
		"The tool manifest may be a routed subset of all available tools.",
		"If no listed tool fits, ask one concise clarifying question instead of inventing a tool.",
		"Do not answer library-stat or library-count questions from model memory.",
		"For exact counts, prefer stats or facet tools over counting a capped list.",
		"Server session context may include authoritative cached result sets.",
		"Reuse prior artists or albums in follow-ups, and prefer multi-value tool args when available.",
		`For follow-ups like "those" or "which of those", stay anchored to the available history or session result set.`,
		"If a follow-up depends on a prior result set you do not actually have in history or session context, ask one concise clarifying question.",
		"Preserve the original subject when narrowing prior recommendation or semantic-search results, then add explicit filters.",
		"For decade/year follow-ups on semanticAlbumSearch, keep queryText and add minYear/maxYear.",
		"Preserve explicit song and album title qualifiers from the user verbatim when they matter, including mixes, live versions, remasters, demos, and parenthetical subtitles.",
		`For track-based tools, do not shorten or normalize away a user-provided version qualifier like "(live)", "(demo)", or "(original Steve Albini 1993 mix)".`,
		"Never invent, rewrite, or approximate a sceneKey from a scene name, subtitle, or mood words.",
		"If you do not already have an authoritative sceneKey and the scene name may be ambiguous, ask one concise clarifying question instead of fabricating a backend-style key.",
		"Recommendations are global by default. Use discoverAlbums unless the user explicitly limits them to their library.",
		`For "best/top/essential <artist>" prompts without an ownership cue, ask whether the user wants general picks or albums from their library before choosing discoverAlbums vs albums.`,
		"For library-only vibe recommendations, prefer semanticAlbumSearch over albums or discoverAlbums.",
		`Treat vague recent phrases like "lately" or "recently" as last month unless the user asks for another window.`,
		"Do not invent tool names, arg names, filter keys, or enum values.",
		"If you cannot identify one best tool with valid arguments, ask a clarifying question.",
		`If the user asks for vague "stats", ask whether they mean library composition or listening over time.`,
		"For playlist creation requests, including when the user already provides the exact songs they want, use the playlist preview tool rather than replying with a manual availability-confirmation step.",
		"Do not ask to confirm whether requested playlist tracks are available before using the playlist preview tool; the preview flow already resolves availability and missing tracks.",
		"If the user asks to make a playlist without a mood, theme, purpose, seed artist, or songs, ask what kind of playlist they want instead of inventing a generic default.",
		"Tool groups:",
		"Detailed tool names and args are injected separately for the relevant groups on each turn.",
		"Library Browse: Library totals plus artist, album, and track lookups.",
		"Playlist Planning: Preview creating, extending, refreshing, or repairing playlists.",
		"Decision examples:",
		`If the user asks "Give me artist stats.", ask whether they mean library composition or listening over time.`,
		`If the user asks for "best/top/essential <artist> albums" without saying whether they mean general or owned albums, ask which scope they want before choosing a tool.`,
		`Treat vague recent phrases like "lately" or "recently" as last month by default.`,
		`If the user asks "Make me a playlist" with no other direction, ask what kind of playlist they want.`,
		"If the user asks to make a playlist and already names the songs they want, call the playlist preview tool with that request instead of asking to check availability first.",
		`If the user asks "Which of those have I played recently?", use the prior result set when available; otherwise ask which items they mean.`,
		"If the user gives a fully specified track title with a version qualifier, keep that exact title when calling a track or song-path tool.",
		"If the user refers to a sonic scene loosely and there is no exact prior sceneKey in context, ask which scene they mean rather than synthesizing a sceneKey.",
		"Preview before state-changing operations.",
	}
	for _, fragment := range required {
		if !strings.Contains(got, fragment) {
			t.Fatalf("buildSystemPrompt() missing %q", fragment)
		}
	}
}

func TestBuildSystemPromptIsCompact(t *testing.T) {
	got := buildSystemPrompt()
	if len(got) > 7500 {
		t.Fatalf("buildSystemPrompt() too long: %d chars", len(got))
	}
}

func TestBuildConversationWithRuntimePlacesRuntimeBeforeHistory(t *testing.T) {
	history := []Message{{Role: "assistant", Content: "Earlier context"}}
	got := buildConversationWithRuntime(
		"system prompt",
		"Authoritative runtime context:\nCurrent date: Sunday, March 8, 2026",
		"Tool manifest for this turn.\nTool manifest:\n- albums: ...",
		history,
		"latest user message",
	)
	if len(got) != 5 {
		t.Fatalf("len(messages) = %d, want 5", len(got))
	}
	if got[0].Role != "system" || got[0].Content != "system prompt" {
		t.Fatalf("messages[0] = %+v", got[0])
	}
	if got[1].Role != "assistant" || !strings.Contains(got[1].Content, "Current date: Sunday, March 8, 2026") {
		t.Fatalf("messages[1] = %+v", got[1])
	}
	if got[2].Role != "assistant" || !strings.Contains(got[2].Content, "Tool manifest for this turn.") {
		t.Fatalf("messages[2] = %+v", got[2])
	}
	if got[3] != history[0] {
		t.Fatalf("messages[3] = %+v, want %+v", got[3], history[0])
	}
	if got[4].Role != "user" || got[4].Content != "latest user message" {
		t.Fatalf("messages[4] = %+v", got[4])
	}
}

func TestBuildToolManifestPromptRoutesDiscoveryPrompt(t *testing.T) {
	prompt := buildToolManifestPromptForMode("Best 5 Bjork albums", nil, toolManifestModeRouted)
	if !strings.Contains(strings.Join(prompt.Categories, ","), "Discovery") {
		t.Fatalf("categories = %v, want Discovery", prompt.Categories)
	}
	if !strings.Contains(prompt.Content, "discoverAlbums: Discover albums beyond the user's current library.") {
		t.Fatalf("manifest = %q", prompt.Content)
	}
	if strings.Contains(prompt.Content, "startArtistRemovalPreview") {
		t.Fatalf("manifest unexpectedly contains cleanup tools: %q", prompt.Content)
	}
}

func TestBuildToolManifestPromptRoutesAnalyticsPrompt(t *testing.T) {
	prompt := buildToolManifestPromptForMode("How many Pink Floyd albums are in my library?", nil, toolManifestModeRouted)
	if !strings.Contains(strings.Join(prompt.Categories, ","), "Library Analytics") {
		t.Fatalf("categories = %v, want Library Analytics", prompt.Categories)
	}
	if !strings.Contains(prompt.Content, "artistLibraryStats: Artist-level library composition stats.") {
		t.Fatalf("manifest = %q", prompt.Content)
	}
}

func TestBuildToolManifestPromptRoutesExplicitPlaylistSongListPrompt(t *testing.T) {
	prompt := buildToolManifestPromptForMode(
		"Create a playlist for these Radiohead songs: Lift, 15 Step, Separator, Lotus Flower",
		nil,
		toolManifestModeRouted,
	)
	if !strings.Contains(strings.Join(prompt.Categories, ","), "Playlist Planning") {
		t.Fatalf("categories = %v, want Playlist Planning", prompt.Categories)
	}
	required := []string{
		"startPlaylistCreatePreview: Preview a new playlist, including one built from an explicit song list.",
		"Default for playlist creation, including when the user already names the exact songs they want.",
		"do not ask for a separate availability check first",
	}
	for _, fragment := range required {
		if !strings.Contains(prompt.Content, fragment) {
			t.Fatalf("manifest = %q, missing %q", prompt.Content, fragment)
		}
	}
}

func TestBuildToolManifestPromptUsesHistoryForFollowUp(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "Give me three records for a rainy late-night walk."},
		{Role: "assistant", Content: "I would start with these three albums."},
	}
	prompt := buildToolManifestPromptForMode("From those, give me three albums to revisit today.", history, toolManifestModeRouted)
	if !strings.Contains(strings.Join(prompt.Categories, ","), "Discovery") {
		t.Fatalf("categories = %v, want Discovery from history", prompt.Categories)
	}
}

func TestBuildToolManifestPromptFullModeIncludesAllTools(t *testing.T) {
	prompt := buildToolManifestPromptForMode("hello", nil, toolManifestModeFull)
	required := []string{
		"discoverAlbums: Discover albums beyond the user's current library.",
		"startArtistRemovalPreview: Prepare a preview for removing an artist from the library.",
		"navidromePlaylists: List saved Navidrome playlists.",
	}
	for _, fragment := range required {
		if !strings.Contains(prompt.Content, fragment) {
			t.Fatalf("full manifest missing %q", fragment)
		}
	}
}

func TestRenderPlaylistPlanDetailsResult(t *testing.T) {
	raw := `{"data":{"playlistPlanDetails":{"playlistName":"Late Night","counts":{"planned":2},"resolutionCounts":{"resolved":2,"available":1,"missing":1,"ambiguous":0,"errors":0,"unresolved":0},"tracks":[{"rank":1,"artistName":"Air","trackTitle":"Alone in Kyoto","status":"available","reason":"fits the nocturnal mood"},{"rank":2,"artistName":"Air","trackTitle":"La Femme d'Argent","status":"missing","reason":"expands the same palette"}]}}}`
	got, ok := renderToolResult("playlistPlanDetails", nil, raw)
	if !ok {
		t.Fatal("renderToolResult() did not render playlistPlanDetails")
	}
	if !strings.Contains(got, `Current plan for "Late Night" (2 tracks). Resolution snapshot: 1 available, 1 missing, 0 ambiguous, 0 errors`) {
		t.Fatalf("renderToolResult() = %q", got)
	}
	if !strings.Contains(got, `1. Alone in Kyoto by Air [available] - fits the nocturnal mood`) {
		t.Fatalf("renderToolResult() = %q", got)
	}
	if !strings.Contains(got, `2. La Femme d'Argent by Air [missing] - expands the same palette`) {
		t.Fatalf("renderToolResult() = %q", got)
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

func TestRenderToolResultPlaylistCreatePreviewResponse(t *testing.T) {
	raw := `{"data":{"startPlaylistCreatePreview":{"response":"I prepared 12 direct track(s) and 2 queued track(s) for \"Late Nights\". Use the approval buttons if you want me to create or update it."}}}`
	got, ok := renderToolResult("startPlaylistCreatePreview", nil, raw)
	if !ok {
		t.Fatal("renderToolResult() did not render playlist create preview response")
	}
	want := `I prepared 12 direct track(s) and 2 queued track(s) for "Late Nights". Use the approval buttons if you want me to create or update it.`
	if got != want {
		t.Fatalf("renderToolResult() = %q, want %q", got, want)
	}
}

func TestRenderToolResultPlaylistRefreshPreviewResponse(t *testing.T) {
	raw := `{"data":{"startPlaylistRefreshPreview":{"response":"I prepared 4 safe replacement(s) for \"Late Nights\". Use the approval buttons if you want me to refresh it."}}}`
	got, ok := renderToolResult("startPlaylistRefreshPreview", nil, raw)
	if !ok {
		t.Fatal("renderToolResult() did not render playlist refresh preview response")
	}
	want := `I prepared 4 safe replacement(s) for "Late Nights". Use the approval buttons if you want me to refresh it.`
	if got != want {
		t.Fatalf("renderToolResult() = %q, want %q", got, want)
	}
}

func TestRenderToolResultPlaylistRepairPreviewResponse(t *testing.T) {
	raw := `{"data":{"startPlaylistRepairPreview":{"response":"I prepared 3 repair replacement(s) for \"Late Nights\". Use the approval buttons if you want me to apply them."}}}`
	got, ok := renderToolResult("startPlaylistRepairPreview", nil, raw)
	if !ok {
		t.Fatal("renderToolResult() did not render playlist repair preview response")
	}
	want := `I prepared 3 repair replacement(s) for "Late Nights". Use the approval buttons if you want me to apply them.`
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

func TestRenderToolResultAddOrQueueTrackToNavidromePlaylistAdded(t *testing.T) {
	raw := `{"data":{"addOrQueueTrackToNavidromePlaylist":{"playlistName":"Late Night","artistName":"Air","trackTitle":"Alone in Kyoto","mode":"added"}}}`
	got, ok := renderToolResult("addOrQueueTrackToNavidromePlaylist", nil, raw)
	if !ok {
		t.Fatal("renderToolResult() did not render addOrQueueTrackToNavidromePlaylist")
	}
	want := `Added "Alone in Kyoto" by Air to playlist "Late Night".`
	if got != want {
		t.Fatalf("renderToolResult() = %q, want %q", got, want)
	}
}

func TestRenderToolResultAddOrQueueTrackToNavidromePlaylistAlreadyQueued(t *testing.T) {
	raw := `{"data":{"addOrQueueTrackToNavidromePlaylist":{"playlistName":"Late Night","artistName":"Air","trackTitle":"La Femme d'Argent","mode":"already_queued"}}}`
	got, ok := renderToolResult("addOrQueueTrackToNavidromePlaylist", nil, raw)
	if !ok {
		t.Fatal("renderToolResult() did not render addOrQueueTrackToNavidromePlaylist")
	}
	want := `"La Femme d'Argent" by Air is already queued for playlist "Late Night". Reconcile will add it once it becomes available.`
	if got != want {
		t.Fatalf("renderToolResult() = %q, want %q", got, want)
	}
}

func TestRenderToolResultAddOrQueueTrackToNavidromePlaylistAmbiguous(t *testing.T) {
	raw := `{"data":{"addOrQueueTrackToNavidromePlaylist":{"playlistName":"Late Night","artistName":"Air","trackTitle":"Alone","mode":"ambiguous","matchCount":3}}}`
	got, ok := renderToolResult("addOrQueueTrackToNavidromePlaylist", nil, raw)
	if !ok {
		t.Fatal("renderToolResult() did not render addOrQueueTrackToNavidromePlaylist")
	}
	want := `I found 3 library matches for "Alone" by Air, so I did not change playlist "Late Night".`
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
