package db

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
)

const (
	defaultTasteProfileScope  = "global"
	tasteProfileRecentWindow  = 30 * 24 * time.Hour
	tasteProfileFatigueWindow = 14 * 24 * time.Hour
)

type TasteProfileSummary struct {
	Scope                 string
	TotalPlays            int
	DistinctPlayedTracks  int
	DistinctPlayedArtists int
	RatedTracks           int
	ReplayAffinityScore   float64
	NoveltyToleranceScore float64
	UpdatedAt             time.Time
}

type TasteProfileArtistFeature struct {
	Scope            string
	ArtistName       string
	TotalPlays       int
	RecentPlays      int
	LastPlayed       *time.Time
	AverageRating    float64
	FamiliarityScore float64
	FatigueScore     float64
	UpdatedAt        time.Time
}

type TasteProfileAlbumFeature struct {
	Scope             string
	AlbumID           string
	AlbumName         string
	ArtistName        string
	TotalPlays        int
	RecentPlays       int
	LastPlayed        *time.Time
	Rating            int
	OverexposureScore float64
	UpdatedAt         time.Time
}

type artistTasteSignal struct {
	ArtistName    string
	TotalPlays    int
	RecentPlays   int
	LastPlayed    *time.Time
	AverageRating float64
}

type albumTasteSignal struct {
	AlbumID     string
	AlbumName   string
	ArtistName  string
	TotalPlays  int
	RecentPlays int
	LastPlayed  *time.Time
	Rating      int
}

func (c *Client) RebuildTasteProfile(ctx context.Context) (*TasteProfileSummary, error) {
	now := time.Now().UTC()
	recentCutoff := now.Add(-tasteProfileRecentWindow)

	summary, err := c.collectTasteProfileSummary(ctx)
	if err != nil {
		return nil, err
	}
	artistSignals, err := c.collectArtistTasteSignals(ctx, recentCutoff)
	if err != nil {
		return nil, err
	}
	albumSignals, err := c.collectAlbumTasteSignals(ctx, recentCutoff)
	if err != nil {
		return nil, err
	}

	summary.Scope = defaultTasteProfileScope
	summary.ReplayAffinityScore = computeReplayAffinity(summary.TotalPlays, summary.DistinctPlayedTracks)
	summary.NoveltyToleranceScore = computeNoveltyTolerance(summary.TotalPlays, summary.DistinctPlayedTracks)
	summary.UpdatedAt = now

	artistFeatures := buildArtistTasteProfile(now, artistSignals)
	albumFeatures := buildAlbumTasteProfile(now, albumSignals)

	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM taste_profile_artist_features WHERE scope = $1`, defaultTasteProfileScope); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM taste_profile_album_features WHERE scope = $1`, defaultTasteProfileScope); err != nil {
		return nil, err
	}

	artistBatch := &pgx.Batch{}
	for _, item := range artistFeatures {
		artistBatch.Queue(
			`INSERT INTO taste_profile_artist_features
				(scope, artist_name, total_plays, recent_plays, last_played, average_rating, familiarity_score, fatigue_score, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			item.Scope,
			item.ArtistName,
			item.TotalPlays,
			item.RecentPlays,
			item.LastPlayed,
			item.AverageRating,
			item.FamiliarityScore,
			item.FatigueScore,
			item.UpdatedAt,
		)
	}
	artistResults := tx.SendBatch(ctx, artistBatch)
	for range artistFeatures {
		if _, err := artistResults.Exec(); err != nil {
			artistResults.Close()
			return nil, err
		}
	}
	if err := artistResults.Close(); err != nil {
		return nil, err
	}

	albumBatch := &pgx.Batch{}
	for _, item := range albumFeatures {
		albumBatch.Queue(
			`INSERT INTO taste_profile_album_features
				(scope, album_id, album_name, artist_name, total_plays, recent_plays, last_played, rating, overexposure_score, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			item.Scope,
			item.AlbumID,
			item.AlbumName,
			item.ArtistName,
			item.TotalPlays,
			item.RecentPlays,
			item.LastPlayed,
			item.Rating,
			item.OverexposureScore,
			item.UpdatedAt,
		)
	}
	albumResults := tx.SendBatch(ctx, albumBatch)
	for range albumFeatures {
		if _, err := albumResults.Exec(); err != nil {
			albumResults.Close()
			return nil, err
		}
	}
	if err := albumResults.Close(); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO taste_profile_summary
			(scope, total_plays, distinct_played_tracks, distinct_played_artists, rated_tracks, replay_affinity_score, novelty_tolerance_score, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (scope) DO UPDATE SET
			total_plays = EXCLUDED.total_plays,
			distinct_played_tracks = EXCLUDED.distinct_played_tracks,
			distinct_played_artists = EXCLUDED.distinct_played_artists,
			rated_tracks = EXCLUDED.rated_tracks,
			replay_affinity_score = EXCLUDED.replay_affinity_score,
			novelty_tolerance_score = EXCLUDED.novelty_tolerance_score,
			updated_at = EXCLUDED.updated_at`,
		summary.Scope,
		summary.TotalPlays,
		summary.DistinctPlayedTracks,
		summary.DistinctPlayedArtists,
		summary.RatedTracks,
		summary.ReplayAffinityScore,
		summary.NoveltyToleranceScore,
		summary.UpdatedAt,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return summary, nil
}

func (c *Client) GetTasteProfileSummary(ctx context.Context) (*TasteProfileSummary, error) {
	row := c.pool.QueryRow(
		ctx,
		`SELECT scope, total_plays, distinct_played_tracks, distinct_played_artists, rated_tracks, replay_affinity_score, novelty_tolerance_score, updated_at
		 FROM taste_profile_summary
		 WHERE scope = $1`,
		defaultTasteProfileScope,
	)
	var summary TasteProfileSummary
	if err := row.Scan(
		&summary.Scope,
		&summary.TotalPlays,
		&summary.DistinctPlayedTracks,
		&summary.DistinctPlayedArtists,
		&summary.RatedTracks,
		&summary.ReplayAffinityScore,
		&summary.NoveltyToleranceScore,
		&summary.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &summary, nil
}

func (c *Client) GetArtistTasteFeatures(ctx context.Context, artistNames []string) (map[string]TasteProfileArtistFeature, error) {
	keys := uniqueNormalizedTasteProfileKeys(artistNames)
	if len(keys) == 0 {
		return map[string]TasteProfileArtistFeature{}, nil
	}
	query := fmt.Sprintf(`SELECT
		scope,
		artist_name,
		total_plays,
		recent_plays,
		last_played,
		average_rating,
		familiarity_score,
		fatigue_score,
		updated_at
	FROM taste_profile_artist_features
	WHERE scope = $1
	  AND %s = ANY($2)`, normalizedArtistKeySQL("artist_name"))

	rows, err := c.pool.Query(ctx, query, defaultTasteProfileScope, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]TasteProfileArtistFeature, len(keys))
	for rows.Next() {
		var item TasteProfileArtistFeature
		var lastPlayed sql.NullTime
		if err := rows.Scan(
			&item.Scope,
			&item.ArtistName,
			&item.TotalPlays,
			&item.RecentPlays,
			&lastPlayed,
			&item.AverageRating,
			&item.FamiliarityScore,
			&item.FatigueScore,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		item.LastPlayed = nullTimePtr(lastPlayed)
		out[normalizeTasteProfileKey(item.ArtistName)] = item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetAlbumTasteFeatures(ctx context.Context, albumIDs []string) (map[string]TasteProfileAlbumFeature, error) {
	keys := uniqueTrimmedStrings(albumIDs)
	if len(keys) == 0 {
		return map[string]TasteProfileAlbumFeature{}, nil
	}
	rows, err := c.pool.Query(
		ctx,
		`SELECT
			scope,
			album_id,
			album_name,
			artist_name,
			total_plays,
			recent_plays,
			last_played,
			rating,
			overexposure_score,
			updated_at
		FROM taste_profile_album_features
		WHERE scope = $1
		  AND album_id = ANY($2)`,
		defaultTasteProfileScope,
		keys,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]TasteProfileAlbumFeature, len(keys))
	for rows.Next() {
		var item TasteProfileAlbumFeature
		var lastPlayed sql.NullTime
		if err := rows.Scan(
			&item.Scope,
			&item.AlbumID,
			&item.AlbumName,
			&item.ArtistName,
			&item.TotalPlays,
			&item.RecentPlays,
			&lastPlayed,
			&item.Rating,
			&item.OverexposureScore,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		item.LastPlayed = nullTimePtr(lastPlayed)
		out[strings.TrimSpace(item.AlbumID)] = item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) collectTasteProfileSummary(ctx context.Context) (*TasteProfileSummary, error) {
	artistKeyExpr := normalizedArtistKeySQL("artist_name")
	query := fmt.Sprintf(`SELECT
		COALESCE(SUM(play_count), 0) AS total_plays,
		COUNT(*) FILTER (WHERE play_count > 0) AS distinct_played_tracks,
		COUNT(DISTINCT %s) FILTER (WHERE play_count > 0 AND artist_name IS NOT NULL AND TRIM(artist_name) <> '') AS distinct_played_artists,
		COUNT(*) FILTER (WHERE rating > 0) AS rated_tracks
		FROM tracks`, artistKeyExpr)

	summary := &TasteProfileSummary{Scope: defaultTasteProfileScope}
	if err := c.pool.QueryRow(ctx, query).Scan(
		&summary.TotalPlays,
		&summary.DistinctPlayedTracks,
		&summary.DistinctPlayedArtists,
		&summary.RatedTracks,
	); err != nil {
		return nil, err
	}
	return summary, nil
}

func (c *Client) collectArtistTasteSignals(ctx context.Context, recentCutoff time.Time) ([]artistTasteSignal, error) {
	trackArtistKey := normalizedArtistKeySQL("artist_name")
	eventArtistKey := normalizedArtistKeySQL("t.artist_name")
	query := fmt.Sprintf(`WITH track_rollup AS (
		SELECT
			%s AS artist_key,
			string_agg(DISTINCT artist_name, '||') AS artist_variants,
			COALESCE(SUM(play_count), 0) AS total_plays,
			MAX(last_played) AS track_last_played,
			AVG(NULLIF(rating, 0)) AS average_rating
		FROM tracks
		WHERE artist_name IS NOT NULL AND TRIM(artist_name) <> ''
		GROUP BY artist_key
	), recent_rollup AS (
		SELECT
			%s AS artist_key,
			COUNT(*) AS recent_plays,
			MAX(e.played_at) AS recent_last_played
		FROM play_events e
		JOIN tracks t ON t.id = e.track_id
		WHERE e.played_at >= $1
		  AND t.artist_name IS NOT NULL
		  AND TRIM(t.artist_name) <> ''
		GROUP BY artist_key
	)
	SELECT
		COALESCE(track_rollup.artist_variants, recent_rollup.artist_key) AS artist_variants,
		COALESCE(track_rollup.artist_key, recent_rollup.artist_key) AS artist_key,
		COALESCE(track_rollup.total_plays, 0) AS total_plays,
		COALESCE(recent_rollup.recent_plays, 0) AS recent_plays,
		track_rollup.track_last_played,
		recent_rollup.recent_last_played,
		COALESCE(track_rollup.average_rating, 0) AS average_rating
	FROM track_rollup
	FULL OUTER JOIN recent_rollup ON recent_rollup.artist_key = track_rollup.artist_key
	WHERE COALESCE(track_rollup.total_plays, 0) > 0
	   OR COALESCE(recent_rollup.recent_plays, 0) > 0
	   OR COALESCE(track_rollup.average_rating, 0) > 0`, trackArtistKey, eventArtistKey)

	rows, err := c.pool.Query(ctx, query, recentCutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	signals := make([]artistTasteSignal, 0, 128)
	for rows.Next() {
		var variants string
		var fallback string
		var totalPlays int
		var recentPlays int
		var trackLastPlayed sql.NullTime
		var recentLastPlayed sql.NullTime
		var averageRating float64
		if err := rows.Scan(&variants, &fallback, &totalPlays, &recentPlays, &trackLastPlayed, &recentLastPlayed, &averageRating); err != nil {
			return nil, err
		}
		lastPlayed := maxTimePointer(nullTimePtr(trackLastPlayed), nullTimePtr(recentLastPlayed))
		signals = append(signals, artistTasteSignal{
			ArtistName:    preferredArtistDisplayName(variants, fallback),
			TotalPlays:    totalPlays,
			RecentPlays:   recentPlays,
			LastPlayed:    lastPlayed,
			AverageRating: averageRating,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return signals, nil
}

func (c *Client) collectAlbumTasteSignals(ctx context.Context, recentCutoff time.Time) ([]albumTasteSignal, error) {
	query := `WITH recent_rollup AS (
		SELECT
			t.album_id,
			COUNT(*) AS recent_plays,
			MAX(e.played_at) AS recent_last_played
		FROM play_events e
		JOIN tracks t ON t.id = e.track_id
		WHERE e.played_at >= $1
		  AND t.album_id IS NOT NULL
		  AND TRIM(t.album_id) <> ''
		GROUP BY t.album_id
	)
	SELECT
		a.id,
		a.name,
		a.artist_name,
		COALESCE(a.play_count, 0) AS total_plays,
		COALESCE(recent_rollup.recent_plays, 0) AS recent_plays,
		a.last_played,
		recent_rollup.recent_last_played,
		COALESCE(a.rating, 0) AS rating
	FROM albums a
	LEFT JOIN recent_rollup ON recent_rollup.album_id = a.id
	WHERE a.play_count > 0
	   OR a.rating > 0
	   OR COALESCE(recent_rollup.recent_plays, 0) > 0`

	rows, err := c.pool.Query(ctx, query, recentCutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	signals := make([]albumTasteSignal, 0, 128)
	for rows.Next() {
		var signal albumTasteSignal
		var albumLastPlayed sql.NullTime
		var recentLastPlayed sql.NullTime
		if err := rows.Scan(
			&signal.AlbumID,
			&signal.AlbumName,
			&signal.ArtistName,
			&signal.TotalPlays,
			&signal.RecentPlays,
			&albumLastPlayed,
			&recentLastPlayed,
			&signal.Rating,
		); err != nil {
			return nil, err
		}
		signal.LastPlayed = maxTimePointer(nullTimePtr(albumLastPlayed), nullTimePtr(recentLastPlayed))
		signals = append(signals, signal)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return signals, nil
}

func buildArtistTasteProfile(now time.Time, signals []artistTasteSignal) []TasteProfileArtistFeature {
	maxTotal := 0
	maxRecent := 0
	for _, item := range signals {
		if item.TotalPlays > maxTotal {
			maxTotal = item.TotalPlays
		}
		if item.RecentPlays > maxRecent {
			maxRecent = item.RecentPlays
		}
	}

	out := make([]TasteProfileArtistFeature, 0, len(signals))
	for _, item := range signals {
		familiarity := clampUnit(
			0.55*normalizedLogScore(item.TotalPlays, maxTotal) +
				0.25*normalizedLogScore(item.RecentPlays, maxRecent) +
				0.20*ratingScore(item.AverageRating),
		)
		fatigue := clampUnit(
			0.65*normalizedLogScore(item.RecentPlays, maxRecent) +
				0.35*recencyScore(item.LastPlayed, now, tasteProfileFatigueWindow),
		)
		out = append(out, TasteProfileArtistFeature{
			Scope:            defaultTasteProfileScope,
			ArtistName:       item.ArtistName,
			TotalPlays:       item.TotalPlays,
			RecentPlays:      item.RecentPlays,
			LastPlayed:       item.LastPlayed,
			AverageRating:    roundTo(item.AverageRating, 2),
			FamiliarityScore: roundTo(familiarity, 4),
			FatigueScore:     roundTo(fatigue, 4),
			UpdatedAt:        now,
		})
	}
	return out
}

func buildAlbumTasteProfile(now time.Time, signals []albumTasteSignal) []TasteProfileAlbumFeature {
	maxTotal := 0
	maxRecent := 0
	for _, item := range signals {
		if item.TotalPlays > maxTotal {
			maxTotal = item.TotalPlays
		}
		if item.RecentPlays > maxRecent {
			maxRecent = item.RecentPlays
		}
	}

	out := make([]TasteProfileAlbumFeature, 0, len(signals))
	for _, item := range signals {
		overexposure := clampUnit(
			0.60*normalizedLogScore(item.TotalPlays, maxTotal) +
				0.30*normalizedLogScore(item.RecentPlays, maxRecent) +
				0.10*recencyScore(item.LastPlayed, now, tasteProfileFatigueWindow),
		)
		out = append(out, TasteProfileAlbumFeature{
			Scope:             defaultTasteProfileScope,
			AlbumID:           item.AlbumID,
			AlbumName:         item.AlbumName,
			ArtistName:        item.ArtistName,
			TotalPlays:        item.TotalPlays,
			RecentPlays:       item.RecentPlays,
			LastPlayed:        item.LastPlayed,
			Rating:            item.Rating,
			OverexposureScore: roundTo(overexposure, 4),
			UpdatedAt:         now,
		})
	}
	return out
}

func computeReplayAffinity(totalPlays, distinctTracks int) float64 {
	if totalPlays <= 0 || distinctTracks <= 0 {
		return 0
	}
	return clampUnit(1 - (float64(distinctTracks) / float64(totalPlays)))
}

func computeNoveltyTolerance(totalPlays, distinctTracks int) float64 {
	if totalPlays <= 0 || distinctTracks <= 0 {
		return 0
	}
	return clampUnit(float64(distinctTracks) / float64(totalPlays))
}

func normalizedLogScore(value, maxValue int) float64 {
	if value <= 0 || maxValue <= 0 {
		return 0
	}
	return clampUnit(math.Log1p(float64(value)) / math.Log1p(float64(maxValue)))
}

func ratingScore(value float64) float64 {
	if value <= 0 {
		return 0
	}
	if value > 5 {
		value = 5
	}
	return clampUnit(value / 5)
}

func recencyScore(lastPlayed *time.Time, now time.Time, window time.Duration) float64 {
	if lastPlayed == nil || window <= 0 {
		return 0
	}
	age := now.Sub(lastPlayed.UTC())
	if age <= 0 {
		return 1
	}
	if age >= window {
		return 0
	}
	return clampUnit(1 - (age.Seconds() / window.Seconds()))
}

func clampUnit(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}

func roundTo(value float64, decimals int) float64 {
	if decimals < 0 {
		return value
	}
	factor := math.Pow(10, float64(decimals))
	return math.Round(value*factor) / factor
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time.UTC()
	return &t
}

func maxTimePointer(left, right *time.Time) *time.Time {
	switch {
	case left == nil && right == nil:
		return nil
	case left == nil:
		t := right.UTC()
		return &t
	case right == nil:
		t := left.UTC()
		return &t
	case right.After(*left):
		t := right.UTC()
		return &t
	default:
		t := left.UTC()
		return &t
	}
}

func uniqueNormalizedTasteProfileKeys(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		key := normalizeTasteProfileKey(value)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func uniqueTrimmedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func normalizeTasteProfileKey(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastSpace := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
