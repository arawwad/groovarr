package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"groovarr/internal/toolspec"

	"github.com/rs/zerolog/log"
)

type ToolExecutor func(ctx context.Context, tool string, args map[string]interface{}) (string, error)

type Executor struct {
	groqKey             string
	groqModel           string
	huggingFaceKey      string
	toolExecute         ToolExecutor
	maxIterations       int
	maxCompletionTokens int
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionRequest struct {
	Model               string    `json:"model"`
	Messages            []Message `json:"messages"`
	MaxCompletionTokens int       `json:"max_completion_tokens,omitempty"`
}

type ChatCompletionResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		TotalTokens         int `json:"total_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

type AgentAction struct {
	Action   string                 `json:"action"`
	Tool     string                 `json:"tool,omitempty"`
	Args     map[string]interface{} `json:"args,omitempty"`
	Response string                 `json:"response,omitempty"`
}

type toolResultRenderer func(args map[string]interface{}, raw string) (string, bool)

const (
	defaultToolResultPromptChars = 2200
	defaultFailureResponse       = "I couldn't complete that request after multiple attempts."
	defaultRenderedListItems     = 8
	DefaultGroqModel             = "llama-3.3-70b-versatile"
	DefaultGroqKimiModel         = "moonshotai/kimi-k2-instruct-0905"
	DefaultHuggingFaceModel      = "hf:openai/gpt-oss-120b:cerebras"
	promptLayoutSplit            = "split"
	promptLayoutLegacy           = "legacy"

	invalidJSONOnlyInstruction = `Return valid JSON only. Use:
{"action":"query","tool":"albums","args":{"limit":10}}
or
{"action":"respond","response":"..."}`
	missingQueryToolInstruction = `Tool name is required when action="query".`
	missingResponseInstruction  = `response is required when action="respond".`
	unknownActionInstruction    = `Unknown action. Use "query" or "respond".`
)

func New(groqKey, groqModel, huggingFaceKey string, toolExecute ToolExecutor) *Executor {
	return &Executor{
		groqKey:             groqKey,
		groqModel:           groqModel,
		huggingFaceKey:      huggingFaceKey,
		toolExecute:         toolExecute,
		maxIterations:       envInt("AGENT_MAX_ITERATIONS", 4),
		maxCompletionTokens: envInt("AGENT_MAX_COMPLETION_TOKENS", 450),
	}
}

func IsDefaultFailureResponse(text string) bool {
	return strings.TrimSpace(text) == defaultFailureResponse
}

func (e *Executor) ProcessQuery(ctx context.Context, userMsg string, history []Message) (string, error) {
	return e.ProcessQueryWithModel(ctx, userMsg, history, "")
}

func (e *Executor) ProcessQueryWithModel(ctx context.Context, userMsg string, history []Message, modelOverride string) (string, error) {
	now := time.Now().UTC()
	var messages []Message
	switch promptLayout() {
	case promptLayoutLegacy:
		messages = buildConversation(buildLegacySystemPrompt(now), history, userMsg)
	default:
		messages = buildConversationWithRuntime(buildSystemPrompt(), buildRuntimeContext(now), history, userMsg)
	}
	provider, resolvedModel := resolveRequestedModel(modelOverride, e.groqModel)
	return e.runConversationLoop(ctx, provider, resolvedModel, messages)
}

func buildSystemPromptSections() []string {
	return []string{
		"You are a Groovarr assistant with database tools.",
		`Core behavior:
- Derive the user's intent from the latest message, prior chat history, and any server session context already present in history.
- For greetings, thanks, and other casual messages, respond conversationally without tools.
- If a request is ambiguous, the right tool is unclear, or required arguments are missing, ask one concise clarifying question instead of guessing.
- If results are empty, say so and suggest the next useful query.
- Use concrete dates and times for relative periods.
- Keep answers concise and natural.`,
		`Output contract:
- Always return strict JSON only.
- Use {"action":"query","tool":"<tool_name>","args":{...}} when you need data.
- Use {"action":"respond","response":"..."} when you can answer immediately.
- Never fabricate data.`,
		`Operational rules:
- Use only the tools listed in the tool manifest.
- Prefer tools over model memory for the user's library, listening history, playlists, pending state, discovered albums, or cleanup state.
- Do not answer library-stat or library-count questions from model memory. Use a tool or ask a clarifying question.
- For exact counts, prefer stats or facet tools over counting a capped list. Do not infer an exact total from a partial list result.
- Use chat history for follow-ups like "those", "them", "the last one", and "that playlist".
- Reuse prior artists or albums in follow-ups, and prefer multi-value tool args when available.
- Preserve the original subject when narrowing prior recommendation or semantic-search results, then add explicit filters.
- For decade/year follow-ups on semanticAlbumSearch, keep queryText and add minYear/maxYear.
- Recommendations are global by default. Use discoverAlbums unless the user explicitly limits them to their library.
- For "best/top/essential <artist>" prompts, use discoverAlbums unless the user says "in my library"; then use albums.
- For library-only vibe recommendations, prefer semanticAlbumSearch over albums or discoverAlbums.
- If the user already gave a mood, vibe, or scene, do not ask for another mood clarification unless a required filter is still missing.
- Do not invent tool names, arg names, filter keys, or enum values.
- If you cannot identify one best tool with valid arguments, ask a clarifying question.
- If a tool requires arguments you do not have yet, ask for them before calling the tool.
- If the user asks for vague "stats", ask whether they mean library composition or listening over time.
- Preview before state-changing operations. Use preview tools instead of inventing direct actions.
- Similarity tools are only for explicit nearest matches in the user's library, not general recommendations.`,
		`Clarification examples:
- User: "Give me artist stats."
  Assistant: {"action":"respond","response":"Do you want artist library stats or artist listening stats over a time window?"}
- User: "What are my top artists from the last month?"
  Assistant: {"action":"query","tool":"artistListeningStats","args":{"filter":{"playedSince":"2026-02-10","playedUntil":"2026-03-10"},"sort":"plays","limit":10}}
- User: "How many Pink Floyd albums are in my library?"
  Assistant: {"action":"query","tool":"artistLibraryStats","args":{"filter":{"artistName":"Pink Floyd"},"sort":"albums","limit":5}}
- User: "How many albums do Radiohead and The Beatles have in my library combined?"
  Assistant: {"action":"query","tool":"artistLibraryStats","args":{"filter":{"artistNames":["Radiohead","The Beatles"]},"sort":"albums","limit":10}}
- User: "Do I have any badly rated albums?"
  Assistant: {"action":"query","tool":"badlyRatedAlbums","args":{"limit":20,"maxTrackDetails":3}}
- User: "From those, give me three albums to revisit today."
  Assistant: {"action":"query","tool":"albums","args":{"artistNames":["Radiohead","The Beatles","Pink Floyd"],"sortBy":"rating","limit":12}}
- User: "Clean those from Lidarr."
  Assistant: {"action":"respond","response":"I can prepare a cleanup preview for those albums."}
- User: "Give me three records for a rainy late-night walk."
  Assistant: {"action":"query","tool":"discoverAlbums","args":{"query":"three records for a rainy late-night walk","limit":3}}
- User: "Best 5 Bjork albums."
  Assistant: {"action":"query","tool":"discoverAlbums","args":{"query":"best 5 Bjork albums","limit":5}}
- User: "Best 5 Bjork albums in my library."
  Assistant: {"action":"query","tool":"albums","args":{"artistName":"Bjork","sortBy":"rating","limit":5}}
- User: "Narrow that to the 90s."
  Assistant: {"action":"query","tool":"semanticAlbumSearch","args":{"queryText":"melancholic dream pop","minYear":1990,"maxYear":1999,"limit":6}}`,
		toolspec.RenderPromptCatalog(toolspec.PromptCatalog()),
	}
}

func buildSystemPrompt() string {
	return strings.Join(buildSystemPromptSections(), "\n\n")
}

func buildLegacySystemPrompt(now time.Time) string {
	sections := []string{
		buildSystemPromptSections()[0],
		fmt.Sprintf("Current date: %s", now.Format("Monday, January 2, 2006")),
	}
	sections = append(sections, buildSystemPromptSections()[1:]...)
	return strings.Join(sections, "\n\n")
}

func buildConversation(systemPrompt string, history []Message, userMsg string) []Message {
	messages := make([]Message, 0, len(history)+2)
	messages = append(messages, Message{Role: "system", Content: systemPrompt})
	messages = append(messages, history...)
	messages = append(messages, Message{Role: "user", Content: userMsg})
	return messages
}

func buildConversationWithRuntime(systemPrompt, runtimeContext string, history []Message, userMsg string) []Message {
	extra := 2
	if strings.TrimSpace(runtimeContext) != "" {
		extra++
	}
	messages := make([]Message, 0, len(history)+extra)
	messages = append(messages, Message{Role: "system", Content: systemPrompt})
	if strings.TrimSpace(runtimeContext) != "" {
		messages = append(messages, Message{Role: "assistant", Content: runtimeContext})
	}
	messages = append(messages, history...)
	messages = append(messages, Message{Role: "user", Content: userMsg})
	return messages
}

func buildRuntimeContext(now time.Time) string {
	return fmt.Sprintf("Authoritative runtime context:\nCurrent date: %s", now.Format("Monday, January 2, 2006"))
}

func promptLayout() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AGENT_PROMPT_LAYOUT"))) {
	case promptLayoutLegacy:
		return promptLayoutLegacy
	default:
		return promptLayoutSplit
	}
}

func resolveRequestedModel(modelOverride, defaultModel string) (string, string) {
	model := strings.TrimSpace(modelOverride)
	if model == "" {
		model = strings.TrimSpace(defaultModel)
	}
	return resolveModelProvider(model)
}

func (e *Executor) runConversationLoop(ctx context.Context, provider, resolvedModel string, messages []Message) (string, error) {
	for i := 0; i < e.maxIterations; i++ {
		response, nextModel, err := e.requestModelResponse(ctx, provider, resolvedModel, messages)
		if err != nil {
			return "", err
		}
		resolvedModel = nextModel

		action, err := parseAction(response)
		if err != nil {
			messages = appendUserMessage(messages, invalidJSONOnlyInstruction)
			continue
		}

		result, nextMessages, done := e.handleAction(ctx, response, action, messages)
		if done {
			return result, nil
		}
		messages = nextMessages
	}

	return defaultFailureResponse, nil
}

func (e *Executor) requestModelResponse(ctx context.Context, provider, resolvedModel string, messages []Message) (string, string, error) {
	response, err := e.callModel(ctx, provider, resolvedModel, messages)
	if err != nil {
		return "", resolvedModel, err
	}
	return response, resolvedModel, nil
}

func (e *Executor) handleAction(ctx context.Context, response string, action *AgentAction, messages []Message) (string, []Message, bool) {
	switch action.Action {
	case "query":
		return e.handleQueryAction(ctx, response, action, messages)
	case "respond":
		if strings.TrimSpace(action.Response) == "" {
			return "", appendUserMessage(messages, missingResponseInstruction), false
		}
		return action.Response, messages, true
	default:
		return "", appendUserMessage(messages, unknownActionInstruction), false
	}
}

func (e *Executor) handleQueryAction(ctx context.Context, response string, action *AgentAction, messages []Message) (string, []Message, bool) {
	if action.Tool == "" {
		return "", appendUserMessage(messages, missingQueryToolInstruction), false
	}

	result, err := e.toolExecute(ctx, action.Tool, action.Args)
	if err != nil {
		return "", appendUserMessage(messages, fmt.Sprintf(`Tool execution error: %v. Retry with corrected tool/args only if the correction is obvious from the conversation. Otherwise ask one concise clarifying question.`, err)), false
	}
	if rendered, ok := renderToolResult(action.Tool, action.Args, result); ok {
		return rendered, messages, true
	}

	messages = append(messages, Message{Role: "assistant", Content: response})
	messages = appendUserMessage(messages, buildToolFollowUpMessage(action.Tool, result))
	return "", messages, false
}

func appendUserMessage(messages []Message, content string) []Message {
	return append(messages, Message{Role: "user", Content: content})
}

func buildToolFollowUpMessage(tool, result string) string {
	return fmt.Sprintf(
		"Tool result for %s: %s\nNow provide a natural answer. Do not call another tool unless the previous result is unusable or clearly insufficient to answer the user's request.",
		tool,
		compactToolResultForPrompt(result, envInt("AGENT_MAX_TOOL_RESULT_CHARS", defaultToolResultPromptChars)),
	)
}

func compactToolResultForPrompt(raw string, maxChars int) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	if maxChars <= 0 {
		maxChars = 2200
	}

	var compacted bytes.Buffer
	if err := json.Compact(&compacted, []byte(trimmed)); err == nil {
		trimmed = compacted.String()
	} else {
		trimmed = strings.Join(strings.Fields(trimmed), " ")
	}

	runes := []rune(trimmed)
	if len(runes) <= maxChars {
		return trimmed
	}
	return string(runes[:maxChars]) + "... [truncated]"
}

func limitRenderedItems(items []string, limit int) ([]string, int) {
	if limit <= 0 {
		limit = defaultRenderedListItems
	}
	if len(items) <= limit {
		return items, 0
	}
	return items[:limit], len(items) - limit
}

func renderBulletList(prefix string, items []string) string {
	if len(items) == 0 {
		return prefix + "."
	}
	visible, remaining := limitRenderedItems(items, defaultRenderedListItems)
	lines := make([]string, 0, len(visible)+2)
	lines = append(lines, prefix+":")
	for _, item := range visible {
		lines = append(lines, "- "+item)
	}
	if remaining > 0 {
		lines = append(lines, fmt.Sprintf("- ...and %d more.", remaining))
	}
	return strings.Join(lines, "\n")
}

var toolResultRenderers = map[string]toolResultRenderer{
	"addTrackToNavidromePlaylist":              renderAddTrackToNavidromePlaylistResult,
	"applyDiscoveredAlbums":                    renderApplyDiscoveredAlbumsResult,
	"createDiscoveredPlaylist":                 renderCreateDiscoveredPlaylistResult,
	"queueTrackForNavidromePlaylist":           renderQueueTrackForNavidromePlaylistResult,
	"removePendingTracksFromNavidromePlaylist": renderRemovePendingTracksFromNavidromePlaylistResult,
	"queueMissingPlaylistTracks":               renderQueueMissingPlaylistTracksResult,
	"removeTrackFromNavidromePlaylist":         renderRemoveTrackFromNavidromePlaylistResult,
	"removeArtistFromLibrary":                  renderRemoveArtistFromLibraryResult,
	"resolvePlaylistTracks":                    renderResolvePlaylistTracksResult,
	"startArtistRemovalPreview":                renderStartArtistRemovalPreviewResult,
	"startDiscoveredAlbumsApplyPreview":        renderStartDiscoveredAlbumsApplyPreviewResult,
	"startLidarrCleanupApplyPreview":           renderStartLidarrCleanupApplyPreviewResult,
	"startPlaylistAppendPreview":               renderStartPlaylistAppendPreviewResult,
}

func renderArtistLibraryStatsResult(args map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Items []struct {
				ArtistName     string `json:"artistName"`
				AlbumCount     int    `json:"albumCount"`
				PlayedInWindow int    `json:"playedInWindow"`
			} `json:"artistLibraryStats"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil || len(payload.Data.Items) == 0 {
		return "", false
	}

	filter, _ := args["filter"].(map[string]interface{})
	items := make([]string, 0, len(payload.Data.Items))
	for _, item := range payload.Data.Items {
		if strings.TrimSpace(item.ArtistName) == "" {
			continue
		}
		if item.AlbumCount > 0 {
			items = append(items, fmt.Sprintf("%s (%d albums)", item.ArtistName, item.AlbumCount))
			continue
		}
		items = append(items, item.ArtistName)
	}
	if len(items) == 0 {
		return "", false
	}

	if filter != nil {
		if exactAlbums, ok := filter["exactAlbums"]; ok && fmt.Sprintf("%v", exactAlbums) == "1" {
			return renderBulletList("Artists in your library with only one album", items), true
		}
		if minAlbums, ok := filter["minAlbums"]; ok {
			if maxPlays, ok := filter["maxPlaysInWindow"]; ok && fmt.Sprintf("%v", maxPlays) == "0" {
				return renderBulletList(fmt.Sprintf("Artists in your library with at least %v albums and no plays in that window", minAlbums), items), true
			}
			return renderBulletList(fmt.Sprintf("Artists in your library with at least %v albums", minAlbums), items), true
		}
	}
	return renderBulletList("A few artist stats from your library", items), true
}

func renderAlbumLibraryStatsResult(args map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Items []struct {
				AlbumName      string  `json:"albumName"`
				ArtistName     string  `json:"artistName"`
				Year           *int    `json:"year"`
				PlayedInWindow int     `json:"playedInWindow"`
				LastPlayed     *string `json:"lastPlayed"`
			} `json:"albumLibraryStats"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil || len(payload.Data.Items) == 0 {
		return "", false
	}

	filter, _ := args["filter"].(map[string]interface{})
	items := make([]string, 0, len(payload.Data.Items))
	for _, item := range payload.Data.Items {
		name := strings.TrimSpace(item.AlbumName)
		if name == "" {
			continue
		}
		label := name
		if strings.TrimSpace(item.ArtistName) != "" {
			label = fmt.Sprintf("%s by %s", label, strings.TrimSpace(item.ArtistName))
		}
		if item.Year != nil && *item.Year > 0 {
			label = fmt.Sprintf("%s (%d)", label, *item.Year)
		}
		items = append(items, label)
	}
	if len(items) == 0 {
		return "", false
	}

	prefix := "Here are album library stats from your library"
	if filter != nil {
		if unplayed, ok := filter["unplayed"]; ok && fmt.Sprintf("%v", unplayed) == "true" {
			prefix = "Here are unplayed albums from your library"
		}
		if inactiveSince, ok := filter["inactiveSince"]; ok && strings.TrimSpace(fmt.Sprintf("%v", inactiveSince)) != "" {
			prefix = fmt.Sprintf("Albums in your library not played since %v", inactiveSince)
		}
		if notPlayedSince, ok := filter["notPlayedSince"]; ok && strings.TrimSpace(fmt.Sprintf("%v", notPlayedSince)) != "" {
			rawValue := strings.TrimSpace(fmt.Sprintf("%v", notPlayedSince))
			switch strings.ToLower(rawValue) {
			case "years", "months":
				prefix = fmt.Sprintf("Albums in your library not played in %s", rawValue)
			default:
				prefix = fmt.Sprintf("Albums in your library not played since %s", rawValue)
			}
		}
		if maxPlays, ok := filter["maxPlaysInWindow"]; ok && fmt.Sprintf("%v", maxPlays) == "0" {
			prefix = "Albums in your library with no plays in that window"
		}
	}
	return renderBulletList(prefix, items), true
}

func renderArtistListeningStatsResult(args map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Items []struct {
				ArtistName    string  `json:"artistName"`
				AlbumCount    int     `json:"albumCount"`
				PlaysInWindow int     `json:"playsInWindow"`
				LastPlayed    *string `json:"lastPlayed"`
			} `json:"artistListeningStats"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil || len(payload.Data.Items) == 0 {
		return "", false
	}

	filter, _ := args["filter"].(map[string]interface{})
	items := make([]string, 0, len(payload.Data.Items))
	for _, item := range payload.Data.Items {
		name := strings.TrimSpace(item.ArtistName)
		if name == "" {
			continue
		}
		label := fmt.Sprintf("%s (%d plays", name, item.PlaysInWindow)
		if item.AlbumCount > 0 {
			label += fmt.Sprintf(", %d albums", item.AlbumCount)
		}
		label += ")"
		items = append(items, label)
	}
	if len(items) == 0 {
		return "", false
	}

	prefix := "Here are artist listening stats from your library"
	if filter != nil {
		if maxPlays, ok := filter["maxPlaysInWindow"]; ok && fmt.Sprintf("%v", maxPlays) == "0" {
			prefix = "Artists in your library with no plays in that window"
		} else if playedSince, ok := filter["playedSince"]; ok && strings.TrimSpace(fmt.Sprintf("%v", playedSince)) != "" {
			prefix = fmt.Sprintf("Artists you played since %v", playedSince)
		}
	}
	return renderBulletList(prefix, items), true
}

func renderLibraryFacetCountsResult(args map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Items []struct {
				Value string `json:"value"`
				Count int    `json:"count"`
			} `json:"libraryFacetCounts"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil || len(payload.Data.Items) == 0 {
		return "", false
	}

	field := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", args["field"])))
	items := make([]string, 0, len(payload.Data.Items))
	for _, item := range payload.Data.Items {
		value := strings.TrimSpace(item.Value)
		if value == "" {
			continue
		}
		items = append(items, fmt.Sprintf("%s (%d)", value, item.Count))
	}
	if len(items) == 0 {
		return "", false
	}

	prefix := "Here are library facet counts"
	switch field {
	case "genre":
		prefix = "Genres that dominate your library"
	case "year":
		prefix = "Years that dominate your library"
	case "decade":
		prefix = "Decades that dominate your library"
	case "artist_name":
		prefix = "Artists that dominate your library by album count"
	}
	return renderBulletList(prefix, items), true
}

func renderAlbumRelationshipStatsResult(args map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Items []struct {
				AlbumName        string `json:"albumName"`
				ArtistName       string `json:"artistName"`
				Year             *int   `json:"year"`
				ArtistAlbumCount int    `json:"artistAlbumCount"`
			} `json:"albumRelationshipStats"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil || len(payload.Data.Items) == 0 {
		return "", false
	}

	items := make([]string, 0, len(payload.Data.Items))
	for _, item := range payload.Data.Items {
		name := strings.TrimSpace(item.AlbumName)
		if name == "" {
			continue
		}
		label := name
		if strings.TrimSpace(item.ArtistName) != "" {
			label = fmt.Sprintf("%s by %s", label, item.ArtistName)
		}
		if item.Year != nil && *item.Year > 0 {
			label = fmt.Sprintf("%s (%d)", label, *item.Year)
		}
		items = append(items, label)
	}
	if len(items) == 0 {
		return "", false
	}

	filter, _ := args["filter"].(map[string]interface{})
	prefix := "Here are album relationship stats from your library"
	if filter != nil {
		if exact, ok := filter["artistExactAlbums"]; ok && fmt.Sprintf("%v", exact) == "1" {
			prefix = "Albums in your library by artists with only one album"
		}
	}
	return renderBulletList(prefix, items), true
}

func renderToolResult(tool string, args map[string]interface{}, raw string) (string, bool) {
	renderer, ok := toolResultRenderers[tool]
	if !ok {
		return "", false
	}
	return renderer(args, raw)
}

func renderDiscoverAlbumsResult(_ map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			DiscoverAlbums struct {
				Candidates []struct {
					ArtistName string `json:"artistName"`
					AlbumTitle string `json:"albumTitle"`
					Year       int    `json:"year"`
				} `json:"candidates"`
			} `json:"discoverAlbums"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil || len(payload.Data.DiscoverAlbums.Candidates) == 0 {
		return "", false
	}

	items := make([]string, 0, len(payload.Data.DiscoverAlbums.Candidates))
	for _, candidate := range payload.Data.DiscoverAlbums.Candidates {
		label := strings.TrimSpace(candidate.AlbumTitle)
		if label == "" {
			continue
		}
		if candidate.Year > 0 {
			label = fmt.Sprintf("%s (%d)", label, candidate.Year)
		}
		items = append(items, label)
	}

	artist := payload.Data.DiscoverAlbums.Candidates[0].ArtistName
	if artist != "" {
		return renderBulletList(fmt.Sprintf("A few %s albums worth starting with", artist), items), true
	}
	return renderBulletList("A few albums worth starting with", items), true
}

func renderSemanticAlbumSearchResult(_ map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Search struct {
				QueryText string `json:"queryText"`
				Matches   []struct {
					Name         string   `json:"name"`
					ArtistName   string   `json:"artistName"`
					Year         *int     `json:"year"`
					Similarity   float64  `json:"similarity"`
					Explanations []string `json:"explanations"`
				} `json:"matches"`
			} `json:"semanticAlbumSearch"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil || len(payload.Data.Search.Matches) == 0 {
		return "", false
	}

	items := make([]string, 0, len(payload.Data.Search.Matches))
	reasons := make([]string, 0, 3)
	seenReasons := make(map[string]struct{})
	for _, match := range payload.Data.Search.Matches {
		name := strings.TrimSpace(match.Name)
		if name == "" {
			continue
		}
		label := name
		if strings.TrimSpace(match.ArtistName) != "" {
			label = fmt.Sprintf("%s by %s", label, match.ArtistName)
		}
		if match.Year != nil && *match.Year > 0 {
			label = fmt.Sprintf("%s (%d)", label, *match.Year)
		}
		if len(match.Explanations) > 0 {
			reason := strings.TrimSpace(match.Explanations[0])
			if reason != "" {
				key := strings.ToLower(reason)
				if _, ok := seenReasons[key]; !ok && len(reasons) < 3 {
					seenReasons[key] = struct{}{}
					reasons = append(reasons, reason)
				}
			}
		}
		items = append(items, label)
	}
	if len(items) == 0 {
		return "", false
	}

	queryText := strings.TrimSpace(payload.Data.Search.QueryText)
	prefix := "Closest album matches from your library"
	if queryText != "" {
		prefix = fmt.Sprintf("Closest matches in your library for %q", queryText)
	}
	rendered := renderBulletList(prefix, items)
	if len(reasons) == 0 {
		return rendered, true
	}
	return rendered + "\nWhy these: " + strings.Join(reasons, "; ") + ".", true
}

func renderMatchDiscoveredAlbumsResult(_ map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Match struct {
				Matches []struct {
					AlbumTitle string `json:"albumTitle"`
					Status     string `json:"status"`
				} `json:"matches"`
			} `json:"matchDiscoveredAlbumsInLidarr"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil || len(payload.Data.Match.Matches) == 0 {
		return "", false
	}

	var already, can, review []string
	for _, match := range payload.Data.Match.Matches {
		switch match.Status {
		case "already_monitored":
			already = append(already, match.AlbumTitle)
		case "can_monitor":
			can = append(can, match.AlbumTitle)
		default:
			review = append(review, match.AlbumTitle)
		}
	}

	parts := make([]string, 0, 3)
	if len(already) > 0 {
		parts = append(parts, fmt.Sprintf("already in your library (search can still run): %s", strings.Join(already, ", ")))
	}
	if len(can) > 0 {
		parts = append(parts, fmt.Sprintf("ready to add to your library: %s", strings.Join(can, ", ")))
	}
	if len(review) > 0 {
		parts = append(parts, fmt.Sprintf("need review: %s", strings.Join(review, ", ")))
	}
	if len(parts) == 0 {
		return "", false
	}
	return "Library check: " + strings.Join(parts, ". ") + ".", true
}

func renderApplyDiscoveredAlbumsResult(_ map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Apply struct {
				Mode    string `json:"mode"`
				Results []struct {
					AlbumTitle string `json:"albumTitle"`
					Status     string `json:"status"`
				} `json:"results"`
			} `json:"applyDiscoveredAlbums"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil || len(payload.Data.Apply.Results) == 0 {
		return "", false
	}

	okCount := 0
	partialCount := 0
	failCount := 0
	names := make([]string, 0, len(payload.Data.Apply.Results))
	for _, result := range payload.Data.Apply.Results {
		if strings.TrimSpace(result.AlbumTitle) != "" {
			names = append(names, result.AlbumTitle)
		}
		switch result.Status {
		case "ok", "dry_run":
			okCount++
		case "partial":
			partialCount++
		default:
			failCount++
		}
	}

	target := "selected albums"
	if len(names) > 0 && len(names) <= 2 {
		target = strings.Join(names, ", ")
	}
	if payload.Data.Apply.Mode == "dry_run" {
		return fmt.Sprintf("Dry run for %s: %d ready, %d potential issues.", target, okCount, failCount+partialCount), true
	}
	return fmt.Sprintf("Applied in your library for %s: %d successful, %d partial, %d failed.", target, okCount, partialCount, failCount), true
}

func renderRemoveArtistFromLibraryResult(_ map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Remove struct {
				ArtistName string `json:"artistName"`
				Removed    bool   `json:"removed"`
			} `json:"removeArtistFromLibrary"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil || !payload.Data.Remove.Removed {
		return "", false
	}

	name := strings.TrimSpace(payload.Data.Remove.ArtistName)
	if name == "" {
		name = "that artist"
	}
	return fmt.Sprintf("Removed %q from your library.", name), true
}

func renderResolvePlaylistTracksResult(_ map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Resolve struct {
				Counts struct {
					Total     int `json:"total"`
					Available int `json:"available"`
					Missing   int `json:"missing"`
					Ambiguous int `json:"ambiguous"`
					Errors    int `json:"errors"`
				} `json:"counts"`
				PlaylistName string `json:"playlistName"`
			} `json:"resolvePlaylistTracks"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return "", false
	}

	counts := payload.Data.Resolve.Counts
	if counts.Total == 0 {
		return "", false
	}
	name := strings.TrimSpace(payload.Data.Resolve.PlaylistName)
	if name == "" {
		name = "your planned playlist"
	}
	return fmt.Sprintf(
		"Resolved %d tracks for %s: %d available, %d missing, %d ambiguous, %d errors. Use the approval buttons to create the playlist with the available tracks.",
		counts.Total, name, counts.Available, counts.Missing, counts.Ambiguous, counts.Errors,
	), true
}

func renderNavidromePlaylistsResult(_ map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Result struct {
				Playlists []struct {
					Name      string `json:"name"`
					SongCount int    `json:"songCount"`
				} `json:"playlists"`
			} `json:"navidromePlaylists"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil || len(payload.Data.Result.Playlists) == 0 {
		return "", false
	}
	items := make([]string, 0, len(payload.Data.Result.Playlists))
	for _, playlist := range payload.Data.Result.Playlists {
		name := strings.TrimSpace(playlist.Name)
		if name == "" {
			continue
		}
		if playlist.SongCount > 0 {
			items = append(items, fmt.Sprintf("%s (%d tracks)", name, playlist.SongCount))
		} else {
			items = append(items, name)
		}
	}
	if len(items) == 0 {
		return "", false
	}
	return renderBulletList("Saved Navidrome playlists", items), true
}

func renderNavidromePlaylistResult(_ map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Result struct {
				Name   string `json:"name"`
				Tracks []struct {
					Title      string `json:"title"`
					ArtistName string `json:"artistName"`
				} `json:"tracks"`
			} `json:"navidromePlaylist"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return "", false
	}
	name := strings.TrimSpace(payload.Data.Result.Name)
	if name == "" || len(payload.Data.Result.Tracks) == 0 {
		return "", false
	}
	items := make([]string, 0, len(payload.Data.Result.Tracks))
	for _, track := range payload.Data.Result.Tracks {
		title := strings.TrimSpace(track.Title)
		if title == "" {
			continue
		}
		if artist := strings.TrimSpace(track.ArtistName); artist != "" {
			items = append(items, fmt.Sprintf("%s by %s", title, artist))
		} else {
			items = append(items, title)
		}
	}
	if len(items) == 0 {
		return "", false
	}
	return renderBulletList(fmt.Sprintf("Playlist %q currently has", name), items), true
}

func renderNavidromePlaylistStateResult(_ map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Result struct {
				Name   string `json:"name"`
				Counts struct {
					Saved        int `json:"saved"`
					PendingFetch int `json:"pending_fetch"`
					Total        int `json:"total"`
				} `json:"counts"`
			} `json:"navidromePlaylistState"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return "", false
	}
	name := strings.TrimSpace(payload.Data.Result.Name)
	if name == "" {
		return "", false
	}
	counts := payload.Data.Result.Counts
	return fmt.Sprintf(
		"Playlist %q state: %d saved tracks, %d pending fetch, %d total tracked items.",
		name, counts.Saved, counts.PendingFetch, counts.Total,
	), true
}

func renderAddTrackToNavidromePlaylistResult(_ map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Result struct {
				PlaylistName string `json:"playlistName"`
				ArtistName   string `json:"artistName"`
				TrackTitle   string `json:"trackTitle"`
				Added        bool   `json:"added"`
				Reason       string `json:"reason"`
			} `json:"addTrackToNavidromePlaylist"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return "", false
	}
	result := payload.Data.Result
	if strings.TrimSpace(result.PlaylistName) == "" || strings.TrimSpace(result.TrackTitle) == "" {
		return "", false
	}
	if !result.Added && result.Reason == "already_present" {
		return fmt.Sprintf("%q by %s is already in playlist %q.", result.TrackTitle, result.ArtistName, result.PlaylistName), true
	}
	if result.Added {
		return fmt.Sprintf("Added %q by %s to playlist %q.", result.TrackTitle, result.ArtistName, result.PlaylistName), true
	}
	return "", false
}

func renderQueueTrackForNavidromePlaylistResult(_ map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Result struct {
				PlaylistName string `json:"playlistName"`
				ArtistName   string `json:"artistName"`
				TrackTitle   string `json:"trackTitle"`
				Queued       bool   `json:"queued"`
			} `json:"queueTrackForNavidromePlaylist"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return "", false
	}
	result := payload.Data.Result
	if !result.Queued || strings.TrimSpace(result.PlaylistName) == "" || strings.TrimSpace(result.TrackTitle) == "" {
		return "", false
	}
	return fmt.Sprintf("Queued %q by %s for playlist %q. Reconcile will add it once it becomes available.", result.TrackTitle, result.ArtistName, result.PlaylistName), true
}

func renderRemoveTrackFromNavidromePlaylistResult(_ map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Result struct {
				PlaylistName string   `json:"playlistName"`
				Removed      int      `json:"removed"`
				Tracks       []string `json:"tracks"`
			} `json:"removeTrackFromNavidromePlaylist"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return "", false
	}
	result := payload.Data.Result
	if strings.TrimSpace(result.PlaylistName) == "" || result.Removed <= 0 {
		return "", false
	}
	if len(result.Tracks) == 0 {
		return fmt.Sprintf("Removed %d track(s) from playlist %q.", result.Removed, result.PlaylistName), true
	}
	return fmt.Sprintf("Removed %d track(s) from playlist %q: %s.", result.Removed, result.PlaylistName, strings.Join(result.Tracks, ", ")), true
}

func renderRemovePendingTracksFromNavidromePlaylistResult(_ map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Result struct {
				PlaylistName string   `json:"playlistName"`
				Removed      int      `json:"removed"`
				Tracks       []string `json:"tracks"`
			} `json:"removePendingTracksFromNavidromePlaylist"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return "", false
	}
	result := payload.Data.Result
	if strings.TrimSpace(result.PlaylistName) == "" || result.Removed <= 0 {
		return "", false
	}
	if len(result.Tracks) == 0 {
		return fmt.Sprintf("Removed %d pending track(s) from playlist %q.", result.Removed, result.PlaylistName), true
	}
	return fmt.Sprintf("Removed %d pending track(s) from playlist %q: %s.", result.Removed, result.PlaylistName, strings.Join(result.Tracks, ", ")), true
}

func renderCreateDiscoveredPlaylistResult(_ map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Create struct {
				Action         string `json:"action"`
				PlaylistName   string `json:"playlistName"`
				Added          int    `json:"added"`
				ResolvedTracks int    `json:"resolvedTracks"`
				Existing       int    `json:"existing"`
				ToAdd          int    `json:"toAdd"`
			} `json:"createDiscoveredPlaylist"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return "", false
	}

	result := payload.Data.Create
	if strings.TrimSpace(result.PlaylistName) == "" {
		return "", false
	}
	switch result.Action {
	case "created":
		return fmt.Sprintf("Created playlist '%s' with %d tracks from your library.", result.PlaylistName, result.Added), true
	case "updated":
		if result.Added == 0 {
			return fmt.Sprintf("Playlist '%s' already had all resolved tracks, so no new tracks were added.", result.PlaylistName), true
		}
		return fmt.Sprintf("Updated playlist '%s': added %d new tracks.", result.PlaylistName, result.Added), true
	default:
		return fmt.Sprintf("Playlist '%s' processed with action '%s'.", result.PlaylistName, result.Action), true
	}
}

func renderQueueMissingPlaylistTracksResult(_ map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Queue struct {
				Queued int `json:"queued"`
			} `json:"queueMissingPlaylistTracks"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil || payload.Data.Queue.Queued <= 0 {
		return "", false
	}
	return fmt.Sprintf("Queued %d missing tracks for the download agent.", payload.Data.Queue.Queued), true
}

func renderArtistsResult(args map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Artists []struct {
				Name string `json:"name"`
			} `json:"artists"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return "", false
	}

	artistName, ok := args["artistName"].(string)
	if !ok || strings.TrimSpace(artistName) == "" {
		return "", false
	}

	name := strings.TrimSpace(artistName)
	if len(payload.Data.Artists) == 0 {
		return fmt.Sprintf("%q is not in your library.", name), true
	}
	resolved := strings.TrimSpace(payload.Data.Artists[0].Name)
	if resolved == "" {
		resolved = name
	}
	return fmt.Sprintf("%q is in your library.", resolved), true
}

func renderStartArtistRemovalPreviewResult(_ map[string]interface{}, raw string) (string, bool) {
	return renderPreviewResponse(raw, "startArtistRemovalPreview")
}

func renderStartDiscoveredAlbumsApplyPreviewResult(_ map[string]interface{}, raw string) (string, bool) {
	return renderPreviewResponse(raw, "startDiscoveredAlbumsApplyPreview")
}

func renderStartLidarrCleanupApplyPreviewResult(_ map[string]interface{}, raw string) (string, bool) {
	return renderPreviewResponse(raw, "startLidarrCleanupApplyPreview")
}

func renderStartPlaylistAppendPreviewResult(_ map[string]interface{}, raw string) (string, bool) {
	return renderPreviewResponse(raw, "startPlaylistAppendPreview")
}

func renderPreviewResponse(raw, field string) (string, bool) {
	var payload struct {
		Data map[string]struct {
			Response string `json:"response"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return "", false
	}

	preview, ok := payload.Data[field]
	if !ok || strings.TrimSpace(preview.Response) == "" {
		return "", false
	}
	return strings.TrimSpace(preview.Response), true
}

func renderAlbumsResult(args map[string]interface{}, raw string) (string, bool) {
	var payload struct {
		Data struct {
			Albums []struct {
				Name       string `json:"name"`
				ArtistName string `json:"artistName"`
				Year       int    `json:"year"`
			} `json:"albums"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil || len(payload.Data.Albums) == 0 {
		return "", false
	}

	items := make([]string, 0, len(payload.Data.Albums))
	for _, album := range payload.Data.Albums {
		if album.Year > 0 {
			items = append(items, fmt.Sprintf("%s (%d)", album.Name, album.Year))
			continue
		}
		items = append(items, album.Name)
	}

	prefix := "Here are the albums from your library"
	if sortBy, _ := args["sortBy"].(string); strings.TrimSpace(sortBy) != "" {
		switch strings.ToLower(strings.TrimSpace(sortBy)) {
		case "rating":
			prefix = "Here are the best-rated albums from your library"
		case "recent":
			prefix = "Here are the most recently played albums from your library"
		default:
			prefix = "Here are the most played albums from your library"
		}
	}
	return renderBulletList(prefix, items), true
}

const (
	providerGroq = "groq"
	providerHF   = "hf"
)

func resolveModelProvider(model string) (string, string) {
	trimmed := strings.TrimSpace(model)
	if strings.HasPrefix(trimmed, "hf:") {
		return providerHF, strings.TrimSpace(strings.TrimPrefix(trimmed, "hf:"))
	}
	if trimmed == "" {
		return providerGroq, DefaultGroqModel
	}
	return providerGroq, trimmed
}

func (e *Executor) callModel(ctx context.Context, provider, model string, messages []Message) (string, error) {
	switch provider {
	case providerHF:
		if strings.TrimSpace(e.huggingFaceKey) == "" {
			return "", fmt.Errorf("HUGGINGFACE_API_KEY is not configured")
		}
		return e.callOpenAICompatible(
			ctx,
			envString("HUGGINGFACE_CHAT_COMPLETIONS_URL", envString("HF_CHAT_COMPLETIONS_URL", "https://router.huggingface.co/v1/chat/completions")),
			e.huggingFaceKey,
			model,
			messages,
			"Hugging Face",
		)
	case providerGroq:
		if strings.TrimSpace(e.groqKey) == "" {
			return "", fmt.Errorf("GROQ_API_KEY is not configured")
		}
		return e.callOpenAICompatible(
			ctx,
			envString("GROQ_CHAT_COMPLETIONS_URL", "https://api.groq.com/openai/v1/chat/completions"),
			e.groqKey,
			model,
			messages,
			"Groq",
		)
	default:
		return "", fmt.Errorf("unsupported model provider: %s", provider)
	}
}

func (e *Executor) callOpenAICompatible(ctx context.Context, endpoint, apiKey, model string, messages []Message, providerLabel string) (string, error) {
	payload := ChatCompletionRequest{
		Model:               model,
		Messages:            messages,
		MaxCompletionTokens: e.maxCompletionTokens,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("%s API returned %d: %s", providerLabel, resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var result ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no response from %s", providerLabel)
	}
	logModelUsage(providerLabel, model, result)

	return result.Choices[0].Message.Content, nil
}

func logModelUsage(providerLabel, model string, result ChatCompletionResponse) {
	if !envBool("AGENT_LOG_MODEL_USAGE", true) {
		return
	}
	if result.Usage.PromptTokens == 0 && result.Usage.CompletionTokens == 0 && result.Usage.TotalTokens == 0 {
		return
	}
	event := log.Info().
		Str("provider", providerLabel).
		Str("model", model).
		Int("prompt_tokens", result.Usage.PromptTokens).
		Int("completion_tokens", result.Usage.CompletionTokens).
		Int("total_tokens", result.Usage.TotalTokens)
	if result.Usage.PromptTokensDetails.CachedTokens > 0 {
		event = event.Int("cached_prompt_tokens", result.Usage.PromptTokensDetails.CachedTokens)
	}
	event.Msg("LLM response usage")
}

func parseAction(text string) (*AgentAction, error) {
	clean := strings.TrimSpace(text)
	clean = strings.TrimPrefix(clean, "```json")
	clean = strings.TrimPrefix(clean, "```")
	clean = strings.TrimSuffix(clean, "```")
	clean = strings.TrimSpace(clean)

	obj := clean
	if !strings.HasPrefix(obj, "{") {
		start := strings.Index(clean, "{")
		end := strings.LastIndex(clean, "}")
		if start >= 0 && end > start {
			obj = clean[start : end+1]
		}
	}

	var action AgentAction
	if err := json.Unmarshal([]byte(obj), &action); err != nil {
		return nil, err
	}
	if action.Action == "" {
		return nil, errors.New("missing action")
	}
	if action.Args == nil {
		action.Args = map[string]interface{}{}
	}
	return &action, nil
}

func envInt(name string, defaultVal int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return defaultVal
	}
	return v
}

func envBool(name string, defaultVal bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if raw == "" {
		return defaultVal
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultVal
	}
}

func envString(name, defaultVal string) string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultVal
	}
	return raw
}
