package main

import (
	"context"
	"strings"
	"time"
)

func (s *Server) runWorkflowWithDedupe(
	ctx context.Context,
	workflowName string,
	dedupeKey string,
	run func(context.Context) (string, bool, error),
) (string, bool, error) {
	key := strings.TrimSpace(workflowName) + "::" + strings.TrimSpace(dedupeKey)
	if key == "::" {
		return run(ctx)
	}

	now := time.Now().UTC()
	s.workflowMu.Lock()
	for k, cached := range s.workflowCache {
		if now.After(cached.expires) {
			delete(s.workflowCache, k)
		}
	}
	if cached, ok := s.workflowCache[key]; ok && now.Before(cached.expires) {
		s.workflowMu.Unlock()
		return cached.response, cached.handled, nil
	}
	if inflight, ok := s.workflowRuns[key]; ok {
		s.workflowMu.Unlock()
		select {
		case <-ctx.Done():
			return "", false, ctx.Err()
		case <-inflight.done:
			return inflight.response, inflight.handled, inflight.err
		}
	}

	state := &workflowRunState{done: make(chan struct{})}
	s.workflowRuns[key] = state
	s.workflowMu.Unlock()

	resp, handled, err := run(ctx)

	s.workflowMu.Lock()
	state.response = resp
	state.handled = handled
	state.err = err
	if err == nil && handled {
		s.workflowCache[key] = workflowCacheEntry{
			response: resp,
			handled:  true,
			expires:  time.Now().UTC().Add(2 * time.Minute),
		}
	}
	delete(s.workflowRuns, key)
	close(state.done)
	s.workflowMu.Unlock()

	return resp, handled, err
}
