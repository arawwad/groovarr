package main

import (
	"strings"
	"time"

	"groovarr/internal/agent"
)

const llmContextStructuredMemoryTTL = 2 * time.Hour
const defaultMaxLLMHistoryMessages = 6
const maxStructuredRecentRequests = 4

type chatSessionMemory struct {
	UpdatedAt          time.Time
	ActiveRequest      string
	RecentUserRequests []string
	CurrentPlaylist    string
	LastDiscoveryQuery string
	LastPlaylistPrompt string
}

func (s *Server) hydrateChatSessionMemory(sessionID string, history []agent.Message) chatSessionMemory {
	sessionID = normalizeChatSessionID(sessionID)

	s.memoryMu.Lock()
	memory := s.chatMemory[sessionID]
	s.memoryMu.Unlock()

	for _, message := range history {
		applyChatMemoryMessage(&memory, message.Role, message.Content)
	}
	s.syncChatSessionMemory(&memory, sessionID)
	if !chatSessionMemoryEmpty(memory) {
		memory.UpdatedAt = time.Now().UTC()
		s.memoryMu.Lock()
		if s.chatMemory == nil {
			s.chatMemory = make(map[string]chatSessionMemory)
		}
		s.chatMemory[sessionID] = memory
		s.memoryMu.Unlock()
	}
	return memory
}

func (s *Server) rememberChatExchange(sessionID string, history []agent.Message, userMsg, assistantMsg string) {
	sessionID = normalizeChatSessionID(sessionID)
	memory := s.hydrateChatSessionMemory(sessionID, history)
	applyChatMemoryMessage(&memory, "user", userMsg)
	applyChatMemoryMessage(&memory, "assistant", assistantMsg)
	s.syncChatSessionMemory(&memory, sessionID)
	if chatSessionMemoryEmpty(memory) {
		return
	}
	memory.UpdatedAt = time.Now().UTC()
	s.memoryMu.Lock()
	if s.chatMemory == nil {
		s.chatMemory = make(map[string]chatSessionMemory)
	}
	s.chatMemory[sessionID] = memory
	s.memoryMu.Unlock()
}

func (s *Server) latestChatSessionMemory(sessionID string) (chatSessionMemory, bool) {
	sessionID = normalizeChatSessionID(sessionID)
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	memory, ok := s.chatMemory[sessionID]
	return memory, ok
}

func (s *Server) syncChatSessionMemory(memory *chatSessionMemory, sessionID string) {
	if memory == nil {
		return
	}
	turnMemory := loadTurnSessionMemory(sessionID)
	if _, playlistName, _, candidates, _, _, ok := turnMemory.PlaylistContext(); ok && len(candidates) > 0 {
		if playlist := strings.TrimSpace(playlistName); playlist != "" {
			memory.CurrentPlaylist = playlist
		}
	}
	if prompt, _, _, candidates, _, _, ok := turnMemory.PlaylistContext(); ok && len(candidates) > 0 {
		if text := strings.TrimSpace(prompt); text != "" {
			memory.LastPlaylistPrompt = text
		}
	}
	if _, _, query, ok := turnMemory.DiscoveredAlbums(); ok && strings.TrimSpace(query) != "" {
		memory.LastDiscoveryQuery = strings.TrimSpace(query)
	}
}

func applyChatMemoryMessage(memory *chatSessionMemory, role, content string) {
	if memory == nil {
		return
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	if name, ok := extractPlaylistNameFromText(content); ok {
		memory.CurrentPlaylist = name
	}
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user":
		memory.RecentUserRequests = appendRecentRequest(memory.RecentUserRequests, content)
		if !isReferentialFollowUp(content) {
			memory.ActiveRequest = content
		}
	}
}

func appendRecentRequest(existing []string, content string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return existing
	}
	filtered := make([]string, 0, len(existing)+1)
	contentKey := normalizeReferenceText(content)
	for _, item := range existing {
		if normalizeReferenceText(item) == contentKey {
			continue
		}
		filtered = append(filtered, item)
	}
	filtered = append(filtered, content)
	if len(filtered) > maxStructuredRecentRequests {
		filtered = filtered[len(filtered)-maxStructuredRecentRequests:]
	}
	return filtered
}

func chatSessionMemoryEmpty(memory chatSessionMemory) bool {
	return strings.TrimSpace(memory.ActiveRequest) == "" &&
		len(memory.RecentUserRequests) == 0 &&
		strings.TrimSpace(memory.CurrentPlaylist) == "" &&
		strings.TrimSpace(memory.LastDiscoveryQuery) == "" &&
		strings.TrimSpace(memory.LastPlaylistPrompt) == ""
}

func formatStructuredChatMemory(memory chatSessionMemory, now time.Time) string {
	if chatSessionMemoryEmpty(memory) || memory.UpdatedAt.IsZero() || now.Sub(memory.UpdatedAt) > llmContextStructuredMemoryTTL {
		return ""
	}
	parts := make([]string, 0, 5)
	if text := strings.TrimSpace(memory.ActiveRequest); text != "" {
		parts = append(parts, `active_request="`+text+`"`)
	}
	if len(memory.RecentUserRequests) > 0 {
		parts = append(parts, `recent_user_requests="`+strings.Join(memory.RecentUserRequests, " | ")+`"`)
	}
	if text := strings.TrimSpace(memory.CurrentPlaylist); text != "" {
		parts = append(parts, `current_playlist="`+text+`"`)
	}
	if text := strings.TrimSpace(memory.LastDiscoveryQuery); text != "" {
		parts = append(parts, `last_discovery_query="`+text+`"`)
	}
	if text := strings.TrimSpace(memory.LastPlaylistPrompt); text != "" {
		parts = append(parts, `last_playlist_prompt="`+text+`"`)
	}
	if len(parts) == 0 {
		return ""
	}
	return "structured_memory: " + strings.Join(parts, "; ")
}

func selectHistoryForLLM(history []agent.Message, memory chatSessionMemory, userMsg string, maxMessages int) []agent.Message {
	if len(history) == 0 {
		return nil
	}
	if maxMessages <= 0 {
		maxMessages = defaultMaxLLMHistoryMessages
	}
	if len(history) <= maxMessages {
		return append([]agent.Message(nil), history...)
	}

	start := len(history) - maxMessages
	selected := make(map[int]struct{}, maxMessages+2)
	for index := start; index < len(history); index++ {
		selected[index] = struct{}{}
	}

	if isReferentialFollowUp(userMsg) {
		if anchor := findHistoryAnchor(history[:start], memory); anchor >= 0 {
			selected[anchor] = struct{}{}
			if anchor+1 < start && history[anchor].Role == "user" && history[anchor+1].Role == "assistant" {
				selected[anchor+1] = struct{}{}
			}
		}
	}

	out := make([]agent.Message, 0, len(selected))
	for index, message := range history {
		if _, ok := selected[index]; ok {
			out = append(out, message)
		}
	}
	return out
}

func findHistoryAnchor(history []agent.Message, memory chatSessionMemory) int {
	needles := structuredMemoryNeedles(memory)
	if len(needles) == 0 {
		return -1
	}
	for index := len(history) - 1; index >= 0; index-- {
		content := normalizeReferenceText(history[index].Content)
		if content == "" {
			continue
		}
		for _, needle := range needles {
			if needle != "" && strings.Contains(content, needle) {
				return index
			}
		}
	}
	return -1
}

func structuredMemoryNeedles(memory chatSessionMemory) []string {
	needles := make([]string, 0, 4)
	for _, item := range []string{
		memory.ActiveRequest,
		memory.LastDiscoveryQuery,
		memory.CurrentPlaylist,
		memory.LastPlaylistPrompt,
	} {
		item = normalizeReferenceText(item)
		if item != "" {
			needles = append(needles, item)
		}
	}
	return needles
}

func normalizeReferenceText(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"\n", " ",
		"\t", " ",
		"\\u00b7", " ",
		"\\u2022", " ",
		",", " ",
		".", " ",
		"•", " ",
		"·", " ",
		"–", " ",
		"—", " ",
		"?", " ",
		"!", " ",
		":", " ",
		";", " ",
		"(", " ",
		")", " ",
		`"`, " ",
	)
	return strings.Join(strings.Fields(replacer.Replace(text)), " ")
}

func isReferentialFollowUp(text string) bool {
	lower := normalizeReferenceText(text)
	if lower == "" {
		return false
	}
	cues := []string{
		"from those", "from them", "from that", "those ", "them ", "these ", "that ",
		"the last one", "the last ones", "same playlist", "same artist", "same album",
		"narrow that", "expand that", "revisit today", "what about those", "what about that",
	}
	for _, cue := range cues {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}
