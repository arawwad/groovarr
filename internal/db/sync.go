package db

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pgvector/pgvector-go"
	"github.com/rs/zerolog/log"
	_ "modernc.org/sqlite"
)

type Syncer struct {
	navDB                *sql.DB
	pgClient             *Client
	embeddings           string
	artistLimit          int
	albumLimit           int
	trackLimit           int
	embeddingsBatchSize  int
	embeddingsBatchPause time.Duration
	embeddingsRetries    int
	embeddingsBackoff    time.Duration
	lastFMEnabled        bool
	lastFMAPIKey         string
	lastFMBaseURL        string
	lastFMAlbumCap       int
	lastFMMinGap         time.Duration
	lastLastFMCall       time.Time
	musicBrainzEnabled   bool
	musicBrainzBaseURL   string
	musicBrainzUserAgent string
	musicBrainzAlbumCap  int
	musicBrainzMinGap    time.Duration
	lastMusicBrainzCall  time.Time
}

type EmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

const currentAlbumEmbeddingVersion = "album-doc-v4"

func NewSyncer(navidromePath string, pgClient *Client, embeddingsURL string) (*Syncer, error) {
	db, err := sql.Open("sqlite", navidromePath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open navidrome db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping navidrome db: %w", err)
	}
	return &Syncer{
		navDB:                db,
		pgClient:             pgClient,
		embeddings:           embeddingsURL,
		artistLimit:          envInt("SYNC_ARTIST_LIMIT", 150),
		albumLimit:           envInt("SYNC_ALBUM_LIMIT", 300),
		trackLimit:           envInt("SYNC_TRACK_LIMIT", 1200),
		embeddingsBatchSize:  envInt("SYNC_EMBED_BATCH_SIZE", 100),
		embeddingsBatchPause: time.Duration(envInt("SYNC_EMBED_BATCH_PAUSE_MS", 120)) * time.Millisecond,
		embeddingsRetries:    envInt("SYNC_EMBED_RETRIES", 2),
		embeddingsBackoff:    time.Duration(envInt("SYNC_EMBED_RETRY_BACKOFF_MS", 250)) * time.Millisecond,
		lastFMEnabled:        envBool("SYNC_LASTFM_ENABLED", false),
		lastFMAPIKey:         strings.TrimSpace(os.Getenv("LASTFM_API_KEY")),
		lastFMBaseURL:        strings.TrimRight(getEnvDefault("SYNC_LASTFM_BASE_URL", "https://ws.audioscrobbler.com/2.0"), "/"),
		lastFMAlbumCap:       envInt("SYNC_LASTFM_ALBUMS_PER_SYNC", 10),
		lastFMMinGap:         250 * time.Millisecond,
		musicBrainzEnabled:   envBool("SYNC_MUSICBRAINZ_ENABLED", false),
		musicBrainzBaseURL:   strings.TrimRight(getEnvDefault("SYNC_MUSICBRAINZ_BASE_URL", "https://musicbrainz.org/ws/2"), "/"),
		musicBrainzUserAgent: getEnvDefault("SYNC_MUSICBRAINZ_USER_AGENT", "groovarr/0.1 (local sync enrichment)"),
		musicBrainzAlbumCap:  envInt("SYNC_MUSICBRAINZ_ALBUMS_PER_SYNC", 5),
		musicBrainzMinGap:    time.Duration(envInt("SYNC_MUSICBRAINZ_MIN_GAP_MS", 1100)) * time.Millisecond,
	}, nil
}

func (s *Syncer) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Info().Msg("Starting sync daemon")
	s.syncOnce(ctx)

	for {
		select {
		case <-ticker.C:
			s.syncOnce(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Syncer) syncOnce(ctx context.Context) {
	log.Info().Msg("Starting sync cycle")
	start := time.Now()

	if err := s.syncArtists(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to sync artists")
	}
	if err := s.syncAlbums(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to sync albums")
	}
	if err := s.syncTracks(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to sync tracks")
	}
	if err := s.syncPlayEvents(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to sync play events")
	}

	if err := s.pgClient.SetLastSync(ctx, time.Now()); err != nil {
		log.Error().Err(err).Msg("Failed to update last sync")
	}

	log.Info().Dur("duration", time.Since(start)).Msg("Sync cycle complete")
}

func (s *Syncer) syncArtists(ctx context.Context) error {
	query := fmt.Sprintf(`SELECT a.id, a.name, COALESCE(an.rating, 0), COALESCE(an.play_count, 0)
	          FROM artist a
	          LEFT JOIN annotation an ON a.id = an.item_id AND an.item_type = 'artist'
	          ORDER BY COALESCE(an.play_count, 0) DESC LIMIT %d`, s.artistLimit)

	rows, err := s.navDB.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	var artists []Artist
	for rows.Next() {
		var a Artist
		if err := rows.Scan(&a.ID, &a.Name, &a.Rating, &a.PlayCount); err != nil {
			return err
		}
		artists = append(artists, a)
	}

	if len(artists) == 0 {
		return nil
	}

	ids := make([]string, len(artists))
	names := make([]string, len(artists))
	for i, a := range artists {
		ids[i] = a.ID
		names[i] = a.Name
	}
	existing, err := s.pgClient.GetArtistIDsWithEmbedding(ctx, ids)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to check existing artist embeddings")
		existing = map[string]struct{}{}
	}
	missingIdx := make([]int, 0, len(artists))
	missingTexts := make([]string, 0, len(artists))
	for i, a := range artists {
		if _, ok := existing[a.ID]; ok {
			continue
		}
		missingIdx = append(missingIdx, i)
		missingTexts = append(missingTexts, names[i])
	}
	embeddings := make([][]float32, len(missingTexts))
	if len(missingTexts) > 0 {
		embeddings, err = s.fetchEmbeddingsBatched(ctx, missingTexts, s.embeddingsBatchSize)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to fetch missing artist embeddings, syncing without them")
			embeddings = make([][]float32, len(missingTexts))
		}
	}
	embedByArtistIdx := make(map[int][]float32, len(missingIdx))
	for j, idx := range missingIdx {
		if j < len(embeddings) {
			embedByArtistIdx[idx] = embeddings[j]
		}
	}

	for i, a := range artists {
		if e, ok := embedByArtistIdx[i]; ok && len(e) > 0 {
			a.Embedding = pgvector.NewVector(e)
		}
		if err := s.pgClient.UpsertArtist(ctx, a); err != nil {
			log.Error().Err(err).Str("artist", a.Name).Msg("Failed to upsert artist")
		}
	}

	log.Info().
		Int("count", len(artists)).
		Int("missing_embeddings", len(missingTexts)).
		Int("cached_embeddings", len(existing)).
		Msg("Synced artists")
	return nil
}

func (s *Syncer) syncAlbums(ctx context.Context) error {
	totalAlbums, err := s.countNavAlbums(ctx)
	if err != nil {
		return err
	}
	albums, err := s.fetchNavAlbums(ctx)
	if err != nil {
		return err
	}

	ids := make([]string, len(albums))
	for i, a := range albums {
		ids[i] = a.ID
	}
	existingMetadata, err := s.pgClient.GetAlbumMetadata(ctx, ids)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load existing album metadata")
		existingMetadata = map[string]map[string]interface{}{}
	}
	for i := range albums {
		if meta, ok := existingMetadata[albums[i].ID]; ok && len(meta) > 0 {
			albums[i].Metadata = meta
		} else {
			albums[i].Metadata = map[string]interface{}{}
		}
	}
	if s.musicBrainzEnabled {
		enriched, err := s.enrichAlbumsWithMusicBrainz(ctx, albums)
		if err != nil {
			log.Warn().Err(err).Msg("MusicBrainz album enrichment failed")
		} else if enriched > 0 {
			log.Info().Int("albums_enriched", enriched).Msg("Enriched albums from MusicBrainz")
		}
	}
	if s.lastFMEnabled && s.lastFMAPIKey != "" {
		enriched, err := s.enrichAlbumsWithLastFM(ctx, albums)
		if err != nil {
			log.Warn().Err(err).Msg("Last.fm album enrichment failed")
		} else if enriched > 0 {
			log.Info().Int("albums_enriched", enriched).Msg("Enriched albums from Last.fm")
		}
	}
	trackTitlesByAlbum, err := s.fetchAlbumTrackTitles(ctx, ids, 6)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load representative album track titles")
		trackTitlesByAlbum = map[string][]string{}
	}
	for i := range albums {
		albums[i].Genre = canonicalAlbumGenre(albums[i])
		albums[i].EmbeddingDocument = buildAlbumEmbeddingDocument(albums[i], trackTitlesByAlbum[albums[i].ID])
		albums[i].EmbeddingVersion = currentAlbumEmbeddingVersion
	}
	existingStates, err := s.pgClient.GetAlbumEmbeddingStates(ctx, ids)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to check existing album embedding state")
		existingStates = map[string]AlbumEmbeddingState{}
	}
	reembedIdx := make([]int, 0, len(albums))
	reembedDocs := make([]string, 0, len(albums))
	cachedEmbeddings := 0
	for i, a := range albums {
		state, ok := existingStates[a.ID]
		if ok && state.HasEmbedding {
			cachedEmbeddings++
		}
		if ok && state.HasEmbedding && state.Version == a.EmbeddingVersion && state.Document == a.EmbeddingDocument {
			continue
		}
		reembedIdx = append(reembedIdx, i)
		reembedDocs = append(reembedDocs, a.EmbeddingDocument)
	}
	embeddings := make([][]float32, len(reembedDocs))
	if len(reembedDocs) > 0 {
		embeddings, err = s.fetchEmbeddingsBatched(ctx, reembedDocs, s.embeddingsBatchSize)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to fetch refreshed album embeddings, syncing without them")
			embeddings = make([][]float32, len(reembedDocs))
		}
	}
	embedByAlbumIdx := make(map[int][]float32, len(reembedIdx))
	for j, idx := range reembedIdx {
		if j < len(embeddings) {
			embedByAlbumIdx[idx] = embeddings[j]
		}
	}

	for i, a := range albums {
		if e, ok := embedByAlbumIdx[i]; ok && len(e) > 0 {
			a.Embedding = pgvector.NewVector(e)
		}
		if err := s.pgClient.UpsertAlbum(ctx, a); err != nil {
			log.Error().Err(err).Str("album", a.Name).Msg("Failed to upsert album")
		}
	}

	if shouldPruneSnapshot(totalAlbums, len(albums)) {
		staleAlbumIDs, err := s.pgClient.GetStaleAlbumIDs(ctx, ids)
		if err != nil {
			return err
		}
		if len(staleAlbumIDs) > 0 {
			deletedTracks, err := s.pgClient.DeleteTracksByAlbumIDs(ctx, staleAlbumIDs)
			if err != nil {
				return err
			}
			deletedAlbums, err := s.pgClient.DeleteAlbumsByIDs(ctx, staleAlbumIDs)
			if err != nil {
				return err
			}
			log.Info().
				Int("stale_albums", len(staleAlbumIDs)).
				Int64("deleted_tracks", deletedTracks).
				Int64("deleted_albums", deletedAlbums).
				Msg("Pruned stale albums from postgres")
		}
	} else {
		log.Info().
			Int("navidrome_albums", totalAlbums).
			Int("synced_albums", len(albums)).
			Int("album_page_size", syncPageSize(s.albumLimit, 300)).
			Msg("Skipped stale album pruning because the sync did not fetch a full album snapshot")
	}

	log.Info().
		Int("count", len(albums)).
		Int("reembedded_albums", len(reembedDocs)).
		Int("cached_embeddings", cachedEmbeddings).
		Str("album_embedding_version", currentAlbumEmbeddingVersion).
		Msg("Synced albums")
	return nil
}

func buildAlbumEmbeddingDocument(album Album, trackTitles []string) string {
	mbGenres := extractAlbumMetadataStrings(album.Metadata, "musicbrainz", "genres")
	mbTags := extractAlbumMetadataStrings(album.Metadata, "musicbrainz", "tags")
	lastfmTags := extractAlbumMetadataStrings(album.Metadata, "lastfm", "tags")
	descriptors := mergeAlbumEmbeddingDescriptors(mbTags, mbGenres, lastfmTags, splitAlbumEmbeddingGenre(album.Genre))

	parts := []string{
		fmt.Sprintf("Album: %s", strings.TrimSpace(album.Name)),
		fmt.Sprintf("Artist: %s", strings.TrimSpace(album.ArtistName)),
		"Type: album in the user's library",
	}
	if album.Year != nil && *album.Year > 0 {
		parts = append(parts, fmt.Sprintf("Year: %d", *album.Year))
		parts = append(parts, fmt.Sprintf("Decade: %ds", (*album.Year/10)*10))
	}
	if album.Genre != nil && strings.TrimSpace(*album.Genre) != "" {
		parts = append(parts, fmt.Sprintf("Genre: %s", strings.TrimSpace(*album.Genre)))
	}
	if len(mbGenres) > 0 {
		parts = append(parts, fmt.Sprintf("MusicBrainz genres: %s", strings.Join(mbGenres, ", ")))
	}
	if len(mbTags) > 0 {
		parts = append(parts, fmt.Sprintf("MusicBrainz tags: %s", strings.Join(mbTags, ", ")))
	}
	if len(lastfmTags) > 0 {
		parts = append(parts, fmt.Sprintf("Last.fm tags: %s", strings.Join(lastfmTags, ", ")))
	}
	if len(descriptors) > 0 {
		parts = append(parts, fmt.Sprintf("Descriptors: %s", strings.Join(descriptors, ", ")))
		parts = append(parts, fmt.Sprintf("Mood and style: %s", strings.Join(descriptors, ", ")))
	}
	if primaryType := extractAlbumMetadataString(album.Metadata, "musicbrainz", "primary_type"); primaryType != "" {
		parts = append(parts, fmt.Sprintf("Release group type: %s", primaryType))
	}
	if disambiguation := extractAlbumMetadataString(album.Metadata, "musicbrainz", "disambiguation"); disambiguation != "" {
		parts = append(parts, fmt.Sprintf("Disambiguation: %s", disambiguation))
	}
	if len(trackTitles) > 0 {
		parts = append(parts, fmt.Sprintf("Tracks: %s", strings.Join(trackTitles, ", ")))
	}
	return strings.Join(parts, "\n")
}

func splitAlbumEmbeddingGenre(genre *string) []string {
	if genre == nil || strings.TrimSpace(*genre) == "" {
		return nil
	}
	parts := strings.Split(*genre, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func canonicalAlbumGenre(album Album) *string {
	genres := canonicalAlbumGenres(album)
	if len(genres) == 0 {
		return nil
	}
	joined := strings.Join(genres, ", ")
	return &joined
}

func canonicalAlbumGenres(album Album) []string {
	out := make([]string, 0, 4)
	seen := map[string]struct{}{}
	appendUnique := func(values []string, allowLoose bool) {
		for _, value := range values {
			normalized, ok := normalizeCanonicalGenreValue(value, allowLoose)
			if !ok {
				continue
			}
			key := strings.ToLower(normalized)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, normalized)
			if len(out) >= 4 {
				return
			}
		}
	}

	appendUnique(splitAlbumEmbeddingGenre(album.Genre), false)
	appendUnique(extractAlbumMetadataStrings(album.Metadata, "musicbrainz", "genres"), false)
	if len(out) == 0 {
		appendUnique(extractAlbumMetadataStrings(album.Metadata, "lastfm", "tags"), true)
	}
	return out
}

func normalizeCanonicalGenreValue(value string, allowLoose bool) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}

	lower := strings.ToLower(value)
	if strings.ContainsAny(lower, "0123456789") {
		return "", false
	}
	if strings.Contains(lower, "/") {
		return "", false
	}

	words := strings.Fields(lower)
	if len(words) == 0 || len(words) > 4 {
		return "", false
	}

	blockedExact := map[string]struct{}{
		"favourite albums": {},
		"favorite albums":  {},
		"albums i own":     {},
		"seen live":        {},
		"favorites":        {},
		"favourites":       {},
		"favorite":         {},
		"favourite":        {},
	}
	if _, blocked := blockedExact[lower]; blocked {
		return "", false
	}

	blockedContains := []string{
		"album",
		"albums",
		"artist",
		"artists",
		"vocal",
		"vocals",
		"soundtrack",
		"collection",
		"favorite",
		"favourite",
		"seen live",
		"owned",
		"own",
	}
	for _, token := range blockedContains {
		if strings.Contains(lower, token) {
			return "", false
		}
	}

	if !allowLoose {
		return lower, true
	}

	allowedLooseTokens := []string{
		"ambient", "alternative", "art", "blues", "chamber", "chillwave", "classical", "country",
		"downtempo", "dream", "drone", "dub", "electronic", "emo", "experimental", "folk", "funk",
		"grunge", "hip", "hop", "house", "indie", "industrial", "instrumental", "jazz", "lo", "fi",
		"metal", "new", "noise", "pop", "post", "prog", "psychedelic", "punk", "rap", "reggae",
		"rock", "shoegaze", "slowcore", "soul", "synth", "synthpop", "techno", "trip", "wave",
	}
	for _, word := range words {
		for _, token := range allowedLooseTokens {
			if word == token {
				return lower, true
			}
		}
	}
	return "", false
}

func mergeAlbumEmbeddingDescriptors(groups ...[]string) []string {
	out := make([]string, 0, 10)
	seen := map[string]struct{}{}
	for _, group := range groups {
		for _, item := range group {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			key := strings.ToLower(item)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, item)
			if len(out) >= 8 {
				return out
			}
		}
	}
	return out
}

func (s *Syncer) fetchAlbumTrackTitles(ctx context.Context, albumIDs []string, perAlbumLimit int) (map[string][]string, error) {
	out := make(map[string][]string)
	if len(albumIDs) == 0 {
		return out, nil
	}
	if perAlbumLimit <= 0 {
		perAlbumLimit = 6
	}

	placeholders := make([]string, len(albumIDs))
	args := make([]interface{}, len(albumIDs))
	for i, id := range albumIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf(
		`SELECT album_id, title
		   FROM media_file
		  WHERE missing = FALSE
		    AND album_id IN (%s)
		  ORDER BY album_id, title`,
		strings.Join(placeholders, ","),
	)
	rows, err := s.navDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var albumID string
		var title string
		if err := rows.Scan(&albumID, &title); err != nil {
			return nil, err
		}
		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}
		if len(out[albumID]) >= perAlbumLimit {
			continue
		}
		out[albumID] = append(out[albumID], title)
	}
	return out, rows.Err()
}

func (s *Syncer) enrichAlbumsWithMusicBrainz(ctx context.Context, albums []Album) (int, error) {
	if !s.musicBrainzEnabled || len(albums) == 0 {
		return 0, nil
	}
	remaining := s.musicBrainzAlbumCap
	if remaining <= 0 {
		return 0, nil
	}

	enriched := 0
	for i := range albums {
		if remaining <= 0 {
			break
		}
		if hasMusicBrainzMetadata(albums[i].Metadata) {
			continue
		}
		meta, err := s.fetchMusicBrainzAlbumMetadata(ctx, albums[i])
		if err != nil {
			return enriched, err
		}
		if len(meta) == 0 {
			continue
		}
		if albums[i].Metadata == nil {
			albums[i].Metadata = map[string]interface{}{}
		}
		albums[i].Metadata["musicbrainz"] = meta
		enriched++
		remaining--
	}
	return enriched, nil
}

func hasMusicBrainzMetadata(metadata map[string]interface{}) bool {
	if len(metadata) == 0 {
		return false
	}
	section, ok := metadata["musicbrainz"].(map[string]interface{})
	if !ok {
		return false
	}
	status, _ := section["status"].(string)
	return strings.EqualFold(strings.TrimSpace(status), "matched")
}

func (s *Syncer) enrichAlbumsWithLastFM(ctx context.Context, albums []Album) (int, error) {
	if !s.lastFMEnabled || s.lastFMAPIKey == "" || len(albums) == 0 {
		return 0, nil
	}
	remaining := s.lastFMAlbumCap
	if remaining <= 0 {
		return 0, nil
	}

	enriched := 0
	for i := range albums {
		if remaining <= 0 {
			break
		}
		if hasLastFMMetadata(albums[i].Metadata) {
			continue
		}
		meta, err := s.fetchLastFMAlbumMetadata(ctx, albums[i])
		if err != nil {
			if ctx.Err() != nil {
				return enriched, ctx.Err()
			}
			log.Warn().
				Err(err).
				Str("artist", albums[i].ArtistName).
				Str("album", albums[i].Name).
				Msg("Last.fm album enrichment request failed; continuing")
			continue
		}
		if len(meta) == 0 {
			continue
		}
		if albums[i].Metadata == nil {
			albums[i].Metadata = map[string]interface{}{}
		}
		albums[i].Metadata["lastfm"] = meta
		enriched++
		remaining--
	}
	return enriched, nil
}

func hasLastFMMetadata(metadata map[string]interface{}) bool {
	if len(metadata) == 0 {
		return false
	}
	section, ok := metadata["lastfm"].(map[string]interface{})
	if !ok {
		return false
	}
	status, _ := section["status"].(string)
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "matched", "not_found":
		return true
	default:
		return false
	}
}

func (s *Syncer) fetchMusicBrainzAlbumMetadata(ctx context.Context, album Album) (map[string]interface{}, error) {
	artist := strings.TrimSpace(album.ArtistName)
	name := normalizeMusicBrainzAlbumTitle(album.Name)
	if artist == "" || name == "" {
		return nil, nil
	}

	query := fmt.Sprintf(`artist:"%s" AND releasegroup:"%s"`, escapeMusicBrainzTerm(artist), escapeMusicBrainzTerm(name))
	searchURL := s.musicBrainzBaseURL + "/release-group?" + url.Values{
		"query": {query},
		"fmt":   {"json"},
		"limit": {"1"},
	}.Encode()

	var search struct {
		ReleaseGroups []struct {
			ID string `json:"id"`
		} `json:"release-groups"`
	}
	if err := s.fetchMusicBrainzJSON(ctx, searchURL, &search); err != nil {
		return nil, err
	}
	if len(search.ReleaseGroups) == 0 || strings.TrimSpace(search.ReleaseGroups[0].ID) == "" {
		return map[string]interface{}{
			"status":     "not_found",
			"checked_at": time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	releaseGroupID := search.ReleaseGroups[0].ID
	lookupURL := fmt.Sprintf(
		"%s/release-group/%s?%s",
		s.musicBrainzBaseURL,
		url.PathEscape(releaseGroupID),
		url.Values{
			"inc": {"genres+tags"},
			"fmt": {"json"},
		}.Encode(),
	)
	var lookup struct {
		ID               string `json:"id"`
		Title            string `json:"title"`
		PrimaryType      string `json:"primary-type"`
		Disambiguation   string `json:"disambiguation"`
		FirstReleaseDate string `json:"first-release-date"`
		Genres           []struct {
			Name string `json:"name"`
		} `json:"genres"`
		Tags []struct {
			Name string `json:"name"`
		} `json:"tags"`
	}
	if err := s.fetchMusicBrainzJSON(ctx, lookupURL, &lookup); err != nil {
		return nil, err
	}

	genres := make([]string, 0, len(lookup.Genres))
	for _, genre := range lookup.Genres {
		name := strings.TrimSpace(genre.Name)
		if name != "" {
			genres = append(genres, name)
		}
	}
	tags := make([]string, 0, len(lookup.Tags))
	for _, tag := range lookup.Tags {
		name := strings.TrimSpace(tag.Name)
		if name != "" {
			tags = append(tags, name)
		}
	}

	return map[string]interface{}{
		"status":             "matched",
		"checked_at":         time.Now().UTC().Format(time.RFC3339),
		"release_group_id":   lookup.ID,
		"title":              lookup.Title,
		"primary_type":       lookup.PrimaryType,
		"disambiguation":     lookup.Disambiguation,
		"first_release_date": lookup.FirstReleaseDate,
		"genres":             genres,
		"tags":               tags,
	}, nil
}

func (s *Syncer) fetchLastFMAlbumMetadata(ctx context.Context, album Album) (map[string]interface{}, error) {
	artist := strings.TrimSpace(album.ArtistName)
	name := normalizeMusicBrainzAlbumTitle(album.Name)
	if artist == "" || name == "" {
		return nil, nil
	}

	endpoint := s.lastFMBaseURL + "/?" + url.Values{
		"method":      {"album.getInfo"},
		"artist":      {artist},
		"album":       {name},
		"api_key":     {s.lastFMAPIKey},
		"format":      {"json"},
		"autocorrect": {"1"},
	}.Encode()

	var result struct {
		Error   int    `json:"error"`
		Message string `json:"message"`
		Album   struct {
			Name      string      `json:"name"`
			Artist    string      `json:"artist"`
			MBID      string      `json:"mbid"`
			URL       string      `json:"url"`
			Listeners string      `json:"listeners"`
			PlayCount string      `json:"playcount"`
			Tags      interface{} `json:"tags"`
			Wiki      struct {
				Summary   string `json:"summary"`
				Published string `json:"published"`
			} `json:"wiki"`
		} `json:"album"`
	}
	if err := s.fetchLastFMJSON(ctx, endpoint, &result); err != nil {
		return nil, err
	}
	if result.Error != 0 {
		if result.Error == 6 || result.Error == 7 {
			return map[string]interface{}{
				"status":     "not_found",
				"checked_at": time.Now().UTC().Format(time.RFC3339),
			}, nil
		}
		return nil, fmt.Errorf("lastfm request failed: code=%d message=%q", result.Error, strings.TrimSpace(result.Message))
	}

	tags := extractLastFMTags(result.Album.Tags)
	seen := map[string]struct{}{}
	filteredTags := make([]string, 0, len(tags))
	for _, tag := range tags {
		name := strings.TrimSpace(tag)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		filteredTags = append(filteredTags, name)
		if len(filteredTags) >= 12 {
			break
		}
	}
	tags = filteredTags
	if strings.TrimSpace(result.Album.Name) == "" && len(tags) == 0 {
		return map[string]interface{}{
			"status":     "not_found",
			"checked_at": time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	meta := map[string]interface{}{
		"status":     "matched",
		"checked_at": time.Now().UTC().Format(time.RFC3339),
		"name":       strings.TrimSpace(result.Album.Name),
		"artist":     strings.TrimSpace(result.Album.Artist),
		"mbid":       strings.TrimSpace(result.Album.MBID),
		"url":        strings.TrimSpace(result.Album.URL),
		"listeners":  strings.TrimSpace(result.Album.Listeners),
		"playcount":  strings.TrimSpace(result.Album.PlayCount),
		"tags":       tags,
	}
	if summary := strings.TrimSpace(result.Album.Wiki.Summary); summary != "" {
		meta["summary"] = summary
	}
	if published := strings.TrimSpace(result.Album.Wiki.Published); published != "" {
		meta["published"] = published
	}
	return meta, nil
}

func extractLastFMTags(raw interface{}) []string {
	switch value := raw.(type) {
	case map[string]interface{}:
		return extractLastFMTags(value["tag"])
	case []interface{}:
		out := make([]string, 0, len(value))
		for _, item := range value {
			switch tag := item.(type) {
			case map[string]interface{}:
				name, _ := tag["name"].(string)
				name = strings.TrimSpace(name)
				if name != "" {
					out = append(out, name)
				}
			case string:
				tag = strings.TrimSpace(tag)
				if tag != "" {
					out = append(out, tag)
				}
			}
		}
		return out
	case map[string]string:
		name := strings.TrimSpace(value["name"])
		if name == "" {
			return nil
		}
		return []string{name}
	case string:
		value = strings.TrimSpace(value)
		if value == "" {
			return nil
		}
		return []string{value}
	default:
		return nil
	}
}

func (s *Syncer) fetchMusicBrainzJSON(ctx context.Context, endpoint string, out interface{}) error {
	if s.musicBrainzMinGap > 0 {
		wait := s.musicBrainzMinGap - time.Since(s.lastMusicBrainzCall)
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", s.musicBrainzUserAgent)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	s.lastMusicBrainzCall = time.Now()
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("musicbrainz request failed: status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (s *Syncer) fetchLastFMJSON(ctx context.Context, endpoint string, out interface{}) error {
	if s.lastFMMinGap > 0 {
		wait := s.lastFMMinGap - time.Since(s.lastLastFMCall)
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "groovarr/0.1 (lastfm sync enrichment)")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	s.lastLastFMCall = time.Now()
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("lastfm request failed: status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func escapeMusicBrainzTerm(value string) string {
	value = strings.TrimSpace(value)
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return replacer.Replace(value)
}

var musicBrainzTitleCleanupPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\s*\((?:[^)]*\b(?:deluxe|edition|remaster(?:ed)?|anniversary|bonus|expanded|reissue|version|disc|cd|rykodisc)\b[^)]*)\)\s*$`),
	regexp.MustCompile(`(?i)\s*\[(?:[^\]]*\b(?:deluxe|edition|remaster(?:ed)?|anniversary|bonus|expanded|reissue|version|disc|cd|rykodisc)\b[^\]]*)\]\s*$`),
	regexp.MustCompile(`(?i)\s*[-:]\s*(?:\d{4}\s+)?(?:deluxe|bonus|expanded|anniversary|special|collector'?s)\s+edition\s*$`),
	regexp.MustCompile(`(?i)\s*[-:]\s*(?:\d{4}\s+)?remaster(?:ed)?\s*$`),
	regexp.MustCompile(`(?i)\s+(?:\d{4}\s+)?(?:deluxe|bonus|expanded|anniversary|special|collector'?s)\s+edition\s*$`),
	regexp.MustCompile(`(?i)\s+(?:\d{4}\s+)?remaster(?:ed)?\s*$`),
}

func normalizeMusicBrainzAlbumTitle(name string) string {
	title := strings.TrimSpace(name)
	if title == "" {
		return ""
	}
	for {
		previous := title
		for _, pattern := range musicBrainzTitleCleanupPatterns {
			title = pattern.ReplaceAllString(title, "")
			title = strings.TrimSpace(title)
		}
		if title == previous || title == "" {
			break
		}
	}
	if title == "" {
		return strings.TrimSpace(name)
	}
	return title
}

func extractAlbumMetadataString(metadata map[string]interface{}, section, key string) string {
	sectionMap, ok := metadata[section].(map[string]interface{})
	if !ok {
		return ""
	}
	value, _ := sectionMap[key].(string)
	return strings.TrimSpace(value)
}

func extractAlbumMetadataStrings(metadata map[string]interface{}, section, key string) []string {
	sectionMap, ok := metadata[section].(map[string]interface{})
	if !ok {
		return nil
	}
	switch raw := sectionMap[key].(type) {
	case []interface{}:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			text, _ := item.(string)
			text = strings.TrimSpace(text)
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(raw))
		for _, text := range raw {
			text = strings.TrimSpace(text)
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	}
	return nil
}

func (s *Syncer) syncTracks(ctx context.Context) error {
	totalTracks, err := s.countNavTracks(ctx)
	if err != nil {
		return err
	}
	tracks, err := s.fetchNavTracks(ctx)
	if err != nil {
		return err
	}

	ids := make([]string, len(tracks))
	keys := make([]string, len(tracks))
	for i, t := range tracks {
		ids[i] = t.ID
		keys[i] = fmt.Sprintf("%s - %s", t.ArtistName, t.Title)
	}
	existing, err := s.pgClient.GetTrackIDsWithEmbedding(ctx, ids)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to check existing track embeddings")
		existing = map[string]struct{}{}
	}
	missingIdx := make([]int, 0, len(tracks))
	missingTexts := make([]string, 0, len(tracks))
	for i, t := range tracks {
		if _, ok := existing[t.ID]; ok {
			continue
		}
		missingIdx = append(missingIdx, i)
		missingTexts = append(missingTexts, keys[i])
	}
	embeddings := make([][]float32, len(missingTexts))
	if len(missingTexts) > 0 {
		embeddings, err = s.fetchEmbeddingsBatched(ctx, missingTexts, s.embeddingsBatchSize)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to fetch embeddings for missing tracks, syncing without them")
			embeddings = make([][]float32, len(missingTexts))
		}
	}
	embedByTrackIdx := make(map[int][]float32, len(missingIdx))
	for j, idx := range missingIdx {
		if j < len(embeddings) {
			embedByTrackIdx[idx] = embeddings[j]
		}
	}

	for i, t := range tracks {
		if e, ok := embedByTrackIdx[i]; ok && len(e) > 0 {
			t.Embedding = pgvector.NewVector(e)
		}
		if err := s.pgClient.UpsertTrack(ctx, t); err != nil {
			log.Error().Err(err).Str("track", t.Title).Msg("Failed to upsert track")
		}
	}

	if shouldPruneSnapshot(totalTracks, len(tracks)) {
		staleTrackIDs, err := s.pgClient.GetStaleTrackIDs(ctx, ids)
		if err != nil {
			return err
		}
		if len(staleTrackIDs) > 0 {
			deletedTracks, err := s.pgClient.DeleteTracksByIDs(ctx, staleTrackIDs)
			if err != nil {
				return err
			}
			log.Info().
				Int("stale_tracks", len(staleTrackIDs)).
				Int64("deleted_tracks", deletedTracks).
				Msg("Pruned stale tracks from postgres")
		}
	} else {
		log.Info().
			Int("navidrome_tracks", totalTracks).
			Int("synced_tracks", len(tracks)).
			Int("track_page_size", syncPageSize(s.trackLimit, 1200)).
			Msg("Skipped stale track pruning because the sync did not fetch a full track snapshot")
	}

	log.Info().
		Int("count", len(tracks)).
		Int("missing_embeddings", len(missingTexts)).
		Int("cached_embeddings", len(existing)).
		Msg("Synced tracks")
	return nil
}

func (s *Syncer) fetchEmbeddingsBatched(ctx context.Context, texts []string, batchSize int) ([][]float32, error) {
	if strings.TrimSpace(s.embeddings) == "" {
		return make([][]float32, len(texts)), nil
	}
	if batchSize <= 0 {
		batchSize = s.embeddingsBatchSize
		if batchSize <= 0 {
			batchSize = 100
		}
	}
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	out := make([][]float32, len(texts))
	failedBatches := 0
	for start := 0; start < len(texts); start += batchSize {
		end := start + batchSize
		if end > len(texts) {
			end = len(texts)
		}

		batchEmbeddings, err := s.fetchEmbeddingsWithRetry(ctx, texts[start:end])
		if err != nil {
			failedBatches++
			log.Warn().
				Err(err).
				Int("start", start).
				Int("end", end).
				Msg("Embedding batch failed; continuing without this batch")
			continue
		}
		for i := 0; i < len(batchEmbeddings) && start+i < len(out); i++ {
			out[start+i] = batchEmbeddings[i]
		}

		// Add a small pause between batches to avoid bursty CPU/network usage on small devices.
		if end < len(texts) && s.embeddingsBatchPause > 0 {
			timer := time.NewTimer(s.embeddingsBatchPause)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			}
		}
	}
	if failedBatches > 0 {
		log.Warn().Int("failed_batches", failedBatches).Msg("Completed embeddings with partial batch failures")
	}
	return out, nil
}

func shouldPruneSnapshot(total, synced int) bool {
	return total == synced
}

func syncPageSize(configured, fallback int) int {
	if configured > 0 {
		return configured
	}
	return fallback
}

func (s *Syncer) countNavAlbums(ctx context.Context) (int, error) {
	var total int
	if err := s.navDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM album`).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (s *Syncer) countNavTracks(ctx context.Context) (int, error) {
	var total int
	if err := s.navDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_file WHERE missing = FALSE`).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (s *Syncer) fetchNavAlbums(ctx context.Context) ([]Album, error) {
	pageSize := syncPageSize(s.albumLimit, 300)
	albums := make([]Album, 0, pageSize)
	for offset := 0; ; offset += pageSize {
		query := fmt.Sprintf(`SELECT a.id, a.name, a.album_artist, COALESCE(an.rating, 0), COALESCE(an.play_count, 0),
		          an.play_date, a.min_year, a.genre
		          FROM album a
		          LEFT JOIN annotation an ON a.id = an.item_id AND an.item_type = 'album'
		          ORDER BY a.created_at DESC, a.id ASC
		          LIMIT %d OFFSET %d`, pageSize, offset)

		rows, err := s.navDB.QueryContext(ctx, query)
		if err != nil {
			return nil, err
		}

		batchCount := 0
		for rows.Next() {
			var a Album
			var playDate sql.NullTime
			if err := rows.Scan(&a.ID, &a.Name, &a.ArtistName, &a.Rating, &a.PlayCount, &playDate, &a.Year, &a.Genre); err != nil {
				rows.Close()
				return nil, err
			}
			if playDate.Valid {
				a.LastPlayed = &playDate.Time
			}
			albums = append(albums, a)
			batchCount++
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		if batchCount < pageSize {
			break
		}
	}
	return albums, nil
}

func (s *Syncer) fetchNavTracks(ctx context.Context) ([]Track, error) {
	pageSize := syncPageSize(s.trackLimit, 1200)
	tracks := make([]Track, 0, pageSize)
	for offset := 0; ; offset += pageSize {
		query := fmt.Sprintf(`SELECT m.id, m.album_id, m.title, m.artist, COALESCE(an.rating, 0), COALESCE(an.play_count, 0), an.play_date
		          FROM media_file m
		          LEFT JOIN annotation an ON m.id = an.item_id AND an.item_type = 'media_file'
		          WHERE m.missing = FALSE
		          ORDER BY COALESCE(an.play_count, 0) DESC, m.created_at DESC, m.id ASC
		          LIMIT %d OFFSET %d`, pageSize, offset)

		rows, err := s.navDB.QueryContext(ctx, query)
		if err != nil {
			return nil, err
		}

		batchCount := 0
		for rows.Next() {
			var t Track
			var playDate sql.NullTime
			if err := rows.Scan(&t.ID, &t.AlbumID, &t.Title, &t.ArtistName, &t.Rating, &t.PlayCount, &playDate); err != nil {
				rows.Close()
				return nil, err
			}
			if playDate.Valid {
				t.LastPlayed = &playDate.Time
			}
			tracks = append(tracks, t)
			batchCount++
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		if batchCount < pageSize {
			break
		}
	}
	return tracks, nil
}

func (s *Syncer) fetchEmbeddingsWithRetry(ctx context.Context, texts []string) ([][]float32, error) {
	var lastErr error
	attempts := s.embeddingsRetries + 1
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		embeddings, err := s.fetchEmbeddings(ctx, texts)
		if err == nil {
			return embeddings, nil
		}
		lastErr = err
		if attempt == attempts {
			break
		}
		backoff := s.embeddingsBackoff * time.Duration(attempt)
		if backoff <= 0 {
			backoff = 100 * time.Millisecond
		}
		timer := time.NewTimer(backoff)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

func envInt(name string, defaultVal int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultVal
	}
	return n
}

func envBool(name string, defaultVal bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	case "":
		return defaultVal
	default:
		return defaultVal
	}
}

func getEnvDefault(name, defaultVal string) string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultVal
	}
	return raw
}

func (s *Syncer) syncPlayEvents(ctx context.Context) error {
	lastSubmission, err := s.pgClient.GetLastScrobbleSubmissionTime(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load last scrobble sync checkpoint, defaulting to 0")
		lastSubmission = 0
	}

	rows, err := s.navDB.QueryContext(
		ctx,
		`SELECT user_id, media_file_id, submission_time
		 FROM scrobbles
		 WHERE submission_time > ?
		 ORDER BY submission_time ASC
		 LIMIT 20000`,
		lastSubmission,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	events := make([]PlayEvent, 0, 1024)
	maxSubmission := lastSubmission
	for rows.Next() {
		var userID string
		var mediaFileID string
		var submissionTime int64
		if err := rows.Scan(&userID, &mediaFileID, &submissionTime); err != nil {
			return err
		}
		events = append(events, PlayEvent{
			UserID:         userID,
			TrackID:        mediaFileID,
			SubmissionTime: submissionTime,
			PlayedAt:       time.Unix(submissionTime, 0).UTC(),
		})
		if submissionTime > maxSubmission {
			maxSubmission = submissionTime
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if len(events) == 0 {
		return nil
	}

	if err := s.pgClient.InsertPlayEvents(ctx, events); err != nil {
		return err
	}
	if err := s.pgClient.SetLastScrobbleSubmissionTime(ctx, maxSubmission); err != nil {
		return err
	}

	log.Info().Int("count", len(events)).Int64("last_submission_time", maxSubmission).Msg("Synced play events")
	return nil
}

func (s *Syncer) fetchEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	payload := map[string]interface{}{"input": texts}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.embeddings+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result EmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	embeddings := make([][]float32, len(result.Data))
	for i, d := range result.Data {
		embeddings[i] = d.Embedding
	}
	return embeddings, nil
}
