package main

import (
	"context"

	"groovarr/internal/agent"
)

type serverExecutionHandler struct {
	name      string
	canHandle func(serverExecutionRequest) bool
	execute   func(context.Context, *Server, []agent.Message, *resolvedTurnContext) (ChatResponse, bool)
}

func (h serverExecutionHandler) CanHandle(request serverExecutionRequest) bool {
	return h.canHandle != nil && h.canHandle(request)
}

func (h serverExecutionHandler) Execute(ctx context.Context, s *Server, history []agent.Message, resolved *resolvedTurnContext) (ChatResponse, bool) {
	if h.execute == nil {
		return ChatResponse{}, false
	}
	return h.execute(ctx, s, history, resolved)
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
