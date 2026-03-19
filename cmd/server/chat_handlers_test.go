package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"groovarr/internal/agent"
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
	lastMsg     string
	lastHistory []agent.Message
	lastContext string
}

type stubTurnPlanner struct {
	plan        orchestrationDecision
	err         error
	lastMsg     string
	lastHistory []agent.Message
	lastContext string
	lastTurn    *resolvedTurnContext
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

func (s *stubTurnPlanner) PlanTurn(_ context.Context, msg string, history []agent.Message, resolved *resolvedTurnContext, sessionContext string) (orchestrationDecision, error) {
	s.lastMsg = msg
	s.lastHistory = append([]agent.Message(nil), history...)
	s.lastContext = sessionContext
	if resolved != nil {
		copied := *resolved
		s.lastTurn = &copied
	} else {
		s.lastTurn = nil
	}
	return s.plan, s.err
}

type stubTurnResolver struct {
	decision    resultSetResolverDecision
	err         error
	lastRequest resultSetResolverRequest
}

func (s *stubTurnResolver) ResolveTurn(_ context.Context, request resultSetResolverRequest) (resultSetResolverDecision, error) {
	s.lastRequest = request
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
