package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"groovarr/graph"
	"groovarr/internal/agent"
	"groovarr/internal/discovery"
	"groovarr/internal/similarity"
)

type stubChatAgent struct {
	response    string
	err         error
	lastMsg     string
	lastHistory []agent.Message
	lastModel   string
	lastSignals *agent.TurnSignals
}

type stubTurnNormalizer struct {
	turn        normalizedTurn
	err         error
	decision    conversationObjectDecision
	decisionErr error
	lastMsg     string
	lastHistory []agent.Message
	lastContext string
	lastTurn    normalizedTurn
	lastObject  conversationObjectState
}

type stubTurnPlanner struct {
	plan        orchestrationDecision
	err         error
	lastMsg     string
	lastHistory []agent.Message
	lastContext string
	lastTurn    *Turn
}

type scriptedTurnNormalizer struct {
	normalize   func(msg string) normalizedTurn
	classify    func(msg string, turn normalizedTurn, object conversationObjectState) conversationObjectDecision
	lastMsg     string
	lastHistory []agent.Message
	lastContext string
	lastTurn    normalizedTurn
	lastObject  conversationObjectState
}

func testChatContext(sessionID, requestID string) context.Context {
	ctx := context.WithValue(context.Background(), chatSessionKey, sessionID)
	if requestID != "" {
		ctx = context.WithValue(ctx, chatRequestKey, requestID)
	}
	return ctx
}

func (s *stubChatAgent) ProcessQueryWithSignals(_ context.Context, userMsg string, history []agent.Message, modelOverride string, signals *agent.TurnSignals) (string, error) {
	s.lastMsg = userMsg
	s.lastModel = modelOverride
	s.lastHistory = append([]agent.Message(nil), history...)
	if signals != nil {
		copied := *signals
		s.lastSignals = &copied
	} else {
		s.lastSignals = nil
	}
	return s.response, s.err
}

func (s *stubTurnNormalizer) NormalizeTurn(_ context.Context, msg string, history []agent.Message, sessionContext string) (normalizedTurn, error) {
	s.lastMsg = msg
	s.lastHistory = append([]agent.Message(nil), history...)
	s.lastContext = sessionContext
	return s.turn, s.err
}

func (s *stubTurnNormalizer) ClassifyConversationObjectTurn(_ context.Context, msg string, history []agent.Message, sessionContext string, turn normalizedTurn, object conversationObjectState) (conversationObjectDecision, error) {
	s.lastMsg = msg
	s.lastHistory = append([]agent.Message(nil), history...)
	s.lastContext = sessionContext
	s.lastTurn = turn
	s.lastObject = object
	return s.decision, s.decisionErr
}

func (s *stubTurnPlanner) PlanTurn(_ context.Context, turn *Turn, history []agent.Message, sessionContext string) (orchestrationDecision, error) {
	if turn != nil {
		copied := *turn
		s.lastTurn = &copied
		s.lastMsg = turn.UserMessage
	} else {
		s.lastTurn = nil
		s.lastMsg = ""
	}
	s.lastHistory = append([]agent.Message(nil), history...)
	s.lastContext = sessionContext
	return s.plan, s.err
}

func (s *scriptedTurnNormalizer) NormalizeTurn(_ context.Context, msg string, history []agent.Message, sessionContext string) (normalizedTurn, error) {
	s.lastMsg = msg
	s.lastHistory = append([]agent.Message(nil), history...)
	s.lastContext = sessionContext
	if s.normalize == nil {
		return normalizedTurn{}, nil
	}
	return s.normalize(msg), nil
}

func (s *scriptedTurnNormalizer) ClassifyConversationObjectTurn(_ context.Context, msg string, history []agent.Message, sessionContext string, turn normalizedTurn, object conversationObjectState) (conversationObjectDecision, error) {
	s.lastMsg = msg
	s.lastHistory = append([]agent.Message(nil), history...)
	s.lastContext = sessionContext
	s.lastTurn = turn
	s.lastObject = object
	if s.classify == nil {
		return conversationObjectDecision{}, nil
	}
	return s.classify(msg, turn, object), nil
}

type stubTurnResolver struct {
	decision resultSetResolverDecision
	err      error
	lastTurn *Turn
}

func (s *stubTurnResolver) ResolveTurn(_ context.Context, turn *Turn) (resultSetResolverDecision, error) {
	if turn != nil {
		copied := *turn
		s.lastTurn = &copied
	} else {
		s.lastTurn = nil
	}
	return s.decision, s.err
}

func TestNormalizeChatHistoryFiltersRolesAndTruncates(t *testing.T) {
	raw := []agent.Message{
		{Role: "system", Content: "ignore me"},
		{Role: "user", Content: "  first  "},
		{Role: "assistant", Content: ""},
		{Role: "tool", Content: "ignore me too"},
		{Role: "assistant", Content: "second"},
		{Role: "USER", Content: "third message that is too long"},
	}

	got := normalizeChatHistory(raw, 5, 10)
	if len(got) != 3 {
		t.Fatalf("len(normalized) = %d, want 3", len(got))
	}
	if got[0].Role != "user" || got[0].Content != "first" {
		t.Fatalf("first message = %#v", got[0])
	}
	if got[1].Role != "assistant" || got[1].Content != "second" {
		t.Fatalf("second message = %#v", got[1])
	}
	if got[2].Role != "user" || got[2].Content != "third mess" {
		t.Fatalf("third message = %#v", got[2])
	}
}

func TestNormalizeChatHistoryKeepsLastMessagesOnly(t *testing.T) {
	raw := []agent.Message{
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "two"},
		{Role: "user", Content: "three"},
		{Role: "assistant", Content: "four"},
	}

	got := normalizeChatHistory(raw, 2, 20)
	if len(got) != 2 {
		t.Fatalf("len(normalized) = %d, want 2", len(got))
	}
	if got[0].Content != "three" || got[1].Content != "four" {
		t.Fatalf("normalized = %#v, want last two messages", got)
	}
}

func TestBuildChatResponseUsesConversationalPendingActionBeforeRoutes(t *testing.T) {
	srv := &Server{
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}
	srv.registerPendingAction("sess-chat", "test", "Approve", "approve this", nil, func(context.Context) (string, error) {
		return "approved from buildChatResponse", nil
	})

	ctx := testChatContext("sess-chat", "req-approve")
	resp, err := srv.buildChatResponse(ctx, "yes", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if resp.Response != "approved from buildChatResponse" {
		t.Fatalf("response = %q", resp.Response)
	}
	if resp.PendingAction != nil {
		t.Fatalf("expected no pending action after approval, got %#v", resp.PendingAction)
	}
}

func TestBuildChatResponseRoutesNormalPromptToAgent(t *testing.T) {
	agentStub := &stubChatAgent{response: "agent handled it"}
	srv := &Server{
		agent:         agentStub,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext("sess-clarify", "req-normal")
	resp, err := srv.buildChatResponse(ctx, "Tell me a joke.", []agent.Message{{Role: "user", Content: "hello"}}, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if resp.Response != "agent handled it" {
		t.Fatalf("response = %q", resp.Response)
	}
	if resp.PendingAction != nil {
		t.Fatalf("expected no pending action, got %#v", resp.PendingAction)
	}
	if agentStub.lastMsg != "Tell me a joke." {
		t.Fatalf("agent message = %q", agentStub.lastMsg)
	}
	if len(agentStub.lastHistory) != 2 || agentStub.lastHistory[0].Content != "hello" {
		t.Fatalf("agent history = %#v", agentStub.lastHistory)
	}
	if agentStub.lastHistory[1].Role != "assistant" || !strings.Contains(agentStub.lastHistory[1].Content, `structured_memory: active_request="hello"`) {
		t.Fatalf("agent structured memory = %#v", agentStub.lastHistory[1])
	}
}

func TestBuildChatResponseUsesNormalizerClarificationBeforeAgentForBroadDiscovery(t *testing.T) {
	agentStub := &stubChatAgent{response: "agent should not run"}
	normalizerStub := &stubTurnNormalizer{
		turn: normalizedTurn{
			Intent:              "album_discovery",
			FollowupMode:        "none",
			QueryScope:          "unknown",
			TimeWindow:          "none",
			NeedsClarification:  true,
			ClarificationFocus:  "scope",
			ReferenceTarget:     "none",
			Confidence:          "high",
			ClarificationPrompt: "Do you want the best albums in your library, or recommendations narrowed by artist, genre, era, or mood?",
		},
	}
	srv := &Server{
		agent:         agentStub,
		normalizer:    normalizerStub,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext("sess-deterministic", "req-deterministic")
	resp, err := srv.buildChatResponse(ctx, "Best albums", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	want := "Do you want the best albums in your library, or recommendations narrowed by artist, genre, era, or mood?"
	if resp.Response != want {
		t.Fatalf("response = %q, want %q", resp.Response, want)
	}
	if agentStub.lastMsg != "" {
		t.Fatalf("agent should not have been called, lastMsg = %q", agentStub.lastMsg)
	}
}

func TestBuildChatResponseUsesPlannerClarificationBeforeRoutes(t *testing.T) {
	agentStub := &stubChatAgent{response: "agent should not run"}
	plannerStub := &stubTurnPlanner{
		plan: orchestrationDecision{
			NextStage:           "clarify",
			DeterministicMode:   "none",
			ClarificationPrompt: "Do you mean library stats or listening stats?",
			Confidence:          "high",
		},
	}
	srv := &Server{
		agent:         agentStub,
		planner:       plannerStub,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext("sess-plan-clarify", "req-plan-clarify")
	resp, err := srv.buildChatResponse(ctx, "Give me stats.", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if resp.Response != "Do you mean library stats or listening stats?" {
		t.Fatalf("response = %q", resp.Response)
	}
	if agentStub.lastMsg != "" {
		t.Fatalf("agent should not have been called, lastMsg = %q", agentStub.lastMsg)
	}
}

func TestRenderConversationObjectDecisionContextIncludesCapabilities(t *testing.T) {
	contextText := renderConversationObjectDecisionContext(conversationObjectState{
		objectType:      "draft",
		kind:            "playlist_candidates",
		status:          "draft_preview",
		preferredIntent: "playlist",
		referenceTarget: "previous_playlist",
		queryScope:      "playlist",
		promptHint:      "foggy midnight drive",
	})
	if !strings.Contains(contextText, "conversationOps=apply,inspect,query,refine") {
		t.Fatalf("context = %q", contextText)
	}
	if !strings.Contains(contextText, "resultActions=inspect_availability") {
		t.Fatalf("context = %q", contextText)
	}
	if !strings.Contains(contextText, "refine_style") {
		t.Fatalf("context = %q", contextText)
	}
	if !strings.Contains(contextText, "selectors=all,item_key,top_n") {
		t.Fatalf("context = %q", contextText)
	}
}

func TestConversationObjectConversationOpsIncludesPreferredOp(t *testing.T) {
	ops := conversationObjectConversationOps(conversationObjectState{
		kind:        "creative_albums",
		status:      "result_set",
		preferredOp: "pivot",
	})
	if !reflect.DeepEqual(ops, []string{"apply", "constrain", "inspect", "pivot", "query", "refine", "select"}) {
		t.Fatalf("ops = %#v", ops)
	}
}

func TestBuildChatResponseUsesNormalizerClarificationBeforeAgent(t *testing.T) {
	agentStub := &stubChatAgent{response: "agent should not run"}
	normalizerStub := &stubTurnNormalizer{
		turn: normalizedTurn{
			Intent:              "album_discovery",
			FollowupMode:        "none",
			QueryScope:          "unknown",
			TimeWindow:          "none",
			NeedsClarification:  true,
			ClarificationFocus:  "scope",
			ReferenceTarget:     "none",
			Confidence:          "high",
			ClarificationPrompt: "Do you want those recommendations from your library, or more generally?",
		},
	}
	plannerStub := &stubTurnPlanner{
		plan: orchestrationDecision{
			NextStage:         "responder",
			DeterministicMode: "none",
			Confidence:        "high",
		},
	}
	srv := &Server{
		agent:         agentStub,
		normalizer:    normalizerStub,
		planner:       plannerStub,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext("sess-normalizer-clarify", "req-normalizer-clarify")
	resp, err := srv.buildChatResponse(ctx, "Best 5 Bjork albums", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	want := "Do you want those recommendations from your library, or more generally?"
	if resp.Response != want {
		t.Fatalf("response = %q, want %q", resp.Response, want)
	}
	if agentStub.lastMsg != "" {
		t.Fatalf("agent should not have been called, lastMsg = %q", agentStub.lastMsg)
	}
	if normalizerStub.lastMsg != "Best 5 Bjork albums" {
		t.Fatalf("normalizer message = %q", normalizerStub.lastMsg)
	}
}

func TestBuildChatResponseInjectsNormalizedTurnContextForAgent(t *testing.T) {
	agentStub := &stubChatAgent{response: "agent handled it"}
	normalizerStub := &stubTurnNormalizer{
		turn: normalizedTurn{
			Intent:             "stats",
			FollowupMode:       "none",
			QueryScope:         "stats",
			TimeWindow:         "none",
			NeedsClarification: false,
			ClarificationFocus: "none",
			ReferenceTarget:    "none",
			Confidence:         "high",
		},
	}
	plannerStub := &stubTurnPlanner{
		plan: orchestrationDecision{
			NextStage:         "responder",
			DeterministicMode: "none",
			Confidence:        "high",
		},
	}
	srv := &Server{
		agent:         agentStub,
		normalizer:    normalizerStub,
		planner:       plannerStub,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext("sess-normalized-history", "req-normalized-history")
	_, err := srv.buildChatResponse(ctx, "Which artists are pulling away from the rest this month?", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if len(agentStub.lastHistory) == 0 {
		t.Fatal("expected agent history to include normalized turn context")
	}
	found := false
	for _, msg := range agentStub.lastHistory {
		if msg.Role == "assistant" &&
			strings.Contains(msg.Content, `server_turn_request:`) &&
			strings.Contains(msg.Content, `"intent":"stats"`) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("agent history = %#v, want normalized turn context", agentStub.lastHistory)
	}
	foundPlan := false
	for _, msg := range agentStub.lastHistory {
		if msg.Role == "assistant" && strings.Contains(msg.Content, `orchestration_decision: next_stage="responder"`) {
			foundPlan = true
			break
		}
	}
	if !foundPlan {
		t.Fatalf("agent history = %#v, want orchestration decision context", agentStub.lastHistory)
	}
	if agentStub.lastSignals == nil || agentStub.lastSignals.Intent != "stats" || agentStub.lastSignals.QueryScope != "stats" {
		t.Fatalf("agent signals = %#v, want stats signals", agentStub.lastSignals)
	}
}

func TestBuildChatResponseUsesResolverClarificationBeforeAgent(t *testing.T) {
	agentStub := &stubChatAgent{response: "agent should not run"}
	normalizerStub := &stubTurnNormalizer{
		turn: normalizedTurn{
			Intent:          "album_discovery",
			FollowupMode:    "query_previous_set",
			QueryScope:      "general",
			TimeWindow:      "none",
			ResultSetKind:   "discovered_albums",
			ResultAction:    "preview_apply",
			SelectionMode:   "ordinal",
			SelectionValue:  "1",
			ReferenceTarget: "previous_results",
			Confidence:      "high",
		},
	}
	plannerStub := &stubTurnPlanner{
		plan: orchestrationDecision{
			NextStage:  "resolver",
			Confidence: "high",
		},
	}
	resolverStub := &stubTurnResolver{
		decision: resultSetResolverDecision{
			NeedsClarification:  true,
			ClarificationPrompt: "Do you mean the last discovery list or the last library cleanup preview?",
			Confidence:          "high",
		},
	}
	srv := &Server{
		agent:         agentStub,
		normalizer:    normalizerStub,
		planner:       plannerStub,
		turnResolver:  resolverStub,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	setLastDiscoveredAlbums("sess-resolver-clarify", "dream pop", []discoveredAlbumCandidate{
		{ArtistName: "Slowdive", AlbumTitle: "Souvlaki"},
	})
	setLastLidarrCandidates("sess-resolver-clarify", []lidarrCleanupCandidate{
		{AlbumID: 1, ArtistName: "The National", Title: "Sleep Well Beast"},
	})

	ctx := testChatContext("sess-resolver-clarify", "req-resolver-clarify")
	resp, err := srv.buildChatResponse(ctx, "Apply that one.", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if resp.Response != "Do you mean the last discovery list or the last library cleanup preview?" {
		t.Fatalf("response = %q", resp.Response)
	}
	if agentStub.lastMsg != "" {
		t.Fatalf("agent should not have been called, lastMsg = %q", agentStub.lastMsg)
	}
}

func TestBuildChatResponseUsesResolverForDeterministicSceneExecution(t *testing.T) {
	agentStub := &stubChatAgent{response: "agent should not run"}
	normalizerStub := &stubTurnNormalizer{
		turn: normalizedTurn{
			Intent:          "other",
			FollowupMode:    "query_previous_set",
			QueryScope:      "unknown",
			TimeWindow:      "none",
			ResultSetKind:   "scene_candidates",
			ResultAction:    "select_candidate",
			SelectionMode:   "count_match",
			SelectionValue:  "31",
			ReferenceTarget: "previous_results",
			Confidence:      "high",
		},
	}
	plannerStub := &stubTurnPlanner{
		plan: orchestrationDecision{
			NextStage:  "resolver",
			Confidence: "high",
		},
	}
	resolverStub := &stubTurnResolver{
		decision: resultSetResolverDecision{
			SetKind:        "scene_candidates",
			Operation:      "select_candidate",
			SelectionMode:  "count_match",
			SelectionValue: "31",
			Confidence:     "high",
		},
	}
	srv := &Server{
		agent:         agentStub,
		normalizer:    normalizerStub,
		planner:       plannerStub,
		turnResolver:  resolverStub,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}
	setLastSceneSelection("sess-resolver-scene", nil, []sceneSessionItem{
		{
			Key:       "Indie_Rock_Alternative_Medium_Relaxed_Sad_automatic",
			Name:      "Indie / Rock / Alternative • Mid-Tempo",
			Subtitle:  "Relaxed, Sad",
			SongCount: 31,
			SampleTracks: []audioMusePlaylistSong{
				{Title: "Nude", Author: "Radiohead"},
			},
		},
	})

	ctx := testChatContext("sess-resolver-scene", "req-resolver-scene")
	resp, err := srv.buildChatResponse(ctx, "Use the one with 31 tracks.", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if !strings.Contains(resp.Response, "Using Indie / Rock / Alternative • Mid-Tempo (Relaxed, Sad) with 31 tracks.") {
		t.Fatalf("response = %q", resp.Response)
	}
	if agentStub.lastMsg != "" {
		t.Fatalf("agent should not have been called, lastMsg = %q", agentStub.lastMsg)
	}
}

func TestBuildChatResponseClarifiesUnderspecifiedPlaylistCreateBeforeAgent(t *testing.T) {
	agentStub := &stubChatAgent{response: "agent should not run"}
	normalizerStub := &stubTurnNormalizer{
		turn: normalizedTurn{
			Intent:             "playlist",
			FollowupMode:       "none",
			QueryScope:         "playlist",
			TimeWindow:         "none",
			NeedsClarification: false,
			ClarificationFocus: "none",
			ReferenceTarget:    "none",
			Confidence:         "high",
		},
	}
	srv := &Server{
		agent:         agentStub,
		normalizer:    normalizerStub,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext("sess-playlist-clarify", "req-playlist-clarify")
	resp, err := srv.buildChatResponse(ctx, "Make me a playlist", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	want := "What kind of playlist do you want me to make?"
	if resp.Response != want {
		t.Fatalf("response = %q, want %q", resp.Response, want)
	}
	if agentStub.lastMsg != "" {
		t.Fatalf("agent should not have been called, lastMsg = %q", agentStub.lastMsg)
	}
}

func TestBuildChatResponseUsesNormalizedPlaylistIntentWithoutLegacyCue(t *testing.T) {
	agentStub := &stubChatAgent{response: "agent should not run"}
	normalizerStub := &stubTurnNormalizer{
		turn: normalizedTurn{
			Intent:             "playlist",
			FollowupMode:       "none",
			QueryScope:         "playlist",
			TimeWindow:         "none",
			NeedsClarification: false,
			ClarificationFocus: "none",
			ReferenceTarget:    "none",
			Confidence:         "high",
		},
	}
	srv := &Server{
		agent:         agentStub,
		normalizer:    normalizerStub,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext("sess-normalized-playlist", "req-normalized-playlist")
	resp, err := srv.buildChatResponse(ctx, "Spin up a playlist", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	want := "What kind of playlist do you want me to make?"
	if resp.Response != want {
		t.Fatalf("response = %q, want %q", resp.Response, want)
	}
	if agentStub.lastMsg != "" {
		t.Fatalf("agent should not have been called, lastMsg = %q", agentStub.lastMsg)
	}
}

func TestBuildChatResponseClarifiesStructuredPlaylistAppendWithoutTarget(t *testing.T) {
	agentStub := &stubChatAgent{response: "agent should not run"}
	normalizerStub := &stubTurnNormalizer{
		turn: normalizedTurn{
			Intent:             "playlist",
			SubIntent:          "playlist_append",
			FollowupMode:       "none",
			QueryScope:         "playlist",
			TimeWindow:         "none",
			PromptHint:         "colder tracks",
			NeedsClarification: false,
			ClarificationFocus: "none",
			ReferenceTarget:    "none",
			Confidence:         "high",
		},
	}
	srv := &Server{
		agent:         agentStub,
		normalizer:    normalizerStub,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext("sess-playlist-append-clarify", "req-playlist-append-clarify")
	resp, err := srv.buildChatResponse(ctx, "Add five colder tracks to that playlist", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	want := "What playlist would you like me to update?"
	if resp.Response != want {
		t.Fatalf("response = %q, want %q", resp.Response, want)
	}
	if agentStub.lastMsg != "" {
		t.Fatalf("agent should not have been called, lastMsg = %q", agentStub.lastMsg)
	}
}

func TestBuildChatResponseClarifiesStructuredPlaylistQueueWithoutTargetOrPrompt(t *testing.T) {
	agentStub := &stubChatAgent{response: "agent should not run"}
	normalizerStub := &stubTurnNormalizer{
		turn: normalizedTurn{
			Intent:             "playlist",
			SubIntent:          "playlist_queue_request",
			FollowupMode:       "none",
			QueryScope:         "playlist",
			TimeWindow:         "none",
			NeedsClarification: false,
			ClarificationFocus: "none",
			ReferenceTarget:    "none",
			Confidence:         "high",
		},
	}
	srv := &Server{
		agent:         agentStub,
		normalizer:    normalizerStub,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext("sess-playlist-queue-clarify", "req-playlist-queue-clarify")
	resp, err := srv.buildChatResponse(ctx, "Queue those playlist tracks", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	want := "Which playlist do you want me to inspect, or what kind of playlist should I prepare first?"
	if resp.Response != want {
		t.Fatalf("response = %q, want %q", resp.Response, want)
	}
	if agentStub.lastMsg != "" {
		t.Fatalf("agent should not have been called, lastMsg = %q", agentStub.lastMsg)
	}
}

func TestBuildChatResponseClarifiesStructuredArtistRemovalWithoutArtist(t *testing.T) {
	agentStub := &stubChatAgent{response: "agent should not run"}
	normalizerStub := &stubTurnNormalizer{
		turn: normalizedTurn{
			Intent:             "other",
			SubIntent:          "artist_remove",
			QueryScope:         "library",
			TimeWindow:         "none",
			NeedsClarification: false,
			ClarificationFocus: "none",
			ReferenceTarget:    "none",
			Confidence:         "high",
		},
	}
	srv := &Server{
		agent:         agentStub,
		normalizer:    normalizerStub,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext("sess-artist-remove-clarify", "req-artist-remove-clarify")
	resp, err := srv.buildChatResponse(ctx, "remove that artist from lidarr", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	want := "Which artist do you want me to remove from Lidarr?"
	if resp.Response != want {
		t.Fatalf("response = %q, want %q", resp.Response, want)
	}
	if agentStub.lastMsg != "" {
		t.Fatalf("agent should not have been called, lastMsg = %q", agentStub.lastMsg)
	}
}

func TestBuildChatResponseResolvesAmbiguousSceneClarificationBeforeAgent(t *testing.T) {
	agentStub := &stubChatAgent{response: "agent should not run"}
	normalizerStub := &stubTurnNormalizer{
		turn: normalizedTurn{
			Intent:             "other",
			FollowupMode:       "query_previous_set",
			QueryScope:         "unknown",
			TimeWindow:         "none",
			ResultSetKind:      "scene_candidates",
			ResultAction:       "select_candidate",
			SelectionMode:      "count_match",
			SelectionValue:     "31",
			NeedsClarification: false,
			ClarificationFocus: "none",
			ReferenceTarget:    "previous_results",
			Confidence:         "high",
		},
	}
	srv := &Server{
		agent:         agentStub,
		normalizer:    normalizerStub,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}
	setLastSceneSelection("sess-scene-clarify", nil, []sceneSessionItem{
		{
			Key:       "Indie_Rock_Alternative_Medium_Relaxed_Sad_automatic",
			Name:      "Indie / Rock / Alternative • Mid-Tempo",
			Subtitle:  "Relaxed, Sad",
			SongCount: 31,
			SampleTracks: []audioMusePlaylistSong{
				{Title: "Nude", Author: "Radiohead"},
				{Title: "Venice Bitch", Author: "Lana Del Rey"},
			},
		},
		{
			Key:       "Indie_Rock_Alternative_Medium_Sad_Happy_automatic",
			Name:      "Indie / Rock / Alternative • Mid-Tempo",
			Subtitle:  "Sad, Happy",
			SongCount: 23,
		},
	})

	ctx := testChatContext("sess-scene-clarify", "req-scene-clarify")
	resp, err := srv.buildChatResponse(ctx, "Use the one with 31 tracks.", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if !strings.Contains(resp.Response, "Using Indie / Rock / Alternative • Mid-Tempo (Relaxed, Sad) with 31 tracks.") {
		t.Fatalf("response = %q", resp.Response)
	}
	if !strings.Contains(resp.Response, "Nude by Radiohead") {
		t.Fatalf("response = %q, want sample tracks", resp.Response)
	}
	if agentStub.lastMsg != "" {
		t.Fatalf("agent should not have been called, lastMsg = %q", agentStub.lastMsg)
	}
	state, ok := getLastSceneSelection("sess-scene-clarify")
	if !ok || state.Resolved == nil {
		t.Fatalf("expected resolved scene state, got %#v", state)
	}
	if state.Resolved.SongCount != 31 {
		t.Fatalf("resolved scene song count = %d, want 31", state.Resolved.SongCount)
	}
}

func TestConfiguredChatModelsIncludesKimi(t *testing.T) {
	t.Setenv("HUGGINGFACE_API_KEY", "")

	got := configuredChatModels(agent.DefaultGroqModel)
	want := []string{agent.DefaultGroqModel, agent.DefaultGroqKimiModel}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("configuredChatModels() = %#v, want %#v", got, want)
	}
}

func TestConfiguredChatModelsDeduplicatesKimiWhenDefault(t *testing.T) {
	t.Setenv("HUGGINGFACE_API_KEY", "")

	got := configuredChatModels(agent.DefaultGroqKimiModel)
	want := []string{agent.DefaultGroqKimiModel}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("configuredChatModels() = %#v, want %#v", got, want)
	}
}

func TestBuildChatResponseSuppressesPendingActionOnDefaultFailure(t *testing.T) {
	agentStub := &stubChatAgent{response: "I couldn't complete that request after multiple attempts."}
	srv := &Server{
		agent:         agentStub,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}
	setLastPlannedPlaylist("sess-failure", "melancholy jazz", "Late Nights", []playlistCandidateTrack{
		{ArtistName: "Miles Davis", TrackTitle: "Blue in Green"},
	})

	ctx := testChatContext("sess-failure", "req-failure")
	resp, err := srv.buildChatResponse(ctx, "Make me a melancholy jazz playlist for late nights.", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if resp.PendingAction != nil {
		t.Fatalf("expected no pending action on default failure response, got %#v", resp.PendingAction)
	}
}

func TestBuildChatResponseUsesConversationalDiscardBeforeRoutes(t *testing.T) {
	srv := &Server{
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}
	srv.registerPendingAction("sess-chat-discard", "test", "Discard", "discard this", nil, func(context.Context) (string, error) {
		return "should not execute", nil
	})

	ctx := testChatContext("sess-chat-discard", "req-discard")
	resp, err := srv.buildChatResponse(ctx, "no", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if resp.Response != "Request discarded." {
		t.Fatalf("response = %q", resp.Response)
	}
	if resp.PendingAction != nil {
		t.Fatalf("expected no pending action after discard, got %#v", resp.PendingAction)
	}
}

func TestBuildChatResponseApproveWithoutPendingDoesNotFallThrough(t *testing.T) {
	srv := &Server{
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext("sess-no-pending", "req-none")
	resp, err := srv.buildChatResponse(ctx, "yes", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	want := "There isn't a pending action to approve right now."
	if resp.Response != want {
		t.Fatalf("response = %q, want %q", resp.Response, want)
	}
	if resp.PendingAction != nil {
		t.Fatalf("expected no pending action, got %#v", resp.PendingAction)
	}
}

func TestBuildChatResponseDoesNotAttachPendingActionFromDifferentRequest(t *testing.T) {
	agentStub := &stubChatAgent{response: "playlist list"}
	srv := &Server{
		agent:         agentStub,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}
	srv.registerPendingActionWithRequest(
		"sess-shared",
		"req-old",
		"playlist_append",
		"Update playlist",
		"stale append",
		nil,
		func(context.Context) (string, error) { return "ok", nil },
	)

	ctx := testChatContext("sess-shared", "req-new")
	resp, err := srv.buildChatResponse(ctx, "What playlists do I have?", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if resp.PendingAction != nil {
		t.Fatalf("expected no stale pending action, got %#v", resp.PendingAction)
	}
}

func TestBuildChatResponseAttachesPlaylistCreateActionForCurrentRequest(t *testing.T) {
	agentStub := &stubChatAgent{response: "playlist plan ready"}
	srv := &Server{
		agent:         agentStub,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}
	setLastPlannedPlaylist("sess-plan", "melancholy jazz", "Late Nights", []playlistCandidateTrack{
		{ArtistName: "Miles Davis", TrackTitle: "Blue in Green"},
	})

	ctx := testChatContext("sess-plan", "req-plan")
	resp, err := srv.buildChatResponse(ctx, "Make me a melancholy jazz playlist for late nights.", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if resp.PendingAction == nil {
		t.Fatal("expected pending action, got nil")
	}
	if resp.PendingAction.Kind != "playlist_create" {
		t.Fatalf("pending action kind = %q, want playlist_create", resp.PendingAction.Kind)
	}
}

func TestSelectHistoryForLLMKeepsAnchorForReferentialFollowUp(t *testing.T) {
	history := []agent.Message{
		{Role: "user", Content: "Find me some melancholic dream pop albums in my library."},
		{Role: "assistant", Content: "Here are some melancholic dream pop albums in your library."},
		{Role: "user", Content: "Thanks."},
		{Role: "assistant", Content: "Any time."},
		{Role: "user", Content: "What did I listen to last month?"},
		{Role: "assistant", Content: "Here are your top artists."},
		{Role: "user", Content: "Narrow that to the 90s."},
	}
	selected := selectHistoryForLLM(history, chatSessionMemory{
		ActiveRequest: "Find me some melancholic dream pop albums in my library.",
		UpdatedAt:     time.Now().UTC(),
	}, "Narrow that to the 90s.", 4)
	if len(selected) < 5 {
		t.Fatalf("len(selected) = %d, want anchor plus recent turns", len(selected))
	}
	if selected[0].Content != "Find me some melancholic dream pop albums in my library." {
		t.Fatalf("selected[0] = %#v", selected[0])
	}
}

func TestBuildChatResponseCarriesStructuredMemoryAcrossTurns(t *testing.T) {
	agentStub := &stubChatAgent{response: "first response"}
	srv := &Server{
		agent:         agentStub,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx1 := testChatContext("sess-memory", "req-1")
	if _, err := srv.buildChatResponse(ctx1, "Find me some melancholic dream pop albums in my library.", nil, ""); err != nil {
		t.Fatalf("first buildChatResponse() error = %v", err)
	}

	agentStub.response = "second response"
	ctx2 := testChatContext("sess-memory", "req-2")
	if _, err := srv.buildChatResponse(ctx2, "Narrow that to the 90s.", nil, ""); err != nil {
		t.Fatalf("second buildChatResponse() error = %v", err)
	}

	foundStructuredMemory := false
	for _, message := range agentStub.lastHistory {
		if message.Role == "assistant" && strings.Contains(message.Content, "structured_memory:") {
			foundStructuredMemory = true
			if !strings.Contains(message.Content, `active_request="Find me some melancholic dream pop albums in my library."`) {
				t.Fatalf("structured memory = %q", message.Content)
			}
		}
	}
	if !foundStructuredMemory {
		t.Fatalf("agent history missing structured memory: %#v", agentStub.lastHistory)
	}
}

func TestBuildChatResponseIncludesSemanticAlbumSessionContextForReferentialFollowUp(t *testing.T) {
	lastSemanticAlbumSearch.mu.Lock()
	lastSemanticAlbumSearch.sessions = make(map[string]semanticAlbumSearchState)
	lastSemanticAlbumSearch.mu.Unlock()

	setLastSemanticAlbumSearch("sess-semantic-followup", "melancholic dream pop", []semanticAlbumSearchMatch{
		{Name: "Life Of Leisure", ArtistName: "Washed Out", Year: 2009},
		{Name: "Ultraviolence", ArtistName: "Lana Del Rey", Year: 2014, PlayCount: 3, LastPlayed: "2026-03-06T12:28:13Z"},
	})

	agentStub := &stubChatAgent{response: "follow-up handled"}
	srv := &Server{
		agent:         agentStub,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext("sess-semantic-followup", "req-semantic-followup")
	if _, err := srv.buildChatResponse(ctx, "Which of those have I played recently?", nil, ""); err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}

	foundSemanticContext := false
	for _, message := range agentStub.lastHistory {
		if message.Role != "assistant" || !strings.Contains(message.Content, "last_semantic_album_search") {
			continue
		}
		foundSemanticContext = true
		if !strings.Contains(message.Content, "Ultraviolence by Lana Del Rey (2014)") {
			t.Fatalf("semantic session context = %q", message.Content)
		}
		if !strings.Contains(message.Content, "recent_matches=") {
			t.Fatalf("semantic session context = %q", message.Content)
		}
	}
	if !foundSemanticContext {
		t.Fatalf("agent history missing semantic session context: %#v", agentStub.lastHistory)
	}
}

func TestBuildChatResponseFallsBackToAgentForReferentialFollowUpWithExplicitHistory(t *testing.T) {
	agentStub := &stubChatAgent{response: "agent follow-up handled"}
	normalizerStub := &stubTurnNormalizer{
		turn: normalizedTurn{
			Intent:          "listening",
			SubIntent:       "listening_summary",
			FollowupMode:    "query_previous_set",
			QueryScope:      "library",
			ReferenceTarget: "previous_results",
			SelectionMode:   "top_n",
			SelectionValue:  "3",
			Confidence:      "high",
		},
	}
	srv := &Server{
		agent:         agentStub,
		normalizer:    normalizerStub,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	history := []agent.Message{
		{Role: "user", Content: "What are my top artists from the last month?"},
		{Role: "assistant", Content: "Top artists in this window:\n- Radiohead\n- Pink Floyd"},
	}

	ctx := testChatContext("sess-explicit-history-followup", "req-explicit-history-followup")
	resp, err := srv.buildChatResponse(ctx, "From those, give me three albums to revisit today.", history, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if resp.Response != "agent follow-up handled" {
		t.Fatalf("response = %q, want agent fallback", resp.Response)
	}
	if agentStub.lastMsg == "" {
		t.Fatal("expected agent to be called")
	}
}

func TestBuildChatResponseTranscriptCreativeAlbumsThenRecencyFollowUp(t *testing.T) {
	sessionID := "sess-transcript-creative-recency"
	setLastCreativeAlbumSet(sessionID, "semantic", "moody commute", []creativeAlbumCandidate{
		{Name: "Sheer Heart Attack", ArtistName: "Queen", LastPlayed: time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)},
		{Name: "That's All", ArtistName: "Mel Torme", LastPlayed: time.Now().UTC().AddDate(-2, 0, 0).Format(time.RFC3339)},
	})
	setLastActiveFocusFromTurn(sessionID, "creative_albums", "result_set", normalizedTurn{
		Intent:          "album_discovery",
		QueryScope:      "library",
		FollowupMode:    "query_previous_set",
		ReferenceTarget: "previous_results",
		PromptHint:      "moody commute",
	})

	normalizer := &scriptedTurnNormalizer{
		normalize: func(msg string) normalizedTurn {
			if strings.TrimSpace(msg) != "Which of those have I played recently?" {
				return normalizedTurn{}
			}
			return normalizedTurn{
				Intent:       "listening",
				SubIntent:    "result_set_play_recency",
				QueryScope:   "library",
				FollowupMode: "none",
				TimeWindow:   "this_year",
				Confidence:   "high",
			}
		},
		classify: func(msg string, turn normalizedTurn, object conversationObjectState) conversationObjectDecision {
			if strings.TrimSpace(msg) != "Which of those have I played recently?" {
				return conversationObjectDecision{}
			}
			return conversationObjectDecision{
				UseActiveObject:    true,
				FollowupMode:       "query_previous_set",
				ReferenceTarget:    "previous_results",
				ConversationOp:     "inspect",
				SubIntentOverride:  "result_set_play_recency",
				QueryScopeOverride: "library",
				ResultSetKind:      "creative_albums",
				Confidence:         "high",
			}
		},
	}

	srv := &Server{
		normalizer:    normalizer,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext(sessionID, "req-transcript-creative-recency")
	resp, err := srv.buildChatResponse(ctx, "Which of those have I played recently?", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if !strings.Contains(resp.Response, "Sheer Heart Attack") {
		t.Fatalf("response = %q", resp.Response)
	}
	if strings.Contains(resp.Response, "That's All") {
		t.Fatalf("response = %q, did not expect stale album", resp.Response)
	}
	if normalizer.lastObject.kind != "creative_albums" {
		t.Fatalf("conversation object kind = %q, want creative_albums", normalizer.lastObject.kind)
	}
}

func TestBuildChatResponseTranscriptDiscoveredAlbumsThenRiskierPick(t *testing.T) {
	originalRunner := discoverAlbumsRequestRunner
	discoverAlbumsRequestRunner = func(_ context.Context, request discovery.Request) ([]discoveredAlbumCandidate, map[string]interface{}, error) {
		return []discoveredAlbumCandidate{
			{Rank: 1, AlbumTitle: "Risk Surface", ArtistName: "Artist Two", Year: 2007, Reason: "stranger nocturnal edges"},
			{Rank: 2, AlbumTitle: "Safe Harbor", ArtistName: "Artist One", Year: 2003, Reason: "familiar late-night drift"},
			{Rank: 3, AlbumTitle: "Middle Ground", ArtistName: "Artist Three", Year: 2001, Reason: "steady rain mood"},
		}, map[string]interface{}{"query": request.Query}, nil
	}
	defer func() {
		discoverAlbumsRequestRunner = originalRunner
	}()

	normalizer := &scriptedTurnNormalizer{
		normalize: func(msg string) normalizedTurn {
			switch strings.TrimSpace(msg) {
			case "Give me three records for a rainy late-night walk.":
				return normalizedTurn{
					Intent:         "album_discovery",
					SubIntent:      "creative_refinement",
					QueryScope:     "general",
					FollowupMode:   "none",
					SelectionMode:  "top_n",
					SelectionValue: "3",
					PromptHint:     "rainy late-night walk",
					Confidence:     "high",
				}
			case "Pick the riskier one.":
				return normalizedTurn{
					Intent:       "album_discovery",
					SubIntent:    "creative_risk_pick",
					QueryScope:   "general",
					FollowupMode: "none",
					Confidence:   "high",
				}
			default:
				return normalizedTurn{}
			}
		},
		classify: func(msg string, turn normalizedTurn, object conversationObjectState) conversationObjectDecision {
			if strings.TrimSpace(msg) != "Pick the riskier one." {
				return conversationObjectDecision{}
			}
			return conversationObjectDecision{
				UseActiveObject:   true,
				FollowupMode:      "refine_previous",
				ReferenceTarget:   "previous_results",
				ConversationOp:    "select",
				SubIntentOverride: "creative_risk_pick",
				ResultSetKind:     "discovered_albums",
				Confidence:        "high",
			}
		},
	}

	srv := &Server{
		normalizer:    normalizer,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx1 := testChatContext("sess-transcript-discovered-risk", "req-transcript-discovered-risk-1")
	resp1, err := srv.buildChatResponse(ctx1, "Give me three records for a rainy late-night walk.", nil, "")
	if err != nil {
		t.Fatalf("first buildChatResponse() error = %v", err)
	}
	if !strings.Contains(resp1.Response, "Risk Surface by Artist Two") {
		t.Fatalf("first response = %q", resp1.Response)
	}

	ctx2 := testChatContext("sess-transcript-discovered-risk", "req-transcript-discovered-risk-2")
	resp2, err := srv.buildChatResponse(ctx2, "Pick the riskier one.", nil, "")
	if err != nil {
		t.Fatalf("second buildChatResponse() error = %v", err)
	}
	if !strings.Contains(resp2.Response, "The riskier pick is Risk Surface by Artist Two") {
		t.Fatalf("second response = %q", resp2.Response)
	}
	if normalizer.lastObject.kind != "discovered_albums" {
		t.Fatalf("conversation object kind = %q, want discovered_albums", normalizer.lastObject.kind)
	}
}

func TestBuildChatResponseTranscriptTrackCandidatesThenCompare(t *testing.T) {
	sessionID := "sess-transcript-track-compare"
	setLastTrackCandidateSet(sessionID, "similar_tracks", "Windowlicker", []trackCandidate{
		{ID: "1", Title: "Balaclava", ArtistName: "Arctic Monkeys", Score: 0.91},
		{ID: "2", Title: "Doll", ArtistName: "Foo Fighters", Score: 0.83},
	})
	setLastActiveFocusFromTurn(sessionID, "track_candidates", "result_set", normalizedTurn{
		Intent:     "track_discovery",
		QueryScope: "library",
		PromptHint: "Windowlicker",
	})

	normalizer := &scriptedTurnNormalizer{
		normalize: func(msg string) normalizedTurn {
			if strings.TrimSpace(msg) != "Compare the second one to the first." {
				return normalizedTurn{}
			}
			return normalizedTurn{
				Intent:                "general_chat",
				SubIntent:             "compare",
				QueryScope:            "library",
				ResultAction:          "compare",
				CompareSelectionMode:  "ordinal",
				CompareSelectionValue: "1",
				Confidence:            "high",
				RawMessage:            msg,
			}
		},
		classify: func(msg string, turn normalizedTurn, object conversationObjectState) conversationObjectDecision {
			if strings.TrimSpace(msg) != "Compare the second one to the first." {
				return conversationObjectDecision{}
			}
			return conversationObjectDecision{
				UseActiveObject: true,
				FollowupMode:    "query_previous_set",
				ReferenceTarget: "previous_results",
				ConversationOp:  "compare",
				ResultSetKind:   "track_candidates",
				SelectionMode:   "ordinal",
				SelectionValue:  "2",
				Confidence:      "high",
			}
		},
	}

	srv := &Server{
		normalizer:    normalizer,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext(sessionID, "req-transcript-track-compare")
	resp, err := srv.buildChatResponse(ctx, "Compare the second one to the first.", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if !strings.Contains(resp.Response, "Selected anchor: Doll by Foo Fighters") {
		t.Fatalf("response = %q", resp.Response)
	}
	if !strings.Contains(resp.Response, "comparison target: Balaclava by Arctic Monkeys") {
		t.Fatalf("response = %q", resp.Response)
	}
	if normalizer.lastObject.kind != "track_candidates" {
		t.Fatalf("conversation object kind = %q, want track_candidates", normalizer.lastObject.kind)
	}
}

func TestBuildChatResponseTranscriptArtistCandidatesThenCompare(t *testing.T) {
	sessionID := "sess-transcript-artist-compare"
	setLastArtistCandidateSet(sessionID, "Radiohead", []artistCandidate{
		{ID: "1", Name: "Blur", Score: 0.91},
		{ID: "2", Name: "Coldplay", Score: 0.87},
	})
	setLastActiveFocusFromTurn(sessionID, "artist_candidates", "result_set", normalizedTurn{
		Intent:     "artist_discovery",
		QueryScope: "library",
		PromptHint: "Radiohead",
	})

	normalizer := &scriptedTurnNormalizer{
		normalize: func(msg string) normalizedTurn {
			if strings.TrimSpace(msg) != "Compare the second one to the first." {
				return normalizedTurn{}
			}
			return normalizedTurn{
				Intent:                "general_chat",
				SubIntent:             "compare",
				QueryScope:            "library",
				ResultAction:          "compare",
				CompareSelectionMode:  "ordinal",
				CompareSelectionValue: "1",
				Confidence:            "high",
				RawMessage:            msg,
			}
		},
		classify: func(msg string, turn normalizedTurn, object conversationObjectState) conversationObjectDecision {
			if strings.TrimSpace(msg) != "Compare the second one to the first." {
				return conversationObjectDecision{}
			}
			return conversationObjectDecision{
				UseActiveObject: true,
				FollowupMode:    "query_previous_set",
				ReferenceTarget: "previous_results",
				ConversationOp:  "compare",
				ResultSetKind:   "artist_candidates",
				SelectionMode:   "ordinal",
				SelectionValue:  "2",
				Confidence:      "high",
			}
		},
	}

	srv := &Server{
		normalizer:    normalizer,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext(sessionID, "req-transcript-artist-compare")
	resp, err := srv.buildChatResponse(ctx, "Compare the second one to the first.", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if !strings.Contains(resp.Response, "Selected anchor: Coldplay") {
		t.Fatalf("response = %q", resp.Response)
	}
	if !strings.Contains(resp.Response, "comparison target: Blur") {
		t.Fatalf("response = %q", resp.Response)
	}
	if normalizer.lastObject.kind != "artist_candidates" {
		t.Fatalf("conversation object kind = %q, want artist_candidates", normalizer.lastObject.kind)
	}
}

func TestBuildChatResponseTranscriptCreativeAlbumsThenRefine(t *testing.T) {
	sessionID := "sess-transcript-creative-refine"
	setLastCreativeAlbumSet(sessionID, "semantic_structured", "foggy midnight drive", []creativeAlbumCandidate{
		{Name: "Untrue", ArtistName: "Burial", Genre: "electronic ambient techno", PlayCount: 2},
		{Name: "Pink Moon", ArtistName: "Nick Drake", Genre: "folk acoustic singer-songwriter", PlayCount: 5},
		{Name: "Moon Safari", ArtistName: "Air", Genre: "electronic downtempo", PlayCount: 4},
	})
	setLastActiveFocusFromTurn(sessionID, "creative_albums", "result_set", normalizedTurn{
		Intent:     "album_discovery",
		QueryScope: "library",
		PromptHint: "foggy midnight drive",
	})

	normalizer := &scriptedTurnNormalizer{
		normalize: func(msg string) normalizedTurn {
			if strings.TrimSpace(msg) != "Make it less electronic and more intimate." {
				return normalizedTurn{}
			}
			return normalizedTurn{
				Intent:     "album_discovery",
				SubIntent:  "creative_refinement",
				QueryScope: "library",
				StyleHints: []string{"less electronic", "more intimate"},
				Confidence: "high",
				RawMessage: msg,
			}
		},
		classify: func(msg string, turn normalizedTurn, object conversationObjectState) conversationObjectDecision {
			if strings.TrimSpace(msg) != "Make it less electronic and more intimate." {
				return conversationObjectDecision{}
			}
			return conversationObjectDecision{
				UseActiveObject:    true,
				FollowupMode:       "refine_previous",
				ReferenceTarget:    "previous_results",
				ConversationOp:     "refine",
				ResultSetKind:      "creative_albums",
				SubIntentOverride:  "creative_refinement",
				QueryScopeOverride: "library",
				Confidence:         "high",
			}
		},
	}

	srv := &Server{
		normalizer:    normalizer,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext(sessionID, "req-transcript-creative-refine")
	resp, err := srv.buildChatResponse(ctx, "Make it less electronic and more intimate.", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if !strings.Contains(resp.Response, "Reshaping those picks") {
		t.Fatalf("response = %q", resp.Response)
	}
	if !strings.Contains(resp.Response, "Pink Moon by Nick Drake") {
		t.Fatalf("response = %q", resp.Response)
	}
	if normalizer.lastObject.kind != "creative_albums" {
		t.Fatalf("conversation object kind = %q, want creative_albums", normalizer.lastObject.kind)
	}
}

func TestBuildChatResponseTranscriptArtistCandidatesThenSaferPick(t *testing.T) {
	sessionID := "sess-transcript-artist-safe"
	setLastArtistCandidateSet(sessionID, "Radiohead", []artistCandidate{
		{ID: "1", Name: "Broadcast", PlayCount: 1, Rating: 5, Score: 0.91},
		{ID: "2", Name: "Coldplay", PlayCount: 12, Rating: 8, Score: 0.87},
		{ID: "3", Name: "Elbow", PlayCount: 4, Rating: 7, Score: 0.84},
	})
	setLastActiveFocusFromTurn(sessionID, "artist_candidates", "result_set", normalizedTurn{
		Intent:     "artist_discovery",
		QueryScope: "library",
		PromptHint: "Radiohead",
	})

	normalizer := &scriptedTurnNormalizer{
		normalize: func(msg string) normalizedTurn {
			if strings.TrimSpace(msg) != "Pick the safer one." {
				return normalizedTurn{}
			}
			return normalizedTurn{
				Intent:     "general_chat",
				SubIntent:  "creative_safe_pick",
				QueryScope: "library",
				Confidence: "high",
				RawMessage: msg,
			}
		},
		classify: func(msg string, turn normalizedTurn, object conversationObjectState) conversationObjectDecision {
			if strings.TrimSpace(msg) != "Pick the safer one." {
				return conversationObjectDecision{}
			}
			return conversationObjectDecision{
				UseActiveObject: true,
				FollowupMode:    "query_previous_set",
				ReferenceTarget: "previous_results",
				ConversationOp:  "select",
				ResultSetKind:   "artist_candidates",
				Confidence:      "high",
			}
		},
	}

	srv := &Server{
		normalizer:    normalizer,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext(sessionID, "req-transcript-artist-safe")
	resp, err := srv.buildChatResponse(ctx, "Pick the safer one.", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if !strings.Contains(resp.Response, "The safer one is Coldplay") {
		t.Fatalf("response = %q", resp.Response)
	}
	if normalizer.lastObject.kind != "artist_candidates" {
		t.Fatalf("conversation object kind = %q, want artist_candidates", normalizer.lastObject.kind)
	}
}

func TestBuildChatResponseTranscriptCreativeAlbumsThenMostRecent(t *testing.T) {
	sessionID := "sess-transcript-creative-most-recent"
	setLastCreativeAlbumSet(sessionID, "semantic_structured", "night walk", []creativeAlbumCandidate{
		{Name: "Moon Safari", ArtistName: "Air", LastPlayed: time.Now().UTC().Add(-72 * time.Hour).Format(time.RFC3339)},
		{Name: "Dummy", ArtistName: "Portishead", LastPlayed: time.Now().UTC().Add(-12 * time.Hour).Format(time.RFC3339)},
	})
	setLastActiveFocusFromTurn(sessionID, "creative_albums", "result_set", normalizedTurn{
		Intent:     "album_discovery",
		QueryScope: "library",
		PromptHint: "night walk",
	})

	normalizer := &scriptedTurnNormalizer{
		normalize: func(msg string) normalizedTurn {
			if strings.TrimSpace(msg) != "Which one did I play most recently?" {
				return normalizedTurn{}
			}
			return normalizedTurn{
				Intent:     "listening",
				SubIntent:  "result_set_most_recent",
				QueryScope: "library",
				Confidence: "high",
				RawMessage: msg,
			}
		},
		classify: func(msg string, turn normalizedTurn, object conversationObjectState) conversationObjectDecision {
			if strings.TrimSpace(msg) != "Which one did I play most recently?" {
				return conversationObjectDecision{}
			}
			return conversationObjectDecision{
				UseActiveObject:    true,
				FollowupMode:       "query_previous_set",
				ReferenceTarget:    "previous_results",
				ConversationOp:     "inspect",
				ResultSetKind:      "creative_albums",
				SubIntentOverride:  "result_set_most_recent",
				QueryScopeOverride: "library",
				Confidence:         "high",
			}
		},
	}

	srv := &Server{
		normalizer:    normalizer,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext(sessionID, "req-transcript-creative-most-recent")
	resp, err := srv.buildChatResponse(ctx, "Which one did I play most recently?", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if !strings.Contains(resp.Response, "The one you've touched most recently is Dummy by Portishead") {
		t.Fatalf("response = %q", resp.Response)
	}
	if normalizer.lastObject.kind != "creative_albums" {
		t.Fatalf("conversation object kind = %q, want creative_albums", normalizer.lastObject.kind)
	}
}

func TestBuildChatResponseTranscriptCreativeAlbumsThenArtistFilter(t *testing.T) {
	sessionID := "sess-transcript-creative-artist-filter"
	setLastCreativeAlbumSet(sessionID, "semantic_structured", "rainy late-night walk", []creativeAlbumCandidate{
		{Name: "Kid A", ArtistName: "Radiohead", PlayCount: 12, LastPlayed: "2026-03-01T12:00:00Z"},
		{Name: "Amnesiac", ArtistName: "Radiohead", PlayCount: 4, LastPlayed: "2026-02-01T12:00:00Z"},
		{Name: "Moon Safari", ArtistName: "Air", PlayCount: 2, LastPlayed: "2026-03-05T12:00:00Z"},
	})
	setLastActiveFocusFromTurn(sessionID, "creative_albums", "result_set", normalizedTurn{
		Intent:     "album_discovery",
		QueryScope: "library",
		PromptHint: "rainy late-night walk",
	})

	normalizer := &scriptedTurnNormalizer{
		normalize: func(msg string) normalizedTurn {
			if strings.TrimSpace(msg) != "Then give me one Radiohead album I should revisit tonight." {
				return normalizedTurn{}
			}
			return normalizedTurn{
				Intent:         "album_discovery",
				QueryScope:     "library",
				ArtistName:     "Radiohead",
				SelectionMode:  "top_n",
				SelectionValue: "1",
				Confidence:     "high",
				RawMessage:     msg,
			}
		},
		classify: func(msg string, turn normalizedTurn, object conversationObjectState) conversationObjectDecision {
			if strings.TrimSpace(msg) != "Then give me one Radiohead album I should revisit tonight." {
				return conversationObjectDecision{}
			}
			return conversationObjectDecision{
				UseActiveObject:    true,
				FollowupMode:       "query_previous_set",
				ReferenceTarget:    "previous_results",
				ConversationOp:     "select",
				ResultSetKind:      "creative_albums",
				QueryScopeOverride: "library",
				Confidence:         "high",
			}
		},
	}

	srv := &Server{
		normalizer:    normalizer,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext(sessionID, "req-transcript-creative-artist-filter")
	resp, err := srv.buildChatResponse(ctx, "Then give me one Radiohead album I should revisit tonight.", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if !strings.Contains(resp.Response, "Kid A by Radiohead") {
		t.Fatalf("response = %q", resp.Response)
	}
	if strings.Contains(resp.Response, "Moon Safari") {
		t.Fatalf("response leaked non-Radiohead candidate: %q", resp.Response)
	}
	if normalizer.lastObject.kind != "creative_albums" {
		t.Fatalf("conversation object kind = %q, want creative_albums", normalizer.lastObject.kind)
	}
}

func TestBuildChatResponseTranscriptCreativeAlbumsThenConstrainToLibrary(t *testing.T) {
	sessionID := "sess-transcript-creative-constrain-library"
	setLastCreativeAlbumSet(sessionID, "semantic_album_search", "rainy late-night walk", []creativeAlbumCandidate{
		{Name: "Play", ArtistName: "Moby"},
		{Name: "The Campfire Headphase", ArtistName: "Boards of Canada"},
	})
	setLastActiveFocusFromTurn(sessionID, "creative_albums", "result_set", normalizedTurn{
		Intent:     "album_discovery",
		QueryScope: "general",
		PromptHint: "rainy late-night walk",
	})

	normalizer := &scriptedTurnNormalizer{
		normalize: func(msg string) normalizedTurn {
			if strings.TrimSpace(msg) != "Actually keep it to my library." {
				return normalizedTurn{}
			}
			libraryOnly := true
			return normalizedTurn{
				Intent:      "album_discovery",
				QueryScope:  "library",
				LibraryOnly: &libraryOnly,
				Confidence:  "high",
				RawMessage:  msg,
			}
		},
		classify: func(msg string, turn normalizedTurn, object conversationObjectState) conversationObjectDecision {
			if strings.TrimSpace(msg) != "Actually keep it to my library." {
				return conversationObjectDecision{}
			}
			return conversationObjectDecision{
				UseActiveObject:    true,
				FollowupMode:       "query_previous_set",
				ReferenceTarget:    "previous_results",
				ConversationOp:     "constrain",
				ResultSetKind:      "creative_albums",
				QueryScopeOverride: "library",
				Confidence:         "high",
			}
		},
	}

	srv := &Server{
		normalizer:    normalizer,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext(sessionID, "req-transcript-creative-constrain-library")
	resp, err := srv.buildChatResponse(ctx, "Actually keep it to my library.", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if !strings.Contains(resp.Response, "From your library") {
		t.Fatalf("response = %q", resp.Response)
	}
	if !strings.Contains(resp.Response, "Play by Moby") {
		t.Fatalf("response = %q", resp.Response)
	}
	if normalizer.lastObject.kind != "creative_albums" {
		t.Fatalf("conversation object kind = %q, want creative_albums", normalizer.lastObject.kind)
	}
}

func TestBuildChatResponseTranscriptAlbumLibraryLookupThenWhatAbout(t *testing.T) {
	originalToolRunner := executeToolWithSimilarityImpl
	executeToolWithSimilarityImpl = func(_ context.Context, _ *graph.Resolver, _ *similarity.Service, _ string, tool string, args map[string]interface{}) (string, error) {
		if tool != "albums" {
			t.Fatalf("tool = %q, want albums", tool)
		}
		queryText := strings.TrimSpace(args["queryText"].(string))
		artistName := strings.TrimSpace(args["artistName"].(string))
		switch queryText + "::" + artistName {
		case "OK Computer::Radiohead":
			return `{"data":{"albums":[{"name":"OK Computer","artistName":"Radiohead","year":1997}]}}`, nil
		case "The Bends::Radiohead":
			return `{"data":{"albums":[{"name":"The Bends","artistName":"Radiohead","year":1995}]}}`, nil
		default:
			t.Fatalf("unexpected lookup args: %#v", args)
			return "", nil
		}
	}
	defer func() {
		executeToolWithSimilarityImpl = originalToolRunner
	}()

	normalizer := &scriptedTurnNormalizer{
		normalize: func(msg string) normalizedTurn {
			switch strings.TrimSpace(msg) {
			case "Do I have OK Computer by Radiohead in my library?":
				return normalizedTurn{
					Intent:     "album_discovery",
					QueryScope: "library",
					Confidence: "high",
					RawMessage: msg,
				}
			case "What about The Bends by Radiohead?":
				return normalizedTurn{
					Intent:     "album_discovery",
					QueryScope: "general",
					TargetName: "The Bends by Radiohead",
					Confidence: "high",
					RawMessage: msg,
				}
			default:
				return normalizedTurn{}
			}
		},
		classify: func(msg string, turn normalizedTurn, object conversationObjectState) conversationObjectDecision {
			if strings.TrimSpace(msg) != "What about The Bends by Radiohead?" {
				return conversationObjectDecision{}
			}
			return conversationObjectDecision{
				UseActiveObject:    true,
				FollowupMode:       "query_previous_set",
				ReferenceTarget:    "previous_results",
				ConversationOp:     "select",
				QueryScopeOverride: "library",
				Confidence:         "high",
			}
		},
	}

	srv := &Server{
		normalizer:    normalizer,
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx1 := testChatContext("sess-transcript-album-lookup", "req-transcript-album-lookup-1")
	resp1, err := srv.buildChatResponse(ctx1, "Do I have OK Computer by Radiohead in my library?", nil, "")
	if err != nil {
		t.Fatalf("first buildChatResponse() error = %v", err)
	}
	if !strings.Contains(resp1.Response, "Yes, you have OK Computer by Radiohead (1997) in your library.") {
		t.Fatalf("first response = %q", resp1.Response)
	}

	ctx2 := testChatContext("sess-transcript-album-lookup", "req-transcript-album-lookup-2")
	resp2, err := srv.buildChatResponse(ctx2, "What about The Bends by Radiohead?", nil, "")
	if err != nil {
		t.Fatalf("second buildChatResponse() error = %v", err)
	}
	if !strings.Contains(resp2.Response, "Yes, you have The Bends by Radiohead (1995) in your library.") {
		t.Fatalf("second response = %q", resp2.Response)
	}
	if normalizer.lastObject.kind != "library_inventory_lookup" {
		t.Fatalf("conversation object kind = %q, want library_inventory_lookup", normalizer.lastObject.kind)
	}
}

func TestBuildChatResponseTranscriptArtistCandidatesThenRevisitAlbums(t *testing.T) {
	originalToolRunner := executeToolWithSimilarityImpl
	executeToolWithSimilarityImpl = func(_ context.Context, _ *graph.Resolver, _ *similarity.Service, _ string, tool string, args map[string]interface{}) (string, error) {
		if tool != "albums" {
			t.Fatalf("tool = %q, want albums", tool)
		}
		artistName := strings.TrimSpace(args["artistName"].(string))
		switch artistName {
		case "Radiohead":
			return `{"data":{"albums":[{"name":"OK Computer OKNOTOK","artistName":"Radiohead","year":1997}]}}`, nil
		case "Pink Floyd":
			return `{"data":{"albums":[{"name":"The Wall","artistName":"Pink Floyd","year":1979}]}}`, nil
		default:
			t.Fatalf("unexpected artist lookup args: %#v", args)
			return "", nil
		}
	}
	defer func() {
		executeToolWithSimilarityImpl = originalToolRunner
	}()

	sessionID := "sess-transcript-artist-revisit"
	setLastArtistCandidateSet(sessionID, "top artists last month", []artistCandidate{
		{Name: "Radiohead", PlayCount: 20, Score: 0.95},
		{Name: "Pink Floyd", PlayCount: 15, Score: 0.91},
	})
	setLastActiveFocusFromTurn(sessionID, "artist_candidates", "listening_stats", normalizedTurn{
		Intent:     "stats",
		SubIntent:  "library_top_artists",
		QueryScope: "listening",
		PromptHint: "top artists last month",
	})

	normalizer := &scriptedTurnNormalizer{
		normalize: func(msg string) normalizedTurn {
			if strings.TrimSpace(msg) != "From those, give me two to revisit tonight." {
				return normalizedTurn{}
			}
			return normalizedTurn{
				Intent:         "album_discovery",
				QueryScope:     "library",
				SelectionMode:  "top_n",
				SelectionValue: "2",
				Confidence:     "high",
				RawMessage:     msg,
			}
		},
		classify: func(msg string, turn normalizedTurn, object conversationObjectState) conversationObjectDecision {
			if strings.TrimSpace(msg) != "From those, give me two to revisit tonight." {
				return conversationObjectDecision{}
			}
			return conversationObjectDecision{
				UseActiveObject:    true,
				FollowupMode:       "query_previous_set",
				ReferenceTarget:    "previous_results",
				ConversationOp:     "select",
				ResultSetKind:      "artist_candidates",
				QueryScopeOverride: "library",
				Confidence:         "high",
			}
		},
	}

	srv := &Server{
		normalizer:    normalizer,
		resolver:      &graph.Resolver{},
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx := testChatContext(sessionID, "req-transcript-artist-revisit")
	resp, err := srv.buildChatResponse(ctx, "From those, give me two to revisit tonight.", nil, "")
	if err != nil {
		t.Fatalf("buildChatResponse() error = %v", err)
	}
	if !strings.Contains(resp.Response, "OK Computer OKNOTOK by Radiohead") {
		t.Fatalf("response = %q", resp.Response)
	}
	if !strings.Contains(resp.Response, "The Wall by Pink Floyd") {
		t.Fatalf("response = %q", resp.Response)
	}
	if normalizer.lastObject.kind != "artist_candidates" {
		t.Fatalf("conversation object kind = %q, want artist_candidates", normalizer.lastObject.kind)
	}
}

func TestBuildChatResponseTranscriptArtistCandidatesThenRevisitAlbumsThenRecency(t *testing.T) {
	originalToolRunner := executeToolWithSimilarityImpl
	executeToolWithSimilarityImpl = func(_ context.Context, _ *graph.Resolver, _ *similarity.Service, _ string, tool string, args map[string]interface{}) (string, error) {
		if tool != "albums" {
			t.Fatalf("tool = %q, want albums", tool)
		}
		artistName := strings.TrimSpace(args["artistName"].(string))
		switch artistName {
		case "Radiohead":
			return `{"data":{"albums":[{"name":"OK Computer OKNOTOK","artistName":"Radiohead","year":1997,"playCount":14,"lastPlayed":"` + time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339) + `"}]}}`, nil
		case "Pink Floyd":
			return `{"data":{"albums":[{"name":"The Wall","artistName":"Pink Floyd","year":1979,"playCount":3,"lastPlayed":"` + time.Now().UTC().AddDate(-2, 0, 0).Format(time.RFC3339) + `"}]}}`, nil
		default:
			t.Fatalf("unexpected artist lookup args: %#v", args)
			return "", nil
		}
	}
	defer func() {
		executeToolWithSimilarityImpl = originalToolRunner
	}()

	sessionID := "sess-transcript-artist-revisit-recency"
	setLastArtistCandidateSet(sessionID, "top artists last month", []artistCandidate{
		{Name: "Radiohead", PlayCount: 20, Score: 0.95},
		{Name: "Pink Floyd", PlayCount: 15, Score: 0.91},
	})
	setLastActiveFocusFromTurn(sessionID, "artist_candidates", "listening_stats", normalizedTurn{
		Intent:     "stats",
		SubIntent:  "library_top_artists",
		QueryScope: "listening",
		PromptHint: "top artists last month",
	})

	normalizer := &scriptedTurnNormalizer{
		normalize: func(msg string) normalizedTurn {
			switch strings.TrimSpace(msg) {
			case "From those, give me two to revisit tonight.":
				return normalizedTurn{
					Intent:         "album_discovery",
					QueryScope:     "library",
					SelectionMode:  "top_n",
					SelectionValue: "2",
					Confidence:     "high",
					RawMessage:     msg,
				}
			case "Which of those have I played recently?":
				return normalizedTurn{
					Intent:     "listening",
					SubIntent:  "result_set_play_recency",
					QueryScope: "library",
					TimeWindow: "this_year",
					Confidence: "high",
					RawMessage: msg,
				}
			default:
				return normalizedTurn{}
			}
		},
		classify: func(msg string, turn normalizedTurn, object conversationObjectState) conversationObjectDecision {
			switch strings.TrimSpace(msg) {
			case "From those, give me two to revisit tonight.":
				return conversationObjectDecision{
					UseActiveObject:    true,
					FollowupMode:       "query_previous_set",
					ReferenceTarget:    "previous_results",
					ConversationOp:     "select",
					ResultSetKind:      "artist_candidates",
					QueryScopeOverride: "library",
					Confidence:         "high",
				}
			case "Which of those have I played recently?":
				return conversationObjectDecision{
					UseActiveObject:    true,
					FollowupMode:       "query_previous_set",
					ReferenceTarget:    "previous_results",
					ConversationOp:     "inspect",
					ResultSetKind:      "creative_albums",
					SubIntentOverride:  "result_set_play_recency",
					QueryScopeOverride: "library",
					Confidence:         "high",
				}
			default:
				return conversationObjectDecision{}
			}
		},
	}

	srv := &Server{
		normalizer:    normalizer,
		resolver:      &graph.Resolver{},
		chatMemory:    make(map[string]chatSessionMemory),
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}

	ctx1 := testChatContext(sessionID, "req-transcript-artist-revisit-recency-1")
	resp1, err := srv.buildChatResponse(ctx1, "From those, give me two to revisit tonight.", nil, "")
	if err != nil {
		t.Fatalf("first buildChatResponse() error = %v", err)
	}
	if !strings.Contains(resp1.Response, "OK Computer OKNOTOK by Radiohead") {
		t.Fatalf("first response = %q", resp1.Response)
	}
	if !strings.Contains(resp1.Response, "The Wall by Pink Floyd") {
		t.Fatalf("first response = %q", resp1.Response)
	}

	ctx2 := testChatContext(sessionID, "req-transcript-artist-revisit-recency-2")
	resp2, err := srv.buildChatResponse(ctx2, "Which of those have I played recently?", nil, "")
	if err != nil {
		t.Fatalf("second buildChatResponse() error = %v", err)
	}
	if !strings.Contains(resp2.Response, "OK Computer OKNOTOK") {
		t.Fatalf("second response = %q", resp2.Response)
	}
	if strings.Contains(resp2.Response, "The Wall") {
		t.Fatalf("second response included stale album: %q", resp2.Response)
	}
	if normalizer.lastObject.kind != "creative_albums" {
		t.Fatalf("conversation object kind = %q, want creative_albums", normalizer.lastObject.kind)
	}
}

func TestNormalizeResolvedTurnUsesConversationObjectDecisionForInventoryLookupFollowup(t *testing.T) {
	lastActiveFocus.mu.Lock()
	lastActiveFocus.sessions = make(map[string]activeFocusState)
	lastActiveFocus.mu.Unlock()

	sessionID := "sess-inventory-conversation-object"
	setLastActiveFocus(sessionID, "library_inventory_lookup", "album_mode")

	normalizerStub := &stubTurnNormalizer{
		turn: normalizedTurn{
			Intent:       "album_discovery",
			QueryScope:   "general",
			FollowupMode: "none",
		},
		decision: conversationObjectDecision{
			UseActiveObject:    true,
			FollowupMode:       "query_previous_set",
			ReferenceTarget:    "previous_results",
			ConversationOp:     "select",
			IntentOverride:     "album_discovery",
			QueryScopeOverride: "library",
		},
	}
	srv := &Server{normalizer: normalizerStub}

	resolved, err := srv.normalizeResolvedTurn(context.Background(), sessionID, "What about The Bends by Radiohead?", nil, "")
	if err != nil {
		t.Fatalf("normalizeResolvedTurn() error = %v", err)
	}
	if normalizerStub.lastObject.kind != "library_inventory_lookup" {
		t.Fatalf("classifier object kind = %q, want library_inventory_lookup", normalizerStub.lastObject.kind)
	}
	if !resolved.HasConversationObject {
		t.Fatal("expected conversation object to be available")
	}
	if resolved.Turn.FollowupMode != "query_previous_set" {
		t.Fatalf("FollowupMode = %q, want query_previous_set", resolved.Turn.FollowupMode)
	}
	if resolved.Turn.ReferenceTarget != "previous_results" {
		t.Fatalf("ReferenceTarget = %q, want previous_results", resolved.Turn.ReferenceTarget)
	}
	if resolved.Turn.QueryScope != "library" {
		t.Fatalf("QueryScope = %q, want library", resolved.Turn.QueryScope)
	}
	if resolved.Turn.Intent != "album_discovery" {
		t.Fatalf("Intent = %q, want album_discovery", resolved.Turn.Intent)
	}
	if resolved.Turn.ConversationOp != "select" {
		t.Fatalf("ConversationOp = %q, want select", resolved.Turn.ConversationOp)
	}
}

func TestNormalizeResolvedTurnCanReclassifyInventoryContinuationFromOther(t *testing.T) {
	lastActiveFocus.mu.Lock()
	lastActiveFocus.sessions = make(map[string]activeFocusState)
	lastActiveFocus.mu.Unlock()

	sessionID := "sess-inventory-reclassify-conversation-object"
	setLastActiveFocus(sessionID, "library_inventory_lookup", "track_mode")

	normalizerStub := &stubTurnNormalizer{
		turn: normalizedTurn{
			Intent:       "other",
			QueryScope:   "unknown",
			FollowupMode: "none",
		},
		decision: conversationObjectDecision{
			UseActiveObject:    true,
			FollowupMode:       "query_previous_set",
			ReferenceTarget:    "previous_results",
			ConversationOp:     "select",
			IntentOverride:     "track_discovery",
			SubIntentOverride:  "track_search",
			QueryScopeOverride: "library",
		},
	}
	srv := &Server{normalizer: normalizerStub}

	resolved, err := srv.normalizeResolvedTurn(context.Background(), sessionID, "What about Heart-Shaped Box by Nirvana?", nil, "")
	if err != nil {
		t.Fatalf("normalizeResolvedTurn() error = %v", err)
	}
	if resolved.Turn.Intent != "track_discovery" {
		t.Fatalf("Intent = %q, want track_discovery", resolved.Turn.Intent)
	}
	if resolved.Turn.SubIntent != "track_search" {
		t.Fatalf("SubIntent = %q, want track_search", resolved.Turn.SubIntent)
	}
	if resolved.Turn.QueryScope != "library" {
		t.Fatalf("QueryScope = %q, want library", resolved.Turn.QueryScope)
	}
	if resolved.Turn.ReferenceTarget != "previous_results" {
		t.Fatalf("ReferenceTarget = %q, want previous_results", resolved.Turn.ReferenceTarget)
	}
	if resolved.Turn.ConversationOp != "select" {
		t.Fatalf("ConversationOp = %q, want select", resolved.Turn.ConversationOp)
	}
}

func TestNormalizeResolvedTurnCanPromoteInventoryContinuationToAlbumLookup(t *testing.T) {
	lastActiveFocus.mu.Lock()
	lastActiveFocus.sessions = make(map[string]activeFocusState)
	lastActiveFocus.mu.Unlock()

	sessionID := "sess-inventory-promote-album-conversation-object"
	setLastActiveFocus(sessionID, "library_inventory_lookup", "track_mode")

	normalizerStub := &stubTurnNormalizer{
		turn: normalizedTurn{
			Intent:         "track_discovery",
			QueryScope:     "general",
			ArtistName:     "Radiohead",
			SelectionValue: "The Bends by Radiohead",
			FollowupMode:   "none",
		},
		decision: conversationObjectDecision{
			UseActiveObject:    true,
			FollowupMode:       "query_previous_set",
			ReferenceTarget:    "previous_results",
			ConversationOp:     "select",
			IntentOverride:     "album_discovery",
			QueryScopeOverride: "library",
		},
	}
	srv := &Server{normalizer: normalizerStub}

	resolved, err := srv.normalizeResolvedTurn(context.Background(), sessionID, "What about The Bends by Radiohead?", nil, "")
	if err != nil {
		t.Fatalf("normalizeResolvedTurn() error = %v", err)
	}
	if resolved.Turn.Intent != "album_discovery" {
		t.Fatalf("Intent = %q, want album_discovery", resolved.Turn.Intent)
	}
	if resolved.Turn.QueryScope != "library" {
		t.Fatalf("QueryScope = %q, want library", resolved.Turn.QueryScope)
	}
	if resolved.Turn.FollowupMode != "query_previous_set" {
		t.Fatalf("FollowupMode = %q, want query_previous_set", resolved.Turn.FollowupMode)
	}
	if resolved.Turn.ConversationOp != "select" {
		t.Fatalf("ConversationOp = %q, want select", resolved.Turn.ConversationOp)
	}
}

func TestNormalizeResolvedTurnUsesConversationObjectDecisionForArtistCatalogDepth(t *testing.T) {
	lastActiveFocus.mu.Lock()
	lastActiveFocus.sessions = make(map[string]activeFocusState)
	lastActiveFocus.mu.Unlock()
	lastArtistCandidateSet.mu.Lock()
	lastArtistCandidateSet.sessions = make(map[string]artistCandidateSetState)
	lastArtistCandidateSet.mu.Unlock()

	sessionID := "sess-artist-catalog-depth-conversation-object"
	setLastArtistCandidateSet(sessionID, "top artists last month", []artistCandidate{
		{Name: "Radiohead", PlayCount: 12, Score: 9},
		{Name: "Massive Attack", PlayCount: 7, Score: 4},
	})
	setLastActiveFocusFromTurn(sessionID, "artist_candidates", "listening_stats", normalizedTurn{
		Intent:     "stats",
		SubIntent:  "library_top_artists",
		QueryScope: "listening",
	})

	normalizerStub := &stubTurnNormalizer{
		turn: normalizedTurn{
			Intent:       "stats",
			QueryScope:   "unknown",
			FollowupMode: "none",
		},
		decision: conversationObjectDecision{
			UseActiveObject:    true,
			FollowupMode:       "query_previous_set",
			ReferenceTarget:    "previous_results",
			ConversationOp:     "inspect",
			SubIntentOverride:  "artist_catalog_depth",
			ResultSetKind:      "artist_candidates",
			QueryScopeOverride: "listening",
		},
	}
	srv := &Server{normalizer: normalizerStub}

	resolved, err := srv.normalizeResolvedTurn(context.Background(), sessionID, "Who has the deepest catalog?", nil, "")
	if err != nil {
		t.Fatalf("normalizeResolvedTurn() error = %v", err)
	}
	if resolved.Turn.SubIntent != "artist_catalog_depth" {
		t.Fatalf("SubIntent = %q, want artist_catalog_depth", resolved.Turn.SubIntent)
	}
	if resolved.Turn.FollowupMode != "query_previous_set" {
		t.Fatalf("FollowupMode = %q, want query_previous_set", resolved.Turn.FollowupMode)
	}
	if resolved.Turn.ReferenceTarget != "previous_results" {
		t.Fatalf("ReferenceTarget = %q, want previous_results", resolved.Turn.ReferenceTarget)
	}
	if resolved.Turn.ResultSetKind != "artist_candidates" {
		t.Fatalf("ResultSetKind = %q, want artist_candidates", resolved.Turn.ResultSetKind)
	}
	if resolved.ResolvedReferenceKind != "artist_candidates" {
		t.Fatalf("ResolvedReferenceKind = %q, want artist_candidates", resolved.ResolvedReferenceKind)
	}
	if resolved.Turn.ConversationOp != "inspect" {
		t.Fatalf("ConversationOp = %q, want inspect", resolved.Turn.ConversationOp)
	}
}

func TestNormalizeResolvedTurnUsesConversationObjectDecisionForCreativeArtistFollowup(t *testing.T) {
	lastActiveFocus.mu.Lock()
	lastActiveFocus.sessions = make(map[string]activeFocusState)
	lastActiveFocus.mu.Unlock()
	lastCreativeAlbumSet.mu.Lock()
	lastCreativeAlbumSet.sessions = make(map[string]creativeAlbumSetState)
	lastCreativeAlbumSet.mu.Unlock()

	sessionID := "sess-creative-artist-conversation-object"
	setLastCreativeAlbumSet(sessionID, "semantic_structured", "rainy late-night walk", []creativeAlbumCandidate{
		{Name: "Kid A", ArtistName: "Radiohead"},
		{Name: "Moon Safari", ArtistName: "Air"},
	})
	setLastActiveFocusFromTurn(sessionID, "creative_albums", "result_set", normalizedTurn{
		QueryScope:  "library",
		LibraryOnly: boolPtr(true),
	})

	normalizerStub := &stubTurnNormalizer{
		turn: normalizedTurn{
			Intent:       "album_discovery",
			QueryScope:   "general",
			FollowupMode: "none",
		},
		decision: conversationObjectDecision{
			UseActiveObject:    true,
			FollowupMode:       "query_previous_set",
			ReferenceTarget:    "previous_results",
			ConversationOp:     "select",
			IntentOverride:     "album_discovery",
			QueryScopeOverride: "library",
			ResultSetKind:      "creative_albums",
			SelectionMode:      "top_n",
			SelectionValue:     "1",
		},
	}
	srv := &Server{normalizer: normalizerStub}

	resolved, err := srv.normalizeResolvedTurn(context.Background(), sessionID, "Then give me one Radiohead album I should revisit tonight.", nil, "")
	if err != nil {
		t.Fatalf("normalizeResolvedTurn() error = %v", err)
	}
	if resolved.Turn.FollowupMode != "query_previous_set" {
		t.Fatalf("FollowupMode = %q, want query_previous_set", resolved.Turn.FollowupMode)
	}
	if resolved.Turn.ReferenceTarget != "previous_results" {
		t.Fatalf("ReferenceTarget = %q, want previous_results", resolved.Turn.ReferenceTarget)
	}
	if resolved.Turn.ResultSetKind != "creative_albums" {
		t.Fatalf("ResultSetKind = %q, want creative_albums", resolved.Turn.ResultSetKind)
	}
	if resolved.Turn.ArtistName != "Radiohead" {
		t.Fatalf("ArtistName = %q, want Radiohead", resolved.Turn.ArtistName)
	}
	if resolved.Turn.QueryScope != "library" {
		t.Fatalf("QueryScope = %q, want library", resolved.Turn.QueryScope)
	}
	if resolved.Turn.LibraryOnly == nil || !*resolved.Turn.LibraryOnly {
		t.Fatalf("LibraryOnly = %#v, want true", resolved.Turn.LibraryOnly)
	}
	if resolved.Turn.SelectionMode != "top_n" || resolved.Turn.SelectionValue != "1" {
		t.Fatalf("selection = %q/%q, want top_n/1", resolved.Turn.SelectionMode, resolved.Turn.SelectionValue)
	}
	if resolved.ResolvedReferenceKind != "creative_albums" {
		t.Fatalf("ResolvedReferenceKind = %q, want creative_albums", resolved.ResolvedReferenceKind)
	}
}
