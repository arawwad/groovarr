package lidarr

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
	"time"

	"groovarr/internal/discovery"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

type Album struct {
	ID         int    `json:"id"`
	Title      string `json:"title"`
	ArtistName string `json:"artistName"`
	Monitored  bool   `json:"monitored"`
	Path       string `json:"path"`
	Added      string `json:"added"`
	Artist     struct {
		ArtistName string `json:"artistName"`
	} `json:"artist"`
}

type WantedRecord struct {
	AlbumID    int    `json:"albumId"`
	Title      string `json:"title"`
	ArtistName string `json:"artistName"`
	Monitored  bool   `json:"monitored"`
	Artist     struct {
		ArtistName string `json:"artistName"`
	} `json:"artist"`
}

type CleanupCandidate struct {
	AlbumID           int    `json:"albumId"`
	ArtistName        string `json:"artistName"`
	Title             string `json:"title"`
	Monitored         bool   `json:"monitored"`
	Path              string `json:"path,omitempty"`
	Reason            string `json:"reason"`
	RecommendedAction string `json:"recommendedAction"`
}

type CleanupApplyItem struct {
	AlbumID int    `json:"albumId"`
	Status  string `json:"status"`
	Detail  string `json:"detail,omitempty"`
}

type ArtistSearchResult struct {
	ID              int      `json:"id"`
	ArtistName      string   `json:"artistName"`
	ForeignArtistID string   `json:"foreignArtistId"`
	Genres          []string `json:"genres"`
}

type Artist struct {
	ID              int    `json:"id"`
	ArtistName      string `json:"artistName"`
	ForeignArtistID string `json:"foreignArtistId"`
	Monitored       bool   `json:"monitored"`
}

type RootFolder struct {
	Path string `json:"path"`
}

type AlbumSearchResult struct {
	ID              int    `json:"id"`
	Title           string `json:"title"`
	ArtistID        int    `json:"artistId"`
	ArtistName      string `json:"artistName"`
	ForeignAlbumID  string `json:"foreignAlbumId"`
	ForeignArtistID string `json:"foreignArtistId"`
	ReleaseDate     string `json:"releaseDate"`
	Monitored       bool   `json:"monitored"`
	Ratings         struct {
		Value float64 `json:"value"`
	} `json:"ratings"`
	Artist struct {
		ArtistName string `json:"artistName"`
	} `json:"artist"`
}

type wantedPage struct {
	Records []WantedRecord `json:"records"`
}

func NewFromEnv() (*Client, error) {
	baseURL := strings.TrimSpace(os.Getenv("LIDARR_URL"))
	apiKey := strings.TrimSpace(os.Getenv("LIDARR_API_KEY"))
	if baseURL == "" || apiKey == "" {
		return nil, fmt.Errorf("lidarr is not configured (LIDARR_URL and LIDARR_API_KEY are required)")
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 20 * time.Second},
	}, nil
}

func (c *Client) DoJSON(ctx context.Context, method, path string, body interface{}, out interface{}) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("lidarr API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) GetAlbums(ctx context.Context) ([]Album, error) {
	var albums []Album
	if err := c.DoJSON(ctx, http.MethodGet, "/api/v1/album", nil, &albums); err != nil {
		return nil, err
	}
	for i := range albums {
		if strings.TrimSpace(albums[i].ArtistName) == "" {
			albums[i].ArtistName = strings.TrimSpace(albums[i].Artist.ArtistName)
		}
	}
	return albums, nil
}

func (c *Client) GetWantedMissing(ctx context.Context, limit int) ([]WantedRecord, error) {
	path := "/api/v1/wanted/missing?page=1&pageSize=" + strconv.Itoa(limit)
	var page wantedPage
	if err := c.DoJSON(ctx, http.MethodGet, path, nil, &page); err != nil {
		return nil, err
	}
	for i := range page.Records {
		if strings.TrimSpace(page.Records[i].ArtistName) == "" {
			page.Records[i].ArtistName = strings.TrimSpace(page.Records[i].Artist.ArtistName)
		}
	}
	return page.Records, nil
}

func (c *Client) GetWantedCutoff(ctx context.Context, limit int) ([]WantedRecord, error) {
	path := "/api/v1/wanted/cutoff?page=1&pageSize=" + strconv.Itoa(limit)
	var page wantedPage
	if err := c.DoJSON(ctx, http.MethodGet, path, nil, &page); err != nil {
		return nil, err
	}
	for i := range page.Records {
		if strings.TrimSpace(page.Records[i].ArtistName) == "" {
			page.Records[i].ArtistName = strings.TrimSpace(page.Records[i].Artist.ArtistName)
		}
	}
	return page.Records, nil
}

func (c *Client) UnmonitorAlbums(ctx context.Context, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	return c.DoJSON(ctx, http.MethodPut, "/api/v1/album/monitor", map[string]interface{}{
		"albumIds":  ids,
		"monitored": false,
	}, nil)
}

func (c *Client) DeleteAlbum(ctx context.Context, albumID int) error {
	q := url.Values{}
	q.Set("deleteFiles", "true")
	q.Set("addImportListExclusion", "false")
	path := fmt.Sprintf("/api/v1/album/%d?%s", albumID, q.Encode())
	return c.DoJSON(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) DeleteArtist(ctx context.Context, artistID int) error {
	q := url.Values{}
	q.Set("deleteFiles", "true")
	q.Set("addImportListExclusion", "false")
	path := fmt.Sprintf("/api/v1/artist/%d?%s", artistID, q.Encode())
	return c.DoJSON(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) AlbumSearch(ctx context.Context, albumID int) error {
	return c.DoJSON(ctx, http.MethodPost, "/api/v1/command", map[string]interface{}{
		"name":     "AlbumSearch",
		"albumIds": []int{albumID},
	}, nil)
}

func (c *Client) RefreshAlbum(ctx context.Context, albumID int) error {
	return c.DoJSON(ctx, http.MethodPost, "/api/v1/command", map[string]interface{}{
		"name":    "RefreshAlbum",
		"albumId": albumID,
	}, nil)
}

func (c *Client) SearchArtist(ctx context.Context, name string) ([]ArtistSearchResult, error) {
	path := "/api/v1/artist/lookup?term=" + urlQueryEscape(name)
	var results []ArtistSearchResult
	if err := c.DoJSON(ctx, http.MethodGet, path, nil, &results); err != nil {
		return nil, err
	}
	if len(results) > 5 {
		results = results[:5]
	}
	return results, nil
}

func (c *Client) GetArtists(ctx context.Context) ([]Artist, error) {
	var artists []Artist
	if err := c.DoJSON(ctx, http.MethodGet, "/api/v1/artist", nil, &artists); err != nil {
		return nil, err
	}
	return artists, nil
}

func (c *Client) FindExistingArtist(ctx context.Context, artistName string) (*Artist, error) {
	existingArtists, err := c.GetArtists(ctx)
	if err != nil {
		return nil, err
	}
	if artist := SelectExistingArtist(artistName, existingArtists, nil); artist != nil {
		return artist, nil
	}

	results, err := c.SearchArtist(ctx, artistName)
	if err != nil {
		return nil, err
	}
	return SelectExistingArtist(artistName, existingArtists, results), nil
}

func (c *Client) GetRootFolders(ctx context.Context) ([]RootFolder, error) {
	var folders []RootFolder
	if err := c.DoJSON(ctx, http.MethodGet, "/api/v1/rootfolder", nil, &folders); err != nil {
		return nil, err
	}
	return folders, nil
}

func (c *Client) SearchAlbumsByArtist(ctx context.Context, artistID int, albumTitle string) ([]AlbumSearchResult, error) {
	path := "/api/v1/album?artistId=" + strconv.Itoa(artistID)
	var results []AlbumSearchResult
	if err := c.DoJSON(ctx, http.MethodGet, path, nil, &results); err != nil {
		return nil, err
	}
	target := discovery.NormalizeTitle(albumTitle)
	exact := make([]AlbumSearchResult, 0)
	contains := make([]AlbumSearchResult, 0)
	for i := range results {
		if strings.TrimSpace(results[i].ArtistName) == "" {
			results[i].ArtistName = strings.TrimSpace(results[i].Artist.ArtistName)
		}
		normTitle := discovery.NormalizeTitle(results[i].Title)
		if normTitle == target {
			exact = append(exact, results[i])
			continue
		}
		if target != "" && strings.Contains(normTitle, target) {
			contains = append(contains, results[i])
		}
	}
	if len(exact) > 0 {
		return exact, nil
	}
	return contains, nil
}

func (c *Client) SearchAlbumLookup(ctx context.Context, artistName, albumTitle string) ([]AlbumSearchResult, error) {
	terms := uniqueLookupTerms(
		artistName+" "+albumTitle,
		albumTitle+" "+artistName,
		albumTitle,
	)
	var best []AlbumSearchResult
	for _, term := range terms {
		path := "/api/v1/album/lookup?term=" + urlQueryEscape(term)
		var results []AlbumSearchResult
		if err := c.DoJSON(ctx, http.MethodGet, path, nil, &results); err != nil {
			return nil, err
		}
		filtered := filterAlbumLookupCandidates(results, artistName, albumTitle)
		if len(filtered) > 0 {
			best = filtered
			break
		}
	}
	return best, nil
}

func BestArtistResult(results []ArtistSearchResult, searchTerm string) *ArtistSearchResult {
	if len(results) == 0 {
		return nil
	}
	searchTerm = discovery.NormalizeTitle(searchTerm)
	bestIndex := -1
	bestScore := -1
	for i := range results {
		score := 0
		name := discovery.NormalizeTitle(results[i].ArtistName)
		if name == "" || strings.TrimSpace(results[i].ForeignArtistID) == "" {
			continue
		}
		if name == searchTerm {
			score += 1000
		} else if strings.Contains(name, searchTerm) || strings.Contains(searchTerm, name) {
			score += 500
		}
		score += len(results[i].Genres) * 10
		if i == 0 {
			score += 50
		}
		if score > bestScore {
			bestScore = score
			bestIndex = i
		}
	}
	if bestIndex < 0 {
		return nil
	}
	return &results[bestIndex]
}

func SelectExistingArtist(searchTerm string, existing []Artist, results []ArtistSearchResult) *Artist {
	trimmedSearch := strings.TrimSpace(searchTerm)
	searchNorm := discovery.NormalizeTitle(searchTerm)
	for i := range existing {
		if strings.EqualFold(strings.TrimSpace(existing[i].ArtistName), trimmedSearch) ||
			discovery.NormalizeTitle(existing[i].ArtistName) == searchNorm {
			artist := existing[i]
			return &artist
		}
	}

	best := BestArtistResult(results, searchTerm)
	if best == nil {
		return nil
	}
	for i := range existing {
		if strings.TrimSpace(existing[i].ForeignArtistID) == "" {
			continue
		}
		if existing[i].ForeignArtistID == best.ForeignArtistID {
			artist := existing[i]
			return &artist
		}
	}
	return nil
}

func BestAlbumResult(results []AlbumSearchResult) (*AlbumSearchResult, bool) {
	if len(results) == 0 {
		return nil, false
	}
	best := results[0]
	if strings.TrimSpace(best.Title) == "" || strings.TrimSpace(best.ForeignAlbumID) == "" {
		return nil, false
	}
	ambiguous := len(results) > 1
	return &best, ambiguous
}

func (c *Client) EnsureArtistPresent(ctx context.Context, artistName string, dryRun bool) (*Artist, bool, error) {
	results, err := c.SearchArtist(ctx, artistName)
	if err != nil {
		return nil, false, fmt.Errorf("artist lookup failed: %w", err)
	}
	best := BestArtistResult(results, artistName)
	if best == nil {
		return nil, false, fmt.Errorf("artist %q not found in library lookup", artistName)
	}

	existingArtists, err := c.GetArtists(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("failed to load existing artists: %w", err)
	}
	for _, artist := range existingArtists {
		if artist.ForeignArtistID == best.ForeignArtistID || strings.EqualFold(artist.ArtistName, best.ArtistName) {
			return &artist, false, nil
		}
	}

	if dryRun {
		return &Artist{
			ID:              0,
			ArtistName:      best.ArtistName,
			ForeignArtistID: best.ForeignArtistID,
			Monitored:       false,
		}, true, nil
	}

	addedArtist, err := c.AddArtist(ctx, best.ForeignArtistID, best.ArtistName)
	if err != nil {
		return nil, false, err
	}
	return addedArtist, true, nil
}

func (c *Client) AddArtist(ctx context.Context, foreignArtistID, artistName string) (*Artist, error) {
	rootFolderPath := strings.TrimSpace(os.Getenv("LIDARR_ROOT_FOLDER_PATH"))
	if rootFolderPath == "" {
		folders, err := c.GetRootFolders(ctx)
		if err == nil {
			for _, f := range folders {
				if strings.TrimSpace(f.Path) != "" {
					rootFolderPath = strings.TrimSpace(f.Path)
					break
				}
			}
		}
	}
	if rootFolderPath == "" {
		return nil, fmt.Errorf("library root folder path is not configured")
	}
	qualityProfileID := envInt("LIDARR_QUALITY_PROFILE_ID", 1)
	metadataProfileID := envInt("LIDARR_METADATA_PROFILE_ID", 1)

	body := map[string]interface{}{
		"foreignArtistId":   foreignArtistID,
		"artistName":        artistName,
		"monitored":         false,
		"rootFolderPath":    rootFolderPath,
		"qualityProfileId":  qualityProfileID,
		"metadataProfileId": metadataProfileID,
		"addOptions": map[string]interface{}{
			"monitor":                "none",
			"searchForMissingAlbums": false,
		},
	}

	var added Artist
	if err := c.DoJSON(ctx, http.MethodPost, "/api/v1/artist", body, &added); err != nil {
		return nil, fmt.Errorf("failed to add artist %q to your library: %s", artistName, SanitizeApplyError(err))
	}
	return &added, nil
}

func SanitizeApplyError(err error) string {
	if err == nil {
		return ""
	}
	raw := strings.TrimSpace(err.Error())
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "parameter 'path'"),
		strings.Contains(lower, "string can't be left empty"),
		strings.Contains(lower, "root folder"):
		return "Your library root folder path is missing."
	case strings.Contains(lower, "api returned 500"):
		return "Your library backend returned an internal error."
	case strings.Contains(lower, "api returned 4"):
		return "Your library backend rejected the request."
	default:
		return "Library request failed."
	}
}

func (c *Client) FindAlbumForArtist(ctx context.Context, artistID int, artistName, albumTitle string) (*AlbumSearchResult, bool, error) {
	results, err := c.SearchAlbumsByArtist(ctx, artistID, albumTitle)
	if err != nil {
		return nil, false, err
	}
	if len(results) == 0 {
		results, err = c.SearchAlbumLookup(ctx, artistName, albumTitle)
		if err != nil {
			return nil, false, err
		}
	}
	best, ambiguous := BestAlbumResult(results)
	return best, ambiguous, nil
}

func (c *Client) MonitorAlbumByID(ctx context.Context, album *AlbumSearchResult) error {
	if album == nil {
		return fmt.Errorf("album is required")
	}
	if album.Monitored {
		return nil
	}

	body := map[string]interface{}{
		"id":              album.ID,
		"title":           album.Title,
		"artistId":        album.ArtistID,
		"artistName":      album.ArtistName,
		"foreignAlbumId":  album.ForeignAlbumID,
		"foreignArtistId": album.ForeignArtistID,
		"monitored":       true,
		"releaseDate":     album.ReleaseDate,
	}
	return c.DoJSON(ctx, http.MethodPut, fmt.Sprintf("/api/v1/album/%d", album.ID), body, nil)
}

func uniqueLookupTerms(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func filterAlbumLookupCandidates(candidates []AlbumSearchResult, artistName, albumTitle string) []AlbumSearchResult {
	artistNorm := discovery.NormalizeTitle(artistName)
	albumNorm := discovery.NormalizeTitle(albumTitle)

	type scored struct {
		item  AlbumSearchResult
		score float64
	}
	best := make([]scored, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.Title) == "" {
			continue
		}
		if strings.TrimSpace(candidate.ArtistName) == "" {
			candidate.ArtistName = strings.TrimSpace(candidate.Artist.ArtistName)
		}
		if strings.TrimSpace(candidate.ArtistName) == "" {
			continue
		}
		artistScore := discovery.TitleSimilarity(artistNorm, discovery.NormalizeTitle(candidate.ArtistName))
		titleScore := discovery.TitleSimilarity(albumNorm, discovery.NormalizeTitle(candidate.Title))
		if artistScore < 0.70 || titleScore < 0.45 {
			continue
		}
		best = append(best, scored{
			item:  candidate,
			score: (0.6 * artistScore) + (0.4 * titleScore),
		})
	}
	if len(best) == 0 {
		return nil
	}
	sort.Slice(best, func(i, j int) bool {
		return best[i].score > best[j].score
	})
	topScore := best[0].score
	out := make([]AlbumSearchResult, 0, len(best))
	for _, candidate := range best {
		if candidate.score+0.02 >= topScore {
			out = append(out, candidate.item)
		}
	}
	return out
}

func envInt(name string, defaultVal int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return defaultVal
	}
	return v
}

func urlQueryEscape(raw string) string {
	replacer := strings.NewReplacer(
		"%", "%25",
		" ", "%20",
		"\"", "%22",
		"#", "%23",
		"&", "%26",
		"+", "%2B",
		"/", "%2F",
		":", "%3A",
		";", "%3B",
		"=", "%3D",
		"?", "%3F",
	)
	return replacer.Replace(strings.TrimSpace(raw))
}
