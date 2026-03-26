package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/metadata"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
)

const (
	defaultGroovarrURL     = "http://groovarr:8088"
	defaultProvider        = "hybrid"
	defaultSimilarityLimit = 25
	defaultRecentDays      = 14
)

type plugin struct{}

type config struct {
	GroovarrURL       string
	Provider          string
	ExcludeRecentDays int
	ExcludeSeedArtist bool
}

type trackSimilarityRequest struct {
	SeedTrackID       string `json:"seedTrackId,omitempty"`
	SeedTrackTitle    string `json:"seedTrackTitle,omitempty"`
	SeedArtistName    string `json:"seedArtistName,omitempty"`
	Provider          string `json:"provider,omitempty"`
	Limit             int    `json:"limit,omitempty"`
	ExcludeRecentDays int    `json:"excludeRecentDays,omitempty"`
	ExcludeSeedArtist bool   `json:"excludeSeedArtist,omitempty"`
}

type artistSimilarityRequest struct {
	SeedArtistName    string `json:"seedArtistName"`
	Provider          string `json:"provider,omitempty"`
	Limit             int    `json:"limit,omitempty"`
	ExcludeRecentDays int    `json:"excludeRecentDays,omitempty"`
	ExcludeSeedArtist bool   `json:"excludeSeedArtist,omitempty"`
}

type tracksEnvelope struct {
	Tracks similarityTracksPayload `json:"similarityTracks"`
}

type songsByArtistEnvelope struct {
	Tracks similarityTracksPayload `json:"similaritySongsByArtist"`
}

type artistsEnvelope struct {
	Artists similarityArtistsPayload `json:"similarityArtists"`
}

type similarityTracksPayload struct {
	Provider string              `json:"provider"`
	Results  []similarTrackEntry `json:"results"`
}

type similarityArtistsPayload struct {
	Provider string               `json:"provider"`
	Results  []similarArtistEntry `json:"results"`
}

type similarTrackEntry struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	ArtistName string   `json:"artistName"`
	Score      float64  `json:"score"`
	Sources    []string `json:"sources"`
}

type similarArtistEntry struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Score   float64  `json:"score"`
	Sources []string `json:"sources"`
}

func main() {
	metadata.Register(&plugin{})
}

func (p *plugin) GetSimilarSongsByTrack(req metadata.SimilarSongsByTrackRequest) (*metadata.SimilarSongsResponse, error) {
	cfg := loadConfig()
	body, err := p.postJSON(cfg, "/api/similarity/tracks", trackSimilarityRequest{
		SeedTrackID:       strings.TrimSpace(req.ID),
		SeedTrackTitle:    strings.TrimSpace(req.Name),
		SeedArtistName:    strings.TrimSpace(req.Artist),
		Provider:          cfg.Provider,
		Limit:             requestCount(req.Count, defaultSimilarityLimit),
		ExcludeRecentDays: cfg.ExcludeRecentDays,
		ExcludeSeedArtist: cfg.ExcludeSeedArtist,
	})
	if err != nil {
		return nil, err
	}
	var payload tracksEnvelope
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode Groovarr response: %w", err)
	}
	return &metadata.SimilarSongsResponse{
		Songs: mapTracks(payload.Tracks.Results),
	}, nil
}

func (p *plugin) GetSimilarSongsByArtist(req metadata.SimilarSongsByArtistRequest) (*metadata.SimilarSongsResponse, error) {
	cfg := loadConfig()
	body, err := p.postJSON(cfg, "/api/similarity/songs/by-artist", artistSimilarityRequest{
		SeedArtistName:    strings.TrimSpace(req.Name),
		Provider:          cfg.Provider,
		Limit:             requestCount(req.Count, defaultSimilarityLimit),
		ExcludeRecentDays: cfg.ExcludeRecentDays,
		ExcludeSeedArtist: cfg.ExcludeSeedArtist,
	})
	if err != nil {
		return nil, err
	}
	var payload songsByArtistEnvelope
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode Groovarr response: %w", err)
	}
	return &metadata.SimilarSongsResponse{
		Songs: mapTracks(payload.Tracks.Results),
	}, nil
}

func (p *plugin) GetSimilarArtists(req metadata.SimilarArtistsRequest) (*metadata.SimilarArtistsResponse, error) {
	cfg := loadConfig()
	body, err := p.postJSON(cfg, "/api/similarity/artists", artistSimilarityRequest{
		SeedArtistName: strings.TrimSpace(req.Name),
		Provider:       cfg.Provider,
		Limit:          requestCount(req.Limit, 10),
	})
	if err != nil {
		return nil, err
	}
	var payload artistsEnvelope
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode Groovarr response: %w", err)
	}
	return &metadata.SimilarArtistsResponse{
		Artists: mapArtists(payload.Artists.Results),
	}, nil
}

func (p *plugin) postJSON(cfg config, path string, payload interface{}) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	request := pdk.NewHTTPRequest(pdk.MethodPost, cfg.GroovarrURL+path)
	request.SetBody(body)
	request.SetHeader("Content-Type", "application/json")
	response := request.Send()
	if response.Status() < 200 || response.Status() >= 300 {
		return nil, fmt.Errorf("Groovarr similarity returned %d: %s", response.Status(), string(response.Body()))
	}
	return response.Body(), nil
}

func loadConfig() config {
	cfg := config{
		GroovarrURL:       defaultGroovarrURL,
		Provider:          defaultProvider,
		ExcludeRecentDays: defaultRecentDays,
	}
	if value, ok := pdk.GetConfig("groovarr_url"); ok && strings.TrimSpace(value) != "" {
		cfg.GroovarrURL = strings.TrimRight(value, "/")
	}
	if value, ok := pdk.GetConfig("provider"); ok && strings.TrimSpace(value) != "" {
		cfg.Provider = strings.ToLower(value)
	}
	if value, ok := pdk.GetConfig("exclude_recent_days"); ok && strings.TrimSpace(value) != "" {
		if parsed, err := parsePositiveInt(value); err == nil {
			cfg.ExcludeRecentDays = parsed
		}
	}
	if value, ok := pdk.GetConfig("exclude_seed_artist"); ok && strings.TrimSpace(value) != "" {
		cfg.ExcludeSeedArtist = parseBool(value)
	}
	return cfg
}

func requestCount(value int32, fallback int) int {
	if value > 0 {
		return int(value)
	}
	return fallback
}

func mapTracks(items []similarTrackEntry) []metadata.SongRef {
	out := make([]metadata.SongRef, 0, len(items))
	for _, item := range items {
		out = append(out, metadata.SongRef{
			ID:     strings.TrimSpace(item.ID),
			Name:   item.Title,
			Artist: item.ArtistName,
		})
	}
	return out
}

func mapArtists(items []similarArtistEntry) []metadata.ArtistRef {
	out := make([]metadata.ArtistRef, 0, len(items))
	for _, item := range items {
		out = append(out, metadata.ArtistRef{
			ID:   strings.TrimSpace(item.ID),
			Name: item.Name,
		})
	}
	return out
}

func parsePositiveInt(value string) (int, error) {
	n := 0
	for _, r := range strings.TrimSpace(value) {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid integer")
		}
		n = (n * 10) + int(r-'0')
	}
	return n, nil
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
