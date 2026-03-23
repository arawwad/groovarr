package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"groovarr/internal/agent"

	"github.com/rs/zerolog/log"
)

type defaultTurnResolver struct{}

type groqTurnResolver struct {
	apiKey   string
	model    string
	fallback defaultTurnResolver
}

func newDefaultTurnResolver() chatTurnResolver {
	return defaultTurnResolver{}
}

func newGroqTurnResolver(apiKey, defaultModel string) chatTurnResolver {
	if !envBool("CHAT_RESOLVER_ENABLED", true) {
		return newDefaultTurnResolver()
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return newDefaultTurnResolver()
	}
	model := strings.TrimSpace(os.Getenv("CHAT_RESOLVER_MODEL"))
	if model == "" {
		model = strings.TrimSpace(defaultModel)
	}
	if model == "" {
		model = agent.DefaultGroqModel
	}
	return &groqTurnResolver{
		apiKey:   apiKey,
		model:    model,
		fallback: defaultTurnResolver{},
	}
}

func (defaultTurnResolver) ResolveTurn(_ context.Context, turnState *Turn) (resultSetResolverDecision, error) {
	request := buildResultSetResolverRequestFromTurn(turnState)
	turn := request.Turn
	decision := resultSetResolverDecision{
		SetKind:               strings.TrimSpace(turn.Reference.ResolvedSet),
		ItemKey:               strings.TrimSpace(turn.Reference.ResolvedItemKey),
		Operation:             strings.TrimSpace(turn.Workflow.Action),
		SelectionMode:         strings.TrimSpace(turn.Workflow.SelectionMode),
		SelectionValue:        strings.TrimSpace(turn.Workflow.SelectionValue),
		CompareSelectionMode:  strings.TrimSpace(turn.Workflow.CompareSelectionMode),
		CompareSelectionValue: strings.TrimSpace(turn.Workflow.CompareSelectionValue),
		Confidence:            strings.TrimSpace(turn.Confidence),
	}
	if decision.SetKind == "" {
		decision.SetKind = strings.TrimSpace(turn.Reference.RequestedSet)
	}
	if turn.NeedsClarification || turn.Reference.MissingContext || turn.Reference.Ambiguous {
		decision.NeedsClarification = true
		decision.ClarificationPrompt = strings.TrimSpace(turn.ClarificationPrompt)
		if decision.ClarificationPrompt == "" {
			decision.ClarificationPrompt = "Could you clarify what earlier result or item you mean?"
		}
		decision.Reason = "reference_or_clarification_required"
		return decision, nil
	}
	if decision.SelectionMode == "" {
		decision.SelectionMode = "all"
	}
	if decision.Confidence == "" {
		decision.Confidence = "medium"
	}
	decision.Reason = "structured_passthrough"
	return decision, nil
}

func (r *groqTurnResolver) ResolveTurn(ctx context.Context, turn *Turn) (resultSetResolverDecision, error) {
	request := buildResultSetResolverRequestFromTurn(turn)
	fallback, _ := r.fallback.ResolveTurn(ctx, turn)
	if request.Turn.NeedsClarification || request.Turn.Reference.MissingContext || request.Turn.Reference.Ambiguous {
		return sanitizeResolverDecision(fallback, request, fallback), nil
	}

	systemPrompt := `You resolve result-set follow-ups for a music assistant. Return strict JSON only.

Schema:
{
  "setKind": "set kind or empty",
  "itemKey": "focused item key or empty",
  "operation": "operation or empty",
  "selectionMode": "all|top_n|ordinal|explicit_names|missing_only|count_match|item_key|empty",
  "selectionValue": "compact selection payload or empty",
  "compareSelectionMode": "all|top_n|ordinal|explicit_names|item_key|empty",
  "compareSelectionValue": "compact secondary selection payload or empty",
  "needsClarification": false,
  "clarificationPrompt": "one concise question or empty",
  "reason": "short reason or empty",
  "confidence": "low|medium|high"
}

Rules:
- Use only set kinds, operations, and selectors allowed by the provided capabilities.
- Resolve to the already resolved set or item when the request is clearly about the last set or last item.
- Prefer item_key when a resolved item key already exists and the user is clearly referring to one item.
- Preserve compareSelectionMode / compareSelectionValue when the user wants one prior result compared against another.
- Ask for clarification instead of guessing when the referenced set or operation is ambiguous.
- Do not invent a new set kind or operation.
- Keep selectionValue compact.
- Return JSON only.`

	requestJSON, err := json.Marshal(request)
	if err != nil {
		return sanitizeResolverDecision(fallback, request, fallback), nil
	}

	timeoutMS := envInt("CHAT_RESOLVER_TIMEOUT_MS", 3000)
	if timeoutMS < 500 {
		timeoutMS = 500
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()

	raw, err := callGroqJSON(callCtx, r.apiKey, r.model, systemPrompt, string(requestJSON), envInt("CHAT_RESOLVER_MAX_TOKENS", 220))
	if err != nil {
		return sanitizeResolverDecision(fallback, request, fallback), nil
	}

	var decision resultSetResolverDecision
	if err := json.Unmarshal([]byte(raw), &decision); err != nil {
		return sanitizeResolverDecision(fallback, request, fallback), nil
	}
	return sanitizeResolverDecision(decision, request, fallback), nil
}

func sanitizeResolverDecision(decision resultSetResolverDecision, request resultSetResolverRequest, fallback resultSetResolverDecision) resultSetResolverDecision {
	if request.Turn.NeedsClarification || request.Turn.Reference.MissingContext || request.Turn.Reference.Ambiguous {
		if decision.NeedsClarification || strings.TrimSpace(decision.ClarificationPrompt) != "" {
			return sanitizeResolverClarification(mergeResolverDecision(decision, fallback))
		}
		return sanitizeResolverClarification(fallback)
	}

	rawSelectionMode := strings.TrimSpace(decision.SelectionMode)
	decision.SetKind = normalizeSupportedSetKind(strings.TrimSpace(decision.SetKind), request, strings.TrimSpace(fallback.SetKind))
	decision.ItemKey = strings.TrimSpace(decision.ItemKey)
	decision.Operation = normalizeSupportedValue(strings.TrimSpace(decision.Operation), capabilityOperations(request.Capabilities, decision.SetKind), strings.TrimSpace(fallback.Operation))
	decision.SelectionMode = normalizeSupportedValue(strings.TrimSpace(decision.SelectionMode), capabilitySelectors(request.Capabilities, decision.SetKind), strings.TrimSpace(fallback.SelectionMode))
	decision.SelectionValue = compactText(strings.TrimSpace(decision.SelectionValue), 120)
	decision.CompareSelectionMode = normalizeSupportedValue(strings.TrimSpace(decision.CompareSelectionMode), capabilitySelectors(request.Capabilities, decision.SetKind), strings.TrimSpace(fallback.CompareSelectionMode))
	decision.CompareSelectionValue = compactText(strings.TrimSpace(decision.CompareSelectionValue), 120)
	decision.Reason = compactText(strings.TrimSpace(decision.Reason), 180)
	decision.Confidence = normalizeEnum(strings.ToLower(strings.TrimSpace(decision.Confidence)), "medium", "low", "medium", "high")
	decision.ClarificationPrompt = compactText(strings.TrimSpace(decision.ClarificationPrompt), 220)

	if decision.SetKind == "" {
		decision.SetKind = strings.TrimSpace(fallback.SetKind)
	}
	if decision.Operation == "" {
		decision.Operation = strings.TrimSpace(fallback.Operation)
	}
	if decision.ItemKey == "" {
		decision.ItemKey = strings.TrimSpace(fallback.ItemKey)
	}

	selectors := capabilitySelectors(request.Capabilities, decision.SetKind)
	if decision.ItemKey != "" && containsResolverValue(selectors, "item_key") && (decision.SelectionMode == "" || decision.SelectionMode == "all") {
		decision.SelectionMode = "item_key"
	}
	if decision.SelectionMode == "" {
		decision.SelectionMode = strings.TrimSpace(fallback.SelectionMode)
	}
	if rawSelectionMode != "" && decision.SelectionMode != "" && rawSelectionMode != decision.SelectionMode {
		decision.SelectionValue = strings.TrimSpace(fallback.SelectionValue)
	}
	if decision.SelectionMode == "item_key" {
		if decision.ItemKey == "" {
			decision.ItemKey = strings.TrimSpace(fallback.ItemKey)
		}
		if decision.ItemKey == "" {
			decision.SelectionMode = strings.TrimSpace(fallback.SelectionMode)
			decision.SelectionValue = strings.TrimSpace(fallback.SelectionValue)
		} else {
			decision.SelectionValue = ""
		}
	}
	if decision.SelectionValue == "" && decision.SelectionMode != "all" && decision.SelectionMode != "item_key" {
		decision.SelectionValue = strings.TrimSpace(fallback.SelectionValue)
	}
	if decision.CompareSelectionMode == "item_key" && decision.CompareSelectionValue != "" {
		decision.CompareSelectionValue = ""
	}
	if strings.TrimSpace(request.Turn.Workflow.Action) == "compare" &&
		(strings.TrimSpace(request.Turn.Workflow.CompareSelectionMode) != "" ||
			strings.TrimSpace(request.Turn.Workflow.CompareSelectionValue) != "" ||
			strings.TrimSpace(request.Turn.Reference.Qualifier) != "") {
		decision.Operation = "compare"
		if decision.CompareSelectionMode == "" {
			decision.CompareSelectionMode = strings.TrimSpace(request.Turn.Workflow.CompareSelectionMode)
		}
		if decision.CompareSelectionValue == "" {
			decision.CompareSelectionValue = strings.TrimSpace(request.Turn.Workflow.CompareSelectionValue)
		}
	}
	if decision.Reason == "" {
		decision.Reason = strings.TrimSpace(fallback.Reason)
	}

	if decision.NeedsClarification {
		return sanitizeResolverClarification(decision)
	}
	decision.ClarificationPrompt = ""
	return decision
}

func mergeResolverDecision(primary, fallback resultSetResolverDecision) resultSetResolverDecision {
	merged := fallback
	if value := strings.TrimSpace(primary.SetKind); value != "" {
		merged.SetKind = value
	}
	if value := strings.TrimSpace(primary.ItemKey); value != "" {
		merged.ItemKey = value
	}
	if value := strings.TrimSpace(primary.Operation); value != "" {
		merged.Operation = value
	}
	if value := strings.TrimSpace(primary.SelectionMode); value != "" {
		merged.SelectionMode = value
	}
	if value := strings.TrimSpace(primary.SelectionValue); value != "" {
		merged.SelectionValue = value
	}
	if value := strings.TrimSpace(primary.CompareSelectionMode); value != "" {
		merged.CompareSelectionMode = value
	}
	if value := strings.TrimSpace(primary.CompareSelectionValue); value != "" {
		merged.CompareSelectionValue = value
	}
	if value := strings.TrimSpace(primary.ClarificationPrompt); value != "" {
		merged.ClarificationPrompt = value
	}
	if value := strings.TrimSpace(primary.Reason); value != "" {
		merged.Reason = value
	}
	if value := strings.TrimSpace(primary.Confidence); value != "" {
		merged.Confidence = value
	}
	if primary.NeedsClarification {
		merged.NeedsClarification = true
	}
	return merged
}

func sanitizeResolverClarification(decision resultSetResolverDecision) resultSetResolverDecision {
	decision.SetKind = strings.TrimSpace(decision.SetKind)
	decision.ItemKey = strings.TrimSpace(decision.ItemKey)
	decision.Operation = strings.TrimSpace(decision.Operation)
	decision.SelectionMode = strings.TrimSpace(decision.SelectionMode)
	decision.SelectionValue = compactText(strings.TrimSpace(decision.SelectionValue), 120)
	decision.NeedsClarification = true
	decision.ClarificationPrompt = compactText(strings.TrimSpace(decision.ClarificationPrompt), 220)
	if decision.ClarificationPrompt == "" {
		decision.ClarificationPrompt = "Could you clarify what you want me to act on?"
	}
	decision.Reason = compactText(strings.TrimSpace(decision.Reason), 180)
	if decision.Reason == "" {
		decision.Reason = "clarification_required"
	}
	decision.Confidence = normalizeEnum(strings.ToLower(strings.TrimSpace(decision.Confidence)), "medium", "low", "medium", "high")
	return decision
}

func normalizeSupportedSetKind(raw string, request resultSetResolverRequest, fallback string) string {
	eligible := eligibleResolverSetKinds(request)
	if len(eligible) > 0 {
		if containsResolverValue(eligible, raw) {
			return strings.TrimSpace(raw)
		}
		if containsResolverValue(eligible, fallback) {
			return strings.TrimSpace(fallback)
		}
		return ""
	}
	capabilities := request.Capabilities
	raw = strings.TrimSpace(raw)
	if raw != "" {
		for _, capability := range capabilities {
			if capability.SetKind == raw {
				return raw
			}
		}
	}
	fallback = strings.TrimSpace(fallback)
	if fallback != "" {
		for _, capability := range capabilities {
			if capability.SetKind == fallback {
				return fallback
			}
		}
	}
	return ""
}

func eligibleResolverSetKinds(request resultSetResolverRequest) []string {
	candidates := []string{
		strings.TrimSpace(request.Turn.Reference.ResolvedSet),
		strings.TrimSpace(request.Turn.Reference.RequestedSet),
	}
	eligible := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" || containsResolverValue(eligible, candidate) {
			continue
		}
		eligible = append(eligible, candidate)
	}
	return eligible
}

func capabilityOperations(capabilities []resultSetCapability, setKind string) []string {
	for _, capability := range capabilities {
		if capability.SetKind == strings.TrimSpace(setKind) {
			return capability.Operations
		}
	}
	return nil
}

func capabilitySelectors(capabilities []resultSetCapability, setKind string) []string {
	for _, capability := range capabilities {
		if capability.SetKind == strings.TrimSpace(setKind) {
			return capability.Selectors
		}
	}
	return nil
}

func normalizeSupportedValue(raw string, allowed []string, fallback string) string {
	raw = strings.TrimSpace(raw)
	if containsResolverValue(allowed, raw) {
		return raw
	}
	fallback = strings.TrimSpace(fallback)
	if containsResolverValue(allowed, fallback) {
		return fallback
	}
	return ""
}

func containsResolverValue(values []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == needle {
			return true
		}
	}
	return false
}

func (s *Server) maybeResolveExecutionRequest(ctx context.Context, turn *Turn) (*Turn, *resultSetResolverDecision) {
	if s.turnResolver == nil || turn == nil {
		return nil, nil
	}
	request := buildResultSetResolverRequestFromTurn(turn)
	decision, err := s.turnResolver.ResolveTurn(ctx, turn)
	if err != nil {
		log.Warn().Err(err).Str("request_id", chatRequestIDFromContext(ctx)).Msg("Chat resolver failed")
		return nil, nil
	}
	decision = sanitizeResolverDecision(decision, request, resultSetResolverDecision{})
	execReq := buildServerExecutionRequestFromTurn(turn, decision)
	turn = turn.withExecution(turnToExecution(execReq))
	return turn, &decision
}

func applyServerExecutionRequest(resolved *resolvedTurnContext, request serverExecutionRequest) *resolvedTurnContext {
	if resolved == nil {
		return nil
	}
	cloned := *resolved
	cloned.Turn = resolved.Turn
	if value := strings.TrimSpace(request.SetKind); value != "" {
		cloned.Turn.ResultSetKind = value
		cloned.ResolvedReferenceKind = value
	}
	if value := strings.TrimSpace(request.Operation); value != "" {
		cloned.Turn.ResultAction = value
		switch value {
		case "filter_by_play_window":
			cloned.Turn.SubIntent = "result_set_play_recency"
		case "most_recent":
			cloned.Turn.SubIntent = "result_set_most_recent"
		case "pick_riskier":
			cloned.Turn.SubIntent = "creative_risk_pick"
		case "pick_safer":
			cloned.Turn.SubIntent = "creative_safe_pick"
		case "refine_style":
			if strings.TrimSpace(cloned.Turn.SubIntent) == "" {
				cloned.Turn.SubIntent = "creative_refinement"
			}
		}
	}
	if value := strings.TrimSpace(request.SelectionMode); value != "" {
		cloned.Turn.SelectionMode = value
	}
	cloned.Turn.SelectionValue = strings.TrimSpace(request.SelectionValue)
	if value := strings.TrimSpace(request.CompareSelectionMode); value != "" {
		cloned.Turn.CompareSelectionMode = value
	}
	cloned.Turn.CompareSelectionValue = strings.TrimSpace(request.CompareSelectionValue)
	if value := strings.TrimSpace(request.ItemKey); value != "" {
		cloned.ResolvedItemKey = value
	}
	return &cloned
}

func (s *Server) executeServerExecutionRequest(ctx context.Context, history []agent.Message, turn *Turn) (ChatResponse, bool) {
	for _, handler := range currentServerExecutionHandlers() {
		if !handler.CanHandle(turn) {
			continue
		}
		if resp, ok := handler.Execute(ctx, s, history, turn); ok {
			return resp, true
		}
	}

	return ChatResponse{}, false
}
