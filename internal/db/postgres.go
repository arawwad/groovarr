package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

type Client struct {
	pool *pgxpool.Pool
}

type Album struct {
	ID                string
	Name              string
	ArtistName        string
	Rating            int
	PlayCount         int
	LastPlayed        *time.Time
	Year              *int
	Genre             *string
	Embedding         pgvector.Vector
	EmbeddingDocument string
	EmbeddingVersion  string
	Metadata          map[string]interface{}
}

type AlbumEmbeddingState struct {
	HasEmbedding bool
	Document     string
	Version      string
}

type SimilarAlbum struct {
	Album
	Similarity float64
}

type Artist struct {
	ID        string
	Name      string
	Rating    int
	PlayCount int
	Embedding pgvector.Vector
}

type ArtistLibraryStatsFilter struct {
	ArtistName       *string
	ArtistNames      []string
	Genre            *string
	ExactAlbums      *int
	MinAlbums        *int
	MaxAlbums        *int
	MinTotalPlays    *int
	MaxTotalPlays    *int
	InactiveSince    *time.Time
	PlayedSince      *time.Time
	PlayedUntil      *time.Time
	MaxPlaysInWindow *int
}

type ArtistLibraryStat struct {
	ArtistName         string
	AlbumCount         int
	TotalPlayCount     int
	LastPlayed         *time.Time
	UnplayedAlbumCount int
	PlayedInWindow     int
}

type ArtistListeningStatsFilter struct {
	ArtistName       *string
	PlayedSince      *time.Time
	PlayedUntil      *time.Time
	MinPlaysInWindow *int
	MaxPlaysInWindow *int
	MinAlbums        *int
	MaxAlbums        *int
}

type ArtistListeningStat struct {
	ArtistName     string
	AlbumCount     int
	TotalPlayCount int
	PlaysInWindow  int
	LastPlayed     *time.Time
}

func normalizedArtistKeySQL(column string) string {
	expr := fmt.Sprintf("lower(coalesce(%s, ''))", column)
	replacements := [][2]string{
		{"æ", "ae"},
		{"œ", "oe"},
		{"þ", "th"},
		{"ð", "d"},
	}
	for _, pair := range replacements {
		expr = fmt.Sprintf("replace(%s, '%s', '%s')", expr, pair[0], pair[1])
	}
	expr = fmt.Sprintf(
		"translate(%s, 'àáâãäåāăąçćčèéêëēĕėęěìíîïīĭįłñńňòóôõöøōŏőùúûüūŭůűųýÿžźż', 'aaaaaaaaaccceeeeeeeeeiiiiiiilnnnooooooooouuuuuuuuuyyzzz')",
		expr,
	)
	return fmt.Sprintf("trim(regexp_replace(%s, '[^a-z0-9]+', ' ', 'g'))", expr)
}

func normalizedSearchKeySQL(column string) string {
	return normalizedArtistKeySQL(column)
}

func normalizeSearchKey(value string) string {
	return normalizedArtistDisplayKey(value)
}

func preferredArtistDisplayName(variants string, fallback string) string {
	best := cleanArtistDisplayCandidate(fallback)
	bestScore := artistDisplayNameScore(best)
	for _, part := range strings.Split(variants, "||") {
		for _, candidate := range splitArtistDisplayCandidate(part) {
			candidate = cleanArtistDisplayCandidate(candidate)
			if candidate == "" {
				continue
			}
			score := artistDisplayNameScore(candidate)
			if score > bestScore || (score == bestScore && len(candidate) > len(best)) {
				best = candidate
				bestScore = score
			}
		}
	}
	return cleanArtistDisplayCandidate(best)
}

func artistDisplayNameScore(value string) int {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return -1
	}
	score := len(trimmed)
	if trimmed != strings.ToUpper(trimmed) {
		score += 100
	}
	if trimmed != strings.ToLower(trimmed) {
		score += 25
	}
	for _, r := range trimmed {
		if r > 127 {
			score += 200
			break
		}
	}
	return score
}

func splitArtistDisplayCandidate(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	parts := []string{trimmed}
	for _, sep := range []string{" • ", " / ", " & ", " + ", " | "} {
		next := make([]string, 0, len(parts))
		for _, part := range parts {
			split := strings.Split(part, sep)
			if len(split) == 1 {
				next = append(next, part)
				continue
			}
			for _, item := range split {
				item = strings.TrimSpace(item)
				if item != "" {
					next = append(next, item)
				}
			}
		}
		parts = next
	}
	if len(parts) <= 1 {
		return parts
	}
	baseKey := normalizedArtistDisplayKey(parts[0])
	if baseKey == "" {
		return parts
	}
	for _, part := range parts[1:] {
		if normalizedArtistDisplayKey(part) != baseKey {
			return []string{trimmed}
		}
	}
	return parts
}

func cleanArtistDisplayCandidate(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	switch normalizedArtistDisplayKey(trimmed) {
	case "va", "v a", "v a.":
		return "Various Artists"
	}
	if isAllLowercaseASCII(trimmed) {
		compact := strings.ReplaceAll(trimmed, " ", "")
		if len(compact) <= 4 && containsDigitOrAllCapsFriendly(compact) {
			return strings.ToUpper(compact)
		}
	}
	return trimmed
}

func normalizedArtistDisplayKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(
		"æ", "ae",
		"œ", "oe",
		"þ", "th",
		"ð", "d",
		"à", "a", "á", "a", "â", "a", "ã", "a", "ä", "a", "å", "a", "ā", "a", "ă", "a", "ą", "a",
		"ç", "c", "ć", "c", "č", "c",
		"è", "e", "é", "e", "ê", "e", "ë", "e", "ē", "e", "ĕ", "e", "ė", "e", "ę", "e", "ě", "e",
		"ì", "i", "í", "i", "î", "i", "ï", "i", "ī", "i", "ĭ", "i", "į", "i",
		"ł", "l",
		"ñ", "n", "ń", "n", "ň", "n",
		"ò", "o", "ó", "o", "ô", "o", "õ", "o", "ö", "o", "ø", "o", "ō", "o", "ŏ", "o", "ő", "o",
		"ù", "u", "ú", "u", "û", "u", "ü", "u", "ū", "u", "ŭ", "u", "ů", "u", "ű", "u", "ų", "u",
		"ý", "y", "ÿ", "y",
		"ž", "z", "ź", "z", "ż", "z",
		".", " ",
	)
	value = replacer.Replace(value)
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func isAllLowercaseASCII(value string) bool {
	hasLetter := false
	for _, r := range strings.TrimSpace(value) {
		if unicode.IsLetter(r) {
			hasLetter = true
			if unicode.IsUpper(r) || r > unicode.MaxASCII {
				return false
			}
		}
	}
	return hasLetter
}

func containsDigitOrAllCapsFriendly(value string) bool {
	hasDigit := false
	for _, r := range value {
		if unicode.IsDigit(r) {
			hasDigit = true
		}
		if !(unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return false
		}
	}
	return hasDigit || len(value) <= 3
}

type LibraryFacetFilter struct {
	Genre          *string
	ArtistName     *string
	Year           *int
	MinYear        *int
	MaxYear        *int
	Unplayed       *bool
	NotPlayedSince *time.Time
}

type LibraryFacetCount struct {
	Value string
	Count int
}

type AlbumRelationshipStatsFilter struct {
	ArtistExactAlbums *int
	ArtistMinAlbums   *int
	ArtistMaxAlbums   *int
	Genre             *string
	Unplayed          *bool
	NotPlayedSince    *time.Time
}

type AlbumRelationshipStat struct {
	AlbumName        string
	ArtistName       string
	Year             *int
	ArtistAlbumCount int
}

type AlbumLibraryStatsFilter struct {
	ArtistName       *string
	Genre            *string
	Year             *int
	MinYear          *int
	MaxYear          *int
	MinTotalPlays    *int
	MaxTotalPlays    *int
	MinRating        *int
	MaxRating        *int
	InactiveSince    *time.Time
	PlayedSince      *time.Time
	PlayedUntil      *time.Time
	MaxPlaysInWindow *int
	Unplayed         *bool
}

type AlbumLibraryStat struct {
	AlbumName      string
	ArtistName     string
	Year           *int
	Genre          *string
	Rating         int
	TotalPlayCount int
	LastPlayed     *time.Time
	PlayedInWindow int
}

type Track struct {
	ID         string
	AlbumID    string
	Title      string
	ArtistName string
	Rating     int
	PlayCount  int
	LastPlayed *time.Time
	Embedding  pgvector.Vector
}

type BadlyRatedTrack struct {
	TrackID string
	Title   string
	Rating  int
}

type BadlyRatedAlbum struct {
	AlbumID       string
	AlbumName     string
	ArtistName    string
	BadTrackCount int
	BadTracks     []BadlyRatedTrack
}

type SimilarTrack struct {
	Track
	Similarity float64
}

type PlayEvent struct {
	UserID         string
	TrackID        string
	SubmissionTime int64
	PlayedAt       time.Time
}

type ArtistListeningSummary struct {
	ArtistName string
	TrackCount int
}

type ListeningSummary struct {
	WindowStart  time.Time
	WindowEnd    time.Time
	TracksHeard  int
	TotalPlays   int
	ArtistsHeard int
	TopArtists   []ArtistListeningSummary
	TopTracks    []Track
}

type SyncStatus struct {
	LastSync                   time.Time
	LastScrobbleSubmissionTime int64
	LatestPlayEvent            *time.Time
	PlayEventsCount            int
}

func New(ctx context.Context, connString string) (*Client, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	client := &Client{pool: pool}
	if err := client.ensureRuntimeSchema(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ensure runtime schema: %w", err)
	}
	return client, nil
}

func (c *Client) Close() {
	c.pool.Close()
}

func (c *Client) GetAlbums(ctx context.Context, limit int, filters map[string]interface{}) ([]Album, error) {
	query := `SELECT id, name, artist_name, rating, play_count, last_played, year, genre FROM albums WHERE 1=1`
	args := []interface{}{}
	argIdx := 1

	if rating, ok := filters["rating"].(int); ok {
		query += fmt.Sprintf(" AND rating = $%d", argIdx)
		args = append(args, rating)
		argIdx++
	}
	if ratingBelow, ok := filters["ratingBelow"].(int); ok {
		query += fmt.Sprintf(" AND rating > 0 AND rating < $%d", argIdx)
		args = append(args, ratingBelow)
		argIdx++
	}
	if unplayed, ok := filters["unplayed"].(bool); ok && unplayed {
		query += " AND play_count = 0"
	}
	if genre, ok := filters["genre"].(string); ok {
		query += fmt.Sprintf(" AND genre ILIKE $%d", argIdx)
		args = append(args, "%"+genre+"%")
		argIdx++
	}
	if year, ok := filters["year"].(int); ok {
		query += fmt.Sprintf(" AND year = $%d", argIdx)
		args = append(args, year)
		argIdx++
	}
	if queryText, ok := filters["queryText"].(string); ok && strings.TrimSpace(queryText) != "" {
		like := "%" + normalizeSearchKey(queryText) + "%"
		nameKeyExpr := normalizedSearchKeySQL("name")
		artistKeyExpr := normalizedSearchKeySQL("artist_name")
		query += fmt.Sprintf(" AND (%s LIKE $%d OR %s LIKE $%d)", nameKeyExpr, argIdx, artistKeyExpr, argIdx)
		args = append(args, like)
		argIdx++
	}
	if artistNames, ok := filters["artistNames"].([]string); ok && len(artistNames) > 0 {
		clauses := make([]string, 0, len(artistNames))
		for _, artistName := range artistNames {
			trimmed := strings.TrimSpace(artistName)
			if trimmed == "" {
				continue
			}
			clauses = append(clauses, fmt.Sprintf("artist_name ILIKE $%d", argIdx))
			args = append(args, "%"+trimmed+"%")
			argIdx++
		}
		if len(clauses) > 0 {
			query += " AND (" + strings.Join(clauses, " OR ") + ")"
		}
	} else if artistName, ok := filters["artistName"].(string); ok {
		query += fmt.Sprintf(" AND artist_name ILIKE $%d", argIdx)
		args = append(args, "%"+artistName+"%")
		argIdx++
	}
	if notPlayedSince, ok := filters["notPlayedSince"].(time.Time); ok {
		// Include never-played albums and those last played before the cutoff.
		query += fmt.Sprintf(" AND (last_played IS NULL OR last_played < $%d)", argIdx)
		args = append(args, notPlayedSince)
		argIdx++
	}

	sortBy, _ := filters["sortBy"].(string)
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "rating":
		query += " ORDER BY rating DESC, play_count DESC, name ASC"
	case "recent":
		query += " ORDER BY last_played DESC NULLS LAST, play_count DESC, name ASC"
	default:
		query += " ORDER BY play_count DESC, rating DESC, name ASC"
	}
	query += fmt.Sprintf(" LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := c.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var albums []Album
	for rows.Next() {
		var a Album
		if err := rows.Scan(&a.ID, &a.Name, &a.ArtistName, &a.Rating, &a.PlayCount, &a.LastPlayed, &a.Year, &a.Genre); err != nil {
			return nil, err
		}
		albums = append(albums, a)
	}
	return albums, rows.Err()
}

func (c *Client) GetArtists(ctx context.Context, limit int, minPlayCount int, artistName *string) ([]Artist, error) {
	query := `SELECT id, name, rating, play_count FROM artists WHERE play_count >= $1`
	args := []interface{}{minPlayCount}
	argIdx := 2
	if artistName != nil && strings.TrimSpace(*artistName) != "" {
		query += fmt.Sprintf(" AND name ILIKE $%d", argIdx)
		args = append(args, strings.TrimSpace(*artistName))
		argIdx++
	}
	query += fmt.Sprintf(" ORDER BY play_count DESC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := c.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artists []Artist
	for rows.Next() {
		var a Artist
		if err := rows.Scan(&a.ID, &a.Name, &a.Rating, &a.PlayCount); err != nil {
			return nil, err
		}
		artists = append(artists, a)
	}
	return artists, rows.Err()
}

func (c *Client) GetArtistLibraryStats(ctx context.Context, limit int, filter ArtistLibraryStatsFilter, sortBy string) ([]ArtistLibraryStat, error) {
	if limit <= 0 {
		limit = 25
	}

	args := make([]interface{}, 0, 12)
	argIdx := 1
	baseWhere := []string{"artist_name IS NOT NULL", "TRIM(artist_name) <> ''"}
	if len(filter.ArtistNames) > 0 {
		clauses := make([]string, 0, len(filter.ArtistNames))
		for _, artistName := range filter.ArtistNames {
			trimmed := strings.TrimSpace(artistName)
			if trimmed == "" {
				continue
			}
			clauses = append(clauses, fmt.Sprintf("artist_name ILIKE $%d", argIdx))
			args = append(args, "%"+trimmed+"%")
			argIdx++
		}
		if len(clauses) > 0 {
			baseWhere = append(baseWhere, "("+strings.Join(clauses, " OR ")+")")
		}
	} else if filter.ArtistName != nil && strings.TrimSpace(*filter.ArtistName) != "" {
		baseWhere = append(baseWhere, fmt.Sprintf("artist_name ILIKE $%d", argIdx))
		args = append(args, "%"+strings.TrimSpace(*filter.ArtistName)+"%")
		argIdx++
	}
	if filter.Genre != nil && strings.TrimSpace(*filter.Genre) != "" {
		baseWhere = append(baseWhere, fmt.Sprintf("genre ILIKE $%d", argIdx))
		args = append(args, "%"+strings.TrimSpace(*filter.Genre)+"%")
		argIdx++
	}

	artistKeyExpr := normalizedArtistKeySQL("artist_name")
	windowArtistKeyExpr := normalizedArtistKeySQL("t.artist_name")
	query := fmt.Sprintf(`WITH artist_base AS (
		SELECT
			%[1]s AS artist_key,
			string_agg(DISTINCT artist_name, '||') AS artist_variants,
			COUNT(*) AS album_count,
			COALESCE(SUM(play_count), 0) AS total_play_count,
			MAX(last_played) AS last_played,
			SUM(CASE WHEN play_count = 0 THEN 1 ELSE 0 END) AS unplayed_album_count
		FROM albums
		WHERE %s
		GROUP BY artist_key
	)`, artistKeyExpr, strings.Join(baseWhere, " AND "))

	useWindow := filter.PlayedSince != nil || filter.PlayedUntil != nil || filter.MaxPlaysInWindow != nil
	if useWindow {
		windowWhere := make([]string, 0, 2)
		if filter.PlayedSince != nil {
			windowWhere = append(windowWhere, fmt.Sprintf("e.played_at >= $%d", argIdx))
			args = append(args, *filter.PlayedSince)
			argIdx++
		}
		if filter.PlayedUntil != nil {
			windowWhere = append(windowWhere, fmt.Sprintf("e.played_at < $%d", argIdx))
			args = append(args, *filter.PlayedUntil)
			argIdx++
		}
		if len(windowWhere) == 0 {
			windowWhere = append(windowWhere, "1=1")
		}
		query += fmt.Sprintf(`, window_plays AS (
			SELECT
				%[1]s AS artist_key,
				COUNT(*) AS played_in_window
			FROM play_events e
			JOIN tracks t ON t.id = e.track_id
			WHERE %s
			GROUP BY artist_key
		)`, windowArtistKeyExpr, strings.Join(windowWhere, " AND "))
	}

	query += `
		SELECT
			b.artist_variants,
			b.artist_key,
			b.album_count,
			b.total_play_count,
			b.last_played,
			b.unplayed_album_count,
			COALESCE(w.played_in_window, 0) AS played_in_window
		FROM artist_base b`
	if useWindow {
		query += `
		LEFT JOIN window_plays w
			ON w.artist_key = b.artist_key`
	} else {
		query += `
		LEFT JOIN (
			SELECT NULL::text AS artist_key, 0::bigint AS played_in_window
		) w ON false`
	}

	filters := make([]string, 0, 6)
	if filter.ExactAlbums != nil {
		filters = append(filters, fmt.Sprintf("b.album_count = $%d", argIdx))
		args = append(args, *filter.ExactAlbums)
		argIdx++
	}
	if filter.MinAlbums != nil {
		filters = append(filters, fmt.Sprintf("b.album_count >= $%d", argIdx))
		args = append(args, *filter.MinAlbums)
		argIdx++
	}
	if filter.MaxAlbums != nil {
		filters = append(filters, fmt.Sprintf("b.album_count <= $%d", argIdx))
		args = append(args, *filter.MaxAlbums)
		argIdx++
	}
	if filter.MinTotalPlays != nil {
		filters = append(filters, fmt.Sprintf("b.total_play_count >= $%d", argIdx))
		args = append(args, *filter.MinTotalPlays)
		argIdx++
	}
	if filter.MaxTotalPlays != nil {
		filters = append(filters, fmt.Sprintf("b.total_play_count <= $%d", argIdx))
		args = append(args, *filter.MaxTotalPlays)
		argIdx++
	}
	if filter.InactiveSince != nil {
		filters = append(filters, fmt.Sprintf("(b.last_played IS NULL OR b.last_played < $%d)", argIdx))
		args = append(args, *filter.InactiveSince)
		argIdx++
	}
	if filter.MaxPlaysInWindow != nil {
		filters = append(filters, fmt.Sprintf("COALESCE(w.played_in_window, 0) <= $%d", argIdx))
		args = append(args, *filter.MaxPlaysInWindow)
		argIdx++
	}
	if len(filters) > 0 {
		query += "\nWHERE " + strings.Join(filters, " AND ")
	}

	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "name_asc":
		query += "\nORDER BY b.artist_key ASC"
	case "last_played_desc":
		query += "\nORDER BY b.last_played DESC NULLS LAST, b.artist_key ASC"
	case "total_play_count_desc":
		query += "\nORDER BY b.total_play_count DESC, b.album_count DESC, b.artist_key ASC"
	default:
		query += "\nORDER BY b.album_count ASC, b.total_play_count DESC, b.artist_key ASC"
	}
	query += fmt.Sprintf("\nLIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := c.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make([]ArtistLibraryStat, 0, limit)
	for rows.Next() {
		var item ArtistLibraryStat
		var variants string
		var fallback string
		if err := rows.Scan(
			&variants,
			&fallback,
			&item.AlbumCount,
			&item.TotalPlayCount,
			&item.LastPlayed,
			&item.UnplayedAlbumCount,
			&item.PlayedInWindow,
		); err != nil {
			return nil, err
		}
		item.ArtistName = preferredArtistDisplayName(variants, fallback)
		stats = append(stats, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return stats, nil
}

func (c *Client) GetArtistListeningStats(ctx context.Context, limit int, filter ArtistListeningStatsFilter, sortBy string) ([]ArtistListeningStat, error) {
	if limit <= 0 {
		limit = 25
	}

	args := make([]interface{}, 0, 10)
	argIdx := 1
	catalogWhere := []string{"artist_name IS NOT NULL", "TRIM(artist_name) <> ''"}
	if filter.ArtistName != nil && strings.TrimSpace(*filter.ArtistName) != "" {
		catalogWhere = append(catalogWhere, fmt.Sprintf("artist_name ILIKE $%d", argIdx))
		args = append(args, "%"+strings.TrimSpace(*filter.ArtistName)+"%")
		argIdx++
	}

	artistKeyExpr := normalizedArtistKeySQL("artist_name")
	windowArtistKeyExpr := normalizedArtistKeySQL("t.artist_name")
	query := fmt.Sprintf(`WITH artist_catalog AS (
		SELECT
			%s AS artist_key,
			string_agg(DISTINCT artist_name, '||') AS artist_variants,
			COUNT(*) AS album_count,
			COALESCE(SUM(play_count), 0) AS total_play_count
		FROM albums
		WHERE %s
		GROUP BY artist_key
	), window_plays AS (
		SELECT
			%s AS artist_key,
			COUNT(*) AS plays_in_window,
			MAX(e.played_at) AS last_played
		FROM play_events e
		JOIN tracks t ON t.id = e.track_id
		WHERE t.artist_name IS NOT NULL
		  AND TRIM(t.artist_name) <> ''`,
		artistKeyExpr,
		strings.Join(catalogWhere, " AND "),
		windowArtistKeyExpr,
	)
	if filter.ArtistName != nil && strings.TrimSpace(*filter.ArtistName) != "" {
		query += fmt.Sprintf("\n\t\t  AND t.artist_name ILIKE $%d", argIdx)
		args = append(args, "%"+strings.TrimSpace(*filter.ArtistName)+"%")
		argIdx++
	}
	if filter.PlayedSince != nil {
		query += fmt.Sprintf("\n\t\t  AND e.played_at >= $%d", argIdx)
		args = append(args, *filter.PlayedSince)
		argIdx++
	}
	if filter.PlayedUntil != nil {
		query += fmt.Sprintf("\n\t\t  AND e.played_at < $%d", argIdx)
		args = append(args, *filter.PlayedUntil)
		argIdx++
	}
	query += `
		GROUP BY artist_key
	)
	SELECT
		c.artist_variants,
		c.artist_key,
		c.album_count,
		c.total_play_count,
		COALESCE(w.plays_in_window, 0) AS plays_in_window,
		w.last_played
	FROM artist_catalog c
	LEFT JOIN window_plays w
		ON w.artist_key = c.artist_key`

	filters := make([]string, 0, 4)
	if filter.MinPlaysInWindow != nil {
		filters = append(filters, fmt.Sprintf("COALESCE(w.plays_in_window, 0) >= $%d", argIdx))
		args = append(args, *filter.MinPlaysInWindow)
		argIdx++
	}
	if filter.MaxPlaysInWindow != nil {
		filters = append(filters, fmt.Sprintf("COALESCE(w.plays_in_window, 0) <= $%d", argIdx))
		args = append(args, *filter.MaxPlaysInWindow)
		argIdx++
	}
	if filter.MinAlbums != nil {
		filters = append(filters, fmt.Sprintf("c.album_count >= $%d", argIdx))
		args = append(args, *filter.MinAlbums)
		argIdx++
	}
	if filter.MaxAlbums != nil {
		filters = append(filters, fmt.Sprintf("c.album_count <= $%d", argIdx))
		args = append(args, *filter.MaxAlbums)
		argIdx++
	}
	if len(filters) > 0 {
		query += "\nWHERE " + strings.Join(filters, " AND ")
	}

	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "last_played_desc":
		query += "\nORDER BY w.last_played DESC NULLS LAST, c.artist_key ASC"
	case "name_asc":
		query += "\nORDER BY c.artist_key ASC"
	case "album_count_desc":
		query += "\nORDER BY c.album_count DESC, COALESCE(w.plays_in_window, 0) DESC, c.artist_key ASC"
	case "total_play_count_desc":
		query += "\nORDER BY c.total_play_count DESC, COALESCE(w.plays_in_window, 0) DESC, c.artist_key ASC"
	default:
		query += "\nORDER BY COALESCE(w.plays_in_window, 0) DESC, w.last_played DESC NULLS LAST, c.artist_key ASC"
	}
	query += fmt.Sprintf("\nLIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := c.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make([]ArtistListeningStat, 0, limit)
	for rows.Next() {
		var item ArtistListeningStat
		var variants string
		var fallback string
		if err := rows.Scan(
			&variants,
			&fallback,
			&item.AlbumCount,
			&item.TotalPlayCount,
			&item.PlaysInWindow,
			&item.LastPlayed,
		); err != nil {
			return nil, err
		}
		item.ArtistName = preferredArtistDisplayName(variants, fallback)
		stats = append(stats, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return stats, nil
}

func (c *Client) GetLibraryFacetCounts(ctx context.Context, field string, limit int, filter LibraryFacetFilter) ([]LibraryFacetCount, error) {
	if limit <= 0 {
		limit = 10
	}

	if strings.EqualFold(strings.TrimSpace(field), "genre") {
		return c.getGenreFacetCounts(ctx, limit, filter)
	}

	var selectExpr string
	var valueWhere []string
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "year":
		selectExpr = "CAST(year AS TEXT)"
		valueWhere = []string{"year IS NOT NULL", "year > 0"}
	case "decade":
		selectExpr = "CAST((year / 10) * 10 AS TEXT) || 's'"
		valueWhere = []string{"year IS NOT NULL", "year > 0"}
	case "artist_name":
		selectExpr = "artist_name"
		valueWhere = []string{"artist_name IS NOT NULL", "TRIM(artist_name) <> ''"}
	default:
		return nil, fmt.Errorf("unsupported library facet field: %s", field)
	}

	args := make([]interface{}, 0, 8)
	argIdx := 1
	where := make([]string, 0, 10)
	where = append(where, valueWhere...)
	if filter.Genre != nil && strings.TrimSpace(*filter.Genre) != "" {
		where = append(where, fmt.Sprintf("genre ILIKE $%d", argIdx))
		args = append(args, "%"+strings.TrimSpace(*filter.Genre)+"%")
		argIdx++
	}
	if filter.ArtistName != nil && strings.TrimSpace(*filter.ArtistName) != "" {
		where = append(where, fmt.Sprintf("artist_name ILIKE $%d", argIdx))
		args = append(args, "%"+strings.TrimSpace(*filter.ArtistName)+"%")
		argIdx++
	}
	if filter.Year != nil {
		where = append(where, fmt.Sprintf("year = $%d", argIdx))
		args = append(args, *filter.Year)
		argIdx++
	}
	if filter.MinYear != nil {
		where = append(where, fmt.Sprintf("year >= $%d", argIdx))
		args = append(args, *filter.MinYear)
		argIdx++
	}
	if filter.MaxYear != nil {
		where = append(where, fmt.Sprintf("year <= $%d", argIdx))
		args = append(args, *filter.MaxYear)
		argIdx++
	}
	if filter.Unplayed != nil {
		if *filter.Unplayed {
			where = append(where, "play_count = 0")
		} else {
			where = append(where, "play_count > 0")
		}
	}
	if filter.NotPlayedSince != nil {
		where = append(where, fmt.Sprintf("(last_played IS NULL OR last_played < $%d)", argIdx))
		args = append(args, *filter.NotPlayedSince)
		argIdx++
	}

	query := fmt.Sprintf(`
		SELECT %s AS value, COUNT(*) AS count
		FROM albums
		WHERE %s
		GROUP BY value
		ORDER BY count DESC, value ASC
		LIMIT $%d
	`, selectExpr, strings.Join(where, " AND "), argIdx)
	args = append(args, limit)

	rows, err := c.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]LibraryFacetCount, 0, limit)
	for rows.Next() {
		var item LibraryFacetCount
		if err := rows.Scan(&item.Value, &item.Count); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) getGenreFacetCounts(ctx context.Context, limit int, filter LibraryFacetFilter) ([]LibraryFacetCount, error) {
	args := make([]interface{}, 0, 8)
	argIdx := 1
	where := []string{"genre IS NOT NULL", "TRIM(genre) <> ''"}
	if filter.Genre != nil && strings.TrimSpace(*filter.Genre) != "" {
		where = append(where, fmt.Sprintf("genre ILIKE $%d", argIdx))
		args = append(args, "%"+strings.TrimSpace(*filter.Genre)+"%")
		argIdx++
	}
	if filter.ArtistName != nil && strings.TrimSpace(*filter.ArtistName) != "" {
		where = append(where, fmt.Sprintf("artist_name ILIKE $%d", argIdx))
		args = append(args, "%"+strings.TrimSpace(*filter.ArtistName)+"%")
		argIdx++
	}
	if filter.Year != nil {
		where = append(where, fmt.Sprintf("year = $%d", argIdx))
		args = append(args, *filter.Year)
		argIdx++
	}
	if filter.MinYear != nil {
		where = append(where, fmt.Sprintf("year >= $%d", argIdx))
		args = append(args, *filter.MinYear)
		argIdx++
	}
	if filter.MaxYear != nil {
		where = append(where, fmt.Sprintf("year <= $%d", argIdx))
		args = append(args, *filter.MaxYear)
		argIdx++
	}
	if filter.Unplayed != nil {
		if *filter.Unplayed {
			where = append(where, "play_count = 0")
		} else {
			where = append(where, "play_count > 0")
		}
	}
	if filter.NotPlayedSince != nil {
		where = append(where, fmt.Sprintf("(last_played IS NULL OR last_played < $%d)", argIdx))
		args = append(args, *filter.NotPlayedSince)
		argIdx++
	}

	query := fmt.Sprintf(`SELECT genre FROM albums WHERE %s`, strings.Join(where, " AND "))
	rows, err := c.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rawGenres := make([]string, 0, 256)
	for rows.Next() {
		var genre sql.NullString
		if err := rows.Scan(&genre); err != nil {
			return nil, err
		}
		if genre.Valid {
			rawGenres = append(rawGenres, genre.String)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return aggregateGenreFacetCounts(rawGenres, limit), nil
}

func aggregateGenreFacetCounts(rawGenres []string, limit int) []LibraryFacetCount {
	if limit <= 0 {
		limit = 10
	}
	counts := make(map[string]int, len(rawGenres))
	for _, raw := range rawGenres {
		seen := map[string]struct{}{}
		for _, genre := range splitAlbumEmbeddingGenre(&raw) {
			key := strings.ToLower(strings.TrimSpace(genre))
			if key == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			counts[key]++
		}
	}

	out := make([]LibraryFacetCount, 0, len(counts))
	for value, count := range counts {
		out = append(out, LibraryFacetCount{Value: value, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Value < out[j].Value
		}
		return out[i].Count > out[j].Count
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (c *Client) GetAlbumRelationshipStats(ctx context.Context, limit int, filter AlbumRelationshipStatsFilter, sortBy string) ([]AlbumRelationshipStat, error) {
	if limit <= 0 {
		limit = 25
	}

	args := make([]interface{}, 0, 8)
	argIdx := 1
	artistFilters := make([]string, 0, 3)
	artistKeyExpr := normalizedArtistKeySQL("artist_name")
	if filter.ArtistExactAlbums != nil {
		artistFilters = append(artistFilters, fmt.Sprintf("COUNT(*) = $%d", argIdx))
		args = append(args, *filter.ArtistExactAlbums)
		argIdx++
	}
	if filter.ArtistMinAlbums != nil {
		artistFilters = append(artistFilters, fmt.Sprintf("COUNT(*) >= $%d", argIdx))
		args = append(args, *filter.ArtistMinAlbums)
		argIdx++
	}
	if filter.ArtistMaxAlbums != nil {
		artistFilters = append(artistFilters, fmt.Sprintf("COUNT(*) <= $%d", argIdx))
		args = append(args, *filter.ArtistMaxAlbums)
		argIdx++
	}
	havingClause := ""
	if len(artistFilters) > 0 {
		havingClause = "HAVING " + strings.Join(artistFilters, " AND ")
	}

	albumWhere := []string{"a.name IS NOT NULL", "TRIM(a.name) <> ''", "a.artist_name IS NOT NULL", "TRIM(a.artist_name) <> ''"}
	if filter.Genre != nil && strings.TrimSpace(*filter.Genre) != "" {
		albumWhere = append(albumWhere, fmt.Sprintf("a.genre ILIKE $%d", argIdx))
		args = append(args, "%"+strings.TrimSpace(*filter.Genre)+"%")
		argIdx++
	}
	if filter.Unplayed != nil {
		if *filter.Unplayed {
			albumWhere = append(albumWhere, "a.play_count = 0")
		} else {
			albumWhere = append(albumWhere, "a.play_count > 0")
		}
	}
	if filter.NotPlayedSince != nil {
		albumWhere = append(albumWhere, fmt.Sprintf("(a.last_played IS NULL OR a.last_played < $%d)", argIdx))
		args = append(args, *filter.NotPlayedSince)
		argIdx++
	}

	albumArtistKeyExpr := normalizedArtistKeySQL("a.artist_name")
	query := fmt.Sprintf(`
		WITH artist_counts AS (
			SELECT %s AS artist_key, string_agg(DISTINCT artist_name, '||') AS artist_variants, COUNT(*) AS artist_album_count
			FROM albums
			WHERE artist_name IS NOT NULL AND TRIM(artist_name) <> ''
			GROUP BY artist_key
			%s
		)
		SELECT
			a.name,
			ac.artist_variants,
			ac.artist_key,
			a.year,
			ac.artist_album_count
		FROM albums a
		JOIN artist_counts ac ON ac.artist_key = %s
		WHERE %s
	`, artistKeyExpr, havingClause, albumArtistKeyExpr, strings.Join(albumWhere, " AND "))

	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "year_asc":
		query += "\nORDER BY a.year ASC NULLS LAST, a.name ASC"
	default:
		query += "\nORDER BY a.name ASC, a.artist_name ASC"
	}
	query += fmt.Sprintf("\nLIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := c.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AlbumRelationshipStat, 0, limit)
	for rows.Next() {
		var item AlbumRelationshipStat
		var artistVariants string
		var artistFallback string
		if err := rows.Scan(&item.AlbumName, &artistVariants, &artistFallback, &item.Year, &item.ArtistAlbumCount); err != nil {
			return nil, err
		}
		item.ArtistName = preferredArtistDisplayName(artistVariants, artistFallback)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetAlbumLibraryStats(ctx context.Context, limit int, filter AlbumLibraryStatsFilter, sortBy string) ([]AlbumLibraryStat, error) {
	if limit <= 0 {
		limit = 25
	}

	args := make([]interface{}, 0, 16)
	argIdx := 1
	baseWhere := []string{"name IS NOT NULL", "TRIM(name) <> ''", "artist_name IS NOT NULL", "TRIM(artist_name) <> ''"}
	if filter.ArtistName != nil && strings.TrimSpace(*filter.ArtistName) != "" {
		baseWhere = append(baseWhere, fmt.Sprintf("artist_name ILIKE $%d", argIdx))
		args = append(args, "%"+strings.TrimSpace(*filter.ArtistName)+"%")
		argIdx++
	}
	if filter.Genre != nil && strings.TrimSpace(*filter.Genre) != "" {
		baseWhere = append(baseWhere, fmt.Sprintf("genre ILIKE $%d", argIdx))
		args = append(args, "%"+strings.TrimSpace(*filter.Genre)+"%")
		argIdx++
	}
	if filter.Year != nil {
		baseWhere = append(baseWhere, fmt.Sprintf("year = $%d", argIdx))
		args = append(args, *filter.Year)
		argIdx++
	}
	if filter.MinYear != nil {
		baseWhere = append(baseWhere, fmt.Sprintf("year >= $%d", argIdx))
		args = append(args, *filter.MinYear)
		argIdx++
	}
	if filter.MaxYear != nil {
		baseWhere = append(baseWhere, fmt.Sprintf("year <= $%d", argIdx))
		args = append(args, *filter.MaxYear)
		argIdx++
	}
	if filter.MinRating != nil {
		baseWhere = append(baseWhere, fmt.Sprintf("rating >= $%d", argIdx))
		args = append(args, *filter.MinRating)
		argIdx++
	}
	if filter.MaxRating != nil {
		baseWhere = append(baseWhere, fmt.Sprintf("rating <= $%d", argIdx))
		args = append(args, *filter.MaxRating)
		argIdx++
	}
	if filter.Unplayed != nil {
		if *filter.Unplayed {
			baseWhere = append(baseWhere, "play_count = 0")
		} else {
			baseWhere = append(baseWhere, "play_count > 0")
		}
	}

	query := fmt.Sprintf(`WITH album_base AS (
		SELECT
			id,
			name,
			artist_name,
			year,
			genre,
			rating,
			play_count AS total_play_count,
			last_played
		FROM albums
		WHERE %s
	)`, strings.Join(baseWhere, " AND "))

	useWindow := filter.PlayedSince != nil || filter.PlayedUntil != nil || filter.MaxPlaysInWindow != nil
	if useWindow {
		windowWhere := make([]string, 0, 2)
		if filter.PlayedSince != nil {
			windowWhere = append(windowWhere, fmt.Sprintf("e.played_at >= $%d", argIdx))
			args = append(args, *filter.PlayedSince)
			argIdx++
		}
		if filter.PlayedUntil != nil {
			windowWhere = append(windowWhere, fmt.Sprintf("e.played_at < $%d", argIdx))
			args = append(args, *filter.PlayedUntil)
			argIdx++
		}
		if len(windowWhere) == 0 {
			windowWhere = append(windowWhere, "1=1")
		}
		query += fmt.Sprintf(`, window_plays AS (
			SELECT
				t.album_id,
				COUNT(*) AS played_in_window
			FROM play_events e
			JOIN tracks t ON t.id = e.track_id
			WHERE %s
			GROUP BY t.album_id
		)`, strings.Join(windowWhere, " AND "))
	}

	query += `
		SELECT
			b.name,
			b.artist_name,
			b.year,
			b.genre,
			b.rating,
			b.total_play_count,
			b.last_played,
			COALESCE(w.played_in_window, 0) AS played_in_window
		FROM album_base b`
	if useWindow {
		query += `
		LEFT JOIN window_plays w
			ON w.album_id = b.id`
	} else {
		query += `
		LEFT JOIN (
			SELECT NULL::text AS album_id, 0::bigint AS played_in_window
		) w ON false`
	}

	filters := make([]string, 0, 4)
	if filter.MinTotalPlays != nil {
		filters = append(filters, fmt.Sprintf("b.total_play_count >= $%d", argIdx))
		args = append(args, *filter.MinTotalPlays)
		argIdx++
	}
	if filter.MaxTotalPlays != nil {
		filters = append(filters, fmt.Sprintf("b.total_play_count <= $%d", argIdx))
		args = append(args, *filter.MaxTotalPlays)
		argIdx++
	}
	if filter.InactiveSince != nil {
		filters = append(filters, fmt.Sprintf("(b.last_played IS NULL OR b.last_played < $%d)", argIdx))
		args = append(args, *filter.InactiveSince)
		argIdx++
	}
	if filter.MaxPlaysInWindow != nil {
		filters = append(filters, fmt.Sprintf("COALESCE(w.played_in_window, 0) <= $%d", argIdx))
		args = append(args, *filter.MaxPlaysInWindow)
		argIdx++
	}
	if len(filters) > 0 {
		query += "\nWHERE " + strings.Join(filters, " AND ")
	}

	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "last_played_asc":
		query += "\nORDER BY b.last_played ASC NULLS FIRST, b.name ASC"
	case "total_play_count_desc":
		query += "\nORDER BY b.total_play_count DESC, b.last_played DESC NULLS LAST, b.name ASC"
	case "rating_desc":
		query += "\nORDER BY b.rating DESC, b.total_play_count DESC, b.name ASC"
	case "year_asc":
		query += "\nORDER BY b.year ASC NULLS LAST, b.name ASC"
	case "name_asc":
		query += "\nORDER BY b.name ASC, b.artist_name ASC"
	default:
		query += "\nORDER BY b.last_played DESC NULLS LAST, b.total_play_count DESC, b.name ASC"
	}
	query += fmt.Sprintf("\nLIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := c.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make([]AlbumLibraryStat, 0, limit)
	for rows.Next() {
		var item AlbumLibraryStat
		if err := rows.Scan(
			&item.AlbumName,
			&item.ArtistName,
			&item.Year,
			&item.Genre,
			&item.Rating,
			&item.TotalPlayCount,
			&item.LastPlayed,
			&item.PlayedInWindow,
		); err != nil {
			return nil, err
		}
		stats = append(stats, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return stats, nil
}

func (c *Client) GetArtistByName(ctx context.Context, name string) (*Artist, error) {
	query := `SELECT id, name, COALESCE(rating, 0), COALESCE(play_count, 0), COALESCE(embedding, '[0,0,0]'::vector) FROM artists WHERE name ILIKE $1 LIMIT 1`
	row := c.pool.QueryRow(ctx, query, name)

	var a Artist
	err := row.Scan(&a.ID, &a.Name, &a.Rating, &a.PlayCount, &a.Embedding)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

func (c *Client) GetAlbumByName(ctx context.Context, name string) (*Album, error) {
	query := `SELECT id, name, artist_name, rating, play_count, last_played, year, genre, embedding FROM albums WHERE name ILIKE $1 LIMIT 1`
	row := c.pool.QueryRow(ctx, query, name)

	var a Album
	err := row.Scan(&a.ID, &a.Name, &a.ArtistName, &a.Rating, &a.PlayCount, &a.LastPlayed, &a.Year, &a.Genre, &a.Embedding)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

func (c *Client) GetTrackByID(ctx context.Context, id string) (*Track, error) {
	query := `SELECT id, album_id, title, artist_name, rating, play_count, last_played, embedding
		FROM tracks
		WHERE id = $1
		LIMIT 1`
	row := c.pool.QueryRow(ctx, query, id)

	var t Track
	err := row.Scan(&t.ID, &t.AlbumID, &t.Title, &t.ArtistName, &t.Rating, &t.PlayCount, &t.LastPlayed, &t.Embedding)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

func (c *Client) GetTrackByArtistTitle(ctx context.Context, artistName, title string) (*Track, error) {
	query := `SELECT id, album_id, title, artist_name, rating, play_count, last_played, embedding
		FROM tracks
		WHERE artist_name ILIKE $1 AND title ILIKE $2
		ORDER BY play_count DESC, last_played DESC NULLS LAST
		LIMIT 1`
	row := c.pool.QueryRow(ctx, query, artistName, title)

	var t Track
	err := row.Scan(&t.ID, &t.AlbumID, &t.Title, &t.ArtistName, &t.Rating, &t.PlayCount, &t.LastPlayed, &t.Embedding)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

func (c *Client) FindSimilarArtists(ctx context.Context, embedding pgvector.Vector, limit int) ([]Artist, error) {
	query := `SELECT id, name, COALESCE(rating, 0), COALESCE(play_count, 0), 1 - (embedding <=> $1) AS similarity 
	          FROM artists WHERE embedding IS NOT NULL 
	          ORDER BY embedding <=> $1 LIMIT $2`
	rows, err := c.pool.Query(ctx, query, embedding, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artists []Artist
	for rows.Next() {
		var a Artist
		var similarity float64
		if err := rows.Scan(&a.ID, &a.Name, &a.Rating, &a.PlayCount, &similarity); err != nil {
			return nil, err
		}
		artists = append(artists, a)
	}
	return artists, rows.Err()
}

func (c *Client) FindSimilarAlbums(ctx context.Context, embedding pgvector.Vector, limit int) ([]Album, error) {
	query := `SELECT id, name, artist_name, rating, play_count, last_played, year, genre, 1 - (embedding <=> $1) AS similarity 
	          FROM albums WHERE embedding IS NOT NULL 
	          ORDER BY embedding <=> $1 LIMIT $2`
	rows, err := c.pool.Query(ctx, query, embedding, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var albums []Album
	for rows.Next() {
		var a Album
		var similarity float64
		if err := rows.Scan(&a.ID, &a.Name, &a.ArtistName, &a.Rating, &a.PlayCount, &a.LastPlayed, &a.Year, &a.Genre, &similarity); err != nil {
			return nil, err
		}
		albums = append(albums, a)
	}
	return albums, rows.Err()
}

func (c *Client) FindSimilarAlbumsByEmbedding(ctx context.Context, embedding pgvector.Vector, limit int, artistName, genre *string, minYear, maxYear *int) ([]SimilarAlbum, error) {
	if limit <= 0 {
		limit = 10
	}

	query := `SELECT id, name, artist_name, rating, play_count, last_played, year, genre, metadata,
	                 1 - (embedding <=> $1) AS similarity
	          FROM albums
	          WHERE embedding IS NOT NULL`
	args := []interface{}{embedding}
	argIdx := 2

	if artistName != nil && strings.TrimSpace(*artistName) != "" {
		query += fmt.Sprintf(" AND artist_name ILIKE $%d", argIdx)
		args = append(args, "%"+strings.TrimSpace(*artistName)+"%")
		argIdx++
	}
	if genre != nil && strings.TrimSpace(*genre) != "" {
		query += fmt.Sprintf(" AND genre ILIKE $%d", argIdx)
		args = append(args, "%"+strings.TrimSpace(*genre)+"%")
		argIdx++
	}
	if minYear != nil {
		query += fmt.Sprintf(" AND year >= $%d", argIdx)
		args = append(args, *minYear)
		argIdx++
	}
	if maxYear != nil {
		query += fmt.Sprintf(" AND year <= $%d", argIdx)
		args = append(args, *maxYear)
		argIdx++
	}

	query += fmt.Sprintf(" ORDER BY embedding <=> $1 LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := c.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]SimilarAlbum, 0, limit)
	for rows.Next() {
		var item SimilarAlbum
		var metadataBytes []byte
		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&item.ArtistName,
			&item.Rating,
			&item.PlayCount,
			&item.LastPlayed,
			&item.Year,
			&item.Genre,
			&metadataBytes,
			&item.Similarity,
		); err != nil {
			return nil, err
		}
		if len(metadataBytes) > 0 {
			if err := json.Unmarshal(metadataBytes, &item.Metadata); err != nil {
				return nil, err
			}
		}
		results = append(results, item)
	}
	return results, rows.Err()
}

func (c *Client) UpsertAlbum(ctx context.Context, album Album) error {
	query := `INSERT INTO albums (id, name, artist_name, rating, play_count, last_played, year, genre, embedding, embedding_document, embedding_version, metadata)
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	          ON CONFLICT (id) DO UPDATE SET 
	          name = EXCLUDED.name,
	          artist_name = EXCLUDED.artist_name,
	          rating = EXCLUDED.rating,
	          play_count = EXCLUDED.play_count,
	          last_played = EXCLUDED.last_played,
	          year = EXCLUDED.year,
	          genre = EXCLUDED.genre,
	          embedding = COALESCE(EXCLUDED.embedding, albums.embedding),
	          embedding_document = EXCLUDED.embedding_document,
	          embedding_version = EXCLUDED.embedding_version,
	          metadata = COALESCE(EXCLUDED.metadata, albums.metadata),
	          updated_at = NOW()`
	_, err := c.pool.Exec(
		ctx,
		query,
		album.ID,
		album.Name,
		album.ArtistName,
		album.Rating,
		album.PlayCount,
		album.LastPlayed,
		album.Year,
		album.Genre,
		vectorArg(album.Embedding),
		album.EmbeddingDocument,
		album.EmbeddingVersion,
		jsonbArg(album.Metadata),
	)
	return err
}

func (c *Client) DeleteAlbum(ctx context.Context, id string) error {
	_, err := c.pool.Exec(ctx, "DELETE FROM albums WHERE id = $1", id)
	return err
}

func (c *Client) DeleteAlbumsByIDs(ctx context.Context, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := c.pool.Exec(ctx, "DELETE FROM albums WHERE id = ANY($1::text[])", ids)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (c *Client) UpsertArtist(ctx context.Context, artist Artist) error {
	query := `INSERT INTO artists (id, name, rating, play_count, embedding)
	          VALUES ($1, $2, $3, $4, $5)
	          ON CONFLICT (id) DO UPDATE SET 
	          name = EXCLUDED.name, rating = EXCLUDED.rating, play_count = EXCLUDED.play_count,
	          embedding = COALESCE(EXCLUDED.embedding, artists.embedding), updated_at = NOW()`
	_, err := c.pool.Exec(ctx, query, artist.ID, artist.Name, artist.Rating, artist.PlayCount, vectorArg(artist.Embedding))
	return err
}

func (c *Client) DeleteArtist(ctx context.Context, id string) error {
	_, err := c.pool.Exec(ctx, "DELETE FROM artists WHERE id = $1", id)
	return err
}

func (c *Client) GetTracks(ctx context.Context, limit int, mostPlayed bool, filters map[string]interface{}) ([]Track, error) {
	query := `SELECT id, album_id, title, artist_name, rating, play_count, last_played FROM tracks WHERE 1=1`
	args := []interface{}{}
	argIdx := 1

	if artistName, ok := filters["artistName"].(string); ok && strings.TrimSpace(artistName) != "" {
		query += fmt.Sprintf(" AND artist_name ILIKE $%d", argIdx)
		args = append(args, artistName)
		argIdx++
	}
	if queryText, ok := filters["queryText"].(string); ok && strings.TrimSpace(queryText) != "" {
		like := "%" + normalizeSearchKey(queryText) + "%"
		titleKeyExpr := normalizedSearchKeySQL("title")
		artistKeyExpr := normalizedSearchKeySQL("artist_name")
		query += fmt.Sprintf(" AND (%s LIKE $%d OR %s LIKE $%d)", titleKeyExpr, argIdx, artistKeyExpr, argIdx)
		args = append(args, like)
		argIdx++
	}

	playedSince, hasPlayedSince := filters["playedSince"].(time.Time)
	playedUntil, hasPlayedUntil := filters["playedUntil"].(time.Time)
	onlyPlayed, hasOnlyPlayed := filters["onlyPlayed"].(bool)
	if !hasOnlyPlayed {
		onlyPlayed = false
	}

	if hasPlayedSince || hasPlayedUntil {
		// Time window queries should only return actually listened tracks.
		onlyPlayed = true
	}
	if onlyPlayed {
		query += " AND play_count > 0 AND last_played IS NOT NULL"
	}
	if hasPlayedSince {
		query += fmt.Sprintf(" AND last_played >= $%d", argIdx)
		args = append(args, playedSince)
		argIdx++
	}
	if hasPlayedUntil {
		query += fmt.Sprintf(" AND last_played < $%d", argIdx)
		args = append(args, playedUntil)
		argIdx++
	}

	if hasPlayedSince || hasPlayedUntil {
		query += " ORDER BY last_played DESC NULLS LAST, play_count DESC"
	} else {
		sortBy, _ := filters["sortBy"].(string)
		switch strings.ToLower(strings.TrimSpace(sortBy)) {
		case "artistname":
			query += " ORDER BY artist_name ASC, title ASC"
		case "title":
			query += " ORDER BY title ASC, artist_name ASC"
		case "lastplayed":
			query += " ORDER BY last_played DESC NULLS LAST, play_count DESC"
		default:
			order := "DESC"
			if !mostPlayed {
				order = "ASC"
			}
			query += fmt.Sprintf(" ORDER BY play_count %s, last_played DESC NULLS LAST", order)
		}
	}

	query += fmt.Sprintf(" LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := c.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []Track
	for rows.Next() {
		var t Track
		if err := rows.Scan(&t.ID, &t.AlbumID, &t.Title, &t.ArtistName, &t.Rating, &t.PlayCount, &t.LastPlayed); err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

func (c *Client) UpsertTrack(ctx context.Context, track Track) error {
	query := `INSERT INTO tracks (id, album_id, title, artist_name, rating, play_count, last_played, embedding)
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	          ON CONFLICT (id) DO UPDATE SET 
	          album_id = EXCLUDED.album_id, title = EXCLUDED.title, artist_name = EXCLUDED.artist_name,
	          rating = EXCLUDED.rating, play_count = EXCLUDED.play_count, last_played = EXCLUDED.last_played,
	          embedding = COALESCE(EXCLUDED.embedding, tracks.embedding)`
	_, err := c.pool.Exec(ctx, query, track.ID, track.AlbumID, track.Title, track.ArtistName, track.Rating, track.PlayCount, track.LastPlayed, vectorArg(track.Embedding))
	return err
}

func (c *Client) GetAlbumsWithBadlyRatedTracks(ctx context.Context, limit int, maxTrackDetails int) ([]BadlyRatedAlbum, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if maxTrackDetails <= 0 {
		maxTrackDetails = 3
	}
	if maxTrackDetails > 10 {
		maxTrackDetails = 10
	}

	groupRows, err := c.pool.Query(ctx, `
		SELECT t.album_id, a.name, a.artist_name, COUNT(*)::int AS bad_track_count
		FROM tracks t
		JOIN albums a ON a.id = t.album_id
		WHERE t.rating IN (1, 2)
		GROUP BY t.album_id, a.name, a.artist_name
		ORDER BY bad_track_count DESC, a.name ASC, a.artist_name ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer groupRows.Close()

	albums := make([]BadlyRatedAlbum, 0, limit)
	albumIndex := make(map[string]int, limit)
	albumIDs := make([]string, 0, limit)
	for groupRows.Next() {
		var item BadlyRatedAlbum
		if err := groupRows.Scan(&item.AlbumID, &item.AlbumName, &item.ArtistName, &item.BadTrackCount); err != nil {
			return nil, err
		}
		albumIndex[item.AlbumID] = len(albums)
		albumIDs = append(albumIDs, item.AlbumID)
		albums = append(albums, item)
	}
	if err := groupRows.Err(); err != nil {
		return nil, err
	}
	if len(albumIDs) == 0 {
		return nil, nil
	}

	trackRows, err := c.pool.Query(ctx, `
		SELECT ranked.track_id, ranked.album_id, ranked.title, ranked.rating
		FROM (
			SELECT
				t.id AS track_id,
				t.album_id,
				t.title,
				t.rating,
				ROW_NUMBER() OVER (PARTITION BY t.album_id ORDER BY t.rating ASC, t.title ASC, t.id ASC) AS rn
			FROM tracks t
			WHERE t.rating IN (1, 2) AND t.album_id = ANY($1)
		) ranked
		WHERE ranked.rn <= $2
		ORDER BY ranked.album_id ASC, ranked.rating ASC, ranked.title ASC, ranked.track_id ASC
	`, albumIDs, maxTrackDetails)
	if err != nil {
		return nil, err
	}
	defer trackRows.Close()

	for trackRows.Next() {
		var track BadlyRatedTrack
		var albumID string
		if err := trackRows.Scan(&track.TrackID, &albumID, &track.Title, &track.Rating); err != nil {
			return nil, err
		}
		if idx, ok := albumIndex[albumID]; ok {
			albums[idx].BadTracks = append(albums[idx].BadTracks, track)
		}
	}
	if err := trackRows.Err(); err != nil {
		return nil, err
	}

	return albums, nil
}

func (c *Client) DeleteTracksByIDs(ctx context.Context, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := c.pool.Exec(ctx, "DELETE FROM tracks WHERE id = ANY($1::text[])", ids)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (c *Client) DeleteTracksByAlbumIDs(ctx context.Context, albumIDs []string) (int64, error) {
	if len(albumIDs) == 0 {
		return 0, nil
	}
	tag, err := c.pool.Exec(ctx, "DELETE FROM tracks WHERE album_id = ANY($1::text[])", albumIDs)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (c *Client) GetStaleAlbumIDs(ctx context.Context, currentIDs []string) ([]string, error) {
	return c.getStaleIDs(ctx, "albums", currentIDs)
}

func (c *Client) GetStaleTrackIDs(ctx context.Context, currentIDs []string) ([]string, error) {
	return c.getStaleIDs(ctx, "tracks", currentIDs)
}

func (c *Client) GetArtistIDsWithEmbedding(ctx context.Context, ids []string) (map[string]struct{}, error) {
	return c.getIDsWithEmbedding(ctx, "artists", ids)
}

func (c *Client) GetAlbumIDsWithEmbedding(ctx context.Context, ids []string) (map[string]struct{}, error) {
	return c.getIDsWithEmbedding(ctx, "albums", ids)
}

func (c *Client) GetAlbumEmbeddingStates(ctx context.Context, ids []string) (map[string]AlbumEmbeddingState, error) {
	out := make(map[string]AlbumEmbeddingState)
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := c.pool.Query(ctx, `
		SELECT id, embedding IS NOT NULL, COALESCE(embedding_document, ''), COALESCE(embedding_version, '')
		FROM albums
		WHERE id = ANY($1::text[])
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id           string
			hasEmbedding bool
			document     string
			version      string
		)
		if err := rows.Scan(&id, &hasEmbedding, &document, &version); err != nil {
			return nil, err
		}
		out[id] = AlbumEmbeddingState{
			HasEmbedding: hasEmbedding,
			Document:     document,
			Version:      version,
		}
	}
	return out, rows.Err()
}

func (c *Client) GetAlbumMetadata(ctx context.Context, ids []string) (map[string]map[string]interface{}, error) {
	out := make(map[string]map[string]interface{})
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := c.pool.Query(ctx, `
		SELECT id, metadata
		FROM albums
		WHERE id = ANY($1::text[])
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id      string
			rawJSON []byte
		)
		if err := rows.Scan(&id, &rawJSON); err != nil {
			return nil, err
		}
		meta := make(map[string]interface{})
		if len(rawJSON) > 0 {
			if err := json.Unmarshal(rawJSON, &meta); err != nil {
				return nil, err
			}
		}
		out[id] = meta
	}
	return out, rows.Err()
}

func (c *Client) GetTrackIDsWithEmbedding(ctx context.Context, ids []string) (map[string]struct{}, error) {
	return c.getIDsWithEmbedding(ctx, "tracks", ids)
}

func (c *Client) getIDsWithEmbedding(ctx context.Context, table string, ids []string) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	if len(ids) == 0 {
		return out, nil
	}
	query := fmt.Sprintf("SELECT id FROM %s WHERE id = ANY($1::text[]) AND embedding IS NOT NULL", table)
	rows, err := c.pool.Query(ctx, query, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = struct{}{}
	}
	return out, rows.Err()
}

func (c *Client) getStaleIDs(ctx context.Context, table string, currentIDs []string) ([]string, error) {
	switch table {
	case "albums", "tracks":
	default:
		return nil, fmt.Errorf("unsupported table for stale id lookup: %s", table)
	}

	rows, err := c.pool.Query(ctx, fmt.Sprintf("SELECT id FROM %s WHERE NOT (id = ANY($1::text[]))", table), currentIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (c *Client) FindSimilarTracksByEmbedding(ctx context.Context, embedding pgvector.Vector, limit int, start, end *time.Time) ([]SimilarTrack, error) {
	if limit <= 0 {
		limit = 10
	}

	query := `SELECT t.id, t.album_id, t.title, t.artist_name, t.rating, t.play_count, t.last_played,
	                 1 - (t.embedding <=> $1) AS similarity
	          FROM tracks t
	          WHERE t.embedding IS NOT NULL`
	args := []interface{}{embedding}
	argIdx := 2

	if start != nil && end != nil {
		query += fmt.Sprintf(` AND t.id IN (
			SELECT DISTINCT e.track_id
			FROM play_events e
			WHERE e.played_at >= $%d AND e.played_at < $%d
		)`, argIdx, argIdx+1)
		args = append(args, *start, *end)
		argIdx += 2
	}

	query += fmt.Sprintf(` ORDER BY t.embedding <=> $1 LIMIT $%d`, argIdx)
	args = append(args, limit)

	rows, err := c.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]SimilarTrack, 0, limit)
	for rows.Next() {
		var st SimilarTrack
		if err := rows.Scan(
			&st.ID,
			&st.AlbumID,
			&st.Title,
			&st.ArtistName,
			&st.Rating,
			&st.PlayCount,
			&st.LastPlayed,
			&st.Similarity,
		); err != nil {
			return nil, err
		}
		results = append(results, st)
	}
	return results, rows.Err()
}

func vectorArg(v pgvector.Vector) interface{} {
	if len(v.Slice()) == 0 {
		return nil
	}
	return v
}

func jsonbArg(v map[string]interface{}) interface{} {
	if len(v) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func (c *Client) GetLibraryStats(ctx context.Context) (map[string]int, error) {
	stats := make(map[string]int)

	var totalAlbums, totalArtists, unplayedAlbums, ratedAlbums int
	if err := c.pool.QueryRow(ctx, "SELECT COUNT(*) FROM albums").Scan(&totalAlbums); err != nil {
		return nil, err
	}
	stats["totalAlbums"] = totalAlbums

	if err := c.pool.QueryRow(ctx, "SELECT COUNT(*) FROM artists").Scan(&totalArtists); err != nil {
		return nil, err
	}
	stats["totalArtists"] = totalArtists

	if err := c.pool.QueryRow(ctx, "SELECT COUNT(*) FROM albums WHERE play_count = 0").Scan(&unplayedAlbums); err != nil {
		return nil, err
	}
	stats["unplayedAlbums"] = unplayedAlbums

	if err := c.pool.QueryRow(ctx, "SELECT COUNT(*) FROM albums WHERE rating > 0").Scan(&ratedAlbums); err != nil {
		return nil, err
	}
	stats["ratedAlbums"] = ratedAlbums

	return stats, nil
}

func (c *Client) GetLastSync(ctx context.Context) (time.Time, error) {
	var value string
	err := c.pool.QueryRow(ctx, "SELECT value FROM sync_metadata WHERE key = 'last_sync'").Scan(&value)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, value)
}

func (c *Client) SetLastSync(ctx context.Context, t time.Time) error {
	_, err := c.pool.Exec(ctx, "UPDATE sync_metadata SET value = $1, updated_at = NOW() WHERE key = 'last_sync'", t.Format(time.RFC3339))
	return err
}

func (c *Client) GetLastScrobbleSubmissionTime(ctx context.Context) (int64, error) {
	var value string
	err := c.pool.QueryRow(ctx, "SELECT value FROM sync_metadata WHERE key = 'last_scrobble_submission_time'").Scan(&value)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

func (c *Client) SetLastScrobbleSubmissionTime(ctx context.Context, submissionTime int64) error {
	_, err := c.pool.Exec(
		ctx,
		`INSERT INTO sync_metadata (key, value, updated_at)
		 VALUES ('last_scrobble_submission_time', $1, NOW())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
		fmt.Sprintf("%d", submissionTime),
	)
	return err
}

func (c *Client) InsertPlayEvents(ctx context.Context, events []PlayEvent) error {
	if len(events) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, e := range events {
		batch.Queue(
			`INSERT INTO play_events (user_id, track_id, played_at, submission_time)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (user_id, track_id, played_at) DO NOTHING`,
			e.UserID, e.TrackID, e.PlayedAt, e.SubmissionTime,
		)
	}

	results := c.pool.SendBatch(ctx, batch)
	defer results.Close()
	for range events {
		if _, err := results.Exec(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) GetListeningSummary(ctx context.Context, start, end time.Time, trackLimit, artistLimit int) (*ListeningSummary, error) {
	if trackLimit <= 0 {
		trackLimit = 20
	}
	if artistLimit <= 0 {
		artistLimit = 8
	}

	summary := &ListeningSummary{
		WindowStart: start,
		WindowEnd:   end,
	}

	if err := c.pool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM play_events
		 WHERE played_at >= $1 AND played_at < $2`,
		start, end,
	).Scan(&summary.TotalPlays); err != nil {
		return nil, err
	}

	if err := c.pool.QueryRow(
		ctx,
		`SELECT COUNT(DISTINCT e.track_id) FROM play_events e
		 WHERE e.played_at >= $1 AND e.played_at < $2`,
		start, end,
	).Scan(&summary.TracksHeard); err != nil {
		return nil, err
	}

	if err := c.pool.QueryRow(
		ctx,
		`SELECT COUNT(DISTINCT t.artist_name) FROM play_events e
		 JOIN tracks t ON t.id = e.track_id
		 WHERE e.played_at >= $1 AND e.played_at < $2`,
		start, end,
	).Scan(&summary.ArtistsHeard); err != nil {
		return nil, err
	}

	artistRows, err := c.pool.Query(
		ctx,
		`SELECT t.artist_name, COUNT(*) AS play_events
		 FROM play_events e
		 JOIN tracks t ON t.id = e.track_id
		 WHERE e.played_at >= $1 AND e.played_at < $2
		 GROUP BY t.artist_name
		 ORDER BY play_events DESC, t.artist_name ASC
		 LIMIT $3`,
		start, end, artistLimit,
	)
	if err != nil {
		return nil, err
	}
	defer artistRows.Close()

	for artistRows.Next() {
		var item ArtistListeningSummary
		if err := artistRows.Scan(&item.ArtistName, &item.TrackCount); err != nil {
			return nil, err
		}
		summary.TopArtists = append(summary.TopArtists, item)
	}
	if err := artistRows.Err(); err != nil {
		return nil, err
	}

	trackRows, err := c.pool.Query(
		ctx,
		`SELECT t.id, t.album_id, t.title, t.artist_name, COUNT(*) AS window_play_count, MAX(e.played_at) AS last_played_in_window
		 FROM play_events e
		 JOIN tracks t ON t.id = e.track_id
		 WHERE e.played_at >= $1 AND e.played_at < $2
		 GROUP BY t.id, t.album_id, t.title, t.artist_name
		 ORDER BY window_play_count DESC, last_played_in_window DESC
		 LIMIT $3`,
		start, end, trackLimit,
	)
	if err != nil {
		return nil, err
	}
	defer trackRows.Close()

	for trackRows.Next() {
		var t Track
		if err := trackRows.Scan(&t.ID, &t.AlbumID, &t.Title, &t.ArtistName, &t.PlayCount, &t.LastPlayed); err != nil {
			return nil, err
		}
		summary.TopTracks = append(summary.TopTracks, t)
	}
	if err := trackRows.Err(); err != nil {
		return nil, err
	}

	return summary, nil
}

func (c *Client) GetSyncStatus(ctx context.Context) (*SyncStatus, error) {
	lastSync, err := c.GetLastSync(ctx)
	if err != nil {
		return nil, err
	}
	lastSubmission, err := c.GetLastScrobbleSubmissionTime(ctx)
	if err != nil {
		return nil, err
	}

	var latest sql.NullTime
	var total int
	if err := c.pool.QueryRow(
		ctx,
		`SELECT MAX(played_at), COUNT(*) FROM play_events`,
	).Scan(&latest, &total); err != nil {
		return nil, err
	}

	status := &SyncStatus{
		LastSync:                   lastSync,
		LastScrobbleSubmissionTime: lastSubmission,
		PlayEventsCount:            total,
	}
	if latest.Valid {
		t := latest.Time
		status.LatestPlayEvent = &t
	}
	return status, nil
}

func (c *Client) ensureRuntimeSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS play_events (
			id BIGSERIAL PRIMARY KEY,
			user_id TEXT NOT NULL,
			track_id TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
			played_at TIMESTAMP NOT NULL,
			submission_time BIGINT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_play_events_unique ON play_events(user_id, track_id, played_at)`,
		`CREATE INDEX IF NOT EXISTS idx_play_events_played_at ON play_events(played_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_play_events_track_id ON play_events(track_id)`,
		`ALTER TABLE albums ADD COLUMN IF NOT EXISTS embedding_document TEXT`,
		`ALTER TABLE albums ADD COLUMN IF NOT EXISTS embedding_version TEXT`,
		`ALTER TABLE tracks ADD COLUMN IF NOT EXISTS rating INTEGER DEFAULT 0`,
		`INSERT INTO sync_metadata (key, value) VALUES ('last_scrobble_submission_time', '0') ON CONFLICT DO NOTHING`,
	}

	for _, stmt := range statements {
		if _, err := c.pool.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
