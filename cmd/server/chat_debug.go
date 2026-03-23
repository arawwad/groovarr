package main

import (
	"context"
	"strings"

	"github.com/rs/zerolog/log"
)

func chatPipelineDebugEnabled() bool {
	return envBool("CHAT_PIPELINE_DEBUG", false)
}

func logChatPipelineStage(ctx context.Context, stage string, fields map[string]string) {
	if activeChatSessionArchive != nil {
		activeChatSessionArchive.RecordPipelineStage(ctx, stage, fields)
	}
	if !chatPipelineDebugEnabled() {
		return
	}
	event := log.Info().
		Str("request_id", chatRequestIDFromContext(ctx)).
		Str("session_id", chatSessionIDFromContext(ctx)).
		Str("stage", strings.TrimSpace(stage))
	for key, value := range fields {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		event = event.Str(key, compactText(value, 300))
	}
	event.Msg("Chat pipeline")
}
