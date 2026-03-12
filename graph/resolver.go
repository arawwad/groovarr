//go:generate go run github.com/99designs/gqlgen generate

package graph

import (
	"context"
	"fmt"
	"groovarr/internal/db"
	"strings"
	"time"
)

type Resolver struct {
	DB *db.Client
}

func (r *Resolver) Query() QueryResolver {
	return &queryResolver{r}
}

func (r *Resolver) Mutation() MutationResolver {
	return &mutationResolver{r}
}

type queryResolver struct{ *Resolver }
type mutationResolver struct{ *Resolver }

func (r *queryResolver) Albums(ctx context.Context, limit *int, rating *int, ratingBelow *int, unplayed *bool, notPlayedSince *string, genre *string, year *int, artistName *string) ([]*Album, error) {
	l := 10
	if limit != nil {
		l = *limit
	}

	filters := make(map[string]interface{})
	if rating != nil {
		filters["rating"] = *rating
	}
	if ratingBelow != nil {
		filters["ratingBelow"] = *ratingBelow
	}
	if unplayed != nil {
		filters["unplayed"] = *unplayed
	}
	if genre != nil {
		filters["genre"] = *genre
	}
	if year != nil {
		filters["year"] = *year
	}
	if artistName != nil {
		filters["artistName"] = *artistName
	}
	if notPlayedSince != nil && *notPlayedSince != "" {
		cutoff, err := time.Parse(time.RFC3339, *notPlayedSince)
		if err != nil {
			return nil, err
		}
		filters["notPlayedSince"] = cutoff
	}

	albums, err := r.DB.GetAlbums(ctx, l, filters)
	if err != nil {
		return nil, err
	}

	result := make([]*Album, len(albums))
	for i, a := range albums {
		var lastPlayed *string
		if a.LastPlayed != nil {
			t := a.LastPlayed.Format(time.RFC3339)
			lastPlayed = &t
		}
		result[i] = &Album{
			ID:         a.ID,
			Name:       a.Name,
			ArtistName: a.ArtistName,
			Rating:     a.Rating,
			PlayCount:  a.PlayCount,
			LastPlayed: lastPlayed,
			Year:       a.Year,
			Genre:      a.Genre,
		}
	}
	return result, nil
}

func (r *queryResolver) Artists(ctx context.Context, limit *int, minPlayCount *int, artistName *string) ([]*Artist, error) {
	l := 10
	if limit != nil {
		l = *limit
	}
	minPC := 0
	if minPlayCount != nil {
		minPC = *minPlayCount
	}

	artists, err := r.DB.GetArtists(ctx, l, minPC, artistName)
	if err != nil {
		return nil, err
	}

	result := make([]*Artist, len(artists))
	for i, a := range artists {
		result[i] = &Artist{
			ID:        a.ID,
			Name:      a.Name,
			Rating:    a.Rating,
			PlayCount: a.PlayCount,
		}
	}
	return result, nil
}

func (r *queryResolver) Tracks(ctx context.Context, limit *int, mostPlayed *bool, playedSince *string, playedUntil *string, onlyPlayed *bool) ([]*Track, error) {
	l := 10
	if limit != nil {
		l = *limit
	}
	mp := true
	if mostPlayed != nil {
		mp = *mostPlayed
	}

	filters := make(map[string]interface{})
	if playedSince != nil && *playedSince != "" {
		t, err := parseFlexibleTime(*playedSince)
		if err != nil {
			return nil, err
		}
		filters["playedSince"] = t
	}
	if playedUntil != nil && *playedUntil != "" {
		t, err := parseFlexibleTime(*playedUntil)
		if err != nil {
			return nil, err
		}
		filters["playedUntil"] = t
	}
	if onlyPlayed != nil {
		filters["onlyPlayed"] = *onlyPlayed
	}

	tracks, err := r.DB.GetTracks(ctx, l, mp, filters)
	if err != nil {
		return nil, err
	}

	result := make([]*Track, len(tracks))
	for i, t := range tracks {
		var lastPlayed *string
		if t.LastPlayed != nil {
			timeStr := t.LastPlayed.Format(time.RFC3339)
			lastPlayed = &timeStr
		}
		result[i] = &Track{
			ID:         t.ID,
			Title:      t.Title,
			ArtistName: t.ArtistName,
			PlayCount:  t.PlayCount,
			LastPlayed: lastPlayed,
		}
	}
	return result, nil
}

func (r *queryResolver) SimilarArtists(ctx context.Context, seedArtist string, limit *int) ([]*Artist, error) {
	l := 5
	if limit != nil {
		l = *limit
	}

	// 1. Get the seed artist's embedding
	artist, err := r.DB.GetArtistByName(ctx, seedArtist)
	if err != nil {
		return nil, err
	}
	if artist == nil {
		return []*Artist{}, nil
	}

	// 2. Find similar artists using the embedding
	similar, err := r.DB.FindSimilarArtists(ctx, artist.Embedding, l)
	if err != nil {
		return nil, err
	}

	// 3. Convert to graph type
	result := make([]*Artist, len(similar))
	for i, a := range similar {
		result[i] = &Artist{
			ID:        a.ID,
			Name:      a.Name,
			Rating:    a.Rating,
			PlayCount: a.PlayCount,
		}
	}
	return result, nil
}

func (r *queryResolver) SimilarAlbums(ctx context.Context, seedAlbum string, limit *int) ([]*Album, error) {
	l := 5
	if limit != nil {
		l = *limit
	}

	// 1. Get the seed album's embedding
	album, err := r.DB.GetAlbumByName(ctx, seedAlbum)
	if err != nil {
		return nil, err
	}
	if album == nil {
		return []*Album{}, nil
	}

	// 2. Find similar albums using the embedding
	similar, err := r.DB.FindSimilarAlbums(ctx, album.Embedding, l)
	if err != nil {
		return nil, err
	}

	// 3. Convert to graph type
	result := make([]*Album, len(similar))
	for i, a := range similar {
		var lastPlayed *string
		if a.LastPlayed != nil {
			t := a.LastPlayed.Format(time.RFC3339)
			lastPlayed = &t
		}
		result[i] = &Album{
			ID:         a.ID,
			Name:       a.Name,
			ArtistName: a.ArtistName,
			Rating:     a.Rating,
			PlayCount:  a.PlayCount,
			LastPlayed: lastPlayed,
			Year:       a.Year,
			Genre:      a.Genre,
		}
	}
	return result, nil
}

func (r *queryResolver) LibraryStats(ctx context.Context) (*LibraryStats, error) {
	stats, err := r.DB.GetLibraryStats(ctx)
	if err != nil {
		return nil, err
	}

	return &LibraryStats{
		TotalAlbums:    stats["totalAlbums"],
		TotalArtists:   stats["totalArtists"],
		UnplayedAlbums: stats["unplayedAlbums"],
		RatedAlbums:    stats["ratedAlbums"],
	}, nil
}

func (r *queryResolver) ArtistLibraryStats(ctx context.Context, filter *ArtistLibraryStatsFilter, sort *string, limit *int) ([]*ArtistLibraryStat, error) {
	l := 25
	if limit != nil {
		l = *limit
	}
	sortBy := "album_count_asc"
	if sort != nil && strings.TrimSpace(*sort) != "" {
		sortBy = strings.ToLower(strings.TrimSpace(*sort))
	}

	dbFilter := db.ArtistLibraryStatsFilter{}
	if filter != nil {
		dbFilter = db.ArtistLibraryStatsFilter{
			ArtistName:       filter.ArtistName,
			Genre:            filter.Genre,
			ExactAlbums:      filter.ExactAlbums,
			MinAlbums:        filter.MinAlbums,
			MaxAlbums:        filter.MaxAlbums,
			MinTotalPlays:    filter.MinTotalPlays,
			MaxTotalPlays:    filter.MaxTotalPlays,
			MaxPlaysInWindow: filter.MaxPlaysInWindow,
		}
		if filter.InactiveSince != nil && *filter.InactiveSince != "" {
			t, err := parseFlexibleTime(*filter.InactiveSince)
			if err != nil {
				return nil, err
			}
			dbFilter.InactiveSince = &t
		}
		if filter.PlayedSince != nil && *filter.PlayedSince != "" {
			t, err := parseFlexibleTime(*filter.PlayedSince)
			if err != nil {
				return nil, err
			}
			dbFilter.PlayedSince = &t
		}
		if filter.PlayedUntil != nil && *filter.PlayedUntil != "" {
			t, err := parseFlexibleTime(*filter.PlayedUntil)
			if err != nil {
				return nil, err
			}
			dbFilter.PlayedUntil = &t
		}
	}

	stats, err := r.DB.GetArtistLibraryStats(ctx, l, dbFilter, sortBy)
	if err != nil {
		return nil, err
	}
	result := make([]*ArtistLibraryStat, len(stats))
	for i, stat := range stats {
		var lastPlayed *string
		if stat.LastPlayed != nil {
			v := stat.LastPlayed.Format(time.RFC3339)
			lastPlayed = &v
		}
		result[i] = &ArtistLibraryStat{
			ArtistName:         stat.ArtistName,
			AlbumCount:         stat.AlbumCount,
			TotalPlayCount:     stat.TotalPlayCount,
			LastPlayed:         lastPlayed,
			UnplayedAlbumCount: stat.UnplayedAlbumCount,
			PlayedInWindow:     stat.PlayedInWindow,
		}
	}
	return result, nil
}

func (r *queryResolver) ArtistListeningStats(ctx context.Context, filter *ArtistListeningStatsFilter, sort *string, limit *int) ([]*ArtistListeningStat, error) {
	l := 25
	if limit != nil {
		l = *limit
	}
	sortBy := "plays_in_window_desc"
	if sort != nil && strings.TrimSpace(*sort) != "" {
		sortBy = strings.ToLower(strings.TrimSpace(*sort))
	}

	dbFilter := db.ArtistListeningStatsFilter{}
	if filter != nil {
		dbFilter = db.ArtistListeningStatsFilter{
			ArtistName:       filter.ArtistName,
			MinPlaysInWindow: filter.MinPlaysInWindow,
			MaxPlaysInWindow: filter.MaxPlaysInWindow,
			MinAlbums:        filter.MinAlbums,
			MaxAlbums:        filter.MaxAlbums,
		}
		if filter.PlayedSince != nil && *filter.PlayedSince != "" {
			t, err := parseFlexibleTime(*filter.PlayedSince)
			if err != nil {
				return nil, err
			}
			dbFilter.PlayedSince = &t
		}
		if filter.PlayedUntil != nil && *filter.PlayedUntil != "" {
			t, err := parseFlexibleTime(*filter.PlayedUntil)
			if err != nil {
				return nil, err
			}
			dbFilter.PlayedUntil = &t
		}
	}

	stats, err := r.DB.GetArtistListeningStats(ctx, l, dbFilter, sortBy)
	if err != nil {
		return nil, err
	}
	result := make([]*ArtistListeningStat, len(stats))
	for i, stat := range stats {
		var lastPlayed *string
		if stat.LastPlayed != nil {
			v := stat.LastPlayed.Format(time.RFC3339)
			lastPlayed = &v
		}
		result[i] = &ArtistListeningStat{
			ArtistName:     stat.ArtistName,
			AlbumCount:     stat.AlbumCount,
			TotalPlayCount: stat.TotalPlayCount,
			PlaysInWindow:  stat.PlaysInWindow,
			LastPlayed:     lastPlayed,
		}
	}
	return result, nil
}

func (r *queryResolver) LibraryFacetCounts(ctx context.Context, field string, filter *LibraryFacetFilter, limit *int) ([]*LibraryFacetCount, error) {
	l := 10
	if limit != nil {
		l = *limit
	}

	dbFilter := db.LibraryFacetFilter{}
	if filter != nil {
		dbFilter = db.LibraryFacetFilter{
			Genre:      filter.Genre,
			ArtistName: filter.ArtistName,
			Year:       filter.Year,
			MinYear:    filter.MinYear,
			MaxYear:    filter.MaxYear,
			Unplayed:   filter.Unplayed,
		}
		if filter.NotPlayedSince != nil && *filter.NotPlayedSince != "" {
			t, err := parseFlexibleTime(*filter.NotPlayedSince)
			if err != nil {
				return nil, err
			}
			dbFilter.NotPlayedSince = &t
		}
	}

	counts, err := r.DB.GetLibraryFacetCounts(ctx, field, l, dbFilter)
	if err != nil {
		return nil, err
	}
	result := make([]*LibraryFacetCount, len(counts))
	for i, count := range counts {
		result[i] = &LibraryFacetCount{
			Value: count.Value,
			Count: count.Count,
		}
	}
	return result, nil
}

func (r *queryResolver) AlbumRelationshipStats(ctx context.Context, filter *AlbumRelationshipStatsFilter, sort *string, limit *int) ([]*AlbumRelationshipStat, error) {
	l := 25
	if limit != nil {
		l = *limit
	}
	sortBy := "name_asc"
	if sort != nil && strings.TrimSpace(*sort) != "" {
		sortBy = strings.ToLower(strings.TrimSpace(*sort))
	}

	dbFilter := db.AlbumRelationshipStatsFilter{}
	if filter != nil {
		dbFilter = db.AlbumRelationshipStatsFilter{
			ArtistExactAlbums: filter.ArtistExactAlbums,
			ArtistMinAlbums:   filter.ArtistMinAlbums,
			ArtistMaxAlbums:   filter.ArtistMaxAlbums,
			Genre:             filter.Genre,
			Unplayed:          filter.Unplayed,
		}
		if filter.NotPlayedSince != nil && *filter.NotPlayedSince != "" {
			t, err := parseFlexibleTime(*filter.NotPlayedSince)
			if err != nil {
				return nil, err
			}
			dbFilter.NotPlayedSince = &t
		}
	}

	stats, err := r.DB.GetAlbumRelationshipStats(ctx, l, dbFilter, sortBy)
	if err != nil {
		return nil, err
	}
	result := make([]*AlbumRelationshipStat, len(stats))
	for i, stat := range stats {
		result[i] = &AlbumRelationshipStat{
			AlbumName:        stat.AlbumName,
			ArtistName:       stat.ArtistName,
			Year:             stat.Year,
			ArtistAlbumCount: stat.ArtistAlbumCount,
		}
	}
	return result, nil
}

func (r *queryResolver) AlbumLibraryStats(ctx context.Context, filter *AlbumLibraryStatsFilter, sort *string, limit *int) ([]*AlbumLibraryStat, error) {
	l := 25
	if limit != nil {
		l = *limit
	}
	sortBy := "last_played_desc"
	if sort != nil && strings.TrimSpace(*sort) != "" {
		sortBy = strings.ToLower(strings.TrimSpace(*sort))
	}

	dbFilter := db.AlbumLibraryStatsFilter{}
	if filter != nil {
		dbFilter = db.AlbumLibraryStatsFilter{
			ArtistName:       filter.ArtistName,
			Genre:            filter.Genre,
			Year:             filter.Year,
			MinYear:          filter.MinYear,
			MaxYear:          filter.MaxYear,
			MinTotalPlays:    filter.MinTotalPlays,
			MaxTotalPlays:    filter.MaxTotalPlays,
			MinRating:        filter.MinRating,
			MaxRating:        filter.MaxRating,
			MaxPlaysInWindow: filter.MaxPlaysInWindow,
			Unplayed:         filter.Unplayed,
		}
		if filter.InactiveSince != nil && *filter.InactiveSince != "" {
			t, err := parseFlexibleTime(*filter.InactiveSince)
			if err != nil {
				return nil, err
			}
			dbFilter.InactiveSince = &t
		}
		if filter.PlayedSince != nil && *filter.PlayedSince != "" {
			t, err := parseFlexibleTime(*filter.PlayedSince)
			if err != nil {
				return nil, err
			}
			dbFilter.PlayedSince = &t
		}
		if filter.PlayedUntil != nil && *filter.PlayedUntil != "" {
			t, err := parseFlexibleTime(*filter.PlayedUntil)
			if err != nil {
				return nil, err
			}
			dbFilter.PlayedUntil = &t
		}
	}

	stats, err := r.DB.GetAlbumLibraryStats(ctx, l, dbFilter, sortBy)
	if err != nil {
		return nil, err
	}
	result := make([]*AlbumLibraryStat, len(stats))
	for i, stat := range stats {
		var lastPlayed *string
		if stat.LastPlayed != nil {
			v := stat.LastPlayed.Format(time.RFC3339)
			lastPlayed = &v
		}
		result[i] = &AlbumLibraryStat{
			AlbumName:      stat.AlbumName,
			ArtistName:     stat.ArtistName,
			Year:           stat.Year,
			Genre:          stat.Genre,
			Rating:         stat.Rating,
			TotalPlayCount: stat.TotalPlayCount,
			LastPlayed:     lastPlayed,
			PlayedInWindow: stat.PlayedInWindow,
		}
	}
	return result, nil
}

func (r *mutationResolver) DeleteAlbum(ctx context.Context, id string) (bool, error) {
	err := r.DB.DeleteAlbum(ctx, id)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *mutationResolver) DeleteArtist(ctx context.Context, id string) (bool, error) {
	err := r.DB.DeleteArtist(ctx, id)
	if err != nil {
		return false, err
	}
	return true, nil
}

type Album struct {
	ID         string
	Name       string
	ArtistName string
	Rating     int
	PlayCount  int
	LastPlayed *string
	Year       *int
	Genre      *string
}

type Artist struct {
	ID        string
	Name      string
	Rating    int
	PlayCount int
}

type Track struct {
	ID         string
	Title      string
	ArtistName string
	PlayCount  int
	LastPlayed *string
}

type LibraryStats struct {
	TotalAlbums    int
	TotalArtists   int
	UnplayedAlbums int
	RatedAlbums    int
}

type ArtistLibraryStatsFilter struct {
	ArtistName       *string
	Genre            *string
	ExactAlbums      *int
	MinAlbums        *int
	MaxAlbums        *int
	MinTotalPlays    *int
	MaxTotalPlays    *int
	InactiveSince    *string
	PlayedSince      *string
	PlayedUntil      *string
	MaxPlaysInWindow *int
}

type ArtistLibraryStat struct {
	ArtistName         string
	AlbumCount         int
	TotalPlayCount     int
	LastPlayed         *string
	UnplayedAlbumCount int
	PlayedInWindow     int
}

type ArtistListeningStatsFilter struct {
	ArtistName       *string
	PlayedSince      *string
	PlayedUntil      *string
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
	LastPlayed     *string
}

type LibraryFacetFilter struct {
	Genre          *string
	ArtistName     *string
	Year           *int
	MinYear        *int
	MaxYear        *int
	Unplayed       *bool
	NotPlayedSince *string
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
	NotPlayedSince    *string
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
	InactiveSince    *string
	PlayedSince      *string
	PlayedUntil      *string
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
	LastPlayed     *string
	PlayedInWindow int
}

type QueryResolver interface {
	Albums(ctx context.Context, limit *int, rating *int, ratingBelow *int, unplayed *bool, notPlayedSince *string, genre *string, year *int, artistName *string) ([]*Album, error)
	Artists(ctx context.Context, limit *int, minPlayCount *int, artistName *string) ([]*Artist, error)
	Tracks(ctx context.Context, limit *int, mostPlayed *bool, playedSince *string, playedUntil *string, onlyPlayed *bool) ([]*Track, error)
	SimilarArtists(ctx context.Context, seedArtist string, limit *int) ([]*Artist, error)
	SimilarAlbums(ctx context.Context, seedAlbum string, limit *int) ([]*Album, error)
	ArtistLibraryStats(ctx context.Context, filter *ArtistLibraryStatsFilter, sort *string, limit *int) ([]*ArtistLibraryStat, error)
	ArtistListeningStats(ctx context.Context, filter *ArtistListeningStatsFilter, sort *string, limit *int) ([]*ArtistListeningStat, error)
	LibraryFacetCounts(ctx context.Context, field string, filter *LibraryFacetFilter, limit *int) ([]*LibraryFacetCount, error)
	AlbumRelationshipStats(ctx context.Context, filter *AlbumRelationshipStatsFilter, sort *string, limit *int) ([]*AlbumRelationshipStat, error)
	AlbumLibraryStats(ctx context.Context, filter *AlbumLibraryStatsFilter, sort *string, limit *int) ([]*AlbumLibraryStat, error)
	LibraryStats(ctx context.Context) (*LibraryStats, error)
}

type MutationResolver interface {
	DeleteAlbum(ctx context.Context, id string) (bool, error)
	DeleteArtist(ctx context.Context, id string) (bool, error)
}

func parseFlexibleTime(value string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if t, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", trimmed); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid time format: %q (expected RFC3339 or YYYY-MM-DD)", value)
}
