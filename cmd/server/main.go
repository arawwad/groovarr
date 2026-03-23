package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"groovarr/graph"
	"groovarr/internal/agent"
	"groovarr/internal/db"
	"groovarr/internal/similarity"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Server struct {
	dbClient      *db.Client
	agent         chatAgent
	normalizer    chatTurnNormalizer
	planner       chatTurnPlanner
	turnResolver  chatTurnResolver
	chatArchive   *chatSessionArchive
	resolver      *graph.Resolver
	similarity    *similarity.Service
	embeddingsURL string
	events        *eventBroker
	workflowMu    sync.Mutex
	workflowRuns  map[string]*workflowRunState
	workflowCache map[string]workflowCacheEntry
	memoryMu      sync.Mutex
	chatMemory    map[string]chatSessionMemory
	approvalMu    sync.Mutex
	approvals     map[string]*pendingActionState
	latestPending map[string]string
}

type chatAgent interface {
	ProcessQueryWithSignals(ctx context.Context, userMsg string, history []agent.Message, modelOverride string, signals *agent.TurnSignals) (string, error)
}

type chatTurnNormalizer interface {
	NormalizeTurn(ctx context.Context, msg string, history []agent.Message, sessionContext string) (normalizedTurn, error)
}

type chatTurnPlanner interface {
	PlanTurn(ctx context.Context, turn *Turn, history []agent.Message, sessionContext string) (orchestrationDecision, error)
}

type chatTurnResolver interface {
	ResolveTurn(ctx context.Context, turn *Turn) (resultSetResolverDecision, error)
}

type ChatRequest struct {
	Message   string          `json:"message"`
	History   []agent.Message `json:"history,omitempty"`
	Model     string          `json:"model,omitempty"`
	Stream    bool            `json:"stream,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

type ChatResponse struct {
	Response      string         `json:"response"`
	Error         string         `json:"error,omitempty"`
	PendingAction *PendingAction `json:"pendingAction,omitempty"`
}

type ChatModelsResponse struct {
	Models       []string `json:"models"`
	DefaultModel string   `json:"defaultModel"`
}

type ChatStreamResponse struct {
	Type          string         `json:"type"`
	Delta         string         `json:"delta,omitempty"`
	Response      string         `json:"response,omitempty"`
	Error         string         `json:"error,omitempty"`
	PendingAction *PendingAction `json:"pendingAction,omitempty"`
}

const defaultMaxChatBodyBytes = 32 * 1024
const defaultMaxChatHistoryMessages = 12
const defaultMaxHistoryMessageChars = 1200

func recentListeningSummaryTTL() time.Duration {
	return time.Duration(envInt("RECENT_LISTENING_SUMMARY_TTL_SECONDS", 45)) * time.Second
}

func pendingActionTTL() time.Duration {
	return time.Duration(envInt("PENDING_ACTION_TTL_MINUTES", 30)) * time.Minute
}

type cachedToolResult struct {
	value   string
	expires time.Time
}

type workflowRunState struct {
	done     chan struct{}
	response string
	handled  bool
	err      error
}

type workflowCacheEntry struct {
	response string
	handled  bool
	expires  time.Time
}

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})

	ctx := context.Background()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal().Msg("DATABASE_URL required")
	}

	dbClient, err := db.New(ctx, dbURL)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer dbClient.Close()
	log.Info().Msg("Database connected")
	preflightRuntimeConfig()

	navidromePath := os.Getenv("NAVIDROME_DB_PATH")
	embeddingsURL := os.Getenv("EMBEDDINGS_ENDPOINT")
	if navidromePath != "" {
		syncer, err := db.NewSyncer(navidromePath, dbClient, embeddingsURL)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to create syncer")
		}
		syncInterval := time.Duration(envInt("SYNC_INTERVAL_MINUTES", 15)) * time.Minute
		go syncer.Run(ctx, syncInterval)
		event := log.Info().Dur("interval", syncInterval)
		if embeddingsURL == "" {
			event = event.Bool("embeddings_enabled", false)
		}
		event.Msg("Sync daemon started")
	} else {
		log.Info().Msg("Sync daemon disabled because NAVIDROME_DB_PATH is not set")
	}

	resolver := &graph.Resolver{DB: dbClient}
	similarityService := similarity.NewService(dbClient, similarity.ConfigFromEnv())

	groqKey := os.Getenv("GROQ_API_KEY")
	groqModel := os.Getenv("GROQ_MODEL")
	huggingFaceKey := firstEnv("HUGGINGFACE_API_KEY", "HF_API_KEY", "HF_TOKEN")
	if groqModel == "" {
		groqModel = agent.DefaultGroqModel
	}

	var srv *Server
	toolExecute := func(ctx context.Context, tool string, args map[string]interface{}) (string, error) {
		if srv != nil && srv.chatArchive != nil {
			srv.chatArchive.RecordToolStart(ctx, tool, args)
		}
		log.Info().Str("tool", tool).Interface("args", args).Msg("Executing agent tool")
		var (
			result string
			err    error
		)
		switch tool {
		case "startArtistRemovalPreview", "startDiscoveredAlbumsApplyPreview", "startLidarrCleanupApplyPreview", "startPlaylistCreatePreview", "startPlaylistAppendPreview", "startPlaylistRefreshPreview", "startPlaylistRepairPreview":
			result, err = srv.executeServerFlowTool(ctx, tool, args)
		case "applyDiscoveredAlbums":
			err = fmt.Errorf("direct applyDiscoveredAlbums calls are disabled for the agent; use startDiscoveredAlbumsApplyPreview instead")
		case "applyLidarrCleanup":
			err = fmt.Errorf("direct applyLidarrCleanup calls are disabled for the agent; use startLidarrCleanupApplyPreview instead")
		case "createDiscoveredPlaylist":
			err = fmt.Errorf("direct createDiscoveredPlaylist calls are disabled for the agent; plan the playlist and let the server-managed approval flow create it")
		case "removeArtistFromLibrary":
			err = fmt.Errorf("direct removeArtistFromLibrary calls are disabled for the agent; use startArtistRemovalPreview instead")
		default:
			result, err = executeToolWithSimilarity(ctx, resolver, similarityService, embeddingsURL, tool, args)
		}
		event := log.Info().Str("tool", tool).Str("result", result)
		if err != nil {
			event = event.Err(err)
		}
		event.Msg("Agent tool result")
		if srv != nil && srv.chatArchive != nil {
			srv.chatArchive.RecordToolResult(ctx, tool, result, err)
		}
		return result, err
	}

	agentExec := agent.New(groqKey, groqModel, huggingFaceKey, toolExecute)

	srv = &Server{
		dbClient:      dbClient,
		agent:         agentExec,
		normalizer:    newGroqTurnNormalizer(groqKey, groqModel),
		planner:       newGroqTurnPlanner(groqKey, groqModel),
		turnResolver:  newGroqTurnResolver(groqKey, groqModel),
		chatArchive:   newChatSessionArchive(),
		resolver:      resolver,
		similarity:    similarityService,
		embeddingsURL: embeddingsURL,
		events:        newEventBroker(),
		workflowRuns:  make(map[string]*workflowRunState),
		workflowCache: make(map[string]workflowCacheEntry),
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}
	installChatSessionArchive(srv.chatArchive)

	go startPlaylistReconcileManager(ctx, func(message string) {
		srv.publishEvent("reconcile", message)
	})
	log.Info().
		Int("active_interval_minutes", playlistReconcileIntervalMinutes()).
		Msg("Playlist reconcile manager started")

	go startScenePlaylistSyncManager(ctx, func(message string) {
		srv.publishEvent("playlist", message)
	})
	log.Info().
		Dur("active_interval", scenePlaylistSyncInterval).
		Msg("Scene playlist sync manager started")

	mux := http.NewServeMux()
	// Attempt to register gqlgen handler if available (no-op otherwise).
	registerGQL(mux, resolver)
	// GraphQL endpoint for external clients and agent testing
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}

		out, err := executeGraphQL(r.Context(), resolver, payload.Query)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(out))
	})
	mux.HandleFunc("/api/chat", srv.handleChat)
	mux.HandleFunc("/api/chat/models", srv.handleChatModels)
	mux.HandleFunc("/api/debug/chat-sessions", srv.handleChatSessionsDebug)
	mux.HandleFunc("/api/debug/chat-sessions/", srv.handleChatSessionDebug)
	mux.HandleFunc("/api/pending-actions/", srv.handlePendingAction)
	mux.HandleFunc("/api/events", srv.handleEvents)
	mux.HandleFunc("/api/health", srv.handleHealth)
	mux.HandleFunc("/api/listen/overview", srv.handleListenOverview)
	mux.HandleFunc("/api/listen/clusters", srv.handleListenClusters)
	mux.HandleFunc("/api/listen/clusters/start", srv.handleListenClustersStart)
	mux.HandleFunc("/api/listen/map", srv.handleListenMap)
	mux.HandleFunc("/api/listen/text-search", srv.handleListenTextSearch)
	mux.HandleFunc("/api/listen/path-search", srv.handleListenPathSearch)
	mux.HandleFunc("/api/listen/path", srv.handleListenSongPath)
	mux.HandleFunc("/api/listen/track-search", srv.handleListenTrackSearch)
	mux.HandleFunc("/api/listen/preview", srv.handleListenPreview)
	mux.HandleFunc("/api/listen/neighborhood", srv.handleListenNeighborhood)
	mux.HandleFunc("/api/similarity/context", srv.handleSimilarityContext)
	mux.HandleFunc("/api/similarity/health", srv.handleSimilarityHealth)
	mux.HandleFunc("/api/similarity/songs/by-artist", srv.handleSimilaritySongsByArtist)
	mux.HandleFunc("/api/similarity/tracks", srv.handleSimilarityTracks)
	mux.HandleFunc("/api/similarity/artists", srv.handleSimilarityArtists)
	mux.HandleFunc("/api/sync/status", srv.handleSyncStatus)
	mux.Handle("/static/", http.StripPrefix("/static/", srv.staticUIHandler()))
	mux.HandleFunc("/", srv.handleIndex)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8088"
	}

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: time.Duration(envInt("HTTP_READ_HEADER_TIMEOUT_SEC", 5)) * time.Second,
		ReadTimeout:       time.Duration(envInt("HTTP_READ_TIMEOUT_SEC", 10)) * time.Second,
		WriteTimeout:      time.Duration(envInt("HTTP_WRITE_TIMEOUT_SEC", 40)) * time.Second,
		IdleTimeout:       time.Duration(envInt("HTTP_IDLE_TIMEOUT_SEC", 60)) * time.Second,
	}

	go func() {
		log.Info().Str("port", port).Msg("Server starting")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Server failed")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpServer.Shutdown(ctx)
}

func preflightRuntimeConfig() {
	navidromePath := strings.TrimSpace(os.Getenv("NAVIDROME_DB_PATH"))
	if navidromePath != "" {
		info, err := os.Stat(navidromePath)
		if err != nil {
			log.Fatal().Err(err).Str("path", navidromePath).Msg("NAVIDROME_DB_PATH is not readable")
		}
		if info.IsDir() {
			log.Fatal().Str("path", navidromePath).Msg("NAVIDROME_DB_PATH must point to navidrome.db, not a directory")
		}
	}

	lidarrURL := strings.TrimSpace(os.Getenv("LIDARR_URL"))
	lidarrAPIKey := strings.TrimSpace(os.Getenv("LIDARR_API_KEY"))
	if (lidarrURL == "") != (lidarrAPIKey == "") {
		log.Warn().Msg("Lidarr is partially configured; set both LIDARR_URL and LIDARR_API_KEY to enable Lidarr workflows")
	}
	if lidarrURL != "" && lidarrAPIKey != "" && strings.TrimSpace(os.Getenv("LIDARR_ROOT_FOLDER_PATH")) == "" {
		log.Warn().Msg("LIDARR_ROOT_FOLDER_PATH is not set; add workflows will rely on Lidarr root-folder discovery")
	}

	if strings.TrimSpace(os.Getenv("EMBEDDINGS_ENDPOINT")) == "" {
		log.Warn().Msg("EMBEDDINGS_ENDPOINT is not set; semantic search and embedding-backed recommendations will degrade")
	}

	if strings.TrimSpace(os.Getenv("GROQ_API_KEY")) == "" && strings.TrimSpace(firstEnv("HUGGINGFACE_API_KEY", "HF_API_KEY", "HF_TOKEN")) == "" {
		log.Warn().Msg("No LLM API key is configured; chat and discovery requests will fail")
	}
}
