package main

import (
	"context"

	"groovarr/internal/agent"
)

type serverExecutionHandler struct {
	name            string
	canHandle       func(*Turn) bool
	execute         func(context.Context, *Server, []agent.Message, *resolvedTurnContext) (ChatResponse, bool)
	executeWithTurn func(context.Context, *Server, []agent.Message, *Turn) (ChatResponse, bool)
}

func (h serverExecutionHandler) CanHandle(turn *Turn) bool {
	return h.canHandle != nil && h.canHandle(turn)
}

func (h serverExecutionHandler) Execute(ctx context.Context, s *Server, history []agent.Message, turn *Turn) (ChatResponse, bool) {
	if h.executeWithTurn != nil {
		return h.executeWithTurn(ctx, s, history, turn)
	}
	if h.execute == nil {
		return ChatResponse{}, false
	}
	resolved := executionResolvedTurnContext(turn)
	if resolved == nil {
		return ChatResponse{}, false
	}
	return h.execute(ctx, s, history, resolved)
}

func executionResolvedTurnContext(turn *Turn) *resolvedTurnContext {
	return applyServerExecutionRequest(turnToResolvedTurnContext(turn), executionRequestFromTurn(turn))
}

func currentServerExecutionHandlers() []serverExecutionHandler {
	handlers := make([]serverExecutionHandler, 0, 12)
	handlers = append(handlers, discoveryExecutionHandlers()...)
	handlers = append(handlers, cleanupExecutionHandlers()...)
	handlers = append(handlers, sceneExecutionHandlers()...)
	handlers = append(handlers, creativeExecutionHandlers()...)
	handlers = append(handlers, trackExecutionHandlers()...)
	handlers = append(handlers, playlistExecutionHandlers()...)
	return handlers
}
