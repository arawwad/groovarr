package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"groovarr/internal/agent"

	"github.com/rs/zerolog/log"
)

type chatSessionContextKey string

const chatSessionKey chatSessionContextKey = "chat_session_id"
const chatRequestKey chatSessionContextKey = "chat_request_id"

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, int64(envInt("MAX_CHAT_BODY_BYTES", defaultMaxChatBodyBytes)))

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		s.sendError(w, "Message is required", http.StatusBadRequest)
		return
	}

	history := normalizeChatHistory(
		req.History,
		envInt("CHAT_MAX_HISTORY_MESSAGES", defaultMaxChatHistoryMessages),
		envInt("CHAT_MAX_HISTORY_MESSAGE_CHARS", defaultMaxHistoryMessageChars),
	)
	sessionID := normalizeChatSessionID(req.SessionID)

	timeoutSec := envInt("CHAT_REQUEST_TIMEOUT_SEC", 25)
	chatCtx, cancel := context.WithTimeout(r.Context(), time.Duration(timeoutSec)*time.Second)
	defer cancel()
	chatCtx = context.WithValue(chatCtx, chatSessionKey, sessionID)
	chatCtx = context.WithValue(chatCtx, chatRequestKey, newChatRequestID())

	response, err := s.buildChatResponse(chatCtx, msg, history, strings.TrimSpace(req.Model))
	if err != nil {
		if s.chatArchive != nil {
			s.chatArchive.RecordResponse(chatCtx, ChatResponse{}, err)
		}
		log.Error().Err(err).Msg("Agent error")
		if req.Stream {
			s.sendChatStreamError(w, "Failed to process query", http.StatusInternalServerError)
			return
		}
		s.sendError(w, "Failed to process query", http.StatusInternalServerError)
		return
	}
	if s.chatArchive != nil {
		s.chatArchive.RecordResponse(chatCtx, response, nil)
	}

	if req.Stream {
		s.streamChatText(w, response)
		return
	}

	s.sendJSON(w, ChatResponse{
		Response:      response.Response,
		PendingAction: response.PendingAction,
	})
}

func normalizeChatSessionID(raw string) string {
	sessionID := strings.TrimSpace(raw)
	if sessionID == "" {
		return "global"
	}
	return sessionID
}

func chatSessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return "global"
	}
	if sessionID, ok := ctx.Value(chatSessionKey).(string); ok && strings.TrimSpace(sessionID) != "" {
		return sessionID
	}
	return "global"
}

func chatRequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if requestID, ok := ctx.Value(chatRequestKey).(string); ok {
		return strings.TrimSpace(requestID)
	}
	return ""
}

func (s *Server) buildChatResponse(chatCtx context.Context, msg string, history []agent.Message, modelOverride string) (ChatResponse, error) {
	sessionID := chatSessionIDFromContext(chatCtx)
	memory := s.hydrateChatSessionMemory(sessionID, history)
	logChatPipelineStage(chatCtx, "request", map[string]string{
		"message": msg,
	})
	if pending, ok := s.tryConversationalPendingAction(chatCtx, msg); ok {
		logChatPipelineStage(chatCtx, "pending_action", map[string]string{
			"response": pending.Response,
		})
		s.rememberChatExchange(sessionID, history, msg, pending.Response)
		return pending, nil
	}
	sessionContext := strings.TrimSpace(s.buildLLMSessionContext(sessionID))
	resolvedTurn := s.maybeNormalizeTurn(chatCtx, sessionID, msg, history, sessionContext)
	plan := s.maybePlanTurn(chatCtx, msg, history, resolvedTurn, sessionContext)
	if plan != nil {
		if resp, ok := s.executeOrchestrationDecision(chatCtx, msg, history, resolvedTurn, plan); ok {
			s.rememberChatExchange(sessionID, history, msg, resp.Response)
			return resp, nil
		}
	}
	if resp, ok := s.tryNormalizedClarification(msg, resolvedTurn); ok {
		logChatPipelineStage(chatCtx, "normalized_clarification", map[string]string{
			"response": resp,
		})
		s.rememberChatExchange(sessionID, history, msg, resp)
		return ChatResponse{Response: resp}, nil
	}
	if resp, ok := s.tryNormalizedIntentRoute(chatCtx, msg, history, resolvedTurn); ok {
		logChatPipelineStage(chatCtx, "normalized_route_fallback", map[string]string{
			"response":           resp.Response,
			"has_pending_action": boolString(resp.PendingAction != nil),
		})
		s.rememberChatExchange(sessionID, history, msg, resp.Response)
		return resp, nil
	}
	if resp, ok := s.tryEmbeddingsUnavailableSemanticLibraryQuery(msg); ok {
		logChatPipelineStage(chatCtx, "embeddings_unavailable", map[string]string{
			"response": resp,
		})
		s.rememberChatExchange(sessionID, history, msg, resp)
		return ChatResponse{Response: resp}, nil
	}

	agentStartedAt := time.Now().UTC()
	llmHistory := selectHistoryForLLM(history, memory, msg, envInt("CHAT_LLM_HISTORY_MESSAGES", defaultMaxLLMHistoryMessages))
	if planContext := strings.TrimSpace(buildOrchestrationDecisionContext(plan)); planContext != "" {
		llmHistory = append(append([]agent.Message(nil), llmHistory...), agent.Message{
			Role:    "assistant",
			Content: planContext,
		})
	}
	if normalizedContext := strings.TrimSpace(buildNormalizedTurnContext(resolvedTurn)); normalizedContext != "" {
		llmHistory = append(append([]agent.Message(nil), llmHistory...), agent.Message{
			Role:    "assistant",
			Content: normalizedContext,
		})
	}
	if sessionContext != "" {
		llmHistory = append(append([]agent.Message(nil), llmHistory...), agent.Message{
			Role:    "assistant",
			Content: sessionContext,
		})
	}
	response, err := s.agent.ProcessQueryWithSignals(chatCtx, msg, llmHistory, modelOverride, buildAgentTurnSignals(resolvedTurn))
	if err != nil {
		return ChatResponse{}, err
	}
	logChatPipelineStage(chatCtx, "agent", map[string]string{
		"response": response,
		"model":    modelOverride,
	})
	s.rememberChatExchange(sessionID, history, msg, response)
	var pendingAction *PendingAction
	if !agent.IsDefaultFailureResponse(response) {
		pendingAction = s.maybeBuildPendingAction(chatCtx, agentStartedAt.Add(-2*time.Second), msg)
	}
	return ChatResponse{
		Response:      response,
		PendingAction: pendingAction,
	}, nil
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func (s *Server) streamChatText(w http.ResponseWriter, response ChatResponse) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.sendJSON(w, response)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	const chunkSize = 120
	for _, chunk := range chunkRunes(response.Response, chunkSize) {
		if err := s.sendChatStreamEvent(w, ChatStreamResponse{Type: "delta", Delta: chunk}); err != nil {
			return
		}
		flusher.Flush()
	}

	_ = s.sendChatStreamEvent(w, ChatStreamResponse{
		Type:          "done",
		Response:      response.Response,
		PendingAction: response.PendingAction,
	})
	flusher.Flush()
}

func (s *Server) sendChatStreamError(w http.ResponseWriter, message string, code int) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.sendError(w, message, code)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(code)
	_ = s.sendChatStreamEvent(w, ChatStreamResponse{Type: "error", Error: message})
	flusher.Flush()
}

func (s *Server) sendChatStreamEvent(w io.Writer, payload ChatStreamResponse) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", raw)
	return err
}

func chunkRunes(text string, size int) []string {
	runes := []rune(text)
	if len(runes) == 0 {
		return []string{""}
	}
	if size <= 0 {
		size = 120
	}

	out := make([]string, 0, len(runes)/size+1)
	for start := 0; start < len(runes); start += size {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[start:end]))
	}
	return out
}

func configuredChatModels(defaultModel string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 3)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}

	add(defaultModel)
	add(agent.DefaultGroqKimiModel)
	if firstEnv("HUGGINGFACE_API_KEY", "HF_API_KEY", "HF_TOKEN") != "" {
		add(agent.DefaultHuggingFaceModel)
	}

	return out
}

func (s *Server) handleChatModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	defaultModel := strings.TrimSpace(os.Getenv("GROQ_MODEL"))
	if defaultModel == "" {
		defaultModel = strings.TrimSpace(os.Getenv("DEFAULT_CHAT_MODEL"))
	}
	if defaultModel == "" {
		defaultModel = agent.DefaultGroqModel
	}

	s.sendJSON(w, ChatModelsResponse{
		Models:       configuredChatModels(defaultModel),
		DefaultModel: defaultModel,
	})
}

func normalizeChatHistory(raw []agent.Message, maxMessages, maxChars int) []agent.Message {
	if maxMessages <= 0 {
		maxMessages = defaultMaxChatHistoryMessages
	}
	if maxChars <= 0 {
		maxChars = defaultMaxHistoryMessageChars
	}
	if len(raw) == 0 {
		return nil
	}

	start := 0
	if len(raw) > maxMessages {
		start = len(raw) - maxMessages
	}

	normalized := make([]agent.Message, 0, len(raw)-start)
	for _, m := range raw[start:] {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		if role != "user" && role != "assistant" {
			continue
		}

		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}

		runes := []rune(content)
		if len(runes) > maxChars {
			content = string(runes[:maxChars])
		}

		normalized = append(normalized, agent.Message{
			Role:    role,
			Content: content,
		})
	}

	return normalized
}
