package main

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"groovarr/internal/agent"

	"github.com/rs/zerolog/log"
	"golang.org/x/text/unicode/norm"
)

type playlistCandidateTrack struct {
	Rank       int    `json:"rank"`
	ArtistName string `json:"artistName"`
	TrackTitle string `json:"trackTitle"`
	Reason     string `json:"reason,omitempty"`
	SourceHint string `json:"sourceHint,omitempty"`
}

type resolvedPlaylistTrack struct {
	Rank          int    `json:"rank"`
	ArtistName    string `json:"artistName"`
	TrackTitle    string `json:"trackTitle"`
	Status        string `json:"status"`
	SongID        string `json:"songId,omitempty"`
	MatchedArtist string `json:"matchedArtist,omitempty"`
	MatchedTitle  string `json:"matchedTitle,omitempty"`
	MatchCount    int    `json:"matchCount,omitempty"`
	Detail        string `json:"detail,omitempty"`
}

type playlistDiscoveryState struct {
	prompt       string
	playlistName string
	plannedAt    time.Time
	candidates   []playlistCandidateTrack
	resolvedAt   time.Time
	resolved     []resolvedPlaylistTrack
}

type playlistDiscoveryStore struct {
	mu       sync.RWMutex
	sessions map[string]playlistDiscoveryState
}

var lastPlaylistDiscovery = playlistDiscoveryStore{
	sessions: make(map[string]playlistDiscoveryState),
}

var playlistReconcileMu sync.Mutex
var playlistReconcileTrigger = make(chan struct{}, 1)

type playlistReconcileQueue struct {
	Items []playlistReconcileItem `json:"items"`
}

type playlistReconcileItem struct {
	ID         string    `json:"id"`
	QueueFile  string    `json:"queueFile,omitempty"`
	ArtistName string    `json:"artistName"`
	TrackTitle string    `json:"trackTitle"`
	Playlists  []string  `json:"playlists"`
	Attempts   int       `json:"attempts"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	LastError  string    `json:"lastError,omitempty"`
}

type pendingPlaylistTrack struct {
	JobID        string   `json:"jobId"`
	JobFile      string   `json:"jobFile,omitempty"`
	Rank         int      `json:"rank"`
	ArtistName   string   `json:"artistName"`
	TrackTitle   string   `json:"trackTitle"`
	State        string   `json:"state"`
	Attempts     int      `json:"attempts"`
	LastError    string   `json:"lastError,omitempty"`
	PlaylistName string   `json:"playlistName,omitempty"`
	Playlists    []string `json:"playlists,omitempty"`
	QueueFile    string   `json:"queueFile,omitempty"`
}

func setLastPlannedPlaylist(sessionID, prompt, playlistName string, candidates []playlistCandidateTrack) {
	newTurnSessionMemoryWriter(sessionID).SetPlannedPlaylist(prompt, playlistName, candidates)
}

func getLastPlannedPlaylist(sessionID string) (string, string, time.Time, []playlistCandidateTrack) {
	lastPlaylistDiscovery.mu.RLock()
	state, ok := lastPlaylistDiscovery.sessions[normalizeChatSessionID(sessionID)]
	lastPlaylistDiscovery.mu.RUnlock()
	if !ok {
		return "", "", time.Time{}, nil
	}
	copied := make([]playlistCandidateTrack, len(state.candidates))
	copy(copied, state.candidates)
	return state.prompt, state.playlistName, state.plannedAt, copied
}

func setLastResolvedPlaylist(sessionID string, items []resolvedPlaylistTrack) {
	newTurnSessionMemoryWriter(sessionID).SetResolvedPlaylist(items)
}

func getLastResolvedPlaylist(sessionID string) (time.Time, []resolvedPlaylistTrack) {
	lastPlaylistDiscovery.mu.RLock()
	state, ok := lastPlaylistDiscovery.sessions[normalizeChatSessionID(sessionID)]
	lastPlaylistDiscovery.mu.RUnlock()
	if !ok {
		return time.Time{}, nil
	}
	copied := make([]resolvedPlaylistTrack, len(state.resolved))
	copy(copied, state.resolved)
	return state.resolvedAt, copied
}

func playlistReconcileDir() string {
	dir := strings.TrimSpace(os.Getenv("PLAYLIST_RECONCILE_DIR"))
	if dir != "" {
		return dir
	}
	queueDir := strings.TrimSpace(os.Getenv("PLAYLIST_QUEUE_DIR"))
	if queueDir == "" {
		queueDir = "/app/data/playlist-queue"
	}
	return filepath.Join(filepath.Dir(queueDir), "playlist-reconcile")
}

func playlistReconcileIntervalMinutes() int {
	v := strings.TrimSpace(os.Getenv("PLAYLIST_RECONCILE_INTERVAL_MINUTES"))
	if v == "" {
		return 3
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 3
	}
	return n
}

func playlistReconcileFastIntervalSeconds() int {
	v := strings.TrimSpace(os.Getenv("PLAYLIST_RECONCILE_FAST_INTERVAL_SECONDS"))
	if v == "" {
		return 60
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 60
	}
	return n
}

func playlistReconcileFastPasses() int {
	v := strings.TrimSpace(os.Getenv("PLAYLIST_RECONCILE_FAST_PASSES"))
	if v == "" {
		return 25
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 25
	}
	return n
}

func playlistReconcileMaxAttempts() int {
	v := strings.TrimSpace(os.Getenv("PLAYLIST_RECONCILE_MAX_ATTEMPTS"))
	if v == "" {
		return 30
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 30
	}
	return n
}

func triggerPlaylistReconcile() {
	select {
	case playlistReconcileTrigger <- struct{}{}:
	default:
	}
}

func playlistReconcileStatePath() string {
	return filepath.Join(playlistReconcileDir(), "queue.json")
}

func playlistQueueDir() string {
	queueDir := strings.TrimSpace(os.Getenv("PLAYLIST_QUEUE_DIR"))
	if queueDir == "" {
		queueDir = "/app/data/playlist-queue"
	}
	return queueDir
}

func playlistQueueStatusDir() string {
	dir := strings.TrimSpace(os.Getenv("PLAYLIST_QUEUE_STATUS_DIR"))
	if dir != "" {
		return dir
	}
	return filepath.Join(filepath.Dir(playlistQueueDir()), "playlist-processed")
}

func playlistQueueStatusPaths(queueFile string) (string, string) {
	base := filepath.Base(strings.TrimSpace(queueFile))
	if base == "" {
		return "", ""
	}
	statusDir := playlistQueueStatusDir()
	return filepath.Join(statusDir, base+".done"), filepath.Join(statusDir, base+".failed")
}

func loadPlaylistReconcileQueue() (playlistReconcileQueue, error) {
	path := playlistReconcileStatePath()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return playlistReconcileQueue{}, nil
		}
		return playlistReconcileQueue{}, err
	}
	var queue playlistReconcileQueue
	if err := json.Unmarshal(raw, &queue); err != nil {
		return playlistReconcileQueue{}, err
	}
	if queue.Items == nil {
		queue.Items = []playlistReconcileItem{}
	}
	return queue, nil
}

func writePlaylistReconcileQueue(queue playlistReconcileQueue) error {
	dir := playlistReconcileDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(queue, "", "  ")
	if err != nil {
		return err
	}
	path := playlistReconcileStatePath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func countPendingPlaylistReconcileJobs() (int, error) {
	queue, err := loadPlaylistReconcileQueue()
	if err != nil {
		return 0, err
	}
	return len(queue.Items), nil
}

func normalizedPlaylistTrackKey(artistName, trackTitle string) string {
	artist := normalizeSearchTerm(artistName)
	title := normalizeSearchTerm(trackTitle)
	if artist == "" && title == "" {
		return ""
	}
	return artist + "\x00" + title
}

func pendingPlaylistTrackKeysForPlaylist(playlistName string) (map[string]struct{}, error) {
	pending, err := pendingPlaylistTracksForPlaylist(playlistName)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]struct{}, len(pending))
	for _, item := range pending {
		key := normalizedPlaylistTrackKey(item.ArtistName, item.TrackTitle)
		if key == "" {
			continue
		}
		keys[key] = struct{}{}
	}
	return keys, nil
}

func normalizePlaylistNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := normalizeSearchTerm(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func writePlaylistQueueFile(itemID, artistName, trackTitle string) (string, error) {
	queueDir := playlistQueueDir()
	if err := os.MkdirAll(queueDir, 0755); err != nil {
		return "", fmt.Errorf("creating queue directory: %w", err)
	}
	statusDir := playlistQueueStatusDir()
	if err := os.MkdirAll(statusDir, 0755); err != nil {
		return "", fmt.Errorf("creating queue status directory: %w", err)
	}
	filename := filepath.Join(queueDir, itemID+".txt")
	content := fmt.Sprintf("%s - %s\n", artistName, trackTitle)
	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("writing queue file: %w", err)
	}
	return filename, nil
}

func removePlaylistQueueArtifacts(queueFile string) {
	queueFile = strings.TrimSpace(queueFile)
	if queueFile != "" {
		_ = os.Remove(queueFile)
	}
	donePath, failedPath := playlistQueueStatusPaths(queueFile)
	if donePath != "" {
		_ = os.Remove(donePath)
	}
	if failedPath != "" {
		_ = os.Remove(failedPath)
	}
}

func enqueuePlaylistReconcileTracks(playlistName string, missing []resolvedPlaylistTrack) (int, string, string, error) {
	playlistName = strings.TrimSpace(playlistName)
	if playlistName == "" {
		playlistName = "Discover: Mixed"
	}
	queue, err := loadPlaylistReconcileQueue()
	if err != nil {
		return 0, "", "", err
	}
	now := time.Now().UTC()
	firstQueueFile := ""
	firstItemID := ""
	created := 0
	seenInput := make(map[string]struct{}, len(missing))
	for _, item := range missing {
		key := normalizedPlaylistTrackKey(item.ArtistName, item.TrackTitle)
		if key == "" {
			continue
		}
		if _, ok := seenInput[key]; ok {
			continue
		}
		seenInput[key] = struct{}{}
		found := false
		for i := range queue.Items {
			existingKey := normalizedPlaylistTrackKey(queue.Items[i].ArtistName, queue.Items[i].TrackTitle)
			if existingKey != key {
				continue
			}
			queue.Items[i].Playlists = normalizePlaylistNames(append(queue.Items[i].Playlists, playlistName))
			queue.Items[i].UpdatedAt = now
			found = true
			break
		}
		if found {
			continue
		}
		itemID := fmt.Sprintf("track-%d-%d", now.UnixNano(), created+1)
		queueFile, err := writePlaylistQueueFile(itemID, item.ArtistName, item.TrackTitle)
		if err != nil {
			return 0, "", "", err
		}
		queue.Items = append(queue.Items, playlistReconcileItem{
			ID:         itemID,
			QueueFile:  queueFile,
			ArtistName: strings.TrimSpace(item.ArtistName),
			TrackTitle: strings.TrimSpace(item.TrackTitle),
			Playlists:  []string{playlistName},
			Attempts:   0,
			CreatedAt:  now,
			UpdatedAt:  now,
		})
		if firstQueueFile == "" {
			firstQueueFile = queueFile
			firstItemID = itemID
		}
		created++
	}
	if err := writePlaylistReconcileQueue(queue); err != nil {
		return 0, "", "", err
	}
	if created > 0 {
		triggerPlaylistReconcile()
	}
	return created, firstQueueFile, firstItemID, nil
}

func pendingPlaylistTracksForPlaylist(playlistName string) ([]pendingPlaylistTrack, error) {
	queue, err := loadPlaylistReconcileQueue()
	if err != nil {
		return nil, err
	}
	target := normalizeSearchTerm(playlistName)
	out := make([]pendingPlaylistTrack, 0)
	for _, item := range queue.Items {
		matched := false
		for _, name := range item.Playlists {
			if target == "" || normalizeSearchTerm(name) == target {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		state := "pending_fetch"
		donePath, failedPath := playlistQueueStatusPaths(item.QueueFile)
		if failedPath != "" {
			if _, err := os.Stat(failedPath); err == nil {
				continue
			}
		}
		if donePath != "" {
			if _, err := os.Stat(donePath); err == nil {
				state = "waiting_import"
			}
		}
		out = append(out, pendingPlaylistTrack{
			JobID:        item.ID,
			JobFile:      playlistReconcileStatePath(),
			Rank:         0,
			ArtistName:   item.ArtistName,
			TrackTitle:   item.TrackTitle,
			State:        state,
			Attempts:     item.Attempts,
			LastError:    strings.TrimSpace(item.LastError),
			PlaylistName: strings.TrimSpace(playlistName),
			Playlists:    append([]string(nil), item.Playlists...),
			QueueFile:    item.QueueFile,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ArtistName == out[j].ArtistName {
			return out[i].TrackTitle < out[j].TrackTitle
		}
		return out[i].ArtistName < out[j].ArtistName
	})
	return out, nil
}

func removePendingPlaylistTracksForPlaylist(playlistName, selection string) ([]pendingPlaylistTrack, error) {
	queue, err := loadPlaylistReconcileQueue()
	if err != nil {
		return nil, err
	}
	target := normalizeSearchTerm(playlistName)
	refs := make([]pendingPlaylistTrack, 0)
	candidates := make([]playlistCandidateTrack, 0)
	indexByRank := make(map[int]int)
	for _, item := range queue.Items {
		matched := false
		for _, name := range item.Playlists {
			if target == "" || normalizeSearchTerm(name) == target {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		rank := len(candidates) + 1
		indexByRank[rank] = len(refs)
		for _, pendingPlaylist := range item.Playlists {
			if target != "" && normalizeSearchTerm(pendingPlaylist) != target {
				continue
			}
			refs = append(refs, pendingPlaylistTrack{
				JobID:        item.ID,
				JobFile:      playlistReconcileStatePath(),
				Rank:         rank,
				ArtistName:   item.ArtistName,
				TrackTitle:   item.TrackTitle,
				State:        "pending_fetch",
				Attempts:     item.Attempts,
				LastError:    strings.TrimSpace(item.LastError),
				PlaylistName: pendingPlaylist,
				Playlists:    append([]string(nil), item.Playlists...),
				QueueFile:    item.QueueFile,
			})
			candidates = append(candidates, playlistCandidateTrack{
				Rank:       rank,
				ArtistName: item.ArtistName,
				TrackTitle: item.TrackTitle,
			})
		}
	}
	selected, err := selectPlaylistCandidates(candidates, selection)
	if err != nil {
		return nil, err
	}
	selectedRefs := make([]pendingPlaylistTrack, 0, len(selected))
	selectedIDs := make(map[string]struct{}, len(selected))
	for _, item := range selected {
		refIndex, ok := indexByRank[item.Rank]
		if !ok {
			continue
		}
		selectedRefs = append(selectedRefs, refs[refIndex])
		selectedIDs[refs[refIndex].JobID] = struct{}{}
	}
	updated := make([]playlistReconcileItem, 0, len(queue.Items))
	now := time.Now().UTC()
	for _, item := range queue.Items {
		if _, ok := selectedIDs[item.ID]; !ok {
			updated = append(updated, item)
			continue
		}
		remainingPlaylists := make([]string, 0, len(item.Playlists))
		for _, name := range item.Playlists {
			if target != "" && normalizeSearchTerm(name) == target {
				continue
			}
			remainingPlaylists = append(remainingPlaylists, name)
		}
		if len(remainingPlaylists) == 0 {
			removePlaylistQueueArtifacts(item.QueueFile)
			continue
		}
		item.Playlists = normalizePlaylistNames(remainingPlaylists)
		item.UpdatedAt = now
		updated = append(updated, item)
	}
	queue.Items = updated
	if err := writePlaylistReconcileQueue(queue); err != nil {
		return nil, err
	}
	return selectedRefs, nil
}

func upsertPlaylistSongs(ctx context.Context, client *navidromeClient, playlistName string, songIDs []string) (string, int, int, error) {
	seen := make(map[string]struct{}, len(songIDs))
	unique := make([]string, 0, len(songIDs))
	for _, id := range songIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return "none", 0, 0, nil
	}

	playlist, err := client.GetPlaylistByName(ctx, playlistName)
	if err != nil {
		return "", 0, 0, err
	}
	if playlist == nil {
		if _, err := client.CreatePlaylist(ctx, playlistName, unique); err != nil {
			return "", 0, 0, err
		}
		return "created", len(unique), 0, nil
	}

	existingIDs, err := client.GetPlaylistSongIDs(ctx, playlist.ID)
	if err != nil {
		return "", 0, 0, err
	}
	existing := make(map[string]struct{}, len(existingIDs))
	for _, id := range existingIDs {
		existing[id] = struct{}{}
	}
	toAdd := make([]string, 0, len(unique))
	for _, id := range unique {
		if _, ok := existing[id]; ok {
			continue
		}
		toAdd = append(toAdd, id)
	}
	if len(toAdd) == 0 {
		return "updated", 0, len(existingIDs), nil
	}
	if err := client.UpdatePlaylistAddSongs(ctx, playlist.ID, toAdd); err != nil {
		return "", 0, 0, err
	}
	return "updated", len(toAdd), len(existingIDs), nil
}

func runPlaylistReconcilePass(ctx context.Context, publish func(string)) error {
	playlistReconcileMu.Lock()
	defer playlistReconcileMu.Unlock()

	queue, err := loadPlaylistReconcileQueue()
	if err != nil {
		return err
	}
	if len(queue.Items) == 0 {
		return nil
	}

	processed := 0
	now := time.Now().UTC()
	maxAttempts := playlistReconcileMaxAttempts()
	var client *navidromeClient
	ensureClient := func() error {
		if client != nil {
			return nil
		}
		created, err := newNavidromeClientFromEnv()
		if err != nil {
			return err
		}
		client = created
		return nil
	}
	remainingItems := make([]playlistReconcileItem, 0, len(queue.Items))
	for _, item := range queue.Items {
		donePath, failedPath := playlistQueueStatusPaths(item.QueueFile)
		if failedPath != "" {
			if _, err := os.Stat(failedPath); err == nil {
				removePlaylistQueueArtifacts(item.QueueFile)
				processed++
				if publish != nil {
					publish(fmt.Sprintf("Removed %q by %s from the global reconcile queue after download-agent rejection.", item.TrackTitle, item.ArtistName))
				}
				continue
			}
		}

		if item.Attempts >= maxAttempts {
			removePlaylistQueueArtifacts(item.QueueFile)
			processed++
			if publish != nil {
				publish(fmt.Sprintf("Removed %q by %s from the global reconcile queue after %d attempts.", item.TrackTitle, item.ArtistName, item.Attempts))
			}
			continue
		}

		if err := ensureClient(); err != nil {
			return err
		}

		matches, err := client.SearchTrackByArtistTitle(ctx, item.ArtistName, item.TrackTitle)
		if err != nil {
			item.Attempts++
			item.UpdatedAt = now
			item.LastError = err.Error()
			remainingItems = append(remainingItems, item)
			processed++
			continue
		}
		if len(matches) == 0 {
			item.Attempts++
			item.UpdatedAt = now
			item.LastError = ""
			if donePath != "" {
				if _, err := os.Stat(donePath); err == nil {
					item.LastError = "download completed; waiting for Navidrome import"
				}
			}
			remainingItems = append(remainingItems, item)
			processed++
			continue
		}

		songID := strings.TrimSpace(matches[0].ID)
		if songID == "" {
			item.Attempts++
			item.UpdatedAt = now
			item.LastError = "matched track had no id"
			remainingItems = append(remainingItems, item)
			processed++
			continue
		}

		remainingPlaylists := make([]string, 0, len(item.Playlists))
		reconciled := 0
		for _, playlistName := range item.Playlists {
			if _, _, _, err := upsertPlaylistSongs(ctx, client, playlistName, []string{songID}); err != nil {
				remainingPlaylists = append(remainingPlaylists, playlistName)
				item.LastError = err.Error()
				continue
			}
			reconciled += 1
			item.LastError = ""
			if publish != nil {
				publish(fmt.Sprintf("Reconciled %q by %s into playlist %q.", item.TrackTitle, item.ArtistName, playlistName))
			}
		}

		processed++
		if len(remainingPlaylists) == 0 {
			removePlaylistQueueArtifacts(item.QueueFile)
			continue
		}

		item.Playlists = normalizePlaylistNames(remainingPlaylists)
		item.Attempts++
		item.UpdatedAt = now
		if reconciled == 0 && item.LastError == "" {
			item.LastError = "track found but could not be applied to any playlist"
		}
		remainingItems = append(remainingItems, item)
	}
	queue.Items = remainingItems
	if err := writePlaylistReconcileQueue(queue); err != nil {
		return err
	}
	if processed > 0 {
		log.Info().Int("jobs_processed", processed).Msg("Playlist reconcile pass completed")
		if publish != nil {
			publish(fmt.Sprintf("Playlist reconcile pass completed: %d queue item(s) processed.", processed))
		}
	}
	return nil
}

func startPlaylistReconcileManager(ctx context.Context, publish func(string)) {
	activeInterval := time.Duration(playlistReconcileIntervalMinutes()) * time.Minute

	runPass := func() (int, error) {
		passCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		err := runPlaylistReconcilePass(passCtx, publish)
		cancel()
		if err != nil {
			return 0, err
		}
		return countPendingPlaylistReconcileJobs()
	}

	startupPending, err := runPass()
	if err != nil {
		log.Warn().Err(err).Msg("Startup reconcile pass failed")
	} else if startupPending == 0 {
		log.Info().Msg("Startup reconcile check complete; no pending jobs")
		if publish != nil {
			publish("No pending reconcile jobs. Reconcile is idle.")
		}
	} else {
		triggerPlaylistReconcile()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-playlistReconcileTrigger:
		}

		if publish != nil {
			publish(fmt.Sprintf("Reconcile triggered. Running checks every %s until the queue is empty.", activeInterval))
		}

		for {
			pending, err := runPass()
			if err != nil {
				log.Warn().Err(err).Msg("Reconcile pass failed")
			} else if pending == 0 {
				break
			}

			select {
			case <-ctx.Done():
				return
			case <-playlistReconcileTrigger:
			case <-time.After(activeInterval):
			}
		}

		if publish != nil {
			publish("Reconcile complete. No pending jobs; runner is idle.")
		}
	}
}

type navidromeSong struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
}

type navidromePlaylist struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SongCount int    `json:"songCount,omitempty"`
}

type navidromePlaylistEntry struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
}

type navidromePlaylistDetail struct {
	ID        string                   `json:"id"`
	Name      string                   `json:"name"`
	SongCount int                      `json:"songCount,omitempty"`
	Entries   []navidromePlaylistEntry `json:"entry"`
}

type navidromeClient struct {
	baseURL  string
	username string
	password string
	client   *http.Client
}

func newNavidromeClientFromEnv() (*navidromeClient, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("NAVIDROME_URL")), "/")
	username := strings.TrimSpace(os.Getenv("NAVIDROME_USERNAME"))
	password := strings.TrimSpace(os.Getenv("NAVIDROME_PASSWORD"))
	if baseURL == "" || username == "" || password == "" {
		return nil, fmt.Errorf("NAVIDROME_URL, NAVIDROME_USERNAME, and NAVIDROME_PASSWORD are required")
	}
	return &navidromeClient{
		baseURL:  baseURL,
		username: username,
		password: password,
		client:   &http.Client{Timeout: 25 * time.Second},
	}, nil
}

func (c *navidromeClient) authParams() url.Values {
	salt := fmt.Sprintf("%d", time.Now().UnixNano())
	token := fmt.Sprintf("%x", md5.Sum([]byte(c.password+salt)))
	params := url.Values{}
	params.Set("u", c.username)
	params.Set("t", token)
	params.Set("s", salt)
	params.Set("v", "1.16.1")
	params.Set("c", "groovarr")
	params.Set("f", "json")
	return params
}

func (c *navidromeClient) doGET(ctx context.Context, endpoint string, params url.Values, out interface{}) error {
	reqURL := fmt.Sprintf("%s/rest/%s?%s", c.baseURL, endpoint, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("navidrome API %s returned %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *navidromeClient) SearchTrackByArtistTitle(ctx context.Context, artist, title string) ([]navidromeSong, error) {
	params := c.authParams()
	params.Set("query", artist+" "+title)
	var payload struct {
		SubsonicResponse struct {
			SearchResult3 struct {
				Song []navidromeSong `json:"song"`
			} `json:"searchResult3"`
		} `json:"subsonic-response"`
	}
	if err := c.doGET(ctx, "search3", params, &payload); err != nil {
		return nil, err
	}

	artistVariants := searchTermVariants(artist)
	titleVariants := searchTermVariants(title)
	matches := make([]navidromeSong, 0)
	for _, song := range payload.SubsonicResponse.SearchResult3.Song {
		if searchVariantSetExactMatch(searchTermVariants(song.Artist), artistVariants) &&
			searchVariantSetExactMatch(searchTermVariants(song.Title), titleVariants) {
			matches = append(matches, song)
		}
	}
	if len(matches) > 0 {
		return matches, nil
	}
	for _, song := range payload.SubsonicResponse.SearchResult3.Song {
		artistMatch := searchVariantSetLooseMatch(searchTermVariants(song.Artist), artistVariants)
		titleMatch := searchVariantSetLooseMatch(searchTermVariants(song.Title), titleVariants)
		if artistMatch && titleMatch {
			matches = append(matches, song)
		}
	}
	return matches, nil
}

func (c *navidromeClient) GetPlaylists(ctx context.Context) ([]navidromePlaylist, error) {
	params := c.authParams()
	var payload struct {
		SubsonicResponse struct {
			Playlists struct {
				Playlist []navidromePlaylist `json:"playlist"`
			} `json:"playlists"`
		} `json:"subsonic-response"`
	}
	if err := c.doGET(ctx, "getPlaylists", params, &payload); err != nil {
		return nil, err
	}
	return payload.SubsonicResponse.Playlists.Playlist, nil
}

func (c *navidromeClient) GetPlaylist(ctx context.Context, playlistID string) (*navidromePlaylistDetail, error) {
	params := c.authParams()
	params.Set("id", playlistID)
	var payload struct {
		SubsonicResponse struct {
			Playlist navidromePlaylistDetail `json:"playlist"`
		} `json:"subsonic-response"`
	}
	if err := c.doGET(ctx, "getPlaylist", params, &payload); err != nil {
		return nil, err
	}
	if strings.TrimSpace(payload.SubsonicResponse.Playlist.ID) == "" {
		return nil, nil
	}
	return &payload.SubsonicResponse.Playlist, nil
}

func (c *navidromeClient) GetPlaylistByName(ctx context.Context, name string) (*navidromePlaylist, error) {
	playlists, err := c.GetPlaylists(ctx)
	if err != nil {
		return nil, err
	}
	normName := normalizeSearchTerm(name)
	for _, p := range playlists {
		if normalizeSearchTerm(p.Name) == normName {
			return &p, nil
		}
	}
	return nil, nil
}

func (c *navidromeClient) CreatePlaylist(ctx context.Context, name string, songIDs []string) (string, error) {
	params := c.authParams()
	params.Set("name", name)
	for _, songID := range songIDs {
		params.Add("songId", songID)
	}
	var payload struct {
		SubsonicResponse struct {
			Playlist struct {
				ID string `json:"id"`
			} `json:"playlist"`
		} `json:"subsonic-response"`
	}
	if err := c.doGET(ctx, "createPlaylist", params, &payload); err != nil {
		return "", err
	}
	if payload.SubsonicResponse.Playlist.ID == "" {
		return "", fmt.Errorf("playlist create returned empty playlist id")
	}
	return payload.SubsonicResponse.Playlist.ID, nil
}

func (c *navidromeClient) UpdatePlaylistAddSongs(ctx context.Context, playlistID string, songIDs []string) error {
	params := c.authParams()
	params.Set("playlistId", playlistID)
	for _, songID := range songIDs {
		params.Add("songIdToAdd", songID)
	}
	var payload map[string]interface{}
	return c.doGET(ctx, "updatePlaylist", params, &payload)
}

func (c *navidromeClient) UpdatePlaylistName(ctx context.Context, playlistID, name string) error {
	playlistID = strings.TrimSpace(playlistID)
	name = strings.TrimSpace(name)
	if playlistID == "" || name == "" {
		return nil
	}
	params := c.authParams()
	params.Set("playlistId", playlistID)
	params.Set("name", name)
	var payload map[string]interface{}
	return c.doGET(ctx, "updatePlaylist", params, &payload)
}

func (c *navidromeClient) GetPlaylistSongIDs(ctx context.Context, playlistID string) ([]string, error) {
	playlist, err := c.GetPlaylist(ctx, playlistID)
	if err != nil {
		return nil, err
	}
	if playlist == nil {
		return nil, nil
	}
	ids := make([]string, 0, len(playlist.Entries))
	for _, entry := range playlist.Entries {
		if strings.TrimSpace(entry.ID) != "" {
			ids = append(ids, strings.TrimSpace(entry.ID))
		}
	}
	return ids, nil
}

func (c *navidromeClient) UpdatePlaylistRemoveIndexes(ctx context.Context, playlistID string, indexes []int) error {
	if strings.TrimSpace(playlistID) == "" || len(indexes) == 0 {
		return nil
	}
	params := c.authParams()
	params.Set("playlistId", playlistID)
	sort.Ints(indexes)
	for i := len(indexes) - 1; i >= 0; i-- {
		params.Add("songIndexToRemove", strconv.Itoa(indexes[i]))
	}
	var payload map[string]interface{}
	return c.doGET(ctx, "updatePlaylist", params, &payload)
}

var bracketedSearchMetadataPattern = regexp.MustCompile(`[\(\[][^\)\]]*[\)\]]`)

func normalizeSearchTerm(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	raw = strings.ReplaceAll(raw, "&", " and ")
	decomposed := norm.NFD.String(raw)
	var b strings.Builder
	b.Grow(len(decomposed))
	spacePending := false
	for _, r := range decomposed {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if spacePending && b.Len() > 0 {
				b.WriteByte(' ')
			}
			spacePending = false
			b.WriteRune(r)
		default:
			spacePending = true
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func containsSimilar(a, b string) bool {
	if len(a) < 3 || len(b) < 3 {
		return false
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	return len(a) >= 3 && strings.Contains(b, a)
}

func searchTermVariants(raw string) []string {
	candidates := []string{
		raw,
		bracketedSearchMetadataPattern.ReplaceAllString(raw, " "),
		stripFeaturedArtists(raw),
		stripFeaturedArtists(bracketedSearchMetadataPattern.ReplaceAllString(raw, " ")),
	}
	seen := make(map[string]struct{}, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		normalized := normalizeSearchTerm(candidate)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func stripFeaturedArtists(raw string) string {
	lower := strings.ToLower(raw)
	for _, marker := range []string{" feat. ", " feat ", " featuring ", " ft. ", " ft "} {
		if idx := strings.Index(lower, marker); idx >= 0 {
			return raw[:idx]
		}
	}
	return raw
}

func searchVariantSetExactMatch(a, b []string) bool {
	for _, left := range a {
		for _, right := range b {
			if left == right {
				return true
			}
		}
	}
	return false
}

func searchVariantSetLooseMatch(a, b []string) bool {
	for _, left := range a {
		for _, right := range b {
			if left == right || containsSimilar(left, right) || containsSimilar(right, left) {
				return true
			}
		}
	}
	return false
}

func buildDefaultPlaylistName(prompt string) string {
	base := strings.TrimSpace(prompt)
	if base == "" {
		return "Discover: Mixed"
	}
	words := strings.Fields(base)
	if len(words) > 6 {
		words = words[:6]
	}
	name := strings.Join(words, " ")
	if len(name) > 40 {
		name = strings.TrimSpace(name[:40])
	}
	return "Discover: " + name
}

func planDiscoverPlaylist(ctx context.Context, args map[string]interface{}) ([]playlistCandidateTrack, map[string]interface{}, error) {
	prompt := strings.TrimSpace(toolStringArg(args, "prompt"))
	if prompt == "" {
		return nil, nil, fmt.Errorf("prompt is required")
	}
	trackCount := toolIntArg(args, "trackCount", 20)
	if trackCount < 1 {
		trackCount = 20
	}
	if trackCount > 40 {
		trackCount = 40
	}
	playlistName := strings.TrimSpace(toolStringArg(args, "playlistName"))
	if playlistName == "" {
		playlistName = buildDefaultPlaylistName(prompt)
	}
	personalized := false
	if val := toolOptBoolArg(args, "personalized"); val != nil {
		personalized = *val
	}

	groqKey := strings.TrimSpace(os.Getenv("GROQ_API_KEY"))
	if groqKey == "" {
		return nil, nil, fmt.Errorf("GROQ_API_KEY is not configured")
	}
	groqModel := strings.TrimSpace(os.Getenv("GROQ_MODEL"))
	if groqModel == "" {
		groqModel = agent.DefaultGroqModel
	}

	systemPrompt := `You are a music playlist planner.
Return strict JSON only.

Output schema:
{
  "normalizedIntent": "string",
  "playlistName": "string",
  "tracks": [
    {
      "artistName": "Artist",
      "trackTitle": "Track",
      "reason": "Short reason",
      "sourceHint": "Optional short hint"
    }
  ]
}

Rules:
- Return exactly the requested number of tracks when possible.
- Avoid duplicate tracks.
- Avoid live versions, remasters, and alternate takes unless the prompt asks for them.
- Keep reasons concise, under 15 words.
- No markdown. JSON object only.`

	userPrompt := fmt.Sprintf(
		`Prompt: %s
Requested track count: %d
Playlist name: %s
Personalized: %t

Generate a coherent playlist track list.`,
		prompt, trackCount, playlistName, personalized,
	)

	raw, err := callGroqJSON(ctx, groqKey, groqModel, systemPrompt, userPrompt, 1400)
	if err != nil {
		return nil, nil, err
	}

	var parsed struct {
		NormalizedIntent string                   `json:"normalizedIntent"`
		PlaylistName     string                   `json:"playlistName"`
		Tracks           []playlistCandidateTrack `json:"tracks"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, nil, fmt.Errorf("failed to parse planner response: %w", err)
	}

	seen := make(map[string]struct{}, len(parsed.Tracks))
	candidates := make([]playlistCandidateTrack, 0, trackCount)
	for _, item := range parsed.Tracks {
		artistName := strings.TrimSpace(item.ArtistName)
		trackTitle := strings.TrimSpace(item.TrackTitle)
		if artistName == "" || trackTitle == "" {
			continue
		}
		key := normalizeSearchTerm(artistName) + "::" + normalizeSearchTerm(trackTitle)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		item.Rank = len(candidates) + 1
		item.ArtistName = artistName
		item.TrackTitle = trackTitle
		item.Reason = strings.TrimSpace(item.Reason)
		item.SourceHint = strings.TrimSpace(item.SourceHint)
		candidates = append(candidates, item)
		if len(candidates) >= trackCount {
			break
		}
	}
	if len(candidates) == 0 {
		return nil, nil, fmt.Errorf("planner returned no usable track candidates")
	}

	if strings.TrimSpace(parsed.PlaylistName) != "" {
		playlistName = strings.TrimSpace(parsed.PlaylistName)
	}
	setLastPlannedPlaylist(chatSessionIDFromContext(ctx), prompt, playlistName, candidates)
	return candidates, map[string]interface{}{
		"prompt":           prompt,
		"playlistName":     playlistName,
		"normalizedIntent": strings.TrimSpace(parsed.NormalizedIntent),
		"trackCount":       len(candidates),
		"personalized":     personalized,
	}, nil
}

func selectPlaylistCandidates(candidates []playlistCandidateTrack, selection string) ([]playlistCandidateTrack, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no planned playlist candidates found; run planDiscoverPlaylist first")
	}
	selection = strings.TrimSpace(selection)
	if selection == "" || selection == "all" {
		return candidates, nil
	}
	lower := strings.ToLower(selection)
	if n, ok := parseLeadingCountSelection(lower); ok {
		if n > len(candidates) {
			n = len(candidates)
		}
		if n <= 0 {
			return nil, fmt.Errorf("selection resolved to zero candidates")
		}
		return candidates[:n], nil
	}
	matches := make([]playlistCandidateTrack, 0)
	for _, c := range candidates {
		artistNorm := normalizeSearchTerm(c.ArtistName)
		trackNorm := normalizeSearchTerm(c.TrackTitle)
		needle := normalizeSearchTerm(lower)
		if strings.Contains(artistNorm, needle) || strings.Contains(trackNorm, needle) {
			matches = append(matches, c)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("selection did not match any planned tracks")
	}
	return matches, nil
}

func parseLeadingCountSelection(selection string) (int, bool) {
	fields := strings.Fields(selection)
	if len(fields) < 2 {
		return 0, false
	}
	switch fields[0] {
	case "first", "top":
	default:
		return 0, false
	}
	n, err := strconv.Atoi(fields[1])
	if err == nil {
		return n, true
	}
	wordToNum := map[string]int{
		"one":   1,
		"two":   2,
		"three": 3,
		"four":  4,
		"five":  5,
		"six":   6,
		"seven": 7,
		"eight": 8,
	}
	n, ok := wordToNum[fields[1]]
	return n, ok
}

func resolvePlaylistCandidates(ctx context.Context, client *navidromeClient, selected []playlistCandidateTrack) ([]resolvedPlaylistTrack, error) {
	out := make([]resolvedPlaylistTrack, 0, len(selected))
	for _, candidate := range selected {
		item := resolvedPlaylistTrack{
			Rank:       candidate.Rank,
			ArtistName: candidate.ArtistName,
			TrackTitle: candidate.TrackTitle,
		}
		matches, err := client.SearchTrackByArtistTitle(ctx, candidate.ArtistName, candidate.TrackTitle)
		if err != nil {
			item.Status = "error"
			item.Detail = err.Error()
			out = append(out, item)
			continue
		}
		item.MatchCount = len(matches)
		switch len(matches) {
		case 0:
			item.Status = "missing"
		case 1:
			item.Status = "available"
			item.SongID = matches[0].ID
			item.MatchedArtist = matches[0].Artist
			item.MatchedTitle = matches[0].Title
		default:
			item.Status = "ambiguous"
			item.MatchedArtist = matches[0].Artist
			item.MatchedTitle = matches[0].Title
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Rank < out[j].Rank })
	return out, nil
}

func resolvePlaylistTracks(ctx context.Context, args map[string]interface{}) ([]resolvedPlaylistTrack, map[string]interface{}, error) {
	sessionID := chatSessionIDFromContext(ctx)
	_, playlistName, plannedAt, candidates, _, _, ok := loadTurnSessionMemory(sessionID).PlaylistContext()
	if !ok {
		return nil, nil, fmt.Errorf("no playlist plan available")
	}
	selected, err := selectPlaylistCandidates(candidates, toolStringArg(args, "selection"))
	if err != nil {
		return nil, nil, err
	}
	client, err := newNavidromeClientFromEnv()
	if err != nil {
		return nil, nil, err
	}
	resolved, err := resolvePlaylistCandidates(ctx, client, selected)
	if err != nil {
		return nil, nil, err
	}
	setLastResolvedPlaylist(sessionID, resolved)

	available := 0
	missing := 0
	ambiguous := 0
	errors := 0
	for _, item := range resolved {
		switch item.Status {
		case "available":
			available++
		case "missing":
			missing++
		case "ambiguous":
			ambiguous++
		default:
			errors++
		}
	}
	return resolved, map[string]interface{}{
		"playlistName": playlistName,
		"plannedAt":    plannedAt.Format(time.RFC3339),
		"resolvedAt":   time.Now().Format(time.RFC3339),
		"selection":    strings.TrimSpace(toolStringArg(args, "selection")),
		"counts": map[string]int{
			"total":     len(resolved),
			"available": available,
			"missing":   missing,
			"ambiguous": ambiguous,
			"errors":    errors,
		},
	}, nil
}

func queueMissingPlaylistTracks(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	confirm := false
	if val := toolOptBoolArg(args, "confirm"); val != nil {
		confirm = *val
	}
	if !confirm {
		return nil, fmt.Errorf("queueing requires confirm=true")
	}

	sessionID := chatSessionIDFromContext(ctx)
	_, playlistName, _, _, _, cached, ok := loadTurnSessionMemory(sessionID).PlaylistContext()
	if !ok {
		return nil, fmt.Errorf("no playlist plan available")
	}
	selection := strings.TrimSpace(toolStringArg(args, "selection"))
	var resolved []resolvedPlaylistTrack
	if selection == "" || strings.EqualFold(selection, "all") {
		resolved = cached
	}
	if len(resolved) == 0 {
		_, _, _, candidates, _, _, ok := loadTurnSessionMemory(sessionID).PlaylistContext()
		if !ok {
			return nil, fmt.Errorf("no playlist plan available")
		}
		selected, err := selectPlaylistCandidates(candidates, selection)
		if err != nil {
			return nil, err
		}
		for _, item := range selected {
			resolved = append(resolved, resolvedPlaylistTrack{
				Rank:       item.Rank,
				ArtistName: item.ArtistName,
				TrackTitle: item.TrackTitle,
				Status:     "missing",
			})
		}
	}

	missing := make([]resolvedPlaylistTrack, 0)
	for _, item := range resolved {
		if item.Status == "missing" {
			missing = append(missing, item)
		}
	}
	if len(missing) == 0 {
		return nil, fmt.Errorf("no missing tracks available to queue")
	}

	queued, queueFile, itemID, err := enqueuePlaylistReconcileTracks(playlistName, missing)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"queueFile":        queueFile,
		"queued":           queued,
		"selection":        selection,
		"reconcileJobFile": itemID,
	}, nil
}

func createDiscoveredPlaylist(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	confirm := false
	if val := toolOptBoolArg(args, "confirm"); val != nil {
		confirm = *val
	}
	if !confirm {
		return nil, fmt.Errorf("playlist creation requires confirm=true")
	}

	sessionID := chatSessionIDFromContext(ctx)
	_, defaultPlaylistName, _, candidates, _, _, ok := loadTurnSessionMemory(sessionID).PlaylistContext()
	if !ok {
		return nil, fmt.Errorf("no playlist plan available")
	}
	selected, err := selectPlaylistCandidates(candidates, toolStringArg(args, "selection"))
	if err != nil {
		return nil, err
	}

	playlistName := strings.TrimSpace(toolStringArg(args, "playlistName"))
	if playlistName == "" {
		playlistName = defaultPlaylistName
	}
	if playlistName == "" {
		playlistName = "Discover: Mixed"
	}

	client, err := newNavidromeClientFromEnv()
	if err != nil {
		return nil, err
	}
	resolved, err := resolvePlaylistCandidates(ctx, client, selected)
	if err != nil {
		return nil, err
	}
	setLastResolvedPlaylist(sessionID, resolved)

	uniqueSongIDs := make([]string, 0, len(resolved))
	seen := make(map[string]struct{}, len(resolved))
	for _, item := range resolved {
		if item.Status != "available" || strings.TrimSpace(item.SongID) == "" {
			continue
		}
		if _, ok := seen[item.SongID]; ok {
			continue
		}
		seen[item.SongID] = struct{}{}
		uniqueSongIDs = append(uniqueSongIDs, item.SongID)
	}
	result := map[string]interface{}{
		"playlistName":    playlistName,
		"requestedTracks": len(selected),
		"resolvedTracks":  len(uniqueSongIDs),
	}
	if len(uniqueSongIDs) == 0 {
		existingPlaylist, err := client.GetPlaylistByName(ctx, playlistName)
		if err != nil {
			return nil, err
		}
		if existingPlaylist == nil {
			return nil, fmt.Errorf("no available tracks resolved in Navidrome; refusing to create empty playlist")
		}
		result["action"] = "updated"
		result["existing"] = existingPlaylist.SongCount
		result["added"] = 0
		return result, nil
	}
	action, added, existing, err := upsertPlaylistSongs(ctx, client, playlistName, uniqueSongIDs)
	if err != nil {
		return nil, err
	}
	result["action"] = action
	result["existing"] = existing
	result["added"] = added
	return result, nil
}
