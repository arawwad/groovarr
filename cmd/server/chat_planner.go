package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"groovarr/internal/agent"

	"github.com/rs/zerolog/log"
)

type groqTurnPlanner struct {
	apiKey string
	model  string
}

func newGroqTurnPlanner(apiKey, defaultModel string) chatTurnPlanner {
	if !envBool("CHAT_PLANNER_ENABLED", true) {
		return nil
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil
	}
	model := strings.TrimSpace(os.Getenv("CHAT_PLANNER_MODEL"))
	if model == "" {
		model = strings.TrimSpace(defaultModel)
	}
	if model == "" {
		model = agent.DefaultGroqModel
	}
	return &groqTurnPlanner{
		apiKey: apiKey,
		model:  model,
	}
}

func (p *groqTurnPlanner) PlanTurn(ctx context.Context, turn *Turn, history []agent.Message, sessionContext string) (orchestrationDecision, error) {
	resolved := turnToResolvedTurnContext(turn)
	msg := ""
	if turn != nil {
		msg = turn.UserMessage
	}
	systemPrompt := `You are a routing planner for a music assistant. Return strict JSON only.

Schema:
{
  "nextStage": "clarify|deterministic|resolver|responder",
  "deterministicMode": "normalized_first|none",
  "clarificationPrompt": "short question or empty",
  "reason": "short reason or empty",
  "confidence": "low|medium|high"
}

Rules:
- Use clarify when the normalized turn says clarification is needed or when referenced context is missing.
- Use deterministic when the request likely fits stable server routes such as listening summaries, listening stats, library stats, underplayed/library discovery, or playlist preview flows.
- Prefer deterministicMode=normalized_first when the normalized turn is strong and structured.
- Use resolver when the next step should be result-set or selection resolution before execution.
- Use responder for open-ended chat, broad interpretation, weakly grounded requests, or when deterministic coverage is unlikely.
- Keep clarificationPrompt empty unless nextStage is clarify.
- Return JSON only.`

	userPrompt := fmt.Sprintf(
		"User message:\n%s\n\nNormalized turn:\n%s\n\nRecent chat history:\n%s\n\nServer session context:\n%s",
		strings.TrimSpace(msg),
		renderPlannerResolvedTurn(resolved),
		renderNormalizerHistory(history),
		renderNormalizerSessionContext(sessionContext),
	)

	timeoutMS := envInt("CHAT_PLANNER_TIMEOUT_MS", 3500)
	if timeoutMS < 500 {
		timeoutMS = 500
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()

	raw, err := callGroqJSON(callCtx, p.apiKey, p.model, systemPrompt, userPrompt, envInt("CHAT_PLANNER_MAX_TOKENS", 180))
	if err != nil {
		return orchestrationDecision{}, err
	}
	var decision orchestrationDecision
	if err := json.Unmarshal([]byte(raw), &decision); err != nil {
		return orchestrationDecision{}, fmt.Errorf("failed to parse planner response: %w", err)
	}
	return sanitizeOrchestrationDecision(decision, resolved), nil
}

func sanitizeOrchestrationDecision(decision orchestrationDecision, resolved *resolvedTurnContext) orchestrationDecision {
	decision.NextStage = normalizeEnum(strings.ToLower(strings.TrimSpace(decision.NextStage)), "responder",
		"clarify", "deterministic", "resolver", "responder",
	)
	decision.DeterministicMode = normalizeEnum(strings.ToLower(strings.TrimSpace(decision.DeterministicMode)), "none",
		"normalized_first", "none",
	)
	decision.ClarificationPrompt = compactText(strings.TrimSpace(decision.ClarificationPrompt), 220)
	decision.Reason = compactText(strings.TrimSpace(decision.Reason), 180)
	decision.Confidence = normalizeEnum(strings.ToLower(strings.TrimSpace(decision.Confidence)), "medium",
		"low", "medium", "high",
	)
	if decision.NextStage != "clarify" {
		decision.ClarificationPrompt = ""
	}
	if decision.NextStage != "deterministic" {
		decision.DeterministicMode = "none"
	}
	if resolved != nil {
		if resolved.Turn.NeedsClarification {
			decision.NextStage = "clarify"
			if decision.ClarificationPrompt == "" {
				decision.ClarificationPrompt = strings.TrimSpace(resolved.Turn.ClarificationPrompt)
			}
		}
		if decision.NextStage == "clarify" && decision.ClarificationPrompt == "" {
			decision.ClarificationPrompt = "Could you clarify what you want me to focus on?"
		}
		switch strings.TrimSpace(resolved.Turn.SubIntent) {
		case "track_search", "track_similarity", "track_description", "artist_similarity", "artist_starting_album":
			if !resolved.Turn.NeedsClarification {
				decision.NextStage = "deterministic"
				decision.DeterministicMode = "normalized_first"
			}
		}
		if !resolved.Turn.NeedsClarification &&
			strings.TrimSpace(resolved.Turn.Intent) == "album_discovery" &&
			strings.TrimSpace(resolved.Turn.QueryScope) == "library" &&
			strings.TrimSpace(resolved.Turn.FollowupMode) == "none" &&
			buildStructuredCreativeLibraryQuery(resolved.Turn) != "" {
			decision.NextStage = "deterministic"
			decision.DeterministicMode = "normalized_first"
		}
		if !resolved.Turn.NeedsClarification &&
			strings.TrimSpace(resolved.Turn.Intent) == "album_discovery" &&
			strings.TrimSpace(resolved.Turn.QueryScope) != "library" &&
			strings.TrimSpace(resolved.Turn.FollowupMode) == "none" &&
			(strings.TrimSpace(resolved.Turn.ResultAction) == "" || strings.TrimSpace(resolved.Turn.ResultAction) == "none") {
			decision.NextStage = "deterministic"
			decision.DeterministicMode = "normalized_first"
		}
		if !resolved.Turn.NeedsClarification &&
			strings.TrimSpace(resolved.Turn.Intent) == "playlist" &&
			strings.TrimSpace(resolved.Turn.FollowupMode) == "none" &&
			(strings.TrimSpace(resolved.Turn.TargetName) == "" || strings.TrimSpace(resolved.Turn.SubIntent) == "playlist_vibe") {
			decision.NextStage = "deterministic"
			decision.DeterministicMode = "normalized_first"
		}
		if strings.TrimSpace(resolved.Turn.ResultSetKind) == "artist_candidates" &&
			strings.TrimSpace(resolved.Turn.ReferenceTarget) == "previous_results" &&
			isArtistAlbumFollowUpPrompt(strings.ToLower(strings.TrimSpace(resolved.Turn.RawMessage))) &&
			!resolved.Turn.NeedsClarification {
			decision.NextStage = "deterministic"
			decision.DeterministicMode = "normalized_first"
		}
		if !resolved.Turn.NeedsClarification &&
			strings.TrimSpace(resolved.Turn.ReferenceTarget) == "previous_results" &&
			(strings.TrimSpace(resolved.ResolvedReferenceKind) == "artist_candidates" ||
				strings.TrimSpace(resolved.Turn.ResultSetKind) == "artist_candidates") &&
			strings.TrimSpace(resolved.Turn.SelectionMode) != "" &&
			strings.TrimSpace(resolved.Turn.SelectionMode) != "none" &&
			strings.TrimSpace(resolved.Turn.ResultAction) != "compare" {
			decision.NextStage = "deterministic"
			decision.DeterministicMode = "normalized_first"
		}
		if decision.NextStage != "resolver" &&
			resolved.Turn.FollowupMode != "none" &&
			strings.TrimSpace(resolved.Turn.ReferenceTarget) == "previous_results" &&
			!resolved.Turn.NeedsClarification &&
			(strings.TrimSpace(resolved.ResolvedReferenceKind) != "" ||
				(strings.TrimSpace(resolved.Turn.ResultSetKind) != "" && strings.TrimSpace(resolved.Turn.ResultSetKind) != "none")) {
			decision.NextStage = "deterministic"
			decision.DeterministicMode = "normalized_first"
		}
		if strings.TrimSpace(resolved.Turn.ConversationOp) == "constrain" &&
			strings.TrimSpace(resolved.Turn.ReferenceTarget) == "previous_results" &&
			!resolved.Turn.NeedsClarification &&
			(strings.TrimSpace(resolved.ResolvedReferenceKind) != "" ||
				(strings.TrimSpace(resolved.Turn.ResultSetKind) != "" && strings.TrimSpace(resolved.Turn.ResultSetKind) != "none")) {
			decision.NextStage = "deterministic"
			decision.DeterministicMode = "normalized_first"
		}
		if strings.TrimSpace(resolved.Turn.ResultAction) == "compare" && strings.TrimSpace(resolved.Turn.ReferenceTarget) == "previous_results" {
			decision.NextStage = "resolver"
			decision.DeterministicMode = "none"
		}
	} else if decision.NextStage == "resolver" {
		decision.NextStage = "responder"
		decision.Reason = compactText("resolver_requires_normalized_turn", 180)
	}
	return decision
}

func renderPlannerResolvedTurn(resolved *resolvedTurnContext) string {
	return renderServerTurnRequest(resolved)
}

func (s *Server) maybePlanTurn(ctx context.Context, msg string, history []agent.Message, resolved *resolvedTurnContext, sessionContext string) *orchestrationDecision {
	if s.planner == nil {
		logChatPipelineStage(ctx, "planner_skipped", map[string]string{
			"message": msg,
		})
		return nil
	}
	turn := turnFromResolved(resolved)
	if turn != nil {
		turn.UserMessage = strings.TrimSpace(msg)
		turn.SessionID = chatSessionIDFromContext(ctx)
		turn = turn.withHistorySummary(renderNormalizerHistory(history))
	}
	decision, err := s.planner.PlanTurn(ctx, turn, history, sessionContext)
	if err != nil {
		log.Warn().Err(err).Str("request_id", chatRequestIDFromContext(ctx)).Msg("Chat planner failed")
		return nil
	}
	logChatPipelineStage(ctx, "planner", map[string]string{
		"message": msg,
		"plan":    buildOrchestrationDecisionContext(&decision),
	})
	return &decision
}

func (s *Server) executeOrchestrationDecision(ctx context.Context, msg string, history []agent.Message, resolved *resolvedTurnContext, decision *orchestrationDecision) (ChatResponse, bool) {
	if decision == nil {
		return ChatResponse{}, false
	}
	turn := turnFromResolved(resolved)
	if turn != nil {
		turn.UserMessage = strings.TrimSpace(msg)
		turn.SessionID = chatSessionIDFromContext(ctx)
		turn = turn.withHistorySummary(renderNormalizerHistory(history))
	}
	switch decision.NextStage {
	case "clarify":
		prompt := strings.TrimSpace(decision.ClarificationPrompt)
		if prompt == "" {
			return ChatResponse{}, false
		}
		logChatPipelineStage(ctx, "plan_executed", map[string]string{
			"next_stage": "clarify",
			"response":   prompt,
		})
		return ChatResponse{Response: prompt}, true
	case "deterministic":
		switch decision.DeterministicMode {
		case "normalized_first":
			if resp, ok := s.tryTurnIntentRoute(ctx, turn, history); ok {
				logChatPipelineStage(ctx, "plan_executed", map[string]string{
					"next_stage":         "deterministic",
					"deterministic_mode": decision.DeterministicMode,
					"response":           resp.Response,
					"has_pending_action": boolString(resp.PendingAction != nil),
				})
				return resp, true
			}
		default:
			if resp, ok := s.tryTurnIntentRoute(ctx, turn, history); ok {
				logChatPipelineStage(ctx, "plan_executed", map[string]string{
					"next_stage":         "deterministic",
					"deterministic_mode": decision.DeterministicMode,
					"response":           resp.Response,
					"has_pending_action": boolString(resp.PendingAction != nil),
				})
				return resp, true
			}
		}
	case "responder", "resolver":
		if decision.NextStage == "resolver" {
			turn, resolverDecision := s.maybeResolveExecutionRequest(ctx, turn)
			if resolverDecision != nil {
				logFields := map[string]string{
					"next_stage": decision.NextStage,
					"reason":     resolverDecision.Reason,
				}
				if turn != nil && strings.TrimSpace(turn.Execution.Operation) != "" {
					logFields["execution_request"] = renderServerExecutionRequest(executionRequestFromTurn(turn))
				}
				logChatPipelineStage(ctx, "resolver", logFields)
				if resolverDecision.NeedsClarification {
					return ChatResponse{Response: resolverDecision.ClarificationPrompt}, true
				}
				if turn != nil && strings.TrimSpace(turn.Execution.Operation) != "" {
					if resp, ok := s.executeServerExecutionRequest(ctx, history, turn); ok {
						return resp, true
					}
				}
			}
		}
		logChatPipelineStage(ctx, "plan_executed", map[string]string{
			"next_stage": decision.NextStage,
		})
		return ChatResponse{}, false
	}
	return ChatResponse{}, false
}

func buildOrchestrationDecisionContext(decision *orchestrationDecision) string {
	if decision == nil {
		return ""
	}
	parts := []string{
		fmt.Sprintf("next_stage=%q", decision.NextStage),
		fmt.Sprintf("deterministic_mode=%q", decision.DeterministicMode),
		fmt.Sprintf("confidence=%q", decision.Confidence),
	}
	if strings.TrimSpace(decision.ClarificationPrompt) != "" {
		parts = append(parts, fmt.Sprintf("clarification_prompt=%q", decision.ClarificationPrompt))
	}
	if strings.TrimSpace(decision.Reason) != "" {
		parts = append(parts, fmt.Sprintf("reason=%q", decision.Reason))
	}
	return "orchestration_decision: " + strings.Join(parts, "; ")
}
