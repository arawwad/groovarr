package toolspec

import "strings"

type ToolSpec struct {
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
			Name:        "libraryStats",
			Description: "High-level counts for the user's library.",
			UseWhen:     "The user asks for overall library totals or a broad overview.",
			Example:     `{"action":"query","tool":"libraryStats","args":{}}`,
		},
		{
			Name:        "artists",
			Description: "List artists in the user's library.",
			UseWhen:     "The user asks for artists in their library or top artists without advanced grouping.",
			Args: []ToolArgSpec{
				{Name: "limit", Type: "number", Description: "Maximum results."},
				{Name: "minPlayCount", Type: "number", Description: "Only include artists with at least this many plays."},
			},
		},
		{
			Name:        "albums",
			Description: "List albums in the user's library.",
			UseWhen:     "Owned-album lists, album title lookups, or follow-ups like 'from those artists'. Not for song lookups or global best/top artist-discography requests unless library-limited.",
			Args: []ToolArgSpec{
				{Name: "artistName", Type: "string", Description: "Filter to one artist."},
				{Name: "artistNames", Type: "array<string>", Description: "Filter to multiple artists from prior context."},
				{Name: "queryText", Type: "string", Description: "Match album title or artist text for exact-lookups like 'do I have Dark Side of the Moon?'."},
				{Name: "genre", Type: "string", Description: "Filter by genre."},
				{Name: "year", Type: "number", Description: "Filter by release year."},
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
			Name:        "recentListeningSummary",
			Description: "Summaries for a listening window with top tracks and artists heard.",
			UseWhen:     "The user asks what they listened to in a recent period.",
			Args: []ToolArgSpec{
				{Name: "window", Type: "string", Description: "Named window such as last_week or last_month."},
				{Name: "playedSince", Type: "string", Description: "Lower bound time."},
				{Name: "playedUntil", Type: "string", Description: "Upper bound time."},
				{Name: "trackLimit", Type: "number", Description: "Top tracks to include."},
				{Name: "artistLimit", Type: "number", Description: "Top artists to include."},
			},
			Example: `{"action":"query","tool":"recentListeningSummary","args":{"window":"last_month","trackLimit":10,"artistLimit":6}}`,
		},
		{
			Name:        "artistListeningStats",
			Description: "Artist-level listening stats over a time window.",
			UseWhen:     "The user asks which artists they played most or least in a period.",
			Schema:      filterKeySchema(ArtistListeningStatsFilterKeys),
			Args: []ToolArgSpec{
				{Name: "filter", Type: "object", Description: "Use only the allowed filter keys listed below."},
				{Name: "sort", Type: "string", Description: "Sort order."},
				{Name: "limit", Type: "number", Description: "Maximum results."},
			},
			Example: `{"action":"query","tool":"artistListeningStats","args":{"filter":{"playedSince":"2026-02-10","playedUntil":"2026-03-10"},"sort":"plays","limit":10}}`,
		},
		{
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
			Name:        "discoverAlbums",
			Description: "Discover albums beyond the user's current library.",
			UseWhen:     "Default recommendation tool for best, top, essential, mood, artist-discography, or 'like X but more Y' album requests unless the user explicitly wants only library-owned results.",
			Args: []ToolArgSpec{
				{Name: "query", Type: "string", Required: true, Description: "Discovery request."},
				{Name: "limit", Type: "number", Description: "Maximum results."},
			},
			Example: `{"action":"query","tool":"discoverAlbums","args":{"query":"records like Talk Talk's Laughing Stock but warmer and more spacious","limit":5}}`,
		},
		{
			Name:        "matchDiscoveredAlbumsInLidarr",
			Description: "Check whether previously discovered albums are available or matched in Lidarr.",
			UseWhen:     "The user asks whether discovered albums are available, matched, or addable.",
			Args: []ToolArgSpec{
				{Name: "selection", Type: "string", Required: true, Description: `Which discovered albums to inspect, such as "all" or "first 3".`},
			},
		},
		{
			Name:        "startDiscoveredAlbumsApplyPreview",
			Description: "Prepare a preview for adding or monitoring discovered albums.",
			UseWhen:     "The user is ready to act on discovered albums. Preview first rather than applying directly.",
			Args: []ToolArgSpec{
				{Name: "selection", Type: "string", Required: true, Description: `Which discovered albums to act on, such as "all" or a specific album.`},
			},
		},
		{
			Name:        "startPlaylistCreatePreview",
			Description: "Preview a new playlist.",
			UseWhen:     "Default for playlist creation.",
			Args: []ToolArgSpec{
				{Name: "prompt", Type: "string", Required: true, Description: "Playlist request."},
				{Name: "playlistName", Type: "string", Description: "Optional playlist title."},
				{Name: "trackCount", Type: "number", Description: "Desired number of tracks."},
			},
		},
		{
			Name:        "resolvePlaylistTracks",
			Description: "Resolve previously planned playlist candidates against available tracks.",
			UseWhen:     "The user asks to resolve or inspect availability for a planned playlist.",
			Args: []ToolArgSpec{
				{Name: "selection", Type: "string", Required: true, Description: `Which planned tracks to resolve, such as "all".`},
			},
		},
		{
			Name:        "queueMissingPlaylistTracks",
			Description: "Queue unresolved playlist tracks for download or fetching.",
			UseWhen:     "The user explicitly asks to queue or download missing planned playlist tracks.",
			Args: []ToolArgSpec{
				{Name: "selection", Type: "string", Required: true, Description: `Which planned tracks to queue, such as "all missing".`},
				{Name: "confirm", Type: "boolean", Required: true, Description: "Must be true for execution."},
			},
		},
		{
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
			Name:        "startPlaylistRefreshPreview",
			Description: "Preview a playlist refresh.",
			UseWhen:     "Use when the user wants to refresh a playlist.",
			Args: []ToolArgSpec{
				{Name: "playlistName", Type: "string", Required: true, Description: "Existing playlist name."},
				{Name: "replaceCount", Type: "number", Description: "How many tracks to replace."},
			},
		},
		{
			Name:        "startPlaylistRepairPreview",
			Description: "Preview a playlist repair.",
			UseWhen:     "Use when the user asks to repair a playlist.",
			Args: []ToolArgSpec{
				{Name: "playlistName", Type: "string", Required: true, Description: "Existing playlist name."},
			},
		},
		{
			Name:        "navidromePlaylists",
			Description: "List saved Navidrome playlists.",
			UseWhen:     "The user asks what playlists exist or wants to search playlist names.",
			Args: []ToolArgSpec{
				{Name: "limit", Type: "number", Description: "Maximum playlists."},
				{Name: "query", Type: "string", Description: "Playlist name search."},
			},
		},
		{
			Name:        "navidromePlaylist",
			Description: "Inspect tracks in one saved Navidrome playlist.",
			UseWhen:     "The user asks what is in a playlist.",
			Args: []ToolArgSpec{
				{Name: "playlistName", Type: "string", Description: "Playlist name."},
				{Name: "playlistId", Type: "string", Description: "Playlist ID."},
			},
		},
		{
			Name:        "navidromePlaylistState",
			Description: "Inspect a saved playlist plus pending queued additions.",
			UseWhen:     "The user asks for the current state of a playlist including pending changes.",
			Args: []ToolArgSpec{
				{Name: "playlistName", Type: "string", Description: "Playlist name."},
				{Name: "playlistId", Type: "string", Description: "Playlist ID."},
			},
		},
		{
			Name:        "addTrackToNavidromePlaylist",
			Description: "Add a specific available track directly to a saved playlist.",
			UseWhen:     "The user explicitly wants a known track added to a saved playlist right now.",
			Args: []ToolArgSpec{
				{Name: "playlistName", Type: "string", Description: "Playlist name."},
				{Name: "playlistId", Type: "string", Description: "Playlist ID."},
				{Name: "artistName", Type: "string", Required: true, Description: "Track artist."},
				{Name: "trackTitle", Type: "string", Required: true, Description: "Track title."},
			},
		},
		{
			Name:        "queueTrackForNavidromePlaylist",
			Description: "Queue a specific missing track for a saved playlist.",
			UseWhen:     "The user explicitly wants a specific track queued for a playlist.",
			Args: []ToolArgSpec{
				{Name: "playlistName", Type: "string", Description: "Playlist name."},
				{Name: "playlistId", Type: "string", Description: "Playlist ID."},
				{Name: "artistName", Type: "string", Required: true, Description: "Track artist."},
				{Name: "trackTitle", Type: "string", Required: true, Description: "Track title."},
			},
		},
		{
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
			Name:        "startLidarrCleanupApplyPreview",
			Description: "Prepare a preview to apply a Lidarr cleanup action.",
			UseWhen:     "The user is ready to act on cleanup candidates. Preview first.",
			Args: []ToolArgSpec{
				{Name: "action", Type: "string", Required: true, Description: "Cleanup action to apply."},
				{Name: "selection", Type: "string", Required: true, Description: `Which candidates to act on, such as "all".`},
			},
		},
		{
			Name:        "startArtistRemovalPreview",
			Description: "Prepare a preview for removing an artist from the library.",
			UseWhen:     "The user asks to remove an artist from their library. Preview first.",
			Args: []ToolArgSpec{
				{Name: "artistName", Type: "string", Required: true, Description: "Artist to remove."},
			},
			Example: `{"action":"query","tool":"startArtistRemovalPreview","args":{"artistName":"Warpaint"}}`,
		},
		{
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
			Name:        "similarAlbums",
			Description: "Find nearest album matches already in the user's library.",
			UseWhen:     "Only when the user explicitly asks for nearest library matches, not general recommendations.",
			Args: []ToolArgSpec{
				{Name: "seedAlbum", Type: "string", Required: true, Description: "Reference album."},
				{Name: "limit", Type: "number", Description: "Maximum results."},
			},
		},
	}
}
