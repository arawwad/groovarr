package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type chatSessionArchive struct {
	dir            string
	retention      time.Duration
	mu             sync.Mutex
	lastCleanupDay string
}

type archivedChatEvent struct {
	Timestamp string            `json:"timestamp"`
	SessionID string            `json:"sessionId"`
	RequestID string            `json:"requestId,omitempty"`
	Kind      string            `json:"kind"`
	Stage     string            `json:"stage,omitempty"`
	Message   string            `json:"message,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
	Tool      string            `json:"tool,omitempty"`
	Args      string            `json:"args,omitempty"`
	Result    string            `json:"result,omitempty"`
	Error     string            `json:"error,omitempty"`
}

type archivedToolView struct {
	Timestamp string `json:"timestamp"`
	Phase     string `json:"phase"`
	Tool      string `json:"tool"`
	Args      string `json:"args,omitempty"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
}

type archivedRequestView struct {
	RequestID      string              `json:"requestId"`
	StartedAt      string              `json:"startedAt,omitempty"`
	EndedAt        string              `json:"endedAt,omitempty"`
	UserMessage    string              `json:"userMessage,omitempty"`
	RouteStages    []string            `json:"routeStages,omitempty"`
	Tools          []archivedToolView  `json:"tools,omitempty"`
	Events         []archivedChatEvent `json:"events,omitempty"`
	LatestResponse string              `json:"latestResponse,omitempty"`
}

type archivedSessionView struct {
	SessionID         string                `json:"sessionId"`
	StartedAt         string                `json:"startedAt,omitempty"`
	EndedAt           string                `json:"endedAt,omitempty"`
	RequestCount      int                   `json:"requestCount"`
	LatestUserMessage string                `json:"latestUserMessage,omitempty"`
	Requests          []archivedRequestView `json:"requests,omitempty"`
}

var activeChatSessionArchive *chatSessionArchive

func installChatSessionArchive(archive *chatSessionArchive) {
	activeChatSessionArchive = archive
}

func newChatSessionArchive() *chatSessionArchive {
	if !envBool("CHAT_SESSION_ARCHIVE_ENABLED", false) {
		return nil
	}
	dir := strings.TrimSpace(os.Getenv("CHAT_SESSION_ARCHIVE_DIR"))
	if dir == "" {
		dir = "/groovarr-data/debug/chat-sessions"
	}
	retentionDays := envInt("CHAT_SESSION_ARCHIVE_RETENTION_DAYS", 31)
	if retentionDays < 1 {
		retentionDays = 31
	}
	return &chatSessionArchive{
		dir:       dir,
		retention: time.Duration(retentionDays) * 24 * time.Hour,
	}
}

func (a *chatSessionArchive) RecordPipelineStage(ctx context.Context, stage string, fields map[string]string) {
	if a == nil {
		return
	}
	copied := make(map[string]string, len(fields))
	for key, value := range fields {
		key = strings.TrimSpace(key)
		value = compactText(strings.TrimSpace(value), 500)
		if key == "" || value == "" {
			continue
		}
		copied[key] = value
	}
	event := archivedChatEvent{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		SessionID: chatSessionIDFromContext(ctx),
		RequestID: chatRequestIDFromContext(ctx),
		Kind:      "pipeline",
		Stage:     strings.TrimSpace(stage),
		Fields:    copied,
	}
	if event.Stage == "request" {
		event.Kind = "request"
		event.Message = copied["message"]
		delete(event.Fields, "message")
		if len(event.Fields) == 0 {
			event.Fields = nil
		}
	}
	a.appendEvent(event)
}

func (a *chatSessionArchive) RecordToolStart(ctx context.Context, tool string, args map[string]interface{}) {
	if a == nil {
		return
	}
	event := archivedChatEvent{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		SessionID: chatSessionIDFromContext(ctx),
		RequestID: chatRequestIDFromContext(ctx),
		Kind:      "tool",
		Stage:     "start",
		Tool:      strings.TrimSpace(tool),
		Args:      compactJSON(args, 1200),
	}
	a.appendEvent(event)
}

func (a *chatSessionArchive) RecordToolResult(ctx context.Context, tool, result string, err error) {
	if a == nil {
		return
	}
	event := archivedChatEvent{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		SessionID: chatSessionIDFromContext(ctx),
		RequestID: chatRequestIDFromContext(ctx),
		Kind:      "tool",
		Stage:     "result",
		Tool:      strings.TrimSpace(tool),
		Result:    compactText(strings.TrimSpace(result), 1800),
	}
	if err != nil {
		event.Error = compactText(err.Error(), 400)
	}
	a.appendEvent(event)
}

func (a *chatSessionArchive) RecordResponse(ctx context.Context, response ChatResponse, err error) {
	if a == nil {
		return
	}
	event := archivedChatEvent{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		SessionID: chatSessionIDFromContext(ctx),
		RequestID: chatRequestIDFromContext(ctx),
		Kind:      "response",
		Result:    compactText(strings.TrimSpace(response.Response), 2200),
	}
	if response.PendingAction != nil {
		if event.Fields == nil {
			event.Fields = make(map[string]string, 1)
		}
		event.Fields["pendingActionId"] = strings.TrimSpace(response.PendingAction.ID)
	}
	if err != nil {
		event.Error = compactText(err.Error(), 400)
	}
	a.appendEvent(event)
}

func (a *chatSessionArchive) appendEvent(event archivedChatEvent) {
	if a == nil {
		return
	}
	if strings.TrimSpace(event.Timestamp) == "" {
		event.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if strings.TrimSpace(event.SessionID) == "" {
		event.SessionID = "global"
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return
	}

	ts, err := time.Parse(time.RFC3339Nano, event.Timestamp)
	if err != nil {
		ts = time.Now().UTC()
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if err := os.MkdirAll(a.dir, 0o755); err != nil {
		return
	}
	a.cleanupOldFilesLocked(ts.UTC())

	path := filepath.Join(a.dir, ts.UTC().Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(encoded, '\n'))
}

func (a *chatSessionArchive) cleanupOldFilesLocked(now time.Time) {
	dayKey := now.UTC().Format("2006-01-02")
	if a.lastCleanupDay == dayKey {
		return
	}
	a.lastCleanupDay = dayKey
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return
	}
	cutoff := now.Add(-a.retention).UTC()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		stamp := strings.TrimSuffix(name, ".jsonl")
		day, err := time.Parse("2006-01-02", stamp)
		if err != nil {
			continue
		}
		if day.Before(midnightUTC(cutoff)) {
			_ = os.Remove(filepath.Join(a.dir, name))
		}
	}
}

func (a *chatSessionArchive) readEvents(since, until time.Time, sessionID string) ([]archivedChatEvent, error) {
	if a == nil {
		return nil, errors.New("chat session archive disabled")
	}
	since = since.UTC()
	until = until.UTC()
	if until.Before(since) {
		return nil, nil
	}
	events := make([]archivedChatEvent, 0, 256)
	for day := midnightUTC(since); !day.After(until); day = day.AddDate(0, 0, 1) {
		path := filepath.Join(a.dir, day.Format("2006-01-02")+".jsonl")
		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			var event archivedChatEvent
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				continue
			}
			if sessionID != "" && event.SessionID != sessionID {
				continue
			}
			ts, err := time.Parse(time.RFC3339Nano, event.Timestamp)
			if err != nil {
				continue
			}
			if ts.Before(since) || ts.After(until) {
				continue
			}
			events = append(events, event)
		}
		_ = f.Close()
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp < events[j].Timestamp
	})
	return events, nil
}

func (s *Server) handleChatSessionsDebug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.chatArchive == nil {
		http.Error(w, "Chat session archive is disabled", http.StatusServiceUnavailable)
		return
	}
	since, until := parseDebugTimeWindow(r)
	events, err := s.chatArchive.readEvents(since, until, strings.TrimSpace(r.URL.Query().Get("sessionId")))
	if err != nil {
		http.Error(w, "Failed to load chat sessions", http.StatusInternalServerError)
		return
	}
	limit := parseDebugLimit(r, 50, 200)
	s.sendJSON(w, map[string]interface{}{
		"since":    since.UTC().Format(time.RFC3339),
		"until":    until.UTC().Format(time.RFC3339),
		"sessions": buildArchivedSessionViews(events, false, limit),
	})
}

func (s *Server) handleChatSessionDebug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.chatArchive == nil {
		http.Error(w, "Chat session archive is disabled", http.StatusServiceUnavailable)
		return
	}
	sessionID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/debug/chat-sessions/"))
	if sessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}
	since, until := parseDebugTimeWindow(r)
	events, err := s.chatArchive.readEvents(since, until, sessionID)
	if err != nil {
		http.Error(w, "Failed to load chat session", http.StatusInternalServerError)
		return
	}
	sessions := buildArchivedSessionViews(events, true, 1)
	if len(sessions) == 0 {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	s.sendJSON(w, sessions[0])
}

func buildArchivedSessionViews(events []archivedChatEvent, includeEvents bool, limit int) []archivedSessionView {
	type requestAgg struct {
		startedAt      string
		endedAt        string
		userMessage    string
		routeStages    []string
		stageSeen      map[string]struct{}
		tools          []archivedToolView
		events         []archivedChatEvent
		latestResponse string
	}
	type sessionAgg struct {
		startedAt         string
		endedAt           string
		latestUserMessage string
		requests          map[string]*requestAgg
		requestOrder      []string
	}

	sessions := make(map[string]*sessionAgg)
	sessionOrder := make([]string, 0, 32)
	for _, event := range events {
		sessionID := strings.TrimSpace(event.SessionID)
		if sessionID == "" {
			sessionID = "global"
		}
		agg, ok := sessions[sessionID]
		if !ok {
			agg = &sessionAgg{requests: make(map[string]*requestAgg)}
			sessions[sessionID] = agg
			sessionOrder = append(sessionOrder, sessionID)
		}
		if agg.startedAt == "" || event.Timestamp < agg.startedAt {
			agg.startedAt = event.Timestamp
		}
		if agg.endedAt == "" || event.Timestamp > agg.endedAt {
			agg.endedAt = event.Timestamp
		}

		requestID := strings.TrimSpace(event.RequestID)
		if requestID == "" {
			continue
		}
		reqAgg, ok := agg.requests[requestID]
		if !ok {
			reqAgg = &requestAgg{stageSeen: make(map[string]struct{})}
			agg.requests[requestID] = reqAgg
			agg.requestOrder = append(agg.requestOrder, requestID)
		}
		if reqAgg.startedAt == "" || event.Timestamp < reqAgg.startedAt {
			reqAgg.startedAt = event.Timestamp
		}
		if reqAgg.endedAt == "" || event.Timestamp > reqAgg.endedAt {
			reqAgg.endedAt = event.Timestamp
		}
		switch event.Kind {
		case "request":
			if reqAgg.userMessage == "" {
				reqAgg.userMessage = event.Message
			}
			if strings.TrimSpace(event.Message) != "" {
				agg.latestUserMessage = event.Message
			}
		case "pipeline":
			stage := strings.TrimSpace(event.Stage)
			if stage != "" {
				if _, seen := reqAgg.stageSeen[stage]; !seen {
					reqAgg.stageSeen[stage] = struct{}{}
					reqAgg.routeStages = append(reqAgg.routeStages, stage)
				}
			}
		case "tool":
			reqAgg.tools = append(reqAgg.tools, archivedToolView{
				Timestamp: event.Timestamp,
				Phase:     event.Stage,
				Tool:      event.Tool,
				Args:      event.Args,
				Result:    event.Result,
				Error:     event.Error,
			})
		case "response":
			if strings.TrimSpace(event.Result) != "" {
				reqAgg.latestResponse = event.Result
			}
		}
		if includeEvents {
			reqAgg.events = append(reqAgg.events, event)
		}
	}

	sort.Slice(sessionOrder, func(i, j int) bool {
		left := sessions[sessionOrder[i]]
		right := sessions[sessionOrder[j]]
		return left.endedAt > right.endedAt
	})

	if limit > 0 && len(sessionOrder) > limit {
		sessionOrder = sessionOrder[:limit]
	}

	out := make([]archivedSessionView, 0, len(sessionOrder))
	for _, sessionID := range sessionOrder {
		agg := sessions[sessionID]
		requests := make([]archivedRequestView, 0, len(agg.requestOrder))
		for _, requestID := range agg.requestOrder {
			reqAgg := agg.requests[requestID]
			requests = append(requests, archivedRequestView{
				RequestID:      requestID,
				StartedAt:      reqAgg.startedAt,
				EndedAt:        reqAgg.endedAt,
				UserMessage:    reqAgg.userMessage,
				RouteStages:    reqAgg.routeStages,
				Tools:          reqAgg.tools,
				Events:         reqAgg.events,
				LatestResponse: reqAgg.latestResponse,
			})
		}
		out = append(out, archivedSessionView{
			SessionID:         sessionID,
			StartedAt:         agg.startedAt,
			EndedAt:           agg.endedAt,
			RequestCount:      len(requests),
			LatestUserMessage: agg.latestUserMessage,
			Requests:          requests,
		})
	}
	return out
}

func parseDebugTimeWindow(r *http.Request) (time.Time, time.Time) {
	now := time.Now().UTC()
	until := now
	if raw := strings.TrimSpace(r.URL.Query().Get("until")); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			until = parsed.UTC()
		}
	}
	since := until.Add(-7 * 24 * time.Hour)
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		if parsed, err := parseRelativeDebugWindow(raw); err == nil {
			since = until.Add(-parsed)
		} else if absolute, err := time.Parse(time.RFC3339, raw); err == nil {
			since = absolute.UTC()
		}
	}
	if since.After(until) {
		since = until.Add(-7 * 24 * time.Hour)
	}
	return since, until
}

func parseRelativeDebugWindow(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return 0, errors.New("empty window")
	}
	switch {
	case strings.HasSuffix(raw, "d"):
		v, err := strconv.Atoi(strings.TrimSuffix(raw, "d"))
		if err != nil || v <= 0 {
			return 0, errors.New("invalid day window")
		}
		return time.Duration(v) * 24 * time.Hour, nil
	case strings.HasSuffix(raw, "w"):
		v, err := strconv.Atoi(strings.TrimSuffix(raw, "w"))
		if err != nil || v <= 0 {
			return 0, errors.New("invalid week window")
		}
		return time.Duration(v) * 7 * 24 * time.Hour, nil
	case strings.HasSuffix(raw, "m"):
		v, err := strconv.Atoi(strings.TrimSuffix(raw, "m"))
		if err != nil || v <= 0 {
			return 0, errors.New("invalid month window")
		}
		return time.Duration(v) * 30 * 24 * time.Hour, nil
	default:
		return time.ParseDuration(raw)
	}
}

func parseDebugLimit(r *http.Request, fallback, max int) int {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	if v > max {
		return max
	}
	return v
}

func midnightUTC(ts time.Time) time.Time {
	ts = ts.UTC()
	return time.Date(ts.Year(), ts.Month(), ts.Day(), 0, 0, 0, 0, time.UTC)
}

func compactJSON(v interface{}, maxChars int) string {
	if v == nil {
		return ""
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return compactText(string(raw), maxChars)
}
