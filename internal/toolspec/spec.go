package toolspec

import "strings"

type ToolSpec struct {
	Category    string
	Name        string
	Description string
	UseWhen     string
	Schema      string
	Args        []ToolArgSpec
	Example     string
}

type ToolArgSpec struct {
	Name        string
	Type        string
	Required    bool
	Description string
}

const (
	CategoryLibraryBrowse    = "Library Browse"
	CategoryListening        = "Listening"
	CategoryLibraryAnalytics = "Library Analytics"
	CategorySemanticSearch   = "Semantic Search"
	CategoryDiscovery        = "Discovery"
	CategoryPlaylistPlanning = "Playlist Planning"
	CategoryPlaylistState    = "Playlist State"
	CategoryPlaylistActions  = "Playlist Actions"
	CategoryCleanup          = "Cleanup"
	CategorySimilarity       = "Similarity"
)

var (
	ArtistListeningStatsFilterKeys = []string{
		"artistName", "playedSince", "playedUntil",
		"minPlaysInWindow", "maxPlaysInWindow", "minAlbums", "maxAlbums",
	}
	ArtistLibraryStatsFilterKeys = []string{
		"artistName", "artistNames", "genre", "exactAlbums", "minAlbums", "maxAlbums",
		"minTotalPlays", "maxTotalPlays", "inactiveSince", "notPlayedSince",
		"playedSince", "playedUntil", "maxPlaysInWindow",
	}
	AlbumLibraryStatsFilterKeys = []string{
		"artistName", "genre", "year", "minYear", "maxYear",
		"minTotalPlays", "maxTotalPlays", "minRating", "maxRating",
		"inactiveSince", "notPlayedSince", "playedSince", "playedUntil",
		"maxPlaysInWindow", "unplayed",
	}
	AlbumRelationshipStatsFilterKeys = []string{
		"artistExactAlbums", "artistMinAlbums", "artistMaxAlbums", "genre", "unplayed", "notPlayedSince",
	}
	LibraryFacetCountsFilterKeys = []string{
		"genre", "artistName", "year", "minYear", "maxYear", "unplayed", "notPlayedSince",
	}
)

func filterKeySchema(keys []string) string {
	return "filter keys: " + strings.Join(keys, ", ")
}

func requiredFieldFilterKeySchema(keys []string) string {
	return "field is required; " + filterKeySchema(keys)
}

func PromptCatalog() []ToolSpec {
	return []ToolSpec{
		{
			Category:    CategoryLibraryBrowse,
			Name:        "libraryStats",
			Description: "High-level counts for the user's library.",
			UseWhen:     "The user asks for overall library totals or a broad overview.",
			Example:     `{"action":"query","tool":"libraryStats","args":{}}`,
		},
		{
			Category:    CategoryLibraryBrowse,
			Name:        "artists",
			Description: "List artists in the user's library.",
			UseWhen:     "The user asks for artists in their library or top artists without advanced grouping.",
			Args: []ToolArgSpec{
				{Name: "limit", Type: "number", Description: "Maximum results."},
				{Name: "minPlayCount", Type: "number", Description: "Only include artists with at least this many plays."},
			},
		},
		{
			Category:    CategoryLibraryBrowse,
			Name:        "albums",
			Description: "List albums in the user's library.",
			UseWhen:     "Owned-album lists, album title lookups, or follow-ups like 'from those artists'. Not for song lookups or global best/top artist-discography requests unless library-limited.",
			Args: []ToolArgSpec{
				{Name: "artistName", Type: "string", Description: "Filter to one artist."},
				{Name: "artistNames", Type: "array<string>", Description: "Filter to multiple artists from prior context."},
				{Name: "queryText", Type: "string", Description: "Match album title or artist text for exact-lookups like 'do I have Dark Side of the Moon?'."},
				{Name: "genre", Type: "string", Description: "Filter by genre."},
				{Name: "year", Type: "number", Description: "Filter by release year."},
				{Name: "minYear", Type: "number", Description: "Lower bound release year for decade/range follow-ups."},
				{Name: "maxYear", Type: "number", Description: "Upper bound release year for decade/range follow-ups."},
				{Name: "unplayed", Type: "boolean", Description: "Only unplayed albums."},
				{Name: "notPlayedSince", Type: "string", Description: "ISO date or supported relative date phrase."},
				{Name: "rating", Type: "number", Description: "Minimum rating filter."},
				{Name: "ratingBelow", Type: "number", Description: "Maximum rating filter."},
				{Name: "sortBy", Type: "string", Description: "Result ordering."},
				{Name: "limit", Type: "number", Description: "Maximum results."},
			},
			Example: `{"action":"query","tool":"albums","args":{"artistName":"Pink Floyd","limit":5}}`,
		},
		{
			Category:    CategoryLibraryBrowse,
			Name:        "tracks",
			Description: "List tracks from the user's library and listening history.",
			UseWhen:     "Track lists, most-played tracks, or song/title checks in the library, such as 'Do I have Heart-Shaped Box by Nirvana?'.",
			Args: []ToolArgSpec{
				{Name: "mostPlayed", Type: "boolean", Description: "Prefer top-played tracks."},
				{Name: "playedSince", Type: "string", Description: "Lower bound time."},
				{Name: "playedUntil", Type: "string", Description: "Upper bound time."},
				{Name: "onlyPlayed", Type: "boolean", Description: "Require at least one play."},
				{Name: "window", Type: "string", Description: "Named time window such as last_month."},
				{Name: "artistName", Type: "string", Description: "Restrict to one artist."},
				{Name: "queryText", Type: "string", Description: "Filter by title text."},
				{Name: "sortBy", Type: "string", Description: "Result ordering."},
				{Name: "limit", Type: "number", Description: "Maximum results."},
			},
		},
		{
			Category:    CategoryLibraryBrowse,
			Name:        "badlyRatedAlbums",
			Description: "Find albums in the user's library that contain any badly rated tracks.",
			UseWhen:     "The user asks about disliked or badly rated albums, or albums containing 1-star or 2-star tracks.",
			Args: []ToolArgSpec{
				{Name: "limit", Type: "number", Description: "Maximum albums."},
				{Name: "maxTrackDetails", Type: "number", Description: "Maximum bad-track examples per album."},
			},
			Example: `{"action":"query","tool":"badlyRatedAlbums","args":{"limit":20,"maxTrackDetails":3}}`,
		},
		{
			Category:    CategoryListening,
			Name:        "recentListeningSummary",
			Description: "Summaries for a listening window with top tracks and artists heard.",
			UseWhen:     `The user asks what they listened to in a recent period. For vague recency phrases like "lately" or "recently", prefer a sensible default such as last_month rather than an invalid custom window.`,
			Args: []ToolArgSpec{
				{Name: "window", Type: "string", Description: "Named window such as last_week or last_month. Do not combine with playedSince/playedUntil."},
				{Name: "playedSince", Type: "string", Description: "Lower bound time. If set, playedUntil must also be set."},
				{Name: "playedUntil", Type: "string", Description: "Upper bound time. If set, playedSince must also be set."},
				{Name: "trackLimit", Type: "number", Description: "Top tracks to include."},
				{Name: "artistLimit", Type: "number", Description: "Top artists to include."},
			},
			Example: `{"action":"query","tool":"recentListeningSummary","args":{"window":"last_month","trackLimit":10,"artistLimit":6}}`,
		},
		{
			Category:    CategoryListening,
			Name:        "artistListeningStats",
			Description: "Artist-level listening stats over a time window.",
			UseWhen:     "The user asks which artists they played most or least in a period.",
			Schema:      filterKeySchema(ArtistListeningStatsFilterKeys),
			Args: []ToolArgSpec{
				{Name: "filter", Type: "object", Description: "Use only the allowed filter keys listed below."},
				{Name: "sort", Type: "string", Description: "Sort order: plays, recent, name, albums, total_plays."},
				{Name: "limit", Type: "number", Description: "Maximum results."},
			},
			Example: `{"action":"query","tool":"artistListeningStats","args":{"filter":{"playedSince":"2026-02-10","playedUntil":"2026-03-10"},"sort":"plays","limit":10}}`,
		},
		{
			Category:    CategoryLibraryAnalytics,
			Name:        "artistLibraryStats",
			Description: "Artist-level library composition stats.",
			UseWhen:     "The user asks for grouped artist analytics about albums, ratings, library shape, or exact album counts for a specific artist rather than listening windows.",
			Schema:      filterKeySchema(ArtistLibraryStatsFilterKeys),
			Args: []ToolArgSpec{
				{Name: "filter", Type: "object", Description: "Use only the allowed filter keys listed below."},
				{Name: "sort", Type: "string", Description: "Sort order."},
				{Name: "limit", Type: "number", Description: "Maximum results."},
			},
			Example: `{"action":"query","tool":"artistLibraryStats","args":{"filter":{"artistName":"Pink Floyd"},"sort":"albums","limit":5}}`,
		},
		{
			Category:    CategoryLibraryAnalytics,
			Name:        "albumLibraryStats",
			Description: "Album-level library stats.",
			UseWhen:     "The user asks for album counts, unplayed albums, neglected albums, or album stats inside their own library.",
			Schema:      filterKeySchema(AlbumLibraryStatsFilterKeys),
			Args: []ToolArgSpec{
				{Name: "filter", Type: "object", Description: "Use only the allowed filter keys listed below."},
				{Name: "sort", Type: "string", Description: "Sort order."},
				{Name: "limit", Type: "number", Description: "Maximum results."},
			},
		},
		{
			Category:    CategoryLibraryAnalytics,
			Name:        "albumRelationshipStats",
			Description: "Album stats that depend on artist-to-album relationships.",
			UseWhen:     "The user asks for albums by artists with a certain number of albums or other artist-album relationship patterns.",
			Schema:      filterKeySchema(AlbumRelationshipStatsFilterKeys),
			Args: []ToolArgSpec{
				{Name: "filter", Type: "object", Description: "Use only the allowed filter keys listed below."},
				{Name: "sort", Type: "string", Description: "Sort order."},
				{Name: "limit", Type: "number", Description: "Maximum results."},
			},
		},
		{
			Category:    CategoryLibraryAnalytics,
			Name:        "libraryFacetCounts",
			Description: "Facet counts such as genres, years, or decades.",
			UseWhen:     "The user asks for distributions, breakdowns, or dominant categories in the library.",
			Schema:      requiredFieldFilterKeySchema(LibraryFacetCountsFilterKeys),
			Args: []ToolArgSpec{
				{Name: "field", Type: "string", Required: true, Description: "Facet field such as genre, year, or decade."},
				{Name: "filter", Type: "object", Description: "Use only the allowed filter keys listed below."},
				{Name: "limit", Type: "number", Description: "Maximum buckets."},
			},
			Example: `{"action":"query","tool":"libraryFacetCounts","args":{"field":"genre","limit":10}}`,
		},
		{
			Category:    CategorySemanticSearch,
			Name:        "semanticTrackSearch",
			Description: "Semantic search over tracks in the user's library.",
			UseWhen:     "The user asks for tracks with a mood, vibe, or descriptive sound and wants matches from their own library.",
			Args: []ToolArgSpec{
				{Name: "queryText", Type: "string", Required: true, Description: "Semantic search text."},
				{Name: "window", Type: "string", Description: "Optional listening window."},
				{Name: "playedSince", Type: "string", Description: "Lower bound time."},
				{Name: "playedUntil", Type: "string", Description: "Upper bound time."},
				{Name: "limit", Type: "number", Description: "Maximum results."},
			},
		},
		{
			Category:    CategorySemanticSearch,
			Name:        "textToTrack",
			Description: "Text-to-track search over the analyzed library.",
			UseWhen:     "The user describes a sound, mood, or texture and wants library tracks that match it sonically.",
			Args: []ToolArgSpec{
				{Name: "queryText", Type: "string", Required: true, Description: "Text description of the sound or vibe."},
				{Name: "limit", Type: "number", Description: "Maximum results."},
			},
			Example: `{"action":"query","tool":"textToTrack","args":{"queryText":"smoky nocturnal trip-hop","limit":8}}`,
		},
		{
			Category:    CategorySemanticSearch,
			Name:        "semanticAlbumSearch",
			Description: "Semantic search over albums in the user's library.",
			UseWhen:     "Use only for library-only vibe/scene recommendations or narrowing a prior library semantic album request.",
			Args: []ToolArgSpec{
				{Name: "queryText", Type: "string", Required: true, Description: "Semantic search text."},
				{Name: "artistName", Type: "string", Description: "Restrict to one artist."},
				{Name: "genre", Type: "string", Description: "Restrict by genre."},
				{Name: "minYear", Type: "number", Description: "Lower year bound."},
				{Name: "maxYear", Type: "number", Description: "Upper year bound."},
				{Name: "limit", Type: "number", Description: "Maximum results."},
			},
			Example: `{"action":"query","tool":"semanticAlbumSearch","args":{"queryText":"melancholic dream pop","minYear":1990,"maxYear":1999,"limit":5}}`,
		},
		{
			Category:    CategoryDiscovery,
			Name:        "discoverAlbums",
			Description: "Discover albums beyond the user's current library.",
			UseWhen:     `Default recommendation tool for best, top, essential, mood, artist-discography, or "like X but more Y" album requests when the user clearly wants global recommendations. If an opening-turn prompt like "best Bjork albums" could mean either general picks or only owned albums, ask one concise scope clarification first.`,
			Args: []ToolArgSpec{
				{Name: "query", Type: "string", Required: true, Description: "Discovery request."},
				{Name: "limit", Type: "number", Description: "Maximum results."},
			},
			Example: `{"action":"query","tool":"discoverAlbums","args":{"query":"records like Talk Talk's Laughing Stock but warmer and more spacious","limit":5}}`,
		},
		{
			Category:    CategoryDiscovery,
			Name:        "discoverAlbumsFromScene",
			Description: "Discover albums beyond the user's library using one sonic scene as the seed context.",
			UseWhen:     "The user wants album recommendations based on a known scene, sonic region, or a prior clusterScenes result, especially for prompts like 'albums from that scene' or 'like that scene but darker'. Use sceneKey only when it came from a prior tool result or server context; otherwise use sceneName or ask a clarifying question.",
			Args: []ToolArgSpec{
				{Name: "sceneKey", Type: "string", Description: "Exact stable scene key from a prior tool result or server context. Never synthesize or approximate this from a scene label."},
				{Name: "sceneName", Type: "string", Description: "User-facing scene name when no authoritative sceneKey is available yet."},
				{Name: "queryText", Type: "string", Description: "Optional directional cue such as darker, more electronic, or less polished."},
				{Name: "limit", Type: "number", Description: "Maximum albums."},
			},
			Example: `{"action":"query","tool":"discoverAlbumsFromScene","args":{"sceneKey":"Indie_Rock_Alternative_Medium_Relaxed_Sad_automatic","queryText":"darker and more spacious","limit":5}}`,
		},
		{
			Category:    CategoryDiscovery,
			Name:        "matchDiscoveredAlbumsInLidarr",
			Description: "Check whether previously discovered albums are available or matched in Lidarr.",
			UseWhen:     "The user asks whether discovered albums are available, matched, or addable.",
			Args: []ToolArgSpec{
				{Name: "selection", Type: "string", Required: true, Description: `Which discovered albums to inspect, such as "all" or "first 3".`},
			},
		},
		{
			Category:    CategoryDiscovery,
			Name:        "startDiscoveredAlbumsApplyPreview",
			Description: "Prepare a preview for adding or monitoring discovered albums.",
			UseWhen:     "The user is ready to act on discovered albums. Preview first rather than applying directly.",
			Args: []ToolArgSpec{
				{Name: "selection", Type: "string", Required: true, Description: `Which discovered albums to act on, such as "all" or a specific album.`},
			},
		},
		{
			Category:    CategoryPlaylistPlanning,
			Name:        "startPlaylistCreatePreview",
			Description: "Preview a new playlist, including one built from an explicit song list.",
			UseWhen:     "Default for playlist creation, including when the user already names the exact songs they want. This preview already resolves library availability and missing tracks, so do not ask for a separate availability check first. If the user gives no theme, purpose, mood, seed artist, or songs, ask one concise clarifying question before using this tool.",
			Args: []ToolArgSpec{
				{Name: "prompt", Type: "string", Required: true, Description: "Playlist request, which can be a vibe prompt or an explicit list of songs/artists."},
				{Name: "playlistName", Type: "string", Description: "Optional playlist title."},
				{Name: "trackCount", Type: "number", Description: "Desired number of tracks."},
			},
		},
		{
			Category:    CategoryPlaylistPlanning,
			Name:        "resolvePlaylistTracks",
			Description: "Resolve previously planned playlist candidates against available tracks.",
			UseWhen:     "The user asks to resolve or inspect availability for a planned playlist.",
			Args: []ToolArgSpec{
				{Name: "selection", Type: "string", Required: true, Description: `Which planned tracks to resolve, such as "all".`},
			},
		},
		{
			Category:    CategoryPlaylistPlanning,
			Name:        "playlistPlanDetails",
			Description: "Inspect the current planned playlist before creating or queueing anything.",
			UseWhen:     "The user asks what is in the current plan, why tracks were chosen, or wants to inspect planned tracks before applying playlist actions.",
			Args: []ToolArgSpec{
				{Name: "selection", Type: "string", Description: `Optional subset of planned tracks, such as "all", "first 5", or part of an artist/title.`},
			},
			Example: `{"action":"query","tool":"playlistPlanDetails","args":{"selection":"first 5"}}`,
		},
		{
			Category:    CategoryPlaylistPlanning,
			Name:        "queueMissingPlaylistTracks",
			Description: "Queue unresolved playlist tracks for download or fetching.",
			UseWhen:     "The user explicitly asks to queue or download missing planned playlist tracks.",
			Args: []ToolArgSpec{
				{Name: "selection", Type: "string", Required: true, Description: `Which planned tracks to queue, such as "all missing".`},
				{Name: "confirm", Type: "boolean", Required: true, Description: "Must be true for execution."},
			},
		},
		{
			Category:    CategoryPlaylistPlanning,
			Name:        "startPlaylistAppendPreview",
			Description: "Preview adding new tracks to an existing saved playlist.",
			UseWhen:     "Use when the user wants to add to an existing playlist.",
			Args: []ToolArgSpec{
				{Name: "playlistName", Type: "string", Required: true, Description: "Existing playlist name."},
				{Name: "prompt", Type: "string", Required: true, Description: "What to add."},
				{Name: "trackCount", Type: "number", Description: "Desired number of additions."},
			},
			Example: `{"action":"query","tool":"startPlaylistAppendPreview","args":{"playlistName":"Melancholy Jazz","prompt":"add five colder tracks","trackCount":5}}`,
		},
		{
			Category:    CategoryPlaylistPlanning,
			Name:        "startPlaylistRefreshPreview",
			Description: "Preview a playlist refresh.",
			UseWhen:     "Use when the user wants to refresh a playlist.",
			Args: []ToolArgSpec{
				{Name: "playlistName", Type: "string", Required: true, Description: "Existing playlist name."},
				{Name: "replaceCount", Type: "number", Description: "How many tracks to replace."},
			},
		},
		{
			Category:    CategoryPlaylistPlanning,
			Name:        "startPlaylistRepairPreview",
			Description: "Preview a playlist repair.",
			UseWhen:     "Use when the user asks to repair a playlist.",
			Args: []ToolArgSpec{
				{Name: "playlistName", Type: "string", Required: true, Description: "Existing playlist name."},
			},
		},
		{
			Category:    CategoryPlaylistState,
			Name:        "navidromePlaylists",
			Description: "List saved Navidrome playlists.",
			UseWhen:     "The user asks what playlists exist or wants to search playlist names.",
			Args: []ToolArgSpec{
				{Name: "limit", Type: "number", Description: "Maximum playlists."},
				{Name: "query", Type: "string", Description: "Playlist name search."},
			},
		},
		{
			Category:    CategoryPlaylistState,
			Name:        "navidromePlaylist",
			Description: "Inspect one saved Navidrome playlist, including saved tracks and any pending queued additions.",
			UseWhen:     "Default playlist read tool when the user asks what is in a playlist or wants its current state.",
			Args: []ToolArgSpec{
				{Name: "playlistName", Type: "string", Description: "Playlist name."},
				{Name: "playlistId", Type: "string", Description: "Playlist ID."},
			},
		},
		{
			Category:    CategoryPlaylistActions,
			Name:        "addOrQueueTrackToNavidromePlaylist",
			Description: "Add a track to a saved playlist now, or queue it if it is not available yet.",
			UseWhen:     "Default when the user wants a specific track put into a playlist and does not care whether that means add-now or queue-for-fetch.",
			Args: []ToolArgSpec{
				{Name: "playlistName", Type: "string", Description: "Playlist name."},
				{Name: "playlistId", Type: "string", Description: "Playlist ID."},
				{Name: "artistName", Type: "string", Required: true, Description: "Track artist."},
				{Name: "trackTitle", Type: "string", Required: true, Description: "Track title."},
			},
		},
		{
			Category:    CategoryPlaylistActions,
			Name:        "addTrackToNavidromePlaylist",
			Description: "Add a specific available track directly to a saved playlist.",
			UseWhen:     "Low-level primitive for explicit immediate-add requests when availability is already known.",
			Args: []ToolArgSpec{
				{Name: "playlistName", Type: "string", Description: "Playlist name."},
				{Name: "playlistId", Type: "string", Description: "Playlist ID."},
				{Name: "artistName", Type: "string", Required: true, Description: "Track artist."},
				{Name: "trackTitle", Type: "string", Required: true, Description: "Track title."},
			},
		},
		{
			Category:    CategoryPlaylistActions,
			Name:        "queueTrackForNavidromePlaylist",
			Description: "Queue a specific missing track for a saved playlist.",
			UseWhen:     "Low-level primitive for explicit queue-only requests instead of the smarter add-or-queue flow.",
			Args: []ToolArgSpec{
				{Name: "playlistName", Type: "string", Description: "Playlist name."},
				{Name: "playlistId", Type: "string", Description: "Playlist ID."},
				{Name: "artistName", Type: "string", Required: true, Description: "Track artist."},
				{Name: "trackTitle", Type: "string", Required: true, Description: "Track title."},
			},
		},
		{
			Category:    CategoryPlaylistActions,
			Name:        "removePendingTracksFromNavidromePlaylist",
			Description: "Clear pending queued tracks from a saved playlist.",
			UseWhen:     "The user wants pending playlist additions removed before they are fulfilled.",
			Args: []ToolArgSpec{
				{Name: "playlistName", Type: "string", Description: "Playlist name."},
				{Name: "playlistId", Type: "string", Description: "Playlist ID."},
				{Name: "selection", Type: "string", Required: true, Description: `Which pending tracks to remove, such as "all".`},
			},
		},
		{
			Category:    CategoryPlaylistActions,
			Name:        "removeTrackFromNavidromePlaylist",
			Description: "Remove saved tracks from a Navidrome playlist.",
			UseWhen:     "The user wants tracks removed from an existing playlist.",
			Args: []ToolArgSpec{
				{Name: "playlistName", Type: "string", Description: "Playlist name."},
				{Name: "playlistId", Type: "string", Description: "Playlist ID."},
				{Name: "selection", Type: "string", Required: true, Description: `Which saved tracks to remove, such as "track 2" or "all".`},
			},
		},
		{
			Category:    CategoryCleanup,
			Name:        "lidarrCleanupCandidates",
			Description: "Preview cleanup candidates from Lidarr-managed content.",
			UseWhen:     "The user asks for cleanup candidates, duplicates, or stale content before applying cleanup.",
			Args: []ToolArgSpec{
				{Name: "scope", Type: "string", Description: "Cleanup scope."},
				{Name: "artist", Type: "string", Description: "Restrict to one artist."},
				{Name: "pathContains", Type: "string", Description: "Restrict by path fragment."},
				{Name: "olderThanDays", Type: "number", Description: "Minimum age."},
				{Name: "limit", Type: "number", Description: "Maximum candidates."},
			},
		},
		{
			Category:    CategoryCleanup,
			Name:        "startLidarrCleanupApplyPreview",
			Description: "Prepare a preview to apply a Lidarr cleanup action.",
			UseWhen:     "The user is ready to act on cleanup candidates. Preview first.",
			Args: []ToolArgSpec{
				{Name: "action", Type: "string", Required: true, Description: "Cleanup action to apply."},
				{Name: "selection", Type: "string", Required: true, Description: `Which candidates to act on, such as "all".`},
			},
		},
		{
			Category:    CategoryCleanup,
			Name:        "startArtistRemovalPreview",
			Description: "Prepare a preview for removing an artist from the library.",
			UseWhen:     "The user asks to remove an artist from their library. Preview first.",
			Args: []ToolArgSpec{
				{Name: "artistName", Type: "string", Required: true, Description: "Artist to remove."},
			},
			Example: `{"action":"query","tool":"startArtistRemovalPreview","args":{"artistName":"Warpaint"}}`,
		},
		{
			Category:    CategorySimilarity,
			Name:        "similarArtists",
			Description: "Find nearest artist matches already in the user's library.",
			UseWhen:     "Only when the user explicitly asks for nearest library matches, not general recommendations.",
			Args: []ToolArgSpec{
				{Name: "seedArtist", Type: "string", Required: true, Description: "Reference artist."},
				{Name: "provider", Type: "string", Description: "Similarity provider: local, audiomuse, or hybrid."},
				{Name: "limit", Type: "number", Description: "Maximum results."},
			},
		},
		{
			Category:    CategorySimilarity,
			Name:        "similarTracks",
			Description: "Find nearest track matches already in the user's library.",
			UseWhen:     "Only when the user explicitly asks for nearest library track matches or an instant-mix style seed expansion.",
			Args: []ToolArgSpec{
				{Name: "seedTrackId", Type: "string", Description: "Reference track ID."},
				{Name: "seedTrackTitle", Type: "string", Description: "Reference track title when ID is unavailable."},
				{Name: "seedArtistName", Type: "string", Description: "Reference artist when seedTrackTitle is used."},
				{Name: "provider", Type: "string", Description: "Similarity provider: local, audiomuse, or hybrid."},
				{Name: "excludeRecentDays", Type: "number", Description: "Exclude tracks played within this many days."},
				{Name: "excludeSeedArtist", Type: "boolean", Description: "Exclude tracks by the seed artist."},
				{Name: "limit", Type: "number", Description: "Maximum results."},
			},
		},
		{
			Category:    CategorySimilarity,
			Name:        "similarAlbums",
			Description: "Find nearest album matches already in the user's library.",
			UseWhen:     "Only when the user explicitly asks for nearest library matches, not general recommendations.",
			Args: []ToolArgSpec{
				{Name: "seedAlbum", Type: "string", Required: true, Description: "Reference album."},
				{Name: "limit", Type: "number", Description: "Maximum results."},
			},
		},
		{
			Category:    CategorySimilarity,
			Name:        "songPath",
			Description: "Find a bridge path between two songs.",
			UseWhen:     "The user explicitly wants a path, bridge, or gradual transition between two specific songs.",
			Args: []ToolArgSpec{
				{Name: "startTrackId", Type: "string", Description: "Start track ID when already known."},
				{Name: "startTrackTitle", Type: "string", Description: "Exact start track title. Preserve user-provided qualifiers like live, demo, mix, remaster, or parenthetical subtitles."},
				{Name: "startArtistName", Type: "string", Description: "Start track artist."},
				{Name: "endTrackId", Type: "string", Description: "End track ID when already known."},
				{Name: "endTrackTitle", Type: "string", Description: "Exact end track title. Preserve user-provided qualifiers like live, demo, mix, remaster, or parenthetical subtitles."},
				{Name: "endArtistName", Type: "string", Description: "End track artist."},
				{Name: "maxSteps", Type: "number", Description: "Requested path length cap."},
				{Name: "keepExactSize", Type: "boolean", Description: "Request an exact path size when possible."},
			},
			Example: `{"action":"query","tool":"songPath","args":{"startTrackTitle":"Heart-Shaped Box (original Steve Albini 1993 mix)","startArtistName":"Nirvana","endTrackTitle":"All Apologies","endArtistName":"Nirvana","maxSteps":18}}`,
		},
		{
			Category:    CategorySimilarity,
			Name:        "describeTrackSound",
			Description: "Describe the sonic profile of a specific track and ground it with nearby songs.",
			UseWhen:     "The user asks what a specific track sounds like, wants its sonic character described, or wants a compact track profile for one song.",
			Args: []ToolArgSpec{
				{Name: "trackId", Type: "string", Description: "Track ID when already known."},
				{Name: "trackTitle", Type: "string", Description: "Exact track title. Preserve user-provided qualifiers like live, demo, mix, remaster, or parenthetical subtitles."},
				{Name: "artistName", Type: "string", Description: "Track artist when trackTitle is used."},
				{Name: "neighborLimit", Type: "number", Description: "How many nearby tracks to include for grounding."},
			},
			Example: `{"action":"query","tool":"describeTrackSound","args":{"trackTitle":"Man Of War","artistName":"Radiohead","neighborLimit":5}}`,
		},
		{
			Category:    CategorySimilarity,
			Name:        "clusterScenes",
			Description: "List cluster playlists as sonic scenes.",
			UseWhen:     "The user asks for clusters, scenes, sonic regions, or wants a no-seed exploration starting point. Use this to find or disambiguate scene labels before relying on a sceneKey.",
			Args: []ToolArgSpec{
				{Name: "queryText", Type: "string", Description: "Optional scene-name filter."},
				{Name: "limit", Type: "number", Description: "Maximum scenes."},
				{Name: "sampleTracks", Type: "number", Description: "Sample tracks to include per scene."},
			},
			Example: `{"action":"query","tool":"clusterScenes","args":{"limit":6,"sampleTracks":3}}`,
		},
		{
			Category:    CategorySimilarity,
			Name:        "sceneTracks",
			Description: "Inspect the tracks inside one sonic scene.",
			UseWhen:     "The user asks what is inside a scene, wants the core tracks from a known scene, or follows up on a prior clusterScenes result. Use sceneKey only when it came from a prior tool result or server context; otherwise use sceneName or ask a clarifying question.",
			Args: []ToolArgSpec{
				{Name: "sceneKey", Type: "string", Description: "Exact stable scene key from a prior tool result or server context. Never synthesize or approximate this from a scene label."},
				{Name: "sceneName", Type: "string", Description: "User-facing scene name when no authoritative sceneKey is available yet."},
				{Name: "limit", Type: "number", Description: "Maximum tracks to return."},
			},
			Example: `{"action":"query","tool":"sceneTracks","args":{"sceneKey":"Indie_Rock_Alternative_Medium_Relaxed_Sad_automatic","limit":10}}`,
		},
		{
			Category:    CategorySimilarity,
			Name:        "sceneExpand",
			Description: "Find adjacent tracks that extend one sonic scene without repeating its stored tracks.",
			UseWhen:     "The user wants more tracks from the same region, asks to extend a scene, or gives modifiers like calmer, sadder, or less familiar relative to a known scene. Use sceneKey only when it came from a prior tool result or server context; otherwise use sceneName or ask a clarifying question.",
			Args: []ToolArgSpec{
				{Name: "sceneKey", Type: "string", Description: "Exact stable scene key from a prior tool result or server context. Never synthesize or approximate this from a scene label."},
				{Name: "sceneName", Type: "string", Description: "User-facing scene name when no authoritative sceneKey is available yet."},
				{Name: "queryText", Type: "string", Description: "Optional directional cue such as calmer, sadder, danceable, or less familiar."},
				{Name: "limit", Type: "number", Description: "Maximum tracks to return."},
				{Name: "seedCount", Type: "number", Description: "How many scene tracks to use as expansion seeds."},
				{Name: "provider", Type: "string", Description: "Similarity provider: local, audiomuse, or hybrid."},
				{Name: "excludeRecentDays", Type: "number", Description: "Exclude tracks played within this many days."},
			},
			Example: `{"action":"query","tool":"sceneExpand","args":{"sceneKey":"Indie_Rock_Alternative_Medium_Relaxed_Sad_automatic","queryText":"calmer and less familiar","limit":10,"provider":"hybrid"}}`,
		},
	}
}
