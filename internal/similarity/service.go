package similarity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"groovarr/internal/db"

	"github.com/pgvector/pgvector-go"
	"github.com/rs/zerolog/log"
)

const (
	ProviderLocal     = "local"
	ProviderAudioMuse = "audiomuse"
	ProviderHybrid    = "hybrid"

	scoreListeningAffinity = "listening_affinity"
	scoreDiversityPenalty  = "diversity_penalty"
	scoreModeAdjustment    = "mode_adjustment"
	scoreMoodAdjustment    = "mood_adjustment"
)

const (
	ModeFamiliar    = "familiar"
	ModeAdjacent    = "adjacent"
	ModeDeepCut     = "deep-cut"
	ModeSurprise    = "surprise"
	ModeLibraryOnly = "library-only"
)

type repository interface {
	GetTrackByID(ctx context.Context, id string) (*db.Track, error)
	GetTrackByArtistTitle(ctx context.Context, artistName, title string) (*db.Track, error)
	GetTracks(ctx context.Context, limit int, mostPlayed bool, filters map[string]interface{}) ([]db.Track, error)
	FindSimilarTracksByEmbedding(ctx context.Context, embedding pgvector.Vector, limit int, start, end *time.Time) ([]db.SimilarTrack, error)
	GetArtistByName(ctx context.Context, name string) (*db.Artist, error)
	FindSimilarArtists(ctx context.Context, embedding pgvector.Vector, limit int) ([]db.Artist, error)
	GetTasteProfileSummary(ctx context.Context) (*db.TasteProfileSummary, error)
	GetArtistTasteFeatures(ctx context.Context, artistNames []string) (map[string]db.TasteProfileArtistFeature, error)
	GetAlbumTasteFeatures(ctx context.Context, albumIDs []string) (map[string]db.TasteProfileAlbumFeature, error)
	GetListeningContext(ctx context.Context) (*db.ListeningContext, error)
	UpsertListeningContext(ctx context.Context, mode, mood string, expiresAt *time.Time, source string) (*db.ListeningContext, error)
	DeleteListeningContext(ctx context.Context) error
}

type Config struct {
	DefaultProvider                string
	AudioMuseBaseURL               string
	AudioMuseTracksPath            string
	AudioMuseArtistsPath           string
	AudioMuseHealthPath            string
	AudioMuseTimeout               time.Duration
	AudioMuseBootstrapEnabled      bool
	AudioMuseBootstrapRecentAlbums int
	AudioMuseBootstrapTopMoods     int
	HybridLocalWeight              float64
	HybridAudioWeight              float64
}

type Service struct {
	repo               repository
	audioMuse          *audioMuseClient
	defaultProvider    string
	hybridLocalWeight  float64
	hybridAudioWeight  float64
	audioMuseBootstrap audioMuseBootstrapConfig
	bootstrapMu        sync.RWMutex
	bootstrapState     bootstrapState
	bootstrapOnce      sync.Once
	now                func() time.Time
}

type TrackRequest struct {
	SeedTrackID         string `json:"seedTrackId"`
	SeedTrackTitle      string `json:"seedTrackTitle"`
	SeedArtistName      string `json:"seedArtistName"`
	Provider            string `json:"provider"`
	Limit               int    `json:"limit"`
	ExcludeRecentDays   int    `json:"excludeRecentDays"`
	ExcludeSeedArtist   bool   `json:"excludeSeedArtist"`
	Mode                string `json:"mode,omitempty"`
	Mood                string `json:"mood,omitempty"`
	IgnoreStoredContext bool   `json:"ignoreStoredContext,omitempty"`
}

type ArtistRequest struct {
	SeedArtistName      string `json:"seedArtistName"`
	Provider            string `json:"provider"`
	Limit               int    `json:"limit"`
	Mode                string `json:"mode,omitempty"`
	Mood                string `json:"mood,omitempty"`
	IgnoreStoredContext bool   `json:"ignoreStoredContext,omitempty"`
}

type ArtistSongsRequest struct {
	SeedArtistName      string `json:"seedArtistName"`
	Provider            string `json:"provider"`
	Limit               int    `json:"limit"`
	ExcludeRecentDays   int    `json:"excludeRecentDays"`
	ExcludeSeedArtist   bool   `json:"excludeSeedArtist"`
	Mode                string `json:"mode,omitempty"`
	Mood                string `json:"mood,omitempty"`
	IgnoreStoredContext bool   `json:"ignoreStoredContext,omitempty"`
}

type TrackSeed struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	ArtistName string `json:"artistName"`
}

type ArtistSeed struct {
	Name string `json:"name"`
}

type TrackResult struct {
	ID           string             `json:"id,omitempty"`
	AlbumID      string             `json:"albumId,omitempty"`
	Title        string             `json:"title"`
	ArtistName   string             `json:"artistName"`
	Rating       int                `json:"rating,omitempty"`
	PlayCount    int                `json:"playCount,omitempty"`
	LastPlayed   *time.Time         `json:"lastPlayed,omitempty"`
	Score        float64            `json:"score"`
	SourceScores map[string]float64 `json:"sourceScores,omitempty"`
	Sources      []string           `json:"sources"`
}

type ArtistResult struct {
	ID           string             `json:"id,omitempty"`
	Name         string             `json:"name"`
	Rating       int                `json:"rating,omitempty"`
	PlayCount    int                `json:"playCount,omitempty"`
	Score        float64            `json:"score"`
	SourceScores map[string]float64 `json:"sourceScores,omitempty"`
	Sources      []string           `json:"sources"`
}

type TrackResponse struct {
	Provider string        `json:"provider"`
	Seed     TrackSeed     `json:"seed"`
	Results  []TrackResult `json:"results"`
}

type ArtistResponse struct {
	Provider string         `json:"provider"`
	Seed     ArtistSeed     `json:"seed"`
	Results  []ArtistResult `json:"results"`
}

type ArtistSongsResponse struct {
	Provider string        `json:"provider"`
	Seed     ArtistSeed    `json:"seed"`
	Results  []TrackResult `json:"results"`
}

type ListeningContext struct {
	Mode      string     `json:"mode"`
	Mood      string     `json:"mood,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	UpdatedAt time.Time  `json:"updatedAt"`
	Source    string     `json:"source,omitempty"`
}

type Health struct {
	DefaultProvider         string   `json:"defaultProvider"`
	AudioMuseConfigured     bool     `json:"audioMuseConfigured"`
	AudioMuseReachable      bool     `json:"audioMuseReachable"`
	AudioMuseError          string   `json:"audioMuseError,omitempty"`
	AudioMuseLibraryState   string   `json:"audioMuseLibraryState,omitempty"`
	AudioMuseBootstrap      string   `json:"audioMuseBootstrap,omitempty"`
	AudioMuseBootstrapError string   `json:"audioMuseBootstrapError,omitempty"`
	AvailableProviders      []string `json:"availableProviders"`
	PreferredTrackSource    string   `json:"preferredTrackSource"`
}

type audioMuseClient struct {
	baseURL     string
	tracksPath  string
	artistsPath string
	healthPath  string
	client      *http.Client
}

type audioMuseBootstrapConfig struct {
	enabled      bool
	recentAlbums int
	topMoods     int
}

type bootstrapState struct {
	status       string
	libraryState string
	lastError    string
	lastTask     string
}

type trackRerankContext struct {
	summary        *db.TasteProfileSummary
	artistFeatures map[string]db.TasteProfileArtistFeature
	albumFeatures  map[string]db.TasteProfileAlbumFeature
}

type artistRerankContext struct {
	summary        *db.TasteProfileSummary
	artistFeatures map[string]db.TasteProfileArtistFeature
}

type resolvedListeningContext struct {
	Mode      string
	Mood      string
	ExpiresAt *time.Time
	UpdatedAt time.Time
	Source    string
}

type moodProfile struct {
	familiarityBias float64
	noveltyBias     float64
	diversityBias   float64
}

type audioMuseTaskSummary struct {
	Status   string                 `json:"status"`
	TaskID   *string                `json:"task_id"`
	TaskType *string                `json:"task_type"`
	Details  map[string]interface{} `json:"details"`
}

const (
	audioMuseLibraryStateUnknown       = "unknown"
	audioMuseLibraryStateUninitialized = "uninitialized"
	audioMuseLibraryStateProcessing    = "processing"
	audioMuseLibraryStateReady         = "ready"
	audioMuseLibraryStateFailed        = "failed"

	audioMuseBootstrapDisabled       = "disabled"
	audioMuseBootstrapPending        = "pending"
	audioMuseBootstrapChecking       = "checking"
	audioMuseBootstrapAnalysisQueued = "analysis_queued"
	audioMuseBootstrapAlreadyRunning = "analysis_in_progress"
	audioMuseBootstrapReady          = "ready"
	audioMuseBootstrapFailed         = "failed"
	audioMuseBootstrapNotNeeded      = "not_needed"
)

func ConfigFromEnv() Config {
	defaultProvider := strings.ToLower(strings.TrimSpace(os.Getenv("SIMILARITY_DEFAULT_PROVIDER")))
	if defaultProvider == "" {
		defaultProvider = ProviderHybrid
	}
	timeoutSeconds := envInt("AUDIOMUSE_TIMEOUT_SECONDS", 8)
	if timeoutSeconds < 1 {
		timeoutSeconds = 8
	}
	return Config{
		DefaultProvider:                defaultProvider,
		AudioMuseBaseURL:               strings.TrimRight(strings.TrimSpace(os.Getenv("AUDIOMUSE_URL")), "/"),
		AudioMuseTracksPath:            defaultEnv("AUDIOMUSE_TRACKS_PATH", "/similarity"),
		AudioMuseArtistsPath:           defaultEnv("AUDIOMUSE_ARTISTS_PATH", "/similarity/artists"),
		AudioMuseHealthPath:            defaultEnv("AUDIOMUSE_HEALTH_PATH", "/health"),
		AudioMuseTimeout:               time.Duration(timeoutSeconds) * time.Second,
		AudioMuseBootstrapEnabled:      envBool("AUDIOMUSE_BOOTSTRAP_ENABLED", true),
		AudioMuseBootstrapRecentAlbums: envInt("AUDIOMUSE_BOOTSTRAP_RECENT_ALBUMS", 0),
		AudioMuseBootstrapTopMoods:     envInt("AUDIOMUSE_BOOTSTRAP_TOP_N_MOODS", 5),
		HybridLocalWeight:              envFloat("SIMILARITY_HYBRID_LOCAL_WEIGHT", 0.45),
		HybridAudioWeight:              envFloat("SIMILARITY_HYBRID_AUDIOMUSE_WEIGHT", 0.55),
	}
}

func NewService(repo repository, cfg Config) *Service {
	service := &Service{
		repo:              repo,
		defaultProvider:   normalizeProvider(cfg.DefaultProvider),
		hybridLocalWeight: normalizeWeight(cfg.HybridLocalWeight, 0.45),
		hybridAudioWeight: normalizeWeight(cfg.HybridAudioWeight, 0.55),
		audioMuseBootstrap: audioMuseBootstrapConfig{
			enabled:      cfg.AudioMuseBootstrapEnabled,
			recentAlbums: cfg.AudioMuseBootstrapRecentAlbums,
			topMoods:     cfg.AudioMuseBootstrapTopMoods,
		},
		bootstrapState: bootstrapState{
			status:       audioMuseBootstrapDisabled,
			libraryState: audioMuseLibraryStateUnknown,
		},
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
	if strings.TrimSpace(cfg.AudioMuseBaseURL) != "" {
		service.audioMuse = &audioMuseClient{
			baseURL:     cfg.AudioMuseBaseURL,
			tracksPath:  ensureLeadingSlash(cfg.AudioMuseTracksPath),
			artistsPath: ensureLeadingSlash(cfg.AudioMuseArtistsPath),
			healthPath:  ensureLeadingSlash(cfg.AudioMuseHealthPath),
			client:      &http.Client{Timeout: cfg.AudioMuseTimeout},
		}
		if service.audioMuseBootstrap.enabled {
			service.bootstrapState.status = audioMuseBootstrapPending
			service.startAudioMuseBootstrap()
		}
	}
	return service
}

func (s *Service) SimilarTracks(ctx context.Context, req TrackRequest) (TrackResponse, error) {
	seed, err := s.resolveTrackSeed(ctx, req)
	if err != nil {
		return TrackResponse{}, err
	}
	listeningContext, err := s.resolveListeningContext(ctx, req.Mode, req.Mood, defaultTrackMode(), req.IgnoreStoredContext)
	if err != nil {
		return TrackResponse{}, err
	}
	provider := s.resolveProvider(req.Provider)
	results, providerUsed, err := s.fetchTrackResults(ctx, seed, req, provider)
	if err != nil {
		return TrackResponse{}, err
	}
	results = s.rerankTrackCandidates(ctx, seed.ArtistName, results, listeningContext)
	return TrackResponse{
		Provider: providerUsed,
		Seed: TrackSeed{
			ID:         seed.ID,
			Title:      seed.Title,
			ArtistName: seed.ArtistName,
		},
		Results: results,
	}, nil
}

func (s *Service) SimilarArtists(ctx context.Context, req ArtistRequest) (ArtistResponse, error) {
	seedName := strings.TrimSpace(req.SeedArtistName)
	if seedName == "" {
		return ArtistResponse{}, fmt.Errorf("seedArtistName is required")
	}
	listeningContext, err := s.resolveListeningContext(ctx, req.Mode, req.Mood, defaultArtistMode(), req.IgnoreStoredContext)
	if err != nil {
		return ArtistResponse{}, err
	}
	provider := s.resolveProvider(req.Provider)
	results, providerUsed, err := s.fetchArtistResults(ctx, seedName, clampLimit(req.Limit, 5, 50), provider)
	if err != nil {
		return ArtistResponse{}, err
	}
	results = s.rerankArtistCandidates(ctx, seedName, results, listeningContext)
	return ArtistResponse{
		Provider: providerUsed,
		Seed:     ArtistSeed{Name: seedName},
		Results:  results,
	}, nil
}

func (s *Service) SimilarSongsByArtist(ctx context.Context, req ArtistSongsRequest) (ArtistSongsResponse, error) {
	seedName := strings.TrimSpace(req.SeedArtistName)
	if seedName == "" {
		return ArtistSongsResponse{}, fmt.Errorf("seedArtistName is required")
	}
	listeningContext, err := s.resolveListeningContext(ctx, req.Mode, req.Mood, defaultArtistSongsMode(), req.IgnoreStoredContext)
	if err != nil {
		return ArtistSongsResponse{}, err
	}
	provider := s.resolveProvider(req.Provider)
	results, providerUsed, err := s.fetchTracksByArtist(ctx, seedName, req, provider)
	if err != nil {
		return ArtistSongsResponse{}, err
	}
	results = s.rerankTrackCandidates(ctx, seedName, results, listeningContext)
	return ArtistSongsResponse{
		Provider: providerUsed,
		Seed:     ArtistSeed{Name: seedName},
		Results:  results,
	}, nil
}

func (s *Service) GetListeningContext(ctx context.Context) (*ListeningContext, error) {
	item, err := s.repo.GetListeningContext(ctx)
	if err != nil || item == nil {
		return nil, err
	}
	return &ListeningContext{
		Mode:      normalizeMode(item.Mode),
		Mood:      strings.TrimSpace(item.Mood),
		ExpiresAt: item.ExpiresAt,
		UpdatedAt: item.UpdatedAt,
		Source:    strings.TrimSpace(item.Source),
	}, nil
}

func (s *Service) SetListeningContext(ctx context.Context, mode, mood string, ttl time.Duration, source string) (*ListeningContext, error) {
	mode = normalizeMode(mode)
	if mode == "" {
		return nil, fmt.Errorf("mode is required")
	}
	var expiresAt *time.Time
	if ttl > 0 {
		t := s.now().Add(ttl)
		expiresAt = &t
	}
	item, err := s.repo.UpsertListeningContext(ctx, mode, strings.TrimSpace(mood), expiresAt, strings.TrimSpace(source))
	if err != nil {
		return nil, err
	}
	return &ListeningContext{
		Mode:      item.Mode,
		Mood:      item.Mood,
		ExpiresAt: item.ExpiresAt,
		UpdatedAt: item.UpdatedAt,
		Source:    item.Source,
	}, nil
}

func (s *Service) DeleteListeningContext(ctx context.Context) error {
	return s.repo.DeleteListeningContext(ctx)
}

func (s *Service) Health(ctx context.Context) Health {
	health := Health{
		DefaultProvider:      s.defaultProvider,
		AudioMuseConfigured:  s.audioMuse != nil,
		PreferredTrackSource: s.resolveProvider(""),
	}
	health.AvailableProviders = []string{ProviderLocal}
	state := s.getBootstrapState()
	health.AudioMuseBootstrap = state.status
	health.AudioMuseLibraryState = state.libraryState
	health.AudioMuseBootstrapError = state.lastError
	if s.audioMuse != nil {
		health.AvailableProviders = append(health.AvailableProviders, ProviderAudioMuse, ProviderHybrid)
		reachable, err := s.audioMuse.Health(ctx)
		health.AudioMuseReachable = reachable
		if err != nil {
			health.AudioMuseError = err.Error()
			return health
		}
		if libraryState, _, inspectErr := s.audioMuse.LibraryState(ctx); inspectErr == nil {
			health.AudioMuseLibraryState = libraryState
		} else if health.AudioMuseBootstrapError == "" {
			health.AudioMuseBootstrapError = inspectErr.Error()
		}
	}
	return health
}

func (s *Service) startAudioMuseBootstrap() {
	if s.audioMuse == nil || !s.audioMuseBootstrap.enabled {
		return
	}
	s.bootstrapOnce.Do(func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			s.ensureAudioMuseBootstrap(ctx)
		}()
	})
}

func (s *Service) ensureAudioMuseBootstrap(ctx context.Context) {
	s.setBootstrapState(audioMuseBootstrapChecking, audioMuseLibraryStateUnknown, "", "")
	reachable, err := s.audioMuse.Health(ctx)
	if err != nil || !reachable {
		if err == nil {
			err = fmt.Errorf("AudioMuse is not reachable")
		}
		s.setBootstrapState(audioMuseBootstrapFailed, audioMuseLibraryStateUnknown, err.Error(), "")
		log.Warn().Err(err).Msg("AudioMuse bootstrap check failed")
		return
	}

	libraryState, lastTask, err := s.audioMuse.LibraryState(ctx)
	if err != nil {
		s.setBootstrapState(audioMuseBootstrapFailed, audioMuseLibraryStateUnknown, err.Error(), "")
		log.Warn().Err(err).Msg("AudioMuse bootstrap state inspection failed")
		return
	}

	switch libraryState {
	case audioMuseLibraryStateUninitialized:
		taskID, startErr := s.audioMuse.StartAnalysis(ctx, s.audioMuseBootstrap.recentAlbums, s.audioMuseBootstrap.topMoods)
		if startErr != nil {
			s.setBootstrapState(audioMuseBootstrapFailed, libraryState, startErr.Error(), lastTask)
			log.Warn().Err(startErr).Msg("Failed to enqueue AudioMuse bootstrap analysis")
			return
		}
		s.setBootstrapState(audioMuseBootstrapAnalysisQueued, audioMuseLibraryStateProcessing, "", taskID)
		log.Info().
			Str("task_id", taskID).
			Int("num_recent_albums", s.audioMuseBootstrap.recentAlbums).
			Int("top_n_moods", s.audioMuseBootstrap.topMoods).
			Msg("Enqueued initial AudioMuse analysis")
	case audioMuseLibraryStateProcessing:
		s.setBootstrapState(audioMuseBootstrapAlreadyRunning, libraryState, "", lastTask)
	case audioMuseLibraryStateReady:
		s.setBootstrapState(audioMuseBootstrapReady, libraryState, "", lastTask)
	case audioMuseLibraryStateFailed:
		s.setBootstrapState(audioMuseBootstrapFailed, libraryState, "", lastTask)
	default:
		s.setBootstrapState(audioMuseBootstrapNotNeeded, libraryState, "", lastTask)
	}
}

func (s *Service) setBootstrapState(status, libraryState, lastError, lastTask string) {
	s.bootstrapMu.Lock()
	defer s.bootstrapMu.Unlock()
	s.bootstrapState.status = status
	s.bootstrapState.libraryState = libraryState
	s.bootstrapState.lastError = strings.TrimSpace(lastError)
	s.bootstrapState.lastTask = strings.TrimSpace(lastTask)
}

func (s *Service) getBootstrapState() bootstrapState {
	s.bootstrapMu.RLock()
	defer s.bootstrapMu.RUnlock()
	return s.bootstrapState
}

func (s *Service) resolveTrackSeed(ctx context.Context, req TrackRequest) (*db.Track, error) {
	if strings.TrimSpace(req.SeedTrackID) != "" {
		track, err := s.repo.GetTrackByID(ctx, strings.TrimSpace(req.SeedTrackID))
		if err != nil {
			return nil, err
		}
		if track != nil {
			return track, nil
		}
	}
	title := strings.TrimSpace(req.SeedTrackTitle)
	artist := strings.TrimSpace(req.SeedArtistName)
	if title == "" || artist == "" {
		return nil, fmt.Errorf("seedTrackId or both seedTrackTitle and seedArtistName are required")
	}
	track, err := s.repo.GetTrackByArtistTitle(ctx, artist, title)
	if err != nil {
		return nil, err
	}
	if track == nil {
		return nil, fmt.Errorf("seed track %q by %q was not found", title, artist)
	}
	return track, nil
}

func (s *Service) fetchTrackResults(ctx context.Context, seed *db.Track, req TrackRequest, provider string) ([]TrackResult, string, error) {
	limit := clampLimit(req.Limit, 10, 100)
	switch provider {
	case ProviderLocal:
		results, err := s.localSimilarTracks(ctx, seed, req, limit)
		return results, ProviderLocal, err
	case ProviderAudioMuse:
		if s.audioMuse == nil {
			results, err := s.localSimilarTracks(ctx, seed, req, limit)
			return results, ProviderLocal, err
		}
		results, err := s.audioMuse.SimilarTracks(ctx, s.repo, seed, req, limit)
		if err != nil {
			results, fallbackErr := s.localSimilarTracks(ctx, seed, req, limit)
			if fallbackErr != nil {
				return nil, "", err
			}
			return results, ProviderLocal, nil
		}
		return results, ProviderAudioMuse, nil
	default:
		local, localErr := s.localSimilarTracks(ctx, seed, req, limit*3)
		audio, audioErr := s.audioSimilarTracks(ctx, seed, req, limit*3)
		switch {
		case localErr == nil && audioErr == nil:
			return s.mergeTrackResults(seed, req, local, audio, limit), ProviderHybrid, nil
		case localErr == nil:
			return trimTrackResults(local, limit), ProviderLocal, nil
		case audioErr == nil:
			return trimTrackResults(audio, limit), ProviderAudioMuse, nil
		default:
			return nil, "", localErr
		}
	}
}

func (s *Service) fetchArtistResults(ctx context.Context, seedName string, limit int, provider string) ([]ArtistResult, string, error) {
	switch provider {
	case ProviderLocal:
		results, err := s.localSimilarArtists(ctx, seedName, limit)
		return results, ProviderLocal, err
	case ProviderAudioMuse:
		if s.audioMuse == nil {
			results, err := s.localSimilarArtists(ctx, seedName, limit)
			return results, ProviderLocal, err
		}
		results, err := s.audioMuse.SimilarArtists(ctx, s.repo, seedName, limit)
		if err != nil {
			results, fallbackErr := s.localSimilarArtists(ctx, seedName, limit)
			if fallbackErr != nil {
				return nil, "", err
			}
			return results, ProviderLocal, nil
		}
		return results, ProviderAudioMuse, nil
	default:
		local, localErr := s.localSimilarArtists(ctx, seedName, limit*3)
		audio, audioErr := s.audioSimilarArtists(ctx, seedName, limit*3)
		switch {
		case localErr == nil && audioErr == nil:
			return s.mergeArtistResults(seedName, local, audio, limit), ProviderHybrid, nil
		case localErr == nil:
			return trimArtistResults(local, limit), ProviderLocal, nil
		case audioErr == nil:
			return trimArtistResults(audio, limit), ProviderAudioMuse, nil
		default:
			return nil, "", localErr
		}
	}
}

func (s *Service) fetchTracksByArtist(ctx context.Context, seedName string, req ArtistSongsRequest, provider string) ([]TrackResult, string, error) {
	limit := clampLimit(req.Limit, 10, 100)
	switch provider {
	case ProviderLocal:
		results, err := s.localTracksByArtist(ctx, seedName, req, limit)
		return results, ProviderLocal, err
	case ProviderAudioMuse:
		if s.audioMuse == nil {
			results, err := s.localTracksByArtist(ctx, seedName, req, limit)
			return results, ProviderLocal, err
		}
		results, err := s.audioTracksByArtist(ctx, seedName, req, limit)
		if err != nil {
			results, fallbackErr := s.localTracksByArtist(ctx, seedName, req, limit)
			if fallbackErr != nil {
				return nil, "", err
			}
			return results, ProviderLocal, nil
		}
		return results, ProviderAudioMuse, nil
	default:
		local, localErr := s.localTracksByArtist(ctx, seedName, req, limit*3)
		audio, audioErr := s.audioTracksByArtist(ctx, seedName, req, limit*3)
		switch {
		case localErr == nil && audioErr == nil:
			return s.mergeTrackResultsForArtist(seedName, req, local, audio, limit), ProviderHybrid, nil
		case localErr == nil:
			return trimTrackResults(local, limit), ProviderLocal, nil
		case audioErr == nil:
			return trimTrackResults(audio, limit), ProviderAudioMuse, nil
		default:
			return nil, "", localErr
		}
	}
}

func (s *Service) localSimilarTracks(ctx context.Context, seed *db.Track, req TrackRequest, limit int) ([]TrackResult, error) {
	if len(seed.Embedding.Slice()) == 0 {
		return nil, fmt.Errorf("seed track %q by %q has no embedding", seed.Title, seed.ArtistName)
	}
	matches, err := s.repo.FindSimilarTracksByEmbedding(ctx, seed.Embedding, clampLimit(limit, 10, 200), nil, nil)
	if err != nil {
		return nil, err
	}
	results := make([]TrackResult, 0, len(matches))
	cutoff := time.Time{}
	if req.ExcludeRecentDays > 0 {
		cutoff = s.now().AddDate(0, 0, -req.ExcludeRecentDays)
	}
	seedArtistKey := normalizeKey(seed.ArtistName)
	for _, match := range matches {
		if match.ID == seed.ID {
			continue
		}
		if req.ExcludeSeedArtist && normalizeKey(match.ArtistName) == seedArtistKey {
			continue
		}
		if !cutoff.IsZero() && match.LastPlayed != nil && match.LastPlayed.After(cutoff) {
			continue
		}
		score := clampScore(match.Similarity)
		results = append(results, TrackResult{
			ID:         match.ID,
			AlbumID:    match.AlbumID,
			Title:      match.Title,
			ArtistName: match.ArtistName,
			Rating:     match.Rating,
			PlayCount:  match.PlayCount,
			LastPlayed: match.LastPlayed,
			Score:      score,
			SourceScores: map[string]float64{
				ProviderLocal: score,
			},
			Sources: []string{ProviderLocal},
		})
	}
	return trimTrackResults(results, limit), nil
}

func (s *Service) audioSimilarTracks(ctx context.Context, seed *db.Track, req TrackRequest, limit int) ([]TrackResult, error) {
	if s.audioMuse == nil {
		return nil, fmt.Errorf("AudioMuse is not configured")
	}
	return s.audioMuse.SimilarTracks(ctx, s.repo, seed, req, limit)
}

func (s *Service) localSimilarArtists(ctx context.Context, seedName string, limit int) ([]ArtistResult, error) {
	artist, err := s.repo.GetArtistByName(ctx, seedName)
	if err != nil {
		return nil, err
	}
	if artist == nil {
		return nil, fmt.Errorf("seed artist %q was not found", seedName)
	}
	if len(artist.Embedding.Slice()) == 0 {
		return nil, fmt.Errorf("seed artist %q has no embedding", seedName)
	}
	items, err := s.repo.FindSimilarArtists(ctx, artist.Embedding, clampLimit(limit, 5, 100))
	if err != nil {
		return nil, err
	}
	results := make([]ArtistResult, 0, len(items))
	seedKey := normalizeKey(seedName)
	for idx, item := range items {
		if normalizeKey(item.Name) == seedKey {
			continue
		}
		score := rankScore(idx, len(items))
		results = append(results, ArtistResult{
			ID:        item.ID,
			Name:      item.Name,
			Rating:    item.Rating,
			PlayCount: item.PlayCount,
			Score:     score,
			SourceScores: map[string]float64{
				ProviderLocal: score,
			},
			Sources: []string{ProviderLocal},
		})
	}
	return trimArtistResults(results, limit), nil
}

func (s *Service) audioSimilarArtists(ctx context.Context, seedName string, limit int) ([]ArtistResult, error) {
	if s.audioMuse == nil {
		return nil, fmt.Errorf("AudioMuse is not configured")
	}
	return s.audioMuse.SimilarArtists(ctx, s.repo, seedName, limit)
}

func (s *Service) localTracksByArtist(ctx context.Context, seedName string, req ArtistSongsRequest, limit int) ([]TrackResult, error) {
	artists, err := s.localSimilarArtists(ctx, seedName, clampLimit(limit, 10, 100))
	if err != nil {
		return nil, err
	}
	return s.expandArtistsToTracks(ctx, artists, seedName, req.ExcludeRecentDays, req.ExcludeSeedArtist, limit, ProviderLocal)
}

func (s *Service) audioTracksByArtist(ctx context.Context, seedName string, req ArtistSongsRequest, limit int) ([]TrackResult, error) {
	if s.audioMuse == nil {
		return nil, fmt.Errorf("AudioMuse is not configured")
	}
	artists, err := s.audioSimilarArtists(ctx, seedName, clampLimit(limit, 10, 100))
	if err != nil {
		return nil, err
	}
	return s.expandArtistsToTracks(ctx, artists, seedName, req.ExcludeRecentDays, req.ExcludeSeedArtist, limit, ProviderAudioMuse)
}

func (s *Service) mergeTrackResults(seed *db.Track, req TrackRequest, local []TrackResult, audio []TrackResult, limit int) []TrackResult {
	merged := make(map[string]TrackResult, len(local)+len(audio))
	for _, result := range local {
		key := trackResultKey(result)
		merged[key] = result
	}
	for _, result := range audio {
		key := trackResultKey(result)
		existing, ok := merged[key]
		if !ok {
			merged[key] = result
			continue
		}
		if existing.SourceScores == nil {
			existing.SourceScores = map[string]float64{}
		}
		for name, score := range result.SourceScores {
			existing.SourceScores[name] = score
		}
		existing.Sources = mergeSources(existing.Sources, result.Sources)
		if existing.ID == "" {
			existing.ID = result.ID
		}
		if existing.AlbumID == "" {
			existing.AlbumID = result.AlbumID
		}
		if existing.Title == "" {
			existing.Title = result.Title
		}
		if existing.ArtistName == "" {
			existing.ArtistName = result.ArtistName
		}
		if existing.LastPlayed == nil {
			existing.LastPlayed = result.LastPlayed
		}
		if existing.Rating == 0 {
			existing.Rating = result.Rating
		}
		if existing.PlayCount == 0 {
			existing.PlayCount = result.PlayCount
		}
		merged[key] = existing
	}
	results := make([]TrackResult, 0, len(merged))
	seedArtistKey := normalizeKey(seed.ArtistName)
	for _, item := range merged {
		localScore, hasLocal := item.SourceScores[ProviderLocal]
		audioScore, hasAudio := item.SourceScores[ProviderAudioMuse]
		item.Score = weightedScore(hasLocal, localScore, s.hybridLocalWeight, hasAudio, audioScore, s.hybridAudioWeight)
		if req.ExcludeSeedArtist && normalizeKey(item.ArtistName) == seedArtistKey {
			continue
		}
		if normalizeKey(item.ArtistName) == seedArtistKey {
			item.Score *= 0.92
		}
		results = append(results, item)
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			if results[i].ArtistName == results[j].ArtistName {
				return results[i].Title < results[j].Title
			}
			return results[i].ArtistName < results[j].ArtistName
		}
		return results[i].Score > results[j].Score
	})
	return trimTrackResults(results, limit)
}

func (s *Service) mergeTrackResultsForArtist(seedArtist string, req ArtistSongsRequest, local []TrackResult, audio []TrackResult, limit int) []TrackResult {
	merged := make(map[string]TrackResult, len(local)+len(audio))
	for _, result := range local {
		merged[trackResultKey(result)] = result
	}
	for _, result := range audio {
		key := trackResultKey(result)
		existing, ok := merged[key]
		if !ok {
			merged[key] = result
			continue
		}
		if existing.SourceScores == nil {
			existing.SourceScores = map[string]float64{}
		}
		for name, score := range result.SourceScores {
			existing.SourceScores[name] = score
		}
		existing.Sources = mergeSources(existing.Sources, result.Sources)
		if existing.ID == "" {
			existing.ID = result.ID
		}
		if existing.AlbumID == "" {
			existing.AlbumID = result.AlbumID
		}
		if existing.LastPlayed == nil {
			existing.LastPlayed = result.LastPlayed
		}
		if existing.Rating == 0 {
			existing.Rating = result.Rating
		}
		if existing.PlayCount == 0 {
			existing.PlayCount = result.PlayCount
		}
		merged[key] = existing
	}
	seedKey := normalizeKey(seedArtist)
	results := make([]TrackResult, 0, len(merged))
	for _, item := range merged {
		localScore, hasLocal := item.SourceScores[ProviderLocal]
		audioScore, hasAudio := item.SourceScores[ProviderAudioMuse]
		item.Score = weightedScore(hasLocal, localScore, s.hybridLocalWeight, hasAudio, audioScore, s.hybridAudioWeight)
		if req.ExcludeSeedArtist && normalizeKey(item.ArtistName) == seedKey {
			continue
		}
		results = append(results, item)
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			if results[i].ArtistName == results[j].ArtistName {
				return results[i].Title < results[j].Title
			}
			return results[i].ArtistName < results[j].ArtistName
		}
		return results[i].Score > results[j].Score
	})
	return trimTrackResults(results, limit)
}

func (s *Service) expandArtistsToTracks(ctx context.Context, artists []ArtistResult, seedArtist string, excludeRecentDays int, excludeSeedArtist bool, limit int, source string) ([]TrackResult, error) {
	candidateArtists := clampLimit(limit, 10, 100)
	if candidateArtists < len(artists) {
		artists = artists[:candidateArtists]
	}
	perArtist := 3
	if limit <= 5 {
		perArtist = 2
	}
	cutoff := time.Time{}
	if excludeRecentDays > 0 {
		cutoff = s.now().AddDate(0, 0, -excludeRecentDays)
	}
	seedKey := normalizeKey(seedArtist)
	results := make([]TrackResult, 0, limit*perArtist)
	seen := make(map[string]struct{}, limit*perArtist)
	for _, artist := range artists {
		if excludeSeedArtist && normalizeKey(artist.Name) == seedKey {
			continue
		}
		tracks, err := s.repo.GetTracks(ctx, perArtist, true, map[string]interface{}{
			"artistName": artist.Name,
			"onlyPlayed": true,
		})
		if err != nil {
			return nil, err
		}
		for idx, track := range tracks {
			if !cutoff.IsZero() && track.LastPlayed != nil && track.LastPlayed.After(cutoff) {
				continue
			}
			key := "id:" + track.ID
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			score := artist.Score * (1 - (0.07 * float64(idx)))
			result := TrackResult{
				ID:         track.ID,
				AlbumID:    track.AlbumID,
				Title:      track.Title,
				ArtistName: track.ArtistName,
				Rating:     track.Rating,
				PlayCount:  track.PlayCount,
				LastPlayed: track.LastPlayed,
				Score:      clampScore(score),
				SourceScores: map[string]float64{
					source: clampScore(score),
				},
				Sources: []string{source},
			}
			results = append(results, result)
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			if results[i].ArtistName == results[j].ArtistName {
				return results[i].Title < results[j].Title
			}
			return results[i].ArtistName < results[j].ArtistName
		}
		return results[i].Score > results[j].Score
	})
	return trimTrackResults(results, limit), nil
}

func (s *Service) mergeArtistResults(seedName string, local []ArtistResult, audio []ArtistResult, limit int) []ArtistResult {
	merged := make(map[string]ArtistResult, len(local)+len(audio))
	for _, item := range local {
		merged[artistResultKey(item)] = item
	}
	for _, item := range audio {
		key := artistResultKey(item)
		existing, ok := merged[key]
		if !ok {
			merged[key] = item
			continue
		}
		if existing.SourceScores == nil {
			existing.SourceScores = map[string]float64{}
		}
		for name, score := range item.SourceScores {
			existing.SourceScores[name] = score
		}
		existing.Sources = mergeSources(existing.Sources, item.Sources)
		if existing.ID == "" {
			existing.ID = item.ID
		}
		if existing.Name == "" {
			existing.Name = item.Name
		}
		if existing.Rating == 0 {
			existing.Rating = item.Rating
		}
		if existing.PlayCount == 0 {
			existing.PlayCount = item.PlayCount
		}
		merged[key] = existing
	}
	results := make([]ArtistResult, 0, len(merged))
	seedKey := normalizeKey(seedName)
	for _, item := range merged {
		if normalizeKey(item.Name) == seedKey {
			continue
		}
		localScore, hasLocal := item.SourceScores[ProviderLocal]
		audioScore, hasAudio := item.SourceScores[ProviderAudioMuse]
		item.Score = weightedScore(hasLocal, localScore, s.hybridLocalWeight, hasAudio, audioScore, s.hybridAudioWeight)
		results = append(results, item)
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Name < results[j].Name
		}
		return results[i].Score > results[j].Score
	})
	return trimArtistResults(results, limit)
}

func (s *Service) rerankTrackCandidates(ctx context.Context, seedArtist string, results []TrackResult, listeningContext resolvedListeningContext) []TrackResult {
	if len(results) == 0 {
		return results
	}
	rerankCtx, err := s.loadTrackRerankContext(ctx, results)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load taste-profile context for track reranking")
		return results
	}

	preliminary := make([]TrackResult, len(results))
	for i, item := range results {
		preliminary[i] = item
		if preliminary[i].SourceScores == nil {
			preliminary[i].SourceScores = map[string]float64{}
		}
		affinity := s.trackListeningAffinity(preliminary[i], rerankCtx)
		modeAdjustment := s.trackModeAdjustment(preliminary[i], rerankCtx, listeningContext)
		moodAdjustment := s.trackMoodAdjustment(preliminary[i], rerankCtx, listeningContext)
		preliminary[i].SourceScores[scoreListeningAffinity] = affinity
		preliminary[i].SourceScores[scoreModeAdjustment] = modeAdjustment
		preliminary[i].SourceScores[scoreMoodAdjustment] = moodAdjustment
		preliminary[i].Score = clampScore(
			s.trackBlendWeights(listeningContext).base*preliminary[i].Score +
				s.trackBlendWeights(listeningContext).affinity*affinity +
				modeAdjustment +
				moodAdjustment,
		)
	}
	sort.SliceStable(preliminary, func(i, j int) bool {
		if preliminary[i].Score == preliminary[j].Score {
			if preliminary[i].ArtistName == preliminary[j].ArtistName {
				return preliminary[i].Title < preliminary[j].Title
			}
			return preliminary[i].ArtistName < preliminary[j].ArtistName
		}
		return preliminary[i].Score > preliminary[j].Score
	})

	seedArtistKey := normalizeKey(seedArtist)
	artistSeen := make(map[string]int, len(preliminary))
	albumSeen := make(map[string]int, len(preliminary))
	out := make([]TrackResult, 0, len(preliminary))
	for _, item := range preliminary {
		penalty := trackDiversityPenalty(item, seedArtistKey, artistSeen, albumSeen, listeningContext)
		item.SourceScores[scoreDiversityPenalty] = penalty
		item.Score = clampScore(item.Score - penalty)
		out = append(out, item)
		artistSeen[normalizeKey(item.ArtistName)]++
		if albumID := strings.TrimSpace(item.AlbumID); albumID != "" {
			albumSeen[albumID]++
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			if out[i].ArtistName == out[j].ArtistName {
				return out[i].Title < out[j].Title
			}
			return out[i].ArtistName < out[j].ArtistName
		}
		return out[i].Score > out[j].Score
	})
	return out
}

func (s *Service) rerankArtistCandidates(ctx context.Context, seedArtist string, results []ArtistResult, listeningContext resolvedListeningContext) []ArtistResult {
	if len(results) == 0 {
		return results
	}
	rerankCtx, err := s.loadArtistRerankContext(ctx, results)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load taste-profile context for artist reranking")
		return results
	}

	seedKey := normalizeKey(seedArtist)
	out := make([]ArtistResult, 0, len(results))
	for _, item := range results {
		if normalizeKey(item.Name) == seedKey {
			continue
		}
		if item.SourceScores == nil {
			item.SourceScores = map[string]float64{}
		}
		affinity := s.artistListeningAffinity(item, rerankCtx)
		modeAdjustment := s.artistModeAdjustment(item, rerankCtx, listeningContext)
		moodAdjustment := s.artistMoodAdjustment(item, rerankCtx, listeningContext)
		item.SourceScores[scoreListeningAffinity] = affinity
		item.SourceScores[scoreModeAdjustment] = modeAdjustment
		item.SourceScores[scoreMoodAdjustment] = moodAdjustment
		item.Score = clampScore(
			s.artistBlendWeights(listeningContext).base*item.Score +
				s.artistBlendWeights(listeningContext).affinity*affinity +
				modeAdjustment +
				moodAdjustment,
		)
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Name < out[j].Name
		}
		return out[i].Score > out[j].Score
	})
	return out
}

func (s *Service) loadTrackRerankContext(ctx context.Context, results []TrackResult) (trackRerankContext, error) {
	summary, err := s.repo.GetTasteProfileSummary(ctx)
	if err != nil {
		return trackRerankContext{}, err
	}
	artistNames := make([]string, 0, len(results))
	albumIDs := make([]string, 0, len(results))
	for _, item := range results {
		artistNames = append(artistNames, item.ArtistName)
		if strings.TrimSpace(item.AlbumID) != "" {
			albumIDs = append(albumIDs, item.AlbumID)
		}
	}
	artistFeatures, err := s.repo.GetArtistTasteFeatures(ctx, artistNames)
	if err != nil {
		return trackRerankContext{}, err
	}
	albumFeatures, err := s.repo.GetAlbumTasteFeatures(ctx, albumIDs)
	if err != nil {
		return trackRerankContext{}, err
	}
	return trackRerankContext{
		summary:        summary,
		artistFeatures: artistFeatures,
		albumFeatures:  albumFeatures,
	}, nil
}

func (s *Service) loadArtistRerankContext(ctx context.Context, results []ArtistResult) (artistRerankContext, error) {
	summary, err := s.repo.GetTasteProfileSummary(ctx)
	if err != nil {
		return artistRerankContext{}, err
	}
	artistNames := make([]string, 0, len(results))
	for _, item := range results {
		artistNames = append(artistNames, item.Name)
	}
	artistFeatures, err := s.repo.GetArtistTasteFeatures(ctx, artistNames)
	if err != nil {
		return artistRerankContext{}, err
	}
	return artistRerankContext{
		summary:        summary,
		artistFeatures: artistFeatures,
	}, nil
}

func (s *Service) resolveListeningContext(ctx context.Context, explicitMode, explicitMood, defaultMode string, ignoreStored bool) (resolvedListeningContext, error) {
	active, err := s.repo.GetListeningContext(ctx)
	if err != nil {
		return resolvedListeningContext{}, err
	}

	resolved := resolvedListeningContext{
		Mode: defaultMode,
	}
	if active != nil && !ignoreStored {
		resolved.Mode = normalizeMode(active.Mode)
		if resolved.Mode == "" {
			resolved.Mode = defaultMode
		}
		resolved.Mood = strings.TrimSpace(active.Mood)
		resolved.ExpiresAt = active.ExpiresAt
		resolved.UpdatedAt = active.UpdatedAt
		resolved.Source = strings.TrimSpace(active.Source)
	}
	if mode := normalizeMode(explicitMode); mode != "" {
		resolved.Mode = mode
	}
	if mood := strings.TrimSpace(explicitMood); mood != "" {
		resolved.Mood = mood
	}
	if resolved.Mode == "" {
		resolved.Mode = defaultMode
	}
	return resolved, nil
}

func defaultTrackMode() string {
	return ModeAdjacent
}

func defaultArtistMode() string {
	return ModeAdjacent
}

func defaultArtistSongsMode() string {
	return ModeFamiliar
}

func normalizeMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ModeFamiliar:
		return ModeFamiliar
	case ModeAdjacent, "":
		return strings.TrimSpace(strings.ToLower(raw))
	case "deepcut", "deep_cut", "deep cut", ModeDeepCut:
		return ModeDeepCut
	case ModeSurprise:
		return ModeSurprise
	case "library_only", "library only", ModeLibraryOnly:
		return ModeLibraryOnly
	default:
		return ""
	}
}

func parseMoodProfile(raw string) moodProfile {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return moodProfile{}
	}
	profile := moodProfile{}
	switch {
	case strings.Contains(value, "night"), strings.Contains(value, "dark"), strings.Contains(value, "late"), strings.Contains(value, "moody"), strings.Contains(value, "melanch"), strings.Contains(value, "dream"):
		profile.noveltyBias += 0.015
		profile.diversityBias += 0.01
	case strings.Contains(value, "warm"), strings.Contains(value, "soft"), strings.Contains(value, "gentle"), strings.Contains(value, "calm"), strings.Contains(value, "cozy"):
		profile.familiarityBias += 0.02
		profile.diversityBias -= 0.01
	case strings.Contains(value, "ener"), strings.Contains(value, "drive"), strings.Contains(value, "bright"), strings.Contains(value, "lift"), strings.Contains(value, "focus"):
		profile.familiarityBias += 0.01
		profile.noveltyBias += 0.01
	case strings.Contains(value, "weird"), strings.Contains(value, "advent"), strings.Contains(value, "surpris"), strings.Contains(value, "wild"), strings.Contains(value, "explor"):
		profile.noveltyBias += 0.03
		profile.diversityBias += 0.02
	}
	return profile
}

type blendWeights struct {
	base     float64
	affinity float64
}

func (s *Service) trackBlendWeights(listeningContext resolvedListeningContext) blendWeights {
	switch listeningContext.Mode {
	case ModeFamiliar:
		return blendWeights{base: 0.70, affinity: 0.30}
	case ModeDeepCut:
		return blendWeights{base: 0.72, affinity: 0.12}
	case ModeSurprise:
		return blendWeights{base: 0.74, affinity: 0.06}
	case ModeLibraryOnly:
		return blendWeights{base: 0.76, affinity: 0.24}
	default:
		return blendWeights{base: 0.78, affinity: 0.22}
	}
}

func (s *Service) artistBlendWeights(listeningContext resolvedListeningContext) blendWeights {
	switch listeningContext.Mode {
	case ModeFamiliar:
		return blendWeights{base: 0.68, affinity: 0.32}
	case ModeDeepCut:
		return blendWeights{base: 0.70, affinity: 0.12}
	case ModeSurprise:
		return blendWeights{base: 0.72, affinity: 0.06}
	case ModeLibraryOnly:
		return blendWeights{base: 0.78, affinity: 0.22}
	default:
		return blendWeights{base: 0.80, affinity: 0.20}
	}
}

func (s *Service) trackModeAdjustment(result TrackResult, rerankCtx trackRerankContext, listeningContext resolvedListeningContext) float64 {
	artistFeature := rerankCtx.artistFeatures[normalizeKey(result.ArtistName)]
	albumFeature := rerankCtx.albumFeatures[strings.TrimSpace(result.AlbumID)]
	familiarity := clampScore(artistFeature.FamiliarityScore)
	overexposure := clampScore(albumFeature.OverexposureScore)
	playCount := clampScore(float64(result.PlayCount) / 25.0)

	switch listeningContext.Mode {
	case ModeFamiliar:
		return clampSignedScore(0.05*familiarity + 0.02*playCount - 0.02*overexposure)
	case ModeDeepCut:
		return clampSignedScore(0.07*(1-familiarity) + 0.04*(1-playCount) - 0.05*familiarity - 0.01*overexposure)
	case ModeSurprise:
		return clampSignedScore(0.10*(1-familiarity) + 0.05*(1-playCount) - 0.09*familiarity - 0.02*overexposure)
	default:
		return 0
	}
}

func (s *Service) artistModeAdjustment(result ArtistResult, rerankCtx artistRerankContext, listeningContext resolvedListeningContext) float64 {
	artistFeature := rerankCtx.artistFeatures[normalizeKey(result.Name)]
	familiarity := clampScore(artistFeature.FamiliarityScore)
	playCount := clampScore(float64(result.PlayCount) / 25.0)

	switch listeningContext.Mode {
	case ModeFamiliar:
		return clampSignedScore(0.05*familiarity + 0.02*playCount)
	case ModeDeepCut:
		return clampSignedScore(0.07*(1-familiarity) + 0.04*(1-playCount) - 0.05*familiarity)
	case ModeSurprise:
		return clampSignedScore(0.10*(1-familiarity) + 0.05*(1-playCount) - 0.09*familiarity)
	default:
		return 0
	}
}

func (s *Service) trackMoodAdjustment(result TrackResult, rerankCtx trackRerankContext, listeningContext resolvedListeningContext) float64 {
	profile := parseMoodProfile(listeningContext.Mood)
	if profile == (moodProfile{}) {
		return 0
	}
	artistFeature := rerankCtx.artistFeatures[normalizeKey(result.ArtistName)]
	albumFeature := rerankCtx.albumFeatures[strings.TrimSpace(result.AlbumID)]
	familiarity := clampScore(artistFeature.FamiliarityScore)
	overexposure := clampScore(albumFeature.OverexposureScore)
	return clampSignedScore(profile.familiarityBias*familiarity + profile.noveltyBias*(1-familiarity) + profile.diversityBias*(1-overexposure))
}

func (s *Service) artistMoodAdjustment(result ArtistResult, rerankCtx artistRerankContext, listeningContext resolvedListeningContext) float64 {
	profile := parseMoodProfile(listeningContext.Mood)
	if profile == (moodProfile{}) {
		return 0
	}
	artistFeature := rerankCtx.artistFeatures[normalizeKey(result.Name)]
	familiarity := clampScore(artistFeature.FamiliarityScore)
	return clampSignedScore(profile.familiarityBias*familiarity + profile.noveltyBias*(1-familiarity) + profile.diversityBias*(1-familiarity))
}

func (s *Service) trackListeningAffinity(result TrackResult, rerankCtx trackRerankContext) float64 {
	artistFeature := rerankCtx.artistFeatures[normalizeKey(result.ArtistName)]
	albumFeature := rerankCtx.albumFeatures[strings.TrimSpace(result.AlbumID)]

	familiarity := clampScore(artistFeature.FamiliarityScore)
	fatigue := clampScore(artistFeature.FatigueScore)
	overexposure := clampScore(albumFeature.OverexposureScore)
	rating := clampScore(float64(result.Rating) / 5.0)
	playCount := clampScore(float64(result.PlayCount) / 25.0)

	replayPreference := 0.0
	noveltyPreference := 0.0
	if rerankCtx.summary != nil {
		replayPreference = clampScore(rerankCtx.summary.ReplayAffinityScore * ((0.65 * familiarity) + (0.35 * playCount)))
		noveltyPreference = clampScore(rerankCtx.summary.NoveltyToleranceScore * (1 - familiarity))
	}

	return clampScore(
		(0.34 * familiarity) +
			(0.14 * rating) +
			(0.14 * playCount) +
			(0.14 * replayPreference) +
			(0.10 * noveltyPreference) +
			(0.08 * (1 - fatigue)) +
			(0.06 * (1 - overexposure)),
	)
}

func (s *Service) artistListeningAffinity(result ArtistResult, rerankCtx artistRerankContext) float64 {
	artistFeature := rerankCtx.artistFeatures[normalizeKey(result.Name)]

	familiarity := clampScore(artistFeature.FamiliarityScore)
	fatigue := clampScore(artistFeature.FatigueScore)
	rating := clampScore(float64(result.Rating) / 5.0)
	playCount := clampScore(float64(result.PlayCount) / 25.0)

	replayPreference := 0.0
	noveltyPreference := 0.0
	if rerankCtx.summary != nil {
		replayPreference = clampScore(rerankCtx.summary.ReplayAffinityScore * ((0.70 * familiarity) + (0.30 * playCount)))
		noveltyPreference = clampScore(rerankCtx.summary.NoveltyToleranceScore * (1 - familiarity))
	}

	return clampScore(
		(0.42 * familiarity) +
			(0.14 * rating) +
			(0.14 * playCount) +
			(0.15 * replayPreference) +
			(0.07 * noveltyPreference) +
			(0.08 * (1 - fatigue)),
	)
}

func trackDiversityPenalty(result TrackResult, seedArtistKey string, artistSeen, albumSeen map[string]int, listeningContext resolvedListeningContext) float64 {
	artistKey := normalizeKey(result.ArtistName)
	penalty := 0.0
	weight := 1.0
	switch listeningContext.Mode {
	case ModeFamiliar:
		weight = 0.65
	case ModeDeepCut:
		weight = 1.2
	case ModeSurprise:
		weight = 1.45
	}
	profile := parseMoodProfile(listeningContext.Mood)
	weight += profile.diversityBias
	if weight < 0.4 {
		weight = 0.4
	}
	if artistSeen[artistKey] > 0 {
		penalty += 0.07 * float64(artistSeen[artistKey]) * weight
	}
	if albumID := strings.TrimSpace(result.AlbumID); albumID != "" && albumSeen[albumID] > 0 {
		penalty += 0.05 * float64(albumSeen[albumID]) * weight
	}
	if seedArtistKey != "" && artistKey == seedArtistKey {
		sameArtistPenalty := 0.03
		if listeningContext.Mode == ModeFamiliar {
			sameArtistPenalty = 0.015
		}
		penalty += sameArtistPenalty
	}
	return clampScore(penalty)
}

func (c *audioMuseClient) SimilarTracks(ctx context.Context, repo repository, seed *db.Track, req TrackRequest, limit int) ([]TrackResult, error) {
	payload := map[string]interface{}{
		"limit":               clampLimit(limit, 10, 200),
		"track_id":            seed.ID,
		"seed_track_id":       seed.ID,
		"song_id":             seed.ID,
		"seed_song_id":        seed.ID,
		"title":               seed.Title,
		"track_title":         seed.Title,
		"artist":              seed.ArtistName,
		"artist_name":         seed.ArtistName,
		"exclude_recent_days": req.ExcludeRecentDays,
	}
	var raw interface{}
	if err := c.doJSON(ctx, c.tracksPath, payload, &raw); err != nil {
		return nil, err
	}
	items := extractObjectList(raw, []string{"tracks", "songs", "results", "recommendations", "similar_tracks", "similarSongs"})
	if len(items) == 0 {
		return nil, fmt.Errorf("AudioMuse returned no track candidates")
	}
	results := make([]TrackResult, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for idx, item := range items {
		result, ok := mapTrackCandidate(item, idx, len(items))
		if !ok {
			continue
		}
		if result.ID == "" && result.ArtistName != "" && result.Title != "" {
			match, err := repo.GetTrackByArtistTitle(ctx, result.ArtistName, result.Title)
			if err != nil {
				return nil, err
			}
			if match != nil {
				result.ID = match.ID
				result.AlbumID = match.AlbumID
				result.Rating = match.Rating
				result.PlayCount = match.PlayCount
				result.LastPlayed = match.LastPlayed
			}
		}
		if result.ID == seed.ID {
			continue
		}
		key := trackResultKey(result)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		results = append(results, result)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("AudioMuse returned candidates that could not be mapped into library tracks")
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			if results[i].ArtistName == results[j].ArtistName {
				return results[i].Title < results[j].Title
			}
			return results[i].ArtistName < results[j].ArtistName
		}
		return results[i].Score > results[j].Score
	})
	return trimTrackResults(results, limit), nil
}

func (c *audioMuseClient) SimilarArtists(ctx context.Context, repo repository, seedName string, limit int) ([]ArtistResult, error) {
	payload := map[string]interface{}{
		"limit":       clampLimit(limit, 5, 100),
		"artist":      seedName,
		"artist_name": seedName,
		"seed_artist": seedName,
	}
	var raw interface{}
	if err := c.doJSON(ctx, c.artistsPath, payload, &raw); err != nil {
		return nil, err
	}
	items := extractObjectList(raw, []string{"artists", "results", "recommendations", "similar_artists", "similarArtists"})
	if len(items) == 0 {
		return nil, fmt.Errorf("AudioMuse returned no artist candidates")
	}
	results := make([]ArtistResult, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for idx, item := range items {
		result, ok := mapArtistCandidate(item, idx, len(items))
		if !ok {
			continue
		}
		if normalizeKey(result.Name) == normalizeKey(seedName) {
			continue
		}
		if result.ID == "" {
			match, err := repo.GetArtistByName(ctx, result.Name)
			if err != nil {
				return nil, err
			}
			if match != nil {
				result.ID = match.ID
				result.Rating = match.Rating
				result.PlayCount = match.PlayCount
			}
		}
		key := artistResultKey(result)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		results = append(results, result)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("AudioMuse returned candidates that could not be mapped into library artists")
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Name < results[j].Name
		}
		return results[i].Score > results[j].Score
	})
	return trimArtistResults(results, limit), nil
}

func (c *audioMuseClient) Health(ctx context.Context) (bool, error) {
	if c == nil {
		return false, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+c.healthPath, nil)
	if err != nil {
		return false, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return false, fmt.Errorf("AudioMuse health returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return true, nil
}

func (c *audioMuseClient) LibraryState(ctx context.Context) (string, string, error) {
	activeTasks, err := c.ActiveTasks(ctx)
	if err != nil {
		return audioMuseLibraryStateUnknown, "", err
	}
	if len(activeTasks) > 0 {
		return audioMuseLibraryStateProcessing, "", nil
	}

	lastTask, err := c.LastTask(ctx)
	if err != nil {
		return audioMuseLibraryStateUnknown, "", err
	}
	lastStatus := strings.ToUpper(strings.TrimSpace(lastTask.Status))
	lastTaskID := ""
	if lastTask.TaskID != nil {
		lastTaskID = strings.TrimSpace(*lastTask.TaskID)
	}
	switch lastStatus {
	case "", "NO_PREVIOUS_MAIN_TASK":
		return audioMuseLibraryStateUninitialized, lastTaskID, nil
	case "PENDING", "STARTED", "PROGRESS", "RUNNING", "QUEUED":
		return audioMuseLibraryStateProcessing, lastTaskID, nil
	case "SUCCESS":
		return audioMuseLibraryStateReady, lastTaskID, nil
	case "FAILURE", "FAILED", "REVOKED":
		return audioMuseLibraryStateFailed, lastTaskID, nil
	default:
		return audioMuseLibraryStateUnknown, lastTaskID, nil
	}
}

func (c *audioMuseClient) LastTask(ctx context.Context) (audioMuseTaskSummary, error) {
	var payload audioMuseTaskSummary
	if err := c.doRequestJSON(ctx, http.MethodGet, "/api/last_task", nil, &payload); err != nil {
		return audioMuseTaskSummary{}, err
	}
	return payload, nil
}

func (c *audioMuseClient) ActiveTasks(ctx context.Context) (map[string]interface{}, error) {
	var payload map[string]interface{}
	if err := c.doRequestJSON(ctx, http.MethodGet, "/api/active_tasks", nil, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (c *audioMuseClient) StartAnalysis(ctx context.Context, numRecentAlbums, topNMoods int) (string, error) {
	payload := map[string]interface{}{
		"num_recent_albums": numRecentAlbums,
		"top_n_moods":       topNMoods,
	}
	var response struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
	}
	if err := c.doRequestJSON(ctx, http.MethodPost, "/api/analysis/start", payload, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.TaskID) == "" {
		return "", fmt.Errorf("AudioMuse analysis enqueue did not return a task id")
	}
	return strings.TrimSpace(response.TaskID), nil
}

func (c *audioMuseClient) doJSON(ctx context.Context, path string, payload map[string]interface{}, out interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	postReq.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(postReq)
	if err == nil && resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		defer resp.Body.Close()
		return json.NewDecoder(resp.Body).Decode(out)
	}
	if resp != nil {
		resp.Body.Close()
	}
	params := url.Values{}
	for key, value := range payload {
		switch v := value.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				params.Set(key, v)
			}
		case int:
			params.Set(key, strconv.Itoa(v))
		case bool:
			params.Set(key, strconv.FormatBool(v))
		}
	}
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err = c.client.Do(getReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("AudioMuse similarity returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *audioMuseClient) doRequestJSON(ctx context.Context, method, path string, payload map[string]interface{}, out interface{}) error {
	var bodyReader io.Reader
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+ensureLeadingSlash(path), bodyReader)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("AudioMuse %s %s returned %d: %s", method, ensureLeadingSlash(path), resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func mapTrackCandidate(item map[string]interface{}, index, total int) (TrackResult, bool) {
	title := firstString(item, "title", "track_title", "song_title", "name")
	artist := firstString(item, "artist_name", "artistName", "artist")
	if strings.TrimSpace(title) == "" || strings.TrimSpace(artist) == "" {
		return TrackResult{}, false
	}
	score := firstFloat(item, "score", "similarity", "confidence")
	if score <= 0 {
		score = rankScore(index, total)
	}
	result := TrackResult{
		ID:         firstString(item, "id", "track_id", "song_id"),
		AlbumID:    firstString(item, "album_id", "albumId"),
		Title:      title,
		ArtistName: artist,
		Score:      clampScore(score),
		SourceScores: map[string]float64{
			ProviderAudioMuse: clampScore(score),
		},
		Sources: []string{ProviderAudioMuse},
	}
	return result, true
}

func mapArtistCandidate(item map[string]interface{}, index, total int) (ArtistResult, bool) {
	name := firstString(item, "name", "artist", "artist_name", "artistName")
	if strings.TrimSpace(name) == "" {
		return ArtistResult{}, false
	}
	score := firstFloat(item, "score", "similarity", "confidence")
	if score <= 0 {
		score = rankScore(index, total)
	}
	result := ArtistResult{
		ID:    firstString(item, "id", "artist_id"),
		Name:  name,
		Score: clampScore(score),
		SourceScores: map[string]float64{
			ProviderAudioMuse: clampScore(score),
		},
		Sources: []string{ProviderAudioMuse},
	}
	return result, true
}

func extractObjectList(raw interface{}, preferredKeys []string) []map[string]interface{} {
	switch value := raw.(type) {
	case []interface{}:
		items := toObjectList(value)
		if len(items) > 0 {
			return items
		}
		for _, entry := range value {
			if nested := extractObjectList(entry, preferredKeys); len(nested) > 0 {
				return nested
			}
		}
	case map[string]interface{}:
		for _, key := range preferredKeys {
			if nested, ok := value[key]; ok {
				if list := extractObjectList(nested, nil); len(list) > 0 {
					return list
				}
			}
		}
		for _, nested := range value {
			if list := extractObjectList(nested, preferredKeys); len(list) > 0 {
				return list
			}
		}
	}
	return nil
}

func toObjectList(items []interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]interface{})
		if !ok {
			return nil
		}
		out = append(out, object)
	}
	return out
}

func (s *Service) resolveProvider(raw string) string {
	provider := normalizeProvider(raw)
	switch provider {
	case ProviderLocal, ProviderAudioMuse, ProviderHybrid:
	default:
		provider = s.defaultProvider
	}
	if provider == ProviderAudioMuse && s.audioMuse == nil {
		return ProviderLocal
	}
	if provider == ProviderHybrid && s.audioMuse == nil {
		return ProviderLocal
	}
	return provider
}

func weightedScore(hasLocal bool, localScore, localWeight float64, hasAudio bool, audioScore, audioWeight float64) float64 {
	totalWeight := 0.0
	total := 0.0
	if hasLocal {
		total += localScore * localWeight
		totalWeight += localWeight
	}
	if hasAudio {
		total += audioScore * audioWeight
		totalWeight += audioWeight
	}
	if totalWeight == 0 {
		return 0
	}
	return total / totalWeight
}

func trimTrackResults(results []TrackResult, limit int) []TrackResult {
	limit = clampLimit(limit, 1, 100)
	if len(results) <= limit {
		return results
	}
	return results[:limit]
}

func trimArtistResults(results []ArtistResult, limit int) []ArtistResult {
	limit = clampLimit(limit, 1, 100)
	if len(results) <= limit {
		return results
	}
	return results[:limit]
}

func trackResultKey(result TrackResult) string {
	if strings.TrimSpace(result.ID) != "" {
		return "id:" + strings.TrimSpace(result.ID)
	}
	return "name:" + normalizeKey(result.ArtistName) + "|" + normalizeKey(result.Title)
}

func artistResultKey(result ArtistResult) string {
	if strings.TrimSpace(result.ID) != "" {
		return "id:" + strings.TrimSpace(result.ID)
	}
	return "name:" + normalizeKey(result.Name)
}

func mergeSources(left, right []string) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	out := make([]string, 0, len(left)+len(right))
	for _, source := range append(left, right...) {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		if _, ok := seen[source]; ok {
			continue
		}
		seen[source] = struct{}{}
		out = append(out, source)
	}
	sort.Strings(out)
	return out
}

func normalizeProvider(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ProviderLocal:
		return ProviderLocal
	case ProviderAudioMuse, "audio_muse", "audio-muse":
		return ProviderAudioMuse
	case ProviderHybrid:
		return ProviderHybrid
	default:
		return ""
	}
}

func normalizeKey(value string) string {
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

func clampLimit(value, defaultValue, maxValue int) int {
	if value <= 0 {
		value = defaultValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func clampScore(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func clampSignedScore(score float64) float64 {
	if score < -0.15 {
		return -0.15
	}
	if score > 0.15 {
		return 0.15
	}
	return score
}

func rankScore(index, total int) float64 {
	if total <= 1 {
		return 1
	}
	return clampScore(1 - (float64(index) / float64(total)))
}

func firstString(values map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		raw, ok := values[key]
		if !ok {
			continue
		}
		switch value := raw.(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		case fmt.Stringer:
			text := strings.TrimSpace(value.String())
			if text != "" {
				return text
			}
		}
	}
	return ""
}

func firstFloat(values map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		raw, ok := values[key]
		if !ok {
			continue
		}
		switch value := raw.(type) {
		case float64:
			return value
		case float32:
			return float64(value)
		case int:
			return float64(value)
		case int64:
			return float64(value)
		case json.Number:
			if parsed, err := value.Float64(); err == nil {
				return parsed
			}
		case string:
			parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err == nil {
				return parsed
			}
		}
	}
	return 0
}

func defaultEnv(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func envFloat(name string, fallback float64) float64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return value
}

func envBool(name string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func normalizeWeight(value, fallback float64) float64 {
	if value <= 0 {
		return fallback
	}
	return value
}

func ensureLeadingSlash(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}
